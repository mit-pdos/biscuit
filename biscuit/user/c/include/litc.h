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

#define		EPERM		1
#define		ENOENT		2
#define		EINTR		4
#define		EIO		5
#define		E2BIG		7
#define		EBADF		9
#define		ECHILD		10
#define		EAGAIN		11
#define		EWOULDBLOCK	EAGAIN
#define		ENOMEM		12
#define		EFAULT		14
#define		EBUSY		16
#define		EEXIST		17
#define		ENODEV		19
#define		ENOTDIR		20
#define		EISDIR		21
#define		EINVAL		22
#define		ENOSPC		28
#define		ESPIPE		29
#define		EPIPE		32
#define		ERANGE		34
#define		ENAMETOOLONG	36
#define		ENOSYS		38
#define		ENOTEMPTY	39
#define		EOVERFLOW	75
#define		ENOTSOCK	88
#define		EOPNOTSUPP	95
#define		ECONNRESET	104
#define		EISCONN		106
#define		ENOTCONN	107
#define		ETIMEDOUT	110
#define		ECONNREFUSED	111
#define		EINPROGRESS	115

#define		MAP_FAILED	((void *) -1)

#define		MAP_PRIVATE	0x2
#define		MAP_ANON	0x20
#define		MAP_ANONYMOUS	MAP_ANON

#define		PROT_NONE	0x0
#define		PROT_READ	0x1
#define		PROT_WRITE	0x2
#define		PROT_EXEC	0x4

#define		FORK_PROCESS	0x1
#define		FORK_THREAD	0x2

#define		MAXBUF		4096

#define		MAJOR(x)	((long)((ulong)x >> 32))
#define		MINOR(x)	((long)(uint)x)
#define		MKDEV(x, y)	((dev_t)((ulong)x << 32 | (ulong)y))

/*
 * system calls
 */
#define 	FD_WORDS	8
// bits
#define 	FD_SETSIZE	(FD_WORDS*8*8)

typedef struct {
	ulong	mask[FD_WORDS];
} fd_set;

#define 	FD_ZERO(ft)	memset((ft), 0, sizeof(fd_set))
#define 	FD_SET(n, ft)	(ft)->mask[n/64] |= 1ull << (n%64)
#define 	FD_CLR(n, ft)	(ft)->mask[n/64] &= ~(1ull << (n%64))
#define 	FD_ISSET(n, ft)	((ft)->mask[n/64] & (1ull << (n%64)))

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

struct timezone {
};

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

struct stat {
	dev_t	st_dev;
	ulong	st_ino;
	mode_t	st_mode;
	off_t	st_size;
	dev_t	st_rdev;
};

#define		S_IFMT		((ulong)-1)
#define		S_IFREG		1
#define		S_IFDIR		2

#define		S_ISDIR(mode)	(mode == S_IFDIR)
#define		S_ISREG(mode)	(mode == S_IFREG)
#define		S_ISSOCK(mode)	(MAJOR(mode) == 2)

struct tfork_t {
	void *tf_tcb;
	void *tf_tid;
	void *tf_stack;
};

struct timespec {
	long tv_sec;
	long tv_nsec;
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
int dup2(int, int);
void _exit(int)
    __attribute__((noreturn));
int execv(const char *, char * const[]);
int execve(const char *, char * const[], char * const[]);
int execvp(const char *, char * const[]);
long fake_sys(long);
long fake_sys2(long);
int fork(void);
int fstat(int, struct stat *);
int ftruncate(int, off_t);
int futex(const int, void *, int, struct timespec *);
#define		FUTEX_SLEEP	1
#define		FUTEX_WAKE	2

char *getcwd(char *, size_t);
int getpid(void);
int getrlimit(int, struct rlimit *);
#define		RLIMIT_NOFILE	1
#define		RLIM_INFINITY	ULONG_MAX
int getrusage(int, struct rusage *);
#define		RUSAGE_SELF	1
#define		RUSAGE_CHILDREN	2
int getsockopt(int, int, int, void *, socklen_t *);
int gettimeofday(struct timeval *, struct timezone *);

int fcntl(int, int, ...);
#define		F_GETFL		1
#define		F_SETFL		2
#define		F_GETFD		3
#define		F_SETFD		4

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

#define		S_IRWXU		0700
int pause(void);
int pipe(int *);
int pipe2(int *, int);
int poll(struct pollfd *, nfds_t, int);
ssize_t pread(int, void *, size_t, off_t);
ssize_t pwrite(int, const void *, size_t, off_t);
ssize_t read(int, void*, size_t);
int reboot(void);
ssize_t recv(int, void *, size_t, int);
ssize_t recvfrom(int, void *, size_t, int, struct sockaddr *, socklen_t *);
int rename(const char *, const char *);
int select(int, fd_set*, fd_set*, fd_set*, struct timeval *);
ssize_t send(int, const void *, size_t, int);
ssize_t sendto(int, const void *, size_t, int, const struct sockaddr *,
    socklen_t);
int setrlimit(int, const struct rlimit *);
pid_t setsid(void);
//int setsockopt(int, int, int, void *, socklen_t);
// levels
#define		SOL_SOCKET	1
// socket options
#define		SO_SNDBUF	1
#define		SO_SNDTIMEO	2
#define		SO_ERROR	3
int sigaction(int, const struct sigaction *, struct sigaction *);
#define		SIGHUP		1
#define		SIGINT		2
#define		SIGILL		4
#define		SIGKILL		9
#define		SIGUSR1		10
#define		SIGSEGV		11
#define		SIGPIPE		13
#define		SIGALRM		14
#define		SIGTERM		15
void (*signal(int, void (*)(int)))(int);
#define		SIG_DFL		((void (*)(int))1)
#define		SIG_IGN		((void (*)(int))2)
//int sigprocmask(int, sigset_t *, sigset_t *); /*REDIS*/
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
int truncate(const char *, off_t);
int unlink(const char *);
int wait(int *);
int waitpid(int, int *, int);
int wait3(int *, int, struct rusage *);
int wait4(int, int *, int, struct rusage *);
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
} pthread_cond_t;

typedef struct {
} pthread_condattr_t;

typedef struct {
	uint locks;
	uint unlocks;
} pthread_mutex_t;
#define		PTHREAD_MUTEX_INITIALIZER	{0, 1}

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

int pthread_cond_destroy(pthread_cond_t *);
int pthread_cond_init(pthread_cond_t *, const pthread_condattr_t *);
int pthread_cond_wait(pthread_cond_t *, pthread_mutex_t *);
int pthread_cond_signal(pthread_cond_t *);

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

extern __thread int errno;

#define		BUFSIZ		4096

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
int atoi(const char *);
double ceil(double);
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
int feof(FILE *);
int ferror(FILE *);
int fileno(FILE *);
int fflush(FILE *);
int fgetc(FILE *);
char *fgets(char *, int, FILE *);
FILE *fopen(const char *, const char *);
int fprintf(FILE *, const char *, ...)
    __attribute__((format(printf, 2, 3)));
int fsync(int);
//int fputs(const char *, FILE *); /*REDIS*/
size_t fread(void *, size_t, size_t, FILE *);
off_t ftello(FILE *);
size_t fwrite(const void *, size_t, size_t, FILE *);
//char *getcwd(char *, size_t); /*REDIS*/
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
char *readline(const char *);
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
//void qsort(void *, size_t, size_t, int (*)(const void *, const void *)); /*REDIS*/

void *malloc(size_t);
void free(void *);
void *calloc(size_t, size_t);
void *realloc(void *, size_t);

extern char __progname[64];
extern char **environ;

#ifdef __cplusplus
}	// extern "C"
#endif
