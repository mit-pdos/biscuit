#include <littypes.h>
#include <litc.h>

#define SYS_READ         0
#define SYS_WRITE        1
#define SYS_OPEN         2
#define SYS_CLOSE        3
#define SYS_STAT         4
#define SYS_FSTAT        5
#define SYS_POLL         7
#define SYS_LSEEK        8
#define SYS_MMAP         9
#define SYS_MUNMAP       11
#define SYS_SIGACTION    13
#define SYS_READV        19
#define SYS_WRITEV       20
#define SYS_ACCESS       21
#define SYS_DUP2         33
#define SYS_PAUSE        34
#define SYS_GETPID       39
#define SYS_GETPPID      40
#define SYS_SOCKET       41
#define SYS_CONNECT      42
#define SYS_ACCEPT       43
#define SYS_SENDTO       44
#define SYS_RECVFROM     45
#define SYS_SOCKETPAIR   46
#define SYS_SHUTDOWN     48
#define SYS_BIND         49
#define SYS_LISTEN       50
#define SYS_RECVMSG      51
#define SYS_SENDMSG      52
#define SYS_GETSOCKOPT   55
#define SYS_SETSOCKOPT   56
#define SYS_FORK         57
#define SYS_EXECV        59
#define SYS_EXIT         60
#define SYS_WAIT4        61
#define SYS_KILL         62
#define SYS_FCNTL        72
#define SYS_TRUNC        76
#define SYS_FTRUNC       77
#define SYS_GETCWD       79
#define SYS_CHDIR        80
#define SYS_RENAME       82
#define SYS_MKDIR        83
#define SYS_LINK         86
#define SYS_UNLINK       87
#define SYS_GETTOD       96
#define SYS_GETRLIMIT    97
#define SYS_GETRUSAGE    98
#define SYS_MKNOD        133
#define SYS_SETRLIMIT    160
#define SYS_SYNC         162
#define SYS_REBOOT       169
#define SYS_NANOSLEEP    230
#define SYS_PIPE2        293
#define SYS_PROF         31337
#define SYS_THREXIT      31338
#define SYS_INFO         31339
#define SYS_PREAD        31340
#define SYS_PWRITE       31341
#define SYS_FUTEX        31342
#define SYS_GETTID       31343

__thread int errno;

static int dolock = 1;

static void pmsg(char *, long);

struct kinfo_t {
	void *freshtls;
	size_t len;
	void *t0tls;
	ulong pspercycle;
};

// initialized in _entry, given to us by kernel
static struct kinfo_t *kinfo;

// tfork_thread and _pcreate use inline asm to call some syscalls in order to
// 1) make sure the new thread immediately calls the destination function
// without executing any C code (which would be unsafe since the new thread is
// running on a different stack) 2) to make sure that, on thread exit, the
// stack is not used after munmapping it, but before calling exit(2). we use
// this macro to make sure the clobbers are coherent for these three pieces of
// code using syscalls.
#define SYSCALL_CLOBBERS "cc", "memory", "r9", "r10", "r11", "r12", "r13", \
			 "r14", "r15"
long
syscall(long a1, long a2, long a3, long a4,
    long a5, long trap)
{
	long ret;
	register long r8 asm("r8") = a5;

	// we may want to follow the sys5 abi and have the kernel restore
	// r14-r15 too...
	asm volatile(
		"movq	%%rsp, %%r10\n"
		"leaq	2(%%rip), %%r11\n"
		"sysenter\n"
		: "=a"(ret)
		: "0"(trap), "D"(a1), "S"(a2), "d"(a3), "c"(a4), "r"(r8)
		: SYSCALL_CLOBBERS);

	return ret;
}

#define SA(x)     ((long)x)
#define ERRNO_NZ(x) do {				\
				if (x != 0) {		\
					errno = -x;	\
					x = -1;		\
				}			\
			} while (0)

#define ERRNO_NEG(x)	do {				\
				if (x < 0) {		\
					errno = -x;	\
					x = -1;		\
				}			\
			} while (0)

int
accept(int fd, struct sockaddr *s, socklen_t *sl)
{
	int ret = syscall(SA(fd), SA(s), SA(sl), 0, 0, SYS_ACCEPT);
	ERRNO_NEG(ret);
	return ret;
}

int
access(const char *p, int mode)
{
	int ret = syscall(SA(p), SA(mode), 0, 0, 0, SYS_ACCESS);
	ERRNO_NZ(ret);
	return ret;
}

int
bind(int sockfd, const struct sockaddr *s, socklen_t sl)
{
	int ret = syscall(SA(sockfd), SA(s), SA(sl), 0, 0, SYS_BIND);
	ERRNO_NZ(ret);
	return ret;
}

int
close(int fd)
{
	int ret = syscall(SA(fd), 0, 0, 0, 0, SYS_CLOSE);
	ERRNO_NZ(ret);
	return ret;
}

int
chdir(const char *path)
{
	int ret = syscall(SA(path), 0, 0, 0, 0, SYS_CHDIR);
	ERRNO_NZ(ret);
	return ret;
}

int
dup(int o)
{
	int fd;
	if ((fd = open("/", O_RDONLY | O_DIRECTORY)) == -1)
		return -1;
	if (dup2(o, fd) == -1)
		return -1;
	return fd;
}

int
connect(int fd, const struct sockaddr *sa, socklen_t salen)
{
	int ret = syscall(SA(fd), SA(sa), SA(salen), 0, 0, SYS_CONNECT);
	ERRNO_NZ(ret);
	return ret;
}

int
chmod(const char *path, mode_t mode)
{
	printf("warning: chmod is a no-op\n");
	return 0;
}

int
dup2(int old, int new)
{
	int ret = syscall(SA(old), SA(new), 0, 0, 0, SYS_DUP2);
	ERRNO_NEG(ret);
	return ret;
}

void
_exit(int status)
{
	syscall(status, 0, 0, 0, 0, SYS_EXIT);
	errx(-1, "exit returned");
	while(1);
}

int
execv(const char *path, char * const argv[])
{
	return execve(path, argv, environ);
}

int
execve(const char *path, char * const argv[], char * const envp[])
{
	if (envp != environ)
		errx(-1, "envp not supported yet");
	int ret = syscall(SA(path), SA(argv), 0, 0, 0, SYS_EXECV);
	errno = -ret;
	return -1;
}

static const char *
_binname(const char *bin)
{
	static char buf[64];
	// absoulte path
	if (strchr(bin, '/')) {
		snprintf(buf, sizeof(buf), "%s", bin);
		return bin;
	}

	// try paths
	char *paths[] = {"/bin/"};
	const int elems = sizeof(paths)/sizeof(paths[0]);
	int i;
	for (i = 0; i < elems; i++) {
		snprintf(buf, sizeof(buf), "%s%s", paths[i], bin);
		struct stat st;
		if (stat(buf, &st) == 0)
			return buf;
		else if (errno != ENOENT)
			return NULL;
	}
	return NULL;
}

int
execvp(const char *path, char * const argv[])
{
	const char *p = _binname(path);
	if (!p)
		return -1;
	return execv(p, argv);
}

int
fcntl(int fd, int cmd, ...)
{
	int ret;
	long a1 = SA(fd);
	long a2 = SA(cmd);

	va_list ap;
	va_start(ap, cmd);
	switch (cmd) {
	case F_GETFD:
	case F_SETFD:
	case F_GETFL:
	case F_SETFL:
	{
		int fl = va_arg(ap, int);
		ret = syscall(a1, a2, SA(fl), 0, 0, SYS_FCNTL);
		ERRNO_NEG(ret);
		break;
	}
	case F_SETOWN:
	{
		fprintf(stderr, "warning: F_SETOWN is no-op\n");
		ret = 0;
		break;
	}
	default:
		errx(-1, "no imp");
	}
	va_end(ap);
	return ret;
}

pid_t
fork(void)
{
	long flags = FORK_PROCESS;
	int ret = syscall(0, flags, 0, 0, 0, SYS_FORK);
	ERRNO_NEG(ret);
	return ret;
}

int
fstat(int fd, struct stat *buf)
{
	int ret = syscall(SA(fd), SA(buf), 0, 0, 0, SYS_FSTAT);
	ERRNO_NZ(ret);
	return ret;
}

char *
getcwd(char *buf, size_t sz)
{
	int ret = syscall(SA(buf), SA(sz), 0, 0, 0, SYS_GETCWD);
	ERRNO_NZ(ret);
	if (ret)
		return NULL;
	return buf;
}

pid_t
getpid(void)
{
	return syscall(0, 0, 0, 0, 0, SYS_GETPID);
}

pid_t
getppid(void)
{
	return syscall(0, 0, 0, 0, 0, SYS_GETPPID);
}

int
getsockopt(int fd, int level, int opt, void *optv, socklen_t *optlen)
{
	int ret = syscall(SA(fd), SA(level), SA(opt), SA(optv), SA(optlen),
	    SYS_GETSOCKOPT);
	ERRNO_NZ(ret);
	return ret;
}

int
getpeername(int s, struct sockaddr *sa, socklen_t *sl)
{
	return getsockopt(s, SOL_SOCKET, SO_PEER, sa, sl);
}

int
getsockname(int s, struct sockaddr *sa, socklen_t *sl)
{
	return getsockopt(s, SOL_SOCKET, SO_NAME, sa, sl);
}

int
gettimeofday(struct timeval *tv, struct timezone *tz)
{
	if (tz)
		errx(-1, "timezone not supported");
	//int ret = syscall(SA(tv), 0, 0, 0, 0, SYS_GETTOD);
	//ERRNO_NZ(ret);
	//return ret;
	ulong c = rdtsc();
	ulong us = c * kinfo->pspercycle / 1000000;
	tv->tv_sec = us / 1000000;
	tv->tv_usec = us % 1000000;
	return 0;
}

// XXX should simply look at TCB
long
gettid(void)
{
	return syscall(0, 0, 0, 0, 0, SYS_GETTID);
}

int
getrlimit(int res, struct rlimit *rlp)
{
	int ret = syscall(SA(res), SA(rlp), 0, 0, 0, SYS_GETRLIMIT);
	ERRNO_NEG(ret);
	return ret;
}

int
getrusage(int who, struct rusage *r)
{
	int ret = syscall(SA(who), SA(r), 0, 0, 0, SYS_GETRUSAGE);
	ERRNO_NZ(ret);
	return ret;
}

int
kill(int pid, int sig)
{
	if (sig != SIGKILL) {
		printf("%s: kill: only SIGKILL is supported\n", __progname);
		return -1;
	}
	int ret = syscall(SA(pid), SA(sig), 0, 0, 0, SYS_KILL);
	ERRNO_NZ(ret);
	return ret;
}

int
link(const char *old, const char *new)
{
	int ret = syscall(SA(old), SA(new), 0, 0, 0, SYS_LINK);
	ERRNO_NZ(ret);
	return ret;
}

int
listen(int fd, int backlog)
{
	int ret = syscall(SA(fd), SA(backlog), 0, 0, 0, SYS_LISTEN);
	ERRNO_NZ(ret);
	return ret;
}

off_t
lseek(int fd, off_t off, int whence)
{
	int ret = syscall(SA(fd), SA(off), SA(whence), 0, 0, SYS_LSEEK);
	ERRNO_NEG(ret);
	return ret;
}

int
mkdir(const char *p, long mode)
{
	int ret = syscall(SA(p), mode, 0, 0, 0, SYS_MKDIR);
	ERRNO_NZ(ret);
	return ret;
}

