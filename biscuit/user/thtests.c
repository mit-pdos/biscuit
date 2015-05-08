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
	int i, j;
	for (j = 0; j < iters; j++) {
		for (i = 0; i < 100000000; i++);
		printf(msg);
	}
}

void child(void *msg)
{
	printf("child loop (msg: %s)\n", msg);
	loop("c", 10);
	printf("child exit\n");
}

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
	tf.tf_tcb = NULL;
	tf.tf_tid = NULL;
	tf.tf_stack = mkstack(4096*3);
	int tid = tfork_thread(&tf, child, "yahoo!");
	if (tid < 0)
		err(tid, "tfork");
	printf("parent loop\n");
	loop("p", 10);
	printf("parent exit");

	return 0;
}
