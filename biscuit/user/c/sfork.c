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
	snprintf(o, sizeof(o), "o%ld", id);
	snprintf(n, sizeof(n), "n%ld", id);

	int fd = open(o, O_CREAT | O_RDONLY, 0600);
	if (fd < 0)
		err(fd, "open");
	close(fd);

	long total = 0;
	while (!cease) {
		int ret;
		if ((ret = rename(o, n)) < 0)
			err(ret, "rename");
		if ((ret = rename(n, o)) < 0)
			err(ret, "rename");
		total += 2;
	}
	return (void *)total;
}

void *lookups(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		int ret;
		if ((ret = open("NONE", O_RDONLY)) != -ENOENT)
			err(ret, "open: unexpected error");
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
		int ret;
		if ((ret = munmap(p, l)) < 0)
			err(ret, "munmap");

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
		if ((ret = unlink(o)) < 0)
			err(ret, "unlink");

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
		err(fd, "socket");

	int ret;
	if ((ret = bind(fd, (struct sockaddr *)&sa, sizeof(sa))) < 0)
		err(ret, "bind");

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
		err(ret, "recv");
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
		err(fd, "socket");

	char msg[] = "exit";
	int ret;
	ret = sendto(fd, msg, sizeof(msg), 0, (struct sockaddr *)&sa,
	    sizeof(sa));
	if (ret < 0)
		err(ret, "sendto");
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
		err(fd, "socket");

	pthread_barrier_wait(&bar);

	long total = 0;
	const char msg[] = "123456789";
	while (!cease) {
		int ret;
		ret = sendto(fd, msg, sizeof(msg), 0, (struct sockaddr *)&sa,
		    sizeof(sa));
		if (ret < 0)
			err(ret, "sendto");
		else if (ret != sizeof(msg))
			errx(-1, "short write");

		total++;
	}
	return (void *)total;
}

void usage(char *n)
{
	printf( "usage:\n"
		"%s [-s seconds] [-b [c|r|u|f|p]] <num threads>\n"
		"  -s seconds\n"
		"       run benchmark for seconds\n"
		"  -b [c|r|u]\n"
		"       only run create message, rename, unix\n"
		"       socket, sys_fake2, or getpid benchmark\n", n);
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
	while ((ch = getopt(argc, argv, "b:s:")) != -1) {
		switch (ch) {
		case 's':
			bmsecs = atoi(optarg);
			break;
		case 'b':
			onebm = *optarg;
			break;
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
	};

	const int nbms = sizeof(bms)/sizeof(bms[0]);

	for (i = 0; i < nbms; i++) {
		if (onebm && onebm != bms[i].sname)
			continue;
		if (bms[i].begin)
			bms[i].begin();
		bm(bms[i].name, bms[i].fn);
		if (bms[i].end)
			bms[i].end();
	}

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
