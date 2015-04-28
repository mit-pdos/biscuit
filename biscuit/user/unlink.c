#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s <file>\n", argv[0]);

	int ret = unlink(argv[1]);
	if (ret)
		err(ret, "unlink");
	return 0;
}
