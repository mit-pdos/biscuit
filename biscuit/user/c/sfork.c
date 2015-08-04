#include <stdio.h>
#include <stdlib.h>
#include <fcntl.h>
#include <errno.h>
#include <string.h>

#include <sys/mman.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <sys/wait.h>

#include <pthread.h>

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

static const int bmsecs = 2;

pthread_barrier_t bar;
static int volatile cease;

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
	while (!cease) {
		char msg[] = "123456789";
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

int main(int argc, char **argv)
{
	if (argc != 2)
		errx(-1, "usage: %s <num threads>", argv[0]);

	nthreads = atoi(argv[1]);
	if (nthreads < 0)
		nthreads = 3;
	printf("using %d threads for %d seconds\n", nthreads, bmsecs);

	bm("renames", crrename);
	bm("create/write", crmessage);
	sunspawn();

	bm("unix socket send", sunsend);

	sunkill();

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
