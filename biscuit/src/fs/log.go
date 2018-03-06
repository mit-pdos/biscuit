package fs

import "fmt"
import "strconv"

import "common"

const log_debug = false

// File system journal.  The file system brackets FS calls (e.g.,create) with
// Op_begin and Op_end(); the log makes sure that these operations happen
// atomically with respect to crashes.  Operations are grouped in
// transactions. A transaction is committed to the on-disk log on sync() or when
// the log is close to full.  After a transaction is committed, a new
// transaction starts.  Transactions in the log are applied to the file system
// when the on-disk log close to full.  All writes go through the log, but
// ordered writes are not appended to the on-disk log, but overwrite their home
// location.  The file system should use logged writes for all its data
// structures, and use ordered writes only for file data.  The file system must
// guarantee that it performs no more than maxblkspersys logged writes in an
// operation, to ensure that its operation will fit in the log.

var loglen = 0 // for marshalling/unmarshalling

const LogOffset = 1 // log block 0 is used for head

type buf_t struct {
	block   *common.Bdev_block_t
	ordered bool
}

type log_t struct {
	log            []*common.Bdev_block_t       // in-memory log
	logpresent     map[int]bool                 // enable quick check to see if block is in log
	absorb         map[int]*common.Bdev_block_t // map from block number to block to absorb in current transaction
	ordered        *common.BlkList_t            // list of ordered blocks
	orderedpresent map[int]bool                 // enable quick check so see if block is in ordered
	memhead        int                          // head of the log in memory
	diskhead       int                          // head of the log on disk
	logstart       int
	loglen         int
	incoming       chan buf_t
	admission      chan bool
	done           chan bool
	force          chan bool
	commitwait     chan bool
	stop           chan bool
	stopwait       chan bool

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
	norder2logwrite int
	nblkapply       int
	nabsorbapply    int
	nforceordered   int
}

//
// The public interface to the logging layer
//

func (log *log_t) Op_begin(s string) {
	if memfs {
		return
	}
	if log_debug {
		fmt.Printf("op_begin: admit? %v\n", s)
	}
	<-log.admission
	if log_debug {
		fmt.Printf("op_begin: go %v\n", s)
	}
}

func (log *log_t) Op_end() {
	if memfs {
		return
	}
	log.done <- true
}

// ensure any fs ops in the journal preceding this sync call are flushed to disk
// by waiting for log commit.
func (log *log_t) Force() {
	log.force <- true
	<-log.commitwait
}

// Write increments ref so that the log has always a valid ref to the buf's page
// the logging layer refdowns when it it is done with the page.  the caller of
// log_write shouldn't hold buf's lock.
func (log *log_t) Write(b *common.Bdev_block_t) {
	if memfs {
		return
	}
	if log_debug {
		fmt.Printf("log_write %v\n", b.Block)
	}
	log.fs.bcache.Refup(b, "log_write")
	log.incoming <- buf_t{b, false}
}

func (log *log_t) Write_ordered(b *common.Bdev_block_t) {
	if memfs {
		return
	}
	if log_debug {
		fmt.Printf("log_write_ordered %v\n", b.Block)
	}
	log.fs.bcache.Refup(b, "log_write_ordered")
	log.incoming <- buf_t{b, true}
}

// All layers above log read blocks through the log layer, which are mostly
// wrappers for the the corresponding cache operations.
func (log *log_t) Get_fill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.read(mkread(log.fs.bcache.Get_fill, blkn, s, lock))
}

func (log *log_t) Get_zero(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.read(mkread(log.fs.bcache.Get_zero, blkn, s, lock))
}

func (log *log_t) Get_nofill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	return log.read(mkread(log.fs.bcache.Get_nofill, blkn, s, lock))
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
	s += "\n\tnordered2logwrite "
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
	s += "\n\tnforceordered "
	s += strconv.Itoa(log.nforceordered)
	s += "\n"
	return s
}

func StartLog(logstart, loglen int, fs *Fs_t, disk common.Disk_i) *log_t {
	if memfs {
		return nil
	}
	fslog := &log_t{}
	fslog.fs = fs
	fslog.init(logstart, loglen, disk)
	err := fslog.recover()
	if err != 0 {
		return nil
	}
	go log_daemon(fslog)
	return fslog
}

func (log *log_t) stopLog() {
	log.stop <- true
	<-log.stopwait
}

//
// Log implementation
//

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

