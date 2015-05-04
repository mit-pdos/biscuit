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
	if (link("/biscuit", "/spin") >= 0)
		errx(-1, "should have failed");

	if (link("/boot/uefi/readme.txt", "/crap") != 0)
		errx(-1, "should have suceeded");

	int fd;
	if ((fd = open("/crap", O_RDONLY, 0)) < 0)
		errx(-1, "open failed");
	readprint(fd);

	if (link("/boot/uefi/readme.txt", "/boot/uefi/crap") != 0)
		errx(-1, "should have suceeded");

	if ((fd = open("/boot/uefi/crap", O_RDONLY, 0)) < 0)
		errx(-1, "open failed");
	readprint(fd);

	if (link("/boot", "/dirhardlink") >= 0)
		errx(-1, "dir link should fail");

	return 0;
}
