package common

import "sync"
import "sync/atomic"
import "time"

type Accnt_t struct {
	// nanoseconds
	userns		int64
	sysns		int64
	// for getting consistent snapshot of both times; not always needed
	sync.Mutex
}

func (a *Accnt_t) utadd(delta int) {
	atomic.AddInt64(&a.userns, int64(delta))
}

func (a *Accnt_t) systadd(delta int) {
	atomic.AddInt64(&a.sysns, int64(delta))
}

func (a *Accnt_t) now() int {
	return int(time.Now().UnixNano())
}

func (a *Accnt_t) io_time(since int) {
	d := a.now() - since
	a.systadd(-d)
}

func (a *Accnt_t) sleep_time(since int) {
	d := a.now() - since
	a.systadd(-d)
}

func (a *Accnt_t) finish(inttime int) {
	a.systadd(a.now() - inttime)
}

func (a *Accnt_t) add(n *Accnt_t) {
	a.Lock()
	a.userns += n.userns
	a.sysns += n.sysns
	a.Unlock()
}

func (a *Accnt_t) fetch() []uint8 {
	a.Lock()
	ru := a.to_rusage()
	a.Unlock()
	return ru
}

func (a *Accnt_t) to_rusage() []uint8 {
	words := 4
	ret := make([]uint8, words*8)
	totv := func(nano int64) (int, int) {
		secs := int(nano/1e9)
		usecs := int((nano%1e9)/1000)
		return secs, usecs
	}
	off := 0
	// user timeval
	s, us := totv(a.userns)
	Writen(ret, 8, off, s)
	off += 8
	Writen(ret, 8, off, us)
	off += 8
	// sys timeval
	s, us = totv(a.sysns)
	Writen(ret, 8, off, s)
	off += 8
	Writen(ret, 8, off, us)
	off += 8
	return ret
}
