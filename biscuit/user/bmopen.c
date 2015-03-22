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
	int make = 1;
	int i;
	for (i = 0; i < depth; i++) {
		p += snprintf(p, end - p, "%s", dname);
		if (make)
			if (mkdir(boof, 0) < 0) {
				make = 0;
				printf("dirs already made\n");
			}
	}
	char *fn = "/politicians";
	snprintf(p, end - p, "%s", fn);

	printf("using \"%s\"...\n", boof);
	for (i = 0; i < 100; i++) {
		int ret;
		if ((ret = open(boof, O_RDWR | O_CREAT, 0)) < 0) {
			err(ret, "open");
		}
		if ((ret = unlink(boof)) < 0)
			err(ret, "unlink");
	}
	printf("done\n");

	return 0;
}
