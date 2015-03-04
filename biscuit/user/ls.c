#include <litc.h>

 struct  __attribute__((packed)) dirent_t {
 #define NMAX	14
	char	name[NMAX];
	ulong	inum;
};

 struct dirdata_t {
 #define NDIRENTS	(512/sizeof(struct dirent_t))
	struct dirent_t de[NDIRENTS];
 };

 char *dename(char *den)
 {
	static char nbuf[NMAX + 1];

	if (den[0] == 0)
		return NULL;
	int i;
	for (i = 0; i < NMAX; i++)
		nbuf[i] = den[i];
	nbuf[NMAX] = 0;
	return nbuf;
 }

static char buf[512];

void dprint(int fd, int *fds, size_t sz)
{
	//int cfd = 0;
	long ret;
	while ((ret = read(fd, buf, sizeof(buf))) > 0) {
		struct dirdata_t *dd = (struct dirdata_t *)buf;
		int i;
		for (i = 0; i < NDIRENTS; i++) {
			char *fn = dename(dd->de[i].name);
			if (!fn)
				continue;
			struct stat st;
			int tfd;
			if ((tfd = open(fn, O_RDONLY, 0)) < 0)
				errx(-1, "rec open %s", fn);
			if (fstat(tfd, &st))
				errx(-1, "fstat");
			if (close(tfd))
				errx(-1, "close");
			if (S_ISDIR(st.st_mode))
				printf("drwxr-xr-x %d %s\n", st.st_size, fn);
			else
				printf("-rwxr-xr-x %d %s\n", st.st_size, fn);
		}
	}
}

int main(int argc, char **argv)
{
	if (sizeof(struct dirent_t) != 22)
		errx(-1, "unexpected dirent size");

	const int tfd = 256;
	int fds[tfd];
	int fd;
	if ((fd = open("/", O_RDONLY, 0)) < 0)
		errx(fd, "open root");
	dprint(fd, fds, tfd);

	close(fd);

	return 0;
}
