#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 2) {
		printf("usage: %s <seconds>\n", argv[0]);
		exit(-1);
	}

	uint secs = atoi(argv[1]);
	if (secs < 0)
		errx(-1, "negative time");
	return sleep(secs);
}
