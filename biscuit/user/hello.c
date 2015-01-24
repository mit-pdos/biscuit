#include <litc.h>

int main() {
	int i;
	for (i = 0; i < 3; i++) {
		pmsg("hello world! ");
		int j;
		for (j = 0; j < 100000000; j++)
			asm volatile("":::"memory");
	}
	pmsg("faulting!");

	int *p = (int *)0;
	*p = 0;

	return 0;
}
