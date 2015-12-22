#include <stdio.h>
#include <unistd.h>

int main(int argc, char **argv)
{
	char buf[256];
	if (!getcwd(buf, sizeof(buf)))
		err(-1, "getcwd");
	printf("%s\n", buf);
	return 0;
}
