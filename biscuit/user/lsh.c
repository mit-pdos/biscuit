#include <litc.h>

int main(int argc, char **argv)
{
	while (1) {
		char *p = readline("# ");
		int pid = fork();
		if (pid)
			// XXX wait()
			continue;
		int ret = execv(p, NULL);
		if (ret)
			errx(ret, "exec \"%s\" failed\n", p);
	}

	return 0;
}
