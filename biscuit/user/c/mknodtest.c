#include <litc.h>

int main(int argc, char **argv)
{
	int ret;
	if ((ret = mknod("/tmp/flea", 10, MKDEV(10, 0))) < 0)
		err(ret, "mknod");

	int fd = open("/tmp/flea", O_RDWR, 0);
	if (fd < 0)
		err(fd, "open");
	char buf[512];
	if ((ret = read(fd, buf, sizeof(buf))) < 0)
		err(ret, "read");

	close(fd);

	return 0;
}
