#pragma once

#define		BISCUIT_RELEASE	"REARDEN"
#define		BISCUIT_VERSION	"0.0.0"

#include <littypes.h>

#ifdef __cplusplus
extern "C" {
#endif

#define		STDIN_FILENO	0
#define		STDOUT_FILENO	1
#define		STDERR_FILENO	2
#define		EOF		(-1)

#define		ERRNO_FIRST	1
#define		EPERM		1
#define		ENOENT		2
#define		ESRCH		3
#define		EINTR		4
#define		EIO		5
#define		E2BIG		7
#define		EBADF		9
#define		ECHILD		10
#define		EAGAIN		11
#define		EWOULDBLOCK	EAGAIN
#define		ENOMEM		12
#define		EACCES		13
#define		EFAULT		14
#define		EBUSY		16
#define		EEXIST		17
#define		EXDEV		18
#define		ENODEV		19
#define		ENOTDIR		20
#define		EISDIR		21
#define		EINVAL		22
#define		ENFILE		23
#define		EMFILE		24
#define		ENOSPC		28
#define		ESPIPE		29
#define		EPIPE		32
#define		ERANGE		34
#define		ENAMETOOLONG	36
#define		ENOSYS		38
#define		ENOTEMPTY	39
#define		EDESTADDRREQ	40
#define		EAFNOSUPPORT	47
#define		EADDRINUSE	48
#define		EADDRNOTAVAIL	49
#define		ENETDOWN	50
#define		ENETUNREACH	51
#define		ECONNABORTED	53
#define		ELOOP		62
#define		EHOSTDOWN	64
#define		EHOSTUNREACH	65
#define		EOVERFLOW	75
#define		ENOTSOCK	88
#define		EOPNOTSUPP	95
#define		ECONNRESET	104
#define		EISCONN		106
#define		ENOTCONN	107
#define		ETIMEDOUT	110
#define		ECONNREFUSED	111
#define		EINPROGRESS	115
#define		ERRNO_LAST	115

#define		MAP_FAILED	((void *)(long) -1)

#define		MAP_SHARED	0x01
#define		MAP_PRIVATE	0x02
#define		MAP_ANON	0x20
#define		MAP_ANONYMOUS	MAP_ANON

#define		PROT_NONE	0x0
#define		PROT_READ	0x1
#define		PROT_WRITE	0x2
#define		PROT_EXEC	0x4

#define		FORK_PROCESS	0x1
#define		FORK_THREAD	0x2

#define		MAXBUF		4096
#define		PIPE_BUF	4096

#define		MAJOR(x)	((long)((ulong)x >> 40))
#define		MINOR(x)	((long)(((ulong)x >> 32) & 0xff))
#define		MKDEV(x, y)	((dev_t)((ulong)x << 40 | (ulong)y << 32))

/*
 * system calls
 */
#define 	FD_WORDS	16
// bits
#define 	FD_SETSIZE	(FD_WORDS*8*8)

typedef struct {
	ulong	mask[FD_WORDS];
} fd_set;

#define 	FD_ZERO(ft)	memset((ft), 0, sizeof(fd_set))
#define 	FD_SET(n, ft)	(ft)->mask[n/64] |= 1ull << (n%64)
#define 	FD_CLR(n, ft)	(ft)->mask[n/64] &= ~(1ull << (n%64))
#define 	FD_ISSET(n, ft)	(((ft)->mask[n/64] & (1ull << (n%64))) != 0)

struct iovec {
	void *iov_base;
	size_t iov_len;
};

struct pollfd {
	int	fd;
#define		POLLRDNORM	0x1
#define		POLLRDBAND	0x2
#define		POLLIN		(POLLRDNORM | POLLRDBAND)
#define		POLLPRI		0x4
#define		POLLWRNORM	0x8
#define		POLLOUT		POLLWRNORM
#define		POLLWRBAND	0x10
// revents only, whose status is always reported
#define		POLLERR		0x20
#define		POLLHUP		0x40
#define		POLLNVAL	0x80
	ushort	events;
	ushort	revents;
};

struct timeval {
	time_t tv_sec;
	time_t tv_usec;
};

struct tm {
	int    tm_sec;
	int    tm_min;
	int    tm_hour;
	int    tm_mday;
	int    tm_mon;
	int    tm_year;
	int    tm_wday;
	int    tm_yday;
	int    tm_isdst;
};

struct timezone {
};

struct timespec {
	time_t tv_sec;
	long tv_nsec;
};

extern long timezone;

struct rlimit {
	rlim_t rlim_cur;
	rlim_t rlim_max;
};

struct rusage {
	struct timeval ru_utime;
	struct timeval ru_stime;
};

union sigval {
	int	sival_int;
	void	*sival_ptr;
};

typedef struct {
	int	si_signo;
	int	si_code;
	int	si_errno;
	pid_t	si_pid;
	uid_t	si_uid;
	void	*si_addr;
	int	si_status;
	long	si_band;
	union	sigval  si_value;
} siginfo_t;

struct sigaction {
	void (*sa_handler)(int);
	void (*sa_sigaction)(int, siginfo_t *, void *);
	sigset_t sa_mask;
#define		sigemptyset(ss)		(*ss = 0)
#define		sigfillset(ss)		(*ss = -1)
#define		sigaddset(ss, s)	(*ss |= (1ull << s))
#define		sigdelset(ss, s)	(*ss &= ~(1ull << s))
#define		sigismember(ss, s)	(*ss & (1ull << s))
	int	sa_flags;
#define		SA_SIGINFO		1
};

struct sockaddr {
	uchar	sa_len;
	uchar	sa_family;
	char	sa_data[];
};

struct sockaddr_un {
	uchar	sun_len;
	uchar	sun_family;
	char	sun_path[104];
};

#define		SUN_LEN(x)	(sizeof(struct sockaddr_un))

typedef uint32_t in_addr_t;
typedef uint16_t in_port_t;

struct sockaddr_in {
	uchar		sin_len;
	uchar		sin_family;
	in_port_t	sin_port;
	struct {
		in_addr_t s_addr;
	} sin_addr;
};

struct sockaddr_storage {
	uchar		ss_len;
	uchar		ss_family;
	char		ss_data[sizeof(struct sockaddr_un) - 2];
};

#define		INADDR_ANY	((uint32_t)0)

struct stat {
	dev_t		st_dev;
	ino_t		st_ino;
	mode_t		st_mode;
	off_t		st_size;
	dev_t		st_rdev;
	uid_t		st_uid;
	blkcnt_t	st_blocks;
	time_t		st_mtime;
	ulong		st_mtimensec;
};

#define		S_IFMT		(0xffff0000ul)
#define		S_IFREG		(1ul << 16)
#define		S_IFDIR		(2ul << 16)
#define		S_IFIFO		(3ul << 16)
#define		S_IFLNK		(4ul << 16)
#define		S_IFBLK		(5ul << 16)

// XXX cleanup modes/dev majors
#define		S_ISDIR(mode)	((mode & S_IFMT) == S_IFDIR)
#define		S_ISDEV(mode)	(MAJOR(mode) != 0)
#define		S_ISFIFO(mode)	((mode & S_IFMT) == S_IFIFO)
#define		S_ISREG(mode)	((mode & S_IFMT) == S_IFREG)
#define		S_ISSOCK(mode)	(MAJOR(mode) == 2)
#define		S_ISLNK(mode)	((mode & S_IFMT) == S_IFLNK)
#define		S_ISBLK(mode)	(MAJOR(mode) == S_IFBLK)

// please never use these
#define		S_ISUID		(04000)
#define		S_ISGID		(02000)
#define		S_IRWXU		(00700)
#define		S_IRUSR		(00400)
#define		S_IWUSR		(00200)
#define		S_IXUSR		(00100)
#define		S_IRWXG		(00070)
#define		S_IRGRP		(00040)
#define		S_IWGRP		(00020)
#define		S_IXGRP		(00010)
#define		S_IRWXO		(00007)
#define		S_IROTH		(00004)
#define		S_IWOTH		(00002)
#define		S_IXOTH		(00001)

struct tfork_t {
	void *tf_tcb;
	// tf_tid is merely a convenient way for a new thread to learn its tid.
	void *tf_tid;
	void *tf_stack;
};

int accept(int, struct sockaddr *, socklen_t*);
// access(2) cannot be a wrapper around stat(2) because access(2) uses real-id
// instead of effective-id
int access(const char *, int);
#define		R_OK	(1 << 0)
#define		W_OK	(1 << 1)
#define		X_OK	(1 << 2)
int bind(int, const struct sockaddr *, socklen_t);
int connect(int, const struct sockaddr *, socklen_t);
int chmod(const char *, mode_t);
int close(int);
int chdir(const char *);
int dup(int);
int dup2(int, int);
void _exit(int)
    __attribute__((noreturn));
int execv(const char *, char * const[]);
int execve(const char *, char * const[], char * const[]);
int execvp(const char *, char * const[]);
pid_t fork(void);
int fstat(int, struct stat *);
int ftruncate(int, off_t);
int futex(const int, void *, void *, int, const struct timespec *);
#define		FUTEX_SLEEP	1
#define		FUTEX_WAKE	2
#define		FUTEX_CNDGIVE	3

char *getcwd(char *, size_t);
pid_t getpid(void);
pid_t getppid(void);

int getrlimit(int, struct rlimit *);
#define		RLIMIT_NOFILE	1
#define		RLIMIT_CORE	2
#define		RLIM_INFINITY	ULONG_MAX
int getrusage(int, struct rusage *);
#define		RUSAGE_SELF	1
#define		RUSAGE_CHILDREN	2
int getsockopt(int, int, int, void *, socklen_t *);
#define		SHUT_WR		(1 << 0)
#define		SHUT_RD		(1 << 1)
int shutdown(int, int);
int gettimeofday(struct timeval *, struct timezone *);
long gettid(void);

int fcntl(int, int, ...);
#define		F_GETFL		1
#define		F_SETFL		2
#define		F_GETFD		3
#define		F_SETFD		4
#define		F_SETLK		5
#define		F_SETLKW	6
#define		F_SETOWN	7

#define		FD_CLOEXEC	0x4

int kill(int, int);
int link(const char *, const char *);
int listen(int, int);
off_t lseek(int, off_t, int);
#define		SEEK_SET	1
#define		SEEK_CUR	2
#define		SEEK_END	4

int mkdir(const char *, long);
int mknod(const char *, mode_t, dev_t);
void *mmap(void *, size_t, int, int, int, long);
int munmap(void *, size_t);
int nanosleep(const struct timespec *, struct timespec *);
int open(const char *, int, ...);
#define		O_RDONLY	0
#define		O_WRONLY	1
#define		O_RDWR		2
#define		O_CREAT		0x40
#define		O_EXCL		0x80
#define		O_TRUNC		0x200
#define		O_APPEND	0x400
#define		O_NONBLOCK	0x800
#define		O_DIRECTORY	0x10000
#define		O_CLOEXEC	0x80000

int pause(void);
int pipe(int[2]);
int pipe2(int[2], int);
int poll(struct pollfd *, nfds_t, int);
ssize_t pread(int, void *, size_t, off_t);
ssize_t pwrite(int, const void *, size_t, off_t);
ssize_t read(int, void*, size_t);
ssize_t readv(int, const struct iovec *, int);
int reboot(void);
ssize_t recv(int, void *, size_t, int);
ssize_t recvfrom(int, void *, size_t, int, struct sockaddr *, socklen_t *);

struct msghdr {
	void		*msg_name;
	socklen_t	msg_namelen;
	struct iovec	*msg_iov;
	int		msg_iovlen;
	uint		msg_flags;
	void		*msg_control;
	socklen_t	msg_controllen;
};

struct cmsghdr {
	socklen_t	cmsg_len;
	int		cmsg_level;
	int		cmsg_type;
#define		SCM_RIGHTS	1
	char		_data[];
};

#define		MSG_TRUNC	(1 << 0)
#define		MSG_CTRUNC	(1 << 1)

#define		CMSG_FIRSTHDR(m)	\
	(((m)->msg_controllen > 0) ? (struct cmsghdr *)(m)->msg_control : NULL)
#define		CMSG_NXTHDR(m, c)	\
	(((char *)c + c->cmsg_len == \
	  (char *)(m)->msg_control + (m)->msg_controllen) ? \
	    NULL : (struct cmsghdr *)((char *)c + c->cmsg_len))
#define		CMSG_DATA(c)		((uchar *)&(c)->_data)
#define		ROUND2(x, n)		((x + (n - 1)) & ~(n - 1))
#define		CMSG_LEN(x)		(ROUND2(sizeof(struct cmsghdr) + x, 8ul))
#define		CMSG_SPACE(x)		CMSG_LEN(x)

ssize_t recvmsg(int, struct msghdr *, int);
int rename(const char *, const char *);
int rmdir(const char *);
int select(int, fd_set*, fd_set*, fd_set*, struct timeval *);
ssize_t send(int, const void *, size_t, int);
ssize_t sendto(int, const void *, size_t, int, const struct sockaddr *,
    socklen_t);
ssize_t sendmsg(int, struct msghdr *, int);
int setrlimit(int, const struct rlimit *);
pid_t setsid(void);
// levels
#define		SOL_SOCKET	1
#define		IPPROTO_TCP	2
// socket options
#define		SO_SNDBUF	1
#define		SO_SNDTIMEO	2
#define		SO_ERROR	3
#define		SO_TYPE		4
#define		SO_RCVBUF	5
#define		SO_REUSEADDR	6
#define		SO_KEEPALIVE	7
#define		SO_LINGER	8
// not supported on linux...
#define		SO_SNDLOWAT	9
#define		SO_NAME		10
#define		SO_PEER		11
struct linger {
	int l_onoff;
	int l_linger;
};
// TCP options
#define		TCP_NODELAY	20
int sigaction(int, const struct sigaction *, struct sigaction *);
#define		SIGHUP		1
#define		SIGINT		2
#define		SIGQUIT		3
#define		SIGILL		4
#define		SIGKILL		9
#define		SIGUSR1		10
#define		SIGSEGV		11
#define		SIGSYS		12
#define		SIGPIPE		13
#define		SIGALRM		14
#define		SIGTERM		15
#define		SIGSTOP		17
#define		SIGCHLD		20
#define		SIGIO		23
#define		SIGWINCH	28
#define		SIGUSR2		31
void (*signal(int, void (*)(int)))(int);
#define		SIG_DFL		((void (*)(int))1)
#define		SIG_IGN		((void (*)(int))2)
#define		SIG_BLOCK	1
#define		SIG_SETMASK	2
#define		SIG_UNBLOCK	3
int socket(int, int, int);
#define		AF_UNIX		1
#define		AF_LOCAL	AF_UNIX
#define		AF_INET		2

#define		SOCK_STREAM	(1 << 0)
#define		SOCK_DGRAM	(1 << 1)
#define		SOCK_RAW	(1 << 2)
#define		SOCK_SEQPACKET	(1 << 3)
#define		SOCK_CLOEXEC	(1 << 4)
#define		SOCK_NONBLOCK	(1 << 5)

int stat(const char *, struct stat *);
int sync(void);
long sys_prof(long, long, long, long);
#define		PROF_DISABLE   (1ul << 0)
#define		PROF_GOLANG    (1ul << 1)
#define		PROF_SAMPLE    (1ul << 2)
#define		PROF_COUNT     (1ul << 3)

// symbolic PMU event ids
#define		PROF_EV_UNHALTED_CORE_CYCLES		(1ul << 0)
#define		PROF_EV_LLC_MISSES			(1ul << 1)
#define		PROF_EV_LLC_REFS			(1ul << 2)
#define		PROF_EV_BRANCH_INSTR_RETIRED		(1ul << 3)
#define		PROF_EV_BRANCH_MISS_RETIRED		(1ul << 4)
#define		PROF_EV_INSTR_RETIRED			(1ul << 5)
	// non-architectural
	// "all dTLB misses that cause a page walk"
#define		PROF_EV_DTLB_LOAD_MISS_ANY		(1ul << 6)
	// "number of completed walks due to miss in sTLB"
#define		PROF_EV_DTLB_LOAD_MISS_STLB		(1ul << 7)
	// "retired stores that missed in the dTLB"
#define		PROF_EV_STORE_DTLB_MISS			(1ul << 8)
#define		PROF_EV_L2_LD_HITS			(1ul << 9)
	// "all iTLB misses that cause a page walk"
#define		PROF_EV_ITLB_LOAD_MISS_ANY		(1ul << 10)
#define		PROF_EV_CYCLES_STALLS_LDM_PENDING	(1ul << 11)

// PMU event flags
#define		PROF_EVF_OS		(1ul << 0)
#define		PROF_EVF_USR		(1ul << 1)
#define		PROF_EVF_BACKTRACE	(1ul << 2)

long sys_info(long);
#define		SINFO_GCCOUNT				0l
#define		SINFO_GCPAUSENS				1l
#define		SINFO_GCHEAPSZ				2l
#define		SINFO_GCMS				4l
#define		SINFO_GCTOTALLOC			5l
#define		SINFO_GCMARKTIME			6l
#define		SINFO_GCSWEEPTIME			7l
#define		SINFO_GCWBARTIME			8l
#define		SINFO_GCOBJS				9l
#define		SINFO_DOGC				10l
#define		SINFO_PROCLIST				11l

int truncate(const char *, off_t);
int unlink(const char *);
pid_t wait(int *);
pid_t waitpid(pid_t, int *, int);
pid_t wait3(int *, int, struct rusage *);
pid_t wait4(pid_t, int *, int, struct rusage *);
#define		WAIT_ANY	(-1)
#define		WAIT_MYPGRP	0

#define		WCONTINUED	1
#define		WNOHANG		2
#define		WUNTRACED	4

#define		WIFCONTINUED(x)		(x & (1 << 9))
#define		WIFEXITED(x)		(x & (1 << 10))
#define		WIFSIGNALED(x)		(x & (1 << 11))
#define		WEXITSTATUS(x)		(x & 0xff)
#define		WTERMSIG(x)		((int)((uint)x >> 27) & 0x1f)
ssize_t write(int, const void*, size_t);
ssize_t writev(int, const struct iovec *, int);

/*
 * thread stuff
 */
void tfork_done(long);
int tfork_thread(struct tfork_t *, long (*fn)(void *), void *);
void threxit(long);
int thrwait(int, long *);

typedef long pthread_t;

typedef struct {
	size_t stacksize;
#define		_PTHREAD_DEFSTKSZ		4096ull
} pthread_attr_t;

typedef struct {
	uint gen;
	int bcast;
} pthread_cond_t;

typedef struct {
} pthread_condattr_t;

typedef struct {
	uint locks;
} pthread_mutex_t;
#define		PTHREAD_MUTEX_INITIALIZER	{0}

typedef struct {
} pthread_mutexattr_t;

typedef struct {
} pthread_once_t;

typedef struct {
	uint target;
	volatile uint current;
} pthread_barrier_t;

typedef struct {
} pthread_barrierattr_t;

int pthread_attr_destroy(pthread_attr_t *);
int pthread_attr_init(pthread_attr_t *);
int pthread_attr_getstacksize(pthread_attr_t *, size_t *);
int pthread_attr_setstacksize(pthread_attr_t *, size_t);

int pthread_barrier_init(pthread_barrier_t *, pthread_barrierattr_t *, uint);
int pthread_barrier_destroy(pthread_barrier_t *);
int pthread_barrier_wait(pthread_barrier_t *);
#define		PTHREAD_BARRIER_SERIAL_THREAD	1

int pthread_cond_broadcast(pthread_cond_t *);
int pthread_cond_destroy(pthread_cond_t *);
int pthread_cond_init(pthread_cond_t *, const pthread_condattr_t *);
int pthread_cond_wait(pthread_cond_t *, pthread_mutex_t *);
int pthread_cond_signal(pthread_cond_t *);
int pthread_cond_timedwait(pthread_cond_t *, pthread_mutex_t *,
    const struct timespec *);

int pthread_create(pthread_t *, pthread_attr_t *, void* (*)(void *), void *);
int pthread_join(pthread_t, void **);
int pthread_mutex_init(pthread_mutex_t *, const pthread_mutexattr_t *);
int pthread_mutex_lock(pthread_mutex_t *);
int pthread_mutex_unlock(pthread_mutex_t *);
int pthread_mutex_destroy(pthread_mutex_t *);
int pthread_once(pthread_once_t *, void (*)(void));
pthread_t pthread_self(void);
int pthread_cancel(pthread_t);

int pthread_sigmask(int, const sigset_t *, sigset_t *);

int pthread_setcancelstate(int, int *);
#define		PTHREAD_CANCEL_ENABLE		1
#define		PTHREAD_CANCEL_DISABLE		2

int pthread_setcanceltype(int, int *);
#define		PTHREAD_CANCEL_DEFERRED		1
#define		PTHREAD_CANCEL_ASYNCHRONOUS	2

/*
 * posix stuff
 */
typedef struct {
	struct {
		int from;
		int to;
	} dup2s[10];
	int dup2slot;
} posix_spawn_file_actions_t;

typedef struct {
} posix_spawnattr_t;

int posix_spawn(pid_t *, const char *, const posix_spawn_file_actions_t *,
    const posix_spawnattr_t *, char *const argv[], char *const envp[]);
int posix_spawn_file_actions_adddup2(posix_spawn_file_actions_t *, int, int);
int posix_spawn_file_actions_destroy(posix_spawn_file_actions_t *);
int posix_spawn_file_actions_init(posix_spawn_file_actions_t *);

/*
 * libc
 */
#define		EXIT_FAILURE	(-1)
#define		EXIT_SUCCESS	0

#define		offsetof(s, m)	__builtin_offsetof(s, m)

#define		isfinite(x)	__builtin_isfinite(x)
#define		isinf(x)	__builtin_isinf(x)
#define		isnan(x)	__builtin_isnan(x)
#define		HUGE_VAL	__builtin_huge_val()
#define		NAN		__builtin_nanf("")

// these "builtins" sometimes result in a call to the library function. the
// builtin version just tries to optimize some cases.
#define		ceil(x)		__builtin_ceil(x)
#define		floor(x)	__builtin_floor(x)
#define		log(x)		__builtin_log(x)
#define		sqrt(x)		__builtin_sqrt(x)
#define		trunc(x)	__builtin_trunc(x)
#define		pow(x, y)	__builtin_pow(x, y)

// annoyingly, some GCC versions < 4.8 are bugged and do not have
// __builtin_bswap16.
#define		ntohs(x)	(((x & 0xff) << 8) | ((x & 0xff00) >> 8))
#define		htons(x)	(((x & 0xff) << 8) | ((x & 0xff00) >> 8))
#define		ntohl(x)	__builtin_bswap32(x)
#define		htonl(x)	__builtin_bswap32(x)

#define		MIN(x, y)	(x < y ? x : y)
#define		MAX(x, y)	(x > y ? x : y)

#define		atof(s)		strtod(s, NULL)

#define		_POSIX_NAME_MAX	14
struct dirent {
	ino_t d_ino;
	char d_name[_POSIX_NAME_MAX];
};

typedef struct {
	int fd;
	uint cent;
	uint nent;
	struct dirent dents[];
} DIR;

extern __thread int errno;

#define		BUFSIZ		4096
#define		L_tmpnam	20

struct _FILE {
	int fd;
	int btype;
#define		_IOFBF		1
#define		_IOLBF		2
#define		_IONBF		3
	char buf[BUFSIZ];
	// &buf[0] <= p <= end <= &buf[BUFSIZ]
	char *p;
	char *end;
	int eof;
	int error;
	// [&buf[0], pend) is dirty
	int writing;
	pthread_mutex_t mut;
	// if something else needs builtin linked lists, replace this with
	// fancy linked list macros.
	struct _FILE *lnext;
	struct _FILE *lprev;
};

typedef struct _FILE FILE;
extern FILE  *stdin, *stdout, *stderr;

typedef struct {
} jmp_buf;

struct lconv {
	int a;
	int decimal_point[10];
};

struct utsname {
#define		UTSMAX	64
	char	sysname[UTSMAX];
	char	nodename[UTSMAX];
	char	release[UTSMAX];
	char	version[UTSMAX];
	char	machine[UTSMAX];
};

void abort(void);
unsigned int alarm(unsigned int);
int atoi(const char *);
double ceil(double);
int closedir(DIR *);
int creat(const char *, mode_t);
char *ctime(const time_t *);
char *ctime_r(const time_t *, char *);
void err(int, const char *, ...)
    __attribute__((format(printf, 2, 3)))
    __attribute__((__noreturn__));
void errx(int, const char *, ...)
    __attribute__((format(printf, 2, 3)))
    __attribute__((__noreturn__));
void exit(int)
    __attribute__((noreturn));
int fclose(FILE *);
DIR *fdopendir(int);
int feof(FILE *);
int ferror(FILE *);
int fileno(FILE *);
int fflush(FILE *);
int fgetc(FILE *);
char *fgets(char *, int, FILE *);
FILE *fdopen(int, const char *);
FILE *fopen(const char *, const char *);
int fprintf(FILE *, const char *, ...)
    __attribute__((format(printf, 2, 3)));
int fsync(int);
//int fputs(const char *, FILE *); /*REDIS*/
size_t fread(void *, size_t, size_t, FILE *);
off_t ftello(FILE *);
size_t fwrite(const void *, size_t, size_t, FILE *);
struct gcfrac_t {
	long startms;
	long gcworkms;
	struct {
		// write barrier ms
		long wbms;
		long bgsweepms;
		long markms;
	} details;
};
struct gcfrac_t gcfracst(void);
double gcfracend(struct gcfrac_t *, long *, long *, long *);
int getopt(int, char * const *, const char *);
extern char *optarg;
extern int   optind;

int isalpha(int);
int isdigit(int);
int islower(int);
int isprint(int);
int ispunct(int);
int isspace(int);
int isupper(int);
int isxdigit(int);

//struct lconv* localeconv(void); /*REDIS*/
double log(double);
dev_t makedev(uint, uint);
int memcmp(const void *, const void *, size_t);
void *memcpy(void *, const void *, size_t);
void *memmove(void *, const void *, size_t);
void *memset(void *, int, size_t);
char *mkdtemp(char *);
int mkstemp(char *);
DIR *opendir(const char *);
void openlog(const char *, int, int);
// log options
#define		LOG_PID		(1ull << 0)
#define		LOG_CONS	(1ull << 1)
#define		LOG_NDELAY	(1ull << 2)
#define		LOG_ODELAY	(1ull << 3)
#define		LOG_NOWAIT	(1ull << 4)
int printf(const char *, ...)
    __attribute__((format(printf, 1, 2)));
double pow(double, double);
void perror(const char *);
void qsort(void *, size_t, size_t, int (*)(const void *, const void *));
int rand(void);
int rand_r(uint *);
#define		RAND_MAX	0x7fffffff
long random(void);
ulong rdtsc(void);
struct dirent *readdir(DIR *);
int readdir_r(DIR *, struct dirent *, struct dirent **);
char *readline(const char *);
void rewinddir(DIR *);
//int scanf(const char *, ...) /*REDIS*/
//    __attribute__((format(scanf, 1, 2))); /*REDIS*/
int setenv(const char *, const char *, int);
char *setlocale(int, const char *);
#define		LC_COLLATE	1
uint sleep(uint);
int snprintf(char *, size_t, const char *,...)
    __attribute__((format(printf, 3, 4)));
int sprintf(char *, const char *,...)
    __attribute__((format(printf, 2, 3)));
void srand(uint);
void srandom(uint);
int sscanf(const char *, const char *, ...)
    __attribute__((format(scanf, 2, 3)));
int strcasecmp(const char *, const char *);
int strncasecmp(const char *, const char *, size_t);
char *strchr(const char *, int);
char *strdup(const char *);
char *strerror(int);
#define		NL_TEXTMAX	64
int strerror_r(int, char *, size_t);
char *strncpy(char *, const char *, size_t);
size_t strlen(const char *);
int strcmp(const char *, const char *);
int strcoll(const char *, const char *);
int strncmp(const char *, const char *, size_t);
long strtol(const char *, char **, int);
double strtod(const char *, char **);
long double strtold(const char *, char **);
long long strtoll(const char *, char **, int);
ulong strtoul(const char *, char **, int);
unsigned long long strtoull(const char *, char **, int);
char *strstr(const char *, const char *);
void syslog(int, const char *, ...);
// priorities
#define		LOG_EMERG	(1ull << 0)
#define		LOG_ALERT	(1ull << 1)
#define		LOG_CRIT	(1ull << 2)
#define		LOG_ERR		(1ull << 3)
#define		LOG_WARNING	(1ull << 4)
#define		LOG_NOTICE	(1ull << 5)
#define		LOG_INFO	(1ull << 6)
#define		LOG_DEBUG	(1ull << 7)
#define		LOG_LOCAL0	(1ull << 8)
#define		LOG_LOCAL1	(1ull << 9)
#define		LOG_LOCAL2	(1ull << 10)
#define		LOG_LOCAL3	(1ull << 11)
#define		LOG_LOCAL4	(1ull << 12)
#define		LOG_LOCAL5	(1ull << 13)
#define		LOG_LOCAL6	(1ull << 14)
#define		LOG_LOCAL7	(1ull << 15)
#define		LOG_USER	(1ull << 16)
#define		LOG_ALL		(0x1ffff)
time_t time(time_t*);
int tolower(int);
int toupper(int);
double trunc(double);
int uname(struct utsname *);
int ungetc(int, FILE *);
int usleep(uint);
int vfprintf(FILE *, const char *, va_list)
    __attribute__((format(printf, 2, 0)));
int vprintf(const char *, va_list)
    __attribute__((format(printf, 1, 0)));
int vsnprintf(char *, size_t, const char *, va_list)
    __attribute__((format(printf, 3, 0)));
int vsscanf(const char *, const char *, va_list)
    __attribute__((format(scanf, 2, 0)));

void *malloc(size_t);
void free(void *);
void *calloc(size_t, size_t);
void *realloc(void *, size_t);

extern char __progname[64];
extern char **environ;

/* NGINX STUFF */
char *getenv(char *);
uid_t geteuid(void);

struct passwd {
	char *pw_name;
	uid_t pw_uid;
	gid_t pw_gid;
	char *pw_dir;
	char *pw_shell;
};

struct passwd *getpwnam(const char *);

struct group {
	char *gr_name;
	gid_t gr_gid;
	char **gr_mem;
};

struct group *getgrnam(const char *);

struct hostent {
	char *h_name;
	char **h_aliases;
	int h_addrtype;
	int h_length;
	char **h_addr_list;
};

struct hostent *gethostbyname(const char *);
int chown(const char *, uid_t, gid_t);
time_t mktime(struct tm *);
int getpeername(int, struct sockaddr *, socklen_t *);
int getsockname(int, struct sockaddr *, socklen_t *);
int setsockopt(int, int, int, const void *, socklen_t);
int gethostname(char *, size_t);
char *strpbrk(const char *, const char *);

struct itimerval {
	struct timeval it_interval;
	struct timeval it_value;
};

#define		ITIMER_REAL	1

int setitimer(int, struct itimerval *, struct itimerval *);

struct tm *localtime(const time_t *);
struct tm *gmtime(const time_t *);
int utimes(const char *, const struct timeval[2]);

typedef struct {
	int	gl_pathc;
	int	gl_matchc;
	char	**gl_pathv;
	int	gl_offs;
} glob_t;

int glob(const char *, int, int (*)(const char *, int), glob_t *);
void globfree(glob_t *);

struct flock {
	short	l_type;
#define		F_WRLCK		1
#define		F_UNLCK		2
	short	l_whence;
	off_t	l_start;
	off_t	l_len;
	pid_t	l_pid;
};

int socketpair(int, int, int, int[2]);
int ioctl(int, ulong, ...);
#define		FIOASYNC	3

int raise(int);
mode_t umask(mode_t);
int getpagesize(void);
int sigprocmask(int, sigset_t *, sigset_t *);
int sigsuspend(const sigset_t *);

int setpriority(int, int, int);
#define		PRIO_PROCESS	1

uid_t getuid(void);
int setuid(uid_t);
int setgid(gid_t);
int initgroups(const char *, gid_t);

#define		MSG_PEEK	1

char *realpath(const char *, char *);
int lstat(const char *, struct stat *);

//static inline pid_t
//getppid(void)
//{
//	pid_t ret;
//	asm volatile(
//		"movq	%%rsp, %%r10\n"
//		"leaq	2(%%rip), %%r11\n"
//		"sysenter\n"
//		"callq	flea\n"
//		: "=a"(ret)
//		: "0"(40)
//		: "cc", "rdi", "rsi", "rdx", "rcx", "r8", "memory", "r9", "r10", "r11", "r12", "r13", "r14", "r15");
//	return ret;
//}

#ifdef __cplusplus
}	// extern "C"
#endif
