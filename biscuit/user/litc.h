#include <littypes.h>

#define         RED     "\x1b[31;1m"
#define         GREEN   "\x1b[32;1m"
#define         BLUE    "\x1b[34;1m"
#define         RESET   "\x1b[0m"

#define MAXBUF        4096

void exit(int);
int fork(void);
int getpid(void);
int link(const char *, const char *);
int mkdir(const char *, long);
int open(const char *, int, int);
#define    O_RDONLY          0
#define    O_WRONLY          1
#define    O_RDWR            2
#define    O_CREAT        0x80
long read(int, void*, size_t);
long write(int, void*, size_t);

void errx(int, const char *, ...);
int printf(char *, ...);
int vprintf(const char *, va_list);
int printf_blue(char *, ...);
int printf_red(char *, ...);
int snprintf(char *, size_t, const char *, ...);
size_t strlen(char *);
