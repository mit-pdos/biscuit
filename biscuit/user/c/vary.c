#include <litc.h>

static long vmin = LONG_MAX, vmax = LONG_MIN;
static long vtot, vn;

static void fexec(char * const args[])
{
	int p[2];
	if (pipe(p) == -1)
		err(-1, "pipe");
	switch (fork()) {
	case -1:
		err(-1, "fork (%s)", args[0]);
	case 0:
		if (dup2(p[1], 1) == -1)
			err(-1, "dup2");
		//if (dup2(p[1], 2) == -1)
		//	err(-1, "dup2");
		close(p[0]);
		close(p[1]);
		execvp(args[0], args);
		err(-1, "exec (%s)", args[0]);
	default:
		{
		close(p[1]);
		int status;
		if (wait(&status) == -1)
			err(-1, "wait");
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
			errx(-1, "child failed (%s) %d", args[0],
			    WEXITSTATUS(status));
		FILE *fop = fdopen(p[0], "r");
		if (!fop)
			err(-1, "fdopen");

		char buf[256];
		while (fgets(buf, sizeof(buf), fop) != NULL) {
			char *h;
			const char * const pat = "ops: ";
			if ((h = strstr(buf, pat)) != NULL) {
				h += strlen(pat);
				long t = strtol(h, NULL, 10);
				vmin = (t < vmin) ? t : vmin;
				vmax = (t > vmax) ? t : vmax;
				vn++;
				vtot += t;
			}
		}
		if (ferror(fop))
			err(-1, "ferror");
		if (!feof(fop))
			err(-1, "no eof?");

		fclose(fop);
		close(p[0]);
		printf("highest variance: %.3f (%ld / %ld), avg %.3f"
		    " (%ld / %ld)\n", (double)vmax / vmin, vmax, vmin,
		    (double)vtot / vn, vtot, vn);
		}
	}
}

static void usage(void)
{
	printf("\n"
		"usage: %s [-n threads] [-s seconds] [-b sfork benchmark]\n"
		"\n", __progname);
	exit(-1);
}

int main(int argc, char **argv)
{
	char *secs = "5";
	char *threads = "1";
	char *bm = "c";
	int c;
	while ((c = getopt(argc, argv, "n:s:b:")) != -1) {
		switch (c) {
		default:
			usage();
			break;
		case 'n':
			threads = optarg;
			break;
		case 'b':
			bm = optarg;
			break;
		case 's':
			secs = optarg;
			break;
		}
	}
	argc -= optind;
	argv += optind;
	if (argc != 0)
		usage();

	for (;;) {
		int sleeps = random() % 5;
		printf("%s threads, %s secs, sleep %d...\n", threads, secs, sleeps);
		sleep(sleeps);
		char * const args[] = {"sfork", "-s", secs, "-b", bm, threads, NULL};
		fexec(args);
	}

	return 0;
}
