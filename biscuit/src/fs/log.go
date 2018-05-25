package fs

import "fmt"
import "strconv"

import "common"
import "sync/atomic"

const log_debug = false

// File system journal.  The file system brackets FS calls (e.g.,create) with
// Op_begin and Op_end(); the log makes sure that these operations happen
// atomically with respect to crashes.  Operations are grouped in
// transactions. A transaction is committed to the on-disk log on sync() or when
// the log is close to full.  Transactions in the log are applied to the file
// system when the on-disk log close to full.  All writes go through the log,
// but ordered writes are not appended to the on-disk log, but overwrite their
// home location.  The file system should use logged writes for all its data
// structures, and use ordered writes only for file data.  The file system must
// guarantee that it performs no more than maxblkspersys logged writes in an
// operation, to ensure that its operation will fit in the log.

const LogOffset = 1 // log block 0 is used for head

// an upperbound on the number of blocks written per system call. this is
// necessary in order to guarantee that the log is long enough for the allowed
// number of concurrent fs syscalls.
const MaxBlkPerOp = 10
const MaxOrdered = 5000
const MaxDescriptor = common.BSIZE / 8

type opid_t int

//
// The public interface to the logging layer
//

func (log *log_t) Op_begin(s string) opid_t {
	if memfs {
		return 0
	}
	if log_debug {
		fmt.Printf("op_begin: admit? %v\n", s)
	}
	opid := <-log.admission
	if log_debug {
		fmt.Printf("op_begin: go %d %v\n", opid, s)
	}
	return opid
}

func (log *log_t) Op_end(opid opid_t) {
	if memfs {
		return
	}
	if log_debug {
		fmt.Printf("op_end: done %d\n", opid)
	}
	log.done <- opid
}

// ensure any fs ops in the journal preceding this sync call are flushed to disk
// by waiting for log commit.
func (log *log_t) Force() {
	if memfs {
		panic("memfs")
	}
	if log_debug {
		fmt.Printf("log force\n")
	}
	log.force <- true
	c := <-log.forcewait
	<-c
}

// Write increments ref so that the log has always a valid ref to the buf's page
// the logging layer refdowns when it it is done with the page.  the caller of
// log_write shouldn't hold buf's lock.
func (log *log_t) Write(opid opid_t, b *common.Bdev_block_t) {
	if memfs {
		return
	}
	if log_debug {
		fmt.Printf("log_write logged %d blk %v\n", opid, b.Block)
	}
	log.fs.bcache.Refup(b, "log_write")
	log.incoming <- buf_t{opid, b, false}
}

func (log *log_t) Write_ordered(opid opid_t, b *common.Bdev_block_t) {
	if memfs {
		return
	}
	if log_debug {
		fmt.Printf("log_write_ordered %d %v\n", opid, b.Block)
	}
	log.fs.bcache.Refup(b, "log_write_ordered")
	log.incoming <- buf_t{opid, b, true}
}

func (log *log_t) Loglen() int {
	return log.loglen
}

// All layers above log read blocks through the log layer, which are mostly
// wrappers for the the corresponding cache operations.
func (log *log_t) Get_fill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.fs.bcache.Get_fill(blkn, s, lock)
}

func (log *log_t) Get_zero(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.fs.bcache.Get_zero(blkn, s, lock)
}

func (log *log_t) Get_nofill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.fs.bcache.Get_nofill(blkn, s, lock)
}

func (log *log_t) Relse(blk *common.Bdev_block_t, s string) {
	log.fs.bcache.Relse(blk, s)
}

func (log *log_t) Stats() string {
	s := "log:"
	s += "\n\tnlogwrite "
	s += strconv.Itoa(log.nlogwrite)
	s += "\n\tnorderedwrite "
	s += strconv.Itoa(log.norderedwrite)
	s += "\n\tnorder2logwrite "
	s += strconv.Itoa(log.norder2logwrite)
	s += "\n\tnabsorb "
	s += strconv.Itoa(log.nabsorption)
	s += "\n\tnblkcommited "
	s += strconv.Itoa(log.nblkcommitted)
	s += "\n\tncommit "
	s += strconv.Itoa(log.ncommit)
	s += "\n\tmaxblks_per_commit "
	s += strconv.Itoa(log.maxblks_per_op)
	s += "\n\tnapply "
	s += strconv.Itoa(log.napply)
	s += "\n\tnblkapply "
	s += strconv.Itoa(log.nblkapply)
	s += "\n\tnabsorbapply "
	s += strconv.Itoa(log.nabsorbapply)
	s += "\n\tnforce "
	s += strconv.Itoa(log.nforce)
	s += "\n\tnwriteordered "
	s += strconv.Itoa(log.nwriteordered)
	s += "\n\tnccommit "
	s += strconv.Itoa(log.nccommit)
	s += "\n"
	return s
}

