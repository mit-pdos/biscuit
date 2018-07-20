package accnt

import "sync"
import "sync/atomic"
import "time"

import "util"

type Accnt_t struct {
	// nanoseconds
	Userns int64
	Sysns  int64
	// for getting consistent snapshot of both times; not always needed
	sync.Mutex
}

func (a *Accnt_t) Utadd(delta int) {
	atomic.AddInt64(&a.Userns, int64(delta))
}

func (a *Accnt_t) Systadd(delta int) {
	atomic.AddInt64(&a.Sysns, int64(delta))
}

func (a *Accnt_t) Now() int {
	return int(time.Now().UnixNano())
}

func (a *Accnt_t) Io_time(since int) {
	d := a.Now() - since
	a.Systadd(-d)
}

func (a *Accnt_t) Sleep_time(since int) {
	d := a.Now() - since
	a.Systadd(-d)
}

func (a *Accnt_t) Finish(inttime int) {
	a.Systadd(a.Now() - inttime)
}

func (a *Accnt_t) Add(n *Accnt_t) {
	a.Lock()
	a.Userns += n.Userns
	a.Sysns += n.Sysns
	a.Unlock()
}

func (a *Accnt_t) Fetch() []uint8 {
	a.Lock()
	ru := a.To_rusage()
	a.Unlock()
	return ru
}

func (a *Accnt_t) To_rusage() []uint8 {
	words := 4
	ret := make([]uint8, words*8)
	totv := func(nano int64) (int, int) {
		secs := int(nano / 1e9)
		usecs := int((nano % 1e9) / 1000)
		return secs, usecs
	}
	off := 0
	// user timeval
	s, us := totv(a.Userns)
	util.Writen(ret, 8, off, s)
	off += 8
	util.Writen(ret, 8, off, us)
	off += 8
	// sys timeval
	s, us = totv(a.Sysns)
	util.Writen(ret, 8, off, s)
	off += 8
	util.Writen(ret, 8, off, us)
	off += 8
	return ret
}
