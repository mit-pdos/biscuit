#include <litc.h>

int main(int argc, char **argv)
{
	// find unique dirname for depth
	char dname[] = "/dA";
	while (1) {
		int fd;
		if ((fd = open(dname, O_RDONLY, 0)) < 0) {
			close(fd);
			break;
		}
		if (dname[2] > 'Z')
			errx(-1, "couldn't find unique dirname");
		dname[2]++;
	}

	const int depth = 10;
	char boof[1024];
	char *p = boof;
	char *end = boof + sizeof(boof);
	int i;
	for (i = 0; i < depth; i++) {
		p += snprintf(p, end - p, "%s", dname);
		int ret;
		if ((ret = mkdir(boof, 0)) < 0)
			err(ret, "mkdir");
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
