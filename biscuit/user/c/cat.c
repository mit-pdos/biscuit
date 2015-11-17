#include <litc.h>

int main(int argc, char **argv)
{
	if (argc <= 1)
		errx(-1, "usage: %s file1 [file2]...", argv[0]);

	int i;
	for (i = 1; i < argc; i++) {
		int fd;
		fd = open(argv[i], O_RDONLY, 0);
		if (fd < 0)
			err(-1, "open");
		char buf[512];
		long ret;
		while ((ret = read(fd, buf, sizeof(buf))) > 0)
			write(1, buf, ret);
		if (ret < 0)
			err(-1, "read");
		close(fd);
	}
	return 0;
}
