#include <stdio.h>
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <unistd.h>
#include <string.h>


#define BSIZE  4096
char buf[BSIZE];

int main(int argc, char *argv[])
{
  if (argc != 3) {
    printf("Usage: %s filename n\n", argv[0]);
    exit(-1);
  }
  int n = atoi(argv[2]);

  FILE *f = fopen(argv[1], "r");
  if (f == NULL)
    err(-1, "fopen");
  size_t r;
  if ((r = fread(buf, 1, n, f)) > 0) {
    for (int i = 0; i < r; i++) {
      printf("%c", buf[i]);
    }
  }
  printf("\n");
  return 0;
}