int
mknod(const char *p, mode_t m, dev_t d)
{
	if (MAJOR(d) == 0)
		errx(-1, "bad major: 0");
	int ret = syscall(SA(p), SA(m), SA(d), 0, 0, SYS_MKNOD);
	ERRNO_NZ(ret);
	return ret;
}

void *
mmap(void *addr, size_t len, int prot, int flags, int fd, long offset)
{
	ulong protflags = (ulong)prot << 32;
	protflags |= flags;
	long ret;
	ret = syscall(SA(addr), SA(len), SA(protflags), SA(fd),
	    SA(offset), SYS_MMAP);
	if (ret < 0 && -ret >= ERRNO_FIRST && -ret <= ERRNO_LAST) {
		errno = -ret;
		ret = (long)MAP_FAILED;
	}
	return (void *)ret;
}

int
munmap(void *addr, size_t len)
{
	int ret = syscall(SA(addr), SA(len), 0, 0, 0, SYS_MUNMAP);
	ERRNO_NZ(ret);
	return ret;
}

int
nanosleep(const struct timespec *timeout, struct timespec *remainder)
{
	int ret = syscall(SA(timeout), SA(remainder), 0, 0, 0, SYS_NANOSLEEP);
	ERRNO_NZ(ret);
	return ret;
}

int
open(const char *path, int flags, ...)
{
	mode_t mode = 0;
	if (flags & O_CREAT) {
		va_list l;
		va_start(l, flags);
		mode = va_arg(l, mode_t);
		va_end(l);
	}
	int ret = syscall(SA(path), flags, mode, 0, 0, SYS_OPEN);
	ERRNO_NEG(ret);
	return ret;
}

int
pause(void)
{
	int ret = syscall(0, 0, 0, 0, 0, SYS_PAUSE);
	errno = ret;
	return -1;
}

int
pipe(int pfds[2])
{
	int ret = pipe2(pfds, 0);
	ERRNO_NZ(ret);
	return ret;
}

int
pipe2(int pfds[2], int flags)
{
	int ret = syscall(SA(pfds), SA(flags), 0, 0, 0, SYS_PIPE2);
	ERRNO_NZ(ret);
	return ret;
}

int
poll(struct pollfd *fds, nfds_t nfds, int timeout)
{
	struct pollfd *end = fds + nfds;
	struct pollfd *p;
	for (p = fds; p < end; p++)
		p->revents = 0;
	int ret = syscall(SA(fds), SA(nfds), SA(timeout), 0, 0, SYS_POLL);
	ERRNO_NEG(ret);
	return ret;
}

ssize_t
pread(int fd, void *dst, size_t len, off_t off)
{
	ssize_t ret = syscall(SA(fd), SA(dst), SA(len), SA(off), 0, SYS_PREAD);
	ERRNO_NEG(ret);
	return ret;
}

ssize_t
pwrite(int fd, const void *src, size_t len, off_t off)
{
	ssize_t ret = syscall(SA(fd), SA(src), SA(len), SA(off), 0, SYS_PWRITE);
	ERRNO_NEG(ret);
	return ret;
}

long
read(int fd, void *buf, size_t c)
{
	int ret = syscall(SA(fd), SA(buf), SA(c), 0, 0, SYS_READ);
	ERRNO_NEG(ret);
	return ret;
}

int
reboot(void)
{
	int ret = syscall(0, 0, 0, 0, 0, SYS_REBOOT);
	ERRNO_NZ(ret);
	return ret;
}

ssize_t
recv(int fd, void *buf, size_t len, int flags)
{
	int ret = recvfrom(fd, buf, len, flags, NULL, NULL);
	ERRNO_NEG(ret);
	return ret;
}

ssize_t
recvfrom(int fd, void *buf, size_t len, int flags, struct sockaddr *sa,
    socklen_t *salen)
{
	if (len >= (1ULL << 32))
		errx(-1, "len too big");
	ulong flaglen = len << 32 | flags;
	int ret = syscall(SA(fd), SA(buf), SA(flaglen), SA(sa),
	    SA(salen), SYS_RECVFROM);
	ERRNO_NEG(ret);
	return ret;
}

ssize_t
recvmsg(int fd, struct msghdr *msg, int flags)
{
	ssize_t ret = syscall(SA(fd), SA(msg), SA(flags), 0, 0, SYS_RECVMSG);
	ERRNO_NEG(ret);
	return ret;
}

int
rename(const char *old, const char *new)
{
	int ret = syscall(SA(old), SA(new), 0, 0, 0, SYS_RENAME);
	ERRNO_NZ(ret);
	return ret;
}

ssize_t
send(int fd, const void *buf, size_t len, int flags)
{
	return sendto(fd, buf, len, flags, NULL, 0);
}

ssize_t
sendto(int fd, const void *buf, size_t len, int flags,
    const struct sockaddr *sa, socklen_t slen)
{
	if (len >= (1ULL << 32))
		errx(-1, "len too big");
	ulong flaglen = len << 32 | flags;
	int ret = syscall(SA(fd), SA(buf), SA(flaglen), SA(sa), SA(slen),
	    SYS_SENDTO);
	ERRNO_NEG(ret);
	return ret;
}

ssize_t
sendmsg(int fd, struct msghdr *msg, int flags)
{
	ssize_t ret = syscall(SA(fd), SA(msg), SA(flags), 0, 0, SYS_SENDMSG);
	ERRNO_NEG(ret);
	return ret;
}

int
setrlimit(int res, const struct rlimit *rlp)
{
	int ret = syscall(SA(res), SA(rlp), 0, 0, 0, SYS_SETRLIMIT);
	ERRNO_NEG(ret);
	return ret;
}

pid_t
setsid(void)
{
	printf("warning: no sessions or process groups yet\n");
	return getpid();
}

int
setsockopt(int a, int b, int c, const void *d, socklen_t e)
{
	static char *on[] = {
#define F(x) [x] = #x
		F(SO_SNDTIMEO),
		F(SO_ERROR),
		F(SO_TYPE),
		F(SO_REUSEADDR),
		F(SO_KEEPALIVE),
		F(SO_LINGER),
		F(SO_SNDLOWAT),
		F(TCP_NODELAY),
#undef F
	};
	long ret = 0;
	if (on[c] != NULL) {
		errno = 0;
		fprintf(stderr, "warning: setsockopt no-op for %s\n", on[c]);
	} else {
		ret = syscall(SA(a), SA(b), SA(c), SA(d), SA(e),
		    SYS_SETSOCKOPT);
		ERRNO_NEG(ret);
	}
	return (int)ret;
}

int
sigaction(int sig, const struct sigaction *act, struct sigaction *oact)
{
	printf("warning: no signals yet\n");
	return 0;
	//int ret = syscall(SA(sig), SA(act), SA(oact), 0, 0, SYS_SIGACTION);
	//ERRNO_NEG(ret);
	//return ret;
}

ssize_t
readv(int fd, const struct iovec *iovs, int niovs)
{
	ssize_t ret = syscall(SA(fd), SA(iovs), SA(niovs), 0, 0, SYS_READV);
	ERRNO_NEG(ret);
	return ret;
}

void
(*signal(int sig, void (*f)(int)))(int)
{
	struct sigaction sa, oa;
	memset(&sa, 0, sizeof(struct sigaction));
	sa.sa_handler = f;
	sigaction(sig, &sa, &oa);
	return oa.sa_handler;
}

int
shutdown(int fd, int how)
{
	int ret = syscall(SA(fd), SA(how), 0, 0, 0, SYS_SHUTDOWN);
	ERRNO_NZ(ret);
	return ret;
}

int
socket(int dom, int type, int proto)
{
	int ret = syscall(SA(dom), SA(type), SA(proto), 0, 0, SYS_SOCKET);
	ERRNO_NEG(ret);
	return ret;
}

int
socketpair(int dom, int type, int proto, int p[2])
{
	int ret = syscall(SA(dom), SA(type), SA(proto), SA(p), 0,
	    SYS_SOCKETPAIR);
	ERRNO_NZ(ret);
	return ret;
}

int
stat(const char *path, struct stat *st)
{
	int ret = syscall(SA(path), SA(st), 0, 0, 0, SYS_STAT);
	ERRNO_NZ(ret);
	return ret;
}

int
sync(void)
{
	int ret = syscall(0, 0, 0, 0, 0, SYS_SYNC);
	ERRNO_NZ(ret);
	return ret;
}

long
sys_prof(long ptype, long events, long flags, long intperiod)
{
	long ret = syscall(ptype, events, flags, intperiod, 0, SYS_PROF);
	ERRNO_NZ(ret);
	return ret;
}

long
sys_info(long n)
{
	long ret = syscall(n, 0, 0, 0, 0, SYS_INFO);
	ERRNO_NEG(ret);
	return ret;
}

int
truncate(const char *p, off_t newlen)
{
	int ret = syscall(SA(p), SA(newlen), 0, 0, 0, SYS_TRUNC);
	ERRNO_NZ(ret);
	return ret;
}

static int
_unlink(const char *path, int wantdir)
{
	int ret = syscall(SA(path), SA(wantdir), 0, 0, 0, SYS_UNLINK);
	ERRNO_NZ(ret);
	return ret;
}

int
unlink(const char *path)
{
	return _unlink(path, 0);
}

int
rmdir(const char *path)
{
	return _unlink(path, 1);
}

pid_t
wait(int *status)
{
	pid_t ret = syscall(WAIT_ANY, SA(status), 0, 0, 0, SYS_WAIT4);
	ERRNO_NEG(ret);
	return ret;
}

pid_t
waitpid(pid_t pid, int *status, int options)
{
	return wait4(pid, status, options, NULL);
}

pid_t
wait4(pid_t pid, int *status, int options, struct rusage *r)
{
	pid_t ret = syscall(pid, SA(status), SA(options), SA(r), 0,
	    SYS_WAIT4);
	ERRNO_NEG(ret);
	return ret;
}

pid_t
wait3(int *status, int options, struct rusage *r)
{
	pid_t ret = syscall(WAIT_ANY, SA(status), SA(options), SA(r), 0,
	    SYS_WAIT4);
	ERRNO_NEG(ret);
	return ret;
}

long
write(int fd, const void *buf, size_t c)
{
	int ret = syscall(fd, SA(buf), SA(c), 0, 0, SYS_WRITE);
	ERRNO_NEG(ret);
	return ret;
}

ssize_t
writev(int fd, const struct iovec *iovs, int niovs)
{
	ssize_t ret = syscall(SA(fd), SA(iovs), SA(niovs), 0, 0, SYS_WRITEV);
	ERRNO_NEG(ret);
	return ret;
}

/*
 * thread stuff
 */
void
tfork_done(long status)
{
	threxit(status);
	errx(-1, "threxit returned");
}

int
tfork_thread(struct tfork_t *args, long (*fn)(void *), void *fnarg)
{
	int tid;
	long flags = FORK_THREAD;

	// rbx and rbp are preserved across syscalls. i don't know how to
	// specify rbp as a register contraint.
	register ulong rbp asm("rbp") = (ulong)fn;
	register ulong rbx asm("rbx") = (ulong)fnarg;

	asm volatile(
	    "movq	%%rsp, %%r10\n"
	    "leaq	2(%%rip), %%r11\n"
	    "sysenter\n"
	    "cmpl	$0, %%eax\n"
	    // parent or error
	    "jne	1f\n"
	    // child
	    "movq	%5, %%rdi\n"
	    "call	*%4\n"
	    "movq	%%rax, %%rdi\n"
	    "call	tfork_done\n"
	    "movq	$0, 0x0\n"
	    "1:\n"
	    : "=a"(tid)
	    : "D"(args), "S"(flags), "0"(SYS_FORK), "r"(rbp), "r"(rbx)
	    : SYSCALL_CLOBBERS);
	return tid;
}

