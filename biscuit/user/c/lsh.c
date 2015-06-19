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
		while (*line == ' ')
			line++;
		if (*line == 0)
			break;
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

char *binname(char *bin)
{
	static char buf[64];

	// absoulte path
	if (bin[0] == '/') {
		snprintf(buf, sizeof(buf), "%s", bin);
		return bin;
	}

	// try paths
	char *paths[] = {"/bin/"};
	const int elems = sizeof(paths)/sizeof(paths[0]);
	int i;
	for (i = 0; i < elems; i++) {
		snprintf(buf, sizeof(buf), "%s%s", paths[i], bin);
		struct stat st;
		if (stat(buf, &st) == 0)
			return buf;

	}
	return NULL;
}

int builtins(char *args[], size_t n)
{
	char *cmd = args[0];
	if (strncmp(cmd, "cd", 2) == 0) {
		int ret = chdir(args[1]);
		if (ret)
			printf("chdir to %s failed\n", args[1]);
		return 1;
	}
	return 0;
}

int main(int argc, char **argv)
{
	while (1) {
		char *args[10];
		size_t sz = sizeof(args)/sizeof(args[0]);
		char *p = readline("# ");
		mkargs(p, args, sz);
		if (args[0] == NULL)
			continue;
		if (builtins(args, sz))
			continue;
		int pid = fork();
		if (pid < 0)
			err(pid, "fork");
		if (pid) {
			wait(NULL);
			continue;
		}
		char *bin = binname(args[0]);
		if (bin == NULL)
			errx(-1, "no such binary: %s", args[0]);
		int ret = execv(bin, args);
		if (ret)
			err(ret, "couldn't exec \"%s\"\n", p);
	}

	return 0;
}
