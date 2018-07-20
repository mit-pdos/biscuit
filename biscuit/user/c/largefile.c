#include <stdio.h>
#include <errno.h>
#include <fcntl.h>
#include <stdlib.h>
#include <unistd.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/time.h>

/* Measure creating a large file and overwriting that file */

#define WSIZE (4096)
#define FILESIZE 50 * 1024 * 1024
#define NAMESIZE 100

static char name[NAMESIZE];
static char buf[WSIZE];
static char *prog;
static char *dir;

void printstats(int reset)
{
  int fd;
  int r;

  sprintf(name, "dev/stats");
  if((fd = open(name, O_RDONLY)) < 0) {
    return;
  }

  memset(buf, 0, WSIZE);

  if ((r = read(fd, buf, WSIZE)) < 0) {
    perror("read");
    exit(1);
  }

  if (!reset) fprintf(stdout, "=== FS Stats ===\n%s========\n", buf);

  if ((r = close(fd)) < 0) {
    perror("close");
  }
}

void makefile()
{
  int i;
  int fd;

  int n = FILESIZE/WSIZE;

  memset(buf, 'a', WSIZE);
  
  sprintf(name, "%s/d/f", dir);
  if((fd = open(name, O_RDWR | O_CREAT | O_TRUNC, S_IRWXU)) < 0) {
    printf("%s: create %s failed %s\n", prog, name, strerror(errno));
    exit(1);
  }

  for (i = 0; i < n; i++) {
    if (write(fd, buf, WSIZE) != WSIZE) {
      printf("%s: write %s failed %s\n", prog, name, strerror(errno));
      exit(1);
    }
  }

  if (fsync(fd) < 0) {
    printf("%s: fsync %s failed %s\n", prog, name, strerror(errno));
    exit(1);
  }


  fd = open(".", O_DIRECTORY | O_RDONLY);
  if (fd < 0) {
    perror("open dir");
    exit(-1);
  }
  if (fsync(fd) < 0) {
    perror("fsync");
    exit(-1);
  }
}

void writefile()
{
  int i;
  int fd;

  int n = FILESIZE/WSIZE;
  
  sprintf(name, "%s/d/f", dir);
  if((fd = open(name, O_RDWR, S_IRWXU)) < 0) {
    printf("%s: open %s failed %s\n", prog, name, strerror(errno));
    exit(1);
  }
  
  memset(buf, 'b', WSIZE);
    
  for (i = 0; i < n; i++) {
    if (write(fd, buf, WSIZE) != WSIZE) {
      printf("%s: write %s failed %s\n", prog, name, strerror(errno));
      exit(1);
    }
    if (((i + 1) * WSIZE) % (10 * 1024 * 1024) == 0) {
      if (fsync(fd) < 0) {
	  printf("%s: fsync %s failed %s\n", prog, name, strerror(errno));
	  exit(1);
	}
    }
  }
  if ((i * WSIZE) % (10 * 1024 * 1024) != 0 && fsync(fd) < 0) {
    printf("%s: fsync %s failed %s\n", prog, name, strerror(errno));
    exit(1);
  }
  close(fd);
}

void usage(char *name) {
    printf("Usage: %s basedir\n", name);
    exit(-1);
}

int main(int argc, char *argv[])
{
  long time;
  struct timeval before;
  struct timeval after;
  float tput;
  int make = 1;
  int write = 1;
  int ch;
  
  prog = argv[0];

  while ((ch = getopt(argc, argv, "mw")) != -1) {
    switch (ch) {
    case 'm':
      make = 0;
      break;
    case 'w':
      write = 0;
      break;
    default:
      usage(argv[0]);
      break;
    }
  }

  int rem = argc - optind;
  if (rem != 1) {
    usage(argv[0]);
  }
    
  dir = argv[optind];
  sprintf(name, "%s/d", dir);

    
  if (make) {
    if (mkdir(name,  S_IRWXU) < 0) {
      printf("%s: create %s failed %s\n", prog, name, strerror(errno));
      exit(1);
    }
    
    printstats(1);

    gettimeofday ( &before, NULL );  
    makefile();
    gettimeofday ( &after, NULL );

    time = (after.tv_sec - before.tv_sec) * 1000000 +
      (after.tv_usec - before.tv_usec);
    tput = ((float) (FILESIZE/1024) /  (time / 1000000.0));
    printf("makefile %d MB %ld usec throughput %f KB/s\n", FILESIZE/(1024*1024), time, tput);

    printstats(0);
  }

  if (write) {
    printstats(1);
    
    gettimeofday ( &before, NULL );
    writefile();
    gettimeofday ( &after, NULL );
  
    time = (after.tv_sec - before.tv_sec) * 1000000 +
      (after.tv_usec - before.tv_usec);
    tput = ((float) (FILESIZE/1024) /  (time / 1000000.0));
    printf("writefile %d MB %ld usec throughput %f KB/s\n", FILESIZE/(1024*1024), time, tput);

    printstats(0);
  }
  
  return 0;
}
