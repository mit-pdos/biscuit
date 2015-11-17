#include <err.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <sys/un.h>
#include <sys/wait.h>

#include <pthread.h>

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

pthread_barrier_t bar;
static int volatile cease;

static int bmsecs = 10;
static int nthreads;

void *crrename(void *idp)
{
	pthread_barrier_wait(&bar);

	char o[32];
	char n[32];

	long id = (long)idp;
	int pid = getpid();
	snprintf(o, sizeof(o), "o%d-%ld", pid, id);
	snprintf(n, sizeof(n), "n%d-%ld", pid, id);

	int fd = open(o, O_CREAT | O_RDONLY, 0600);
	if (fd < 0)
		err(-1, "open");
	close(fd);

	long total = 0;
	while (!cease) {
		if (rename(o, n) < 0)
			err(-1, "rename");
		if (rename(n, o) < 0)
			err(-1, "rename");
		total += 2;
	}
	return (void *)total;
}

void *lookups(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		if (open("NONE", O_RDONLY) >= 0 || errno != ENOENT)
			err(-1, "open: unexpected error");
		total++;
	}
	return (void *)total;
}

void *mapper(void *n)
{
	pthread_barrier_wait(&bar);

	long c = 0;
	while (!cease) {
		size_t l = 4096;
		void *p = mmap(NULL, l, PROT_READ, MAP_PRIVATE | MAP_ANON,
		    -1, 0);
		if (p == MAP_FAILED)
			errx(-1, "mmap");
		if (munmap(p, l) < 0)
			err(-1, "munmap");

		//getpid();
		c++;
	}
	printf("dune\n");
	return (void *)c;
}

void *crmessage(void *idp)
{
	pthread_barrier_wait(&bar);

	long id = (long)idp;

	char msg[1660];
	memset(msg, 'X', sizeof(msg));
	long total = 0;
	int pid = getpid();
	while (!cease) {
		char o[32];
		snprintf(o, sizeof(o), "f%d-%ld.%ld", pid, id, total);

		int fd = open(o, O_CREAT | O_WRONLY | O_EXCL, 0600);
		if (fd < 0)
			err(-1, "open");
		int ret = write(fd, msg, sizeof(msg));
		if (ret < 0)
			err(-1, "write");
		else if (ret != sizeof(msg))
			errx(-1, "short write");
		close(fd);
		if (unlink(o) < 0)
			err(-1, "unlink");

		total++;
	}
	return (void *)total;
}

void *getpids(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		getpid();
		total++;
	}
	return (void *)total;
}

void *fake2(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		fake_sys2(0);
		total++;
	}
	return (void *)total;
}

void bm(char const *tl, void *(*fn)(void *))
{
	cease = 0;

	pthread_barrier_init(&bar, NULL, nthreads + 1);

	pthread_t t[nthreads];
	int i;
	for (i = 0; i < nthreads; i++)
		if (pthread_create(&t[i], NULL, fn, (void *)(long)i))
			errx(-1, "pthread create");

	pthread_barrier_wait(&bar);

	ulong st = now();
	sleep(bmsecs);

	cease = 1;
	ulong beforejoin = now();
	long total = jointot(t, nthreads);
	ulong actual = now() - st;

	printf("%s ran for %lu ms (slept %lu)\n", tl, actual, beforejoin - st);
	double secs = actual / 1000;
	printf("\tops: %lf /sec\n\n", (double)total/secs);
}

char const *sunsock = "sock";

void sunspawn()
{
	if (fork() != 0) {
		sleep(1);
		return;
	}

	unlink(sunsock);

	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, sunsock, sizeof(sa.sun_path));

	int fd;
	if ((fd = socket(AF_UNIX, SOCK_DGRAM, 0)) < 0)
		err(-1, "socket");

	int ret;
	if (bind(fd, (struct sockaddr *)&sa, sizeof(sa)) < 0)
		err(-1, "bind");

	ulong mcnt;
	ulong bytes;
	mcnt = bytes = 0;
	char buf[64];
	while ((ret = recv(fd, buf, sizeof(buf), 0)) > 0) {
		if (!strncmp(buf, "exit", 4)) {
			ret = 0;
			break;
		}
		mcnt++;
		bytes += ret;
	}

	if (ret < 0)
		err(-1, "recv");
	close(fd);

	printf("%lu total messages received (%lu bytes)\n", mcnt, bytes);
	exit(0);
}

void sunkill()
{
	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, sunsock, sizeof(sa.sun_path));

	int fd;
	if ((fd = socket(AF_UNIX, SOCK_DGRAM, 0)) < 0)
		err(-1, "socket");

	char msg[] = "exit";
	int ret;
	ret = sendto(fd, msg, sizeof(msg), 0, (struct sockaddr *)&sa,
	    sizeof(sa));
	if (ret < 0)
		err(-1, "sendto");
	else if (ret != sizeof(msg))
		errx(-1, "short write");
	close(fd);

	printf("waiting for daemon:\n");
	int status;
	wait(&status);
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "sun daemon exited with status %d", status);
	printf("done\n");
}

