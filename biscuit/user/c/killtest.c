#include <litc.h>

int main(int argc, char **argv)
{
	int pid = fork();
	if (pid == 0)
		while (1);
	int ret;
	printf("killing %d...", pid);
	if ((ret = kill(pid, SIGKILL)) < 0)
		errx(ret, "kill");
	printf("killed. waiting...");
	if ((ret = wait(NULL)) < 0)
		errx(ret, "wait failed\n");
	printf("done\n");
	printf("success\n");
	return 0;
}