void
threxit(long status)
{
	syscall(SA(status), 0, 0, 0, 0, SYS_THREXIT);
}

int
thrwait(int tid, long *status)
{
	if (tid <= 0)
		errx(-1, "thrwait: bad tid %d", tid);
	int ret = syscall(SA(tid), SA(status), 0, 0, 1, SYS_WAIT4);
	return ret;
}

static void *
mkstack(size_t size)
{
	const size_t pgsize = 1 << 12;
	size += pgsize - 1;
	size &= ~(pgsize - 1);
	char *ret = mmap(NULL, size, PROT_READ | PROT_WRITE,
	    MAP_ANON | MAP_PRIVATE, -1, 0);
	if (!ret)
		return NULL;
	return ret + size;
}

struct pcargs_t {
	void* (*fn)(void *);
	void *arg;
	void *stack;
	void *tls;
	ulong stksz;
};

static long
_pcreate(void *vpcarg)
{
	struct pcargs_t pcargs = *(struct pcargs_t *)vpcarg;
	free(vpcarg);
	long status;
	status = (long)(pcargs.fn(pcargs.arg));
	free(pcargs.tls);

	// rbx and rbp are preserved across syscalls. i don't know how to
	// specify rbp as a register contraint.
	register ulong rbp asm("rbp") = SYS_THREXIT;
	register long rbx asm("rbx") = status;

	asm volatile(
	    "movq	%%rsp, %%r10\n"
	    "leaq	2(%%rip), %%r11\n"
	    "sysenter\n"
	    "cmpq	$0, %%rax\n"
	    "je		1f\n"
	    "movq	$0, 0x0\n"
	    "1:\n"
	    "movq	%3, %%rax\n"
	    "movq	%4, %%rdi\n"
	    "leaq	2(%%rip), %%r11\n"
	    "sysenter\n"
	    "movq	$0, 0x1\n"
	    :
	    : "a"(SYS_MUNMAP), "D"(pcargs.stack), "S"(pcargs.stksz),
	      "r"(rbp), "r"(rbx)
	    : SYSCALL_CLOBBERS);
	// not reached
	return 0;
}

int
pthread_create(pthread_t *t, pthread_attr_t *attrs, void* (*fn)(void *),
    void *arg)
{
	// allocate and setup TLS space
	if (!kinfo)
		errx(-1, "nil kinfo");
	// +8 for the tls indirect pointer
	void *newtls = malloc(kinfo->len + 8);
	if (!newtls)
		return ENOMEM;
	char *tlsdata = newtls;
	tlsdata += 8;
	memmove(tlsdata, kinfo->freshtls, kinfo->len);

	char **tlsptr = newtls;
	*tlsptr = tlsdata + kinfo->len;

	size_t stksz = _PTHREAD_DEFSTKSZ;
	if (attrs) {
		stksz = attrs->stacksize;
		if (stksz > 1024*1024*128)
			errx(-1, "big fat stack");
		const ulong pgsize = 1ul << 12;
		if ((stksz % pgsize) != 0)
			errx(-1, "stack size not aligned");
	}
	int ret;
	// XXX setup guard page
	void *stack = mkstack(stksz);
	if (!stack) {
		ret = ENOMEM;
		goto tls;
	}
	long tid;
	struct tfork_t tf = {
		.tf_tcb = tlsptr,
		.tf_tid = &tid,
		.tf_stack = stack,
	};
	struct pcargs_t *pca = malloc(sizeof(struct pcargs_t));
	if (!pca) {
		ret = ENOMEM;
		goto errstack;
	}

	pca->fn = fn;
	pca->arg = arg;
	pca->stack = stack - stksz;
	pca->tls = newtls;
	pca->stksz = stksz;
	ret = tfork_thread(&tf, _pcreate, pca);
	if (ret < 0) {
		ret = -ret;
		goto both;
	}
	*t = tid;
	return 0;
both:
	free(pca);
errstack:
	munmap(stack - stksz, stksz);
tls:
	free(newtls);
	return ret;
}

int
pthread_join(pthread_t t, void **retval)
{
	int ret = thrwait(t, (long *)retval);
	if (ret < 0)
		return -ret;
	return 0;
}

int
pthread_cancel(pthread_t t)
{
	errx(-1, "no imp");
}

int
pthread_mutex_init(pthread_mutex_t *m, const pthread_mutexattr_t *attr)
{
	if (attr)
		errx(-1, "mutex attributes not supported");
	m->locks = 0;
	return 0;
}

int
pthread_mutex_lock(pthread_mutex_t *m)
{
	if (!dolock)
		return 0;
	// fast path
	uint * const p = &m->locks;
	uint old;
	if ((old = __sync_val_compare_and_swap(p, 0, 1)) == 0)
		return 0;

	while (1) {
		// upgrade lock to "has waiters"
		old = __sync_lock_test_and_set(p, 2);
		// did we get lucky?
		if (old == 0)
			return 0;

		// sleep and acquire
		int ret;
		ret = futex(FUTEX_SLEEP, &m->locks, NULL, 2, NULL);
		if (ret < 0)
			return ret;
	}
}

int
pthread_mutex_unlock(pthread_mutex_t *m)
{
	uint old;
	old = __sync_lock_test_and_set(&m->locks, 0);

	int ret = 0;
	if (old == 2)
		ret = futex(FUTEX_WAKE, &m->locks, NULL, 1, NULL);
	return ret;
}

int
pthread_mutex_destroy(pthread_mutex_t *mutex)
{
	return 0;
}

int
pthread_once(pthread_once_t *octl, void (*fn)(void))
{
	errx(-1, "pthread_once: no imp");
	return 0;
}

pthread_t
pthread_self(void)
{
	return gettid();
}

// XXX use fancy new futexes
int
pthread_barrier_init(pthread_barrier_t *b, pthread_barrierattr_t *attr, uint c)
{
	if (attr)
		errx(-1, "barrier attributes not supported");
	b->target = c;
	b->current = 0;
	return 0;
}

int
pthread_barrier_destroy(pthread_barrier_t *b)
{
	return 0;
}

int
pthread_barrier_wait(pthread_barrier_t *b)
{
	uint m = 1ull << 31;
	uint c;
	while (1) {
		uint o = b->current;
		uint n = o + 1;
		if ((o & m) != 0) {
			asm volatile("pause":::"memory");
			continue;
		}
		c = n;
		if (__sync_bool_compare_and_swap(&b->current, o, n))
			break;
	}

	if (c == b->target) {
		while (1) {
			uint o = b->current;
			uint n = (b->current - 1) | m;
			if (__sync_bool_compare_and_swap(&b->current, o, n))
				break;
		}
		return PTHREAD_BARRIER_SERIAL_THREAD;
	}

	while ((b->current & m) == 0)
		asm volatile("pause":::"memory");

	c = __sync_add_and_fetch(&b->current, -1);
	if (c == m)
		b->current = 0;
	return 0;
}

int
pthread_cond_broadcast(pthread_cond_t *c)
{
	__sync_add_and_fetch(&c->gen, 1);
	return futex(FUTEX_WAKE, &c->gen, NULL, -1, NULL);
}

int
pthread_cond_destroy(pthread_cond_t *c)
{
	return 0;
}

int
pthread_cond_init(pthread_cond_t *c, const pthread_condattr_t *ca)
{
	if (ca)
		errx(-1, "cond attritbutes not supported");
	c->gen = 0;
	c->bcast = 0;
	return 0;
}

int
pthread_cond_timedwait(pthread_cond_t *c, pthread_mutex_t *m,
    const struct timespec *t)
{
	uint last = c->gen;
	asm volatile("":::"memory");
	int ret;
	if ((ret = pthread_mutex_unlock(m)))
		return ret;

	int moveq;
	if ((moveq = futex(FUTEX_SLEEP, &c->gen, NULL, last, t)) < 0)
		return moveq;

	if ((ret = pthread_mutex_lock(m)))
		return ret;

	// on pthread_cond_broadcast(3), move cond's sleep queue to my mutex
	if (moveq) {
		// make sure threads moved to mutex wait queue are woken up
		c->bcast = 1;
		ret = futex(FUTEX_CNDGIVE, &c->gen, &m->locks, 0, NULL);
	}
	if (c->bcast)
		m->locks = 2;
	return ret;
}

int
pthread_cond_wait(pthread_cond_t *c, pthread_mutex_t *m)
{
	return pthread_cond_timedwait(c, m, NULL);
}

int
pthread_cond_signal(pthread_cond_t *c)
{
	__sync_add_and_fetch(&c->gen, 1);
	return futex(FUTEX_WAKE, &c->gen, NULL, 1, NULL);
}

int
pthread_sigmask(int how, const sigset_t *set, sigset_t *oset)
{
	printf("warning: signals not implemented\n");
	return 0;
}

int
pthread_setcancelstate(int state, int *oldstate)
{
	return 0;
}

int
pthread_setcanceltype(int type, int *oldtype)
{
	return 0;
}

int
pthread_attr_destroy(pthread_attr_t *a)
{
	return 0;
}

int
pthread_attr_init(pthread_attr_t *a)
{
	memset(a, 0, sizeof(pthread_attr_t));
	a->stacksize = _PTHREAD_DEFSTKSZ;
	return 0;
}

int
pthread_attr_getstacksize(pthread_attr_t *a, size_t *sz)
{
	*sz = a->stacksize;
	return 0;
}

int
pthread_attr_setstacksize(pthread_attr_t *a, size_t sz)
{
	a->stacksize = sz;
	return 0;
}

/*
 * posix
 */

int
_posix_dups(const posix_spawn_file_actions_t *fa)
{
	int i;
	for (i = 0; i < fa->dup2slot; i++) {
		int ret;
		int from = fa->dup2s[i].from;
		int to = fa->dup2s[i].to;
		if ((ret = dup2(from, to)) < 0)
			return ret;
	}
	return 0;
}

static char *_environ[] = {"", NULL};

int
posix_spawn(pid_t *pid, const char *path, const posix_spawn_file_actions_t *fa,
    const posix_spawnattr_t *sa, char *const argv[], char *const envp[])
{
	if (sa)
		errx(-1, "spawnattr not supported");
	if (envp != NULL)
		if (envp != _environ || !envp[0] ||
		    envp[0][0] != '\0' || envp[1] != NULL)
			errx(-1, "environ not supported");
	pid_t p = fork();
	if (p < 0)
		return p;

	int child = !p;
	if (child) {
		if (fa) {
			if (_posix_dups(fa))
				errx(127, "posix_spawn dups failed");
		}
		execv(path, argv);
		errx(127, "posix_spawn exec failed");
	}

	if (pid)
		*pid = p;

	return 0;
}

int
posix_spawn_file_actions_adddup2(posix_spawn_file_actions_t *fa, int ofd, int newfd)
{
	if (ofd < 0 || newfd < 0)
		return -EINVAL;

	size_t nelms = sizeof(fa->dup2s)/sizeof(fa->dup2s[0]);
	int myslot = fa->dup2slot++;
	if (myslot < 0 || myslot >= nelms)
		errx(-1, "bad dup2slot: %d", myslot);

	fa->dup2s[myslot].from = ofd;
	fa->dup2s[myslot].to = newfd;
	return 0;
}

