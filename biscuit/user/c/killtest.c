#include <litc.h>

int main(int argc, char **argv)
{
	int pid = fork();
	if (pid == 0)
		while (1);
	printf("killing %d...", pid);
	if (kill(pid, SIGKILL) < 0)
		err(-1, "kill");
	printf("killed. waiting...");
	if (wait(NULL) < 0)
		err(-1, "wait failed\n");
	printf("done\n");
	printf("success\n");
	return 0;
}
