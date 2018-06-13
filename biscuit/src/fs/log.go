package fs

import "fmt"
import "runtime"
import "sync"

import "common"

const log_debug = false

// File system journal.  The file system brackets FS calls (e.g.,create) with
// Op_begin and Op_end(); the log makes sure that these operations happen
// atomically with respect to crashes.  Operations are grouped in
// transactions. A transaction is committed to the on-disk log on force() or when
// the descriptor block of transaction is full.  Transactions in the log are
// applied to the file system when the on-disk log is close to full.  All writes go
// through the log, but ordered writes are not appended to the on-disk log, but
// overwrite their home location.  The file system should use logged writes for
// all its data structures, and use ordered writes only for file data.  The file
// system must guarantee that it performs no more than maxblkspersys logged
// writes in an operation, to ensure that its operation will fit in the log.

const LogOffset = 1 // log block 0 is used for head

// an upperbound on the number of blocks written per system call. this is
// necessary in order to guarantee that the log is long enough for the allowed
// number of concurrent fs syscalls.
const MaxBlkPerOp = 10
const MaxDescriptor = common.BSIZE / 8
const MaxOrdered = 3000
const EndDescriptor = 0
const NCommitBlk = 1
const Canceled = -2

type opid_t int
type index_t uint64

//
// The public interface to the logging layer
//

func (log *log_t) Op_begin(s string) opid_t {
	if memfs {
		return 0
	}

	log.Lock()
	defer log.Unlock()

	ts := runtime.Rdtsc()

	opid := log.nextop
	log.nextop += 1
	log.stats.Nop += 1

	t := log.curtrans

	for t.isfull() {
		if log_debug {
			fmt.Printf("op_begin: %d wait %s\n", opid, s)
		}
		log.admissioncond.Wait()
		t = log.curtrans // maybe a different trans
	}

	t.add_op(opid)

	log.stats.Opbegincycles.Add(runtime.Rdtsc() - ts)

	if log_debug {
		fmt.Printf("op_begin: go %d %v\n", opid, s)
	}
	return opid
}

func (log *log_t) Op_end(opid opid_t) {
	if memfs {
		return
	}
	log.Lock()
	defer log.Unlock()

	if log_debug {
		fmt.Printf("op_end: done %d\n", opid)
	}
	s := runtime.Rdtsc()

	t := log.curtrans

	t.mark_done(opid)

	committing := false

	if (t.isfull() || t.force) && t.iscommittable() { // are we the last op of this trans?
		committing = true
		if log_debug {
			fmt.Printf("Op_end: wakeup committer start %d\n", t.start)
		}
		log.commitcond.Signal()
	}

	if !committing {
		// maybe this trans didn't use all its reserved space
		log.admissioncond.Broadcast()
	}

	log.stats.Opendcycles.Add(runtime.Rdtsc() - s)
}

// Ensure any fs ops in the journal preceding this sync call are flushed to disk
// by waiting for log commit.
func (log *log_t) Force(doapply bool) {
	if memfs {
		panic("memfs")
	}

	log.Lock()
	defer log.Unlock()

	t := log.curtrans

	s := runtime.Rdtsc()

	log.stats.Nforce++

	if t.isempty() || t.forcedone {
		log.stats.Nbatchforce++
		return
	}

	if t.force {
		log.stats.Nbatchforce++
	} else {
		t.force = true
	}
	if doapply {
		t.forceapply = true
	}

	if t.iscommittable() { // no outstanding ops?
		if log_debug {
			fmt.Printf("Force: wakeup committer start %d\n", t.start)
		}
		log.commitcond.Signal()
	}

	if log_debug {
		fmt.Printf("Force: wait for commit trans %d\n", t.start)
	}

	for !t.forcedone {
		t.forcecond.Wait()
	}

	log.stats.Forcecycles.Add(runtime.Rdtsc() - s)

	if log_debug {
		fmt.Printf("Force: done trans %d\n", t.start)
	}
}

