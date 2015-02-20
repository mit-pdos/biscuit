#include <littypes.h>

#define         RED     "\x1b[31;1m"
#define         GREEN   "\x1b[32;1m"
#define         BLUE    "\x1b[34;1m"
#define         RESET   "\x1b[0m"

#define MAXBUF        4096

void exit(int);
int fork(void);
int getpid(void);
int open(const char *, int, int);
#define    O_RDONLY          0
#define    O_WRONLY          1
#define    O_RDWR            2
long read(int, void*, size_t);
long write(int, void*, size_t);

int printf(char *, ...);
int printf_blue(char *, ...);
int printf_red(char *, ...);
int snprintf(char *, size_t, char *, ...);
size_t strlen(char *);