func StartLog(logstart, loglen int, fs *Fs_t, disk common.Disk_i) *log_t {
	log := &log_t{}
	log.fs = fs
	log.mk_log(logstart, loglen, disk)
	head := log.recover()
	log.applytail = int32(head)
	if !memfs {
		go log.log_daemon(head)
		go log.commit_daemon(head)
		go log.apply_daemon(head)
	}
	return log
}

func (log *log_t) StopLog() {
	log.stop <- true
	<-log.stop
	log.commitc <- nil
	<-log.commitc
	log.applyc <- -1
	<-log.applyc
}

//
// Log implementation
//

func logindex(j, loglen int) int {
	return j % loglen
}

func loginc(i, loglen int) int {
	return (i + 1) % loglen
}

func logdec(i, loglen int) int {
	i = i - 1
	if i < 0 {
		i += loglen
	}
	return i
}

func logsize(s, e, loglen int) int {
	n := e - s
	if n < 0 {
		n += loglen
	}
	return n
}

type buf_t struct {
	opid    opid_t
	block   *common.Bdev_block_t
	ordered bool
}

type trans_t struct {
	start          int // index where this trans starts in on-disk log
	head           int // index into in-memory log
	loglen         int // length of log (should be >= 2 * maxtrans)
	maxtrans       int
	ops            map[opid_t]bool              // ops in progress this transaction
	logged         *common.BlkList_t            // list of to-be-logged blocks
	ordered        *common.BlkList_t            // list of ordered blocks
	orderedcopy    *common.BlkList_t            // list of ordered blocks
	logpresent     map[int]bool                 // enable quick check to see if block is in log
	orderedpresent map[int]bool                 // enable quick check so see if block is in ordered
	absorb         map[int]*common.Bdev_block_t // absorb writes to same block number
	waiters        int                          // number of processes wait for this trans to commit
	waitc          chan bool                    // channel on which waiters are waiting
}

func mk_trans(start, loglen, maxtrans int) *trans_t {
	t := &trans_t{start: start, head: loginc(start, loglen)} // reserve first block for descriptor
	t.ops = make(map[opid_t]bool)
	t.logged = common.MkBlkList()      // bounded by MaxOrdered
	t.ordered = common.MkBlkList()     // bounded by MaxOrdered
	t.orderedcopy = common.MkBlkList() // bounded by MaxOrdered
	t.logpresent = make(map[int]bool, loglen)
	t.orderedpresent = make(map[int]bool)
	t.absorb = make(map[int]*common.Bdev_block_t, MaxOrdered)
	t.waitc = make(chan bool)
	t.loglen = loglen
	t.maxtrans = maxtrans
	return t
}

func (trans *trans_t) add_op(opid opid_t) {
	trans.ops[opid] = true
}

func (trans *trans_t) mark_done(opid opid_t) {
	delete(trans.ops, opid)
}

func (trans *trans_t) add_write(log *log_t, buf buf_t) {
	if log_debug {
		fmt.Printf("trans.add_write: opid %d start %d head %d %d(%v)\n", buf.opid,
			trans.start, trans.head, buf.block.Block, buf.ordered)
	}

	// if block ordered, and now logged, then logged
	_, presentordered := trans.orderedpresent[buf.block.Block]
	if !buf.ordered && presentordered {
		trans.ordered.RemoveBlock(buf.block.Block)
		delete(trans.orderedpresent, buf.block.Block)
	}

	// if block is in logged, then it stays in log
	_, present := trans.logpresent[buf.block.Block]
	if buf.ordered && present {
		log.norder2logwrite++
		buf.ordered = false
	}

	if _, ok := trans.absorb[buf.block.Block]; ok {
		// Buffer is already in logged or in ordered, but not on
		// committed yet. We wrote it (since there is only one
		// common.Bdev_block_t for each blockno per uncommited trans),
		// so it has already been absorbed.
		//
		// If the write of this block is in a later op, we know this
		// later op will commit with the one that modified this block
		// earlier, because the op was admitted.
		log.nabsorption++
		log.fs.bcache.Relse(buf.block, "absorption")
		return
	}
	trans.absorb[buf.block.Block] = buf.block

	if buf.ordered {
		log.norderedwrite++
		trans.ordered.PushBack(buf.block)
		if trans.ordered.Len() >= MaxOrdered {
			panic("add_write")
		}
		trans.orderedpresent[buf.block.Block] = true
	} else {
		log.nlogwrite++
		trans.logged.PushBack(buf.block)
		trans.logpresent[buf.block.Block] = true
		trans.head = loginc(trans.head, log.loglen)
	}
}