// Write increments ref so that the log has always a valid ref to the buf's
// page.  The logging layer refdowns when it it is done with the page.  The
// caller of log_write shouldn't hold buf's lock.
func (log *log_t) Write(opid opid_t, b *common.Bdev_block_t) {
	if memfs {
		return
	}
	log.write(opid, b, false)
}

func (log *log_t) Write_ordered(opid opid_t, b *common.Bdev_block_t) {
	if memfs {
		return
	}
	log.write(opid, b, true)
}

func (log *log_t) Loglen() int {
	return log.ml.loglen
}

// All layers above log read blocks through the log layer, which are mostly
// wrappers for the the corresponding cache operations.
func (log *log_t) Get_fill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	t := runtime.Rdtsc()
	r, e := log.ml.bcache.Get_fill(blkn, s, lock)
	log.stats.Readcycles.Add(runtime.Rdtsc() - t)
	return r, e
}

func (log *log_t) Get_zero(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.ml.bcache.Get_zero(blkn, s, lock)
}

func (log *log_t) Get_nofill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.ml.bcache.Get_nofill(blkn, s, lock)
}

func (log *log_t) Relse(blk *common.Bdev_block_t, s string) {
	log.ml.bcache.Relse(blk, s)
}

func (log *log_t) Stats() string {
	s1 := "log: " + common.Stats2String(log.stats)
	s2 := "mlog: " + common.Stats2String(log.ml.stats)
	log.stats = logstat_t{}
	log.ml.stats = memlogstat_t{}
	return s1 + s2
}

func StartLog(logstart, loglen int, bcache *bcache_t) *log_t {
	log := &log_t{}
	log.mk_log(logstart, loglen, bcache)
	log.recover()
	log.curtrans = log.mk_trans(log.head, log.ml)
	go log.committer()
	return log
}

func (log *log_t) StopLog() {

	log.Force(true)

	log.Lock()

	log.stop = true
	log.commitcond.Signal()
	if log_debug {
		fmt.Printf("Wait for logging system to stop\n")
	}
	log.Unlock()

	<-log.stopc
	if log_debug {
		fmt.Printf("Logging system stopped\n")
	}
}

//
// Log implementation
//

type memlogstat_t struct {
	Ncommit          common.Counter_t
	Nccommit         common.Counter_t
	Ncommithead      common.Counter_t
	Headcycles       common.Cycles_t
	Flushdatacycles  common.Cycles_t
	Commitcopycycles common.Cycles_t
	Ncommitter       common.Counter_t
	Committercycles  common.Cycles_t

	Ncommittail          common.Counter_t
	Tailcycles           common.Cycles_t
	Flushapplydatacycles common.Cycles_t

	Nblkcommitted     common.Counter_t
	Maxblks_per_trans common.Counter_t
	Nwriteordered     common.Counter_t
	Nrevokeblk        common.Counter_t

	Napply       common.Counter_t
	Nblkapply    common.Counter_t
	Nabsorbapply common.Counter_t
}

type memlog_t struct {
	log      []*common.Bdev_block_t // in-memory log, MaxBlkPerOp per op
	loglen   int                    // length of log (should be >= 2 * maxtrans)
	maxtrans int                    // max number of blocks in transaction
	logstart int                    // position of memlog on disk
	bcache   *bcache_t              // the backing store for memlog
	stats    memlogstat_t
}

func mk_memlog(ls, ll int, bcache *bcache_t) *memlog_t {
	ml := &memlog_t{}
	ml.loglen = ll - LogOffset // first block of the log is commit block
	ml.maxtrans = common.Min(ll/2, MaxDescriptor)
	fmt.Printf("ll %d maxtrans %d\n", ll, ml.maxtrans)
	if ml.maxtrans > MaxDescriptor {
		panic("max trans too large")
	}
	ml.logstart = ls
	ml.bcache = bcache
	ml.log = make([]*common.Bdev_block_t, ml.loglen)
	for i := 0; i < len(ml.log); i++ {
		ml.log[i] = common.MkBlock_newpage(0, "log", ml.bcache.mem,
			ml.bcache.disk, &_nop_relse)
	}
	return ml
}

func (ml *memlog_t) logindex(i index_t) int {
	return int(i) % ml.loglen
}

