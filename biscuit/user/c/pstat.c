#include <stdio.h>

__attribute__((unused))
static long
nowms(void)
{
	struct timeval tv;
	if (gettimeofday(&tv, NULL))
		err(-1, "gettimeofday");
	return tv.tv_sec*1000 + tv.tv_usec/1000;
}

static void
usage(void)
{
	printf( "\n"
		"Usage:\n"
		"%s [-s sec] [-n nprocs]\n"
		"\n", __progname);
	exit(-1);
}

static void
child(int me, int childin, int childout, int durationsec)
{
	char stdir[64];
	snprintf(stdir, sizeof(stdir), "%d", me);
	if (mkdir(stdir, 0700) == -1)
		err(-1, "mkdir");

	if (chdir(stdir) == -1)
		err(-1, "chdir");

	if (mkdir(stdir, 0700) == -1)
		err(-1, "mkdir");

	char stfn[64];
	snprintf(stfn, sizeof(stfn), "%s/%d", stdir, me);
	int fd;
	if ((fd = open(stfn, O_CREAT | O_WRONLY | O_EXCL, 0600)) == -1)
		err(-1, "open");
	close(fd);

	char go;
	if (read(childin, &go, 1) != 1)
		err(-1, "go read");

	long st = nowms();
	ulong did = 0;
	struct stat stbuf;
	for (;;) {
		if ((++did % 100) == 0)
			if ((nowms() - st) >= durationsec * 1000)
				break;
		if (stat(stfn, &stbuf) == -1)
			err(-1, "stat");
	}
	if (write(childout, &did, sizeof(did)) != sizeof(did))
		err(-1, "xput write");

	if (unlink(stfn) == -1)
		err(-1, "unlink");
	if (rmdir(stdir) == -1)
		err(-1, "rmdir");
	if (chdir("../") == -1)
		err(-1, "chdir");
	if (rmdir(stdir) == -1)
		err(-1, "rmdir");

	exit(0);
}

static void
waitone(void)
{
	int status;
	if (wait(&status) == -1)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed %d", WEXITSTATUS(status));
}

int main(int argc, char **argv)
{
	int durationsec = 5;
	int nprocs = 4;
	int c;
	while ((c = getopt(argc, argv, "n:s:")) != -1) {
		switch (c) {
		case 's':
			durationsec = strtol(optarg, NULL, 0);
			break;
		case 'n':
			nprocs = strtol(optarg, NULL, 0);
			break;
		default:
			usage();
			break;
		}
	}
	argc -= optind;
	argv += optind;
	if (argc > 0 || nprocs <= 0 || durationsec <= 0)
		usage();
	printf("%d procs, %d seconds\n", nprocs, durationsec);

	char dbuf[] = "/tmp/dir.XXXXXX";
	if (mkdtemp(dbuf) == NULL)
		err(-1, "mkdtemp");
	if (chdir(dbuf) == -1)
		err(-1, "chdir");

	int tochald[2];
	if (pipe(tochald) == -1)
		err(-1, "pipe");
	int topar[2];
	if (pipe(topar) == -1)
		err(-1, "pipe");

	for (int i = 0; i < nprocs; i++) {
		pid_t c = fork();
		if (c == -1)
			err(-1, "fork");
		else if (c == 0) {
			close(tochald[1]);
			close(topar[0]);
			child(i, tochald[0], topar[1], durationsec);
		}
	}
	close(tochald[0]);
	close(topar[1]);
	int parin = topar[0];
	int parout = tochald[1];
	for (int i = 0; i < nprocs; i++) {
		if (write(parout, "A", 1) != 1)
			err(-1, "go write");
	}

	long outter = nowms();
	ulong tot = 0;
	for (int i = 0; i < nprocs; i++) {
		ulong t;
		if (read(parin, &t, sizeof(t)) != sizeof(t))
			err(-1, "read xput");
		printf("    [%ld]\n", t);
		tot += t;
	}
	long elapms = nowms() - outter;
	for (int i = 0; i < nprocs; i++) {
		waitone();
	}
	double secs = (double)elapms / 1000;
	ulong xpsec = (double)tot / secs;
	printf("ops: %lu /sec, took %ld ms\n", xpsec, elapms);

	if (rmdir(dbuf) == -1)
		err(-1, "rmdir");

	return 0;
}
