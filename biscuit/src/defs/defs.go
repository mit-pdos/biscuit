package defs

const (
	EPERM         Err_t = 1
	ENOENT        Err_t = 2
	ESRCH         Err_t = 3
	EINTR         Err_t = 4
	EIO           Err_t = 5
	E2BIG         Err_t = 7
	EBADF         Err_t = 9
	ECHILD        Err_t = 10
	EAGAIN        Err_t = 11
	EWOULDBLOCK         = EAGAIN
	ENOMEM        Err_t = 12
	EACCES        Err_t = 13
	EFAULT        Err_t = 14
	EBUSY         Err_t = 16
	EEXIST        Err_t = 17
	ENODEV        Err_t = 19
	ENOTDIR       Err_t = 20
	EISDIR        Err_t = 21
	EINVAL        Err_t = 22
	EMFILE        Err_t = 24
	ENOSPC        Err_t = 28
	ESPIPE        Err_t = 29
	EPIPE         Err_t = 32
	ERANGE        Err_t = 34
	ENAMETOOLONG  Err_t = 36
	ENOSYS        Err_t = 38
	ENOTEMPTY     Err_t = 39
	EDESTADDRREQ  Err_t = 40
	EAFNOSUPPORT  Err_t = 47
	EADDRINUSE    Err_t = 48
	EADDRNOTAVAIL Err_t = 49
	ENETDOWN      Err_t = 50
	ENETUNREACH   Err_t = 51
	EHOSTUNREACH  Err_t = 65
	ENOTSOCK      Err_t = 88
	EMSGSIZE      Err_t = 90
	EOPNOTSUPP    Err_t = 95
	ECONNRESET    Err_t = 104
	EISCONN       Err_t = 106
	ENOTCONN      Err_t = 107
	ETIMEDOUT     Err_t = 110
	ECONNREFUSED  Err_t = 111
	EINPROGRESS   Err_t = 115
	ENOHEAP       Err_t = 511
)

type Err_t int

const (
	D_CONSOLE int = 1
	// UNIX domain sockets
	D_SUD     = 2
	D_SUS     = 3
	D_DEVNULL = 4
	D_RAWDISK = 5
	D_STAT    = 6
	D_PROF    = 7
	D_FIRST   = D_CONSOLE
	D_LAST    = D_SUS
)

const (
	DIVZERO  = 0
	UD       = 6
	GPFAULT  = 13
	PGFAULT  = 14
	TIMER    = 32
	SYSCALL  = 64
	TLBSHOOT = 70
	PERFMASK = 72

	IRQ_BASE = 32

	IRQ_KBD  = 1
	IRQ_COM1 = 4
	INT_KBD  = IRQ_BASE + IRQ_KBD
	INT_COM1 = IRQ_BASE + IRQ_COM1

	INT_MSI0 = 56
	INT_MSI1 = 57
	INT_MSI2 = 58
	INT_MSI3 = 59
	INT_MSI4 = 60
	INT_MSI5 = 61
	INT_MSI6 = 62
	INT_MSI7 = 63
)

type Inum_t int

type Tid_t int

type Msgfl_t uint

type Fdopt_t uint