func (ml *memlog_t) loginc(i index_t) int {
	return int(i+1) % ml.loglen
}

func (ml *memlog_t) diskindex(i index_t) int {
	li := ml.logindex(i)
	return ml.logstart + LogOffset + li
}

func (ml *memlog_t) getmemlog(i index_t) *common.Bdev_block_t {
	n := ml.logindex(i)
	return ml.log[n]
}

func (ml *memlog_t) readhdr() (*logheader_t, *common.Bdev_block_t) {
	headblk, err := ml.bcache.Get_fill(ml.logstart, "readhdr", true)
	if err != 0 {
		panic("cannot read head/commit block\n")
	}
	return &logheader_t{headblk.Data}, headblk
}

func (ml *memlog_t) mkdescriptor(blk *common.Bdev_block_t) *logdescriptor_t {
	return &logdescriptor_t{blk.Data, ml.maxtrans}
}

func (ml *memlog_t) getdescriptor(i index_t) *logdescriptor_t {
	b := ml.getmemlog(i)
	return ml.mkdescriptor(b)
}

func (ml *memlog_t) readdescriptor(i index_t) (*logdescriptor_t, *common.Bdev_block_t) {
	dblk, err := ml.bcache.Get_fill(ml.diskindex(i), "readdescriptor", false)
	if err != 0 {
		panic("cannot read descriptor block\n")
	}
	return ml.mkdescriptor(dblk), dblk
}

func (ml *memlog_t) flush() {
	ider := common.MkRequest(nil, common.BDEV_FLUSH, true)
	if ml.bcache.disk.Start(ider) {
		<-ider.AckCh
	}
}

func (ml *memlog_t) commit_head(head index_t) {
	ml.stats.Ncommithead++
	lh, headblk := ml.readhdr()
	lh.w_head(head)
	headblk.Unlock()
	ml.bcache.Write_async(headblk)
	s := runtime.Rdtsc()
	ml.flush() // commit log header
	ml.stats.Headcycles.Add(runtime.Rdtsc() - s)
	ml.bcache.Relse(headblk, "commit_done")
}

func (ml *memlog_t) commit_tail(tail index_t) {
	ml.stats.Ncommittail++
	lh, headblk := ml.readhdr()
	lh.w_tail(tail)
	headblk.Unlock()
	ml.bcache.Write_async(headblk)
	s := runtime.Rdtsc()
	ml.flush() // commit log header
	t := runtime.Rdtsc()
	ml.stats.Tailcycles.Add(t - s)
	ml.bcache.Relse(headblk, "commit_tail")
}

func (ml *memlog_t) freespace(head, tail index_t) bool {
	n := ml.loglen - int(head-tail)
	if n < 0 {
		panic("space")
	}
	space := n >= ml.maxtrans
	return space
}

func (ml *memlog_t) almosthalffull(tail, head index_t) bool {
	n := head - tail
	n += MaxBlkPerOp
	full := int(n) >= ml.loglen/2
	return full
}

type revokelist_t struct {
	revoked *common.BlkList_t
	index   int
}

func mkRevokeList() *revokelist_t {
	rl := &revokelist_t{}
	rl.revoked = common.MkBlkList()
	return rl
}

func (rl *revokelist_t) len() int {
	return rl.revoked.Len()
}

func (rl *revokelist_t) addRevokeRecord(blkno int, ml *memlog_t) {
	var db *logdescriptor_t
	blk := rl.revoked.Back()
	if blk == nil || rl.index >= MaxDescriptor {
		blk = common.MkBlock_newpage(blkno, "addRevokeRecord", ml.bcache.mem,
			ml.bcache.disk, &copy_relse_t{})
		blk.Type = common.RevokeBlk
		rl.revoked.PushBack(blk)
		db = ml.mkdescriptor(blk)
		db.w_logdest(0, int(common.RevokeBlk))
		rl.index = 1
		ml.stats.Nrevokeblk.Inc()
	} else {
		db = ml.mkdescriptor(blk)
	}
	if log_debug {
		fmt.Printf("add revoke record: %d\n", blkno)
	}
	db.w_logdest(rl.index, blkno)
	db.w_logdest(rl.index+1, EndDescriptor)
	rl.index++
}

