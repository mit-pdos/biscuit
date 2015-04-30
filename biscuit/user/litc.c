#include <littypes.h>
#include <litc.h>

#define SYS_READ         0
#define SYS_WRITE        1
#define SYS_OPEN         2
#define SYS_CLOSE        3
#define SYS_FSTAT        5
#define SYS_PIPE         22
#define SYS_PAUSE        34
#define SYS_GETPID       39
#define SYS_FORK         57
#define SYS_EXECV        59
#define SYS_EXIT         60
#define SYS_WAIT         61
#define SYS_CHDIR        80
#define SYS_MKDIR        83
#define SYS_LINK         86
#define SYS_UNLINK       87
#define SYS_FAKE         31337

static void pmsg(char *);

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
close(int fd)
{
	return syscall(SA(fd), 0, 0, 0, 0, SYS_CLOSE);
}

int
chdir(char *path)
{
	return syscall(SA(path), 0, 0, 0, 0, SYS_CHDIR);
}

void
exit(int status)
{
	syscall(status, 0, 0, 0, 0, SYS_EXIT);
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
	return syscall(0, 0, 0, 0, 0, SYS_FORK);
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
link(const char *old, const char *new)
{
	return syscall(SA(old), SA(new), 0, 0, 0, SYS_LINK);
}

int
mkdir(const char *p, long mode)
{
	return syscall(SA(p), mode, 0, 0, 0, SYS_MKDIR);
}

int
open(const char *path, int flags, int mode)
{
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
	return syscall(SA(pfds), 0, 0, 0, 0, SYS_PIPE);
}

long
read(int fd, void *buf, size_t c)
{
	return syscall(SA(fd), SA(buf), SA(c), 0, 0, SYS_READ);
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
	int ret = syscall(SA(&_status), 0, 0, 0, 0, SYS_WAIT);
	if (status)
		*status = _status;
	return ret;
}

long
write(int fd, void *buf, size_t c)
{
	return syscall(fd, SA(buf), SA(c), 0, 0, SYS_WRITE);
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
	const char *es[] = {
	    [EPERM] = "Permission denied",
	    [ENOENT] = "No such file or directory",
	    [EBADF] = "Bad file descriptor",
	    [ECHILD] = "No child processes",
	    [EFAULT] = "Bad address",
	    [EEXIST] = "File exists",
	    [ENOTDIR] = "Not a directory",
	    [EINVAL] = "Invalid argument",
	    [ENAMETOOLONG] = "File name too long",
	    [ENOSYS] = "Function not implemented",
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
	pmsg("\n");
	exit(eval);
}

void
errx(int eval, const char *fmt, ...)
{
	printf("%s: ", __progname);
	va_list ap;
	va_start(ap, fmt);
	vprintf(fmt, ap);
	va_end(ap);
	pmsg("\n");
	exit(eval);
}

size_t
strlen(char *msg)
{
	size_t ret = 0;
	while (*msg++)
		ret++;
	return ret;
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
pmsg(char *msg)
{
	write(1, msg, strlen(msg));
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

static char pbuf[MAXBUF];

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
vprintf(const char *fmt, va_list ap)
{
	int ret;
	ret = vsprintf(fmt, ap, pbuf, &pbuf[MAXBUF]);
	pmsg(pbuf);
	return ret;
}

int
printf(char *fmt, ...)
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
readline(char *prompt)
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
		if (i < sizeof(readlineb) - 1)
			readlineb[i++] = c;
	}
	readlineb[i] = 0;
	return readlineb;
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
printf_blue(char *fmt, ...)
{
	va_list ap;
	int ret;

	pmsg(BLUE);
	va_start(ap, fmt);
	ret = vsprintf(fmt, ap, pbuf, &pbuf[MAXBUF]);
	va_end(ap);
	pmsg(pbuf);
	pmsg(RESET);

	return ret;
}

int
printf_red(char *fmt, ...)
{
	va_list ap;
	int ret;

	pmsg(RED);
	va_start(ap, fmt);
	ret = vsprintf(fmt, ap, pbuf, &pbuf[MAXBUF]);
	va_end(ap);
	pmsg(pbuf);
	pmsg(RESET);

	return ret;
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
