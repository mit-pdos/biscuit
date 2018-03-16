#include <stdio.h>

int main(int argc, char **argv)
{
	srand(time(NULL));
	//pid_t master = getpid();
	//switch (fork()) {
	//case 0:
	//	dirents();
	//case -1:
	//	err(-1, "fork");
	//}

	int done = 0;
	for (int i = 0; i < 10 && !done; i++) {
		switch (fork()) {
		case -1:
			err(-1, "fork");
			break;
		case 0:
			done = 1;
			break;
		}
	}


	int afd, bfd;
	if ((afd = open("/", O_RDONLY | O_DIRECTORY)) == -1)
		err(-1, "open");
	if ((bfd = open("/", O_RDONLY | O_DIRECTORY)) == -1)
		err(-1, "open");

	char buf[512];
	sprintf(buf, "%ld", getpid());
	for (;;) {
		if (open(buf, O_CREAT | O_RDWR, 0600) == -1)
			break;
		if (unlink(buf) == -1)
			err(-1, "unlink");
	}

	for (;;) {
		void *p = mmap(NULL, 4096, PROT_READ | PROT_WRITE,
		    MAP_PRIVATE, afd, 0);
		if (p == MAP_FAILED)
			err(-1, "mmap");
		p = mmap(NULL, 4096, PROT_READ | PROT_WRITE,
		    MAP_PRIVATE, bfd, 0);
		if (p == MAP_FAILED)
			err(-1, "mmap");
	}

	return 0;
}