type trans_t struct {
	forcecond      *sync.Cond
	ml             *memlog_t
	start          index_t
	head           index_t
	inprogress     int               // ops in progress this transaction
	logged         *common.BlkList_t // list of to-be-logged blocks
	ordered        *common.BlkList_t // list of ordered blocks
	orderedcopy    *common.BlkList_t // list of copied ordered blocks
	revokel        *revokelist_t
	logpresent     map[int]bool // enable quick check to see if block is in log
	orderedpresent map[int]bool // enable quick check so see if block is in ordered
	force          bool
	forceapply     bool
	forcedone      bool
}

func (log *log_t) mk_trans(start index_t, ml *memlog_t) *trans_t {
	t := &trans_t{start: start, head: start + NCommitBlk}
	t.ml = ml
	t.forcecond = sync.NewCond(log)
	t.logged = common.MkBlkList()      // bounded by MaxDescriptor
	t.ordered = common.MkBlkList()     // bounded by MaxOrdered
	t.orderedcopy = common.MkBlkList() // bounded by MaxOrdered
	t.logpresent = make(map[int]bool, ml.loglen)
	t.orderedpresent = make(map[int]bool, MaxOrdered)
	t.revokel = mkRevokeList()
	return t
}

func (trans *trans_t) add_op(opid opid_t) {
	trans.inprogress += 1
}

func (trans *trans_t) mark_done(opid opid_t) {
	if trans.inprogress == 0 {
		panic("mark done")
	}
	trans.inprogress -= 1
}

func (trans *trans_t) add_write(opid opid_t, log *log_t, blk *common.Bdev_block_t, ordered bool) {
	if log_debug {
		fmt.Printf("add_write: opid %d start %d #logged %d #ordered %d b %d(%v)\n", opid,
			trans.start, trans.logged.Len(), trans.ordered.Len(), blk.Block, ordered)
	}

	// if block is in log and now ordered, remove from log
	_, lp := trans.logpresent[blk.Block]
	if ordered && lp {
		log.stats.Nlogwrite2order++
		trans.logged.RemoveBlock(blk.Block)
		delete(trans.logpresent, blk.Block)
	}

	// if block is in ordered list and now logged, remove from ordered
	_, op := trans.orderedpresent[blk.Block]
	if !ordered && op {
		log.stats.Norder2logwrite++
		trans.ordered.RemoveBlock(blk.Block)
		delete(trans.orderedpresent, blk.Block)
	}

	_, lp = trans.logpresent[blk.Block]
	_, op = trans.orderedpresent[blk.Block]

	if lp || op {
		// Buffer is already in logged or in ordered.  We wrote it
		// (since there is only one common.Bdev_block_t for each blockno
		// per uncommited trans), so it has already been absorbed.
		//
		// If the write of this block is in a later op, we know this
		// later op will commit with the one that modified this block
		// earlier, because the op was admitted.
		log.stats.Nabsorption++
		log.ml.bcache.Relse(blk, "absorption")
		return
	}

	// if ordered but logged in committed trans, add revoke record
	if ordered && log.translog.islogged(blk.Block) {
		trans.revokel.addRevokeRecord(blk.Block, log.ml)
	}

	if ordered {
		log.stats.Norderedwrite++
		trans.ordered.PushBack(blk)
		if trans.ordered.Len() >= MaxOrdered {
			panic("add_write")
		}
		trans.orderedpresent[blk.Block] = true
	} else {
		log.stats.Nlogwrite++
		trans.logged.PushBack(blk)
		trans.logpresent[blk.Block] = true
	}
}

func (trans *trans_t) iscommittable() bool {
	return trans.inprogress == 0
}

func (trans *trans_t) isorderedfull() bool {
	n := trans.ordered.Len()
	n += MaxBlkPerOp
	return n >= MaxOrdered
}

func (trans *trans_t) iscommitdescriptorfull() bool {
	n := trans.logged.Len() + trans.revokel.len()
	n += trans.inprogress * MaxBlkPerOp
	n += MaxBlkPerOp
	return n >= trans.ml.maxtrans
}

