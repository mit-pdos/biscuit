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

void datagram()
{
	printf("datagram test\n");

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
	struct timespec to = {0, 1000000};
	for (i = 0; i < 100; i++) {
		// stall to make sure the socket buffer fills up and blocks the
		// sender
		nanosleep(&to, NULL);
		struct sockaddr_un cli;
		socklen_t clen = sizeof(struct sockaddr_un);
		ret = recvfrom(s, buf, sizeof(buf) - 1, 0,
		    (struct sockaddr *)&cli, &clen);
		if (ret < 0)
			err(ret, "recvfrom");
		buf[ret] = 0;
		//printf("from: %s got: %s\n", cli.sun_path, buf);
		char cmp[64];
		snprintf(cmp, sizeof(cmp), "message %d", i);
		if (strncmp(buf, cmp, strlen(cmp)))
			errx(-1, "msg mismatch");
	}

	int status;
	if (wait(&status) != pid)
		errx(-1, "wrong child");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");

	printf("datagram test OK\n");
}

__attribute__((noreturn))
void connector(char *path, char *msg1, char *msg2)
{
	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, path, sizeof(sa.sun_path));

	int s = socket(AF_UNIX, SOCK_STREAM, 0);
	if (s < 0)
		err(-1, "socket");

	if (connect(s, (struct sockaddr *)&sa, sizeof(sa)) < 0)
		err(-1, "connect");
	ssize_t r;
	if ((r = write(s, msg1, strlen(msg1))) != strlen(msg1))
		err(-1, "write failed (%ld != %lu)", r, strlen(msg1));
	close(s);

	s = socket(AF_UNIX, SOCK_STREAM, 0);
	if (s < 0)
		err(-1, "socket");

	if (connect(s, (struct sockaddr *)&sa, sizeof(sa)) < 0)
		err(-1, "connect");
	if ((r = write(s, msg2, strlen(msg2))) != strlen(msg2))
		err(-1, "write failed (%ld != %lu)", r, strlen(msg2));

	char buf[256];
	// parent writes two messages in a row; make sure we read them one at a
	// time
	if ((r = read(s, buf, strlen(msg1))) != strlen(msg1))
		err(-1, "c1 read failed (%ld != %lu)", r, strlen(msg1));
	if (strncmp(buf, msg1, strlen(msg1)))
		errx(-1, "string mismatch");

	char *mymsg = "child";
	char *theirmsg = "parent";
	if ((r = write(s, mymsg, strlen(mymsg))) != strlen(mymsg))
		err(-1, "write failed (%ld != %lu)", r, strlen(mymsg));
	if ((r = read(s, buf, sizeof(buf))) != strlen(theirmsg))
		err(-1, "c2 read failed (%ld != %lu)", r, strlen(theirmsg));
	if (strncmp(buf, theirmsg, strlen(theirmsg)))
		errx(-1, "unexpected data");
	close(s);

	exit(0);
}

void stream()
{
	printf("stream test\n");

	char *path = "/tmp/stream";
	char *msg1 = "hello1";
	char *msg2 = "hello2 happy day";

	unlink(path);

	int s = socket(AF_UNIX, SOCK_STREAM, 0);
	if (s < 0)
		err(-1, "socket");

	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	strncpy(sa.sun_path, path, sizeof(sa.sun_path));

	if (bind(s, (struct sockaddr *)&sa, SUN_LEN(&sa)) < 0)
		err(-1, "bind");

	if (listen(s, 10) < 0)
		err(-1, "listen");

	pid_t child = fork();
	if (child < 0)
		err(-1, "fork");
	if (!child) {
		close(s);
		connector(path, msg1, msg2);
	}

	int c = accept(s, NULL,  NULL);
	if (c < 0)
		err(-1, "accept");

	int sockerr;
	socklen_t solen = sizeof(sockerr);
	if (getsockopt(c, SOL_SOCKET, SO_ERROR, &sockerr, &solen))
		err(-1, "getsockopt");
	if (sockerr != 0)
		errx(-1, "socket error");

	char buf[256];
	ssize_t r;
	if ((r = read(c, buf, sizeof(buf) - 1)) < 0)
		err(-1, "read");
	buf[r] = 0;
	if (strncmp(buf, msg1, strlen(msg1)))
		errx(-1, "msg mismatch");
	if (close(c))
		err(-1, "closey");

	c = accept(s, NULL,  NULL);
	if (c < 0)
		err(-1, "accept");

	// make sure non-blocking accept fails at this point
	int fl;
	if ((fl = fcntl(s, F_GETFL)) == -1)
		err(-1, "fcntlg");
	if (fcntl(s, F_SETFL, fl | O_NONBLOCK) == -1)
		err(-1, "fcntls");
	if (accept(s, NULL, NULL) != -1 || errno != EWOULDBLOCK)
		errx(-1, "expected EWOULDBLOCK");
	if (fcntl(s, F_SETFL, fl) == -1)
		err(-1, "fcntls");

	if ((r = read(c, buf, sizeof(buf) - 1)) < 0)
		err(-1, "read");
	buf[r] = 0;
	if (strncmp(buf, msg2, strlen(msg2)))
		errx(-1, "msg mismatch");

	if ((r = write(c, msg1, strlen(msg1))) != strlen(msg1))
		err(-1, "write failed (%ld != %lu)", r, strlen(msg1));

	char *mymsg = "parent";
	char *theirmsg = "child";
	if ((r = write(c, mymsg, strlen(mymsg))) != strlen(mymsg))
		err(-1, "write failed (%ld != %lu)", r, strlen(mymsg));
	if ((r = read(c, buf, sizeof(buf))) != strlen(theirmsg))
		err(-1, "s1 read failed (%ld != %lu)", r, strlen(theirmsg));
	if (strncmp(buf, theirmsg, strlen(theirmsg)))
		errx(-1, "unexpected data");

	// child closed connection; writes should fail, should get EOF
	if ((r = read(c, buf, sizeof(buf))) != 0)
		errx(-1, "expected failure1 %ld %d", r, errno);
	if (write(c, buf, 10) != -1 || errno != ECONNRESET)
		errx(-1, "expected failure2");

	if (close(c))
		err(-1, "closey");
	if (close(s))
		err(-1, "closey");

	int status;
	if (wait(&status) != child)
		errx(-1, "wrong child");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");

	printf("stream test OK\n");
}

int main(int argc, char **argv)
{
	datagram();
	stream();
	return 0;
}
