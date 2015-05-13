#include <litc.h>

static char buf[1024];

int main(int argc, char **argv)
{
	int ret;
	if ((ret = mkdir("/wtf", 0755)) < 0)
	  err(ret, "mkdir failed");

	if ((ret = mkdir("/wtf/happyday", 0755)) < 0)
	  err(ret, "mkdir failed");

	if ((ret = mkdir("/wtf/happyday/wut", 0755)) < 0)
	  err(ret, "mkdir failed");

	if ((ret = open("/wtf/happyday/wut/afile", O_RDWR|O_CREAT, 0755)) < 0)
	  err(ret, "open failed");
	int fd = ret;

	snprintf(buf, sizeof(buf), "we fight for the user!");
	if ((ret = write(fd, buf, strlen(buf))) != strlen(buf))
	  err(ret, "write failed");

	return 0;
}