func (trans *trans_t) isempty() bool {
	return trans.logged.Len() == 0 && trans.ordered.Len() == 0
}

func (trans *trans_t) isfull() bool {
	return trans.iscommitdescriptorfull() || trans.isorderedfull()
}

func (trans *trans_t) copyrevoked(ml *memlog_t) {
	if log_debug {
		fmt.Printf("copyrevoked: transhead %d len %d\n", trans.head, trans.revokel.len())
	}
	i := trans.start + NCommitBlk
	trans.revokel.revoked.Apply(func(b *common.Bdev_block_t) {
		l := ml.getmemlog(i)
		l.Type = b.Type
		l.Block = b.Block
		copy(l.Data[:], b.Data[:])
		i += 1
		b.Free_page()
	})
}

func (trans *trans_t) copylogged(ml *memlog_t) {
	if log_debug {
		fmt.Printf("copylogged: transhead %d len %d\n", trans.head, trans.logged.Len())
	}
	i := trans.start + NCommitBlk + index_t(trans.revokel.len())
	trans.logged.Apply(func(b *common.Bdev_block_t) {
		// Make a "private" copy of b that isn't visible to FS or cache
		// XXX Use COW
		l := ml.getmemlog(i)
		l.Type = b.Type
		l.Block = b.Block
		copy(l.Data[:], b.Data[:])
		i += 1
		ml.bcache.Relse(b, "copylogged")
	})
	if i != trans.head {
		panic("copylogged")
	}
	trans.logged.Delete()
}

type copy_relse_t struct {
}

func (cp *copy_relse_t) Relse(blk *common.Bdev_block_t, s string) {
	blk.Free_page()
}

func (trans *trans_t) copyordered(ml *memlog_t) {
	if log_debug {
		fmt.Printf("copyordered: %d\n", trans.ordered.Len())
	}
	trans.ordered.Apply(func(b *common.Bdev_block_t) {
		// Make a "private" copy of b that isn't visible to FS or cache
		// XXX Use COW
		cp := common.MkBlock_newpage(b.Block, "copyordered", ml.bcache.mem,
			ml.bcache.disk, &copy_relse_t{})
		copy(cp.Data[:], b.Data[:])
		trans.orderedcopy.PushBack(cp)
		ml.bcache.Relse(b, "copyordered")
	})
	if trans.ordered.Len() != trans.orderedcopy.Len() {
		panic("copyordered")
	}
	trans.ordered.Delete()
}

func (trans *trans_t) write_ordered(ml *memlog_t) {
	if log_debug {
		fmt.Printf("write_ordered: %d\n", trans.orderedcopy.Len())
	}
	ml.bcache.Write_async_through_coalesce(trans.orderedcopy)
	trans.orderedcopy.Delete()
}

func (trans *trans_t) commit(tail index_t, ml *memlog_t) {
	if log_debug {
		fmt.Printf("commit: start %d head %d\n", trans.start, trans.head)
	}
	blks1 := common.MkBlkList()
	blks2 := common.MkBlkList()
	blks := blks1

	ml.getmemlog(trans.start).Type = common.CommitBlk
	db := ml.getdescriptor(trans.start)
	j := 0
	for i := trans.start; i != trans.head; i++ {
		if tail > i {
			panic("commit runs into log start")
		}
		// write log destination in the commit block
		l := ml.getmemlog(i)
		if l.Type == common.DataBlk {
			db.w_logdest(j, l.Block)
		} else {
			db.w_logdest(j, int(l.Type))
		}
		j++
		b := common.MkBlock(ml.diskindex(i), "commit", ml.bcache.mem, ml.bcache.disk, &_nop_relse)
		// it is safe to reuse the underlying physical page; this page
		// will not be freed, and no thread will update its content
		// until this transaction has been committed and applied.
		b.Data = l.Data
		b.Pa = l.Pa
		blks.PushBack(b)
		if ml.loginc(i) == 0 {
			blks = blks2
		}
	}
	db.w_logdest(j, EndDescriptor) // marker

	if log_debug {
		fmt.Printf("commit: commit descriptor block at %d:\n", trans.start)
		for k := 1; k < j; k++ {
			fmt.Printf("\tdescriptor %d: %d\n", k, db.r_logdest(k))
		}
	}

	// write blocks to log in batch; no need to release
	trans.ml.bcache.Write_async_blks_through(blks1)
	if blks2.Len() > 0 {
		ml.bcache.Write_async_blks_through(blks2)
	}

	trans.write_ordered(ml)

	s := runtime.Rdtsc()
	ml.flush() // flush outstanding writes  (if you kill this line, then Atomic test fails)
	ml.stats.Flushdatacycles.Add(runtime.Rdtsc() - s)

	ml.commit_head(trans.head)

	n := common.Counter_t(blks1.Len() + blks2.Len())
	ml.stats.Nblkcommitted += n
	if n > ml.stats.Maxblks_per_trans {
		ml.stats.Maxblks_per_trans = n
	}
	ml.stats.Ncommit++
	if log_debug {
		fmt.Printf("commit: committed %d blks\n", n)
	}
}

