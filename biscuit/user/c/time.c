#include <litc.h>

ulong now()
{
	struct timeval t;
	int ret;
	if ((ret = gettimeofday(&t, NULL)) < 0)
		err(ret, "gettimeofday");

	// ms
	return t.tv_sec * 1000 + t.tv_usec / 1000;
}

int main(int argc, char **argv)
{
	if (argc < 2)
		errx(-1, "usage: %s <command> <arg1> ...", argv[0]);

	ulong start = now();

	if (fork() == 0) {
		int ret;
		ret = execvp(argv[1], &argv[1]);
		err(ret, "execv");
	}

	int status;
	wait(&status);
	ulong elapsed = now() - start;

	if (!WIFEXITED(status) || WEXITSTATUS(status))
		printf("child failed with status: %d\n", WEXITSTATUS(status));

	printf("%lu seconds, %lu ms\n", elapsed/1000, elapsed%1000);
	return 0;
}
