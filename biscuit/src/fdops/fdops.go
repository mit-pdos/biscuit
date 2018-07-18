package fdops

import "time"

import "defs"
import "mem"
import "stat"
import "tinfo"

type Ready_t uint8

const (
	R_READ  Ready_t = 1 << iota
	R_WRITE Ready_t = 1 << iota
	R_ERROR Ready_t = 1 << iota
	R_HUP   Ready_t = 1 << iota
)

// interface for reading/writing from user space memory either via a pointer
// and length or an array of pointers and lengths (iovec)
type Userio_i interface {
	// copy src to user memory
	Uiowrite(src []uint8) (int, defs.Err_t)
	// copy user memory to dst
	Uioread(dst []uint8) (int, defs.Err_t)
	// returns the number of unwritten/unread bytes remaining
	Remain() int
	// the total buffer size
	Totalsz() int
}

type Fdops_i interface {
	// fd ops
	Close() defs.Err_t
	Fstat(*stat.Stat_t) defs.Err_t
	Lseek(int, int) (int, defs.Err_t)
	Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t)
	Pathi() defs.Inum_t
	Read(Userio_i) (int, defs.Err_t)
	// reopen() is called with Proc_t.fdl is held
	Reopen() defs.Err_t
	Write(Userio_i) (int, defs.Err_t)
	Truncate(uint) defs.Err_t

	Pread(Userio_i, int) (int, defs.Err_t)
	Pwrite(Userio_i, int) (int, defs.Err_t)

	// socket ops
	// returns fops of new fd, size of connector's address written to user
	// space, and error
	Accept(Userio_i) (Fdops_i, int, defs.Err_t)
	Bind([]uint8) defs.Err_t
	Connect([]uint8) defs.Err_t
	// listen changes the underlying socket type; thus is returns the new
	// fops.
	Listen(int) (Fdops_i, defs.Err_t)
	Sendmsg(data Userio_i, saddr []uint8, cmsg []uint8,
		flags int) (int, defs.Err_t)
	// returns number of bytes read, size of from sock address written,
	// size of ancillary data written, msghdr flags, and error. if no from
	// address or ancillary data is requested, the userio objects have
	// length 0.
	Recvmsg(data Userio_i, saddr Userio_i,
		cmsg Userio_i, flags int) (int, int, int, defs.Msgfl_t, defs.Err_t)

	// for poll/select
	// returns the current ready flags. pollone() will only cause the
	// device to send a notification if none of the states being polled are
	// currently true.
	Pollone(Pollmsg_t) (Ready_t, defs.Err_t)

	Fcntl(int, int) int
	Getsockopt(int, Userio_i, int) (int, defs.Err_t)
	Setsockopt(int, int, Userio_i, int) defs.Err_t
	Shutdown(rdone, wdone bool) defs.Err_t
}

type Pollmsg_t struct {
	notif  chan bool
	Events Ready_t
	Dowait bool
	tid    defs.Tid_t
}

func (pm *Pollmsg_t) Pm_set(tid defs.Tid_t, events Ready_t, dowait bool) {
	if pm.notif == nil {
		// 1-element buffered channel; that way devices can send
		// notifies on the channel asynchronously without blocking.
		pm.notif = make(chan bool, 1)
	}
	pm.Events = events
	pm.Dowait = dowait
	pm.tid = tid
}

// returns whether we timed out, and error
func (pm *Pollmsg_t) Pm_wait(to int) (bool, defs.Err_t) {
	var tochan <-chan time.Time
	if to != -1 {
		tochan = time.After(time.Duration(to) * time.Millisecond)
	}
	kn := &tinfo.Current().Killnaps

	var timeout bool
	select {
	case <-pm.notif:
	case <-tochan:
		timeout = true
	case <-kn.Killch:
		if kn.Kerr == 0 {
			panic("eh?")
		}
		return false, kn.Kerr
	}
	return timeout, 0
}

// keeps track of all outstanding pollers. used by devices supporting poll(2)
type Pollers_t struct {
	allmask Ready_t
	waiters []Pollmsg_t
}

// returns tid pollmsg and empty pollmsg
func (p *Pollers_t) _find(tid defs.Tid_t, empty bool) (*Pollmsg_t, *Pollmsg_t) {
	var eret *Pollmsg_t
	for i := range p.waiters {
		if p.waiters[i].tid == tid {
			return &p.waiters[i], eret
		}
		if empty && eret == nil && p.waiters[i].tid == 0 {
			eret = &p.waiters[i]
		}
	}
	return nil, eret
}

func (p *Pollers_t) _findempty() *Pollmsg_t {
	_, e := p._find(-1, true)
	return e
}

var lhits int

func (p *Pollers_t) Addpoller(pm *Pollmsg_t) defs.Err_t {
	if p.waiters == nil {
		p.waiters = make([]Pollmsg_t, 10)
	}
	p.allmask |= pm.Events
	t, e := p._find(pm.tid, true)
	if t != nil {
		*t = *pm
	} else if e != nil {
		*e = *pm
	} else {
		lhits++
		return -defs.ENOMEM
	}
	return 0
}

func (p *Pollers_t) Wakeready(r Ready_t) {
	if p.allmask&r == 0 {
		return
	}
	var newallmask Ready_t
	for i := 0; i < len(p.waiters); i++ {
		pm := p.waiters[i]
		if pm.Events&r != 0 {
			// found a waiter
			pm.Events &= r
			// non-blocking send on a 1-element buffered channel
			select {
			case pm.notif <- true:
			default:
			}
			p.waiters[i].tid = 0
		} else {
			newallmask |= pm.Events
		}
	}
	p.allmask = newallmask
}