func (trans *trans_t) iscommittable() bool {
	if log_debug {
		fmt.Printf("iscommittable: %d %v\n", len(trans.ops), trans.ops)
	}
	return len(trans.ops) == 0
}

func (trans *trans_t) isorderedfull() bool {
	n := trans.ordered.Len()
	n += MaxBlkPerOp
	return n >= MaxOrdered
}

func (trans *trans_t) isdescriptorfull() bool {
	n := logsize(trans.start, trans.head, trans.loglen)
	n += len(trans.ops) * MaxBlkPerOp
	n += MaxBlkPerOp
	return n >= trans.maxtrans
}

func (trans *trans_t) startcommit() bool {
	return trans.isdescriptorfull() || trans.isorderedfull()
}

func (trans *trans_t) unblock_waiters() {
	if log_debug {
		fmt.Printf("wakeup waiters for Force %v %v\n", trans.waiters, trans.waitc)
	}
	for i := 0; i < trans.waiters; i++ {
		trans.waitc <- true
	}
}

func (trans *trans_t) copyintolog(log *log_t) {
	if log_debug {
		fmt.Printf("copyintolog: transhead %d\n", trans.head)
	}
	i := loginc(trans.start, log.loglen) // skip descriptor
	trans.logged.Apply(func(b *common.Bdev_block_t) {
		// Make a "private" copy of b that isn't visible to FS or cache
		// XXX Use COW
		l := log.log[i]
		l.Block = b.Block
		copy(l.Data[:], b.Data[:])
		i = loginc(i, log.loglen)
		log.fs.bcache.Relse(b, "writelog")
	})
	if i != trans.head {
		panic("copyintolog")
	}
	trans.logged.Delete()
}

type copy_relse_t struct {
}

func (cp *copy_relse_t) Relse(blk *common.Bdev_block_t, s string) {
	blk.Free_page()
}

func (trans *trans_t) copyordered(log *log_t) {
	if log_debug {
		fmt.Printf("copyordered: %d\n", trans.ordered.Len())
	}
	trans.ordered.Apply(func(b *common.Bdev_block_t) {
		// Make a "private" copy of b that isn't visible to FS or cache
		// XXX Use COW
		cp := common.MkBlock_newpage(b.Block, "copyordered", log.fs.bcache.mem,
			log.fs.bcache.disk, &copy_relse_t{})
		copy(cp.Data[:], b.Data[:])
		trans.orderedcopy.PushBack(cp)
		log.fs.bcache.Relse(b, "writelog")
	})
	if trans.ordered.Len() != trans.orderedcopy.Len() {
		panic("copyordered")
	}
	trans.ordered.Delete()
}

// Update the ordered blocks in place
func (trans *trans_t) write_ordered(log *log_t) {
	if log_debug {
		fmt.Printf("write_ordered: %d\n", trans.orderedcopy.Len())
	}
	log.nwriteordered++
	trans.orderedcopy.Apply(func(b *common.Bdev_block_t) {
		log.fs.bcache.Write_async_through(b)
	})
	trans.orderedcopy.Delete()
}

