#include <litc.h>

static void gcrun(char * const *cmd, long *_ngcs, long *_xput, double *_gcfrac)
{
	int p[2];
	if (pipe(p) == -1)
		err(-1, "pipe");
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	if (!c) {
		close(p[0]);
		if (dup2(p[1], 1) == -1)
			err(-1, "dup2");
		close(p[1]);

		const int cmdsz = 62;
		char *cmds[62+2] = {"time", "-g"};
		int i;

		for (i = 0; i < cmdsz; i++) {
			cmds[2+i] = cmd[i];
			if (cmd[i] == NULL)
				break;
		}
		execvp(cmds[0], cmds);
		err(-1, "exec");
	}
	close(p[1]);

	long ngcs = -1;
	long xput = 0;
	double gcfrac = 0;

	char buf[512];
	ssize_t r;
	int off = 0;
	while ((r = read(p[0], &buf[off], sizeof(buf) - off - 1)) > 0) {
		char *end = &buf[off+r];
		*end = '\0';
		if (strchr(buf, '\n') == NULL) {
			fprintf(stderr, "warning: ignoring long line\n");
			off = 0;
			continue;
		}
		char *nl, *last = buf;
		while ((nl = strchr(last, '\n')) != NULL) {
			*nl = '\0';
			sscanf(last, "GCs: %ld", &ngcs);
			sscanf(last, "GC CPU frac: %lf", &gcfrac);
			sscanf(last, "\tops: %ld", &xput);
			last = nl + 1;
		}
		off = end - last;
		memmove(buf, last, off);
	}
	if (r == -1)
		err(-1, "read");

	int status;
	if (wait(&status) == -1)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
	close(p[0]);

	if (_ngcs)
		*_ngcs = ngcs;
	if (_gcfrac)
		*_gcfrac = gcfrac;
	if (_xput)
		*_xput = xput;
}

static void _run(char **cmd)
{
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	if (!c) {
		int fd = open("/dev/null", O_WRONLY);
		if (fd == -1)
			err(-1, "open");
		if (dup2(fd, 1) == -1)
			err(-1, "dup2");
		close(fd);
		execvp(cmd[0], cmd);
		err(-1, "execvp");
	}
	int status;
	if (wait(&status) == -1)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
}

// format the benchmark command given the parameters. only the most recent
// command returned is valid.
static void _mkbmcmd(char **cmd, size_t ncmd, long allocr, long duration)
{
	if (ncmd < 6)
		errx(-1, "little cmd buf");
	// duration in seconds
	static char dbuf[32];
	snprintf(dbuf, sizeof(dbuf), "-s%ld", duration);
	static char abuf[32];
	snprintf(abuf, sizeof(abuf), "-A%ld", allocr);
	cmd[0] = "sfork";
	cmd[1] = dbuf;
	cmd[2] = "-ba";
	cmd[3] = abuf;
	cmd[4] = "1";
	cmd[5] = NULL;
}

#define	GOGC	100

// find the xput for a run of benchmark at a particular allocation rate
__attribute__((unused))
static long nogcxput(long allocr)
{
	errx(-1, "do not use; taints GC costs for smaller heaps");
	// set kernel heap size to ~16GB to avoid any gcs
	char *hcmd[] = {"bmgc", "-h", "16000", NULL};
	char *rcmd2[] = {"bmgc", "-g", NULL};
	_run(hcmd);
	_run(rcmd2);

	const int ncmd = 10;
	char *cmd[ncmd];
	_mkbmcmd(cmd, ncmd, allocr, 10);
	long ngcs, xput;
	int i;
	for (i = 0; i < 10; i++) {
		gcrun(cmd, &ngcs, &xput, NULL);
		if (ngcs == 0)
			break;
		xput = 0;
	}
	if (!xput)
		errx(-1, "failed to get 0 gc xput for allocrate %ld", allocr);

	// restore old total heap size
	char *rcmd1[] = {"bmgc", "-H", "100", NULL};
	_run(rcmd1);
	_run(rcmd2);

	return xput;
}

