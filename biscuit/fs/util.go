package fs

import "sync/atomic"
import "unsafe"

// syscall.go
type err_t int

// fsrb.go
type rbc_t int

// bdev
type pa_t	uintptr
type pmap_t	[512]pa_t
type pg_t	[512]int
type bytepg_t	[PGSIZE]uint8
const PGSIZE     int = 1 << 12

// main.go
type fd_t struct {
	// fops is an interface implemented via a "pointer receiver", thus fops
	// is a reference, not a value
	fops	fdops_i
	perms	int
}

type proc_t struct {
	pid		int
}

type userio_i interface {
	// copy src to user memory
	uiowrite(src []uint8) (int, err_t)
	// copy user memory to dst
	uioread(dst []uint8) (int, err_t)
	// returns the number of unwritten/unread bytes remaining
	remain() int
	// the total buffer size
	totalsz() int
}

type fdops_i interface {
	// fd ops
	close() err_t
	fstat(*stat_t) err_t
	lseek(int, int) (int, err_t)
	mmapi(int, int, bool) ([]mmapinfo_t, err_t)
	pathi() inum_t
	read(*proc_t, userio_i) (int, err_t)
	// reopen() is called with proc_t.fdl is held
	reopen() err_t
	write(*proc_t, userio_i) (int, err_t)
	fullpath() (string, err_t)
	truncate(uint) err_t

	pread(userio_i, int) (int, err_t)
	pwrite(userio_i, int) (int, err_t)

	// socket ops
	// returns fops of new fd, size of connector's address written to user
	// space, and error
	accept(*proc_t, userio_i) (fdops_i, int, err_t)
	bind(*proc_t, []uint8) err_t
	connect(*proc_t, []uint8) err_t
	// listen changes the underlying socket type; thus is returns the new
	// fops.
	listen(*proc_t, int) (fdops_i, err_t)
	sendmsg(p *proc_t, data userio_i, saddr []uint8, cmsg []uint8,
	    flags int) (int, err_t)
	// returns number of bytes read, size of from sock address written,
	// size of ancillary data written, msghdr flags, and error. if no from
	// address or ancillary data is requested, the userio objects have
	// length 0.
	recvmsg(p *proc_t, data userio_i, saddr userio_i,
	    cmsg userio_i, flags int) (int, int, int, msgfl_t, err_t)

	// for poll/select
	// returns the current ready flags. pollone() will only cause the
	// device to send a notification if none of the states being polled are
	// currently true.
	pollone(pollmsg_t) (ready_t, err_t)

	fcntl(*proc_t, int, int) int
	getsockopt(*proc_t, int, userio_i, int) (int, err_t)
	setsockopt(*proc_t, int, int, userio_i, int) err_t
	shutdown(rdone, wdone bool) err_t
}

type stat_t struct {
	_dev	uint
	_ino	uint
	_mode	uint
	_size	uint
	_rdev	uint
	_uid	uint
	_blocks	uint
	_m_sec	uint
	_m_nsec	uint
}

type mmapinfo_t struct {
	pg	*pg_t
	phys	pa_t
}

type msgfl_t uint
type ready_t uint8

type pollmsg_t struct {
	notif	chan bool
	events	ready_t
	dowait	bool
	tid	tid_t
}
type tid_t int

const(
	D_CONSOLE int	= 1
	// UNIX domain sockets
	D_SUD 		= 2
	D_SUS 		= 3
	D_DEVNULL	= 4
	D_RAWDISK	= 5
	D_STAT          = 6
	D_FIRST		= D_CONSOLE
	D_LAST		= D_SUS
)

const(
	EPERM		err_t = 1
	ENOENT		err_t = 2
	ESRCH		err_t = 3
	EINTR		err_t = 4
	EIO		err_t = 5
	E2BIG		err_t = 7
	EBADF		err_t = 9
	ECHILD		err_t = 10
	EAGAIN		err_t = 11
	EWOULDBLOCK	= EAGAIN
	ENOMEM		err_t = 12
	EACCES		err_t = 13
	EFAULT		err_t = 14
	EBUSY		err_t = 16
	EEXIST		err_t = 17
	ENODEV		err_t = 19
	ENOTDIR		err_t = 20
	EISDIR		err_t = 21
	EINVAL		err_t = 22
	EMFILE		err_t = 24
	ENOSPC		err_t = 28
	ESPIPE		err_t = 29
	EPIPE		err_t = 32
	ERANGE		err_t = 34
	ENAMETOOLONG	err_t = 36
	ENOSYS		err_t = 38
	ENOTEMPTY	err_t = 39
	EDESTADDRREQ	err_t = 40
	EAFNOSUPPORT	err_t = 47
	EADDRINUSE	err_t = 48
	EADDRNOTAVAIL	err_t = 49
	ENETDOWN	err_t = 50
	ENETUNREACH	err_t = 51
	EHOSTUNREACH	err_t = 65
	ENOTSOCK	err_t = 88
	EMSGSIZE	err_t = 90
	EOPNOTSUPP	err_t = 95
	ECONNRESET	err_t = 104
	EISCONN		err_t = 106
	ENOTCONN	err_t = 107
	ETIMEDOUT	err_t = 110
	ECONNREFUSED	err_t = 111
	EINPROGRESS	err_t = 115
)

