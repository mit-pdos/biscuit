#include <litc.h>

__thread int id;

int nthreads;
int count;

void *fn(void *a)
{
	int b = (int)(long)a;
	id = b;

	// barrier
	for (;;) {
		int oc = count;
		int nc = oc + 1;
		if (__sync_bool_compare_and_swap(&count, oc, nc))
			break;
	}

	int volatile *p = &count;
	while (*p != nthreads)
		;

	printf("my id is still: %d (%d)\n", id, b);
	if (id != b)
		errx(-1, "TLS fyuked!");

	return NULL;
}

int main(int argc, char **argv)
{
	if (argc > 1)
		nthreads = atoi(argv[1]);
	if (nthreads <= 0)
		nthreads = 3;

	printf("making %d threads\n", nthreads);
	pthread_t t[nthreads];
	int i;
	for (i = 0; i < nthreads; i++)
		if (pthread_create(&t[i], NULL, fn, (void *)(long)i))
			errx(-1, "pthread create");

	printf("started\n");

	for (i = 0; i < nthreads; i++) {
		void *ret;
		if (pthread_join(t[i], &ret))
			errx(-1, "pthread join");
		if (ret)
			errx(-1, "bad exit");
	}

	printf("joined\n");
	return 0;
}
