#include <litc.h>

int nc(int argc, char **argv)
{
	printf("NC\n");
	uint16_t dport = 31338;
	if (argc > 1)
		dport = (uint16_t)strtoul(argv[1], NULL, 0);

	int s = socket(AF_INET, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");
	printf("connecting to 18.26.5.48:%d\n", dport);
	struct sockaddr_in sin;
	sin.sin_port = htons(dport);
	sin.sin_addr.s_addr = htonl(0x121a0530);
	if (connect(s, (struct sockaddr *)&sin, sizeof(sin)) == -1)
		err(-1, "connect");
	printf("connected\n");

	struct pollfd pfds[2] = {{.fd = 0}, {.fd = s}};
	const int nfds = 2;
	char buf[512];
	int done = 0;
	while (!done) {
		pfds[0].events = pfds[1].events = POLLIN;
		int ret;
		if ((ret = poll(pfds, nfds, -1)) == -1)
			err(-1, "poll");
		if (ret == 0)
			errx(-1, "what");
		int i;
		for (i = 0; i < nfds; i++) {
			if ((pfds[i].revents & POLLIN) == 0)
				continue;
			ssize_t c = read(pfds[i].fd, buf, sizeof(buf));
			if (c == -1) {
				err(-1, "read");
			} else if (c == 0) {
				printf("fd %d closed\n", pfds[i].fd);
				done = 1;
				break;
			}
			ssize_t w = write(i == 0 ? s : 1, buf, c);
			if (w == -1)
				err(-1, "write");
			else if (w != c)
				errx(-1, "short write");
		}
	}
	printf("lnc finished\n");
	return 0;
}

static char lmss[1ul << 11];

static int mss(int argc, char **argv)
{
	printf("MSS\n");
	uint16_t dport = 31338;
	if (argc > 1)
		dport = (uint16_t)strtoul(argv[1], NULL, 0);

	int fd = open("/redis.conf", O_RDONLY);
	if (fd == -1)
		err(-1, "open");

	int s = socket(AF_INET, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");
	printf("connecting to 18.26.5.48:%d\n", dport);
	struct sockaddr_in sin;
	sin.sin_port = htons(dport);
	sin.sin_addr.s_addr = htonl(0x121a0530);
	if (connect(s, (struct sockaddr *)&sin, sizeof(sin)) == -1)
		err(-1, "connect");
	printf("connected\n");

	ssize_t r;
	if ((r = read(fd, lmss, sizeof(lmss))) == -1)
		err(-1, "read");
	else if (r != sizeof(lmss))
		errx(-1, "short read?");

	char *p = &lmss[0];
	size_t left = sizeof(lmss);
	while (left) {
		if ((r = write(s, p, left)) == -1)
			err(-1, "write");
		if (r == -1)
			err(-1, "write");
		else if (r == 0)
			errx(-1, "wat?");
		if (r > left)
			errx(-1, "uh oh");
		left -= r;
		p += r;
	}
	printf("lnc finished\n");
	return 0;
}

int main(int argc, char **argv)
{
	if (argc > 2 && strcmp(argv[2], "nc") == 0)
		return nc(argc, argv);
	return mss(argc, argv);
}
