#include<stdio.h>
#include<unistd.h>
#include<err.h>
#include<sys/socket.h>
#include<sys/wait.h>
#include<netinet/in.h>

static ulong nowus()
{
	struct timeval t;
	if (gettimeofday(&t, NULL))
		errx(-1, "gettimeofday");
	return t.tv_sec * 1000000 + t.tv_usec;
}

static void profdump(void)
{
	int fd = open("/pc.sh", O_RDONLY);
	if (fd == -1)
		err(-1, "open");
	if (dup2(fd, 0) == -1)
		err(-1, "dup");
	close(fd);
	char * const args[] = {"/bin/lsh", NULL};
	execv(args[0], args);
	err(-1, "exec");
}

static void forkwait(void (*f)(void))
{
	switch (fork()) {
	case -1:
		err(-1, "fork");
	case 0:
		f();
		errx(-1, "child returned");
	}

	int status;
	if (wait(&status) == -1)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		printf("child failed\n");
}

static void req(int s)
{
	if (dup2(s, 0) == -1)
		err(-1, "dup2");
	if (dup2(s, 1) == -1)
		err(-1, "dup2");
	if (close(s) == -1)
		err(-1, "close");

	char *args[] = {"./bin/fcgi", NULL};
	execv(args[0], args);
	err(-1, "execv");
}

int main(int argc, char **argv)
{
	int s = socket(AF_INET, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");
	int prof = 1;
	if (argc != 1) {
		printf("profiling\n");
		prof = 0;
	}
	struct sockaddr_in sin;
	int lport = 8080;
	sin.sin_family = AF_INET;
	sin.sin_port = htons(lport);
	sin.sin_addr.s_addr = htonl(INADDR_ANY);
	if (bind(s, (struct sockaddr *)&sin, sizeof(sin)) == -1)
		err(-1, "bind");
	if (listen(s, 8) == -1)
		err(-1, "listen");
	fprintf(stderr, "listen on %d\n", lport);
	srand(time(NULL));
	ulong st = nowus();
	for (;;) {
		if (prof) {
			if (sys_prof(PROF_SAMPLE, PROF_EV_UNHALTED_CORE_CYCLES,
			    //PROF_EVF_OS | PROF_EVF_USR | PROF_EVF_BACKTRACE,
			    PROF_EVF_OS | PROF_EVF_USR,
			    2800/2) == -1)
				err(-1, "sysprof");
		}
		socklen_t slen = sizeof(sin);
		int fd = accept(s, (struct sockaddr *)&sin, &slen);
		if (fd == -1)
			err(-1, "accept");
		pid_t c = fork();
		if (c == -1)
			err(-1, "fork");
		if (!c)
			req(fd);
		if (close(fd) == -1)
			err(-1, "close");
		int status;
		if (wait(&status) != c)
			err(-1, "wait");
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			errx(-1, "child failed");
		if (prof) {
			if (sys_prof(PROF_SAMPLE | PROF_DISABLE, 0, 0, 0) == -1)
				err(-1, "sysprof");
		}
		long end = nowus();
		long elap = end - st;
		st = end;
		if (elap >= 1000 && (rand() % 10000) < 1000) {
			printf("GOT PROFILE for %ldus\n", elap);
			printf("WAIT FOR DUMP...\n");
			forkwait(profdump);
			st = nowus();
		}
	}
}
