#include <stdio.h>
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <unistd.h>
#include <string.h>
#include <sys/stat.h>


#define BSIZE  4096
char buf[BSIZE];
struct stat statbuf;

int main(int argc, char *argv[])
{
  if (argc != 2) {
    printf("Usage: %s filename\n", argv[0]);
    exit(-1);
  }

  if(stat(argv[1], &statbuf) < 0) {
    err(-1, "stat");
  }
  int fd = open(argv[1], O_RDONLY, 0);
  if (fd < 0)
    err(-1, "fopen");
  ulong cksum = 0;
  size_t tot = 0, r;
  while ((r = read(fd, buf, sizeof(buf))) > 0) {
    for (int i = 0; i < r; i++) {
      cksum += buf[i];
    }
    tot += r;
  }
  if (tot != statbuf.st_size) {
    printf("didn't read enough data\n");
    exit(-1);
  }
  printf("cksum: %s = cksum %lx tot %ld\n", argv[1], cksum, tot);
  return 0;
}
