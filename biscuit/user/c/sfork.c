#include <litc.h>

int _main(int argc, char **argv)
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

// ms
ulong now()
{
	struct timeval t;
	if (gettimeofday(&t, NULL))
		errx(-1, "gettimeofday");
	return t.tv_sec * 1000 + t.tv_usec / 1000;
}

long jointot(pthread_t t[], const int nthreads)
{
	long total = 0;
	int i;
	for (i = 0; i < nthreads; i++) {
		void *retp;
		if (pthread_join(t[i], &retp))
			errx(-1, "pthread join");
		long ret = (long)retp;
		if (ret < 0)
			errx(-1, "thread failed");
		total += ret;
	}
	return total;
}

static int volatile start;
static int volatile cease;

void *crrename(void *idp)
{
	while (!start)
		asm volatile("pause":::"memory");

	char o[32];
	//char n[32];

	long id = (long)idp;
	snprintf(o, sizeof(o), "o%ld", id);
	//snprintf(n, sizeof(n), "o%ld", id);

	int fd = open(o, O_CREAT | O_RDONLY, 0600);
	if (fd < 0)
		err(fd, "open");
	close(fd);

	long total = 0;
	while (!cease) {
		int ret;
		if ((ret = rename(o, o)) < 0)
			err(ret, "rename");
		total++;
	}
	return (void *)total;
}

void *lookups(void *idp)
{
	while (!start)
		asm volatile("pause":::"memory");

	long total = 0;
	while (!cease) {
		int ret;
		if ((ret = open("NONE", O_RDONLY)) != -ENOENT)
			err(ret, "open: unexpected error");
		total++;
	}
	return (void *)total;
}

void *crmessage(void *idp)
{
	while (!start)
		asm volatile("pause":::"memory");

	long id = (long)idp;

	char msg[1660];
	memset(msg, 'X', sizeof(msg));
	long total = 0;
	while (!cease) {
		char o[32];
		snprintf(o, sizeof(o), "f%ld.%ld", id, total);

		int fd = open(o, O_CREAT | O_WRONLY | O_EXCL, 0600);
		if (fd < 0)
			err(fd, "open");
		int ret = write(fd, msg, sizeof(msg));
		if (ret < 0)
			err(ret, "write");
		else if (ret != sizeof(msg))
			errx(-1, "short write");
		close(fd);

		total++;
	}
	return (void *)total;
}

int ___main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s <num threads>", argv[0]);

	int nthreads = atoi(argv[1]);
	if (nthreads < 0)
		nthreads = 3;
	const int bmsecs = 5;
	printf("using %d threads for %d seconds\n", nthreads, bmsecs);

	pthread_t t[nthreads];
	int i;
	for (i = 0; i < nthreads; i++)
		if (pthread_create(&t[i], NULL, crmessage, (void *)(long)i))
		//if (pthread_create(&t[i], NULL, lookups, (void *)(long)i))
			errx(-1, "pthread create");

	ulong st = now();
	start = 1;

	sleep(bmsecs);
	cease = 1;
	ulong beforejoin = now();
	long total = jointot(t, nthreads);
	ulong actual = now() - st;

	printf("ran for %lu ms (slept %lu)\n", actual, beforejoin - st);
	double secs = actual / 1000;
	printf("ops: %lf /sec\n", (double)total/secs);

	return 0;
}

void __attribute__((noreturn)) child(int cd)
{
	char dir[32];
	snprintf(dir, sizeof(dir), "%d", cd);
	char *args[] = {"/bin/time", "mailbench", dir, "2", NULL};
	//char *args[] = {"/bin/mailbench", dir, "2", NULL};
	int ret = execv(args[0], args);
	err(ret, "execv");
}

int __main(int argc, char **argv)
{
	int cd = 0;
	for (;;) {
		printf("     directory: %d\n", cd);
		char dn[32];
		snprintf(dn, sizeof(dn), "%d", cd);
		int ret;
		if ((ret = mkdir(dn, 0700)) < 0)
			err(ret, "mkdir");
		if (fork() == 0)
			child(cd);
		wait(NULL);
		cd++;
	}
}

static volatile long county;

void *mapper(void *n)
{
	county++;

	while (!start)
		asm volatile("pause":::"memory");

	long c = 0;
	while (!cease) {
		//size_t l = 4096;
		//void *p = mmap(NULL, l, PROT_READ, MAP_PRIVATE | MAP_ANON,
		//    -1, 0);
		//if (p == MAP_FAILED)
		//	errx(-1, "mmap");
		//int ret;
		//if ((ret = munmap(p, l)) < 0)
		//	err(ret, "munmap");

		getpid();
		c++;
	}
	printf("dune\n");
	return (void *)c;
}

int main(int argc, char **argv)
{
	const int bmsecs = 5;
	printf("going for %d seconds\n", bmsecs);

	pthread_t t;
	if (pthread_create(&t, NULL, mapper, NULL))
		errx(-1, "pthread create");

	while (county != 1)
		;
	ulong st = now();
	start = 1;

	sleep(bmsecs);
	cease = 1;
	ulong beforejoin = now();
	long total = jointot(&t, 1);
	ulong actual = now() - st;

	printf("ran for %lu ms (slept %lu)\n", actual, beforejoin - st);
	double secs = actual / 1000;
	printf("ops: %lf /sec\n", (double)total/secs);

	return 0;
}
