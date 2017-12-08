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
	incoming	chan *bdev_block_t
	admission	chan bool
	done		chan bool
	force		chan bool
	commitwait	chan bool
	headblk		*bdev_block_t
}

func (log *log_t) init(ls int, ll int) {
	log.lhead = 0
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = ll - 1
	log.log = make([]log_entry_t, log.loglen)
	log.headblk = bdev_block_new(log.logstart, "logstart")
	log.incoming = make(chan *bdev_block_t)
	log.admission = make(chan bool)
	log.done = make(chan bool)
	log.force = make(chan bool)
	log.commitwait = make(chan bool)
}

func (log *log_t) addlog(buf *bdev_block_t) {
	// log absorption
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		if l.block == buf.block {
			if l.buf != buf {
				panic("absorption")
			}
			// buffer is already in log and pinned. if the write of
			// this block is in a later transaction, we know this
			// later transaction will commit with the one that
			// modified this block earlier.
			return
		}
	}

	lhead := log.lhead
	if lhead >= len(log.log) {
		panic("log overflow")
	}

	// No need to copy because later transactions who read the modified
	// block will commmit with this transaction.  If log commits, log stops
	// accepting transactions until commit has completed, which will clean
	// the log.
	log.log[lhead] = log_entry_t{buf.block, buf}
	log.lhead++

	buf.pin()
	buf.s += "-addlog"
}

func (log *log_t) commit() {
	if log.lhead == 0 {
		// nothing to commit
		return
	}

	lh := logheader_t{log.headblk.data}
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		// install log destination in the first log block
		lh.w_logdest(i, l.block)

		// write block into log
                b := bdev_get_nofill(log.logstart+i+1, "logapply")
		copy(b.data[:], l.buf.data[:])
		b.relse()
		b.bdev_write_async()
		b.bdev_refdown("writelog")

	}
	
	bdev_flush()   // flush log

	lh.w_recovernum(log.lhead)

	// write log header
	log.headblk.bdev_write()

	bdev_flush()   // commit log header

	// rn := lh.recovernum()
	// if rn > 0 {
	// 	runtime.Crash()
	// }

	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		l.buf.bdev_write_async()
		l.buf.bdev_refdown("logapply")
	}

	bdev_flush()  // flush apply
	
	// success; clear flag indicating to recover from log
	lh.w_recovernum(0)
	log.headblk.bdev_write()
	
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


// log_write increments ref so that the log has always a valid ref to the buf's
// page the logging layer refdowns when it it is done with the page.  the caller
// of log_write shouldn't hold *any* buf locks.
func (b *bdev_block_t) log_write() {
	if memtime {
		return
	}
	b.bdev_refup("log_write")
	fslog.incoming <- b
}
