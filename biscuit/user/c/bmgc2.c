#include <litc.h>

static long
nowms(void)
{
	struct timeval tv;
	if (gettimeofday(&tv, NULL))
		err(-1, "gettimeofday");
	return tv.tv_sec*1000 + tv.tv_usec/1000;
}

static long
_fetch(long n)
{
	long ret;
	if ((ret = sys_info(n)) < 0)
		errx(-1, "sysinfo");
	return ret;
}

static long
gccount(void)
{
	return _fetch(SINFO_GCCOUNT);
}

static long
gctotns(void)
{
	return _fetch(SINFO_GCPAUSENS);
}

static long
gcheapuse(void)
{
	return _fetch(SINFO_GCHEAPSZ);
}

struct res_t {
	long xput;
	long longest;
};

__attribute__((noreturn))
static void _workmmap(long endms, int resfd)
{
	long longest = 0;
	long count = 0;
	while (1) {
		long st = nowms();
		if (st > endms)
			break;
		size_t sz = 4096 * 100;
		void *m = mmap(NULL, sz, PROT_READ | PROT_WRITE,
		    MAP_PRIVATE | MAP_ANON, -1, 0);
		if (m == MAP_FAILED)
			err(-1, "mmap");
		if (munmap(m, sz))
			err(-1, "munmap");
		long tot = nowms() - st;
		if (tot > longest)
			longest = tot;
		count++;
	}
	struct res_t rs = {.xput = count, .longest = longest};
	if (write(resfd, &rs, sizeof(rs)) != sizeof(rs))
		err(-1, "res write");
	exit(0);
}

static void work(long wf, const long np)
{
	long secs = wf;
	if (secs < 0)
		secs = 1;
	else if (secs > 60)
		secs = 60;

	printf("working for %ld seconds with %ld processes...\n", secs, np);

	int resp[2];
	if (pipe(resp))
		err(-1, "pipe");

	long bgcs = gccount();
	long bgcns = gctotns();

	long endms = nowms() + secs*1000;
	pid_t ps[np];
	int i;
	for (i = 0; i < np; i++) {
		if ((ps[i] = fork()) < 0)
			errx(-1, "fork");
		else if (ps[i] == 0) {
			close(resp[0]);
			_workmmap(endms, resp[1]);
		}
	}
	close(resp[1]);

	struct gcfrac_t gcf = gcfracst();
	//fake_sys(1);

	long longest = 0, totalxput = 0;
	long longarr[np];
	for (i = 0; i < np; i++) {
		int status;
		if (wait(&status) < 0)
			err(-1, "wait");
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			err(-1, "child failed");

		struct res_t got;
		if (read(resp[0], &got, sizeof(got)) != sizeof(got))
			err(-1, "res read");

		totalxput += got.xput;
		longarr[i] = got.longest;
		if (got.longest > longest)
			longest = got.longest;
	}

	//fake_sys(0);

	long gcs = gccount() - bgcs;
	long gcns = gctotns() - bgcns;

	long xput = secs > 0 ? totalxput/secs : 0;
	printf("iterations/sec: %ld (%ld total)\n", xput, totalxput);
	printf("CPU time GC'ing: %f%%\n", gcfracend(&gcf, NULL, NULL, NULL));
	printf("max latency: %ld ms\n", longest);
	printf("each process' latency:\n");
	for (i = 0; i < np; i++)
		printf("     %ld\n", longarr[i]);
	printf("%ld gcs (%ld ms)\n", gcs, gcns/1000000);
	printf("kernel heap use:   %ld Mb\n", gcheapuse()/(1 << 20));
}

int _vnodes(long sf)
{
	size_t nf = 1000*sf;
	printf("creating %zu vnodes...\n", nf);
	size_t tenpct = nf/10;
	size_t next = 1;
	size_t n;
	for (n = 0; n < nf; n++) {
		int fd = open("dummy", O_CREAT | O_EXCL | O_RDWR, S_IRWXU);
		if (fd < 0)
			err(-1, "open");
		if (unlink("dummy"))
			err(-1, "unlink");
		size_t cp = n/tenpct;
		if (cp >= next) {
			printf("%zu%%\n", cp*10);
			next = cp + 1;
		}
	}

	return 0;
}

__attribute__((noreturn))
void usage(void)
{
	printf("usage:\n");
	printf("%s [-g] [-s <int>] [-w <int>] [-n <int>]\n", __progname);
	printf("where:\n");
	printf("-g		force kernel GC, then exit\n");
	printf("-s <int>	set scale factor to int\n");
	printf("-w <int>	set work factor to int\n");
	printf("-n <int>	set number of worker processes to int\n\n");
	exit(-1);
}

int main(int argc, char **argv)
{
	long sf = 1, wf = 1, nprocs = 1;
	int dogc = 0;

	int c;
	while ((c = getopt(argc, argv, "n:gms:w:")) != -1) {
		switch (c) {
		case 'g':
			dogc = 1;
			break;
		case 'n':
			nprocs = strtol(optarg, NULL, 0);
			break;
		case 's':
			sf = strtol(optarg, NULL, 0);
			break;
		case 'w':
			wf = strtol(optarg, NULL, 0);
			break;
		default:
			usage();
			break;
		}
	}

	if (optind != argc)
		usage();

	if (dogc) {
		_fetch(10);
		printf("kernel heap use:   %ld Mb\n", gcheapuse()/(1 << 20));
		return 0;
	}

	if (sf < 0)
		sf = 1;
	if (wf < 0)
		wf = 1;
	if (nprocs < 0)
		nprocs = 1;
	printf("scale factor: %ld, work factor: %ld, worker procs: %ld\n",
	    sf, wf, nprocs);

	long st = nowms();

	int (*f)(long) = _vnodes;
	if (f(sf))
		return -1;

	long tot = nowms() - st;
	printf("setup: %ld ms\n", tot);

	work(wf, nprocs);

	return 0;
}
