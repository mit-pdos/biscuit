#include<stdio.h>
#include<unistd.h>
#include<err.h>
#include<sys/socket.h>

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
	struct sockaddr_in sin;
	int lport = 8080;
	sin.sin_family = AF_INET;
	sin.sin_port = htons(lport);
	sin.sin_addr.s_addr = htonl(INADDR_ANY);
	if (bind(s, (struct sockaddr *)&sin, sizeof(sin)) == -1)
		err(-1, "bind");
	if (listen(s, 8) == -1)
		err(-1, "listen");
	fprintf(stderr, "listen on port %d\n", lport);
	for (;;) {
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
	}
}