int
posix_spawn_file_actions_destroy(posix_spawn_file_actions_t *fa)
{
	return 0;
}

int
posix_spawn_file_actions_init(posix_spawn_file_actions_t *fa)
{
	memset(fa, 0, sizeof(posix_spawn_file_actions_t));
	return 0;
}

/*
 * libc
 */
void
abort(void)
{
	errx(-1, "abort");
}

int
atoi(const char *n)
{
	return (int)strtol(n, NULL, 10);
}

double
ceil(double x)
{
	if (x == trunc(x))
		return x;
	return trunc(x + 1);
}

int
creat(const char *p, mode_t m)
{
	return open(p, O_WRONLY | O_CREAT | O_TRUNC, m);
}

char *
ctime(const time_t *t)
{
	static char cbuf[64];
	return ctime_r(t, cbuf);
}

char *
ctime_r(const time_t *t, char *buf)
{
	printf("warning: ctime_r not implemented\n");
	char *msg = "i dont wish to write this";
	memmove(buf, msg, strlen(msg) + 1);
	return buf;
}

void
err(int eval, const char *fmt, ...)
{
	dolock = 0;
	fprintf(stderr, "%s: ", __progname);
	va_list ap;
	va_start(ap, fmt);
	vfprintf(stderr, fmt, ap);
	va_end(ap);
	// errno is dumb
	int neval = errno;
	char p[NL_TEXTMAX];
	strerror_r(neval, p, sizeof(p));
	fprintf(stderr, ": %s\n", p);
	exit(eval);
}

void
errx(int eval, const char *fmt, ...)
{
	dolock = 0;
	fprintf(stderr, "%s: ", __progname);
	va_list ap;
	va_start(ap, fmt);
	vfprintf(stderr, fmt, ap);
	va_end(ap);
	fprintf(stderr, "\n");
	exit(eval);
}

void
exit(int ret)
{
	if (fflush(NULL))
		fprintf(stderr, "warning: failed to fflush\n");
	_exit(ret);
}

int
fprintf(FILE *f, const char *fmt, ...)
{
	va_list ap;
	va_start(ap, fmt);
	int ret;
	ret = vfprintf(f, fmt, ap);
	va_end(ap);
	return ret;
}

static int
_ferror(FILE *f)
{
	return f->error;
}

int
ferror(FILE *f)
{
	pthread_mutex_lock(&f->mut);
	int ret = _ferror(f);
	pthread_mutex_unlock(&f->mut);
	return ret;
}

int
fileno(FILE *f)
{
	pthread_mutex_lock(&f->mut);
	int ret = f->fd;
	pthread_mutex_unlock(&f->mut);
	return ret;
}

static int
_feof(FILE *f)
{
	return f->eof;
}

int
feof(FILE *f)
{
	pthread_mutex_lock(&f->mut);
	int ret = _feof(f);
	pthread_mutex_unlock(&f->mut);
	return ret;
}

static struct {
	FILE *head;
	pthread_mutex_t mut;
} _allfiles;

static FILE  _stdin, _stdout, _stderr;
FILE  *stdin = &_stdin, *stdout = &_stdout, *stderr = &_stderr;

static void fdinit(void)
{
#define FDINIT(f, fdn, bt)	do {			\
	f.fd = fdn;					\
	f.p = &f.buf[0];				\
	f.end = f.p;					\
	f.lnext = _allfiles.head;			\
	f.lprev = NULL;					\
	f.btype = bt;					\
	if (pthread_mutex_init(&f.mut, NULL))		\
		pmsg("mutex init", 9);			\
	if (_allfiles.head)				\
		_allfiles.head->lprev = &f;		\
	_allfiles.head = &f;				\
	} while (0)

	FDINIT(_stdin, 0, _IONBF);
	FDINIT(_stdout, 1, _IONBF);
	FDINIT(_stderr, 2, _IONBF);
#undef FDINIT
	pthread_mutex_init(&_allfiles.mut, NULL);
}

static int
_fflush(FILE *f)
{
	if (!f->writing)
		return 0;
	char * const bst = &f->buf[0];
	size_t len = f->p - bst;
	ssize_t r;
	if ((r = write(f->fd, bst, len)) < 0) {
		f->error = errno;
		return EOF;
	} else if (r != len) {
		printf("asked: %zu, actual: %zu\n", len, r);
		return EOF;
	}
	f->p = f->end = bst;
	return 0;
}

int
fflush(FILE *f)
{
	if (f == NULL) {
		int ret = 0;
		pthread_mutex_lock(&_allfiles.mut);
		FILE *f = _allfiles.head;
		while (f != NULL) {
			ret |= fflush(f);
			f = f->lnext;
		}
		pthread_mutex_unlock(&_allfiles.mut);
		return ret;
	}

	pthread_mutex_lock(&f->mut);
	int ret = _fflush(f);
	pthread_mutex_unlock(&f->mut);
	return ret;
}

static FILE *
_fopen(const char *path, int gfd, const char *mode)
{
	int flags = -1;
	int plus = strchr(mode, '+') != NULL;
	int x = strchr(mode, 'x') != NULL;
	switch (mode[0]) {
	case 'r':
		if (plus)
			flags = O_RDWR;
		else
			flags = O_RDONLY;
		break;
	case 'w':
		if (plus)
			flags = O_RDWR | O_CREAT | O_TRUNC;
		else
			flags = O_WRONLY | O_CREAT | O_TRUNC;
		if (x)
			flags |= O_EXCL;
		break;
	case 'a':
		if (plus)
			flags = O_RDWR | O_CREAT | O_APPEND;
		else
			flags = O_WRONLY | O_CREAT | O_APPEND;
		if (x)
			flags |= O_EXCL;
		break;
	default:
		errno = EINVAL;
		return NULL;
	}

	if (strchr(mode, 'e') != NULL)
		flags |= O_CLOEXEC;

	FILE *ret = malloc(sizeof(FILE));
	if (ret == NULL) {
		errno = ENOMEM;
		return NULL;
	}
	memset(ret, 0, sizeof(FILE));

	int fd = gfd;
	if (path)
		fd = open(path, flags);
	else
		if (fd < 0)
			errno = EBADF;
	if (fd < 0) {
		free(ret);
		return NULL;
	}

	pthread_mutex_init(&ret->mut, NULL);
	ret->fd = fd;
	ret->p = &ret->buf[0];
	ret->end = ret->p;
	// POSIX: fully-buffered iff is not an interactive device
	ret->btype = _IONBF;
	struct stat st;
	if (fstat(ret->fd, &st) == 0 && S_ISREG(st.st_mode))
		ret->btype = _IOFBF;

	// insert into global list
	pthread_mutex_lock(&_allfiles.mut);
	ret->lnext = _allfiles.head;
	if (_allfiles.head)
		_allfiles.head->lprev = ret;
	_allfiles.head = ret;
	pthread_mutex_unlock(&_allfiles.mut);
	return ret;
}

FILE *
fdopen(int fd, const char *mode)
{
	return _fopen(NULL, fd, mode);
}

FILE *
fopen(const char *path, const char *mode)
{
	return _fopen(path, -1, mode);
}

int
fclose(FILE *f)
{
	// remove from global list
	pthread_mutex_lock(&_allfiles.mut);
	if (f->lnext)
		f->lnext->lprev = f->lprev;
	if (f->lprev)
		f->lprev->lnext = f->lnext;
	if (_allfiles.head == f)
		_allfiles.head = f->lnext;
	pthread_mutex_unlock(&_allfiles.mut);

	pthread_mutex_lock(&f->mut);
	_fflush(f);

	// try to crash buggy programs that use f after us
	f->fd = -1;
	f->p = (void *)1;
	f->end = NULL;
	pthread_mutex_unlock(&f->mut);

	pthread_mutex_destroy(&f->mut);
	free(f);

	return 0;
}

static size_t
_fread1(void *dst, size_t left, FILE *f)
{
	if (!(f->btype == _IOFBF || f->btype == _IONBF))
		errx(-1, "noimp");
	// unbuffered stream
	if (f->btype == _IONBF) {
		ssize_t r = read(f->fd, dst, left);
		if (r < 0) {
			f->error = errno;
			return 0;
		} else if (r == 0)
			f->eof = 1;
		return r;
	}

	// switch to read mode
	if (_fflush(f))
		return 0;
	f->writing = 0;

	// fill buffer?
	if (f->p == f->end && !_feof(f) && !_ferror(f)) {
		char * const bst = &f->buf[0];
		size_t r;
		if ((r = read(f->fd, bst, sizeof(f->buf))) < 0) {
			f->error = errno;
			return 0;
		} else if (r == 0) {
			f->eof = 1;
			return 0;
		}
		f->p = bst;
		f->end = bst + r;
	}

	size_t ub = MIN(left, f->end - f->p);
	memmove(dst, f->p, ub);
	f->p += ub;

	return ub;
}

size_t
fread(void *dst, size_t sz, size_t mem, FILE *f)
{
	if (sz == 0)
		return 0;
	if (mem > ULONG_MAX / sz) {
		errno = -EOVERFLOW;
		return 0;
	}

	pthread_mutex_lock(&f->mut);

	size_t ret = 0;
	size_t left = mem * sz;
	while (left > 0 && !_feof(f) && !_ferror(f)) {
		size_t c = _fread1(dst, left, f);
		//if (c == 0)
		//	break;
		dst += c;
		left -= c;
		ret += c;
	}

	pthread_mutex_unlock(&f->mut);
	return ret/sz;
}

static size_t
_fwrite1(const void *src, size_t left, FILE *f)
{
	if (!(f->btype == _IOFBF || f->btype == _IONBF))
		errx(-1, "noimp");
	// unbuffered stream
	if (f->btype == _IONBF) {
		ssize_t r = write(f->fd, src, left);
		if (r < 0) {
			f->error = errno;
			return 0;
		}
		return r;
	}

	char * const bst = &f->buf[0];
	char * const bend = bst + sizeof(f->buf);

	// switch to write mode (discarding unread data and using undefined
	// offset)
	if (!f->writing) {
		f->writing = 1;
		f->p = f->end = bst;
	}

	// buffer full?
	if (f->p == bend) {
		if (_fflush(f))
			return 0;
		f->p = f->end = bst;
	}
	size_t put = MIN(bend - f->p, left);
	memmove(f->p, src, put);
	f->p += put;
	f->end = MAX(f->p, f->end);
	return put;
}

size_t
fwrite(const void *src, size_t sz, size_t mem, FILE *f)
{
	if (sz == 0)
		return 0;
	if (mem > ULONG_MAX / sz) {
		errno = -EOVERFLOW;
		return 0;
	}

	pthread_mutex_lock(&f->mut);

	size_t ret = 0;
	size_t left = mem * sz;
	while (left > 0 && !_ferror(f)) {
		size_t c = _fwrite1(src, left, f);
		src += c;
		left -= c;
		ret += c;
	}

	pthread_mutex_unlock(&f->mut);
	return ret/sz;
}

static int
_fgetc(FILE *f)
{
	char buf;
	if (_fread1(&buf, 1, f) != 1)
		return EOF;
	return buf;
}

int
fgetc(FILE *f)
{
	pthread_mutex_lock(&f->mut);
	int ret = _fgetc(f);
	pthread_mutex_unlock(&f->mut);
	return ret;
}

