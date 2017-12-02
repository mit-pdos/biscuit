package main

import "fmt"

// file system journal
var fslog	= log_t{}

type logread_t struct {
	loggen		uint8
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
	lhead		int
	logstart	int
	loglen		int
	incoming	chan bdev_block_t
	incgen		chan uint8
	logread		chan logread_t
	logreadret	chan logread_t
	admission	chan bool
	done		chan bool
	force		chan bool
	commitwait	chan bool
	tmpblk		*bdev_block_t
}

func (log *log_t) init(ls int, ll int) {
	log.lhead = 0
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = ll - 1
	log.log = make([]log_entry_t, log.loglen)
	log.tmpblk = bdev_block_new(log.logstart, "logstart")
	log.incoming = make(chan bdev_block_t)
	log.incgen = make(chan uint8)
	log.logread = make(chan logread_t)
	log.logreadret = make(chan logread_t)
	log.admission = make(chan bool)
	log.done = make(chan bool)
	log.force = make(chan bool)
	log.commitwait = make(chan bool)
}

func (log *log_t) addlog(buf bdev_block_t) {
	// log absorption
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		if l.block == buf.block {
			copy(l.buf.data[:], buf.data[:])
			buf.bdev_refdown("addlog1")
			buf.s += "-addlog"
			return
		}
	}

	// copy the data into lb's buf, because caller may modify it in a
	// later transaction.  XXX should use COW.

	lhead := log.lhead
	lb := bdev_block_new(log.logstart+lhead+1, "addlog")
	copy(lb.data[:], buf.data[:])

	if lhead >= len(log.log) {
		panic("log overflow")
	}
	log.log[lhead] = log_entry_t{buf.block, lb}
	log.lhead++

	buf.bdev_refdown("addlog2")
	buf.s += "-addlog"
}

func (log *log_t) commit() {
	if log.lhead == 0 {
		// nothing to commit
		return
	}

	lh := logheader_t{log.tmpblk.data}
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		// install log destination in the first log block
		lh.w_logdest(i, l.block)

		// write block into log
		bdev_write_async(l.buf)
	}
	
	bdev_flush()   // flush log

	lh.w_recovernum(log.lhead)

	// commit log: flush log header
	bdev_write(log.tmpblk)

	bdev_flush()   // commit log

	// rn := lh.recovernum()
	// if rn > 0 {
	// 	runtime.Crash()
	// }

	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		b := bdev_block_new_pa(l.block, "logapply", l.buf.pa)
		bdev_write_async(b)
		b.bdev_refdown("logapply1")
		l.buf.bdev_refdown("logapply2")
	}

	bdev_flush()  // flush apply
	
	// success; clear flag indicating to recover from log
	lh.w_recovernum(0)
	bdev_write(log.tmpblk)
	
	bdev_flush()  // flush cleared commit
	log.lhead = 0
}

func log_daemon(l *log_t) {
	// an upperbound on the number of blocks written per system call. this
	// is necessary in order to guarantee that the log is long enough for
	// the allowed number of concurrent fs syscalls.
	maxblkspersys := 10
	loggen := uint8(0)
	for {
		tickets := (l.loglen - l.lhead)/ maxblkspersys
		adm := l.admission

		done := false
		given := 0
		t := 0
		waiters := 0

                if l.loglen-l.lhead < maxblkspersys {
                       panic("must flush. not enough space left")
                }
		
		for !done {
			select {
			case nb := <- l.incoming:
				if t <= 0 {
					panic("log write without admission")
				}
				l.addlog(nb)
				l.incgen <- loggen
			case <- l.done:
				t--
				if t == 0 {
					done = true
				}
			case adm <- true:
				given++
				t++
				if given == tickets {
					adm = nil
				}
			case <- l.force:
				waiters++
				adm = nil
				if t == 0 {
					done = true
				}
			case lr := <- l.logread:
				if lr.loggen != loggen {
					ret := logread_t{}
					ret.had, ret.gencommit = false, true
					l.logreadret <- ret
					continue
				}
				want := lr.buf.block
				had := false
				for _, lblk := range l.log {
					if lblk.buf.block == want {
						copy(lr.buf.data[:], lblk.buf.data[:])
						had = true
						break
					}
				}
				l.logreadret <- logread_t{had: had}
			}
		}
		// XXX because writei doesn't break big writes up in smaller transactions
		// commit every transaction, instead of inside the if statement below
		l.commit()
		loggen++
		if waiters > 0 || l.loglen-l.lhead < maxblkspersys {
			fmt.Printf("commit %v %v\n", l.lhead, l.loglen)
			// l.commit()   // XX make asynchrounous
			// loggen++
			// wake up waiters
			go func() {
				for i := 0; i < waiters; i++ {
					l.commitwait <- true
				}
			}()
		}
	}
}

func op_begin() {
	if memtime {
		return
	}
	<- fslog.admission
}

func op_end() {
	if memtime {
		return
	}
	fslog.done <- true
}


// returns whether the log contained the requested block and whether the log
// identified by loggen has been committed (i.e. there is no need to check the
// log for blocks).
func log_read(loggen uint8, b *bdev_block_t) (bool, bool) {
	if memtime {
		return false, true
	}
	lr := logread_t{loggen: loggen, buf: b}
	fslog.logread <- lr
	ret := <- fslog.logreadret
	if ret.had && ret.gencommit {
		panic("bad state")
	}
	return ret.had, ret.gencommit
}


// log_write increments ref so that the log has always a valid ref to the buf's page
// the logging layer refdowns when it it is done with the page
func (b *bdev_block_t) log_write() uint8 {
	if memtime {
		return 0
	}
	b.bdev_refup("log_write")
	fslog.incoming <- *b
	return <- fslog.incgen
}
