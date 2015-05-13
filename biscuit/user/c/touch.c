#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s <file>\n", argv[0]);

	char *f = argv[1];
	int fd;
	fd = open(f, O_CREAT, 0600);
	if (fd < 0)
		err(fd, "open");
	fd = close(fd);
	if (fd)
		err(fd, "close");
	return 0;
}