// for a given gc cpu fraction upperbound and allocation rate, find total heap
// sizing to keep gc cpu time < that gc cput fraction upperbound. returns the
// total heap size in GOGC terms (all heap sizes are in GOGC terms).
static int findtotalheapsz(double gcfracub, long allocr, const long targetgcs,
    const long gc0xput)
{
	// first, get the xput of this allocation rate with 0 gcs. we use that
	// xput to calculate GC CPU time.
	//long gc0xput = nogcxput(allocr);
	printf("0gc xput is %ld\n", gc0xput);

	// initialize binary search bounds
#define HIGC		700
	int higc = HIGC;
	int logc = 10;
	// minimum heap size increment
#define GCINC		10
	const int gcinc = GCINC;
#define TRIEDSZ 	((HIGC/GCINC)+1)
	// byte array tracking whether we tried a certain heap size yet. when
	// we find two adjacents heap sizes that have been tried but had
	// different outcomes (i.e. gc fraction was higher than upper bound for
	// one and lower than upperbound for the other), we have found the
	// target heap size.
	const char _nottried = 0;
	// this slot resulted in gc frac lower than upperbound
	const char _lower = -1;
	// this slot resulted in gc frac higher than upperbound
	const char _higher = 1;
	char tried[TRIEDSZ] = {0};

	long duration = 10;
	char lastres = _nottried;
	while (1) {
		int curgc = (higc + logc) / 2;
		// round to multiple of incgc
		curgc = (curgc/gcinc)*gcinc;
		int curslot = curgc / gcinc;
		// make sure to round to final slot
		if (tried[curslot] != _nottried) {
			if (lastres == _lower)
				curgc -= gcinc;
			else if (lastres == _higher)
				curgc += gcinc;
			else
				errx(-1, "bad lastres: %d", lastres);
			curslot = curgc/gcinc;
			if (curslot < 0 || curslot >= TRIEDSZ)
				errx(-1, "cannot meet goal gc cpu fraction");
			if (tried[curslot] != _nottried)
				errx(-1, "rounded, but already tried?");
		}
		printf("=== trying GOGC heap size of %d ===\n", curgc);
		char heapszbuf[32];
		snprintf(heapszbuf, sizeof(heapszbuf), "%d", curgc);
		char *resizecmds[] = {"bmgc", "-H", heapszbuf, NULL};
		_run(resizecmds);

		// run the benchmark for increasing durations of time until we
		// have at least 20 gcs.
		long foundxput;
		while (1) {
			const int ncmds = 32;
			char *cmds[32];
			_mkbmcmd(cmds, ncmds, allocr, duration);
			long xput, ngcs;
			printf("trying allocr %ld with heap %d for %ld "
			    "seconds...\n", allocr, curgc, duration);
			gcrun(cmds, &ngcs, &xput, NULL);
			if (ngcs >= targetgcs) {
				printf("good. got %ld gcs\n", ngcs);
				foundxput = xput;
				// prevent duration from growing too large too
				// quickly
				if (ngcs / targetgcs > 1)
					duration /= ngcs / targetgcs;
				break;
			}
			printf("only %ld gcs, trying again...\n", ngcs);
			double gcps = (double)ngcs/duration;
			if (gcps == 0)
				duration = 120;
			else
				duration = targetgcs/gcps + (duration/8);

			char *prep[] = {"bmgc", "-g", NULL};
			_run(prep);
		}

		// finally have a run with >20 gcs. calculate gc cpu frac.
		double gcfrac = 1.0 - (double)foundxput/gc0xput;
		if (gcfrac <= gcfracub)
			lastres = _lower;
		else
			lastres = _higher;
		tried[curslot] = lastres;

		// adjust binary search bounds and duration
		int lastgc = curgc;
		if (lastres == _lower)
			higc = curgc;
		else
			logc = curgc;
		duration *= (double)curgc/lastgc;
		printf("      GC frac: %f\n", gcfrac);
		printf("      adjust heap size %s\n",
		    lastres == _lower ? "SMALLER" : "BIGGER");

		// see if we are done
		int i;
		for (i = 1; i < TRIEDSZ; i++) {
			if (tried[i-1] == _nottried || tried[i] == _nottried)
				continue;
			if (tried[i-1] != tried[i]) {
				printf("FOUND\n");
				int low = (i-1)*gcinc;
				int hi = i*gcinc;
				char st = tried[i-1];
				printf("    heap %d: %s\n", low,
				    st == _lower ? "SMALLER" : "BIGGER");
				st = tried[i];
				printf("    heap %d: %s\n", hi,
				    st == _lower ? "SMALLER" : "BIGGER");
				return hi;
			}
		}
	}
}

__attribute__((unused))
static void usage()
{
	fprintf(stderr, "usage: %s [-n target gcs] [-c target gc frac]"
	    " -x <0gc xput> <allocr>\n", __progname);
	exit(-1);
}

int main(int argc, char **argv)
{
	long targetgcs = 20;
	double gctarget = 0.055;
	long gc0x = 0;
	int c;
	while ((c = getopt(argc, argv, "x:n:mc:")) != -1) {
		switch (c) {
		case 'c':
			gctarget = strtod(optarg, NULL);
			if (gctarget < 0 || gctarget > 100.0)
				gctarget = 0.055;
			break;
		case 'n':
			targetgcs = strtol(optarg, NULL, 0);
			if (targetgcs < 0)
				targetgcs = 20;
			break;
		case 'x':
			gc0x = strtol(optarg, NULL, 0);
			break;
		default:
			usage();
			break;
		}
	}
	if (argc - optind != 1 || gc0x <= 0)
		usage();
	long allocr = strtol(argv[optind], NULL, 0);
	if (allocr < 0)
		allocr = 32;
	long idealheap = findtotalheapsz(gctarget, allocr, targetgcs, gc0x);
	printf("ideal heap for allocr %ld: %ld\n", allocr, idealheap);
	return 0;
}
