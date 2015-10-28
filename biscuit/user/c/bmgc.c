#include <litc.h>

static char buf[4096];

void usage()
{
	printf( "usage:\n"
		"%s -n <number> [-g]\n"
		"    where <number> is the number of files to open\n"
		"\n"
		"    -g forces the kernel to GC via fake_sys2\n", __progname);
	exit(-1);
}

__attribute__((noreturn)) void kgc()
{
	long openfiles = fake_sys2(0);
	printf("open files:   %ld\n", openfiles);
	exit(0);
}

int main(int argc, char **argv)
{
	long n = -1;

	int ch;
	while ((ch = getopt(argc, argv, "n:gh")) != -1) {
		switch (ch) {
		case 'n':
			n = atoi(optarg);
			break;
		case 'g':
			kgc();
			// not reached
			break;
		case 'h':
		default:
			usage();
		}
	}
	if (n < 0) {
		printf("invalid n\n");
		usage();
	}

	long dec = n / 100;
	if (dec == 0)
		dec = 1;
	printf("# n = %ld\n", n);

	int i;
	for (i = 0; i < n; i++) {
		int fd = open("/another/curse", O_RDONLY);
		if (fd < 0)
			err(fd, "open");

		struct stat s;
		if (fstat(fd, &s) < 0)
			errx(-1, "fstat");

		ulong c = 0;
		ulong sz = s.st_size;

		while (c < sz) {
			long ret;
#define min(a, b)	(a < b ? a : b)
			ret = read(fd, buf, min(sz - c, sizeof buf));
			if (ret < 0)
				err(ret, "read");
			c += ret;
		}
		close(fd);

		if (i % dec == 0)
			printf(".");
	}
	printf("done\n");
	return 0;
}
