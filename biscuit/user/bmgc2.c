#include <litc.h>

int main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s nkernelobjs", argv[0]);

	ulong kn = atoul(argv[1]);
	printf("adding %ld kernel objects...\n", kn);
	fake_sys(kn);
	ulong st = rdtsc();

	int i;
	for (i = 0; i < 100; i++)
		if (fork() == 0)
			exit(0);
	ulong tot = rdtsc() - st;

	int gctime;
	gctime = fake_sys(0);

	printf("%ld / %ld cycles GCing\n", gctime, tot);

	return 1;
}
