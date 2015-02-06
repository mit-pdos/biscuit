#include <littypes.h>
#include <litc.h>

#define SYS_WRITE       1
#define SYS_FORK        57
#define SYS_EXIT        60

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
fork(void)
{
	return syscall(0, 0, 0, 0, 0, SYS_FORK);
}

long
write(int fd, void *buf, size_t c)
{
	return syscall(fd, SA(buf), SA(c), 0, 0, SYS_WRITE);
}

void
exit(int status)
{
	syscall(status, 0, 0, 0, 0, SYS_EXIT);
}

size_t
strlen(char *msg)
{
	size_t ret = 0;
	while (*msg++)
		ret++;
	return ret;
}

void
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
vprintf(char *fmt, va_list ap)
{
	char *dst = pbuf;
	char *end = &pbuf[MAXBUF - 1];
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
			}
		}
		c = *fmt;
		prehex = 0;
	}

	if (dst > end)
		dst = end;
	*dst++ = '\0';
	pmsg(pbuf);
	return dst - pbuf;
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

void
_entry(void)
{
	extern int main(void);
	int ret = main();
	exit(ret);
}
