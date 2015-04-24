#include <litc.h>

int main(int argc, char **argv)
{
	ulong sum = 0;
	ulong times = 0;
	while (1) {
		ulong st = rdtsc();
		if (fork() == 0)
			exit(0);
		ulong tot = rdtsc() - st;
		sum += tot;
		times++;
		if (times % 100 == 0)
			while (wait(NULL) > 0);
		if (times % 5000 == 0) {
			printf("%ld cycles/fork (avg)\n", sum/times);
			times = sum = 0;
		}
	}

	return 0;
}
