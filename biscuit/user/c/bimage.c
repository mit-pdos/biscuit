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

int main(int argc, char **argv)
{
	// try to make sure the instructions to call reboot(2) are in the page
	// cache before clobbering the entire disk
	asm volatile(
		"orq	$0, (%0)\n"
		:
		: "r"(reboot)
		: "memory", "cc");

	int fd;
	if ((fd = open("/dev/rsd0c", O_WRONLY)) == -1)
		err(-1, "open");

	int s = lstn(31338);

	const int blksz = 512;
	char buf[blksz];
	size_t did = 0;
	int mb = 1;
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
	if (close(s) == -1)
		err(-1, "close");
	if (close(fd) == -1)
		err(-1, "close");
	fprintf(stderr, "wrote %zu bytes. rebooting.\n", did);
	reboot();
	return 0;
}
