#include <litc.h>

int main(int argc, char **argv)
{
	int rfd;
	if ((rfd = open("/", O_RDONLY, 0)) < 0)
		errx(rfd, "open root");

	return 0;
}