static int
_ungetc(int c, FILE *f)
{
	const char * const bst = &f->buf[0];
	if (f->p == bst)
		return EOF;
	unsigned char put = (unsigned char)c;
	f->p--;
	*f->p = put;
	f->eof = 0;
	return put;
}

int
ungetc(int c, FILE *f)
{
	if (c == EOF)
		return c;

	pthread_mutex_lock(&f->mut);
	int ret = _ungetc(c, f);
	pthread_mutex_unlock(&f->mut);
	return ret;
}

char *
fgets(char *buf, int size, FILE *f)
{
	if (size == 0)
		return NULL;
	// get room for nul
	size--;
	pthread_mutex_lock(&f->mut);

	char *ret = NULL;
	size_t c = 0;
	while (c < size) {
		int ch = _fgetc(f);
		if (ch == EOF) {
			if (c > 0)
				ret = buf;
			goto out;
		}
		buf[c++] = ch;
		if (ch == '\n')
			break;
	}
	ret = buf;
out:
	buf[c] = '\0';
	pthread_mutex_unlock(&f->mut);
	return ret;
}

off_t
ftello(FILE *f)
{
	return lseek(f->fd, 0, SEEK_CUR);
}

int
ftruncate(int fd, off_t newlen)
{
	int ret = syscall(SA(fd), SA(newlen), 0, 0, 0, SYS_FTRUNC);
	ERRNO_NZ(ret);
	return ret;
}

int
futex(const int op, void *fut, void *fut2, int aux, const struct timespec *ts)
{
	int ret = syscall(SA(op), SA(fut), SA(fut2), SA(aux), SA(ts),
	    SYS_FUTEX);
	ERRNO_NEG(ret);
	return ret;
}

int
fsync(int fd)
{
	sync();
	return 0;
}

static void
_gcfrac(struct gcfrac_t *r)
{
	struct timeval tv;
	if (gettimeofday(&tv, NULL))
		err(-1, "gettimeofday");
	long nowms = tv.tv_sec*1000 + tv.tv_usec/1000;
	long gcwork = sys_info(SINFO_GCMS);
	long markt = sys_info(SINFO_GCMARKTIME);
	long sweept = sys_info(SINFO_GCSWEEPTIME);
	long wbtime = sys_info(SINFO_GCWBARTIME);
	if (gcwork == -1 || markt == -1 || sweept == -1 || wbtime == -1)
		err(-1, "sysinfo");

	r->startms = nowms;
	r->gcworkms = gcwork;
	r->details.wbms = wbtime;
	r->details.bgsweepms = sweept;
	r->details.markms = markt;
}

struct gcfrac_t
gcfracst(void)
{
	struct gcfrac_t ret;
	_gcfrac(&ret);
	return ret;
}

double
gcfracend(struct gcfrac_t *of, long *marke, long *sweepe,
    long *wbe)
{
	struct gcfrac_t nf;
	_gcfrac(&nf);
	if (marke)
		*marke = nf.details.markms - of->details.markms;
	if (sweepe)
		*sweepe = nf.details.bgsweepms - of->details.bgsweepms;
	if (wbe)
		*wbe = nf.details.wbms - of->details.wbms;
	//return (double)(nf.gcworkms - of->gcworkms)/(nf.startms - of->startms);
	return NAN;
}

char *optarg;
int   optind = 1;

int
getopt(int argc, char * const *argv, const char *optstring)
{
	optarg = NULL;
	for (; optind < argc && !argv[optind]; optind++)
		;
	if (optind >= argc)
		return -1;
	char *ca = argv[optind];
	if (ca[0] != '-')
		return -1;
	optind++;
	const char wut = '?';
	char *o = strchr(optstring, ca[1]);
	if (!o)
		return wut;
	int needarg = o[1] == ':';
	if (!needarg)
		return ca[1];
	if (ca[2] != '\0') {
		optarg = &ca[2];
		return ca[1];
	}
	const char argwut = optstring[0] == ':' ? ':' : wut;
	if (optind >= argc || argv[optind][0] == '-')
		return argwut;
	optarg = argv[optind];
	optind++;
	return ca[1];
}

int
islower(int c)
{
	return c >= 'a' && c <= 'z';
}

int
isupper(int c)
{
	return c >= 'A' && c <= 'Z';
}

int
isalpha(int c)
{
	return isupper(c) || islower(c);
}

int
isdigit(int c)
{
	return c >= '0' && c <= '9';
}

int
isprint(int c)
{
	return isalpha(c) || isdigit(c) || ispunct(c);
}

static char _punct[255] = {
	['!'] = 1, ['"'] = 1, ['#'] = 1, ['$'] = 1, ['%'] = 1, ['&'] = 1,
	['\'']= 1, ['('] = 1, [')'] = 1, ['*'] = 1, ['+'] = 1, [','] = 1,
	['-'] = 1, ['.'] = 1, ['/'] = 1, [':'] = 1, [';'] = 1, ['<'] = 1,
	['='] = 1, ['>'] = 1, ['?'] = 1, ['@'] = 1, ['['] = 1, ['\\']= 1,
	[']'] = 1, ['^'] = 1, ['_'] = 1, ['`'] = 1, ['{'] = 1, ['|'] = 1,
	['}'] = 1, ['~'] = 1,
};

int
ispunct(int c)
{
	return _punct[(unsigned char)c];
}

int
isspace(int c)
{
	return c == ' ' || c == '\f' || c == '\n' || c == '\t' || c == '\v' ||
	    c == '\r';
}

int
isxdigit(int c)
{
	int d = tolower(c);
	return isdigit(c) || (d >= 'a' && d <= 'f');
}

double
log(double x)
{
	if (x < 1)
		errx(-1, "no imp");
	const double ln2 = 0.693147;
	double ret = 0;
	while (x >= 2) {
		x /= 2;
		ret += ln2;
	}
	if (x > 1)
		ret += x - 1;
	return ret;
}

dev_t
makedev(uint maj, uint min)
{
	return (ulong)maj << 32 | min;
}

int
memcmp(const void *q, const void *p, size_t sz)
{
	const long *e1 = (long *)q;
	const long *e2 = (long *)p;
	while (sz >= 8 && *e1 == *e2)
		sz -= 8, e1++, e2++;

	const char *s1 = (char *)e1;
	const char *s2 = (char *)e2;
	while (sz && *s1 == *s2)
		sz--, s1++, s2++;
	if (!sz)
		return 0;
	return *s1 - *s2;
}

void *
memcpy(void *dst, const void *src, size_t n)
{
	return memmove(dst, src, n);
}

void *
memmove(void *dst, const void *src, size_t n)
{
	char *d = dst;
	const char *s = src;
	if (d == s || n == 0)
		return d;
	if (d > s && d <= s + n) {
		// copy backwards
		s += n;
		d += n;
		if (n >= 8) {
			long *dst8 = (long *)d;
			const long *src8 = (const long *)s;
			for (; n >= 8; n -= 8)
				*--dst8 = *--src8;
			d = (char *)dst8;
			s = (const char *)src8;
		}
		while (n--)
			*--d = *--s;
		return d;
	}
	if (n >= 8) {
		long *dst8 = dst;
		const long *src8 = src;
		for (; n >= 8; n -= 8)
			*dst8++ = *src8++;
		d = (char *)dst8;
		s = (const char *)src8;
	}
	while (n--)
		*d++ = *s++;
	return dst;
}

void *
memset(void *d, int c, size_t n)
{
	if (n == 0)
		return d;
	long *pl = d;
	if (n >= 8) {
		ulong c8 = (uchar)c;
		c8 = c8 | c8 << 8;
		c8 |= c8 << 16;
		c8 |= c8 << 32;
		for (; n >= 8; n -= 8)
			*pl++ = c8;
	}
	char *pc = (char *)pl;
	char v = (char)c;
	while (n--)
		*pc++ = v;
	return d;
}

int
_mkstemp(char *t, int wantdir)
{
	// XXX
	static uint seed;
	if (seed == 0)
		seed = (uint)time(NULL);
	size_t l = strlen(t);
	if (l < 6)
		goto inval;
	char *oe = t + l - 6;
	if (strncmp(oe, "XXXXXX", 6) != 0)
		goto inval;
	int fails = 0;
	for (;;) {
		char *e = oe;
		int i;
		for (i = 0; i < 6; i++) {
			uint n = rand_r(&seed) % 26*2;
			if (n >= 26)
				*e = 'A' + n - 26;
			else
				*e = 'a' + n;
			e++;
		}
		if (wantdir) {
			if (mkdir(t, 0700) == 0)
				return 0;
		} else {
			int fd = open(t, O_RDWR | O_CREAT | O_EXCL, 0600);
			if (fd != -1)
				return fd;
		}
		if (errno != EEXIST)
			return -1;
		else if (fails++ == 10)
			return -1;
	}
inval:
	errno = EINVAL;
	return -1;
}

int
mkstemp(char *t)
{
	return _mkstemp(t, 0);
}

char *
mkdtemp(char *t)
{
	if (_mkstemp(t, 1) != 0)
		return NULL;
	return t;
}

DIR *
fdopendir(int fd)
{
        #define BSIZE 4096
  
	struct stat st;
	if (fstat(fd, &st) == -1)
		return NULL;
	if (!S_ISDIR(st.st_mode)) {
		errno = ENOTDIR;
		return NULL;
	}
	// allocate enough space for all dirents; count them.
	if (lseek(fd, 0, SEEK_SET) == -1)
		return NULL;
	// on disk format of directory entries
	struct  __attribute__((packed)) _dirent_t {
		char	name[_POSIX_NAME_MAX];
		ulong	inum;
	};
	const int nde = BSIZE/sizeof(struct _dirent_t);
	size_t c = 0;
	char buf[BSIZE];
	ssize_t r;
	while ((r = read(fd, buf, sizeof(buf))) > 0) {
		int i;
		for (i = 0; i < nde; i++) {
			struct _dirent_t *de = (struct _dirent_t *)buf + i;
			if (de->name[0] != '\0')
				c++;
		}
	}
	if (r == -1)
		return NULL;
	if (c > (ULONG_MAX - sizeof(DIR))/sizeof(struct dirent))
		errx(-1, "very fat dir");
	DIR *ret = malloc(sizeof(DIR) + c*sizeof(struct dirent));
	if (!ret)
		return NULL;

	// copy dirents into ret
	ret->fd = fd;
	ret->nent = (int)c;
	ret->cent = 0;
	if (lseek(fd, 0, SEEK_SET) == -1)
		goto out;
	struct dirent *p = &ret->dents[0];
	while ((r = read(fd, buf, sizeof(buf))) > 0) {
		int i;
		for (i = 0; i < nde; i++) {
			struct _dirent_t *de = (struct _dirent_t *)buf + i;
			if (de->name[0] != '\0') {
				p->d_ino = de->inum;
				strncpy(p->d_name, de->name,
				    sizeof(p->d_name));
				p++;
			}
		}
	}
	if (r == -1)
		goto out;
	return ret;
out:
	free(ret);
	return NULL;
}

DIR *
opendir(const char *path)
{
	int fd = open(path, O_RDONLY | O_DIRECTORY);
	if (fd == -1)
		return NULL;
	return fdopendir(fd);
}

int
closedir(DIR *d)
{
	int fd = d->fd;
	free(d);
	return close(fd);
}

struct dirent *
readdir(DIR *d)
{
	static struct dirent _ret;
	struct dirent *ret;
	readdir_r(d, &_ret, &ret);
	return ret;
}

