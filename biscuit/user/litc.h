#include <littypes.h>

#define MAXBUF        4096

int getpid(void);
int fork(void);
long write(int, void*, size_t);
void exit(int);

int printf(char *, ...);
