#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s <pid>\n", argv[0]);

	int pid = atoi(argv[1]);
	return kill(pid, SIGKILL);
}
