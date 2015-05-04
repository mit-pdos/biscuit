#include <litc.h>

static char buf[1024];

void readprint(int fd)
{
	long ret;
	if ((ret = read(fd, &buf, sizeof(buf))) < 0) {
		err(ret, "read");
		exit(-1);
	}
	printf("FD %d read %ld bytes\n", fd, ret);
	if (ret == sizeof(buf))
		ret = sizeof(buf) - 1;
	buf[ret] = '\0';
	printf("FD %d returned: %s\n", fd, buf);
}

int main(int argc, char **argv)
{
	int ret;
	if ((ret = open("/etc/passwd", O_RDONLY, 0)) >= 0) {
		errx(-1, "open should fail");
		return -1;
	}
	if ((ret = open("/hi.txt", O_RDONLY, 0)) < 0) {
		err(ret, "open");
		return -1;
	}
	int fd1 = ret;
	if ((ret = open("/boot/uefi/readme.txt", O_RDONLY, 0)) < 0) {
		err(ret, "open");
		return -1;
	}
	int fd2 = ret;
	if ((ret = open("/clouseau.txt", O_RDONLY, 0)) < 0) {
		err(ret, "open");
		return -1;
	}
	int fd3 = ret;
	if ((ret = open("/boot/bsd", O_RDONLY, 0)) < 0) {
		err(ret, "open");
		return -1;
	}
	int fd4 = ret;

	readprint(fd1);
	readprint(fd2);
	readprint(fd3);
	readprint(fd4);

	printf("FS TESTS PASSED!\n");
	return 0;
}
