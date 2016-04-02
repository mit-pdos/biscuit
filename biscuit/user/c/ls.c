#include <litc.h>

void dprint(int fd, char *par, int left, int rec)
{
	printf("%s/:\n", par);

	char *pend = par + strlen(par);
	snprintf(pend, left, "/");
	left--;
	pend = par + strlen(par);

	DIR *dir = fdopendir(fd);
	if (!dir)
		err(-1, "fdopendir");
	struct dirent des, *de;
	int ret;
	while (1) {
		ret = readdir_r(dir, &des, &de);
		if (ret)
			errx(-1, "readdir_r %s", strerror(ret));
		if (!de)
			break;
		int used = snprintf(pend, left, "%s", de->d_name);
		if (used >= left)
			errx(-1, "long filenames!");
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
		printf("%crwxr-xr-x %ld %s\n", spec, st.st_size, de->d_name);
	}
	if (!rec) {
		if (closedir(dir) == -1)
			err(-1, "closedir");
		return;
	}

	// recursive list
	*pend = 0;
	rewinddir(dir);
	while (1) {
		ret = readdir_r(dir, &des, &de);
		if (ret)
			errx(-1, "readdir_r %s", strerror(ret));
		if (!de)
			break;
		char *tn = de->d_name;
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
			dprint(tfd, par, left - strlen(pend), 1);
			// dprint closes tfd
		}
	}
	if (closedir(dir) == -1)
		err(-1, "closedir");
}

__attribute__((noreturn))
void usage(void)
{
	fprintf(stderr, "%s [-R]\n"
	    "-R    recurse subdirectories\n"
	    "\n", __progname);
	exit(-1);
}

int main(int argc, char **argv)
{
	int rec = 0;
	int ch;
	while ((ch = getopt(argc, argv, "R")) != -1) {
		switch (ch) {
		case 'R':
			rec = 1;
			break;
		default:
			usage();
			break;
		}
	}
	char *start;
	int rem = argc - optind;
	if (rem == 0)
		start = ".";
	else if (rem == 1)
		start = argv[optind];
	else
		usage();
	int fd;
	if ((fd = open(start, O_RDONLY | O_DIRECTORY, 0)) == -1)
		err(-1, "open %s", start);

	char pbuf[256];
	strncpy(pbuf, start, sizeof(pbuf));
	dprint(fd, pbuf, sizeof(pbuf) - strlen(pbuf), rec);
	close(fd);

	return 0;
}
