#include <litc.h>

static char buf[1024];

void readprint(int fd)
{
	long ret;
	if ((ret = read(fd, &buf, sizeof(buf))) < 0) {
		err(ret, "read");
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

	char msg[] = "hey yea yea yea yea yea ja ja ja ya ya ya, ho ho ha ha"
	    " ha! he ya ya ya ya ya ja ja ja ye ye ye, ho ho ho ho hoooooooo"
	    "ooooooo yo yo yoooooooooooo ya yay aaaaa yaaaaaa ya ya yaaaaa!"
	    " [%d %d]";
	snprintf(buf, sizeof(buf), msg, getpid(), 1);
	int ret;
	if ((ret = write(fd, buf, strlen(buf))) != strlen(buf)) {
		err(ret, "write");
		return -1;
	}

	snprintf(buf, sizeof(buf), msg, getpid(), 2);
	if ((ret = write(fd, buf, strlen(buf))) != strlen(buf)) {
		err(ret, "write");
		return -1;
	}
	return 0;
}
