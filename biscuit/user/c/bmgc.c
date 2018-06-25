#include <litc.h>

static long
_fetch(long n)
{
	long ret;
	if ((ret = sys_info(n)) == -1)
		errx(-1, "sysinfo");
	return ret;
}

__attribute__((unused))
static long
gccount(void)
{
	return _fetch(SINFO_GCCOUNT);
}

__attribute__((unused))
static long
gctotns(void)
{
	return _fetch(SINFO_GCPAUSENS);
}

__attribute__((unused))
static long
gcheapuse(void)
{
	return _fetch(SINFO_GCHEAPSZ);
}

__attribute__((noreturn))
void usage(const char *msg)
{
	if (msg != NULL)
		printf("%s\n\n", msg);
	printf("usage:\n");
	printf("%s [-mvSg] [-h <int>] [-s <int>] [-w <int>] [-n <int>] "
	    "[-l <int>]\n", __progname);
	printf("where:\n");
	printf("-d		bloat kernel heap with vnodes\n");
	printf("-g		force kernel GC\n");
	printf("-h <int>	set kernel heap minimum to int MB\n");
	printf("-H <int>	set kernel heap growth factor as int\n");
	printf("-l <int>	set kernel heap reservation max to int MB\n");
	printf("-s <int>/<int>	set GC mutator assist ratio to <int>/<int>\n\n");
	exit(-1);
}

int main(int argc, char **argv)
{
	long kheap = 0, growperc = 0, hmax = 0;
	int newthing = 0, dogc = 0;
	long anum = 0, adenom = 0;

	int c;
	while ((c = getopt(argc, argv, "gdH:h:s:l:")) != -1) {
		switch (c) {
		case 'd':
			newthing = 1;
			break;
		case 'g':
			dogc = 1;
			break;
		case 'h':
			kheap = strtol(optarg, NULL, 0);
			break;
		case 'H':
			growperc = strtol(optarg, NULL, 0);
			break;
		case 's':
			{
			char *end;
			anum = strtol(optarg, &end, 0);
			if (*end != '/')
				usage("invalid ratio");
			end++;
			adenom = strtol(end, NULL, 0);
			if (anum <= 0 || adenom <= 0)
				usage("invalid ratio");
			break;
			}
		case 'l':
			hmax = strtol(optarg, NULL, 0);
			if (hmax == 0)
				errx(-1, "must be non-zero");
			break;
		default:
			usage(NULL);
			break;
		}
	}
	if (optind != argc)
		usage(NULL);

	if (newthing) {
		printf("bloat kernel heap...\n");
		const long hack4 = 1 << 7;
		if (sys_prof(hack4, 0, 0, 0) == -1)
			err(-1, "hack4");
		pause();
	}

	if (kheap) {
		const long hack = 1ul << 4;
		if (sys_prof(hack, kheap, 0, 0) == -1)
			err(-1, "sys prof");
	}

	if (growperc) {
		const long hack2 = 1ul << 5;
		if (sys_prof(hack2, growperc, 0, 0) == -1)
			err(-1, "sys prof");
	}

	if (hmax) {
		hmax <<= 20;
		const long prof_hack5 = 1ul << 8;
		if (sys_prof(prof_hack5, hmax, 0, 0) == -1)
			err(-1, "sys prof");
	}

	if (adenom != 0) {
		const long prof_hack6 = 1ul << 9;
		if (sys_prof(prof_hack6, anum, adenom, 0) == -1)
			err(-1, "sys prof");
	}

	if (dogc) {
		_fetch(10);
		printf("kernel heap use:   %ld Mb\n", gcheapuse()/(1 << 20));
	}

	return 0;
}
