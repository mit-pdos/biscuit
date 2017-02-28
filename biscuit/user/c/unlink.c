#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s <file>\n", argv[0]);

	char *p = argv[1];
	struct stat st;
	if (stat(p, &st) == -1)
		err(-1, "stat");
	if (S_ISDIR(st.st_mode)) {
		if (rmdir(p) == -1)
			err(-1, "rmdir");
	} else if (unlink(p) == -1)
			err(-1, "unlink");
	return 0;
}