type logstat_t struct {
	Nop           common.Counter_t
	Opbegincycles common.Cycles_t
	Opendcycles   common.Cycles_t

	Nforce      common.Counter_t
	Nbatchforce common.Counter_t
	Forcecycles common.Cycles_t

	Nlogwrite       common.Counter_t
	Norderedwrite   common.Counter_t
	Nabsorption     common.Counter_t
	Nlogwrite2order common.Counter_t
	Norder2logwrite common.Counter_t
	Writecycles     common.Cycles_t

	Readcycles common.Cycles_t
}

type log_t struct {
	sync.Mutex
	admissioncond *sync.Cond
	commitcond    *sync.Cond
	ml            *memlog_t
	curtrans      *trans_t
	tail          index_t
	head          index_t
	translog      *translog_t
	stop          bool
	stopc         chan (bool)

	nextop opid_t
	stats  logstat_t
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

func (lh *logheader_t) r_tail() index_t {
	return index_t(fieldr(lh.data, START))
}

func (lh *logheader_t) w_tail(n index_t) {
	fieldw(lh.data, START, int(n))
}

func (lh *logheader_t) r_head() index_t {
	return index_t(fieldr(lh.data, END))
}

func (lh *logheader_t) w_head(n index_t) {
	fieldw(lh.data, END, int(n))
}

type logdescriptor_t struct {
	data *common.Bytepg_t
	max  int
}

func (ld *logdescriptor_t) r_logdest(p int) int {
	if p < 0 || p >= ld.max {
		panic("bad dnum")
	}
	return fieldr(ld.data, p)
}

func (ld *logdescriptor_t) w_logdest(p int, n int) {
	if p < 0 || p >= ld.max {
		panic("bad dnum")
	}
	fieldw(ld.data, p, n)
}

func (log *log_t) mk_log(ls, ll int, bcache *bcache_t) {
	log.ml = mk_memlog(ls, ll, bcache)
	log.admissioncond = sync.NewCond(log)
	log.commitcond = sync.NewCond(log)
	log.stopc = make(chan bool)
	log.translog = mkTransLog()
	log.nextop = opid_t(1)
}

func (log *log_t) write(opid opid_t, b *common.Bdev_block_t, ordered bool) {
	if log_debug {
		fmt.Printf("log_write %d %v ordered %v\n", opid, b.Block, ordered)
	}
	log.ml.bcache.Refup(b, "write")

	log.Lock()
	defer log.Unlock()

	t := log.curtrans
	ts := runtime.Rdtsc()
	t.add_write(opid, log, b, ordered)
	log.stats.Writecycles.Add(runtime.Rdtsc() - ts)
}

func (log *log_t) cancel(tail, head index_t, rl *revokelist_t) {
	rl.revoked.Apply(func(b *common.Bdev_block_t) {
		rb := log.ml.mkdescriptor(b)
		for k := 1; ; k++ {
			r := rb.r_logdest(k)
			if r == EndDescriptor {
				break
			}
			for i := tail; i != head; i++ {
				l := log.ml.getmemlog(i)
				if l.Block == r {
					if log_debug {
						fmt.Printf("cancel: i %d blkno %d\n", i, l.Block)
					}
					l.Type = Canceled
				}
			}
		}
	})
}

// the transactions that are in the process of being applied
type translog_t struct {
	trans []*trans_t
}

func mkTransLog() *translog_t {
	tl := &translog_t{}
	tl.trans = make([]*trans_t, 0)
	return tl
}

func (tl *translog_t) add(t *trans_t) {
	tl.trans = append(tl.trans, t)
}

func (tl *translog_t) remove(tail index_t) {
	if log_debug {
		fmt.Printf("translog: remove through tail %d\n", tail)
	}
	for _, t := range tl.trans {
		if t.head <= tail {
			tl.trans = tl.trans[1:]
		}
	}
}

func (tl *translog_t) String() string {
	s := ""
	for _, t := range tl.trans {
		s += fmt.Sprintf("[start %v head %d #ordered %d]; ", t.start, t.head, t.ordered.Len())
	}
	return s
}

func (tl *translog_t) last() *trans_t {
	return tl.trans[len(tl.trans)-1]
}

func (tl *translog_t) islogged(blkno int) bool {
	for _, t := range tl.trans {
		if _, ok := t.logpresent[blkno]; ok {
			return true
		}
	}
	return false
}

func (log *log_t) committer() {
	log.Lock()
	for !log.stop {
		log.ml.stats.Ncommitter.Inc()
		s := runtime.Rdtsc()
		t := log.curtrans
		if (t.force || t.isfull()) && t.iscommittable() {
			t.head = t.head + index_t(t.logged.Len()+t.revokel.len())

			if log_debug {
				fmt.Printf("committer: tail %d start %d head %d #ordered %d\n", log.tail,
					t.start, t.head, t.ordered.Len())
			}

			ts := runtime.Rdtsc()

			t.copyrevoked(log.ml)
			t.copylogged(log.ml)
			t.copyordered(log.ml)
			log.translog.add(t)

			log.ml.stats.Commitcopycycles.Add(runtime.Rdtsc() - ts)

			early := false
			if log.ml.freespace(t.head, log.tail) {
				if log_debug {
					fmt.Printf("start_commit: start next trans early %d\n", t.head)
				}
				early = true
				log.ml.stats.Nccommit++
				log.curtrans = log.mk_trans(t.head, log.ml)
				log.admissioncond.Broadcast()
			}

			if log_debug {
				fmt.Printf("committer: commit trans %d head %d #ordered %d\n",
					t.start, t.head, t.ordered.Len())
			}

			log.Unlock()
			t.commit(log.tail, log.ml)
			log.Lock()

			if log_debug {
				fmt.Printf("committer: wakeup forcer for trans %d\n", t.start)
			}

			t.forcedone = true
			t.forcecond.Broadcast()

			if t.forceapply || log.ml.almosthalffull(log.tail, t.head) {
				log.cancel(log.tail, t.head, t.revokel)
				log.tail = log.apply(log.tail, t.head)
				t.revokel.revoked.Delete()
				log.translog.remove(log.tail)
			}

			if !early {
				if log_debug {
					fmt.Printf("committer: admit tail %d head %d\n", log.tail, t.head)
				}
				log.curtrans = log.mk_trans(t.head, log.ml)
				log.admissioncond.Broadcast()
			}
		}
		log.ml.stats.Committercycles.Add(runtime.Rdtsc() - s)

		// the next trans maybe ready to commit
		t = log.curtrans
		if !log.stop && !((t.force || t.isfull()) && t.iscommittable()) {
			log.commitcond.Wait()
		}
	}

	fmt.Printf("committer: stop\n")

	log.Unlock()

	log.stopc <- true
}

func (log *log_t) apply(tail, head index_t) index_t {
	log.ml.stats.Napply++

	done := make(map[int]bool, log.ml.loglen)

	// The log is committed. If we crash while installing the blocks to
	// their destinations, we can install again.  Install backwards, writing
	// the last version of a block (and not earlier versions).

	if log_debug {
		fmt.Printf("apply log: blks from %d till %d\n", tail, head)
	}

	if tail == head {
		return head
	}

	for i := head - 1; i > tail; i-- {
		l := log.ml.getmemlog(i)
		if l.Type == common.CommitBlk || l.Type == common.RevokeBlk || l.Type == Canceled {
			// nothing to do for descriptor or canceled blocks
		} else {
			log.ml.stats.Nblkapply++
			if _, ok := done[l.Block]; !ok {
				log.ml.bcache.Write_async_through(l)
				done[l.Block] = true
			} else {
				log.ml.stats.Nabsorbapply++
			}
		}
	}

	s := runtime.Rdtsc()
	log.ml.flush() // flush apply
	t := runtime.Rdtsc()
	log.ml.stats.Flushapplydatacycles.Add(t - s)

	log.ml.commit_tail(head)

	if log_debug {
		fmt.Printf("apply log: updated tail %d\n", head)
	}

	return head
}

func (log *log_t) revoke(im []int, tail, until index_t, r int) {
	fmt.Printf("revoke %d tail %d until %d\n", r, tail, until)
	for i := tail; i != until; i++ {
		li := log.ml.logindex(i)
		if im[li] == r {
			im[li] = Canceled
		}
	}
}

func (log *log_t) installmap(tail, head index_t) []int {
	im := make([]int, log.ml.loglen)
	for i := tail; i != head; {
		ti := i
		db, dblk := log.ml.readdescriptor(i)
		im[log.ml.logindex(i)] = Canceled
		i += NCommitBlk
		j := 1
		for ; ; j++ {
			r := db.r_logdest(j)
			if r == int(common.RevokeBlk) {
				index := ti + index_t(j)
				if log_debug {
					fmt.Printf("installmap: revoke descriptor block at i %d\n", i)
				}
				rb, rblk := log.ml.readdescriptor(i)
				im[log.ml.logindex(index)] = Canceled
				for k := 1; ; k++ {
					r := rb.r_logdest(k)
					if r == EndDescriptor {
						break
					}
					log.revoke(im, tail, ti, r)
				}
				log.ml.bcache.Relse(rblk, "installmap")
				i++
			} else {
				break
			}
		}
		for ; ; j++ {
			bdest := db.r_logdest(int(j))
			if bdest == EndDescriptor {
				if log_debug {
					fmt.Printf("installmap: end descriptor block at i %d j %d\n", i, j)
				}
				break
			}
			im[log.ml.logindex(i)] = bdest
			i++
		}
		log.ml.bcache.Relse(dblk, "installmap")
	}
	return im
}

func (log *log_t) install(tail, head index_t) {
	im := log.installmap(tail, head)
	for i := tail; i != head; i++ {
		li := log.ml.logindex(i)
		dst := im[li]
		if dst != Canceled {
			if log_debug {
				fmt.Printf("install: write log %d to %d\n", i, dst)
			}
			lb, err := log.ml.bcache.Get_fill(log.ml.diskindex(i), "i", false)
			if err != 0 {
				panic("must succeed")
			}
			fb, err := log.ml.bcache.Get_fill(dst, "bdest", false)
			if err != 0 {
				panic("must succeed")
			}
			copy(fb.Data[:], lb.Data[:])
			log.ml.bcache.Write(fb)
			log.ml.bcache.Relse(lb, "install lb")
			log.ml.bcache.Relse(fb, "install fb")
		}
	}
}

func (log *log_t) recover() {
	lh, headblk := log.ml.readhdr()
	tail := lh.r_tail()
	head := lh.r_head()
	headblk.Unlock()

	log.tail = tail
	log.head = head

	log.ml.bcache.Relse(headblk, "recover")
	if tail == head {
		fmt.Printf("no FS recovery needed: head %d\n", head)
		return
	}
	fmt.Printf("starting FS recovery start %d end %d\n", tail, head)
	log.install(tail, head)
	log.ml.commit_tail(head)
	log.tail = head

	fmt.Printf("restored blocks from %d till %d\n", tail, head)
}
