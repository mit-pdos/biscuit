#include <litc.h>

int main()
{
	int ret;
	if ((ret = open("/etc/passwd", O_RDONLY, 0)) >= 0) {
		printf_red("should have failed\n");
		return -1;
	}
	if ((ret = open("/hi.txt", O_RDONLY, 0)) < 0) {
		printf_red("should have succeeded 1\n");
		return -1;
	}
	if ((ret = open("/boot/eufi/readme.txt", O_RDONLY, 0)) < 0) {
		printf_red("should have succeeded 2\n");
		return -1;
	}

	printf_blue("fs tests passed!\n");
	return 0;
}
