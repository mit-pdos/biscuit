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
	while (pid < 1000) {
		pid = fork();
		id++;
		if (!pid)
			child(id);
	}
	printf("parent done!\n");
	return 0;
}