func (log *log_t) init(ls int, ll int, disk common.Disk_i) {
	// loglen is global
	loglen = ll - LogOffset // leave space for head and orphan inodes
	log.memhead = 0
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = loglen
	log.log = make([]*common.Bdev_block_t, log.loglen)
	log.logpresent = make(map[int]bool, log.loglen)
	log.absorb = make(map[int]*common.Bdev_block_t, log.loglen)
	log.ordered = common.MkBlkList() // bounded by MaxOrdered
	log.orderedpresent = make(map[int]bool)
	log.incoming = make(chan buf_t)
	log.admission = make(chan bool)
	log.done = make(chan bool)
	log.force = make(chan bool)
	log.commitwait = make(chan bool)
	log.stop = make(chan bool)
	log.stopwait = make(chan bool)
	log.disk = disk

	if log.loglen >= common.BSIZE/4 {
		panic("log_t.init: log will not fill in one header block\n")
	}
}

// an upperbound on the number of blocks written per system call. this is
// necessary in order to guarantee that the log is long enough for the allowed
// number of concurrent fs syscalls.
const MaxBlkPerOp = 10
const MaxOrdered = 5000

func (l *log_t) full(nops int) bool {
	reserved := MaxBlkPerOp * nops
	logfull := reserved+l.memhead >= l.loglen
	orderedfull := l.ordered.Len() >= MaxOrdered
	return logfull || orderedfull
}

func (log *log_t) maxops() int {
	return log.loglen / MaxBlkPerOp
}

func (log *log_t) addlog(buf buf_t) {

	// If a write for buf.block is present in the in-memory log (i.e.,
	// either in memory or in the unapplied disk log), then we put new
	// ordered writes to that block in the log too. Otherwise, we run the
	// risk that apply will overwrite the value of a more recent ordered
	// write.
	_, present := log.logpresent[buf.block.Block]
	if buf.ordered && !present {
		log.norderedwrite++
	} else {
		log.nlogwrite++
		if buf.ordered {
			log.norder2logwrite++
		}
	}

	_, presentordered := log.orderedpresent[buf.block.Block]
	if !buf.ordered && presentordered {
		// XXX maybe orderedpresent should keep track of list element
		log.ordered.RemoveBlock(buf.block.Block)
		delete(log.orderedpresent, buf.block.Block)
		delete(log.absorb, buf.block.Block)
	}

	// log absorption.
	if _, ok := log.absorb[buf.block.Block]; ok {
		// Buffer is already in log or in ordered, but not on disk
		// yet. We wrote it (since there is only one common.Bdev_block_t for
		// each blockno), so it has already been absorbed.
		//
		// If the write of this block is in a later file
		// system op, we know this later op will commit with the one
		// that modified this block earlier, because the op was
		// admitted.
		log.nabsorption++
		log.fs.bcache.Relse(buf.block, "absorption")
		return
	}
	log.absorb[buf.block.Block] = buf.block

	// No need to copy data of buf because later ops who reads the modified
	// block will commmit with this transaction (or crash, but then nop will
	// commit).  We never commit while an operation is still on-going.
	if buf.ordered && !present { // kill !present and don't absorb above, then Ordered test fails)
		log.ordered.PushBack(buf.block)
		log.orderedpresent[buf.block.Block] = true
	} else {
		memhead := log.memhead
		if memhead >= len(log.log) {
			panic("log overflow")
		}
		log.log[memhead] = buf.block
		log.memhead++
		log.logpresent[buf.block.Block] = true
	}
}

// headblk is in cache
func (log *log_t) apply(headblk *common.Bdev_block_t) {
	done := make(map[int]bool, log.loglen)

	if log_debug {
		fmt.Printf("apply log: %v %v %v\n", log.memhead, log.diskhead, log.loglen)
	}

	// The log is committed. If we crash while installing the blocks to
	// their destinations, we should be able to recover.  Install backwards,
	// writing the last version of a block (and not earlier versions).
	for i := log.memhead - 1; i >= 0; i-- { // don't install header and orphan inodes
		l := log.log[i]
		log.nblkapply++
		if _, ok := done[l.Block]; !ok {
			log.fs.bcache.Write_async(l)
			log.fs.bcache.Relse(l, "apply")
			done[l.Block] = true
		} else {
			log.nabsorbapply++
		}
	}

	log.flush() // flush apply

	// success; clear flag indicating to recover from log
	lh := logheader_t{headblk.Data}
	lh.w_recovernum(0)
	log.fs.bcache.Write(headblk)

	log.flush() // flush cleared commit

	log.logpresent = make(map[int]bool, log.loglen)
}

func (log *log_t) write_ordered() {
	// update the ordered blocks in place
	log.nforceordered++
	log.ordered.Apply(func(b *common.Bdev_block_t) {
		// fmt.Printf("write ordered %d\n", b.Block)
		log.fs.bcache.Write_async(b)
		log.fs.bcache.Relse(b, "writeordered")
	})
	log.ordered.Delete()
	log.orderedpresent = make(map[int]bool)
}

