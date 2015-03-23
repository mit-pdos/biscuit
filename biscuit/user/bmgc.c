#include <litc.h>

int main(int argc, char **argv)
{
	// # of fds to open per process
	int p_fds = 100;

	if (argc >= 2)
		p_fds = atoi(argv[1]);

	// use getpid to reset gc ticks
	getpid();

	int i;
	int p10 = p_fds / 10;
	for (i = 0; i < p_fds; i++) {
		int fd;
		if ((fd = open("/clouseau.txt", O_RDONLY, 0)) < 0)
			err(fd, "open");
		if ((i % p10) == 0)
			printf(".");
	}
	printf("opened %d fds\n", p_fds);
	return 0;
}
