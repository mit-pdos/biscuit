#include <stdio.h>
#include <err.h>

#include <sys/utsname.h>

__attribute__((noreturn))
void usage(void)
{
	printf( "usage:\n"
		"%s [-amnrsv]\n"
		"\n"
		"-a	print all utsname data\n"
		"-m	hardware name\n"
		"-n	host name\n"
		"-r	OS release\n"
		"-s	OS name\n"
		"-v	OS version\n"
		"\n", __progname);
	exit(-1);
}

int main(int argc, char **argv)
{
	int sys, nn, rel, ver, mach;
	sys = nn = rel = ver = mach = 0;

	int isopt = 0;
	int c;
	while ((c = getopt(argc, argv, "amnrsv")) != -1) {
		isopt = 1;
		switch (c) {
		case 'a':
			sys = nn = rel = ver = mach = 1;
			break;
		case 'm':
			mach = 1;
			break;
		case 'n':
			nn = 1;
			break;
		case 'r':
			rel = 1;
			break;
		case 's':
			sys = 1;
			break;
		case 'v':
			ver = 1;
			break;
		default:
			usage();
		}
	}
	if (!isopt)
		sys = 1;

	struct utsname un;
	if (uname(&un))
		err(-1, "uname (wut)");

	int sp = 0;
#define	PR(f)	do {	const char *_fmt; 	\
			if (sp) _fmt = " %s";	\
			else _fmt = "%s";	\
			printf(_fmt, un.f);	\
			sp = 1;			\
		} while(0)
	if (sys)
		PR(sysname);
	if (nn)
		PR(nodename);
	if (rel)
		PR(release);
	if (ver)
		PR(version);
	if (mach)
		PR(machine);
	printf("\n");
	return 0;
}
