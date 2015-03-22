#include <littypes.h>

#define         RED     "\x1b[31;1m"
#define         GREEN   "\x1b[32;1m"
#define         BLUE    "\x1b[34;1m"
#define         RESET   "\x1b[0m"

#define EPERM		1
#define ENOENT		2
#define EBADF		9
#define EFAULT		14
#define EEXIST		17
#define ENOTDIR		20
#define EINVAL		22
#define ENAMETOOLONG	36
#define ENOSYS		38

#define MAXBUF        4096

struct __attribute__((packed)) stat {
	ulong	st_dev;
	ulong	st_ino;
	ulong	st_mode;
	ulong	st_size;
};

#define S_ISDIR(mode)	(mode == 2)
#define S_ISREG(mode)	(mode == 1)

int close(int);
void exit(int);
int execv(const char *, const char **);
int fork(void);
int fstat(int, struct stat *);
int getpid(void);
int link(const char *, const char *);
int mkdir(const char *, long);
int open(const char *, int, int);
#define    O_RDONLY          0
#define    O_WRONLY          1
#define    O_RDWR            2
#define    O_CREAT        0x80
long read(int, void*, size_t);
int unlink(const char *);
long write(int, void*, size_t);

void err(int, const char *, ...);
void errx(int, const char *, ...);
int printf(char *, ...);
int vprintf(const char *, va_list);
int printf_blue(char *, ...);
int printf_red(char *, ...);
char *readline(char *);
int snprintf(char *, size_t, const char *, ...);
char *strncpy(char *, const char *, size_t);
size_t strlen(char *);
