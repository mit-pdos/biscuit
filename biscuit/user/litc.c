#include <littypes.h>

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
