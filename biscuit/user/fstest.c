#include <litc.h>

static char buf[1024];

int main()
{
	int ret;
	if ((ret = open("/etc/passwd", O_RDONLY, 0)) >= 0) {
		printf_red("should have failed\n");
		return -1;
	}
	if ((ret = open("/hi.txt", O_RDONLY, 0)) < 0) {
		printf_red("should have succeeded 1\n");
		return -1;
	}
	int fd1 = ret;
	if ((ret = open("/boot/eufi/readme.txt", O_RDONLY, 0)) < 0) {
		printf_red("should have succeeded 2\n");
		return -1;
	}
	int fd2 = ret;

	if ((ret = read(fd1, &buf, sizeof(buf))) < 0) {
		printf_red("read1 failed\n");
		return -1;
	}
	if (ret == sizeof(buf))
		ret = sizeof(buf) - 1;
	buf[ret] = '\0';
	printf("fd1: %s\n", buf);

	if ((ret = read(fd2, &buf, sizeof(buf))) < 0) {
		printf_red("read2 failed\n");
		return -1;
	}
	if (ret == sizeof(buf))
		ret = sizeof(buf) - 1;
	buf[ret] = '\0';
	printf("fd2: %s\n", buf);

	printf_blue("fs tests passed!\n");
	return 0;
}
