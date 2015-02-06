#include <littypes.h>
#include <litc.h>

#define SYS_WRITE       1
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

long
write(int fd, void *buf, size_t c)
{
	syscall(fd, SA(buf), SA(c), 0, 0, SYS_WRITE);
	return 0;
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
putn(char *p, char *end, int n, int base, int acc)
{
	if (acc > n) {
		int pzero = acc == 1;
		if (pzero)
			wc(p, end, numtoch(0));
		return pzero;
	}
	int newbase = acc * base;
	int ret = putn(p, end, n, base, newbase);
	p += ret;
	char c = (n % newbase)/acc;
	c = numtoch(c);
	wc(p, end, c);
	return ret + 1;
}

static char pbuf[MAXBUF];

int vprintf(char *fmt, va_list ap)
{
	char *dst = pbuf;
	char *end = &pbuf[MAXBUF - 1];
	char c;

	int prehex = 0;

	c = *fmt;
	while (c && dst < end) {
		if (c != '%') {
			dst += wc(dst, end, c);
			fmt++;
			c = *fmt;
			continue;
		}

		fmt++;
		int done = 0;
		while (!done) {
			char t = *fmt;
			fmt++;
			switch (t) {
			case 'd':
				{
				int a = va_arg(ap, int);
				dst += putn(dst, end, a, 10, 1);
				done = 1;
				break;
				}
			case '#':
				prehex = 1;
				break;
			case 'x':
				{
				if (prehex) {
					dst += wc(dst, end, '0');
					dst += wc(dst, end, 'x');
				}
				int a = va_arg(ap, int);
				dst += putn(dst, end, a, 16, 1);
				done = 1;
				break;
				}
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
