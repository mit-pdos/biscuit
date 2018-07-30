#include <litc.h>

static void fexec(char * const args[])
{
	printf("init exec: ");
	for (char * const * p = &args[0]; *p; p++)
		printf("%s ", *p);
	printf("\n");

	switch (fork()) {
	case -1:
		err(-1, "fork (%s)", args[0]);
	case 0:
		execv(args[0], args);
		err(-1, "exec (%s)", args[0]);
	default:
		{
		int status;
		if (wait(&status) == -1)
			err(-1, "wait");
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			err(-1, "child failed (%s)", args[0]);
		}
	}
}

int main(int argc, char **argv)
{
	printf("init starting...\n");

	// create dev nodes
	mkdir("/dev", 0);
	int ret;
	ret = mknod("/dev/console", 0, MKDEV(1, 0));
	if (ret != 0 && errno != EEXIST)
		err(-1, "mknod");
	ret = mknod("/dev/null", 0, MKDEV(4, 0));
	if (ret != 0 && errno != EEXIST)
		err(-1, "mknod");
	ret = mknod("/dev/rsd0c", 0, MKDEV(5, 0));
	if (ret != 0 && errno != EEXIST)
		err(-1, "mknod");
	ret = mknod("/dev/stats", 0, MKDEV(6, 0));
	if (ret != 0 && errno != EEXIST)
		err(-1, "mknod");
	ret = mknod("/dev/prof", 0, MKDEV(7, 0));
	if (ret != 0 && errno != EEXIST)
		err(-1, "mknod");

	char * const largs [] = {"/bin/bmgc", "-l", "512", NULL};
	fexec(largs);
	char * const hargs [] = {"/bin/bmgc", "-h", "470", NULL};
	fexec(hargs);

	for (;;) {
		int pid = fork();
		if (!pid) {
			char * const args[] = {"/bin/lsh", NULL};
			execv(args[0], args);
			err(-1, "execv");
		}
		wait(NULL);
		printf("lsh terminated?\n");
	}
	return 0;
}
