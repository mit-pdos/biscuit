#include <litc.h>

int main(int argc, char **argv)
{
	while (1) {
		char *ret = readline("input:");
		printf("got: %s\n", ret);
	}

	return 0;
}