type fdopt_t uint

func refcnt(p_pg pa_t) int {
	return 0
}


// XXX
func refup(p_pg pa_t) {
}

// XXX
func refdown(p_pg pa_t) bool {
	return false
}

func readn(a []uint8, n int, off int) int {
	p := unsafe.Pointer(&a[off])
	var ret int
	switch n {
	case 8:
		ret = *(*int)(p)
	case 4:
		ret = int(*(*uint32)(p))
	case 2:
		ret = int(*(*uint16)(p))
	case 1:
		ret = int(*(*uint8)(p))
	default:
		panic("no")
	}
	return ret
}

func writen(a []uint8, sz int, off int, val int) {
	p := unsafe.Pointer(&a[off])
	switch sz {
	case 8:
		*(*int)(p) = val
	case 4:
		*(*uint32)(p) = uint32(val)
	case 2:
		*(*uint16)(p) = uint16(val)
	case 1:
		*(*uint8)(p) = uint8(val)
	default:
		panic("no")
	}
}

var lhits=0

type sysatomic_t int64

type syslimit_t struct {
	// protected by proclock
	sysprocs	int
	// proctected by idmonl lock
	vnodes		int
	// proctected by _allfutex lock
	futexes		int
	// proctected by arptbl lock
	arpents		int
	// proctected by routetbl lock
	routes		int
	// per TCP socket tx/rx segments to remember
	tcpsegs		int
	// socks includes pipes and all TCP connections in TIMEWAIT.
	socks		sysatomic_t
	// total cached dirents
	dirents		sysatomic_t
	// total pipes
	pipes		sysatomic_t
	// additional memory filesystem per-page objects; each file gets one
	// freebie.
	mfspgs		sysatomic_t
	// shared buffer space
	//shared		sysatomic_t
	// bdev blocks
	blocks          int
}

var syslimit = syslimit_t {
	sysprocs:	1e4,
	futexes:	1024,
	arpents:	1024,
	routes:		32,
	tcpsegs:	16,
	socks:		1e5,
	vnodes:		10000, // 1e6,
	dirents:	1 << 20,
	pipes:		1e4,
	// 8GB of block pages
        blocks:         1000, // 100000, // 1 << 21,
}

const(
    SEEK_SET      = 0x1
    SEEK_CUR      = 0x2
	SEEK_END      = 0x4
)

func (st *stat_t) wdev(v uint) {
	st._dev = v
}

func (st *stat_t) wino(v uint) {
	st._ino = v
}

func (st *stat_t) wmode(v uint) {
	st._mode = v
}

func (st *stat_t) wsize(v uint) {
	st._size = v
}

func (st *stat_t) wrdev(v uint) {
	st._rdev = v
}

func (st *stat_t) mode() uint {
	return st._mode
}

func (st *stat_t) size() uint {
	return st._size
}

func (st *stat_t) rdev() uint {
	return st._rdev
}

const(
	R_READ 	ready_t	= 1 << iota
	R_WRITE	ready_t	= 1 << iota
	R_ERROR	ready_t	= 1 << iota
	R_HUP	ready_t	= 1 << iota
)

const(
    O_RDONLY      fdopt_t = 0
    O_WRONLY      fdopt_t = 1
    O_RDWR        fdopt_t = 2
    O_CREAT       fdopt_t = 0x40
    O_EXCL        fdopt_t = 0x80
    O_TRUNC       fdopt_t = 0x200
    O_APPEND      fdopt_t = 0x400
    O_NONBLOCK    fdopt_t = 0x800
    O_DIRECTORY   fdopt_t = 0x10000
    O_CLOEXEC     fdopt_t = 0x80000
)

var stats_string = ""

const (
	RED	rbc_t = iota
	BLACK	rbc_t = iota
)

func (s *sysatomic_t) _aptr() *int64 {
	return (*int64)(unsafe.Pointer(s))
}

func (s *sysatomic_t) given(_n uint) {
	n := int64(_n)
	if n < 0 {
		panic("too mighty")
	}
	atomic.AddInt64(s._aptr(), n)
}

func (s *sysatomic_t) taken(_n uint) bool {
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


func rounddown(v int, b int) int {
	return v - (v % b)
}

func roundup(v int, b int) int {
	return rounddown(v + b - 1, b)
}

func mkdev(_maj, _min int) uint {
	maj := uint(_maj)
	min := uint(_min)
	if min > 0xff {
		panic("bad minor")
	}
	m := maj << 8 | min
	return uint(m << 32)
}