func (trans *trans_t) commit(tail int, log *log_t) {
	if log_debug {
		fmt.Printf("commit: start %d head %d\n", trans.start, trans.head)
	}
	blks1 := common.MkBlkList()
	blks2 := common.MkBlkList()
	blks := blks1

	db := log.mkdescriptor(log.log[trans.start])
	j := 0
	for i := trans.start; i != trans.head; i = loginc(i, log.loglen) {
		if loginc(i, log.loglen) == tail {
			panic("commit runs into log start")
		}
		// install log destination in the descriptor block
		l := log.log[i]
		db.w_logdest(j, l.Block)
		j++
		b := common.MkBlock(log.logstart+LogOffset+i, "commit", log.fs.bcache.mem,
			log.fs.bcache.disk, &_nop_relse)
		// it is safe to reuse the underlying physical page; this page
		// will not be freed, and no thread will update its content
		// until this transaction has been committed and applied.
		b.Data = l.Data
		b.Pa = l.Pa
		blks.PushBack(b)
		if loginc(i, log.loglen) == 0 {
			blks = blks2
		}
	}
	db.w_logdest(j, 0) // marker

	if log_debug {
		fmt.Printf("commit: descriptor block at %d:\n", trans.start)
		for k := 1; k < j; k++ {
			fmt.Printf("\tdescriptor %d: %d\n", k, db.r_logdest(k))
		}
	}

	// write blocks to log in batch; no need to release
	log.fs.bcache.Write_async_blks_through(blks1)
	if blks2.Len() > 0 {
		log.fs.bcache.Write_async_blks_through(blks2)
	}

	trans.write_ordered(log)

	log.flush() // flush outstanding writes  (if you kill this line, then Atomic test fails)

	log.commit_head(trans.head)

	log.log[trans.start].Block = 0 // now safe to mark descriptor block

	n := blks1.Len() + blks2.Len()
	log.nblkcommitted += n
	if n > log.maxblks_per_op {
		log.maxblks_per_op = n
	}
	log.ncommit++
	if log_debug {
		fmt.Printf("commit: committed %d blks\n", n)
	}
}

type log_t struct {
	loglen    int                    // log len
	maxtrans  int                    // max number of blocks in transaction
	logstart  int                    // disk block where log starts on disk
	applytail int32                  // shared index in log where apply() will start
	log       []*common.Bdev_block_t // in-memory log, MaxBlkPerOp per op
	incoming  chan buf_t
	admission chan opid_t
	done      chan opid_t
	force     chan bool
	forcewait chan (chan bool)
	stop      chan bool

	commitc chan *trans_t
	applyc  chan int

	disk common.Disk_i
	fs   *Fs_t

	// some stats
	maxblks_per_op  int
	nblkcommitted   int
	ncommit         int
	napply          int
	nabsorption     int
	nlogwrite       int
	norderedwrite   int
	nblkapply       int
	nabsorbapply    int
	nforce          int
	nwriteordered   int
	nccommit        int
	norder2logwrite int
}

// first log header block format
// bytes, meaning
// 0-7, start log
// 8-15, end log
// 16-4095, table with destination
const (
	START = 0
	END   = 8
)

type logheader_t struct {
	data *common.Bytepg_t
}

func (lh *logheader_t) r_tail() int {
	return fieldr(lh.data, START)
}

func (lh *logheader_t) w_tail(n int) {
	fieldw(lh.data, START, n)
}

func (lh *logheader_t) r_head() int {
	return fieldr(lh.data, END)
}

func (lh *logheader_t) w_head(n int) {
	fieldw(lh.data, END, n)
}

type logdescriptor_t struct {
	data *common.Bytepg_t
	max  int
}

func (ld *logdescriptor_t) r_logdest(p int) int {
	if p < 0 || p >= ld.max {
		fmt.Printf("max %d\n", ld.max)
		panic("bad dnum")
	}
	return fieldr(ld.data, p)
}

func (ld *logdescriptor_t) w_logdest(p int, n int) {
	if p < 0 || p >= ld.max {
		fmt.Printf("w max %d\n", ld.max)
		panic("bad dnum")
	}
	fieldw(ld.data, p, n)
}

func (log *log_t) mk_log(ls, ll int, disk common.Disk_i) {
	log.loglen = ll - LogOffset // first block of the log is commit block
	log.maxtrans = common.Min(ll/2, MaxDescriptor)
	fmt.Printf("ll %d maxtrans %d\n", ll, log.maxtrans)
	if log.maxtrans > MaxDescriptor {
		panic("max trans too large")
	}
	log.logstart = ls
	log.log = make([]*common.Bdev_block_t, log.loglen)
	for i := 0; i < len(log.log); i++ {
		log.log[i] = common.MkBlock_newpage(0, "log", log.fs.bcache.mem,
			log.fs.bcache.disk, &_nop_relse)
	}
	log.incoming = make(chan buf_t)
	log.admission = make(chan opid_t)
	log.done = make(chan opid_t)
	log.force = make(chan bool)
	log.forcewait = make(chan (chan bool))
	log.commitc = make(chan *trans_t)
	log.applyc = make(chan int)
	log.stop = make(chan bool)
	log.disk = disk
}