void *sunsend(void *idp)
{
	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, sunsock, sizeof(sa.sun_path));

	int fd;
	if ((fd = socket(AF_UNIX, SOCK_DGRAM, 0)) < 0)
		err(-1, "socket");

	pthread_barrier_wait(&bar);

	long total = 0;
	const char msg[] = "123456789";
	while (!cease) {
		int ret;
		ret = sendto(fd, msg, sizeof(msg), 0, (struct sockaddr *)&sa,
		    sizeof(sa));
		if (ret < 0)
			err(-1, "sendto");
		else if (ret != sizeof(msg))
			errx(-1, "short write");

		total++;
	}
	return (void *)total;
}

void *forkonly(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		int pid = fork();
		if (pid < 0)
			err(-1, "fork");
		if (!pid) {
			exit(0);
		}
		int status;
		wait(&status);
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			errx(-1, "child failed: %d", status);
		total++;
	}
	return (void *)total;
}

void *forkexec(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		int pid = fork();
		if (pid < 0)
			err(-1, "fork");
		if (!pid) {
			char * const args[] = {"/bin/true", NULL};
			execv(args[0], args);
			errx(-1, "execv");
		}
		int status;
		wait(&status);
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			errx(-1, "child failed: %d", status);
		total++;
	}
	return (void *)total;
}

void *seqcreate(void *idp)
{
	pthread_barrier_wait(&bar);

	long id = (long)idp;
	int pid = getpid();

	long total = 0;
	while (!cease) {
		char o[64];
		snprintf(o, sizeof(o), "f%d-%ld.%ld", pid, id, total);

		int fd = open(o, O_CREAT | O_WRONLY | O_EXCL, 0600);
		if (fd < 0)
			err(-1, "open");
		close(fd);
		total++;
	}
	return (void *)total;
}

void *openonly(void *idp)
{
	const char *f = "/tmp/dur";
	int fd = open(f, O_CREAT | O_WRONLY, 0600);
	if (fd < 0)
		err(-1, "create");
	close(fd);
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		fd = open(f, O_RDONLY);
		if (fd < 0)
			err(-1, "open");
		close(fd);
		total++;
	}
	return (void *)total;
}

void __attribute__((noreturn)) child(int cd)
{
	char dir[32];
	snprintf(dir, sizeof(dir), "%d", cd);
	//char *args[] = {"/bin/time", "mailbench", dir, "2", NULL};
	char *args[] = {"/bin/mailbench", dir, "1", NULL};
	execv(args[0], args);
	err(-1, "execv");
}

int mbforever()
{
	int cd = 0;
	for (;;) {
		printf("     directory: %d\n", cd);
		char dn[32];
		snprintf(dn, sizeof(dn), "%d", cd);
		if (mkdir(dn, 0700) < 0)
			err(-1, "mkdir");
		if (fork() == 0)
			child(cd);
		wait(NULL);
		cd++;
	}
}

struct {
	char *name;
	char sname;
	void *(*fn)(void *);
	void (*begin)(void);
	void (*end)(void);
} bms[] = {
	{"renames", 'r', crrename, NULL, NULL},
	{"create/write/unlink", 'c', crmessage, NULL, NULL},
	{"unix socket", 'u', sunsend, sunspawn, sunkill},
	{"getpids", 'p', getpids, NULL, NULL},
	{"fake2", 'f', fake2, NULL, NULL},
	{"forkonly", 'k', forkonly, NULL, NULL},
	{"forkexec", 'e', forkexec, NULL, NULL},
	{"seqcreate", 's', seqcreate, NULL, NULL},
	{"openonly", 'o', openonly, NULL, NULL},
};

const int nbms = sizeof(bms)/sizeof(bms[0]);

void usage(char *n)
{
	printf( "usage:\n"
		"%s [-s seconds] [-b <benchmark id>] <num threads>\n"
		"  -s seconds\n"
		"       run benchmark for seconds\n"
		"  -b <benchmark id>\n"
		"       benchmark ids:\n", n);
	int i;
	for (i = 0; i < nbms; i++)
		printf("       %c      %s\n", bms[i].sname, bms[i].name);
	printf("\n");

	printf(	"%s -m\n"
		"       run mailbench forever\n\n", n);
	exit(-1);
}

int main(int argc, char **argv)
{
	printf("> ");
	int i;
	for (i = 0; i < argc; i++)
		printf("%s ", argv[i]);
	printf("\n");

	char *n = argv[0];
	char onebm = 0;

	int ch;
	while ((ch = getopt(argc, argv, "mb:s:")) != -1) {
		switch (ch) {
		case 's':
			bmsecs = atoi(optarg);
			break;
		case 'b':
			onebm = *optarg;
			break;
		case 'm':
			printf("running mailbench forever...\n");
			return mbforever();
		default:
			usage(n);
		}
	}
	argc -= optind;
	argv += optind;
	if (argc != 1)
		usage(n);

	nthreads = atoi(argv[0]);
	if (nthreads < 0)
		nthreads = 3;
	printf("using %d threads for %d seconds\n", nthreads, bmsecs);

	int did = 0;
	for (i = 0; i < nbms; i++) {
		if (onebm && onebm != bms[i].sname)
			continue;
		did++;
		if (bms[i].begin)
			bms[i].begin();
		bm(bms[i].name, bms[i].fn);
		if (bms[i].end)
			bms[i].end();
	}
	if (!did) {
		printf("no benchmarks executed; no such bm\n");
		exit(-1);
	}

	return 0;
}
