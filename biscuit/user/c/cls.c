#include <litc.h>

int main(int argc, char **argv)
{
	char buf[2000];
	int i;
	for (i = 0; i < sizeof(buf); i++)
		buf[i] = ' ';
	buf[sizeof(buf) - 1] = 0;
	printf("%s", buf);
	return 0;
}
