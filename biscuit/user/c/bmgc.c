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
	if ((ret = fake_sys2(n)) < 0)
		errx(-1, "fake 2");
	return ret;
}

static long
gccount(void)
{
	return _fetch(0);
}

static long
gctotns(void)
{
	return _fetch(1);
}

static long
gcheapuse(void)
{
	return _fetch(2);
}

__attribute__((unused))
static long
kmemtotal(void)
{
	return _fetch(3);
}

__attribute__((unused))
static long
kheap(void)
{
	return _fetch(4);
}

__attribute__((unused))
static long
kstack(void)
{
	return _fetch(5);
}

char buf[512];

static void work(long wf)
{
	int fd = open("/bin/mailbench", O_RDONLY);
	if (fd < 0)
		err(-1, "open");

	long bgcs = gccount();
	long bgcns = gctotns();

	long begin = nowms();
	long secs = wf;
	if (secs < 0)
		secs = 1;
	else if (secs > 60)
		secs = 60;
	printf("working for %ld seconds...\n", secs);

	long end = begin + secs*1000;
	long longest = 0;
	while (1) {
		long st = nowms();
		if (st > end)
			break;
		if (lseek(fd, 0, SEEK_SET) < 0)
			err(-1, "lseek");
		ssize_t r;
		while ((r = read(fd, buf, sizeof(buf))) > 0)
			;
		if (r < 0)
			err(-1, "read");
		long tot = nowms() - st;
		if (tot > longest)
			longest = tot;
	}
	close(fd);

	printf("max latency: %ld ms\n", longest);
	long gcs = gccount() - bgcs;
	long gcns = gctotns() - bgcns;
	printf("%ld gcs (%ld ms)\n", gcs, gcns/1000000);
	printf("kernel heap use:   %ld Mb\n", gcheapuse()/(1 << 20));
	printf("kernel stack size: %ld Mb\n", kstack()/(1 << 20));
}

int _mmap(long sf)
{
	const size_t pgsize = 1ull << 12;
	const int pgs = (1ull << 10)*sf;
	printf("creating %d vm regions...\n", pgs);
	int i;
	int tenpct = pgs/10;
	int next = 1;
	for (i = 0; i < pgs; i++) {
		char *mem;
		void *hack = (void *)(intptr_t)(i*2*pgsize);
		if ((mem = mmap(hack, pgsize, PROT_READ | PROT_WRITE,
		    MAP_ANON | MAP_PRIVATE, 0x31337, 0)) == MAP_FAILED)
			err(-1, "mmap");
		int cp = i/tenpct;
		if (cp >= next) {
			printf("%d%%\n", cp*10);
			next = cp + 1;
		}
	}

	return 0;
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
	printf("%s [-mSg] [-s <int>] [-w <int>]\n", __progname);
	printf("where:\n");
	printf("-S		sleep forever instead of exiting\n");
	printf("-m		mmap setup instead of vnodes\n");
	printf("-g		force kernel GC, then exit\n");
	printf("-s <int>	set scale factor to int\n");
	printf("-w <int>	set work factor to int\n\n");
	exit(-1);
}

int main(int argc, char **argv)
{
	long sf = 1, wf = 1;
	int dosleep = 0, dommap = 0, dogc = 0;

	int c;
	while ((c = getopt(argc, argv, "gms:Sw:")) != -1) {
		switch (c) {
		case 'g':
			dogc = 1;
			break;
		case 'm':
			dommap = 1;
			break;
		case 's':
			sf = strtol(optarg, NULL, 0);
			break;
		case 'S':
			dosleep = 1;
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
		return 0;
	}

	if (sf < 0)
		sf = 1;
	if (wf < 0)
		wf = 1;
	printf("scale factor: %ld, work factor: %ld", sf, wf);
	if (dosleep)
		printf(", sleeping forever\n");
	else
		printf("\n");

	long st = nowms();

	int (*f)(long) = _vnodes;
	if (dommap)
		f = _mmap;
	if (f(sf))
		return -1;

	long tot = nowms() - st;
	printf("setup: %ld ms\n", tot);

	//fake_sys(1);
	work(wf);
	//fake_sys(0);

	if (dosleep) {
		printf("sleeping forever...\n");
		pause();
	}

	return 0;
}
