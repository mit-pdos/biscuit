#include <litc.h>

void *mkstack(size_t size)
{
	const size_t pgsize = 1 << 12;
	size += pgsize - 1;
	size &= ~(pgsize - 1);
	char *ret = mmap(NULL, size, PROT_READ | PROT_WRITE,
	    MAP_ANON | MAP_PRIVATE, -1, 0);
	if (!ret)
		errx(-1, "couldn't mmap");
	return ret + size;
}

void loop(char *msg, const int iters)
{
	int j;
	for (j = 0; j < iters; j++)
		// appease gcc
		printf("%s", msg);
}

__thread long tlvar;

long child(void *msg)
{
	printf("thread var is: %ld\n", tlvar);
	printf("child loop (msg: %s)\n", (char *)msg);
	char buf[10];
	snprintf(buf, sizeof(buf), "%ld ", tlvar);
	loop(buf, 30);
	printf("child exit\n");
	return tlvar;
}

static long tls1;
static long tls2;
static void *tcbs[] = {(char *)&tls1 + 8, (char *)&tls2 + 8};

int main(int argc, char **argv)
{
	printf("start thread tests\n");

	//int pid = fork();
	//if (pid == 0) {
	//	threxit(1001);
	//	errx(-1, "why no exit");
	//	return -2;
	//}
	//int status;
	//if (wait(&status) != pid)
	//	errx(-1, "wrong pid: %d\n", pid);
	//if (status != 0)
	//	errx(-1, "posix is sad");

	struct tfork_t tf;

	tf.tf_tcb = &tcbs[0];
	tf.tf_tid = &tls1;
	tf.tf_stack = mkstack(4096*1);
	int tid = tfork_thread(&tf, child, "child 1!");
	if (tid < 0)
		err(tid, "tfork");
	if (tid != tls1)
		errx(-1, "tid mismatch");

	tf.tf_tcb = &tcbs[1];
	tf.tf_tid = &tls2;
	tf.tf_stack = mkstack(4096*1);
	tid = tfork_thread(&tf, child, "child 2!");
	if (tid < 0)
		err(tid, "tfork");
	if (tid != tls2)
		errx(-1, "tid mismatch");

	printf("parent loop\n");
	loop("p ", 10);

	printf("waiting for child threads\n");
	long status;
	if ((tid = thrwait(tls1, &status)) != tls1)
		err(tid, "thrwait1");
	if (status != tls1)
		errx(-1, "bad status %ld %ld", status, tls1);
	if ((tid = thrwait(tls2, &status)) != tls2)
		err(tid, "thrwait2");
	if (status != tls2)
		errx(-1, "bad status %ld %ld", status, tls2);
	printf("parent exit\n");

	return 0;
}
