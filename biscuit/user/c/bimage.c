#include <litc.h>

static int lstn(uint16_t lport)
{
	fprintf(stderr, "listen on port %d\n", (int)lport);

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
	int ret;
	socklen_t slen = sizeof(sin);
	if ((ret = accept(s, (struct sockaddr *)&sin, &slen)) == -1)
		err(-1, "accept");
	if (close(s))
		err(-1, "close");
	uint a = (sin.sin_addr.s_addr >>  0) & 0xff;
	uint b = (sin.sin_addr.s_addr >>  8) & 0xff;
	uint c = (sin.sin_addr.s_addr >> 16) & 0xff;
	uint d = (sin.sin_addr.s_addr >> 24) & 0xff;
	fprintf(stderr, "connection from %u.%u.%u.%u:%d\n", a, b, c, d,
	    (int)ntohs(sin.sin_port));
	return ret;
}

static ulong nowms(void)
{
	struct timeval t;
	int ret;
	if ((ret = gettimeofday(&t, NULL)) < 0)
		err(-1, "gettimeofday");
	// ms
	return t.tv_sec * 1000 + t.tv_usec / 1000;
}

// latency debugging code
//ulong allw[100];
//
//void dump(ulong *worst)
//{
//	for (int i = 0; i < 100; i++)
//		printf("%d: %lu\n", i, worst[i]);
//	printf("\n");
//}
//
//void insert(ulong *worst, ulong n)
//{
//	if (n < worst[0])
//		return;
//	if (n > worst[99]) {
//		int i = 99;
//		memmove(worst, worst + 1, i*sizeof(worst[0]));
//		worst[i] = n;
//		return;
//	}
//	for (int i = 0; i < 99; i++) {
//		if (n > worst[i] && n <= worst[i+1]) {
//			memmove(worst, worst + 1, i*sizeof(worst[0]));
//			worst[i] = n;
//			return;
//		}
//	}
//}

int main(int argc, char **argv)
{
	// bring all file metadaa and binary text/data into the page cache in
	// hopes that the kernel won't mix blocks from the new image with
	// blocks from the old.
	int status;
	switch (fork()) {
	case 0:
	{
		int fd = open("/dev/null", O_WRONLY);
		if (fd == -1)
			err(-1, "open");
		if (dup2(fd, 1) == -1)
			err(-1, "dup2");
		close(fd);
		char * const args[] = {"/bin/cat", "/bin/bimage", NULL};
		execv(args[0], args);
		err(-1, "execv");
	}
	case -1:
		err(-1, "fork");
	default:
		if (wait(&status) == -1)
			err(-1, "wait");
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			errx(-1, "child failed");
	}

	int fd;
	if ((fd = open("/dev/rsd0c", O_WRONLY)) == -1)
		err(-1, "open");

	int s = lstn(31338);

	sync();

	const int blksz = 4096;
	char buf[blksz];
	size_t did = 0;
	int mb = 1;
	ulong stms = nowms();
	for (;;) {
		ssize_t bs = 0;
		ssize_t r = 0;
		while (bs != blksz &&
		    (r = read(s, buf + bs, sizeof(buf) - bs)) > 0)
			bs += r;
		if (r == -1)
			err(-1, "read");
		if (bs == 0)
			break;
		if ((did % blksz) != 0)
			fprintf(stderr, "slow write\n");
		if (write(fd, buf, bs) != bs)
			err(-1, "write");
		did += bs;
		if (did >> 20 >= mb) {
			fprintf(stderr, "%dMB\n", mb);
			mb += 1;
		}
	}
	if (fsync(fd) == -1)
		err(-1, "fsync");
	if (close(s) == -1)
		err(-1, "close");
	if (close(fd) == -1)
		err(-1, "close");
	ulong elap = nowms() - stms;
	double secs = (double)elap / 1000;
	fprintf(stderr, "wrote %zu bytes (%.2fMB/s). rebooting.\n", did,
	    (double)(did>>20)/secs);
	reboot();
	return 0;
}
