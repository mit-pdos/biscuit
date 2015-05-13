#include <litc.h>

int main(int argc, char **argv)
{
	int times = 500;

	if (argc != 2)
		errx(-1, "usage: %s nkernelobjs", argv[0]);

	ulong kn = atoul(argv[1]);
	printf("adding %ld kernel objects...\n", kn);
	fake_sys(kn);
	printf("done. forking %d times...\n", times);
	ulong st = rdtsc();

	int i;
	for (i = 0; i < times; i++)
		if (fork() == 0)
			exit(0);
	ulong tot = rdtsc() - st;

	long gctime;
	gctime = fake_sys(0);

	printf("%ld / %ld cycles GCing\n", gctime, tot);
	printf("%ld cycles per fork\n", tot/times);

	return 1;
}
