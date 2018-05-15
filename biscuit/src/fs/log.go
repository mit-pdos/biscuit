package fs

import "fmt"
import "strconv"

import "common"

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

var loglen = 0 // for marshalling/unmarshalling

const LogOffset = 1 // log block 0 is used for head

// an upperbound on the number of blocks written per system call. this is
// necessary in order to guarantee that the log is long enough for the allowed
// number of concurrent fs syscalls.
const MaxBlkPerOp = 10
const MaxOrdered = 5000

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
	<-log.commitwait
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
	s += "\n\tnforceordered "
	s += strconv.Itoa(log.nforceordered)
	s += "\n"
	return s
}

func StartLog(logstart, loglen int, fs *Fs_t, disk common.Disk_i) *log_t {
	fslog := &log_t{}
	fslog.fs = fs
	fslog.mk_log(logstart, loglen, disk)
	err := fslog.recover()
	if err != 0 {
		return nil
	}
	if !memfs {
		go fslog.log_daemon()
		go fslog.log_commit()
	}
	return fslog
}

func (log *log_t) StopLog() {
	log.stop <- true
	<-log.stop
	log.commitc <- -1
	<-log.commitc
}

//
// Log implementation
//

type buf_t struct {
	opid    opid_t
	block   *common.Bdev_block_t
	ordered bool
}

type trans_t struct {
	logindex       int                          // index where this trans starts in on-disk log
	head           int                          // index into in-memory log
	ops            map[opid_t]bool              // ops in progress this transaction
	logged         *common.BlkList_t            // list of to-be-logged blocks
	ordered        *common.BlkList_t            // list of ordered blocks
	orderedcopy    *common.BlkList_t            // list of ordered blocks
	logpresent     map[int]bool                 // enable quick check to see if block is in log
	orderedpresent map[int]bool                 // enable quick check so see if block is in ordered
	absorb         map[int]*common.Bdev_block_t // absorb writes to same block number
}

func mk_trans(logindex, loglen int) *trans_t {
	t := &trans_t{logindex: logindex, head: logindex}
	t.ops = make(map[opid_t]bool)
	t.logged = common.MkBlkList()      // bounded by MaxOrdered
	t.ordered = common.MkBlkList()     // bounded by MaxOrdered
	t.orderedcopy = common.MkBlkList() // bounded by MaxOrdered
	t.logpresent = make(map[int]bool, loglen)
	t.orderedpresent = make(map[int]bool)
	t.absorb = make(map[int]*common.Bdev_block_t, MaxOrdered)
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
		fmt.Printf("trans.add_write: opid %d index %d head %d %d(%v)\n", buf.opid,
			trans.logindex, trans.head, buf.block.Block, buf.ordered)
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
		trans.head++
	}
}

func (trans *trans_t) iscommittable() bool {
	if log_debug {
		fmt.Printf("iscommittable: %d %v\n", len(trans.ops), trans.ops)
	}
	return len(trans.ops) == 0
}

func (trans *trans_t) isfull(loglen int, onemore bool) bool {
	n := trans.logindex + trans.head + len(trans.ops)*MaxBlkPerOp
	if onemore {
		n += MaxBlkPerOp
	}
	full := n >= loglen
	if log_debug {
		fmt.Printf("trans.isfull(%v): %v nops %d head %d index %d len %d\n", onemore,
			full, len(trans.ops), trans.head, trans.logindex, loglen)
	}
	return full
}

