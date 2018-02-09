package common

import "time"

const(
	FD_READ		= 0x1
	FD_WRITE	= 0x2
	FD_CLOEXEC	= 0x4
)

type Fd_t struct {
	// fops is an interface implemented via a "pointer receiver", thus fops
	// is a reference, not a value
	Fops	Fdops_i
	Perms	int
}

func Copyfd(fd *Fd_t) (*Fd_t, Err_t) {
	nfd := &Fd_t{}
	*nfd = *fd
	err := nfd.Fops.Reopen()
	if err != 0 {
		return nil, err
	}
	return nfd, 0
}

func Close_panic(f *Fd_t) {
	if f.Fops.Close() != 0 {
		panic("must succeed")
	}
}

type Inum_t int

type Ready_t uint8

const(
	R_READ 	Ready_t	= 1 << iota
	R_WRITE	Ready_t	= 1 << iota
	R_ERROR	Ready_t	= 1 << iota
	R_HUP	Ready_t	= 1 << iota
)

type Fdops_i interface {
	// fd ops
	Close() Err_t
	Fstat(*Stat_t) Err_t
	Lseek(int, int) (int, Err_t)
	Mmapi(int, int, bool) ([]Mmapinfo_t, Err_t)
	Pathi() Inum_t
	Read(*Proc_t, Userio_i) (int, Err_t)
	// reopen() is called with Proc_t.fdl is held
	Reopen() Err_t
	Write(*Proc_t, Userio_i) (int, Err_t)
	Fullpath() (string, Err_t)
	Truncate(uint) Err_t

	Pread(Userio_i, int) (int, Err_t)
	Pwrite(Userio_i, int) (int, Err_t)

	// socket ops
	// returns fops of new fd, size of connector's address written to user
	// space, and error
	Accept(*Proc_t, Userio_i) (Fdops_i, int, Err_t)
	Bind(*Proc_t, []uint8) Err_t
	Connect(*Proc_t, []uint8) Err_t
	// listen changes the underlying socket type; thus is returns the new
	// fops.
	Listen(*Proc_t, int) (Fdops_i, Err_t)
	Sendmsg(p *Proc_t, data Userio_i, saddr []uint8, cmsg []uint8,
	    flags int) (int, Err_t)
	// returns number of bytes read, size of from sock address written,
	// size of ancillary data written, msghdr flags, and error. if no from
	// address or ancillary data is requested, the userio objects have
	// length 0.
	Recvmsg(p *Proc_t, data Userio_i, saddr Userio_i,
	    cmsg Userio_i, flags int) (int, int, int, Msgfl_t, Err_t)

	// for poll/select
	// returns the current ready flags. pollone() will only cause the
	// device to send a notification if none of the states being polled are
	// currently true.
	Pollone(Pollmsg_t) (Ready_t, Err_t)

	Fcntl(*Proc_t, int, int) int
	Getsockopt(*Proc_t, int, Userio_i, int) (int, Err_t)
	Setsockopt(*Proc_t, int, int, Userio_i, int) Err_t
	Shutdown(rdone, wdone bool) Err_t
}


type Pollmsg_t struct {
	notif	chan bool
	Events	Ready_t
	Dowait	bool
	tid	Tid_t
}

func (pm *Pollmsg_t) Pm_set(tid Tid_t, events Ready_t, dowait bool) {
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
func (pm *Pollmsg_t) Pm_wait(to int) (bool, Err_t) {
	var tochan <-chan time.Time
	if to != -1 {
		tochan = time.After(time.Duration(to)*time.Millisecond)
	}
	var timeout bool
	select {
	case <- pm.notif:
	case <- tochan:
		timeout = true
	}
	return timeout, 0
}

// keeps track of all outstanding pollers. used by devices supporting poll(2)
type Pollers_t struct {
	allmask		Ready_t
	waiters		[]Pollmsg_t
}

// returns tid pollmsg and empty pollmsg
func (p *Pollers_t) _find(tid Tid_t, empty bool) (*Pollmsg_t, *Pollmsg_t) {
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

func (p *Pollers_t) Addpoller(pm *Pollmsg_t) Err_t {
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
		return -ENOMEM
	}
	return 0
}

func (p *Pollers_t) Wakeready(r Ready_t) {
	if p.allmask & r == 0 {
		return
	}
	var newallmask Ready_t
	for i := 0; i < len(p.waiters); i++ {
		pm := p.waiters[i]
		if pm.Events & r != 0 {
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
