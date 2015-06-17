#include <litc.h>

void child()
{
	struct sockaddr_un me;
	snprintf(me.sun_path, sizeof(me.sun_path), "/tmp/child");
	int s = socket(AF_UNIX, SOCK_DGRAM, 0);
	int ret;
	if ((ret = bind(s, (struct sockaddr *)&me, sizeof(me))) < 0)
		err(ret, "bind: %d", ret);

	struct sockaddr_un sa;
	snprintf(sa.sun_path, sizeof(sa.sun_path), "/tmp/parent");

	char msg[] = "levee be breakin'";
	int s2 = socket(AF_UNIX, SOCK_DGRAM, 0);
	ret = sendto(s2, msg, sizeof(msg), 0, (struct sockaddr *)&sa,
	    sizeof(sa));
	if (ret < 0)
		err(ret, "sendto");
	if (ret != sizeof(msg))
		errx(-1, "send mismath: %d %ld\n", ret, sizeof(msg));

	char buf[64];
	if ((ret = recvfrom(s, buf, sizeof(buf) - 1, 0, NULL, NULL)) < 0)
		err(ret, "recvfrom");
	buf[ret] = 0;
	printf("child got: %s\n", buf);
	exit(0);
}

int main(int argc, char **argv)
{
	int pid;
	if ((pid = fork()) < 0)
		err(pid, "fork");
	if (pid == 0)
		child();

	int s = socket(AF_UNIX, SOCK_DGRAM, 0);
	if (s < 0)
		err(s, "socket");
	struct sockaddr_un sa;
	sa.sun_len = sizeof(sa);
	sa.sun_family = AF_UNIX;
	snprintf(sa.sun_path, sizeof(sa.sun_path), "/tmp/parent");
	int ret;
	if ((ret = bind(s, (struct sockaddr *)&sa, sizeof(sa))) < 0)
		err(ret, "bind: %d", ret);

	char buf[64];
	if ((ret = recvfrom(s, buf, sizeof(buf) - 1, 0, NULL, NULL)) < 0)
		err(ret, "recvfrom");
	buf[ret] = 0;
	printf("parent got: %s\n", buf);

	struct sockaddr_un them;
	snprintf(them.sun_path, sizeof(them.sun_path), "/tmp/child");

	char msg[] = "indeed! broken as fuke";
	int s2 = socket(AF_UNIX, SOCK_DGRAM, 0);
	ret = sendto(s2, msg, sizeof(msg), 0, (struct sockaddr *)&them,
	    sizeof(them));
	if (ret < 0)
		err(ret, "sendto");
	if (ret != sizeof(msg))
		errx(-1, "send mismath: %d %ld\n", ret, sizeof(msg));

	close(s);
	return 0;
}
