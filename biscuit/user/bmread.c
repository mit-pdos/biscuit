#include <litc.h>

static char buf[512];

int main(int argc, char **argv)
{
	char *fn = "/bigfile.txt";
	int fd;
	if ((fd = open(fn, O_RDONLY, 0)) < 0)
		errx(-1, "open");

	int i;
	int ret;
	int blks = 1000;
	for (i = 0; i < blks; i++) {
		printf("read %d\n", i);
		size_t c = sizeof(buf);
		size_t e = c;
		if ((ret = read(fd, buf, c)) != e) {
			printf("read failed %d\n", ret);
			return -1;
		}
		int j;
		for (j = 0; j < sizeof(buf); j++)
			if (buf[j] != 'A')
				errx(-1, "mismatch!");
	}

	if (close(fd))
		errx(-1, "close");

	return 0;
}
