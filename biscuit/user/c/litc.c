#include <littypes.h>
#include <litc.h>

#define SYS_READ         0
#define SYS_WRITE        1
#define SYS_OPEN         2
#define SYS_CLOSE        3
#define SYS_STAT         4
#define SYS_FSTAT        5
#define SYS_MMAP         9
#define SYS_MUNMAP       11
#define SYS_DUP2         33
#define SYS_PAUSE        34
#define SYS_GETPID       39
#define SYS_SOCKET       41
#define SYS_SENDTO       44
#define SYS_RECVFROM     45
#define SYS_BIND         49
#define SYS_FORK         57
#define SYS_EXECV        59
#define SYS_EXIT         60
#define SYS_WAIT4        61
#define SYS_KILL         62
#define SYS_CHDIR        80
#define SYS_RENAME       82
#define SYS_MKDIR        83
#define SYS_LINK         86
#define SYS_UNLINK       87
#define SYS_MKNOD        133
#define SYS_NANOSLEEP    230
#define SYS_PIPE2        293
#define SYS_FAKE         31337
#define SYS_THREXIT      31338

static FILE  _stdin = {0}, _stdout = {1}, _stderr = {2};
FILE  *stdin = &_stdin, *stdout = &_stdout, *stderr = &_stderr;

static long biglock;
static int dolock = 1;

static void acquire(void)
{
	if (!dolock)
		return;
	while (__sync_lock_test_and_set(&biglock, 1) != 0)
		;
}

static void release(void)
{
	biglock = 0;
}

static void pmsg(char *, long);

long
syscall(long a1, long a2, long a3, long a4,
    long a5, long trap)
{
	long ret;
	register long r8 asm("r8") = a5;

	asm volatile(
		"int	$64\n"
		: "=a"(ret)
		: "0"(trap), "D"(a1), "S"(a2), "d"(a3), "c"(a4), "r"(r8)
		: "cc", "memory");

	return ret;
}

#define SA(x)     ((long)x)

int
bind(int sockfd, const struct sockaddr *s, socklen_t sl)
{
	return syscall(SA(sockfd), SA(s), SA(sl), 0, 0, SYS_BIND);
}

int
close(int fd)
{
	return syscall(SA(fd), 0, 0, 0, 0, SYS_CLOSE);
}

int
chdir(char *path)
{
	return syscall(SA(path), 0, 0, 0, 0, SYS_CHDIR);
}

int
dup2(int old, int new)
{
	return syscall(SA(old), SA(new), 0, 0, 0, SYS_DUP2);
}

void
exit(int status)
{
	syscall(status, 0, 0, 0, 0, SYS_EXIT);
	errx(-1, "exit returned");
	while(1);
}

int
execv(const char *path, char * const argv[])
{
	return syscall(SA(path), SA(argv), 0, 0, 0, SYS_EXECV);
}

long
fake_sys(long n)
{
	return syscall(n, 0, 0, 0, 0, SYS_FAKE);
}

int
fork(void)
{
	long flags = FORK_PROCESS;
	return syscall(0, flags, 0, 0, 0, SYS_FORK);
}

int
fstat(int fd, struct stat *buf)
{
	return syscall(SA(fd), SA(buf), 0, 0, 0, SYS_FSTAT);
}

int
getpid(void)
{
	return syscall(0, 0, 0, 0, 0, SYS_GETPID);
}

int
kill(int pid, int sig)
{
	if (sig != SIGKILL) {
		printf("%s: kill: only SIGKILL is supported\n", __progname);
		return -1;
	}
	return syscall(SA(pid), SA(sig), 0, 0, 0, SYS_KILL);
}

int
link(const char *old, const char *new)
{
	return syscall(SA(old), SA(new), 0, 0, 0, SYS_LINK);
}

off_t
lseek(int fd, off_t off, int whence)
{
	errx(-1, "lseek: no imp");
	return 0;
}

int
mkdir(const char *p, long mode)
{
	return syscall(SA(p), mode, 0, 0, 0, SYS_MKDIR);
}

int
mknod(const char *p, mode_t m, dev_t d)
{
	if (MAJOR(d) == 0)
		errx(-1, "bad major: 0");
	return syscall(SA(p), SA(m), SA(d), 0, 0, SYS_MKNOD);
}

