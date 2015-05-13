#include <litc.h>

void child(int id)
{
	int i;
	for (i = 0; i < 3; i++) {
		printf("hello from %d\n", id);
	}
	exit(id);
}

int main(int argc, char **argv)
{
	int pid = 0;
	int id = 0;
	while (id < 100) {
		pid = fork();
		id++;
		if (!pid)
			child(id);
		//if (wait(NULL) != pid)
		//	errx(-1, "wrong pid %d", pid);
		if (wait4(pid, NULL, 0, NULL) != pid)
			errx(-1, "wrong pid %d", pid);
		if (wait4(pid - 1, NULL, 0, NULL) > 0)
			errx(-1, "wait4 should fail");
		if (wait(NULL) > 0)
			errx(-1, "wait should fail");
	}

	printf("parent done!\n");
	return 0;
}
