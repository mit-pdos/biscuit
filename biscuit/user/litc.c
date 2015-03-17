#include <littypes.h>
#include <litc.h>

#define SYS_READ         0
#define SYS_WRITE        1
#define SYS_OPEN         2
#define SYS_CLOSE        3
#define SYS_FSTAT        5
#define SYS_GETPID       39
#define SYS_FORK         57
#define SYS_EXIT         60
#define SYS_MKDIR        83
#define SYS_LINK         86
#define SYS_UNLINK       87

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

void
exit(int status)
{
	syscall(status, 0, 0, 0, 0, SYS_EXIT);
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

long
write(int fd, void *buf, size_t c)
{
	return syscall(fd, SA(buf), SA(c), 0, 0, SYS_WRITE);
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

void
errx(int eval, const char *fmt, ...)
{
	va_list ap;
	pmsg(RED);
	va_start(ap, fmt);
	vprintf(fmt, ap);
	va_end(ap);
	pmsg("\n");
	pmsg(RESET);
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
putn1(char *p, char *end, ulong n, int base, long acc)
{
	if (acc > n) {
		int pzero = acc == 1;
		if (pzero)
			wc(p, end, numtoch(0));
		return pzero;
	}
	long newbase = acc * base;
	int ret = putn1(p, end, n, base, newbase);
	p += ret;
	char c = (n % newbase)/acc;
	c = numtoch(c);
	wc(p, end, c);
	return ret + 1;
}

static int
putn(char *p, char *end, ulong n, int base)
{
	return putn1(p, end, n, base, 1);
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
				ulong top = n / 10000000000ULL;
				ulong bot = n % 10000000000ULL;
				if (top)
					dst += putn(dst, end, top, 10);
				dst += putn(dst, end, bot, 10);
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
				ulong top = n >> 32;
				ulong bot = n & ((1ULL << 32) - 1);
				if (top)
					dst += putn(dst, end, top, 16);
				dst += putn(dst, end, bot, 16);
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
	*dst++ = '\0';
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

char *
readline(char *prompt)
{
	if (prompt)
		printf("%s\n", prompt);
	int ret;
	int i = 0;
	char c = 0x41;
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

void
_entry(void)
{
	int argc = 0;
	char **argv = NULL;
	extern int main(int, char **);
	int ret = main(argc, argv);
	exit(ret);
}