// POSIX: returns error number, not -1
int
readdir_r(DIR *d, struct dirent *entry, struct dirent **ret)
{
	if (d->cent == d->nent) {
		*ret = NULL;
		return 0;
	}
	struct dirent *src = &d->dents[d->cent++];
	*entry = *src;
	*ret = entry;
	return 0;
}

void
rewinddir(DIR *d)
{
	d->cent = 0;
}

struct {
	const char *prefix;
	int pid;
	int ndelay;
	int fac;
	ulong mask;
} _slogopts = {.fac = LOG_USER, .mask = -1};

void
openlog(const char *ident, int logopt, int fac)
{
	_slogopts.prefix = ident;
	_slogopts.fac = fac;
	if (logopt & LOG_PID)
		_slogopts.pid = 1;
	if (logopt & LOG_NDELAY)
		_slogopts.ndelay = 1;
}

size_t
strlen(const char *msg)
{
	size_t ret = 0;
	while (*msg++)
		ret++;
	return ret;
}

int
strcmp(const char *s1, const char *s2)
{
	while (*s1 && *s1 == *s2)
		s1++, s2++;
	return *s1 - *s2;
}

int
strcoll(const char *s1, const char *s2)
{
	return strcmp(s1, s2);
}

int
strncmp(const char *s1, const char *s2, size_t n)
{
	while (n && *s1 && *s1 == *s2)
		n--, s1++, s2++;
	if (n == 0)
		return 0;
	return *s1 - *s2;
}

static void
pmsg(char *msg, long sz)
{
	write(1, msg, sz);
}

static int
wc(char *p, char *end, char c)
{
	if (p < end) {
		*p = c;
		return 1;
	}
	return 0;
}

static char
numtoch(char n)
{
	char c = n;
	if (n < 10)
		c += '0';
	else
		c += 'a' - 10;
	return c;
}

static int
putn(char *p, char *end, ulong n, int base)
{
	if (n == 0) {
		wc(p, end, '0');
		return 1;
	}
	char buf[21];
	int i = 0;
	while (n) {
		int left = n % base;
		buf[i++] = numtoch(left);
		n /= base;
	}
	int ret = i;
	while (i--)
		p += wc(p, end, buf[i]);
	return ret;
}

static int
_vprintf(const char *fmt, va_list ap, char *dst, char *end)
{
	const char *start = dst;
	char c;

	c = *fmt;
	while (c && dst < end) {
		if (c != '%') {
			dst += wc(dst, end, c);
			fmt++;
			c = *fmt;
			continue;
		}

		fmt++;
		int prehex = 0;
		int done = 0;
		int longmode = 0;
		int sig = 1;
		int precmode = 0;
		int prec = 6;
		int minwidth = 0;
		while (!done && *fmt && !isspace(*fmt)) {
			char t = *fmt;
			fmt++;
			if (isdigit(t) || t == '-') {
				char *nend;
				long num = strtol(fmt - 1, &nend, 10);
				if (precmode) {
					if (num > 0 || t == '0') {
						prec = num;
						fmt = nend;
						continue;
					}
				} else {
					minwidth = num;
					fmt = nend;
					continue;
				}
			}
			switch (t) {
			case '%':
				dst += wc(dst, end, '%');
				done = 1;
				break;
			case '#':
				prehex = 1;
				break;
			case '.':
				precmode = 1;
				break;
			case 'z':
			case 'l':
				longmode = 1;
				break;
			case 'u':
				sig = 0;
			case 'd':
			{
				ulong n;
				if (longmode)
					n = va_arg(ap, ulong);
				else
					n = (ulong)(long)va_arg(ap, int);
				if (sig && (long)n < 0) {
					dst += wc(dst, end, '-');
					n = -n;
				}
				dst += putn(dst, end, n, 10);
				done = 1;
				break;
			}
			case 'g':
			case 'f':
			{
				double n;
				// floats are promoted to double when used for
				// a ... argument
				n = va_arg(ap, double);
				if (isnan(n)) {
					dst += wc(dst, end, 'n');
					dst += wc(dst, end, 'a');
					dst += wc(dst, end, 'n');
					done = 1;
					break;
				}
				if (isinf(n)) {
					dst += wc(dst, end, 'i');
					dst += wc(dst, end, 'n');
					dst += wc(dst, end, 'f');
					done = 1;
					break;
				}
				if (n < 0) {
					dst += wc(dst, end, '-');
					n = -n;
				}
				dst += putn(dst, end, (ulong)n, 10);
				if (prec == 0) {
					done = 1;
					break;
				}
				dst += wc(dst, end, '.');
				n -= (ulong)n;
				for (; prec > 0; prec--) {
					n *= 10;
					dst += putn(dst, end, (ulong)n, 10);
					n -= (ulong)n;
				}
				done = 1;
				break;
			}
			case 'p':
				longmode = 1;
				prehex = 1;
			case 'x':
			{
				if (prehex) {
					dst += wc(dst, end, '0');
					dst += wc(dst, end, 'x');
				}
				ulong n;
				if (longmode)
					n = va_arg(ap, ulong);
				else
					n = (ulong)(uint)va_arg(ap, int);
				dst += putn(dst, end, n, 16);
				done = 1;
				break;
			}
			case 'c':
				dst += wc(dst, end, (char)va_arg(ap, int));
				done = 1;
				break;
			case 's':
			{
				char *s = va_arg(ap, char *);
				if (!s) {
					dst += wc(dst, end, '(');
					dst += wc(dst, end, 'n');
					dst += wc(dst, end, 'i');
					dst += wc(dst, end, 'l');
					dst += wc(dst, end, ')');
					done = 1;
					break;
				}
				int slen = strlen(s);
				if (minwidth > 0 && minwidth > slen)
					minwidth -= slen;
				else if (minwidth < 0 && -minwidth > slen)
					minwidth += slen;
				else
					minwidth = 0;
				for (; minwidth > 0; minwidth--)
					dst += wc(dst, end, ' ');
				while (*s && (!precmode || prec-- > 0))
					dst += wc(dst, end, *s++);
				for (; minwidth < 0; minwidth++)
					dst += wc(dst, end, ' ');
				done = 1;
				break;
			}
			default:
				//errx(-1, "unsupported printf format: %c\n", t);
				dst += wc(dst, end, '?');
				break;
			}
		}
		c = *fmt;
		prehex = 0;
	}

	if (dst > end)
		dst = end - 1;
	*dst = '\0';
	return dst - start;
}

int
vsnprintf(char *dst, size_t sz, const char *fmt, va_list ap)
{
	return _vprintf(fmt, ap, dst, dst + sz);
}

static int
_vscanf(const char *src, const char * const end, const char *fmt, va_list ap)
{
	int ret = 0;
	while (*fmt && src < end) {
		if (isspace(*fmt)) {
			fmt++;
			while (isspace(*src))
				src++;
			continue;
		}
		if (*fmt != '%') {
			if (*src != *fmt)
				return ret;
			fmt++;
			src++;
			continue;
		}
		fmt++;
		int done = 0;
		int longmode = 0;
		int hexmode = 0;
		while (!done) {
			switch (*fmt) {
			case '%':
				if (*src != '%')
					return ret;
				src++;
				done = 1;
				break;
			case 'l':
				longmode = 1;
				break;
			case 'x':
				hexmode = 1;
			case 'u':
			case 'd':
			{
				char *eptr;
				long read = strtol(src, &eptr, hexmode ? 16 : 10);
				if (src == eptr)
					return ret;
				if (longmode) {
					 long *p = va_arg(ap, long *);
					 *p = read;
				} else {
					 int *p = va_arg(ap, int *);
					 *p = (int)read;
				}
				src = eptr;
				done = 1;
				ret++;
			}
				break;
			case 'g':
			case 'f':
			{
				char *eptr;
				long double read = strtold(src, &eptr);
				if (src == eptr)
					return ret;
				if (longmode) {
					 double *p = va_arg(ap, double *);
					 *p = (double)read;
				} else {
					 float *p = va_arg(ap, float *);
					 *p = (float)read;
				}
				src = eptr;
				done = 1;
				ret++;
			}
				break;
			default:
				errx(-1, "unsupported scanf format: %c", *fmt);
			}
			fmt++;
		}
	}
	return ret;
}

int
vsscanf(const char *src, const char *fmt, va_list ap)
{
	const char * const end = strchr(src, '\0');
	if (!end)
		errx(-1, "no null");
	return _vscanf(src, end, fmt, ap);
}

int
printf(const char *fmt, ...)
{
	va_list ap;
	int ret;

	va_start(ap, fmt);
	ret = vfprintf(stdout, fmt, ap);
	va_end(ap);

	return ret;
}

double
pow(double x, double y)
{
	long exp = (long)y;
	if (y - exp != 0)
		errx(-1, "shitty pow(3) only accepts integer exponents");
	if (exp == 0)
		return 1;
	int inv = 0;
	if (exp < 0) {
		exp = -exp;
		inv = 1;
	}
	double ret = 1;
	while (exp--)
		ret *= x;
	if (inv)
		return 1.0/ret;
	return ret;
}

void
perror(const char *str)
{
	char *e = strerror(errno);
	if (str)
		fprintf(stderr, "%s: %s\n", str, e);
	else
		fprintf(stderr, "%s\n", e);
}

static void _qsort(void *_base, size_t elms, size_t esz,
    int (*cmp)(const void *, const void *), char *scratch, char *pivot)
{
	if (elms <= 1)
		return;
	char *base = _base;
	if (elms == 2) {
		if (cmp(base, base+esz) > 0) {
			memmove(pivot, base, esz);
			memmove(base, base+esz, esz);
			memmove(base+esz, pivot, esz);
		}
		return;
	}
	// the first element is the pivot
	memmove(pivot, base, esz);
	// sort to scratch
	char *lh, *hh;
	size_t l, h;
	l = h = 0;
	lh = scratch;
	hh = (char *)scratch + (elms - 1)*esz;
	size_t i;
	char *cur = (char *)base + esz;
	for (i = 1; i < elms; i++, cur += esz) {
		int c = cmp(cur, pivot);
		if (c < 0) {
			memmove(lh, cur, esz);
			lh += esz;
			l++;
		} else {
			memmove(hh, cur, esz);
			hh -= esz;
			h++;
		}
	}
	//copy scratch back to base
	void *plow = base;
	memmove(plow, scratch, l*esz);

	void *piv = base + l*esz;
	memmove(piv, pivot, esz);

	void *phigh = piv + esz;
	size_t tail = elms - h;
	memmove(phigh, scratch + tail*esz, h*esz);

	_qsort(plow,  l, esz, cmp, scratch, pivot);
	_qsort(phigh, h, esz, cmp, scratch, pivot);
}

void
qsort(void *base, size_t elms, size_t esz,
    int (*cmp)(const void *, const void *))
{
	if (elms < 0)
		return;
	// +1 for pivot
	char *scratch = calloc(elms + 1, esz);
	if (scratch == NULL)
		errx(-1, "qsort calloc");
	char *pivot = (char *)scratch + elms*esz;
	_qsort(base, elms, esz, cmp, scratch, pivot);
	free(scratch);
}

uint _seed1;

int
rand(void)
{
	return rand_r(&_seed1);
}

int
rand_r(uint *seed)
{
	*seed = *seed * 1103515245 + 12345;
	return *seed & RAND_MAX;
}

void
srand(uint seed)
{
	_seed1 = seed;
}

uint _seed2;

// POSIX requires that random(3)'s range is [0, 2**31)
long
random(void)
{
	return rand_r(&_seed2);
}

