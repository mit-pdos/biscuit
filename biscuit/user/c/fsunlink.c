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
	if (link("/boot/uefi/readme.txt", "/crap") != 0)
		errx(-1, "should have succeeded");

	int fd;
	if ((fd = open("/crap", O_RDONLY, 0)) < 0)
		errx(-1, "open failed");
	readprint(fd);

	if (unlink("/crap") != 0)
		errx(-1, "should have succeeded");

	if ((fd = open("/crap", O_RDONLY, 0)) >= 0)
		errx(-1, "open of unlinked should have failed");

	if ((fd = open("/boot/uefi/readme.txt", O_RDONLY, 0)) < 0)
		errx(-1, "open original failed");
	readprint(fd);

	if (unlink("/boot/uefi/readme.txt") != 0)
		errx(-1, "should have succeeded");

	if (unlink("/another") >= 0)
		errx(-1, "should have failed");

	if (unlink("/another/you-found-me") != 0)
		errx(-1, "should have succeeded");
	if (unlink("/another") != 0)
		errx(-1, "should have succeeded");

	printf("success\n");

	return 0;
}
