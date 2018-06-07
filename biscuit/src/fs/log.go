package fs

import "fmt"
import "runtime"
import "strconv"
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
const MaxOrdered = common.BSIZE / 8 // limited by size of revoke block
const EndDescriptor = 0
const DescriptorBlk = 0
const RevokeBlk = -1
const NDescriptorBlks = 2
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

	if log_debug {
		fmt.Printf("op_begin: admit? %v\n", s)
	}

	t := runtime.Rdtsc()

	opid := log.nextop
	if log.curtrans.startcommit() || log.curtrans.committing {
		log.Unlock()
		opid = <-log.op_startc
	} else {
		log.nextop++
		log.curtrans.add_op(opid)
		log.Unlock()
	}

	log.opbegincycles += (runtime.Rdtsc() - t)

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
	s := runtime.Rdtsc()

	log.op_endc <- opid

	//if log.curtrans.startcommit() {
	//	log.op_endc <- opid
	//} else {
	//	log.curtrans.mark_done(opid)
	//}
	log.opendcycles += (runtime.Rdtsc() - s)
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

	s := runtime.Rdtsc()

	log.forcec <- true
	c := <-log.forcewaitc
	<-c
	log.forcecycles += (runtime.Rdtsc() - s)
}

// For testing
func (log *log_t) ForceApply() {
	if memfs {
		panic("memfs")
	}
	if log_debug {
		fmt.Printf("log force apply\n")
	}
	log.applyforcec <- true
	<-log.applyforcec
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
	log.readcycles += (runtime.Rdtsc() - t)
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
	s := "log:"
	s += "\n\tnop "
	s += strconv.Itoa(log.nop)
	s += "\n\t begin cycles "
	s += strconv.FormatUint(log.opbegincycles, 10)
	s += "\n\t end cycles "
	s += strconv.FormatUint(log.opendcycles, 10)
	s += "\n\tnlogwrite "
	s += strconv.Itoa(log.nlogwrite)
	s += "\n\t log begin cycles "
	s += strconv.FormatUint(log.logbegincycles, 10)
	s += "\n\t log end cycles "
	s += strconv.FormatUint(log.logendcycles, 10)
	s += "\n\t commit done cycles "
	s += strconv.FormatUint(log.logcommitdcycles, 10)
	s += "\n\t log force cycles "
	s += strconv.FormatUint(log.logforcecycles, 10)
	s += "\n\t logger commit cycles "
	s += strconv.FormatUint(log.commitcycles, 10)
	s += "\n\tnorderedwrite "
	s += strconv.Itoa(log.norderedwrite)
	s += "\n\tnorder2logwrite "
	s += strconv.Itoa(log.norder2logwrite)
	s += "\n\tnlogwrite2order "
	s += strconv.Itoa(log.nlogwrite2order)
	s += "\n\tnabsorb "
	s += strconv.Itoa(log.nabsorption)
	s += "\n\tnblkcommited "
	s += strconv.Itoa(log.ml.nblkcommitted)
	s += "\n\tncommit "
	s += strconv.Itoa(log.ml.ncommit)
	s += "\n\tnccommit "
	s += strconv.Itoa(log.nccommit)
	s += "\n\tmaxblks_per_commit "
	s += strconv.Itoa(log.ml.maxblks_per_op)
	s += "\n\tnapply "
	s += strconv.Itoa(log.napply)
	s += "\n\tnbapply "
	s += strconv.Itoa(log.nbapply)
	s += "\n\tnblkapply "
	s += strconv.Itoa(log.nblkapply)
	s += "\n\tnabsorbapply "
	s += strconv.Itoa(log.nabsorbapply)
	s += "\n\tnforce "
	s += strconv.Itoa(log.nforce)
	s += "\n\t force cycles "
	s += strconv.FormatUint(log.forcecycles, 10)
	s += "\n\tnbatchforce "
	s += strconv.Itoa(log.nbatchforce)
	s += "\n\tndelayforce "
	s += strconv.Itoa(log.ndelayforce)
	s += "\n\tncommithead "
	s += strconv.Itoa(log.ml.ncommithead)
	s += "\n\t flush head cycles "
	s += strconv.FormatUint(log.ml.headcycles, 10)
	s += "\n\t flush data cycles "
	s += strconv.FormatUint(log.ml.flushdatacycles, 10)
	s += "\n\tncommittail "
	s += strconv.Itoa(log.ml.ncommittail)
	s += "\n\t flush tail cycles\t"
	s += strconv.FormatUint(log.ml.tailcycles, 10)
	s += "\n\t flush log data cycles\t"
	s += strconv.FormatUint(log.ml.flushlogdatacycles, 10)
	s += "\n\tread cycles "
	s += strconv.FormatUint(log.readcycles, 10)
	s += "\n"
	return s
}