void
srandom(uint seed)
{
	_seed2 = seed;
}

int
sscanf(const char *src, const char *fmt, ...)
{
	va_list ap;
	va_start(ap, fmt);
	int ret = vsscanf(src, fmt, ap);
	va_end(ap);

	return ret;
}

ulong
rdtsc(void)
{
	ulong low, hi;
	asm volatile(
	    "rdtsc\n"
	    : "=a"(low), "=d"(hi)
	    :
	    :);
	return hi << 32 | low;
}

static char readlineb[256];

char *
readline(const char *prompt)
{
	if (prompt)
		printf("%s", prompt);
	int ret;
	int i = 0;
	char c = 0x41;
	// XXX
	while ((ret = read(0, &c, 1)) > 0) {
		if (c == '\n')
			break;
		if (c == '\b') {
			if (--i < 0)
				i = 0;
			continue;
		}
		if (i < sizeof(readlineb) - 1)
			readlineb[i++] = c;
	}
	if (ret < 0)
		printf("readline: read failed: %d\n", ret);
	if (ret == 0)
		return NULL;
	readlineb[i] = 0;
	return readlineb;
}

int
setenv(const char *k, const char *v, int overwrite)
{
	printf("setenv: no environment yet\n");
	errno = ENOSYS;
	return -1;
}

static inline int _fdisset(int fd, fd_set *fds)
{
	if (fds)
		return FD_ISSET(fd, fds);
	return 0;
}

int
select(int maxfd, fd_set *rfds, fd_set *wfds, fd_set *efds,
    struct timeval *timeout)
{
	int nfds = 0;
	int i;
	for (i = 0; i < maxfd; i++)
		if (_fdisset(i, rfds) || _fdisset(i, wfds) ||
		    _fdisset(i, efds))
			nfds++;
	struct pollfd pfds[nfds];
	int curp = 0;
	for (i = 0; i < maxfd; i++) {
		if ((_fdisset(i, rfds) || _fdisset(i, wfds) ||
		    _fdisset(i, efds)) == 0)
			continue;
		pfds[curp].fd = i;
		pfds[curp].events = 0;
		if (_fdisset(i, rfds))
			pfds[curp].events |= POLLIN;
		if (_fdisset(i, wfds))
			pfds[curp].events |= POLLOUT;
		if (_fdisset(i, efds))
			pfds[curp].events |= POLLERR;
		curp++;
	}
	int toms = -1;
	if (timeout)
		toms = timeout->tv_sec * 1000 + timeout->tv_usec / 1000;

	// account for differences between select(2) and poll(2):
	// 1. poll's HUP counts as a read-ready fd for select
	// 2. an invalid fd is an error for select
	// 3. select returns the number of bits set in the fdsets, not the
	// number of ready fds.
	if (rfds)
		FD_ZERO(rfds);
	if (wfds)
		FD_ZERO(wfds);
	if (efds)
		FD_ZERO(efds);
	int ret = poll(pfds, nfds, toms);
	if (ret <= 0)
		return ret;

	int selret = 0;
	for (i = 0; i < nfds; i++) {
		if (pfds[i].events & POLLNVAL) {
			errno = EINVAL;
			return -1;
		}

#define _PTOS(pf, fds)	do { \
				if ((pfds[i].revents & pf) &&	\
				    pfds[i].events & pf) {	\
					FD_SET(pfds[i].fd, fds); \
					selret++;	\
				}	\
			} while (0)

		_PTOS(POLLIN, rfds);
		_PTOS(POLLHUP, rfds);
		_PTOS(POLLOUT, wfds);
		_PTOS(POLLERR, efds);
#undef _PTOS
	}
	return selret;
}

char *
setlocale(int cat, const char *loc)
{
	// null loc to fetch current locale
	if (!loc || strcmp(loc, "") == 0 || strcmp(loc, "C") == 0 ||
	    strcmp(loc, "POSIX") == 0)
		return "C";
	errx(-1, "no fancy locale support");
}

uint
sleep(uint s)
{
	struct timespec t = {s, 0};
	struct timespec r = {0, 0};
	int ret = nanosleep(&t, &r);
	if (ret)
		return (uint)r.tv_sec;
	return 0;
}

int
snprintf(char *dst, size_t sz, const char *fmt, ...)
{
	va_list ap;
	int ret;

	va_start(ap, fmt);
	ret = vsnprintf(dst, sz, fmt, ap);
	va_end(ap);

	return ret;
}

int
sprintf(char *dst, const char *fmt, ...)
{
	size_t dur = (uintptr_t)-1 - (uintptr_t)dst;

	va_list ap;
	int ret;

	va_start(ap, fmt);
	ret = vsnprintf(dst, dur, fmt, ap);
	va_end(ap);

	return ret;
}

int
strcasecmp(const char *s1, const char *s2)
{
	return strncasecmp(s1, s2, ULONG_MAX);
}

int
strncasecmp(const char *s1, const char *s2, size_t n)
{
	uint a = tolower(*s1++);
	uint b = tolower(*s2++);
	while (n && a && a == b) {
		n--;
		a = tolower(*s1++);
		b = tolower(*s2++);
	}
	if (n == 0)
		return 0;
	return a - b;
}

char *
strchr(const char *big, int l)
{
	for (; *big; big++)
		if (*big == l)
			return (char *)big;
	if (l == '\0')
		return (char *)big;
	return NULL;
}

char *
strdup(const char *orig)
{
	size_t l = strlen(orig) + 1;
	char *ret = malloc(l);
	if (!l) {
		errno = ENOMEM;
		return NULL;
	}
	memmove(ret, orig, l);
	return ret;
}

static const char * const _errstr[] = {
	[EPERM] = "Operation not permitted",
	[ENOENT] = "No such file or directory",
	[ESRCH] = "No such process",
	[EINTR] = "Interrupted system call",
	[EIO] = "Input/output error",
	[E2BIG] = "Argument list too long",
	[EBADF] = "Bad file descriptor",
	[EAGAIN] = "Resource temporarily unavailable",
	[ECHILD] = "No child processes",
	[ENOMEM] = "Out of memory",
	[EACCES] = "Permission denied",
	[EFAULT] = "Bad address",
	[EBUSY] = "Device busy",
	[EEXIST] = "File exists",
	[EXDEV] = "Cross device link",
	[ENODEV] = "Operation not supported by device",
	[ENOTDIR] = "Not a directory",
	[EISDIR] = "Is a directory",
	[EINVAL] = "Invalid argument",
	[ENFILE] = "Too many open files in system",
	[EMFILE] = "Too many open files",
	[ENOSPC] = "No space left on device",
	[ESPIPE] = "Illegal seek",
	[EPIPE] = "Broken pipe",
	[ERANGE] = "Result too large",
	[ENAMETOOLONG] = "File name too long",
	[ENOSYS] = "Function not implemented",
	[ENOTEMPTY] = "Directory not empty",
	[EDESTADDRREQ] = "Destination address required",
	[EAFNOSUPPORT] = "Protocol family not supported",
	[EADDRINUSE] = "Address already in use",
	[EADDRNOTAVAIL] = "Can't assign requested address",
	[ENETDOWN] = "Network is down",
	[ENETUNREACH] = "Network is unreachable",
	[ECONNABORTED] = "Software caused connection abort",
	[ELOOP] = "Too many levels of symbolic links",
	[EHOSTDOWN] = "Host is down",
	[EHOSTUNREACH] = "No route to host",
	[EOVERFLOW] = "Value too large to be stored in data type",
	[ENOTSOCK] = "Socket operation on non-socket",
	[EOPNOTSUPP] = "Operation not supported",
	[EISCONN] = "Socket is already connected",
	[ENOTCONN] = "Socket is not connected",
	[ETIMEDOUT] = "Operation timed out",
	[ECONNRESET] = "Connection reset by peer",
	[ECONNREFUSED] = "Connection refused",
	[EINPROGRESS] = "Operation now in progress",
};

char *
strerror(int errnum)
{
	int nents = sizeof(_errstr)/sizeof(_errstr[0]);
	const char *p = "Unknown error";
	if (errnum >= 0 && errnum < nents && _errstr[errnum] != NULL)
		p = _errstr[errnum];
	return (char *)p;
}

int
strerror_r(int errnum, char *dst, size_t dstlen)
{
	int nents = sizeof(_errstr)/sizeof(_errstr[0]);
	const char *p = "Unknown error";
	if (errnum >= 0 && errnum < nents && _errstr[errnum] != NULL)
		p = _errstr[errnum];
	strncpy(dst, p, dstlen);
	return 0;
}

char *
strncpy(char *dst, const char *src, size_t sz)
{
	snprintf(dst, sz, "%s", src);
	return dst;
}

char *
strstr(const char *big, const char *little)
{
	while (*big) {
		if (*big == *little) {
			const char *guess = big;
			const char *l = little;
			while (*big) {
				if (*l == 0)
					return (char *)guess;
				if (*big != *l)
					break;
				big++;
				l++;
			}
			if (*big == 0 && *l == 0)
				return (char *)guess;
		} else
			big++;
	}
	return NULL;
}

void
syslog(int priority, const char *msg, ...)
{
	if (((priority & LOG_ALL) & _slogopts.mask) == 0)
		return;
	va_list ap;
	va_start(ap, msg);

	const char *pref = _slogopts.prefix ? _slogopts.prefix : "";
	char lbuf[2048];
	if (_slogopts.pid)
		snprintf(lbuf, sizeof(lbuf), "syslog (pid %ld): %s: %s",
		    getpid(), pref, msg);
	else
		snprintf(lbuf, sizeof(lbuf), "syslog: %s: %s", pref, msg);
	vfprintf(stderr, lbuf, ap);
	va_end(ap);
}

static int _isoct(int c)
{
	return c >= '0' && c <= '7';
}

long
strtol(const char *n, char **endptr, int base)
{
	// gobble spaces, then "+" or "-", and finally "0x" or "0"
	while (isspace(*n))
		n++;
	int sign = 1;
	if (*n == '+')
		n++;
	else if (*n == '-') {
		sign = -1;
		n++;
	}

	int haspre = strcmp(n, "0x") == 0;
	int (*matcher)(int);
	if (base == 16 || (base == 0 && haspre)) {
		base = 16;
		if (haspre)
			n += 2;
		matcher = isxdigit;
	} else if (base == 8 || (base == 0 && *n == '0')) {
		base = 8;
		if (*n == '0')
			n++;
		matcher = _isoct;
	} else if (base == 0 || base == 10) {
		base = 10;
		matcher = isdigit;
	} else {
		errno = EINVAL;
		return 0;
	}
	long tot = 0;
	while (matcher(*n)) {
		int c = tolower(*n++);
		tot = tot*base;
		if (isalpha(c))
			tot += c - 'a' + 10;
		else
			tot += c - '0';
	}
	if (endptr)
		*endptr = (char *)n;
	return tot*sign;
}

double
strtod(const char *s, char **end)
{
	return (double)strtold(s, end);
}

long double
strtold(const char *s, char **end)
{
        char *tend;
        long t = strtol(s, &tend, 10);
        // tend points to '.' or [Ee]
        long double ret = (long double)t;
        if (*tend == '.') {
                tend++;
                char *fend;
                long frac = strtol(tend, &fend, 10);
                long digs = fend - tend;
                if (digs) {
			long double addend = (long double)frac/(pow(10, digs));
			if (ret < 0)
				ret -= addend;
			else
				ret += addend;
                        tend = fend;
                }
        }
        if (tolower(*tend) == 'e') {
                tend++;
                char *eend;
                long exp = strtol(tend, &eend, 10);
                if (eend - tend > 0) {
                        ret *= pow(10, exp);
                        tend = eend;
                }
        }
        if (end)
                *end = tend;
        return ret;
}

