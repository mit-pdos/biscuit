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
	int rshd = 0;
	int c;
	while ((c = getopt(argc, argv, "r")) != -1) {
		switch (c) {
		default:
			printf("bad option/arg\n");
			break;
		case 'r':
			rshd = 1;
			break;
		}
	}
	argc -= optind;
	argv += optind;
	if (argc > 0)
		printf("ignoring extra args\n");

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
	if (rshd) {
		printf("starting rshd...\n");
		pid_t rpid = fork();
		if (rpid == 0) {
			char * const rargs [] = {"/bin/rshd", NULL};
			execv(rargs[0], rargs);
			err(-1, "execv");
		} else if (rpid == -1)
			perror("execv");
	}

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
