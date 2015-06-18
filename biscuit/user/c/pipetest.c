#include <litc.h>

int main(int argc, char **argv)
{
	int pfds[2];
	int ret = pipe(pfds);
	char *msg1 = "hello, sonny!";
	char *msg2 = "hello, laddy!";
	const int msglen = strlen(msg1) + 1;
	if (ret < 0)
		err(ret, "pipe");
	printf("pipe fds: %d %d\n", pfds[0], pfds[1]);
	int child = 0;
	int status;
	int pid1 = fork();
	int pid2 = fork();
	if (pid1 == 0)
		child = 1;

	if (child) {
		close(pfds[1]);

		char buf[msglen];
		ret = read(pfds[0], buf, sizeof(buf));
		if (ret < 0)
			err(ret, "read");
		printf("got: %s\n", buf);
		if (ret != msglen)
			errx(-1, "only read %d of %d bytes\n", ret, msglen);

		if (pid2 != 0) {
			if ((ret = wait(&status)) < 0)
				err(ret, "wait1");
			if (WEXITSTATUS(status) != 0)
				errx(status, "child child failed %d", status);
		}
		return 0;
	}
	close(pfds[0]);

	char *msg = (pid2 == 0) ? msg1 : msg2;
	printf("sending: %s\n", msg);
	ret = write(pfds[1], msg, msglen);
	printf("sent\n");
	if (ret < 0)
		err(ret, "write");
	if (ret != msglen)
		errx(-1, "only wrote %d of %d bytes\n", ret, msglen);

	if (pid2 == 0)
		return 0;

	if ((ret = wait(&status)) < 0)
		err(ret, "wait2");
	if (WEXITSTATUS(status) != 0)
		err(status, "parent child failed %d", status);
	if ((ret = wait(&status)) < 0)
		err(ret, "wait3");
	if (WEXITSTATUS(status) != 0)
		err(status, "parent child2 failed %d", status);

	// try write on pipe with no readers
	if ((ret = write(pfds[1], "fail", 4) >= 0))
		errx(-1, "write should have failed %d", ret);

	printf("success\n");

	return 0;
}
