#include <litc.h>

void sendall(int fd, char *buf, size_t len, struct sockaddr_un *sa,
    socklen_t slen)
{
	size_t c = 0;
	int ret;
	while (c < len) {
		ret = sendto(fd, buf + c, len - c, 0, (struct sockaddr *)sa,
		    slen);
		if (ret < 0)
			err(ret, "sendall");
		c += ret;
	}
}

void sendmany()
{
	int s = socket(AF_UNIX, SOCK_DGRAM, 0);
	if (s < 0)
		err(s, "socket");

	int ret;
	struct sockaddr_un me;
	memset(&me, 0, sizeof(struct sockaddr_un));
	me.sun_family = AF_UNIX;
	snprintf(me.sun_path, sizeof(me.sun_path), "/tmp/child");
	if ((ret = bind(s, (struct sockaddr *)&me,
	     sizeof(struct sockaddr_un))) < 0)
		err(ret, "bind: %d", ret);

	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	snprintf(sa.sun_path, sizeof(sa.sun_path), "/tmp/parent");

	int i;
	for (i = 0; i < 100; i++) {
		char buf[64];
		snprintf(buf, sizeof(buf), "message %d", i);
		size_t mlen = strlen(buf);
		//sendall(s, buf, strlen(buf), &sa, sizeof(struct sockaddr_un));
		int ret = sendto(s, buf, mlen, 0, (void *)&sa,
		    sizeof(struct sockaddr_un));
		if (ret < 0)
			err(ret, "sendto");
		if (ret != mlen)
			printf("short write! (lost %ld bytes)\n", mlen - ret);
	}
	printf("**sender done\n");
	exit(0);
}

void test()
{
	unlink("/tmp/parent");
	unlink("/tmp/child");

	int s = socket(AF_UNIX, SOCK_DGRAM, 0);
	if (s < 0)
		err(s, "socket");

	int ret;
	struct sockaddr_un sa;
	memset(&sa, 0, sizeof(struct sockaddr_un));
	sa.sun_family = AF_UNIX;
	snprintf(sa.sun_path, sizeof(sa.sun_path), "/tmp/parent");
	if ((ret = bind(s, (struct sockaddr *)&sa, sizeof(sa))) < 0)
		err(ret, "bind: %d", ret);

	int pid;
	if ((pid = fork()) < 0)
		err(pid, "fork");
	if (pid == 0) {
		close(s);
		sendmany();
	}

	char buf[64];
	int i;
	for (i = 0; i < 100; i++) {
		// stall to make sure the socket buffer fills up and blocks the
		// sender
		int j;
		for (j = 0; j < 1000000; j++)
			rdtsc();
		struct sockaddr_un cli;
		socklen_t clen = sizeof(struct sockaddr_un);
		ret = recvfrom(s, buf, sizeof(buf) - 1, 0,
		    (struct sockaddr *)&cli, &clen);
		if (ret < 0)
			err(ret, "recvfrom");
		buf[ret] = 0;
		printf("from: %s got: %s\n", cli.sun_path, buf);
	}
	wait(NULL);
}

int main(int argc, char **argv)
{
	test();
	return 0;
}