func (trans *trans_t) copyintolog(log *log_t) {
	if log_debug {
		fmt.Printf("copyintolog: transhead %d\n", trans.head)
	}
	i := trans.logindex
	trans.logged.Apply(func(b *common.Bdev_block_t) {
		// Make a "private" copy of b that isn't visible to FS or cache
		// XXX Use COW
		l := log.log[i]
		l.Block = b.Block
		copy(l.Data[:], b.Data[:])
		i++
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
	log.nforceordered++
	trans.orderedcopy.Apply(func(b *common.Bdev_block_t) {
		log.fs.bcache.Write_async_through(b)
	})
	trans.orderedcopy.Delete()
}

func (trans *trans_t) commit(log *log_t) {
	if log_debug {
		fmt.Printf("commit: trans logindex %d transhead %d\n", trans.logindex, trans.head)
	}
	// read the log header from disk; it may contain commit blocks from
	// current transactions, if we haven't applied yet.
	lh, headblk := log.readhead()
	blks := common.MkBlkList()

	for i := trans.logindex; i < trans.head; i++ {
		// install log destination in the header block
		l := log.log[i]
		lh.w_logdest(i-LogOffset, l.Block)
		b := common.MkBlock(log.logstart+i, "commit", log.fs.bcache.mem, log.fs.bcache.disk, &_nop_relse)
		// it is safe to reuse the underlying physical page; this page
		// will not be freed, and no thread will update its content
		// until this transaction has been committed and applied.
		b.Data = l.Data
		b.Pa = l.Pa
		blks.PushBack(b)
	}

	lh.w_recovernum(trans.head - LogOffset)

	// write blocks to log in batch; no need to release
	log.fs.bcache.Write_async_blks_through(blks)

	trans.write_ordered(log)

	log.flush() // flush outstanding writes  (if you kill this line, then Atomic test fails)

	log.fs.bcache.Write(headblk) // write log header

	log.flush() // commit log header

	log.nblkcommitted += blks.Len()
	if blks.Len() > log.maxblks_per_op {
		log.maxblks_per_op = blks.Len()
	}
	log.ncommit++
	if log_debug {
		fmt.Printf("commit: committed %d blks\n", blks.Len())
	}

	log.fs.bcache.Relse(headblk, "commit done")
}

type log_t struct {
	transactions []*trans_t
	logstart     int // disk block where log starts on disk
	loglen       int
	log          []*common.Bdev_block_t // in-memory log, MaxBlkPerOp per op
	incoming     chan buf_t
	admission    chan opid_t
	done         chan opid_t
	force        chan bool
	commitwait   chan bool
	stop         chan bool

	commitc chan int

	disk common.Disk_i
	fs   *Fs_t

	// some stats
	maxblks_per_op int
	nblkcommitted  int
	ncommit        int
	napply         int
	nabsorption    int
	nlogwrite      int
	norderedwrite  int
	nblkapply      int
	nabsorbapply   int
	nforceordered  int
}

// first log header block format
// bytes, meaning
// 0-7,   valid log blocks
// 8-511, log destination (63)
type logheader_t struct {
	data *common.Bytepg_t
}

func (lh *logheader_t) recovernum() int {
	return fieldr(lh.data, 0)
}

func (lh *logheader_t) w_recovernum(n int) {
	fieldw(lh.data, 0, n)
}

func (lh *logheader_t) logdest(p int) int {
	if p < 0 || p > loglen {
		panic("bad dnum")
	}
	return fieldr(lh.data, 8+p)
}

func (lh *logheader_t) w_logdest(p int, n int) {
	if p < 0 || p > loglen {
		panic("bad dnum")
	}
	fieldw(lh.data, 8+p, n)
}

func (log *log_t) mk_log(ls int, ll int, disk common.Disk_i) {
	// loglen is global
	loglen = ll - LogOffset // leave space for head and orphan inodes
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = loglen
	log.transactions = make([]*trans_t, 0)
	log.log = make([]*common.Bdev_block_t, loglen)
	for i := 0; i < len(log.log); i++ {
		log.log[i] = common.MkBlock_newpage(0, "log", log.fs.bcache.mem,
			log.fs.bcache.disk, &_nop_relse)
	}
	log.incoming = make(chan buf_t)
	log.admission = make(chan opid_t)
	log.done = make(chan opid_t)
	log.force = make(chan bool)
	log.commitwait = make(chan bool)
	log.commitc = make(chan int)
	log.stop = make(chan bool)
	log.disk = disk

	if log.loglen >= common.BSIZE/4 {
		panic("log_t.init: log will not fill in one header block\n")
	}
}

func (log *log_t) last_trans() *trans_t {
	if len(log.transactions) <= 0 {
		panic("no transaction")
	}
	return log.transactions[len(log.transactions)-1]
}

func (log *log_t) add_op(opid opid_t) bool {
	t := log.last_trans()
	t.add_op(opid)
	return log.istoofull()
}

func (log *log_t) add_write(buf buf_t) {
	t := log.last_trans()
	t.add_write(log, buf)
}

func (log *log_t) mark_done(opid opid_t) {
	t := log.last_trans()
	t.mark_done(opid)
}

func (log *log_t) iscommittable() bool {
	if len(log.transactions) == 0 {
		return true
	}
	t := log.last_trans()
	return t.iscommittable()
}

func (log *log_t) isorderedfull(onemore bool) bool {
	n := 0
	for _, t := range log.transactions {
		n += t.ordered.Len()
	}
	if onemore {
		n += MaxBlkPerOp
	}
	return n >= MaxOrdered
}

func (log *log_t) isfull() bool {
	t := log.last_trans()
	return t.isfull(log.loglen, false) || log.isorderedfull(false)
}

func (log *log_t) istoofull() bool {
	t := log.last_trans()
	return t.isfull(log.loglen, true) || log.isorderedfull(true)
}

func (log *log_t) readhead() (*logheader_t, *common.Bdev_block_t) {
	headblk, err := log.fs.bcache.Get_fill(log.logstart, "commit", false)
	if err != 0 {
		panic("cannot read commit block\n")
	}
	return &logheader_t{headblk.Data}, headblk
}

func (log *log_t) apply() {
	log.napply++

	done := make(map[int]bool, log.loglen)

	if log_debug {
		fmt.Printf("apply log: #trans %v\n", len(log.transactions))
	}

	// The log is committed. If we crash while installing the blocks to
	// their destinations, we should be able to recover.  Install backwards,
	// writing the last version of a block (and not earlier versions).
	trans := log.transactions[len(log.transactions)-1]
	for i := trans.head - 1; i >= LogOffset; i-- {
		l := log.log[i]
		log.nblkapply++
		if _, ok := done[l.Block]; !ok {
			log.fs.bcache.Write_async_through(l)
			done[l.Block] = true
		} else {
			log.nabsorbapply++
		}

	}
	log.flush() // flush apply

	// success; clear flag indicating to recover from log
	lh, headblk := log.readhead()
	lh.w_recovernum(0)
	log.fs.bcache.Write(headblk)

	log.flush() // flush cleared commit

	log.fs.bcache.Relse(headblk, "commit done")

	log.transactions = make([]*trans_t, 0)
}

func (log *log_t) unblock_waiters(n int) {
	if n > 0 {
		if log_debug {
			fmt.Printf("wakeup waiters/syncers %v\n", n)
		}
		for i := 0; i < n; i++ {
			log.commitwait <- true
		}
	}
}

func (log *log_t) flush() {
	ider := common.MkRequest(nil, common.BDEV_FLUSH, true)
	if log.disk.Start(ider) {
		<-ider.AckCh
	}
}

func (log *log_t) recover() common.Err_t {
	b, err := log.fs.bcache.Get_fill(log.logstart, "fs_recover_logstart", false)
	if err != 0 {
		panic("must succeed")
	}
	lh := logheader_t{b.Data}
	rlen := lh.recovernum()
	if rlen == 0 {
		fmt.Printf("no FS recovery needed\n")
		log.fs.bcache.Relse(b, "fs_recover_logstart")
		return 0
	}
	fmt.Printf("starting FS recovery...\n")

	for i := 0; i < rlen; i++ {
		bdest := lh.logdest(i)
		if log_debug {
			fmt.Printf("write log %d to %d\n", log.logstart+LogOffset+i, bdest)
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
		log.fs.bcache.Relse(lb, "fs_recover1")
		log.fs.bcache.Relse(fb, "fs_recover2")
	}

	// clear recovery flag
	lh.w_recovernum(0)
	log.fs.bcache.Write(b)
	log.fs.bcache.Relse(b, "fs_recover_logstart")
	fmt.Printf("restored %v blocks\n", rlen)
	return 0
}

func (l *log_t) do_commit(waiters int) {
	t := l.last_trans()
	t.copyintolog(l)
	t.copyordered(l)
	t.commit(l)
	// apply log only when there is no room for another op. this avoids
	// applies when there room in the log (e.g., a sync forced the log)
	if l.istoofull() {
		l.apply()
	}
	l.unblock_waiters(waiters)
}

func (l *log_t) log_commit() {
	for {
		select {
		case waiters := <-l.commitc:
			if waiters < 0 {
				l.commitc <- -1
				return
			}
			t := l.last_trans()
			t.copyintolog(l)
			t.copyordered(l)
			if waiters > 0 && !l.istoofull() { // forced commit?
				l.commitc <- 0 // start next trans
				t.commit(l)
			} else {
				t.commit(l)
				// apply log only when there is no room for another op. this avoids
				// applies when there room in the log (e.g., a sync forced the log)
				if l.istoofull() {
					l.apply()
				}

				l.commitc <- 0
			}
			l.unblock_waiters(waiters)
		}
	}
}

func (l *log_t) log_daemon() {
	nextop := opid_t(0)
	for {
		common.Kunresdebug()
		common.Kresdebug(100<<20, "log daemon")

		// start a new transactions, start in the log where
		// the last one left off
		start := LogOffset
		if len(l.transactions) > 0 {
			t := l.last_trans()
			start = t.head
		}
		t := mk_trans(start, l.loglen)
		l.transactions = append(l.transactions, t)

		done := false
		waiters := 0
		adm := l.admission
		for !done {
			select {
			case nb := <-l.incoming:
				l.add_write(nb)
			case opid := <-l.done:
				l.mark_done(opid)
				if adm == nil { // admit ops again?
					if waiters > 0 || l.istoofull() {
						// No more log space for another op or forced to commit
						if l.iscommittable() {
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
				full := l.add_op(nextop)
				nextop++
				if full {
					adm = nil
				}
			case <-l.force:
				waiters++
				adm = nil
				if l.iscommittable() {
					done = true
				}
			case <-l.stop:
				l.stop <- true
				return
			}
		}

		if len(l.transactions) > 0 {
			// l.do_commit(waiters)
			l.commitc <- waiters
			// wait until commit thread tells us to go ahead with
			// next trans
			<-l.commitc
		}
	}
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
