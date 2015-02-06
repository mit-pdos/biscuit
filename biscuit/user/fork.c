#include <litc.h>

int main()
{
	int pid = 0;
	int id = 0;
	while (pid < 10000) {
		pid = fork();
		id++;
		if (!pid) {
			int i, j;
			for (i = 0; i < 6; i++) {
				printf("hello from %d\n", id);
				for (j = 0; j < 10000000; j++)
					asm volatile("":::"memory");
			}
			exit(id);
		}
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
	return 0;
}
