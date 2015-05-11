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
		printf(msg);
}

__thread long tlvar;

void child(void *msg)
{
	printf("thread var is: %ld\n", tlvar);
	printf("child loop (msg: %s)\n", msg);
	char b[2] = {'0' + tlvar, 0};
	loop(b, 10);
	printf("child exit\n");
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
	loop("p", 10);
	for (tid = 0; tid < 100000000; tid++);
	printf("parent exit");

	return 0;
}
