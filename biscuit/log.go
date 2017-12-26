package main

import "fmt"
import "strconv"

const log_debug = false

// file system journal
var fslog	= log_t{}
var loglen = 0

type logread_t struct {
	buf		*bdev_block_t
	had		bool
	gencommit	bool
}

type log_entry_t struct {
	block           int            // the final destination of buf
	buf             *bdev_block_t 
}

// list of dirty blocks that are pending commit.
type log_t struct {
	log		[]log_entry_t
	logmap          map[int]int   // map from block number to index in log
	memhead		int           // head of the log in memory
	diskhead        int           // head of the log on disk 
	logstart	int
	loglen		int
	incoming	chan *bdev_block_t
	admission	chan bool
	done		chan bool
	force		chan bool
	commitwait	chan bool

	// some stats
	maxblks_per_op int
	nblkcommitted     int
	ncommit           int
	napply            int
	nabsorption       int
	nlogwrite         int
	nblkapply         int
	nabsorpapply      int
}

// first log header block format
// bytes, meaning
// 0-7,   valid log blocks
// 8-511, log destination (63)
type logheader_t struct {
	data	*bytepg_t
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
	return fieldr(lh.data, 8 + p)
}

func (lh *logheader_t) w_logdest(p int, n int) {
	if p < 0 || p > loglen {
		panic("bad dnum")
	}
	fieldw(lh.data, 8 + p, n)
}

func (log *log_t) init(ls int, ll int) {
	loglen = ll-1
	log.memhead = 0
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = ll - 1
	log.log = make([]log_entry_t, log.loglen)
	log.logmap = make(map[int]int, log.loglen)
	log.incoming = make(chan *bdev_block_t)
	log.admission = make(chan bool)
	log.done = make(chan bool)
	log.force = make(chan bool)
	log.commitwait = make(chan bool)

	if log.loglen >= BSIZE/4 {
		panic("log_t.init: log will not fill in one header block\n")
	}
}

// an upperbound on the number of blocks written per system call. this is
// necessary in order to guarantee that the log is long enough for the allowed
// number of concurrent fs syscalls.
const maxblkspersys = 10

func (l *log_t) full(nops int) bool {
	reserved := maxblkspersys * nops
	return reserved + l.memhead >= l.loglen
}

func (log *log_t) addlog(buf *bdev_block_t) {
	log.nlogwrite++
	// log absorption.
	if i, ok := log.logmap[buf.block]; ok {
		l := log.log[i]
		if l.block == buf.block {
			if l.buf != buf {
				panic("absorption")
			}
			// buffer is already in log. if the write of this block
			// is in a later transaction, we know this later
			// transaction will commit with the one that modified
			// this block earlier.
			log.nabsorption++
			bcache_relse(buf, "absorption")
			return
		}
	}
	
	memhead := log.memhead
	if memhead >= len(log.log) {
		panic("log overflow")
	}

	// No need to copy data of buf because later ops who reads the modified
	// block will commmit with this transaction or not commit.  We never
	// commit while an operation is still on-going.
	log.log[memhead] = log_entry_t{buf.block, buf}
	log.logmap[buf.block] = memhead
	log.memhead++
}

// headblk is in cache
func (log *log_t) apply(headblk *bdev_block_t) {
	done := make(map[int]bool, log.loglen)
		
	if log_debug {
		fmt.Printf("apply log: %v %v %v\n", log.memhead, log.diskhead, log.loglen)
	}
	
	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover.  install backwards,
	// writing the last version of a block (and not earlier versions).
	for i := log.memhead-1; i >= 0; i-- {
		l := log.log[i]
		log.nblkapply++
		if _, ok := done[l.buf.block]; !ok {
			bcache_write_async(l.buf)
			bcache_relse(l.buf, "apply")
			done[l.buf.block] = true
		} else {
			log.nabsorpapply++
		}
	}

	bdev_flush()  // flush apply
	
	// success; clear flag indicating to recover from log
	lh := logheader_t{headblk.data}
	lh.w_recovernum(0)
	bcache_write(headblk)

	bdev_flush()  // flush cleared commit
}