func (log *log_t) readhdr() (*logheader_t, *common.Bdev_block_t) {
	headblk, err := log.fs.bcache.Get_fill(log.logstart, "readhdr", true)
	if err != 0 {
		panic("cannot read head/commit block\n")
	}
	return &logheader_t{headblk.Data}, headblk
}

func (log *log_t) mkdescriptor(blk *common.Bdev_block_t) *logdescriptor_t {
	return &logdescriptor_t{blk.Data, log.maxtrans}
}

func (log *log_t) readdescriptor(logblk int) (*logdescriptor_t, *common.Bdev_block_t) {
	dblk, err := log.fs.bcache.Get_fill(log.logstart+logblk+LogOffset, "readdescriptor", false)
	if err != 0 {
		panic("cannot read descriptor block\n")
	}
	return log.mkdescriptor(dblk), dblk
}

func (log *log_t) flush() {
	ider := common.MkRequest(nil, common.BDEV_FLUSH, true)
	if log.disk.Start(ider) {
		<-ider.AckCh
	}
}

func (log *log_t) space(tail, head int) bool {
	n := logsize(head, tail, log.loglen)
	space := n >= log.maxtrans
	return space
}

func (log *log_t) almosthalffull(tail, head int) bool {
	n := logsize(tail, head, log.loglen)
	n += MaxBlkPerOp // a transaction is likely not to fill one op of blocks
	full := n >= log.loglen/2
	return full
}

func (log *log_t) commit_head(head int) {
	lh, headblk := log.readhdr()
	lh.w_head(head)
	headblk.Unlock()
	log.fs.bcache.Write(headblk) // write log header
	log.flush()                  // commit log header
	log.fs.bcache.Relse(headblk, "commit_done")
}

func (log *log_t) commit_tail(tail int) {
	lh, headblk := log.readhdr()
	lh.w_tail(tail)
	headblk.Unlock()
	log.fs.bcache.Write(headblk) // write log header
	log.flush()                  // commit log header
	log.fs.bcache.Relse(headblk, "commit_tail")
}

func (log *log_t) apply(tail, head int) int {
	log.napply++

	done := make(map[int]bool, log.loglen)

	// The log is committed. If we crash while installing the blocks to
	// their destinations, we should be able to recover.  Install backwards,
	// writing the last version of a block (and not earlier versions).

	if log_debug {
		fmt.Printf("apply log: blks from %d till %d\n", tail, head)
	}

	if tail == head {
		return head
	}

	i := logdec(head, log.loglen)
	for {
		l := log.log[i]
		if l.Block == 0 {
			// nothing to do for descriptor block
		} else {
			log.nblkapply++
			if _, ok := done[l.Block]; !ok {
				log.fs.bcache.Write_async_through(l)
				done[l.Block] = true
			} else {
				log.nabsorbapply++
			}
		}
		if i == tail {
			break
		}
		i = logdec(i, log.loglen)
	}
	log.flush() // flush apply

	log.commit_tail(head)

	if log_debug {
		fmt.Printf("apply log: updated tail %d\n", tail)
	}

	return head
}

func (l *log_t) apply_daemon(t int) {
	tail := t
	for {
		select {
		case head := <-l.applyc:
			if head < 0 { // stop apply daemon
				l.applyc <- 0
				return
			}
			tail = l.apply(tail, head)
			atomic.StoreInt32(&l.applytail, int32(tail))
			// l.applyc <- true
		}
	}
}

func (l *log_t) commit_daemon(h int) {
	tail := h
	head := h
	for {
		select {
		case t := <-l.commitc:
			if t == nil { // stop commit daemon?
				l.commitc <- nil
				return
			}
			if log_debug {
				fmt.Printf("commit_daemon: start waiters %d tail %d head %d\n",
					t.waiters, tail, head)
			}

			t.copyintolog(l)
			t.copyordered(l)

			head = t.head
			tail = int(atomic.LoadInt32(&l.applytail))

			start := true
			if l.space(head, tail) { // space for another transaction?
				if log_debug {
					fmt.Printf("commit_daemon: start next trans early\n")
				}
				l.nccommit++
				l.commitc <- nil
			} else {
				start = false
			}

			t.commit(tail, l)

			if l.almosthalffull(tail, head) {
				if log_debug {
					fmt.Printf("commit_daemon: kick apply\n")
				}
				l.applyc <- head
				// <-l.applyc // XXX serialize for now
			}

			if !start {
				if log_debug {
					fmt.Printf("commit_daemon: start next trans\n")
				}
				l.commitc <- nil
			}

			t.unblock_waiters()
			if log_debug {
				fmt.Printf("commit_daemon: done tail %v head %v\n", tail, head)
			}
		}
	}
}

