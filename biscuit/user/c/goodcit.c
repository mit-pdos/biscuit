#include <litc.h>

void cmb(void)
{
	if (mkdir("/mb", 0700) == -1)
		err(-1, "mkdir");
	if (chdir("/mb") == -1)
		err(-1, "chdir");
	char * const args[] = {"/bin/cmailbench", "-d", "1", "./", "1", NULL};
	execv(args[0], args);
	err(-1, "exec");
}

void forkwait(void (*f)(void))
{
	switch (fork()) {
	case -1:
		err(-1, "fork");
	case 0:
		f();
		errx(-1, "child returned");
	}

	int status;
	if (wait(&status) == -1)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
}

void cleanup(void)
{
	char * const args[] = {"/bin/rmtree", "/mb", NULL};
	execv(args[0], args);
	err(-1, "exec");
}

int main(int argc, char **argv)
{
	for (;;) {
		forkwait(cmb);
		forkwait(cleanup);
		sleep(3);
	}

	return 0;
}
