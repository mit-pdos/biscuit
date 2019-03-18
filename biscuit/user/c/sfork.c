// gcc -static -O3 -o sfork sfork.c -Wl,--whole-archive,-lpthread,--no-whole-archive
// openbsd: gcc -nopie -fno-pic -static -O3 -o sfork sfork.c -Wl,--whole-archive,-lpthread,--no-whole-archive

#include <err.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <poll.h>

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

#define SYSCALL_CLOBBERS "cc", "memory", "r9", "r10", "r11", "r12", "r13", \
			 "r14", "r15"
void *igetpids(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		asm volatile(
			"movl	$40, %%eax\n"
			"movq	%%rsp, %%r10\n"
			"leaq	2(%%rip), %%r11\n"
			"sysenter\n"
			:
			:
			: SYSCALL_CLOBBERS, "eax", "edi", "esi", "edx", "ecx", "r8");
		total++;
	}
	return (void *)total;
}

void *getppids(void *idp)
{
	pthread_barrier_wait(&bar);

	long total = 0;
	while (!cease) {
		getppid();
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

	//if (sys_prof(PROF_GOLANG, 0, 0, 0) == -1)
	//	err(-1, "prof");

	ulong st = now();
	sleep(bmsecs);

	cease = 1;
	ulong beforejoin = now();
	long total = jointot(t, nthreads);
	ulong actual = now() - st;

	//if (sys_prof(PROF_DISABLE|PROF_GOLANG, 0, 0, 0) == -1)
	//	err(-1, "prof");

	printf("%s ran for %lu ms (slept %lu)\n", tl, actual, beforejoin - st);
	double secs = (double)actual / 1000;
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
			char * const args[] = {"./true", NULL};
			execv(args[0], args);
			err(-1, "execv");
		}
		int status;
		wait(&status);
		int code = WEXITSTATUS(status);
		if (!WIFEXITED(status) || code != 0)
			errx(-1, "child failed: %d", code);
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
	const char *f = "dur";
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

const char * const _websock = ".websock";

static void *webclient(void *p)
{
	pthread_barrier_wait(&bar);

	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, _websock, sizeof(sa.sun_path));

	long total = 0;
	while (!cease) {
		int s = socket(AF_UNIX, SOCK_STREAM, 0);
		if (s == -1)
			err(-1, "socket");

		if (connect(s, (struct sockaddr *)&sa, sizeof(sa)) == -1)
			err(-1, "connect");

		char gmsg[] = "GET /";
		if (write(s, gmsg, sizeof(gmsg)) != sizeof(gmsg))
			err(-1, "write");
		char buf[1024];
		ssize_t r;
		while ((r = read(s, buf, sizeof(buf))) > 0)
			;
		if (r == -1)
			err(-1, "read");
		if (close(s) == -1)
			err(-1, "close");
		total++;
	}
	return (void *)total;
}

static void _websv(void)
{
	int sfd = open("/clouseau.txt", O_RDONLY);
	if (sfd == -1)
		err(-1, "open");

	int s = socket(AF_UNIX, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");

	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, _websock, sizeof(sa.sun_path));

	if (bind(s, (struct sockaddr *)&sa, SUN_LEN(&sa)) == -1)
		err(-1, "bind");
	if (listen(s, 10) == -1)
		err(-1, "listen");

	while (1) {
		int ns;
		if ((ns = accept(s, NULL, NULL)) == -1)
			err(-1, "accept");

		char req[64];
		ssize_t r;
		if ((r = read(ns, req, sizeof(req))) == -1)
			err(-1, "read");
		if (strncmp(req, "exit", sizeof(req)) == 0) {
			if (unlink(_websock) == -1)
				err(-1, "unlink");
			exit(0);
		}

		if (lseek(sfd, 0, SEEK_SET) == -1)
			err(-1, "lseek");
		char buf[1024];
		while ((r = read(sfd, buf, sizeof(buf))) > 0) {
			if (write(ns, buf, r) != r)
				err(-1, "write");
		}
		if (r == -1)
			err(-1, "read");
		if (close(ns) == -1)
			err(-1, "close");
	}
}

static void webstart(void)
{
	pid_t r;
	if ((r = fork()) == -1)
		err(-1, "fork");
	if (!r)
		_websv();
	// give server some time to initialize
	sleep(1);
}

static void webstop(void)
{
	int s = socket(AF_UNIX, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");

	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, _websock, sizeof(sa.sun_path));

	if (connect(s, (struct sockaddr *)&sa, sizeof(sa)) == -1)
		err(-1, "connect");

	char em[] = "exit";
	if (write(s, em, sizeof(em)) != sizeof(em))
		err(-1, "write");
	if (close(s) == -1)
		err(-1, "close");
	int status;
	wait(&status);
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "websv exited with status %d", status);
}

static int pipec[2];
static int pipep[2];

static void pingstart(void)
{
	if (pipe(pipec) == -1)
		err(-1, "pipe");
	if (pipe(pipep) == -1)
		err(-1, "pipe");
	pid_t c;
	if ((c = fork()) == -1)
		err(-1, "fork");
	if (c) {
		close(pipec[0]);
		close(pipep[1]);
		return;
	}
	close(pipep[0]);
	close(pipec[1]);
	char mymsg[] = "pong";
	char *emsg = "exit";
	while (1) {
		char buf[32];
		if (read(pipec[0], buf, sizeof(buf)) != 5)
			err(-1, "chald read");
		if (buf[0] == 'e' && strcmp(buf, emsg) == 0)
			break;
		if (write(pipep[1], mymsg, sizeof(mymsg)) != sizeof(mymsg))
			err(-1, "chald write");
	}
	exit(0);
}

