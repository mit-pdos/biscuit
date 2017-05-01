#include <stdio.h>

void dirents(void)
{
	char *path = "/tmp/dcbloat";
	mkdir(path, 0755);
	if (chdir(path) == -1)
		err(-1, "chdir");
	for (;;) {
		// fill up vnode and dirent cache
		for (;;) {
			int fd = mkstemp("XXXXXX");
			if (fd == -1)
				break;
			if (close(fd) == -1)
				err(-1, "close");
		}
		DIR *d;
		while (!(d = opendir("."))) {
			printf("-O-");
			usleep(10000);
		}
		struct dirent di, *de;
		int i;
		for (i = 0; i < 1000; i++) {
			while (readdir_r(d, &di, &de) != 0) {
				printf("-R-");
				usleep(10000);
			}
			if (de == NULL) {
				printf("-R-");
				usleep(10000);
			}
			if (strcmp(de->d_name, "..") == 0 ||
			    strcmp(de->d_name, ".") == 0)
				continue;
			if (unlink(de->d_name) == -1)
				errx(-1, "unlink");
		}
		if (closedir(d) == -1)
			err(-1, "closedir");
		printf("REMMED %d\n", i);
	}
}

void gcstat(void)
{
	int stgcs;
	if ((stgcs = sys_info(SINFO_GCCOUNT)) == -1)
		err(-1, "sysinfo");
	for (;;) {
		sleep(5);
		//int gcs;
		//if ((gcs = sys_info(SINFO_GCCOUNT)) == -1)
		//	err(-1, "sysinfo");
		//printf("GCs: %d\n", gcs - stgcs);
		//stgcs = gcs;
		//sys_info(SINFO_GCHEAPSZ);
		sys_info(10);
	}
}

int main(int argc, char **argv)
{
	srand(time(NULL));
	pid_t master = getpid();
	switch (fork()) {
	case 0:
		dirents();
	case -1:
		err(-1, "fork");
	}
	//switch (fork()) {
	//case 0:
	//	gcstat();
	//case -1:
	//	err(-1, "fork");
	//}

	int afd, bfd;
	if ((afd = open("/", O_RDONLY | O_DIRECTORY)) == -1)
		err(-1, "open");
	if ((bfd = open("/", O_RDONLY | O_DIRECTORY)) == -1)
		err(-1, "open");
	int i;
yahoo:
	for (i = 0; i < 100; i++) {
		pid_t c = fork();
		if (c == 0)
			i = -1;
		else if (c == -1)
			break;
	}
	//int upto = i;

	// max size fd tables
	int do1, do2, do3;
	do1 = do2 = do3 = 1;
	pid_t mypid = getpid();
	while (mypid != master && (do1 || do2 || do3)) {
		if (do1 && open("/", O_RDONLY | O_DIRECTORY) == -1)
			do1 = 0;
		// alternate to make sure vma objects aren't coalesced.
		if (do2) {
			void *r = mmap(NULL, 4096, PROT_READ, MAP_PRIVATE, afd, 0);
			if (r == MAP_FAILED)
				do2 = 0;
			r = mmap(NULL, 4096, PROT_READ, MAP_PRIVATE, bfd, 0);
			if (r == MAP_FAILED)
				do2 = 0;
		}
		if (do3) {
			int s;
			if ((s = socket(AF_INET, SOCK_STREAM, 0)) == -1)
				do3 = 0;
			struct sockaddr_in _sa;
			_sa.sin_family = AF_INET;
			_sa.sin_port = htons(10);
			// connect to hacked IP which fails after allocating
			// TCB data structures
			_sa.sin_addr.s_addr = htonl(0xaaaaaaaa);
			struct sockaddr *sa = (struct sockaddr *)&_sa;
			if (connect(s, sa, sizeof(_sa)) == -1 &&
			    errno == -ENOMEM)
				do3 = 0;
		}
	}
	//for (i = 0; i < upto; i++)
	//	if (waitpid(pids[i], NULL, 0) == -1)
	//		err(-1, "wait");
	if (getpid() == master) {
		pid_t w;
		while ((w = wait4(WAIT_ANY, NULL, WNOHANG, NULL)) > 0)
			;
		if (w == -1 && errno != ECHILD)
			err(-1, "master");
		sleep(rand() % 10);
		printf("again %d\n", i);
		goto yahoo;
	}
	exit(0);
}
