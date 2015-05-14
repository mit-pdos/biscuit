#include <litc.h>

char __progname[64];

void
_entry(int argc, char **argv)
{
	if (argc)
		strncpy(__progname, argv[0], sizeof(__progname));
	int main(int, char **);
	int ret = main(argc, argv);
	exit(ret);
}
