#include <litc.h>

static char buf[1024];

void fillbuf(char c)
{
	int i;
	for (i = 0; i < sizeof(buf); i++)
		buf[i] = c;
}

void chk(int fd, char c)
{
	if (read(fd, buf, sizeof(buf)) != sizeof(buf))
		errx(-1, "short read");
	int i;
	for (i = 0; i < sizeof(buf); i++)
		if (buf[i] != c)
			errx(-1, "byte mismatch");
}

int main(int argc, char **argv)
{
	int fd;
	if ((fd = open("/newfile", O_RDWR|O_CREAT, 0755)) < 0)
	  errx(fd, "create failed");

	fillbuf('A');
	if (write(fd, buf, sizeof(buf)) != sizeof(buf))
		errx(-1, "short write");

	if (unlink("/newfile") != 0)
		errx(-1, "should have succeeded");

	printf("erased...\n");

	if ((fd = open("/yetanother", O_RDWR|O_CREAT, 0755)) < 0)
	  errx(fd, "create failed");

	int fd2;
	if ((fd2 = open("/evenmore", O_RDWR|O_CREAT, 0755)) < 0)
	  errx(fd2, "create failed");

	printf("filing two...\n");

	fillbuf('B');
	if (write(fd, buf, sizeof(buf)) != sizeof(buf))
		errx(-1, "short write");

	printf("first fill...\n");

	fillbuf('C');
	if (write(fd2, buf, sizeof(buf)) != sizeof(buf))
		errx(-1, "short write");

	if (close(fd) != 0)
		errx(-1, "close");
	if (close(fd2) != 0)
		errx(-1, "close");

	printf("checking contents...\n");

	if ((fd = open("/yetanother", O_RDONLY, 0)) < 0)
		errx(fd, "open original failed");
	chk(fd, 'B');

	if ((fd2 = open("/evenmore", O_RDONLY, 0755)) < 0)
	  errx(fd2, "create failed");
	chk(fd2, 'C');

	printf("success\n");

	return 0;
}