static void pingstop(void)
{
	char emsg[] = "exit";
	if (write(pipec[1], emsg, sizeof(emsg)) != sizeof(emsg))
		err(-1, "write exit");
	char buf[64];
	while (read(pipep[0], buf, sizeof(buf)) > 0)
		;
	int status;
	if (wait(&status) == -1)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "chald failed");
	close(pipec[1]);
	close(pipep[0]);
}

static void *pingpong(void *_a)
{
	pthread_barrier_wait(&bar);

	char mymsg[] = "pong";
	long tot = 0;
	while (!cease) {
		char buf[32];
		if (write(pipec[1], mymsg, sizeof(mymsg)) != sizeof(mymsg))
			err(-1, "par write");
		if (read(pipep[0], buf, sizeof(buf)) != 5)
			err(-1, "par write");
		tot++;
	}
	return (void *)tot;
}

static void *_poll(const int nfds)
{
	if (nfds <= 0)
		errx(-1, "bad nfds");

	int fds[nfds];
	int i;
	for (i = 0; i < nfds; i++) {
		char buf[64];
		snprintf(buf, sizeof(buf), "/tmp/.poll%d", i);
		unlink(buf);
		if ((fds[i] = open(buf, O_CREAT | O_EXCL | O_RDWR,
		    0600)) == -1)
			err(-1, "creat");
	}

	struct pollfd pfds[nfds];
	for (i = 0; i < nfds; i++) {
		pfds[i].fd = fds[i];
		if (i % 2)
			pfds[i].events = POLLIN;
		else
			pfds[i].events = POLLOUT;
	}

	pthread_barrier_wait(&bar);
	long tot = 0;
	while (!cease) {
		if (poll(pfds, nfds, 0) == -1)
			err(-1, "poll");
		tot++;
	}
	for (i = 0; i < nfds; i++)
		close(fds[i]);
	return (void *)tot;
}

static void *poll50(void *_a)
{
	return _poll(50);
}

static void *poll1(void *_a)
{
	return _poll(1);
}

static long alloc_amount;

static void *alloc(void *_a)
{
	if (alloc_amount <= 0)
		errx(-1, "bad alloc amount %ld", alloc_amount);
	pthread_barrier_wait(&bar);
	long tot = 0;
	while (!cease) {
		const long hack3 = 1 << 6;
		if (sys_prof(hack3, alloc_amount, 0, 0) == -1)
			err(-1, "sysprof");
		tot++;
	}
	return (void *)tot;
}

#define _ST1	"dir1/"
#define _ST2	"dir2/"
#define _ST3	"file"
#define STFN	(_ST1 _ST2 _ST3)

static void *statty(void *_a)
{
	long me = (long)(uintptr_t)_a;

	char stdir[64];
	snprintf(stdir, sizeof stdir, "%ld", me);
	if (mkdir(stdir, 0755) == -1)
		err(-1, "mkdir");

	char stfn[64];
	snprintf(stfn, sizeof stfn, "%s/%ld", stdir, me);
	int fd;
	if ((fd = open(stfn, O_CREAT | O_WRONLY | O_EXCL, 0600)) == -1)
		err(-1, "open");
	close(fd);

	long tot = 0;
	pthread_barrier_wait(&bar);

	struct stat st;
	while (!cease) {
		if (stat(stfn, &st) == -1)
			err(-1, "stat");
		tot++;
	}

	if (unlink(stfn) == -1)
		err(-1, "unlink");
	if (rmdir(stdir) == -1)
		err(-1, "rmdir");
	return (void *)tot;
}

void *locks(void *_arg)
{
	long tot = 0;
	pthread_barrier_wait(&bar);

	while (!cease) {
		asm("lock incq %0\n"
			:
			: "m"(tot)
			: "cc", "memory");
	}
	return (void *)tot;
}

struct {
	char *name;
	char sname;
	void *(*fn)(void *);
	void (*begin)(void);
	void (*end)(void);
} bms[] = {
	{"renames", 'r', crrename, NULL, NULL},
	{"getppids", 'c', getppids, NULL, NULL},
	{"unix socket", 'u', sunsend, sunspawn, sunkill},
	{"inline getpids", 'p', igetpids, NULL, NULL},
	{"forkonly", 'k', forkonly, NULL, NULL},
	{"forkexec", 'e', forkexec, NULL, NULL},
	{"seqcreate", 's', seqcreate, NULL, NULL},
	{"openonly", 'o', openonly, NULL, NULL},
	{"webserver", 'w', webclient, webstart, webstop},
	{"pipe ping pong", 'P', pingpong, pingstart, pingstop},
	{"mmap/munmap", 'm', mapper, NULL, NULL},
	{"poll50", '5', poll50, NULL, NULL},
	{"poll1", '1', poll1, NULL, NULL},
	{"alloc", 'a', alloc, NULL, NULL},
	{"stat", 'S', statty, NULL, NULL},
	{"locks", 'l', locks, NULL, NULL},
};

const int nbms = sizeof(bms)/sizeof(bms[0]);

void usage(char *n)
{
	printf( "usage:\n"
		"%s [-s seconds] [-A int] [-b <benchmark id>] <num threads>\n"
		"  -s seconds\n"
		"       run benchmark for seconds\n"
		"  -A <int>\n"
		"       amount to allocate in bytes for alloc benchmark\n"
		"  -b <benchmark id>\n"
		"       benchmark ids:\n"
		, n);
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
	while ((ch = getopt(argc, argv, "A:mb:s:")) != -1) {
		switch (ch) {
		case 'A':
			alloc_amount = strtol(optarg, NULL, 0);
			break;
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
