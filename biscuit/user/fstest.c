#include <litc.h>

static char buf[1024];

void readprint(int fd)
{
	long ret;
	if ((ret = read(fd, &buf, sizeof(buf))) < 0) {
		printf_red("read1 failed\n");
		exit(-1);
	}
	printf("FD %d read %ld bytes\n", fd, ret);
	if (ret == sizeof(buf))
		ret = sizeof(buf) - 1;
	buf[ret] = '\0';
	printf("FD %d returned: %s\n", fd, buf);
}

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
	if ((ret = open("/clouseau.txt", O_RDONLY, 0)) < 0) {
		printf_red("should have succeeded 3\n");
		return -1;
	}
	int fd3 = ret;
	if ((ret = open("/boot/bsd", O_RDONLY, 0)) < 0) {
		printf_red("should have succeeded 4\n");
		return -1;
	}
	int fd4 = ret;

	readprint(fd1);
	readprint(fd2);
	readprint(fd3);
	readprint(fd4);

	printf_blue("fs tests passed!\n");
	return 0;
}
