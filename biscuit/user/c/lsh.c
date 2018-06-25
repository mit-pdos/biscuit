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
			*f++ = '\0';
		strncpy(bp, line, be - bp);
		bp += strlen(bp) + 1;
		line = f;
	}
	args[ai] = NULL;
	//for (ai = 0; args[ai] != NULL; ai++)
	//	printf("arg %d: %s\n", ai, args[ai]);
}

int builtins(char *args[], size_t n)
{
	char *cmd = args[0];
	if (strncmp(cmd, "cd", 3) == 0) {
		int ret = chdir(args[1]);
		if (ret)
			printf("chdir to %s failed\n", args[1]);
		return 1;
	} else if (strncmp(cmd, "ps", 3) == 0) {
		if (sys_info(SINFO_PROCLIST) == -1)
			err(-1, "sys_info");
		return 1;
	}
	return 0;
}

int removefn(char *line, char *retbuf, size_t retsz)
{
	// gobble
	while (*line == ' ')
		line++;
	if (!*line)
		return -1;
	// get filename end; either nul or space
	char *end, *t;
	end = strchr(line, ' ');
	t = strchr(line, '\0');
	if (!end)
		end =  t;
	if (!end)
		errx(-1, "how");
	if (end - line + 1 > retsz)
		errx(-1, "retbuffer too small");
	while (line < end) {
		*retbuf++ = *line;
		*line++ = ' ';
	}
	*retbuf = '\0';
	return 0;
}

int redir(char *line, char **infn, char **outfn, int *append)
{
	static char inbuf[64];
	static char outbuf[64];

	*infn = *outfn = NULL;
	*append = 0;

	// remove all special tokens before grabbing filenames so that special
	// tokens can separate filenames (i.e.  "< file1>file2")
	char *inp;
	if ((inp = strchr(line, '<'))) {
		if (strchr(inp + 1, '<')) {
			printf("syntax error: two in redirects\n");
			return -1;
		}
		*inp = ' ';
	}
	char *outp;
	if ((outp = strstr(line, ">>"))) {
		if (strchr(outp + 2, '>')) {
			printf("syntax error: two out redirects\n");
			return -1;
		}
		outp[0] = ' ';
		outp[1] = ' ';
		*append = 1;
	} else if ((outp = strchr(line, '>'))) {
		if (strchr(outp + 1, '>')) {
			printf("syntax error: two out redirects\n");
			return -1;
		}
		*outp = ' ';
	}
	if (inp) {
		if (removefn(inp, inbuf, sizeof(inbuf))) {
			printf("syntax error: no in redirect filename\n");
			return -1;
		}
		*infn = inbuf;
	}
	if (outp) {
		if (removefn(outp, outbuf, sizeof(outbuf))) {
			printf("syntax error: no out redirect filename\n");
			return -1;
		}
		*outfn = outbuf;
	}
	return 0;
}

void doredirs(char *infn, char *outfn, int append)
{
	if (infn) {
		int fd = open(infn, O_RDONLY);
		if (fd < 0)
			err(-1, "open in redirect");
		if (dup2(fd, 0) < 0)
			err(-1, "dup2");
		close(fd);
	}
	if (outfn) {
		int flags = O_WRONLY|O_CREAT;
		if (append)
			flags |= O_APPEND;
		else
			flags |= O_TRUNC;
		int fd = open(outfn, flags);
		if (fd < 0)
			err(-1, "open out redirect");
		if (dup2(fd, 1) < 0)
			err(-1, "dup2");
		close(fd);
	}
}

int main(int argc, char **argv)
{
	int nbgs = 0;
	while (1) {
		// if you change the output of lsh, you need to update
		// posixtest() in usertests.c so the test is aware of the new
		// changes.
		char *args[64];
		size_t sz = sizeof(args)/sizeof(args[0]);
		char *infile, *outfile;
		int append;
		char *p = readline("# ");
		if (p == NULL)
			exit(0);
		char *com;
		if ((com = strchr(p, '#')))
			*com = '\0';
		int isbg = 0;
		if ((com = strchr(p, '&'))) {
			*com = ' ';
			isbg = 1;
		}
		if (redir(p, &infile, &outfile, &append))
			continue;
		mkargs(p, args, sz);
		if (args[0] == NULL)
			continue;
		if (builtins(args, sz))
			continue;
		int pid = fork();
		if (pid < 0)
			err(-1, "fork");
		if (pid) {
			if (isbg)
				nbgs++;
			else
				while (wait(NULL) != pid)
					;
			continue;
		}
		// if background job, fork another child to check the commands
		// exit code
		int pid2;
		if (isbg && (pid2 = fork()) != 0) {
			printf("background pid %d\n", pid2);
			int status;
			wait(&status);
			int st = WEXITSTATUS(status);
			if (!WIFEXITED(status) || st != 0)
				printf("background job %d failed with %d",
				    pid2, st);
			else
				printf("background job %d done\n", pid2);
			exit(st);
		}
		doredirs(infile, outfile, append);
		execvp(args[0], args);
		err(-1, "couldn't exec \"%s\"", args[0]);
	}

	return 0;
}
