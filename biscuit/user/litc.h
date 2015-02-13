#include <littypes.h>

#define MAXBUF        4096

void exit(int);
int fork(void);
int getpid(void);
int open(const char *, int, int);
#define    O_RDONLY          0
#define    O_WRONLY          1
#define    O_RDWR            2
long write(int, void*, size_t);

int printf(char *, ...);