long long
strtoll(const char *n, char **endptr, int base)
{
	return strtol(n, endptr, base);
}

ulong
strtoul(const char *n, char **endptr, int base)
{
	long ret = strtol(n, endptr, base);
	return (ulong)ret;
}

unsigned long long
strtoull(const char *n, char **endptr, int base)
{
	return strtoull(n, endptr, base);
}

time_t
time(time_t *tloc)
{
	struct timeval tv;
	if (gettimeofday(&tv, NULL))
	      return -1;
	if (tloc)
		*tloc = tv.tv_sec;
	return tv.tv_sec;
}

int
tolower(int c)
{
	if (!isalpha(c))
		return c;
	return c | 0x20;
}

int
toupper(int c)
{
	if (!isalpha(c))
		return c;
	return c & ~0x20;
}

double
trunc(double x)
{
	return (long)x;
}

int
vprintf(const char *fmt, va_list ap)
{
	char lbuf[2048];

	if (strlen(fmt) >= sizeof(lbuf))
		printf("warning: fmt too long\n");

	int ret;
	ret = vsnprintf(lbuf, sizeof(lbuf), fmt, ap);
	pmsg(lbuf, ret);

	return ret;
}

static struct utsname _unamed = {
	.sysname = "BiscuitOS",
	.nodename = "localhost",
	.release = BISCUIT_RELEASE,
	.version = BISCUIT_VERSION,
	.machine = "amd64",
};

int
uname(struct utsname *name)
{
	memmove(name, &_unamed, sizeof(struct utsname));
	return 0;
}

int
usleep(uint us)
{
	long ns = (us % 1000000)*1000;
	struct timespec t = {us/1000000, ns};
	return nanosleep(&t, NULL);
}

int
vfprintf(FILE *f, const char *fmt, va_list ap)
{
	char lbuf[2048];

	if (strlen(fmt) >= sizeof(lbuf))
		printf("warning: fmt too long\n");

	int ret;
	ret = vsnprintf(lbuf, sizeof(lbuf), fmt, ap);
	int wrote = fwrite(lbuf, ret, 1, f);

	return wrote;
}

#define _DLMALLOC 0

#if _DLMALLOC

#include <../dlmalloc.c>

#else

static volatile long biglock;
static void acquire(void)
{
	if (!dolock)
		return;
	while (__sync_lock_test_and_set(&biglock, 1) != 0)
		;
}

static void release(void)
{
	asm volatile("":::"memory");
	biglock = 0;
}

struct header_t {
	char *start;
	char *end;
	ulong objs;
	size_t maxsz;
	struct header_t *next;
	struct header_t *prev;
};

static struct header_t *allh;
static struct header_t *curh;
static char *bump;

static void
_free_header(struct header_t *ch)
{
	if (ch->prev)
		ch->prev->next = ch->next;
	else
		allh = ch->next;
	if (ch->next)
		ch->next->prev = ch->prev;
	int ret;
	if ((ret = munmap(ch->start, ch->end - ch->start)) < 0)
		err(ret, "munmap");
}

static void *
_malloc(size_t sz)
{
	// Minimum allocation size = 8 bytes.
	sz = (sz + 7) & ~7;
	int newhdr = 0;
	if (curh) {
		size_t left = curh->end - bump;
		newhdr = sz > left;
	} else {
		newhdr = 1;
	}
	if (newhdr) {
		if (curh && curh->objs == 0) {
			_free_header(curh);
			curh = NULL;
		}
		const size_t pgsize = 1 << 12;
		// Also account for the header that we embed within the
		// allocated space.
		size_t mmapsz = (sz + sizeof(struct header_t) + pgsize - 1) &
				~(pgsize - 1);
		if (mmapsz < sz) {
			errno = EOVERFLOW;
			return NULL;
		}
		struct header_t *nh = mmap(NULL, mmapsz, PROT_READ | PROT_WRITE,
		    MAP_ANON | MAP_PRIVATE, -1, 0);
		if (nh == MAP_FAILED) {
			errno = ENOMEM;
			return NULL;
		}
		nh->start = (char *)nh;
		nh->end = nh->start + mmapsz;
		nh->objs = 0;
		nh->next = allh;
		nh->prev = NULL;
		if (nh->next)
			nh->next->prev = nh;
		nh->maxsz = 0;
		allh = nh;

		curh = nh;
		bump = curh->start + sizeof(struct header_t);
	}
	curh->objs++;
	char *ret = bump;
	bump += sz;
	if (sz > curh->maxsz)
		curh->maxsz = sz;
	return ret;
}

#define	ZEROPTR	((void *)1)

void *
malloc(size_t sz)
{
	// try to catch accesses to zero-length objects. is this ok?
	if (sz == 0)
		return ZEROPTR;
	acquire();
	void *ret = _malloc(sz);
	release();

	return ret;
}

void *
calloc(size_t n, size_t sz)
{
	if (n == 0 || sz == 0)
		return ZEROPTR;
	if (n > ULONG_MAX / sz) {
		errno = EOVERFLOW;
		return NULL;
	}
	size_t big = sz*n;
	void *ret = malloc(big);
	if (!ret)
		return NULL;
	memset(ret, 0, big);
	return ret;
}

static struct header_t *
_findseg(void *pp)
{
	char *p = (char *)pp;
	struct header_t *ch;
	for (ch = allh; ch; ch = ch->next)
		if (ch->start <= p && ch->end > p)
			break;
	return ch;
}

static void
_free(void *pp)
{
	// find containing seg
	struct header_t *ch;
	ch = _findseg(pp);
	if (!ch)
		errx(-1, "free: bad pointer");
	ch->objs--;
	if (ch->objs == 0) {
		if (ch == curh)
			return;
		_free_header(ch);
	}
}

void
free(void *pp)
{
	acquire();
	_free(pp);
	release();
}

void *
realloc(void *vold, size_t nsz)
{
	char *old = vold;
	if (!old)
		return malloc(nsz);

	acquire();

	void *ret = old;
	struct header_t *ch = _findseg(old);
	// we don't know the exact size of the object, but its size is bounded
	// as follows
	size_t oldsz = MIN(ch->maxsz, ch->end - old);
	ret = _malloc(nsz);
	if (ret == NULL)
		goto out;
	memmove(ret, old, MIN(oldsz, nsz));
	_free(old);
out:
	release();
	return ret;
}
#endif

char __progname[64];
char **environ = _environ;

void
_start(int argc, char **argv, struct kinfo_t *k)
{
	kinfo = k;

	if (argc)
		strncpy(__progname, argv[0], sizeof(__progname));
	fdinit();
	int main(int, char **);
	int ret = main(argc, argv);
	exit(ret);
}

void
_entry(void)
{
	// make sure that the stack is 16-byte aligned, as gcc assumes, after
	// _start's function prologue. gcc emits SSE instructions that require
	// 16-byte alignment (misalignment generates #GP).
	asm(
	    "andq	$0xfffffffffffffff0, %%rsp\n"
	    "subq	$8, %%rsp\n"
	    "movabs 	$_start, %%rax\n"
	    "jmpq	*%%rax\n"
	    ::: "memory", "cc");
}

/* NGINX STUFF */
long timezone;

#define FAIL	errx(-1, "%s -- no imp", __FUNCTION__)
#define HACK(x)	do { \
	fprintf(stderr, "warning: %s is a no-op\n", __FUNCTION__);	\
	return x;	\
	} while (0)

char *
getenv(char *name)
{
	HACK(NULL);
}

uid_t
geteuid(void)
{
	HACK(666);
}

struct passwd *
getpwnam(const char *a)
{
	if (strcmp(a, "nobody") != 0)
		errx(-1, "unexpected getpwnam");

	static struct passwd dur = {"nobody", 666, 667, "/tmp",
	    "/bin/cmd.exe"};
	HACK(&dur);
}

struct group *
getgrnam(const char *a)
{
	if (strcmp(a, "nogroup") != 0)
		errx(-1, "unexpected getpwnam");

	static struct group dur = {"nogroup", 667, NULL};
	HACK(&dur);
}

struct hostent *
gethostbyname(const char *a)
{
	FAIL;
}

int
chown(const char *a, uid_t b, gid_t c)
{
	if (b != 666 || c != -1)
		errx(-1, "unexpected chown");
	errno = 0;
	HACK(0);
}

time_t
mktime(struct tm *a)
{
	FAIL;
}

int
gethostname(char *a, size_t b)
{
	snprintf(a, b, "localhost");
	HACK(0);
}

char *
strpbrk(const char *a, const char *bb)
{
	while (*a) {
		const char *b = bb;
		while (*b)
			if (*a == *b++)
				return (char *)a;
		a++;
	}
	return NULL;
}

int
setitimer(int a, struct itimerval *b, struct itimerval *c)
{
	FAIL;
}

struct tm *
localtime(const time_t *a)
{
	static struct tm dur;
	struct timeval tv;
	if (gettimeofday(&tv, NULL) == -1)
		err(-1, "localtime/gettimeofday");
	dur.tm_hour = tv.tv_sec / (60*60);
	dur.tm_min = tv.tv_sec / 60;
	dur.tm_sec = tv.tv_sec % 60;
	return &dur;
}

struct tm *
gmtime(const time_t *a)
{
	FAIL;
}

int
utimes(const char *a, const struct timeval b[2])
{
	FAIL;
}

int
glob(const char *a, int b, int (*c)(const char *, int), glob_t *d)
{
	FAIL;
}

void
globfree(glob_t *a)
{
	FAIL;
}

int
ioctl(int fd, ulong req, ...)
{
	if (req != FIOASYNC)
		errx(-1, "nyet");
	HACK(0);
}

int
raise(int a)
{
	FAIL;
}

mode_t
umask(mode_t a)
{
	static mode_t dur = 0022;
	mode_t old = dur;
	dur = a;
	HACK(old);
}

int
getpagesize(void)
{
	return 1 << 12;
}

int
sigprocmask(int a, sigset_t *b, sigset_t *c)
{
	if (c)
		errx(-1, "no old set");
	HACK(0);
}

int
sigsuspend(const sigset_t *a)
{
	fprintf(stderr, "warning: no signals, thus sleep forever...\n");
	pause();
	errx(-1, "sigsuspend no imp");
}

int
setpriority(int a, int b, int c)
{
	FAIL;
}

uid_t
getuid(void)
{
	FAIL;
}

int
setuid(uid_t a)
{
	FAIL;
}

int
setgid(gid_t a)
{
	FAIL;
}

int
initgroups(const char *a, gid_t b)
{
	FAIL;
}

char *
realpath(const char *a, char *b)
{
	FAIL;
}

int
lstat(const char *a, struct stat *b)
{
	// will some applications break because they expect lstat to be a
	// system call?
	// XXX yes, if it has maximum open fds already
	int fd = open(a, O_RDONLY);
	if (fd == -1)
		return -1;
	int ret = fstat(fd, b);
	if (close(fd) == -1)
		fprintf(stderr, "lstat close failed\n");
	return ret;
}

/* LMBENCH STUFF */
unsigned int
alarm(unsigned int sec)
{
	HACK(0);
}

#if 0
#include <../bloat.c>
#endif
