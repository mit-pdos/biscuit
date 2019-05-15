#include <litc.h>

ulong now(void)
{
	struct timeval t;
	int ret;
	if ((ret = gettimeofday(&t, NULL)) < 0)
		err(-1, "gettimeofday");

	// ms
	return t.tv_sec * 1000 + t.tv_usec / 1000;
}

static struct {
	char *name;
	long evid;
	char *desc;
} evs[] = {
	{"cpu", PROF_EV_UNHALTED_CORE_CYCLES, "unhalted CPU cycles"},
	{"binst", PROF_EV_BRANCH_INSTR_RETIRED, "branch instructions"},
	{"bmiss", PROF_EV_BRANCH_MISS_RETIRED, "branch misses"},
	{"llcmiss", PROF_EV_LLC_MISSES, "LLC references"},
	{"llcref", PROF_EV_LLC_REFS, "LLC misses"},
	{"dtlbmissl", PROF_EV_DTLB_LOAD_MISS_ANY, "dTLB load misses"},
	{"dtlbmisss", PROF_EV_STORE_DTLB_MISS, "dTLB store misses"},
	{"itlbmiss", PROF_EV_ITLB_LOAD_MISS_ANY, "iTLB misses"},
	{"ins", PROF_EV_INSTR_RETIRED, "instructions retired"},
	{"ldm_stalls", PROF_EV_CYCLES_STALLS_LDM_PENDING,
	    "cycles w/idle execution ports and >0 outstanding loads"},
};
const int nevs = sizeof(evs)/sizeof(evs[0]);

__attribute__((noreturn))
void usage(char *pre)
{
	if (pre)
		fprintf(stderr, "%s\n\n", pre);
	fprintf(stderr, "usage: %s [-bgr] [-sc pmf] [-e evt] [-i int] <command> "
	    "<arg1> ...\n"
	         "\n"
		 "-b     record backtrace; only used with -s\n"
		 "-r     goprofile command\n"
		 "-g     report GC statistics for command\n"
		 "-s     sample via PMU. must provide \"pmf\" (see below)\n"
		 "       and provide an event via -e\n"
		 "-c     count events via PMU. must provide \"pmf\"\n"
		 "       (see below) and provide an event via -e\n"
		 "pmf    a one or two character string indicating when to\n"
		 "       count/sample the specified events. \"u\" and \"s\"\n"
		 "       cause PMU monitoring during userspace and system\n"
		 "       execution, repsectively. these flags may be\n"
		 "       combined (\"us\").\n"
		 "-e evt use \"evt\" for PMU sampling/counting. for PMU\n"
		 "       counting, -e may be specified more than once.\n"
		 "evt    a string indicating which symbolic event the PMU\n"
		 "       should monitor. valid strings are:\n"
		 , __progname);
	for (int i = 0; i < nevs; i++)
		fprintf(stderr, "    %10s - %s\n", evs[i].name,
		    evs[i].desc);
	fprintf(stderr, "-i int sample after int PMU events. only used with -s.\n"
 	    "\n");
	exit(-1);
}

long ppmf(char *pmf)
{
	long ret = 0;
	if (strchr(pmf, 'u'))
		ret |= PROF_EVF_USR;
	if (strchr(pmf, 's'))
		ret |= PROF_EVF_OS;
	if (!ret)
		usage("invalid pmf");
	return ret;
}

long evtadd(char *evt)
{
	int i;
	for (i = 0; i < nevs; i++) {
		if (strcmp(evs[i].name, evt) != 0)
			continue;
		return evs[i].evid;
	}
	usage("invalid evt");
}