void *
mmap(void *addr, size_t len, int prot, int flags, int fd, long offset)
{
	ulong protflags = (ulong)prot << 32;
	protflags |= flags;
	return (void *)syscall(SA(addr), SA(len), SA(protflags), SA(fd),
	    SA(offset), SYS_MMAP);
}

int
munmap(void *addr, size_t len)
{
	return syscall(SA(addr), SA(len), 0, 0, 0, SYS_MUNMAP);
}

int
nanosleep(const struct timespec *timeout, struct timespec *remainder)
{
	return syscall(SA(timeout), SA(remainder), 0, 0, 0, SYS_NANOSLEEP);
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
	return syscall(SA(path), flags, mode, 0, 0, SYS_OPEN);
}

int
pause(void)
{
	return syscall(0, 0, 0, 0, 0, SYS_PAUSE);
}

int
pipe(int pfds[2])
{
	return pipe2(pfds, 0);
}

int
pipe2(int pfds[2], int flags)
{
	return syscall(SA(pfds), SA(flags), 0, 0, 0, SYS_PIPE2);
}

long
read(int fd, void *buf, size_t c)
{
	return syscall(SA(fd), SA(buf), SA(c), 0, 0, SYS_READ);
}

ssize_t
recv(int fd, void *buf, size_t len, int flags)
{
	return recvfrom(fd, buf, len, flags, NULL, NULL);
}

ssize_t
recvfrom(int fd, void *buf, size_t len, int flags, struct sockaddr *sa,
    socklen_t *salen)
{
	if (len >= (1ULL << 32))
		errx(-1, "len too big");
	ulong flaglen = len << 32 | flags;
	return (ssize_t)syscall(SA(fd), SA(buf), SA(flaglen), SA(sa),
	    SA(salen), SYS_RECVFROM);
}

int
rename(const char *old, const char *new)
{
	return syscall(SA(old), SA(new), 0, 0, 0, SYS_RENAME);
}

ssize_t
sendto(int fd, const void *buf, size_t len, int flags,
    const struct sockaddr *sa, socklen_t slen)
{
	if (len >= (1ULL << 32))
		errx(-1, "len too big");
	ulong flaglen = len << 32 | flags;
	return syscall(SA(fd), SA(buf), SA(flaglen), SA(sa), SA(slen),
	    SYS_SENDTO);
}

int
socket(int dom, int type, int proto)
{
	return syscall(SA(dom), SA(type), SA(proto), 0, 0, SYS_SOCKET);
}

int
stat(const char *path, struct stat *st)
{
	return syscall(SA(path), SA(st), 0, 0, 0, SYS_STAT);
}

int
unlink(const char *path)
{
	return syscall(SA(path), 0, 0, 0, 0, SYS_UNLINK);
}

int
wait(int *status)
{
	int _status;
	int ret = syscall(WAIT_ANY, SA(&_status), 0, 0, 0, SYS_WAIT4);
	if (status)
		*status = _status;
	return ret;
}

int
waitpid(int pid, int *status, int options)
{
	return wait4(pid, status, options, NULL);
}

int
wait4(int pid, int *status, int options, void *rusage)
{
	if (rusage)
		errx(-1, "wait4: rusage not supported");
	int _status;
	int ret = syscall(pid, SA(&_status), SA(options), SA(rusage), 0,
	    SYS_WAIT4);
	if (status)
		*status = _status;
	return ret;
}

long
write(int fd, const void *buf, size_t c)
{
	return syscall(fd, SA(buf), SA(c), 0, 0, SYS_WRITE);
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

	register ulong r8 asm("r8") = (ulong)fn;
	register ulong r9 asm("r9") = (ulong)fnarg;

	asm volatile(
	    "int	$64\n"
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
	    : "D"(args), "S"(flags), "0"(SYS_FORK), "r"(r8), "r"(r9)
	    : "memory", "cc");
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

	long _status;
	int ret = syscall(tid, SA(&_status), 0, 0, 1, SYS_WAIT4);
	if (status)
		*status = _status;
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
};