func (l *log_t) log_daemon(h int) {
	nextop := opid_t(0)
	head := h
	for {
		common.Kunresdebug()
		common.Kresdebug(100<<20, "log daemon")

		t := mk_trans(head, l.loglen, l.maxtrans)

		done := false
		adm := l.admission
		for !done {
			select {
			case nb := <-l.incoming:
				t.add_write(l, nb)
			case opid := <-l.done:
				t.mark_done(opid)
				if adm == nil { // admit ops again?
					if t.waiters > 0 || t.startcommit() {
						// No more log space for another op or forced to commit
						if t.iscommittable() {
							done = true
						}
					} else {
						if log_debug {
							fmt.Printf("log_daemon: can admit op %d\n", nextop)
						}
						// admit another op. this may op
						// did not use all the space
						// that it reserved.
						adm = l.admission
					}
				}
			case adm <- nextop:
				if log_debug {
					fmt.Printf("log_daemon: admit %d\n", nextop)
				}
				t.add_op(nextop)
				nextop++
				if t.startcommit() {
					adm = nil
				}
			case <-l.force:
				if loginc(t.start, l.loglen) == t.head { // nothing to commit?
					l.forcewait <- t.waitc
					t.waitc <- true

				} else {
					l.nforce++
					t.waiters++
					if log_debug {
						fmt.Printf("log_daemon: force %d\n", t.waiters)
					}
					l.forcewait <- t.waitc
					adm = nil
					if t.iscommittable() {
						done = true
					}
				}
			case <-l.stop:
				l.stop <- true
				return
			}
		}

		head = t.head

		l.commitc <- t
		// wait until commit thread tells us to go ahead with
		// next trans
		<-l.commitc
	}
}

func (log *log_t) install(tail, head int) {
	for i := tail; i != head; {
		db, dblk := log.readdescriptor(i)
		i = loginc(i, log.loglen)
		for j := 1; ; j++ {
			bdest := db.r_logdest(j)
			if bdest == 0 {
				if log_debug {
					fmt.Printf("install: end descriptor block at i %d j %d\n", i, j)
				}
				break
			}
			if log_debug {
				fmt.Printf("install: write log %d to %d\n", i, bdest)
			}
			lb, err := log.fs.bcache.Get_fill(log.logstart+LogOffset+i, "i", false)
			if err != 0 {
				panic("must succeed")
			}
			fb, err := log.fs.bcache.Get_fill(bdest, "bdest", false)
			if err != 0 {
				panic("must succeed")
			}
			copy(fb.Data[:], lb.Data[:])
			log.fs.bcache.Write(fb)
			log.fs.bcache.Relse(lb, "install lb")
			log.fs.bcache.Relse(fb, "install fb")
			i = loginc(i, log.loglen)
		}
		log.fs.bcache.Relse(dblk, "install db")
	}
}

func (log *log_t) recover() int {
	lh, headblk := log.readhdr()
	tail := lh.r_tail()
	head := lh.r_head()
	headblk.Unlock()

	log.fs.bcache.Relse(headblk, "recover")
	if tail == head {
		fmt.Printf("no FS recovery needed\n")
		return head
	}
	fmt.Printf("starting FS recovery start %d end %d\n", tail, head)
	log.install(tail, head)
	log.commit_tail(head)

	fmt.Printf("restored blocks from %d till %d\n", tail, head)
	return head
}

// If cache has no space, ask logdaemon to create some space
func (log *log_t) read(readfn func() (*common.Bdev_block_t, common.Err_t)) (*common.Bdev_block_t, common.Err_t) {
	b, err := readfn()
	if err == -common.ENOMEM {
		panic("still no mem")
	}
	return b, err
}

func mkread(readfn func(int, string, bool) (*common.Bdev_block_t, common.Err_t), b int, s string, l bool) func() (*common.Bdev_block_t, common.Err_t) {
	return func() (*common.Bdev_block_t, common.Err_t) {
		return readfn(b, s, l)
	}
}
