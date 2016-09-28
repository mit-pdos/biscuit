#include <litc.h>

int main(int argc, char **argv)
{
	if (argc < 2)
		errx(-1, "usage: %s <file> [<file>...]", argv[0]);
	FILE *fout = fdopen(1, "w");
	if (!fout)
		err(-1, "fdopen");
	int i;
	for (i = 1; i < argc; i++) {
		char *fn = argv[i];
		FILE *f = fopen(fn, "r");
		if (!f)
			err(-1, "fopen");
		char buf;
		size_t ret;
		int nl = 0;
		while ((ret = fread(&buf, 1, 1, f)) == 1) {
			if (fwrite(&buf, 1, 1, fout) != 1)
				err(-1, "fwrite");
			// 24 lines at a time
			if (buf == '\n')
				nl++;
			if (nl == 24) {
				if (read(0, &buf, 1) != 1)
					err(-1, "read stdin");
				nl = 0;
			}
		}
		if (ferror(f))
			err(-1, "fread");
		fclose(f);
	}
	return 0;
}
