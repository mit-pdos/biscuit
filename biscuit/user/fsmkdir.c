#include <litc.h>

static char buf[1024];

int main()
{
	int ret;
	if ((ret = mkdir("/wtf", 0755)) < 0)
	  errx(ret, "mkdir failed");

	if ((ret = mkdir("/wtf/happyday", 0755)) < 0)
	  errx(ret, "mkdir failed");

	if ((ret = mkdir("/wtf/happyday/wut", 0755)) < 0)
	  errx(ret, "mkdir failed");

	if ((ret = open("/wtf/happyday/wut/afile", O_RDWR|O_CREAT, 0755)) < 0)
	  errx(ret, "open failed");
	int fd = ret;

	snprintf(buf, sizeof(buf), "we fight for the user!");
	if ((ret = write(fd, buf, strlen(buf))) != strlen(buf))
	  errx(ret, "write failed");

	return 0;
}
