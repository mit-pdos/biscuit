#include <litc.h>

void mkargs(char *line, char *args[], size_t n)
{
	static char boof[1024];
	int ai;
	char *f = line;
	char *bp = boof;
	char *be = boof + sizeof(boof);
	for (ai = 0; line && ai < n - 1; ai++) {
		if (be - bp <= 0)
			errx(-1, "no boof");
		args[ai] = bp;
		f = strstr(line, " ");
		if (f)
			*f++ = 0;
		strncpy(bp, line, be - bp);
		bp += strlen(bp) + 1;
		line = f;
	}
	args[ai] = NULL;
	//for (ai = 0; args[ai] != NULL; ai++)
	//	printf("arg %d: %s\n", ai, args[ai]);
}

int main(int argc, char **argv)
{
	while (1) {
		char *args[10];
		size_t sz = sizeof(args)/sizeof(args[0]);
		char *p = readline("# ");
		mkargs(p, args, sz);
		int pid = fork();
		if (pid)
			// XXX wait()
			continue;
		int ret = execv(args[0], (const char **)args);
		if (ret)
			errx(ret, "couldn't exec \"%s\"\n", p);
	}

	return 0;
}
