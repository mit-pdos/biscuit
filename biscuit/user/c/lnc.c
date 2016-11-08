#include <litc.h>

static char lmss[1ul << 11];

static int mss(uint32_t dip, uint16_t dport)
{
	printf("MSS sends\n");

	int fd = open("/redis.conf", O_RDONLY);
	if (fd == -1)
		err(-1, "open");

	int s = socket(AF_INET, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");
	uint8_t a = dip >> 24;
	uint8_t b = dip >> 16;
	uint8_t c = dip >> 8;
	uint8_t d = dip;
	printf("connecting to %d.%d.%d.%d:%d\n", a, b, c, d, dport);
	struct sockaddr_in sin;
	sin.sin_port = htons(dport);
	sin.sin_addr.s_addr = htonl(dip);
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

int nc(uint32_t dip, uint16_t dport)
{
	int s = socket(AF_INET, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");
	uint8_t a = dip >> 24;
	uint8_t b = dip >> 16;
	uint8_t c = dip >> 8;
	uint8_t d = dip;
	printf("connecting to %d.%d.%d.%d:%d\n", a, b, c, d, dport);
	struct sockaddr_in sin;
	sin.sin_port = htons(dport);
	sin.sin_addr.s_addr = htonl(dip);
	if (connect(s, (struct sockaddr *)&sin, sizeof(sin)) == -1)
		err(-1, "connect");
	printf("connected\n");

	struct pollfd pfds[2] = {{.fd = 0}, {.fd = s}};
	const int nfds = 2;
	char buf[512];
	int closed = 0;
	//while (closed != 2) {
	while (closed == 0) {
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
				closed++;
				pfds[i].fd = -1;
				continue;
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

static void usage()
{
	fprintf(stderr, "usage:\n"
	    "%s [-M] [-c host] [-p port] \n", __progname);
	exit(-1);
}

int main(int argc, char **argv)
{
	// bhw
	uint32_t dip = 0x121a0530;
	uint16_t dport = 31338;
	int Mss = 0;
	int c;
	while ((c = getopt(argc, argv, "Mp:c:")) != -1) {
		switch (c) {
		case 'M':
			Mss = 1;
			break;
		case 'p':
			dport = strtol(optarg, NULL, 0);
			break;
		case 'c': {
			int a, b, c, d;
			if (sscanf(optarg, "%d.%d.%d.%d", &a, &b, &c, &d) != 4)
				errx(-1, "malformed IP (%s)", optarg);
			dip = a << 24 | b << 16 | c << 8 | d;
			break;
		}
		default:
			usage();
		}
	}

	if (Mss)
		return mss(dip, dport);
	else
		return nc(dip, dport);
}
