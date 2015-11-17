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

void dprint(int fd, char *par, int left)
{
	char buf[512];

	printf("%s/:\n", par);

	char *pend = par + strlen(par);
	snprintf(pend, left, "/");
	left--;
	pend = par + strlen(par);
	while (read(fd, buf, sizeof(buf)) > 0) {
		struct dirdata_t *dd = (struct dirdata_t *)buf;
		int i;
		for (i = 0; i < NDIRENTS; i++) {
			char *tn = dename(dd->de[i].name);
			if (!tn)
				continue;
			snprintf(pend, left, "%s", tn);
			char *fn = par;
			struct stat st;
			if (stat(fn, &st))
				err(-1, "stat");
			char spec;
			if (S_ISDIR(st.st_mode))
				spec = 'd';
			else if (S_ISSOCK(st.st_mode))
				spec = 's';
			else
				spec = '-';
			printf("%crwxr-xr-x %ld %s\n", spec, st.st_size, tn);
		}
	}

	// recursive list
	*pend = 0;
	if (lseek(fd, 0, SEEK_SET) < 0)
		err(-1, "lseek");
	while (read(fd, buf, sizeof(buf)) > 0) {
		struct dirdata_t *dd = (struct dirdata_t *)buf;
		int i;
		for (i = 0; i < NDIRENTS; i++) {
			char *tn = dename(dd->de[i].name);
			if (!tn)
				continue;
			if (strncmp(tn, "..", 3) == 0 ||
			    strncmp(tn, ".", 2) == 0)
				continue;
			snprintf(pend, left, "%s", tn);
			char *fn = par;
			struct stat st;
			if (stat(fn, &st))
				err(-1, "stat");
			if (S_ISDIR(st.st_mode)) {
				int tfd = open(fn, O_RDONLY | O_DIRECTORY, 0);
				if (tfd < 0)
					err(-1, "rec open");
				dprint(tfd, par, left - strlen(pend));
				if (close(tfd))
					err(-1, "close");
			}
		}
	}
}

int main(int argc, char **argv)
{
	char pbuf[256] = {'.'};
	if (sizeof(struct dirent_t) != 22)
		errx(-1, "unexpected dirent size");

	int fd;
	if ((fd = open("./", O_RDONLY, 0)) < 0)
		err(-1, "open root");

	dprint(fd, pbuf, sizeof(pbuf));
	close(fd);

	return 0;
}
