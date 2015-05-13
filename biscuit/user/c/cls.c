#include <litc.h>

int main(int argc, char **argv)
{
	char buf[81];
	int i;
	for (i = 0; i < 79; i++)
		buf[i] = ' ';
	buf[sizeof(buf) - 2] = '\n';
	buf[sizeof(buf) - 1] = 0;
	for (i = 0; i < 24; i++)
		printf("%s", buf);
	return 0;
}
