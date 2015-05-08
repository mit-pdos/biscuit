#include <litc.h>

int main(int argc, char **argv)
{
	printf("start thread tests\n");
	int pid = fork();
	if (pid == 0) {
		threxit(1001);
		errx(-1, "why no exit");
		return -2;
	}
	int status;
	if (wait(&status) != pid)
		errx(-1, "wrong pid: %d\n", pid);
	if (status != 0)
		errx(-1, "posix is sad");
	return 0;
}
