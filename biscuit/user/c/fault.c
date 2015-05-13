#include <litc.h>

int main(int argc, char **argv)
{
	int i;
	for (i = 0; i < 3; i++) {
		printf("hello world!\n");
		int j;
		for (j = 0; j < 100000000; j++)
			asm volatile("":::"memory");
	}

	printf("faulting!\n");

	int *p = (int *)0;
	*p = 0;

	return 0;
}
