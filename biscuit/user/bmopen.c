#include <litc.h>

int main(int argc, char **argv)
{
	char dname[] = "/d";

	int depth = 10;
	if (argc >= 2)
		depth = atoi(argv[1]);
	printf("using depth: %d\n", depth);

	char boof[1024];
	char *p = boof;
	char *end = boof + sizeof(boof);
	int i;
	for (i = 0; i < depth; i++) {
		p += snprintf(p, end - p, "%s", dname);
		mkdir(boof, 0);
	}
	char *fn = "/politicians";
	snprintf(p, end - p, "%s", fn);

	printf("using \"%s\"...\n", boof);
	open(boof, O_RDWR | O_CREAT, 0);
	for (i = 0; i < 100; i++) {
		int ret;
		if ((ret = open(boof, O_RDONLY, 0)) < 0)
			err(ret, "open");
	}
	printf("done\n");

	return 0;
}