const (
	SYS_READ            = 0
	SYS_WRITE           = 1
	SYS_OPEN            = 2
	O_RDONLY    Fdopt_t = 0
	O_WRONLY    Fdopt_t = 1
	O_RDWR      Fdopt_t = 2
	O_CREAT     Fdopt_t = 0x40
	O_EXCL      Fdopt_t = 0x80
	O_TRUNC     Fdopt_t = 0x200
	O_APPEND    Fdopt_t = 0x400
	O_NONBLOCK  Fdopt_t = 0x800
	O_DIRECTORY Fdopt_t = 0x10000
	O_CLOEXEC   Fdopt_t = 0x80000
	SYS_CLOSE           = 3
	SYS_STAT            = 4
	SYS_FSTAT           = 5
	SYS_POLL            = 7
	POLLRDNORM          = 0x1
	POLLRDBAND          = 0x2
	POLLIN              = (POLLRDNORM | POLLRDBAND)
	POLLPRI             = 0x4
	POLLWRNORM          = 0x8
	POLLOUT             = POLLWRNORM
	POLLWRBAND          = 0x10
	POLLERR             = 0x20
	POLLHUP             = 0x40
	POLLNVAL            = 0x80
	SYS_LSEEK           = 8
	SEEK_SET            = 0x1
	SEEK_CUR            = 0x2
	SEEK_END            = 0x4
	SYS_MMAP            = 9
	MAP_SHARED          = uint(0x1)
	MAP_PRIVATE         = uint(0x2)
	MAP_FIXED           = 0x10
	MAP_ANON            = 0x20
	MAP_FAILED          = -1
	PROT_NONE           = 0x0
	PROT_READ           = 0x1
	PROT_WRITE          = 0x2
	PROT_EXEC           = 0x4
	SYS_MUNMAP          = 11
	SYS_SIGACT          = 13
	SYS_READV           = 19
	SYS_WRITEV          = 20
	SYS_ACCESS          = 21
	SYS_DUP2            = 33
	SYS_PAUSE           = 34
	SYS_GETPID          = 39
	SYS_GETPPID         = 40
	SYS_SOCKET          = 41
	// domains
	AF_UNIX = 1
	AF_INET = 2
	// types
	SOCK_STREAM            = 1 << 0
	SOCK_DGRAM             = 1 << 1
	SOCK_RAW               = 1 << 2
	SOCK_SEQPACKET         = 1 << 3
	SOCK_CLOEXEC           = 1 << 4
	SOCK_NONBLOCK          = 1 << 5
	SYS_CONNECT            = 42
	SYS_ACCEPT             = 43
	SYS_SENDTO             = 44
	SYS_RECVFROM           = 45
	SYS_SOCKPAIR           = 46
	SYS_SHUTDOWN           = 48
	SHUT_WR                = 1 << 0
	SHUT_RD                = 1 << 1
	SYS_BIND               = 49
	INADDR_ANY             = 0
	SYS_LISTEN             = 50
	SYS_RECVMSG            = 51
	MSG_TRUNC      Msgfl_t = 1 << 0
	MSG_CTRUNC     Msgfl_t = 1 << 1
	SYS_SENDMSG            = 52
	SYS_GETSOCKOPT         = 55
	SYS_SETSOCKOPT         = 56
	// socket levels
	SOL_SOCKET = 1
	// socket options
	SO_SNDBUF        = 1
	SO_SNDTIMEO      = 2
	SO_ERROR         = 3
	SO_RCVBUF        = 5
	SO_NAME          = 10
	SO_PEER          = 11
	SYS_FORK         = 57
	FORK_PROCESS     = 0x1
	FORK_THREAD      = 0x2
	SYS_EXECV        = 59
	SYS_EXIT         = 60
	CONTINUED        = 1 << 9
	EXITED           = 1 << 10
	SIGNALED         = 1 << 11
	SIGSHIFT         = 27
	SYS_WAIT4        = 61
	WAIT_ANY         = -1
	WAIT_MYPGRP      = 0
	WCONTINUED       = 1
	WNOHANG          = 2
	WUNTRACED        = 4
	SYS_KILL         = 62
	SYS_FCNTL        = 72
	F_GETFL          = 1
	F_SETFL          = 2
	F_GETFD          = 3
	F_SETFD          = 4
	SYS_TRUNC        = 76
	SYS_FTRUNC       = 77
	SYS_GETCWD       = 79
	SYS_CHDIR        = 80
	SYS_RENAME       = 82
	SYS_MKDIR        = 83
	SYS_LINK         = 86
	SYS_UNLINK       = 87
	SYS_GETTOD       = 96
	SYS_GETRLMT      = 97
	RLIMIT_NOFILE    = 1
	RLIM_INFINITY    = ^uint(0)
	SYS_GETRUSG      = 98
	RUSAGE_SELF      = 1
	RUSAGE_CHILDREN  = 2
	SYS_MKNOD        = 133
	SYS_SETRLMT      = 160
	SYS_SYNC         = 162
	SYS_REBOOT       = 169
	SYS_NANOSLEEP    = 230
	SYS_PIPE2        = 293
	SYS_PROF         = 31337
	PROF_DISABLE     = 1 << 0
	PROF_GOLANG      = 1 << 1
	PROF_SAMPLE      = 1 << 2
	PROF_COUNT       = 1 << 3
	PROF_HACK        = 1 << 4
	PROF_HACK2       = 1 << 5
	PROF_HACK3       = 1 << 6
	PROF_HACK4       = 1 << 7
	PROF_HACK5       = 1 << 8
	SYS_THREXIT      = 31338
	SYS_INFO         = 31339
	SINFO_GCCOUNT    = 0
	SINFO_GCPAUSENS  = 1
	SINFO_GCHEAPSZ   = 2
	SINFO_GCMS       = 4
	SINFO_GCTOTALLOC = 5
	SINFO_GCMARKT    = 6
	SINFO_GCSWEEPT   = 7
	SINFO_GCWBARRT   = 8
	SINFO_GCOBJS     = 9
	SYS_PREAD        = 31340
	SYS_PWRITE       = 31341
	SYS_FUTEX        = 31342
	FUTEX_SLEEP      = 1
	FUTEX_WAKE       = 2
	FUTEX_CNDGIVE    = 3
	SYS_GETTID       = 31343
)

const (
	SIGKILL = 9
)

const (
	TFSIZE    = 24
	TFREGS    = 17
	TF_FSBASE = 1
	TF_R13    = 4
	TF_R12    = 5
	TF_R8     = 9
	TF_RBP    = 10
	TF_RSI    = 11
	TF_RDI    = 12
	TF_RDX    = 13
	TF_RCX    = 14
	TF_RBX    = 15
	TF_RAX    = 16
	TF_TRAP   = TFREGS
	TF_ERROR  = TFREGS + 1
	TF_RIP    = TFREGS + 2
	TF_CS     = TFREGS + 3
	TF_RSP    = TFREGS + 5
	TF_SS     = TFREGS + 6
	TF_RFLAGS = TFREGS + 4
	TF_FL_IF  = 1 << 9
)

func Mkexitsig(sig int) int {
	if sig < 0 || sig > 32 {
		panic("bad sig")
	}
	return sig << SIGSHIFT
}
