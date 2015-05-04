#include <litc.h>

static char buf[5120];

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
	int pid1 = fork();
	int pid2 = fork();

	int fd;
	if ((fd = open("/boot/uefi/readme.txt", O_RDWR, 0)) < 0) {
		err(fd, "open");
		return -1;
	}

	if (pid1 && pid2) {
		int j;
		for (j = 0; j < 100000000; j++)
			asm volatile("":::"memory");
		readprint(fd);
		return 0;
	}

	int i;
	for (i = 0; i < sizeof(buf); i++)
		buf[i] = 0x41 + (i / 1000);

	int ret;
	if ((ret = write(fd, buf, sizeof(buf))) != sizeof(buf)) {
		err(ret, "write");
		return -1;
	}
	return 0;
}