func StartLog(logstart, loglen int, bcache *bcache_t) *log_t {
	log := &log_t{}
	log.mk_log(logstart, loglen, bcache)
	head := log.recover()
	if !memfs {
		go log.logger(head)
		go log.committer(head)
		go log.applier(head)
	}
	return log
}

func (log *log_t) StopLog() {
	log.Force()
	log.logstopc <- true
	<-log.logstopc
	log.commitstopc <- true
	<-log.commitstopc
	log.applystopc <- true
	<-log.applystopc
}

//
// Log implementation
//
type memlog_t struct {
	log      []*common.Bdev_block_t // in-memory log, MaxBlkPerOp per op
	loglen   int                    // length of log (should be >= 2 * maxtrans)
	maxtrans int                    // max number of blocks in transaction
	logstart int                    // position of memlog on disk
	bcache   *bcache_t              // the backing store for memlog

	// stats:
	maxblks_per_op     int
	nblkcommitted      int
	ncommit            int
	ncommithead        int
	headcycles         uint64
	flushdatacycles    uint64
	ncommittail        int
	tailcycles         uint64
	flushlogdatacycles uint64
	nwriteordered      int
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
	ml.ncommithead++
	lh, headblk := ml.readhdr()
	lh.w_head(head)
	headblk.Unlock()
	ml.bcache.Write(headblk)
	s := runtime.Rdtsc()
	ml.flush() // commit log header
	ml.headcycles += (runtime.Rdtsc() - s)
	ml.bcache.Relse(headblk, "commit_done")
}