static long
_pcreate(void *vpcarg)
{
#define		PSTACKSZ	(4096)
	struct pcargs_t pcargs = *(struct pcargs_t *)vpcarg;
	free(vpcarg);
	long status;
	status = (long)(pcargs.fn(pcargs.arg));
	asm volatile(
	    "int	$64\n"
	    "cmpq	$0, %%rax\n"
	    "je		1f\n"
	    "movq	$0, 0x0\n"
	    "1:\n"
	    "movq	%1, %%rax\n"
	    "movq	%4, %%rdi\n"
	    "int	$64\n"
	    "movq	$0, 0x1\n"
	    :
	    : "a"(SYS_MUNMAP), "g"(SYS_THREXIT), "D"(pcargs.stack), "S"(PSTACKSZ), "g"(status)
	    : "memory", "cc");
	// not reached
	return 0;
}

int
pthread_create(pthread_t *t, pthread_attr_t *attrs, void* (*fn)(void *), void *arg)
{
	if (attrs != NULL)
		errx(-1, "pthread_create: attrs not yet supported");
	// XXX setup guard page
	void *stack = mkstack(PSTACKSZ);
	if (!stack)
		return -ENOMEM;
	long tid;
	struct tfork_t tf = {
		.tf_tcb = NULL,
		.tf_tid = &tid,
		.tf_stack = stack,
	};
	int ret;
	struct pcargs_t *pca = malloc(sizeof(struct pcargs_t));
	if (!pca) {
		ret = -ENOMEM;
		goto errstack;
	}

	pca->fn = fn;
	pca->arg = arg;
	pca->stack = stack - PSTACKSZ;
	ret = tfork_thread(&tf, _pcreate, pca);
	if (ret < 0) {
		goto both;
	}
	*t = tid;
	return 0;
both:
	free(pca);
errstack:
	munmap(stack, PSTACKSZ);
	return ret;
}

int
pthread_join(pthread_t t, void **retval)
{
	int ret = thrwait(t, (long *)retval);
	if (ret < 0)
		return ret;
	return 0;
}

int
pthread_mutex_lock(pthread_mutex_t *m)
{
	errx(-1, "pthread_mutex_lock: no imp");
	return 0;
}

int
pthread_mutex_unlock(pthread_mutex_t *m)
{
	errx(-1, "pthread_mutex_unlock: no imp");
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
	return getpid();
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
	int tot = 0;
	while (*n)
		tot = tot*10 + (*n++ - '0');
	return tot;
}

ulong
atoul(const char *n)
{
	ulong tot = 0;
	while (*n)
		tot = tot*10 + (*n++ - '0');
	return tot;
}

void
err(int eval, const char *fmt, ...)
{
	dolock = 0;
	const char *es[] = {
	    [EPERM] = "Permission denied",
	    [ENOENT] = "No such file or directory",
	    [EBADF] = "Bad file descriptor",
	    [ECHILD] = "No child processes",
	    [EFAULT] = "Bad address",
	    [EEXIST] = "File exists",
	    [ENOTDIR] = "Not a directory",
	    [EISDIR] = "Is a directory",
	    [EINVAL] = "Invalid argument",
	    [ENAMETOOLONG] = "File name too long",
	    [ENOSYS] = "Function not implemented",
	    [ECONNREFUSED] = "Connection refused",
	};
	int nents = sizeof(es)/sizeof(es[0]);
	printf("%s: ", __progname);
	va_list ap;
	va_start(ap, fmt);
	vprintf(fmt, ap);
	va_end(ap);
	// errno is dumb
	int neval = eval < 0 ? -eval : eval;
	if (neval < nents && es[neval] != NULL) {
		printf(": %s", es[neval]);
	}
	pmsg("\n", 1);
	exit(eval);
}

void
errx(int eval, const char *fmt, ...)
{
	dolock = 0;
	printf("%s: ", __progname);
	va_list ap;
	va_start(ap, fmt);
	vprintf(fmt, ap);
	va_end(ap);
	pmsg("\n", 1);
	exit(eval);
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
	const char argwut = optstring[0] == ':' ? ':' : wut;
	if (optind >= argc || argv[optind][0] == '-')
		return argwut;
	optarg = argv[optind];
	optind++;
	return ca[1];
}

int
gettimeofday(struct timeval *tv, struct timezone *tz)
{
	errx(-1, "gettimeofday: no imp");
	return 0;
}

dev_t
makedev(uint maj, uint min)
{
	return (ulong)maj << 32 | min;
}

