#include <litc.h>

char *ps[1000];

int main(int argc, char **argv)
{
	int times;
	for (times = 0; times < 10; times++) {
		const size_t sz = 1 << 13;
		char *p = mmap(NULL, sz, PROT_READ | PROT_WRITE,
		    MAP_ANON | MAP_PRIVATE, -1, 0);
		if (p == MAP_FAILED)
			errx(-1, "mmap");

		int i;
		for (i = 0; i < sz; i++)
			p[i] = 0xcc;
		int ret;
		if ((ret = munmap(p, sz)) < 0)
			err(ret, "munmap");
	}

	for (times = 0; times < 10; times++) {
		int iters = sizeof(ps)/sizeof(ps[0]);
		int i;
		for (i = 0; i < iters; i++)
			ps[i] = malloc(30);
		for (i = 0; i < iters; i++)
			free(ps[i]);
	}

	printf("success\n");

	return 0;
}
