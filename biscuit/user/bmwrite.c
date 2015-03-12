#include <litc.h>

static char buf[512];

int main(int argc, char **argv)
{
	int fd;
	if ((fd = open("/bigdaddy.txt", O_RDWR | O_CREAT, 0)) < 0)
		errx(-1, "open");

	int i;
	for (i = 0; i < sizeof(buf); i++)
		buf[i] = 0x41 + (i / 1000);

	int ret;
	int blks = 1000;
	for (i = 0; i < blks; i++) {
		printf("write %d\n", i);
		size_t c = sizeof(buf);
		size_t e = c;
		if ((ret = write(fd, buf, c)) != e) {
			printf("write failed %d\n", ret);
			return -1;
		}
	}

	if (close(fd))
		errx(-1, "close");

	return 0;
}
