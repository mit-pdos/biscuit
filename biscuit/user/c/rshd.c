#include <litc.h>

static int lstn(uint16_t lport)
{
	fprintf(stderr, "rshd listening on port %d\n", (int)lport);

	int s = socket(AF_INET, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");
	struct sockaddr_in sin;
	sin.sin_family = AF_INET;
	sin.sin_port = htons(lport);
	sin.sin_addr.s_addr = htonl(INADDR_ANY);
	if (bind(s, (struct sockaddr *)&sin, sizeof(sin)) == -1)
		err(-1, "bind");
	if (listen(s, 10) == -1)
		err(-1, "listen");
	return s;
}

int main(int argc, char **argv)
{
	int lfd = lstn(22);

	for (;;) {
		struct sockaddr_in sin;
		socklen_t slen = sizeof(sin);
		int s;
		if ((s = accept(lfd, (struct sockaddr *)&sin, &slen)) == -1)
			err(-1, "accept");
		pid_t p;
		if ((p = fork()) == -1)
			err(-1, "fork");
		if (p == 0) {
			if (dup2(s, 0) == -1)
				err(-1, "dup2");
			if (dup2(s, 1) == -1)
				err(-1, "dup2");
			if (dup2(s, 2) == -1)
				err(-1, "dup2");
			close(s);
			char *args[] = {"/bin/lsh", NULL};
			execv(args[0], args);
			err(-1, "execv");
		}
		if (close(s) == -1)
			err(-1, "close");
		uint a = (sin.sin_addr.s_addr >>  0) & 0xff;
		uint b = (sin.sin_addr.s_addr >>  8) & 0xff;
		uint c = (sin.sin_addr.s_addr >> 16) & 0xff;
		uint d = (sin.sin_addr.s_addr >> 24) & 0xff;
		printf("%u.%u.%u.%u:%d connected (pid %ld).\n", a, b, c, d,
		    (int)ntohs(sin.sin_port), (long)p);
		pid_t chald;
		int status;
		while ((chald = wait4(WAIT_ANY, &status, WNOHANG, NULL)) > 0)
			if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
				printf("pid %ld terminated abnormally\n",
				    chald);
			else
				printf("pid %ld finished\n", chald);
		if (chald == -1 && errno != ECHILD)
			err(-1, "wait4");
	}
	return 0;
}
