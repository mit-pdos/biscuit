#include <litc.h>

void child(int id)
{
	int i, j;
	for (i = 0; i < 6; i++) {
		printf("hello from %d\n", id);
		for (j = 0; j < 10000000; j++)
			asm volatile("":::"memory");
	}
	int pid = fork();
	if (!pid) {
		for (i = 0; i < 6; i++) {
			printf("hello from baby %d\n", id);
			for (j = 0; j < 10000000; j++)
				asm volatile("":::"memory");
		}
	}
	exit(id);
}

int main()
{
	int pid = 0;
	int id = 0;
	while (pid < 100) {
		pid = fork();
		id++;
		if (!pid)
			child(id);
	}

	//int id = 0;
	//int pid = 0;
	//while (1) {
	//	pid = fork();
	//	if (pid)
	//		exit(id);
	//	id++;
	//	printf("spawn %d\n", id);
	//}
	printf("parent done!\n");
	return 0;
}
