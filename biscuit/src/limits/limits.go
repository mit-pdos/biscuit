package limits

import "unsafe"
import "sync/atomic"

var Lhits int

type Sysatomic_t int64

type Syslimit_t struct {
	// protected by proclock
	Sysprocs int
	// proctected by idmonl lock
	Vnodes int
	// proctected by _allfutex lock
	Futexes int
	// proctected by arptbl lock
	Arpents int
	// proctected by routetbl lock
	Routes int
	// per TCP socket tx/rx segments to remember
	Tcpsegs int
	// socks includes pipes and all TCP connections in TIMEWAIT.
	Socks Sysatomic_t
	// total cached dirents
	// total pipes
	Pipes Sysatomic_t
	// additional memory filesystem per-page objects; each file gets one
	// freebie.
	Mfspgs Sysatomic_t
	// shared buffer space
	//shared		Sysatomic_t
	// bdev blocks
	Blocks int
}

var Syslimit *Syslimit_t = MkSysLimit()

func MkSysLimit() *Syslimit_t {
	return &Syslimit_t{
		Sysprocs: 1e4,
		Futexes:  1024,
		Arpents:  1024,
		Routes:   32,
		Tcpsegs:  16,
		Socks:    1e5,
		Vnodes:   20000, // 1e6,
		Pipes:    1e4,
		// 8GB of block pages
		Blocks: 100000, // 1 << 21,
	}
}

func (s *Sysatomic_t) _aptr() *int64 {
	return (*int64)(unsafe.Pointer(s))
}

func (s *Sysatomic_t) Given(_n uint) {
	n := int64(_n)
	if n < 0 {
		panic("too mighty")
	}
	atomic.AddInt64(s._aptr(), n)
}

func (s *Sysatomic_t) Taken(_n uint) bool {
	n := int64(_n)
	if n < 0 {
		panic("too mighty")
	}
	g := atomic.AddInt64(s._aptr(), -n)
	if g >= 0 {
		return true
	}
	atomic.AddInt64(s._aptr(), n)
	return false
}

// returns false if the limit has been reached.
func (s *Sysatomic_t) Take() bool {
	return s.Taken(1)
}

func (s *Sysatomic_t) Give() {
	s.Given(1)
}
