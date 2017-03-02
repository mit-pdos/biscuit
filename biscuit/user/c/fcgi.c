#include<stdio.h>
#include<unistd.h>
#include<err.h>
#include<poll.h>
#include<fcntl.h>
#include<sys/socket.h>

int main(int argc, char **argv)
{
	if (fcntl(0, F_SETFL, O_NONBLOCK | fcntl(0, F_GETFL, 0)) == -1)
		err(-1, "fcntl");

	struct pollfd pfd = {fd: 0, events: POLLIN};
	if (poll(&pfd, 1, -1) != 1)
		err(-1, "poll");

	char buf[128];
	ssize_t did = 0, r;
	while ((r = read(0, buf, sizeof(buf))) > 0)
		did += r;
	if (r != -1 || errno != EAGAIN)
		err(-1, "read");
	if (did != 18)
		fprintf(stderr, "unexpected GET len (%zd)\n", did);

	char msg[] =
	    "HTTP/1.1 200 OK\n"
	    "Server: fweb\n"
	    "Date: Tue, 06 Dec 2016 23:32:42 GMT\n"
	    "Content-Type: text/plain\n"
	    "Connection: close\n"
	    "\r\n\r\n"
	    "All work and no play make Jack a dull boy.\n"
	    "All work an no play make Jack a dull boy.\n"
	    "all work and no play make Jack a dull boy.\n"
	    "Allw ork and no play make Jack a dull boy.\n"
	    "All work and no play make jack a dull boy.\n"
	    "All work andd no play make Jack a dull boy.\n"
	    "All work and no play make Jack a  dull boy.\n";
	size_t mlen = sizeof(msg) - 1;
	if (write(1, msg, mlen) != mlen)
		err(-1, "write");
	return 0;
}
