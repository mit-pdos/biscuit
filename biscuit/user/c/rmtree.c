#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <err.h>

void rm(char *fn);

void dirrm(char *dir)
{
	DIR *d = opendir(dir);
	if (!d)
		err(-1, "opendir");
	if (chdir(dir) == -1)
		err(-1, "chdir");
	struct dirent des, *de;
	while (1) {
		int ret = readdir_r(d, &des, &de);
		if (ret)
			errx(-1, "readdir: %s", strerror(ret));
		if (!de)
			break;
		if (strncmp(de->d_name, ".", 2) == 0 ||
		    strncmp(de->d_name, "..", 3) == 0)
			continue;
		rm(de->d_name);
	}
	if (chdir("..") == -1)
		err(-1, "chdir");
	if (rmdir(dir) == -1)
		err(-1, "rmdir");
}

void rm(char *fn)
{
	struct stat st;
	if (stat(fn, &st) == -1)
		err(-1, "stat");
	if (S_ISREG(st.st_mode) || S_ISSOCK(st.st_mode) ||
	    S_ISDEV(st.st_mode)) {
		if (unlink(fn) == -1)
			err(-1, "unlink");
		return;
	} else if (S_ISDIR(st.st_mode)) {
		dirrm(fn);
		return;
	}
	errx(-1, "weird file type %ld", st.st_mode & S_IFMT);
}

int main(int argc, char **argv)
{
	if (argc != 2) {
		fprintf(stderr, "usage: %s <non-empty dir to remove>\n",
		    __progname);
		exit(-1);
	}
	char *fn = argv[1];
	rm(fn);
	return 0;
}