func (ml *memlog_t) commit_tail(tail index_t) {
	ml.ncommittail++
	lh, headblk := ml.readhdr()
	lh.w_tail(tail)
	headblk.Unlock()
	ml.bcache.Write(headblk)
	s := runtime.Rdtsc()
	ml.flush() // commit log header
	t := runtime.Rdtsc()
	ml.tailcycles += (t - s)
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

type buf_t struct {
	opid    opid_t
	block   *common.Bdev_block_t
	ordered bool
}

type trans_t struct {
	sync.Mutex
	ml             *memlog_t
	start          index_t
	head           index_t
	inprogress     int               // ops in progress this transaction
	logged         *common.BlkList_t // list of to-be-logged blocks
	ordered        *common.BlkList_t // list of ordered blocks
	orderedcopy    *common.BlkList_t // list of ordered blocks
	logpresent     map[int]bool      // enable quick check to see if block is in log
	orderedpresent map[int]bool      // enable quick check so see if block is in ordered
	waiters        int               // number of processes wait for this trans to commit
	waitc          chan bool         // channel on which waiters are waiting
	committing     bool
}

func mk_trans(start index_t, ml *memlog_t) *trans_t {
	t := &trans_t{start: start, head: start + NDescriptorBlks}
	t.ml = ml
	t.logged = common.MkBlkList()      // bounded by MaxDescriptor
	t.ordered = common.MkBlkList()     // bounded by MaxOrdered
	t.orderedcopy = common.MkBlkList() // bounded by MaxOrdered
	t.logpresent = make(map[int]bool, ml.loglen)
	t.orderedpresent = make(map[int]bool, MaxOrdered)
	t.waitc = make(chan bool)
	return t
}

func (trans *trans_t) add_op(opid opid_t) {
	trans.Lock()
	defer trans.Unlock()

	trans.inprogress += 1
}

func (trans *trans_t) mark_done(opid opid_t) {
	trans.Lock()
	defer trans.Unlock()

	if trans.inprogress == 0 {
		panic("mark done")
	}
	trans.inprogress -= 1
}

func (trans *trans_t) add_write(opid opid_t, log *log_t, blk *common.Bdev_block_t, ordered bool) {
	trans.Lock()
	defer trans.Unlock()

	if log_debug {
		fmt.Printf("add_write: opid %d start %d #logged %d #ordered %d b %d(%v)\n", opid,
			trans.start, trans.logged.Len(), trans.ordered.Len(), blk.Block, ordered)
	}

	// if block is in log and now ordered, remove from log
	_, lp := trans.logpresent[blk.Block]
	if ordered && lp {
		log.nlogwrite2order++
		trans.logged.RemoveBlock(blk.Block)
		delete(trans.logpresent, blk.Block)
	}

	// if block is in ordered list and now logged, remove from ordered
	_, op := trans.orderedpresent[blk.Block]
	if !ordered && op {
		log.norder2logwrite++
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
		log.nabsorption++
		log.ml.bcache.Relse(blk, "absorption")
		return
	}

	if ordered {
		log.norderedwrite++
		trans.ordered.PushBack(blk)
		if trans.ordered.Len() >= MaxOrdered {
			panic("add_write")
		}
		trans.orderedpresent[blk.Block] = true
	} else {
		log.nlogwrite++
		trans.logged.PushBack(blk)
		trans.logpresent[blk.Block] = true
	}
}

func (trans *trans_t) iscommittable() bool {
	return trans.inprogress == 0
}

// Caller must hold lock
func (trans *trans_t) isorderedfull() bool {
	n := trans.ordered.Len()
	n += MaxBlkPerOp
	return n >= MaxOrdered
}

// Caller must hold lock
func (trans *trans_t) isdescriptorfull() bool {
	n := trans.logged.Len()
	n += trans.inprogress * MaxBlkPerOp
	n += MaxBlkPerOp
	return n >= trans.ml.maxtrans
}

func (trans *trans_t) emptytrans() bool {
	trans.Lock()
	defer trans.Unlock()

	return trans.logged.Len() == 0 && trans.ordered.Len() == 0
}

func (trans *trans_t) startcommit() bool {
	trans.Lock()
	defer trans.Unlock()

	return trans.isdescriptorfull() || trans.isorderedfull()
}

func (trans *trans_t) unblock_waiters() {
	if log_debug {
		fmt.Printf("wakeup waiters %v\n", trans.waiters)
	}
	for i := 0; i < trans.waiters; i++ {
		trans.waitc <- true
	}
}

func (trans *trans_t) copyintolog(ml *memlog_t) {
	if log_debug {
		fmt.Printf("copyintolog: transhead %d\n", trans.head)
	}
	i := trans.start + NDescriptorBlks
	trans.logged.Apply(func(b *common.Bdev_block_t) {
		// Make a "private" copy of b that isn't visible to FS or cache
		// XXX Use COW
		l := ml.getmemlog(i)
		l.Block = b.Block
		copy(l.Data[:], b.Data[:])
		i += 1
		ml.bcache.Relse(b, "writelog")
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
		ml.bcache.Relse(b, "writelog")
	})
	if trans.ordered.Len() != trans.orderedcopy.Len() {
		panic("copyordered")
	}
	trans.ordered.Delete()
}

// Update the ordered blocks in place
func (trans *trans_t) write_ordered(ml *memlog_t) {
	if log_debug {
		fmt.Printf("write_ordered: %d\n", trans.orderedcopy.Len())
	}
	ml.bcache.Write_async_through_coalesce(trans.orderedcopy)
	trans.orderedcopy.Delete()
}

func (trans *trans_t) fillRevoke(ml *memlog_t, rl []int) {
	db := ml.getdescriptor(trans.start + 1)
	db.w_logdest(0, RevokeBlk)
	for i, r := range rl {
		db.w_logdest(i+1, r)
	}
	db.w_logdest(len(rl)+1, EndDescriptor)
}

func (trans *trans_t) commit(tail index_t, ml *memlog_t) {
	if log_debug {
		fmt.Printf("commit: start %d head %d\n", trans.start, trans.head)
	}
	blks1 := common.MkBlkList()
	blks2 := common.MkBlkList()
	blks := blks1

	db := ml.getdescriptor(trans.start)
	j := 0
	for i := trans.start; i != trans.head; i++ {
		if tail > i {
			panic("commit runs into log start")
		}
		// write log destination in the descriptor block
		l := ml.getmemlog(i)
		db.w_logdest(j, l.Block)
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
		fmt.Printf("commit: descriptor block at %d:\n", trans.start)
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
	ml.flushdatacycles += (runtime.Rdtsc() - s)

	ml.commit_head(trans.head)

	ml.getmemlog(trans.start).Block = DescriptorBlk // now safe to mark block as a descriptor block in mem
	ml.getmemlog(trans.start + 1).Block = RevokeBlk

	n := blks1.Len() + blks2.Len()
	ml.nblkcommitted += n
	if n > ml.maxblks_per_op {
		ml.maxblks_per_op = n
	}
	ml.ncommit++
	if log_debug {
		fmt.Printf("commit: committed %d blks\n", n)
	}
}

type applyreq_t struct {
	head index_t
	rl   []int
}

type log_t struct {
	sync.Mutex
	ml         *memlog_t
	curtrans   *trans_t
	op_startc  chan opid_t
	op_endc    chan opid_t
	forcec     chan bool
	forcewaitc chan (chan bool)

	logstopc    chan bool
	commitstopc chan bool
	applystopc  chan bool

	commitc     chan *trans_t
	commitdonec chan bool
	applyreqc   chan applyreq_t
	applyrepc   chan index_t
	applyforcec chan bool

	nextop opid_t

	// some stats
	nop              int
	opbegincycles    uint64
	opendcycles      uint64
	napply           int
	nabsorption      int
	nlogwrite        int
	logendcycles     uint64
	logbegincycles   uint64
	logcommitdcycles uint64
	logforcecycles   uint64
	norderedwrite    int
	nblkapply        int
	nabsorbapply     int
	nforce           int
	nbatchforce      int
	forcecycles      uint64
	ndelayforce      int
	commitcycles     uint64
	nccommit         int
	nbapply          int
	nlogwrite2order  int
	norder2logwrite  int
	readcycles       uint64
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
	log.op_startc = make(chan opid_t)
	log.op_endc = make(chan opid_t)
	log.forcec = make(chan bool)
	log.forcewaitc = make(chan (chan bool))
	log.commitc = make(chan *trans_t)
	log.commitdonec = make(chan bool)
	log.applyreqc = make(chan applyreq_t)
	log.applyrepc = make(chan index_t)
	log.applyforcec = make(chan bool)
	log.logstopc = make(chan bool)
	log.commitstopc = make(chan bool)
	log.applystopc = make(chan bool)
}

func (log *log_t) write(opid opid_t, b *common.Bdev_block_t, ordered bool) {
	if log_debug {
		fmt.Printf("log_write %d %v ordered %v\n", opid, b.Block, ordered)
	}
	log.ml.bcache.Refup(b, "write")
	// ok too lookup curtrans, since no concurrent read/mod
	log.curtrans.add_write(opid, log, b, ordered)
}

func (log *log_t) apply(tail, head index_t) index_t {
	log.napply++

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
		if l.Block == DescriptorBlk || l.Block == RevokeBlk || l.Block == Canceled {
			// nothing to do for descriptor blocks
		} else {
			log.nblkapply++
			if _, ok := done[l.Block]; !ok {
				log.ml.bcache.Write_async_through(l)
				done[l.Block] = true
			} else {
				log.nabsorbapply++
			}
		}
	}

	s := runtime.Rdtsc()
	log.ml.flush() // flush apply
	t := runtime.Rdtsc()
	log.ml.flushlogdatacycles += (t - s)

	log.ml.commit_tail(head)

	if log_debug {
		fmt.Printf("apply log: updated tail %d\n", head)
	}

	return head
}

func (log *log_t) revokeList(tail, head index_t, rl []int) {
	for _, r := range rl {
		for i := tail; i != head; i++ {
			l := log.ml.getmemlog(i)
			if l.Block == r {
				l.Block = Canceled
			}

		}
	}
}

// the applier owns the tail of the log
func (l *log_t) applier(t index_t) {
	tail := t
	head := t
	waiting := false
	stopping := false
	for {
		select {
		case stopping = <-l.applystopc:
			if !waiting {
				l.applystopc <- true
				return
			}
		case req := <-l.applyreqc:
			head = req.head
			if log_debug {
				fmt.Printf("applier: tail %d head %d rl %v\n", tail, head, req.rl)
			}
			l.revokeList(tail, head, req.rl)
			if l.ml.freespace(head, tail) {
				l.applyrepc <- tail
			} else {
				// make committer wait
				l.nbapply++
				waiting = true
			}
			if l.ml.almosthalffull(tail, head) {
				// start applying so that by the next commit log
				// has space for another transaction
				tail = l.apply(tail, head)
			}
			if waiting {
				waiting = false
				l.applyrepc <- tail
			}
			if stopping {
				l.applystopc <- true
				return
			}
		case <-l.applyforcec:
			tail = l.apply(tail, head)
			l.applyforcec <- true
		}
	}
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

func (tl *translog_t) revokeLog(trans *trans_t) []int {
	r := make([]int, 0)
	trans.ordered.Apply(func(b *common.Bdev_block_t) {
		for _, t := range tl.trans {
			if _, ok := t.logpresent[b.Block]; ok {
				r = append(r, b.Block)
			}
		}
	})
	return r
}

// the committer owns the head of the log
func (l *log_t) committer(h index_t) {
	tail := h
	head := h
	tl := mkTransLog()
	for {
		select {
		case <-l.commitstopc:
			l.commitstopc <- true
			return
		case t := <-l.commitc:
			if log_debug {
				fmt.Printf("committer: start waiters %d tail %d head %d #ordered %d\n",
					t.waiters, tail, head, t.ordered.Len())
			}

			rl := tl.revokeLog(t)

			tl.add(t)

			t.fillRevoke(l.ml, rl)
			t.copyintolog(l.ml)
			t.copyordered(l.ml)

			if l.ml.freespace(t.head, tail) {
				if log_debug {
					fmt.Printf("committer: start next trans early\n")
				}
				l.nccommit++
				l.commitdonec <- false
			}

			t.commit(tail, l.ml)

			head = t.head

			l.applyreqc <- applyreq_t{head, rl}
			tail = <-l.applyrepc

			tl.remove(tail)
			t.unblock_waiters()

			l.commitdonec <- true

			if log_debug {
				fmt.Printf("committer: done tail %v head %v\n", tail, head)
			}
		}
	}
}

func (l *log_t) logger(h index_t) {
	l.nextop = opid_t(1) // reserve 0 for no opid
	head := h
	commitready := true
	stopping := false
	var ts1 uint64
	for {
		common.Kunresdebug()
		common.Kresdebug(100<<20, "logger")

		l.curtrans = mk_trans(head, l.ml)

		admissionc := l.op_startc // set admissionc to nil when transaction is full

		// Fall out of when the loop when transaction must be committed:
		// transaction is full or a process forces the transaction. We
		// delay the commit until the commit of previous transaction has
		// finished and all ops of this transactions have completed.
		for !(l.curtrans.committing && commitready && l.curtrans.iscommittable()) {
			select {
			case opid := <-l.op_endc:
				l.nop++
				s := runtime.Rdtsc()
				l.curtrans.mark_done(opid)
				if admissionc == nil { // admit ops again?
					if l.curtrans.startcommit() { // nope, full and commit
						l.curtrans.committing = true
					} else {
						if log_debug {
							fmt.Printf("logger: can admit op %d\n", l.nextop)
						}
						// admit another op. this may op
						// did not use all the space
						// that it reserved.
						admissionc = l.op_startc
					}
				}
				l.logendcycles += (runtime.Rdtsc() - s)
			case admissionc <- l.nextop:
				l.Lock()
				s := runtime.Rdtsc()
				if log_debug {
					fmt.Printf("logger: admit %d\n", l.nextop)
				}
				l.curtrans.add_op(l.nextop)
				l.nextop++
				if l.curtrans.startcommit() {
					admissionc = nil
				}
				l.logbegincycles += (runtime.Rdtsc() - s)
				l.Unlock()
			case <-l.forcec:
				s := runtime.Rdtsc()
				l.nforce++
				if l.curtrans.waiters > 0 {
					l.nbatchforce++
				}
				l.curtrans.waiters++
				if log_debug {
					fmt.Printf("logger: force %d\n", l.curtrans.waiters)
				}
				l.forcewaitc <- l.curtrans.waitc
				l.curtrans.committing = true // XXX lock?
				if !commitready {
					l.ndelayforce++
				}
				l.logforcecycles += (runtime.Rdtsc() - s)
			case b := <-l.commitdonec:
				s := runtime.Rdtsc()
				if !b {
					panic("committer: sent false?")
				}
				commitready = true
				if stopping {
					l.logstopc <- true
					return
				}
				l.logcommitdcycles += (runtime.Rdtsc() - s)
			case stopping = <-l.logstopc:
				if commitready {
					l.logstopc <- true
					return
				}
			}
		}

		if l.curtrans.emptytrans() {
			l.curtrans.unblock_waiters()
		} else {
			// commit transaction, and (hopefully) in parallel with
			// committing start new transaction.
			ts1 = runtime.Rdtsc()
			l.curtrans.head = l.curtrans.head + index_t(l.curtrans.logged.Len())
			head = l.curtrans.head

			l.commitc <- l.curtrans
			// wait until commit thread tells us to go ahead with
			// next trans
			commitdone := <-l.commitdonec
			l.commitcycles += (runtime.Rdtsc() - ts1)
			if !commitdone {
				commitready = false
			} else {
				commitready = true
			}
		}
	}
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
		rb, rblk := log.ml.readdescriptor(i + 1)
		im[log.ml.logindex(i)] = Canceled
		im[log.ml.logindex(i+1)] = Canceled
		i += NDescriptorBlks
		for j := 1; ; j++ {
			r := rb.r_logdest(j)
			if r == EndDescriptor {
				break
			}
			log.revoke(im, tail, ti, r)
		}
		for j := NDescriptorBlks; ; j++ {
			bdest := db.r_logdest(j)
			if bdest == EndDescriptor {
				if log_debug {
					fmt.Printf("installmap: end descriptor block at i %d j %d\n", i, j)
				}
				break
			}
			im[log.ml.logindex(i)] = bdest
			i++
		}
		log.ml.bcache.Relse(dblk, "install db")
		log.ml.bcache.Relse(rblk, "install db")
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

func (log *log_t) recover() index_t {
	lh, headblk := log.ml.readhdr()
	tail := lh.r_tail()
	head := lh.r_head()
	headblk.Unlock()

	log.ml.bcache.Relse(headblk, "recover")
	if tail == head {
		fmt.Printf("no FS recovery needed: head %d\n", head)
		return head
	}
	fmt.Printf("starting FS recovery start %d end %d\n", tail, head)
	log.install(tail, head)
	log.ml.commit_tail(head)

	fmt.Printf("restored blocks from %d till %d\n", tail, head)
	return head
}