func (log *log_t) commit() {
	if log.memhead == log.diskhead {
		// nothing to commit, but maybe some file blocks to sync
		if log_debug {
			fmt.Printf("commit: flush ordered blks %d\n", log.ordered.Len())
		}
		log.write_ordered()
		log.flush()
		return
	}

	if log_debug {
		fmt.Printf("commit %v %v\n", log.memhead, log.diskhead)
	}

	log.ncommit++
	newblks := log.memhead - log.diskhead
	if newblks > log.maxblks_per_op {
		log.maxblks_per_op = newblks
	}

	// read the log header from disk; it may contain commit blocks from
	// current transactions, if we haven't applied yet.
	headblk, err := log.fs.bcache.Get_fill(log.logstart, "commit", false)
	if err != 0 {
		panic("cannot read commit block\n")
	}
	lh := logheader_t{headblk.Data}

	blks := common.MkBlkList()
	for i := log.diskhead; i < log.memhead; i++ {
		l := log.log[i]
		// install log destination in the header block
		lh.w_logdest(i, l.Block)

		// fill in log block
		b, err := log.fs.bcache.Get_nofill(log.logstart+i+LogOffset, "log", true)
		if err != 0 {
			panic("cannot get log block\n")
		}
		copy(b.Data[:], l.Data[:])
		b.Unlock()
		blks.PushBack(b)
	}

	lh.w_recovernum(log.memhead)

	// write blocks to log in batch
	log.fs.bcache.Write_async_blks(blks)
	blks.Apply(func(b *common.Bdev_block_t) {
		log.fs.bcache.Relse(b, "writelog")
	})

	log.write_ordered()

	log.flush() // flush outstanding writes  (if you kill this line, then Atomic test fails)

	log.fs.bcache.Write(headblk) // write log header

	log.flush() // commit log header

	log.nblkcommitted += newblks

	if newblks != blks.Len() {
		panic("xxx")
	}

	// apply log only when there is no room for another op. this avoids
	// applies when there room in the log (e.g., a sync forced the log)
	if log.full(1) {
		log.napply++
		log.apply(headblk)
		log.memhead = 0
		log.diskhead = 0
	}
	// data till log.memhead has been written to log
	log.diskhead = log.memhead

	// reset absorption map and ordered list
	log.absorb = make(map[int]*common.Bdev_block_t, log.loglen)

	// done with log header
	log.fs.bcache.Relse(headblk, "commit done")
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
		return err
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
		lb, err := log.fs.bcache.Get_fill(log.logstart+LogOffset+i, "i", false)
		if err != 0 {
			return err
		}
		fb, err := log.fs.bcache.Get_fill(bdest, "bdest", false)
		if err != 0 {
			return err
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

func log_daemon(l *log_t) {
	for {
		adm := l.admission
		done := false
		nops := 0
		waiters := 0

		for !done {
			select {
			case nb := <-l.incoming:
				if nops <= 0 {
					panic("no admit")
				}
				if l.memhead >= l.loglen {
					fmt.Printf("memhead %v loglen %v\n", l.memhead, l.loglen)
					panic("full")
				}
				l.addlog(nb)
			case <-l.done:
				nops--
				//fmt.Printf("done: nops %v adm %v full? %v %v\n", nops, adm, l.full(nops+1),
				// l.memhead)
				if adm == nil { // is an op waiting for admission?
					// fmt.Printf("don't admit %d %v\n", nops, l.full(nops+1))
					if waiters > 0 || l.full(nops+1) {
						// No more log space or forced to commit
						if nops == 0 {
							done = true
						}
					} else {
						// admit another op. this may op
						// did not use all the space
						// that it reserved.
						adm = l.admission
					}
				}
			case adm <- true:
				nops++
				//fmt.Printf("adm: next wait? %v %v %v %v\n", nops, l.full(nops+1),
				//	l.loglen, l.memhead)
				if l.full(nops + 1) { // next one wait?
					// fmt.Printf("don't admit %d\n", nops)
					adm = nil
				}
			case <-l.force:
				waiters++
				adm = nil
				if nops == 0 {
					done = true
				}
			case <-l.stop:
				l.stopwait <- true
				return
			}
		}

		l.commit()

		if waiters > 0 {
			if log_debug {
				fmt.Printf("wakeup waiters/syncers %v\n", waiters)
			}
			go func() {
				for i := 0; i < waiters; i++ {
					l.commitwait <- true
				}
			}()
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
