#include <stdio.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/time.h>
#include <fcntl.h>
#include <stdlib.h>
#include <unistd.h>
#include <errno.h>

static char buf[40960];
static char name[100];
static char *prog_name;
static char *topdir;

#define NDIR 100

static char dir[100];

time_t ds;
time_t du;
struct timeval before, end;

void rgettimeofday(struct timeval *h, void *p)
{
	time_t hi, lo, v;
	asm(
	    "rdtsc\n"
	    : "=a"(lo), "=d"(hi)
	    :
	    :);
	v = hi << 32 | lo;
	if (h)
		h->tv_usec = v;
}

void start()
{
    rgettimeofday(&before, NULL);
}


long stop()
{
    rgettimeofday(&end, NULL);
    long time = (end.tv_sec - before.tv_sec) * 1000000 +
	(end.tv_usec - before.tv_usec);
    return time;
}


void creat_dir()
{
    int i;

    //umask(0);

    for (i = 0; i < NDIR; i++) {
      snprintf(dir, sizeof(dir), "%s/d%d", topdir, i);
      if (mkdir(dir, 0777) != 0) {
	printf("%s: mkdir %s failed %d\n", prog_name, dir, errno);
      }
    }
}



void
creat_test(int n, int size)
{
  int i;
  int r;
  int fd;
  int j;

  start();
  for (i = 0, j = 0; i < n; i ++) {

    snprintf(name, sizeof(name), "%s/d%d/g%d", topdir, j, i);

    if((fd = open(name, O_RDWR | O_CREAT | O_TRUNC, S_IRWXU)) < 0) {
      printf("%s: create %s failed %d\n", prog_name, name, errno);
      exit(1);
    }

    if ((r = write(fd, buf, size)) < 0) {
      printf("%s: write failed %d %d\n", prog_name, r, errno);
      exit(1);
    }

    if ((r = close(fd)) < 0) {
      printf("%s: close failed %d %d\n", prog_name, r, errno);
    }

    if ((i+1) % 100 == 0) j++;

  }
  long time = stop();
  printf("%s: creat took %ld usec\n", prog_name, time);
}


void
write_test(char *name, int n, int size)
{
  int i;
  int r;
  int fd;

  if((fd = open(name, O_RDWR | O_CREAT | O_TRUNC, S_IRWXU)) < 0) {
    printf("%s: create %d failed %d\n", prog_name, fd, errno);
    exit(1);
  }

  for (i = 0; i < n; i ++) {
    if ((r = write(fd, buf, size)) < 0) {
      printf("%s: write failed %d %d\n", prog_name, r, errno);
      exit(1);
    }
  }
    
  if ((r = close(fd)) < 0) {
    printf("%s: close failed %d %d\n", prog_name, r, errno);
  }
}


void
flush_cache()
{
    write_test("t", 20000, 4096);
}

void
read_test(int n, int size)
{
  int i;
  int r;
  int fd;
  int j;

  start();
  for (i = 0, j = 0; i < n; i ++) {

    snprintf(name, sizeof(name), "%s/d%d/g%d", topdir, j, i);

    if((fd = open(name, O_RDONLY, 0)) < 0) {
      printf("%s: open %d failed %d %d\n", prog_name, i, fd, errno);
      exit(1);
    }

    if ((r = read(fd, buf, size)) < 0) {
      printf("%s: read failed %d %d\n", prog_name, r, errno);
      exit(1);
    }

    if ((r = close(fd)) < 0) {
      printf("%s: close failed %d %d\n", prog_name, r, errno);
    }

    if ((i+1) % 100 == 0) j++;
  }

  long time = stop();
  printf("%s: read took %ld usec\n", prog_name, time);
}

void 
delete_test(int n)
{	
  int i;
  int r;
  int j;
 
  start();
  for (i = 0, j = 0; i < n; i ++) {

    snprintf(name, sizeof(name), "%s/d%d/g%d", topdir, j, i);

    if ((r = unlink(name)) < 0) {
      printf("%s: unlink failed %d\n", prog_name, r);
      exit(1);
    }

    if ((i+1) % 100 == 0) j++;
  }

  long time = stop();
  printf("%s: unlink took %ld usec\n", prog_name, time);
}


int main(int argc, char *argv[])
{
  int n;
  int size;

  prog_name = argv[0];

  if (argc != 4) {
    printf("%s: %s num size dir\n", prog_name, prog_name);
    exit(1);
  }

  n = atoi(argv[1]);
  size = atoi(argv[2]);
  topdir = argv[3];

  printf("%s %d %d %s\n", prog_name, n, size, topdir);

  creat_dir();
  
  //printstats(topdir, 1);
  
  creat_test(n, size);
  
  //printstats(topdir, 0);

  read_test(n, size);
  delete_test(n);

  unlink("t");
  return 0;
}
