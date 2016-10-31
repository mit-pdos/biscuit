#include <litc.h>

int main(int argc, char **argv)
{
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

	ssize_t ret;
	char buf[512];
	while ((ret = read(s, buf, sizeof(buf) - 1)) > 0) {
		buf[ret] = '\0';
		printf("GOT: %s\n", buf);
	}
	if (ret < 0)
		err(-1, "read");

	return 0;
}
