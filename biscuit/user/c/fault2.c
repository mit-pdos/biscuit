#include <litc.h>

int main(int argc, char **argv)
{
	if (open((const char *)0x12347, O_RDONLY, 0) >= 0 || errno != EFAULT)
		errx(-1, "no fault?");
	errno = 0;

	if (write(1, (void *)0x7ffffff0, 10) >= 0 || errno != EFAULT)
		errx(-1, "no fault?");
	errno = 0;

	// g0 stack
	if (mkdir((const char *)0xffffffffffffffff, 0) == 0  || errno != EFAULT)
		errx(-1, "no fault?");
	errno = 0;

	int fd;
	if ((fd = open("/bigfile.txt", O_RDONLY, 0)) < 0)
		err(fd, "open");
	if (errno != 0)
		errx(-1, "errno changed on success?");
	// kernel text
	if (read(fd, (void *)0x400040, 1024) >= 0 || errno != EFAULT)
		errx(-1, "no fault?");

	printf("success\n");
	return 0;
}