int
memcmp(const void *q, const void *p, size_t sz)
{
	const char *s1 = (char *)q;
	const char *s2 = (char *)p;
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
		while (n--)
			*--d = *--s;
		return d;
	}
	while (n--)
		*d++ = *s++;
	return d;
}

void *
memset(void *d, int c, size_t n)
{
	char v = (char)c;
	char *p = d;
	while (n--)
		*p++ = v;
	return d;
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

int
vsprintf(const char *fmt, va_list ap, char *dst, char *end)
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
		while (!done) {
			char t = *fmt;
			fmt++;
			switch (t) {
			case '#':
				prehex = 1;
				break;
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
					n = ~n + 1;
				}
				dst += putn(dst, end, n, 10);
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
				while (*s)
					dst += wc(dst, end, *s++);
				done = 1;
				break;
			}
			default:
				done = 1;
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
printf(const char *fmt, ...)
{
	va_list ap;
	int ret;

	va_start(ap, fmt);
	ret = vprintf(fmt, ap);
	va_end(ap);

	return ret;
}

static char readlineb[256];

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
		if (i < sizeof(readlineb) - 2)
			readlineb[i++] = c;
	}
	if (ret < 0)
		printf("readline: read failed: %d\n", ret);
	if (ret == 0)
		return NULL;
	readlineb[i] = 0;
	return readlineb;
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
	ret = vsprintf(fmt, ap, dst, dst + sz);
	va_end(ap);

	return ret;
}

char *
strchr(const char *big, const char l)
{
	for (; *big; big++)
		if (*big == l)
			return (char *)big;
	if (l == '\0')
		return (char *)big;
	return NULL;
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

int
vprintf(const char *fmt, va_list ap)
{
	char lbuf[256];

	int ret;
	ret = vsprintf(fmt, ap, lbuf, lbuf + sizeof(lbuf));
	pmsg(lbuf, ret);

	return ret;
}

int
vfprintf(FILE *f, const char *fmt, va_list ap)
{
	char lbuf[256];

	int ret;
	ret = vsprintf(fmt, ap, lbuf, lbuf + sizeof(lbuf));
	write(f->fd, lbuf, ret);

	return ret;
}

struct header_t {
	char *start;
	char *end;
	ulong objs;
	struct header_t *next;
};

static struct header_t *allh;
static struct header_t *curh;
static char *bump;

void *
malloc(size_t sz)
{
	acquire();

	sz = (sz + 7) & ~7;
	if (!curh || bump + sz > curh->end) {
		const int pgsize = 1 << 12;
		size_t mmapsz = (sz + pgsize - 1) & ~(pgsize - 1);
		struct header_t *nh = mmap(NULL, mmapsz, PROT_READ | PROT_WRITE,
		    MAP_ANON | MAP_PRIVATE, -1, 0);
		if (nh == MAP_FAILED) {
			release();
			printf("malloc: couldn't mmap more mem\n");
			return NULL;
		}
		nh->start = (char *)nh;
		nh->end = nh->start + mmapsz;
		nh->objs = 0;
		nh->next = allh;
		allh = nh;

		curh = nh;
		bump = curh->start + sizeof(struct header_t);
	}
	curh->objs++;
	char *ret = bump;
	bump += sz;

	release();

	return ret;
}

void
free(void *pp)
{
	acquire();

	char *p = pp;
	// find containing seg
	struct header_t *ch;
	struct header_t *prev = NULL;
	for (ch = allh; ch; prev = ch, ch = ch->next)
		if (ch->start <= p && ch->end > p)
			break;
	if (!ch)
		errx(-1, "free: bad pointer");
	ch->objs--;
	if (ch->objs == 0) {
		if (prev)
			prev->next = ch->next;
		else
			allh = ch->next;
		if (curh == ch) {
			bump = NULL;
			curh = NULL;
		}
		int ret;
		if ((ret = munmap(ch->start, ch->end - ch->start)) < 0)
			err(ret, "munmap");
	}
	release();
}

char __progname[64];

void
_entry(int argc, char **argv)
{
	if (argc)
		strncpy(__progname, argv[0], sizeof(__progname));
	int main(int, char **);
	int ret = main(argc, argv);
	exit(ret);
}