func (log *log_t) commit() {
	if log.memhead == log.diskhead {
		// nothing to commit
		return
	}

	if log_debug {
		fmt.Printf("commit %v %v\n", log.memhead, log.diskhead)
	}

	log.ncommit++
	newblks := log.memhead-log.diskhead
	if newblks > log.maxblks_per_op {
		log.maxblks_per_op = newblks
	}

	// read the log header from disk; it may contain commit blocks from
	// current transactions, if we haven't applied yet.
	headblk, err := bcache_get_fill(log.logstart, "commit", false)
	if err != 0 {
		panic("cannot read commit block\n")
	}
	
	lh := logheader_t{headblk.data}
	blks := make([]*bdev_block_t, newblks)
	for i := log.diskhead; i < log.memhead; i++ {
		l := log.log[i]
		// install log destination in the first log block
		lh.w_logdest(i, l.block)

		// fill in log blocks
                b, err := bcache_get_nofill(log.logstart+i+1, "log", true)
		if err != 0 {
			panic("cannot get log block\n")
		}
		copy(b.data[:], l.buf.data[:])
		b.Unlock()
		
		blks[i-log.diskhead] = b
	}
	
	lh.w_recovernum(log.memhead)

	bcache_write_async_blks(blks)  // write head

	for _, b := range blks {
		bcache_relse(b, "writelog")
	}
	
	bdev_flush()   // flush log

	bcache_write(headblk)  	// write log header

	bdev_flush()   // commit log header

	log.nblkcommitted += newblks

	if newblks != len(blks) {
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
	// reset absorption map
	log.logmap = make(map[int]int, log.loglen)

	bcache_relse(headblk, "commit done")
}

func log_daemon(l *log_t) {
	for {
		adm := l.admission
		done := false
		nops := 0
		waiters := 0
		
		for !done {
			select {
			case nb := <- l.incoming:
				if nops <= 0 {
					panic("no admit")
				}
				if l.memhead >= l.loglen {
					panic("full")
				}
				l.addlog(nb)
			case <- l.done:
				nops--
				//fmt.Printf("done: nops %v adm %v full? %v %v\n", nops, adm, l.full(nops+1),
				//	l.memhead)
				if adm == nil {   // is an op waiting for admission?
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
				if l.full(nops+1) {  // next one wait?
					adm = nil
				}
			case <- l.force:
				waiters++
				adm = nil
				if nops == 0 {
					done = true
				}
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

func op_begin(s string) {
	if memtime {
		return
	}
	<- fslog.admission
	if log_debug {
		fmt.Printf("op_begin: go %v\n", s)
	}
}

func op_end() {
	if memtime {
		return
	}
	fslog.done <- true
}


// log_write increments ref so that the log has always a valid ref to the buf's
// page the logging layer refdowns when it it is done with the page.  the caller
// of log_write shouldn't hold buf's lock.
func (b *bdev_block_t) log_write() {
	if memtime {
		return
	}
	if log_debug {
		fmt.Printf("log_write %v\n", b.block)
	}
	bcache_refup(b, "log_write")
	fslog.incoming <- b
}

func log_init(logstart, loglen int) err_t {
	fslog.init(logstart, loglen)
	err := log_recover()
	if err != 0 {
		return err
	}
	go log_daemon(&fslog)
	return 0
}

func log_stat() string {
	s := "log:"
	s += "\n\tnlogwrite "
	s += strconv.Itoa(fslog.nlogwrite)
	s += "\n\tnabsorp "
	s += strconv.Itoa(fslog.nabsorption)
	s += "\n\tnblkcommited "
	s += strconv.Itoa(fslog.nblkcommitted)
	s += "\n\tncommit "
	s += strconv.Itoa(fslog.ncommit)
	s += "\n\tmaxblks_per_commit "
	s += strconv.Itoa(fslog.maxblks_per_op)
	s += "\n\tnapply "
	s += strconv.Itoa(fslog.napply)
	s += "\n\tnblkapply "
	s += strconv.Itoa(fslog.nblkapply)
	s += "\n\tnabsorpapply "
	s += strconv.Itoa(fslog.nabsorpapply)
	s += "\n"
	return s
}

func log_recover() err_t {
	l := &fslog
	b, err := bcache_get_fill(l.logstart, "fs_recover_logstart", false)
	if err != 0 { 
		return err
	}
	lh := logheader_t{b.data}
	rlen := lh.recovernum()
	if rlen == 0 {
		fmt.Printf("no FS recovery needed\n")
		bcache_relse(b, "fs_recover_logstart")
		return 0
	}
	fmt.Printf("starting FS recovery...")

	for i := 0; i < rlen; i++ {
		bdest := lh.logdest(i)
		lb, err := bcache_get_fill(l.logstart + 1 + i, "i", false)
		if err != 0 {
			return err
		}
		fb, err := bcache_get_fill(bdest, "bdest", false)
		if err != 0 {
			return err
		}
		copy(fb.data[:], lb.data[:])
		bcache_write(fb)
		bcache_relse(lb, "fs_recover1")
		bcache_relse(fb, "fs_recover2")
	}

	// clear recovery flag
	lh.w_recovernum(0)
	bcache_write(b)
	bcache_relse(b, "fs_recover_logstart")
	fmt.Printf("restored %v blocks\n", rlen)
	return 0
}
