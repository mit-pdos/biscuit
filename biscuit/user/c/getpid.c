#include <litc.h>

int main()
{
	int i;
	//fork();
	for (i = 0; i < 30000; i++) {
		int pid = getpid();
		printf("my pid is %d.\n", pid);
	}

	return 0;
}
