#include <litc.h>

__thread int id;

int nthreads;
int count;

void barrier_init()
{
	count = 0;
}

void barrier()
{
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
}

void *fn(void *a)
{
	int b = (int)(long)a;
	id = b;

	// barrier
	barrier();

	printf("my id is still: %d (%d)\n", id, b);
	if (id != b)
		errx(-1, "TLS fyuked!");

	return NULL;
}

static long volatile go __attribute__((aligned(4096)));
static int blah __attribute__((aligned(4096)));

void *groupfault(void *a)
{
	int b = (int)(long)a;

	while (!go)
		asm volatile("pause");

	blah = b;

	return NULL;
}

static int volatile observe __attribute__((aligned(4096))) = 10;

void acquire(void)
{
	while (__sync_lock_test_and_set(&go, 1) != 0)
		;
}

void release(void)
{
	go = 0;
}

void *tlbinval(void *a)
{
	acquire();
	if (observe != 0x31337)
		errx(-1, "didn't get tlb invalidate: %d", observe);
	release();
	return NULL;
}

void joinall(pthread_t t[])
{
	int i;
	for (i = 0; i < nthreads; i++) {
		void *ret;
		if (pthread_join(t[i], &ret))
			errx(-1, "pthread join");
		if (ret)
			errx(-1, "bad exit");
	}
}

int main(int argc, char **argv)
{
	if (argc > 1)
		nthreads = atoi(argv[1]);
	if (nthreads <= 0)
		nthreads = 3;

	barrier_init();
	printf("making %d threads\n", nthreads);
	pthread_t t[nthreads];
	int i;
	for (i = 0; i < nthreads; i++)
		if (pthread_create(&t[i], NULL, fn, (void *)(long)i))
			errx(-1, "pthread create");

	printf("started\n");

	joinall(t);
	printf("joined. doing group fault...\n");

	// test simultaneous faults
	for (i = 0; i < nthreads; i++)
		if (pthread_create(&t[i], NULL, groupfault, (void *)(long)i))
			errx(-1, "pthread create");

	sleep(1);
	printf("fork\n");

	// make address space copy-on-write by forking
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	else if (c == 0)
		return 0;

	go = 1;
	joinall(t);
	go = 0;

	printf("testing TLB shootdowns...\n");
	// test TLB shootdowns
	acquire();
	for (i = 0; i < nthreads; i++)
		if (pthread_create(&t[i], NULL, tlbinval, NULL))
			errx(-1, "pthread create");

	observe = 0x31337;
	release();

	joinall(t);

	printf("success\n");

	return 0;
}
