package common

import "sync"

const(
	EPERM		Err_t = 1
	ENOENT		Err_t = 2
	ESRCH		Err_t = 3
	EINTR		Err_t = 4
	EIO		Err_t = 5
	E2BIG		Err_t = 7
	EBADF		Err_t = 9
	ECHILD		Err_t = 10
	EAGAIN		Err_t = 11
	EWOULDBLOCK	= EAGAIN
	ENOMEM		Err_t = 12
	EACCES		Err_t = 13
	EFAULT		Err_t = 14
	EBUSY		Err_t = 16
	EEXIST		Err_t = 17
	ENODEV		Err_t = 19
	ENOTDIR		Err_t = 20
	EISDIR		Err_t = 21
	EINVAL		Err_t = 22
	EMFILE		Err_t = 24
	ENOSPC		Err_t = 28
	ESPIPE		Err_t = 29
	EPIPE		Err_t = 32
	ERANGE		Err_t = 34
	ENAMETOOLONG	Err_t = 36
	ENOSYS		Err_t = 38
	ENOTEMPTY	Err_t = 39
	EDESTADDRREQ	Err_t = 40
	EAFNOSUPPORT	Err_t = 47
	EADDRINUSE	Err_t = 48
	EADDRNOTAVAIL	Err_t = 49
	ENETDOWN	Err_t = 50
	ENETUNREACH	Err_t = 51
	EHOSTUNREACH	Err_t = 65
	ENOTSOCK	Err_t = 88
	EMSGSIZE	Err_t = 90
	EOPNOTSUPP	Err_t = 95
	ECONNRESET	Err_t = 104
	EISCONN		Err_t = 106
	ENOTCONN	Err_t = 107
	ETIMEDOUT	Err_t = 110
	ECONNREFUSED	Err_t = 111
	EINPROGRESS	Err_t = 115
)

const(
    SEEK_SET      = 0x1
    SEEK_CUR      = 0x2
    SEEK_END      = 0x4
)

const PGSIZE     int = 1 << 12
type Pa_t	uintptr
type Bytepg_t	[PGSIZE]uint8

type Err_t      int

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

type Fdopt_t uint

const(
    O_RDONLY      Fdopt_t = 0
    O_WRONLY      Fdopt_t = 1
    O_RDWR        Fdopt_t = 2
    O_CREAT       Fdopt_t = 0x40
    O_EXCL        Fdopt_t = 0x80
    O_TRUNC       Fdopt_t = 0x200
    O_APPEND      Fdopt_t = 0x400
    O_NONBLOCK    Fdopt_t = 0x800
    O_DIRECTORY   Fdopt_t = 0x10000
    O_CLOEXEC     Fdopt_t = 0x80000
)

type Fd_t struct {
	// fops is an interface implemented via a "pointer receiver", thus fops
	// is a reference, not a value
	Fops	Fdops_i
	perms	int
}

type Msgfl_t uint
type Ready_t uint8

const(
	R_READ 	Ready_t	= 1 << iota
	R_WRITE	Ready_t	= 1 << iota
	R_ERROR	Ready_t	= 1 << iota
	R_HUP	Ready_t	= 1 << iota
)

type Pollmsg_t struct {
	notif	chan bool
	Events	Ready_t
	dowait	bool
	tid	tid_t
}

type Inum_t int

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

type Userio_i interface {
	// copy src to user memory
	Uiowrite(src []uint8) (int, Err_t)
	// copy user memory to dst
	Uioread(dst []uint8) (int, Err_t)
	// returns the number of unwritten/unread bytes remaining
	Remain() int
	// the total buffer size
	Totalsz() int
}

type Pg_t	[512]int

type Mmapinfo_t struct {
	Pg	*Pg_t
	Phys	Pa_t
}

type mtype_t uint

type mfile_t struct {
	mfops		Fdops_i
	// once mapcount is 0, close mfops
	mapcount	int
}

type Vminfo_t struct {
	mtype	mtype_t
	pgn	uintptr
	pglen	int
	perms	uint
	file struct {
		foff	int
		mfile	*mfile_t
		shared	bool
	}
	pch	[]Pa_t
}

type Vmregion_t struct {
	rb	Rbh_t
	_pglen	int
	novma	uint
	hole struct {
		startn	uintptr
		pglen	uintptr
	}
}

type Tnote_t struct {
	alive	bool
}

type Threadinfo_t struct {
	notes	map[tid_t]*Tnote_t
	sync.Mutex
}

type tid_t int
type pmap_t	[512]Pa_t


type Proc_t struct {
	pid		int
	// first thread id
	tid0		tid_t
	name		string

	// waitinfo for my child processes
	mywait		Wait_t
	// waitinfo of my parent
	pwait		*Wait_t

	// thread tids of this process
	threadi		Threadinfo_t

	// lock for vmregion, pmpages, pmap, and p_pmap
	pgfl		sync.Mutex

	vmregion	Vmregion_t

	// pmap pages
	pmap		*pmap_t
	p_pmap		Pa_t

	// mmap next virtual address hint
	mmapi		int

	// a process is marked doomed when it has been killed but may have
	// threads currently running on another processor
	pgfltaken	bool
	doomed		bool
	exitstatus	int

	fds		[]*Fd_t
	// where to start scanning for free fds
	fdstart		int
	// fds, fdstart, nfds protected by fdl
	fdl		sync.Mutex
	// number of valid file descriptors
	nfds		int

	cwd		*Fd_t
	// to serialize chdirs
	cwdl		sync.Mutex
	ulim		ulimit_t

	// this proc's rusage
	atime		Accnt_t
	// total child rusage
	catime		Accnt_t
}

// per-process limits
type ulimit_t struct {
	pages	int
	nofile	uint
	novma	uint
	noproc	uint
}

type Accnt_t struct {
	// nanoseconds
	userns		int64
	sysns		int64
	// for getting consistent snapshot of both times; not always needed
	sync.Mutex
}




