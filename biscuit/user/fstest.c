#include <litc.h>

int main()
{
	int ret;
	if ((ret = open("/etc/passwd", O_RDONLY, 0)) < 0) {
		printf("failure! %d\n", ret);
		return -1;
	}

	printf("success!\n");
	return 0;
}
