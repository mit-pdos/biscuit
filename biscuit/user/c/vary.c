#include <litc.h>

static long vmin = LONG_MAX, vmax = LONG_MIN;
static long vtot, vn;

static long
nowms(void)
{
	struct timeval tv;
	if (gettimeofday(&tv, NULL))
		err(-1, "gettimeofday");
	return tv.tv_sec*1000 + tv.tv_usec/1000;
}

__attribute__((unused))
static void fexec_nofail(char * const args[])
{
	const int check = 0;
	switch (fork()) {
	case -1:
		err(-1, "fork (%s)", args[0]);
	case 0:
		execvp(args[0], args);
		err(-1, "exec (%s)", args[0]);
	default:
		{
		int status;
		if (wait(&status) == -1)
			err(-1, "wait");
		if (!WIFEXITED(status) || WEXITSTATUS(status) != 0) {
			if (check)
				err(-1, "child failed (%s)", args[0]);
			else
				fprintf(stderr, "child failed (%s)\n", args[0]);
		}
		}
	}
}

static void forkmeasure(char * const args[])
{
	long st = nowms();

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
		break;
	}

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

	long this = -1;
	char buf[256];
	while (fgets(buf, sizeof(buf), fop) != NULL) {
		char *h;
		const char * const pat = "ops: ";
		//const char * const pat = "messages/sec: ";
		if ((h = strstr(buf, pat)) != NULL) {
			h += strlen(pat);
			long t = strtol(h, NULL, 10);
			vmin = (t < vmin) ? t : vmin;
			vmax = (t > vmax) ? t : vmax;
			vn++;
			vtot += t;
			this = t;
		}
	}
	if (ferror(fop))
		err(-1, "ferror");
	if (!feof(fop))
		err(-1, "no eof?");

	fclose(fop);
	close(p[0]);
	printf("highest variance: %.3f (%ld / %ld), avg %.3f"
	    //" (%ld / %ld)\n",
	    " (%ld)\n",
	    (double)vmax / vmin, vmax, vmin, (double)vtot / vn,
	    //vtot, vn);
	    this);
	printf("   (took %ld ms)\n", nowms() - st);
}

static void usage(void)
{
	printf("\n"
		//"usage: %s [-n threads] [-s seconds] [-b sfork benchmark]\n"
		"usage: %s [-n threads] [-s seconds] [-r runs]\n"
		"\n", __progname);
	exit(-1);
}

static void chtemp(void)
{
	char buf[] = "/tmp/dir.XXXXXX";
	if (mkdtemp(buf) == NULL)
		err(-1, "mkdtemp");
	if (chdir(buf) == -1)
		err(-1, "chdir");
}

int main(int argc, char **argv)
{
	char *secs = "5";
	char *threads = "1";
	int runs = -1;
	//char *bm = "c";
	int c;
	while ((c = getopt(argc, argv, "gn:s:r:")) != -1) {
		switch (c) {
		default:
			usage();
			break;
		case 'n':
			threads = optarg;
			break;
		//case 'b':
		//	bm = optarg;
		//	break;
		case 'r':
			runs = strtol(optarg, NULL, 0);
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

	chtemp();

	for (int r = 0;; r++) {
		if (runs != -1 && r >= runs) {
			printf("completed %d runs\n", r);
			break;
		}
		//int sleeps = random() % 5;
		printf("%s threads, %s secs\n", threads, secs);
		//printf("%s threads, %s secs, sleep %d...\n", threads, secs, sleeps);
		//sleep(sleeps);

		char * const args[] = {"pstat", "-s", secs, "-n", threads, NULL};
		forkmeasure(args);

		//char * const rmt[] = {"rmtree", "./", NULL};
		//fexec_nofail(rmt);
		//char * const args[] = {"parrun", "-d", secs, "-m", threads, NULL};
		//forkmeasure(args);
	}

	return 0;
}