int main(int argc, char **argv)
{
	int goprof = 0, gcstat = 0, nevts = 0, bt = 0;
	long pmuc = 0, pmus = 0, evt = 0, intperiod=1000000;
	int ch;
	while ((ch = getopt(argc, argv, "bi:s:c:e:gr")) != -1) {
		switch (ch) {
		case 'b':
			bt = 1;
			break;
		case 'c':
			pmuc = ppmf(optarg);
			break;
		case 'e':
			evt |= evtadd(optarg);
			nevts++;
			break;
		case 'g':
			gcstat = 1;
			break;
		case 'i':
			intperiod = strtol(optarg, NULL, 0);
			break;
		case 'r':
			goprof = 1;
			break;
		case 's':
			pmus = ppmf(optarg);
			break;
		default:
			fprintf(stderr, "bad option: %c\n", ch);
			usage(NULL);
			break;
		}
	}
	argc -= optind;
	argv += optind;

	if (argc < 1)
		usage(NULL);
	if (pmuc && pmus)
		usage("cannot use -s and -c");
	if ((pmuc || pmus) && evt == 0)
		usage("must specify -e at least once");
	if (pmus && nevts != 1)
		usage("must specify -e exactly once for sampling");
	if (bt && !pmus)
		usage("-b can only be used with sampling");

	ulong start = now();

	// start profiling
	if (bt)
		pmus |= PROF_EVF_BACKTRACE;
	if (goprof && sys_prof(PROF_GOLANG, 0, 0, 0) == -1)
		errx(-1, "prof start");
	if (pmuc && sys_prof(PROF_COUNT, evt, pmuc, 0) == -1)
		errx(-1, "sys prof");
	else if (pmus && sys_prof(PROF_SAMPLE, evt, pmus, intperiod) == -1)
		errx(-1, "sys prof");
	struct gcfrac_t fracst;
	long sgc, talloc;
	if (gcstat) {
		fracst = gcfracst();
		sgc = sys_info(SINFO_GCCOUNT);
		if (sgc == -1)
			err(-1, "sysinfo");
		talloc = sys_info(SINFO_GCTOTALLOC);
		if (talloc == -1)
			err(-1, "sysinfo");
	}

	if (fork() == 0) {
		execvp(argv[0], &argv[0]);
		err(-1, "execv");
	}

	struct rusage r;
	int status;
	int ret = wait4(WAIT_ANY, &status, 0, &r);
	if (ret < 0)
		err(-1, "wait4");
	ulong elapsed = now() - start;

	if (gcstat) {
		long wbms, markms, sweepms;
		double gccpu = gcfracend(&fracst, &markms, &sweepms, &wbms);
		fprintf(stderr, "GC CPU frac: %f%% (no write barriers)\n",
		    gccpu);
		fprintf(stderr, " total elapsed times:\n");
		fprintf(stderr, "      mark   ms: %ld\n", markms);
		fprintf(stderr, "      sweep  ms: %ld\n", sweepms);
		fprintf(stderr, "      writeb ms: %ld\n", wbms);

		long egc = sys_info(SINFO_GCCOUNT);
		if (egc == -1)
			err(-1, "sysinfo");
		fprintf(stderr, "GCs: %ld\n", egc - sgc);
		long etalloc = sys_info(SINFO_GCTOTALLOC);
		if (etalloc == -1)
			err(-1, "sysinfo");
		long bpms = (etalloc - talloc) / elapsed;
		double ar = (double)bpms * 1000 / (1 << 20);
		fprintf(stderr, "Allocation rate: %f MB/sec\n", ar);

		long kobjs = sys_info(SINFO_GCOBJS);
		if (kobjs == -1)
			err(-1, "sysinfo");
		fprintf(stderr, "Number of kernel objects: %ld\n", kobjs);
	}
	// stop profiling
	if (goprof && sys_prof(PROF_DISABLE|PROF_GOLANG, 0, 0, 0) == -1)
		errx(-1, "prof stop");
	if (pmuc && sys_prof(PROF_DISABLE|PROF_COUNT, evt, pmuc, 0) == -1)
		errx(-1, "sys prof stop");
	else if (pmus && sys_prof(PROF_DISABLE|PROF_SAMPLE, 0, 0, 0) == -1)
		errx(-1, "sys prof stop");

	ret = WEXITSTATUS(status);
	if (!WIFEXITED(status) || ret)
		fprintf(stderr, "child failed with status: %d\n", ret);

	fprintf(stderr, "%lu seconds, %lu ms\n", elapsed/1000, elapsed%1000);
	fprintf(stderr, "user   time: %lu seconds, %lu us\n",
	    r.ru_utime.tv_sec, r.ru_utime.tv_usec);
	fprintf(stderr, "system time: %lu seconds, %lu us\n",
	    r.ru_stime.tv_sec, r.ru_stime.tv_usec);
	return ret;
}
