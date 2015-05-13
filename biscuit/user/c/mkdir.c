#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s <file>\n", argv[0]);

	int ret = mkdir(argv[1], 0);
	if (ret)
		err(ret, "mkdir");
	return 0;
}
