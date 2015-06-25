#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 3)
		errx(-1, "%s <old filename> <new filename>\n", argv[0]);

	char *old = argv[1];
	char *new = argv[2];

	int ret;
	if ((ret = rename(old, new)) < 0)
		err(ret, "rename");
	return 0;
}
