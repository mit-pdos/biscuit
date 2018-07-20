package defs

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
	PROF_HACK6       = 1 << 9
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
	SINFO_DOGC       = 10
	SINFO_PROCLIST   = 11
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

func Mkexitsig(sig int) int {
	if sig < 0 || sig > 32 {
		panic("bad sig")
	}
	return sig << SIGSHIFT
}
