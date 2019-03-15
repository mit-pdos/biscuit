#include <stdio.h>
#include <fcntl.h>
#include <unistd.h>

#define BSIZE  4096
#define NADDR  (BSIZE/8)

char buf[2*BSIZE];
char name[3];
char *echoargv[] = { "echo", "ALL", "TESTS", "PASSED", 0 };

#define mkdir(x)	mkdir(x, 0)
#define open(x, y)	open(x, y, 0)
#define exec(x, y)	execv(x, y)
#define kill(x)		kill(x, SIGKILL)


#define O_CREATE	O_CREAT
#define MAXFILE		NADDR*NADDR

// does chdir() call iput(p->cwd) in a transaction?
void
iputtest(void)
{
  printf("iput test\n");

  if(mkdir("iputdir") < 0){
    printf("mkdir failed\n");
    exit(0);
  }
  if(chdir("iputdir") < 0){
    printf("chdir iputdir failed\n");
    exit(0);
  }
  if(rmdir("../iputdir") < 0){
    printf("rmdir ../iputdir failed\n");
    exit(0);
  }
  if(chdir("/") < 0){
    printf("chdir / failed\n");
    exit(0);
  }
  printf("iput test ok\n");
}

// does exit(0) call iput(p->cwd) in a transaction?
void
exitiputtest(void)
{
  int pid;

  printf("exitiput test\n");

  pid = fork();
  if(pid < 0){
    printf("fork failed\n");
    exit(0);
  }
  if(pid == 0){
    if(mkdir("iputdir") < 0){
      printf("mkdir failed\n");
      exit(0);
    }
    if(chdir("iputdir") < 0){
      printf("child chdir failed\n");
      exit(0);
    }
    if(rmdir("../iputdir") < 0){
      printf("rmdir ../iputdir failed\n");
      exit(0);
    }
    exit(0);
  }
  wait(NULL);
  printf("exitiput test ok\n");
}

// does the error path in open() for attempt to write a
// directory call iput() in a transaction?
// needs a hacked kernel that pauses just after the namei()
// call in sys_open():
//    if((ip = namei(path)) == 0)
//      return -1;
//    {
//      int i;
//      for(i = 0; i < 10000; i++)
//        yield();
//    }
void
openiputtest(void)
{
  int pid;

  printf("openiput test\n");
  if(mkdir("oidir") < 0){
    printf("mkdir oidir failed\n");
    exit(0);
  }
  pid = fork();
  if(pid < 0){
    printf("fork failed\n");
    exit(0);
  }
  if(pid == 0){
    int fd = open("oidir", O_RDWR);
    if(fd >= 0){
      printf("open directory for write succeeded\n");
      exit(0);
    }
    exit(0);
  }
  sleep(1);
  if(rmdir("oidir") != 0){
    printf("unlink failed\n");
    exit(0);
  }
  wait(NULL);
  printf("openiput test ok\n");
}

// simple file system tests

void
opentest(void)
{
  int fd;

  printf("open test\n");
  fd = open("/bin/echo", 0);
  if(fd < 0){
    printf("open echo failed!\n");
    exit(0);
  }
  close(fd);
  fd = open("doesnotexist", 0);
  if(fd >= 0){
    printf("open doesnotexist succeeded!\n");
    exit(0);
  }
  printf("open test ok\n");
}

void
writetest(void)
{
  int fd;
  int i;

  printf("small file test\n");
  fd = open("small", O_CREATE|O_RDWR);
  if(fd >= 0){
    printf("creat small succeeded; ok\n");
  } else {
    printf("error: creat small failed!\n");
    exit(0);
  }
  for(i = 0; i < 100; i++){
    if(write(fd, "aaaaaaaaaa", 10) != 10){
      printf("error: write aa %d new file failed\n", i);
      exit(0);
    }
    if(write(fd, "bbbbbbbbbb", 10) != 10){
      printf("error: write bb %d new file failed\n", i);
      exit(0);
    }
  }
  printf("writes ok\n");
  close(fd);
  fd = open("small", O_RDONLY);
  if(fd >= 0){
    printf("open small succeeded ok\n");
  } else {
    printf("error: open small failed!\n");
    exit(0);
  }
  i = read(fd, buf, 2000);
  if(i == 2000){
    printf("read succeeded ok\n");
  } else {
    printf("read failed\n");
    exit(0);
  }
  close(fd);

  if(unlink("small") < 0){
    printf("unlink small failed\n");
    exit(0);
  }
  printf("small file test ok\n");
}

void
writetest1(void)
{
  int i, fd, n;

  printf("big files test\n");

  fd = open("big", O_CREATE|O_RDWR);
  if(fd < 0){
    printf("error: creat big failed!\n");
    exit(0);
  }

  for(i = 0; i < MAXFILE; i++){
    ((int*)buf)[0] = i;
    if(write(fd, buf, BSIZE) != BSIZE){
      printf("error: write big file failed\n");
      exit(0);
    }
  }

  close(fd);

  fd = open("big", O_RDONLY);
  if(fd < 0){
    printf("error: open big failed!\n");
    exit(0);
  }

  n = 0;
  for(;;){
    i = read(fd, buf, BSIZE);
    if(i == 0){
      if(n == MAXFILE - 1){
        printf("read only %d blocks from big", n);
        exit(0);
      }
      break;
    } else if(i != BSIZE){
      printf("read failed %d\n", i);
      exit(0);
    }
    if(((int*)buf)[0] != n){
      printf("read content of block %d is %d\n",
             n, ((int*)buf)[0]);
      exit(0);
    }
    n++;
  }
  close(fd);
  if(unlink("big") < 0){
    printf("unlink big failed\n");
    exit(0);
  }
  printf("big files ok\n");
}

void
createtest(void)
{
  int i, fd;

  printf("many creates, followed by unlink test\n");

  name[0] = 'a';
  name[2] = '\0';
  for(i = 0; i < 52; i++){
    name[1] = '0' + i;
    fd = open(name, O_CREATE|O_RDWR);
    close(fd);
  }
  name[0] = 'a';
  name[2] = '\0';
  for(i = 0; i < 52; i++){
    name[1] = '0' + i;
    unlink(name);
  }
  printf("many creates, followed by unlink; ok\n");
}

void dirtest(void)
{
  printf("mkdir test\n");

  if(mkdir("dir0") < 0){
    printf("mkdir failed\n");
    exit(0);
  }

  if(chdir("dir0") < 0){
    printf("chdir dir0 failed\n");
    exit(0);
  }

  if(chdir("..") < 0){
    printf("chdir .. failed\n");
    exit(0);
  }

  if(rmdir("dir0") < 0){
    printf("rmdir dir0 failed\n");
    exit(0);
  }
  if (mkdir("/") != -1)
    errx(-1, "expected failure");
  printf("mkdir test ok\n");
}

void
exectest(void)
{
  printf("exec test\n");
  if(exec("/bin/echo", echoargv) < 0){
    printf("exec echo failed\n");
    exit(0);
  }
}

// simple fork and pipe read/write

void
pipe1(void)
{
  if (1033 > PIPE_BUF)
    errx(-1, "need to write 1033 bytes atomically");
  int fds[2], pid;
  int seq, i, n, cc, total;
  printf("pipe1 test\n");

  if(pipe(fds) != 0){
    printf("pipe() failed\n");
    exit(0);
  }
  pid = fork();
  seq = 0;
  if(pid == 0){
    close(fds[0]);
    for(n = 0; n < 5; n++){
      for(i = 0; i < 1033; i++)
        buf[i] = seq++;
      if(write(fds[1], buf, 1033) != 1033){
        errx(-1, "pipe1 oops 1\n");
      }
    }
    exit(0);
  } else if(pid > 0){
    close(fds[1]);
    total = 0;
    cc = 1;
    while((n = read(fds[0], buf, cc)) > 0){
      for(i = 0; i < n; i++){
        if((buf[i] & 0xff) != (seq++ & 0xff)){
          errx(-1, "pipe1 oops 2\n");
        }
      }
      total += n;
      cc = cc * 2;
      if(cc > sizeof(buf))
        cc = sizeof(buf);
    }
    if(total != 5 * 1033){
      printf("pipe1 oops 3 total %d\n", total);
      exit(0);
    }
    close(fds[0]);
    wait(NULL);
  } else {
    err(-1, "fork() failed\n");
  }
  printf("pipe1 ok\n");
}

// meant to be run w/ at most two CPUs
void
preempt(void)
{
  int pid1, pid2, pid3;
  int pfds[2];

  printf("preempt: ");
  pid1 = fork();
  if(pid1 == 0)
    for(;;)
      ;

  pid2 = fork();
  if(pid2 == 0)
    for(;;)
      ;

  pipe(pfds);
  pid3 = fork();
  if(pid3 == 0){
    close(pfds[0]);
    if(write(pfds[1], "x", 1) != 1)
      printf("preempt write error");
    close(pfds[1]);
    for(;;)
      ;
  }

  close(pfds[1]);
  if(read(pfds[0], buf, sizeof(buf)) != 1){
    printf("preempt read error");
    return;
  }
  close(pfds[0]);
  printf("kill... ");
  kill(pid1);
  kill(pid2);
  kill(pid3);
  printf("wait... ");
  wait(NULL);
  wait(NULL);
  wait(NULL);
  printf("preempt ok\n");
}

void _childfault(void)
{
  char *p = NULL;
  *p = 'a';
  errx(-1, "no fault");
}

void stchk(int status, int expsig)
{
  if (expsig) {
    if (WIFCONTINUED(status))
      errx(-1, "unexpected continued");
    if (WIFEXITED(status))
      errx(-1, "unexpected exited");
    if (!WIFSIGNALED(status))
      errx(-1, "expected signaled");
    if (WTERMSIG(status) != expsig)
      errx(-1, "signal mismatch");
  } else {
    if (WIFCONTINUED(status))
      errx(-1, "unexpected continued");
    if (!WIFEXITED(status))
      errx(-1, "expected exited");
    if (WIFSIGNALED(status))
      errx(-1, "unexpected signaled");
    if (WTERMSIG(status) != 0)
      errx(-1, "signal mismatch");
  }
}

// try to find any races between exit and wait
void
exitwait(void)
{
  int i, pid;

  for(i = 0; i < 100; i++){
    pid = fork();
    if(pid < 0){
      printf("fork failed\n");
      return;
    }
    if(pid){
      if(wait(NULL) != pid){
        printf("wait wrong pid\n");
        return;
      }
    } else {
      exit(0);
    }
  }
  printf("exitwait ok\n");

  printf("exit status test\n");
  if (!fork())
    _childfault();
  int status;
  wait(&status);
  stchk(status, SIGSEGV);
  if (!fork())
    exit(0);
  wait(&status);
  stchk(status, 0);
  printf("exit status test ok\n");
}

void
mem(void)
{
  void *m1, *m2;
  int pid, ppid;

  printf("mem test\n");
  ppid = getpid();
  if((pid = fork()) == 0){
    m1 = 0;
    while((m2 = malloc(10001)) != 0){
      *(char**)m2 = m1;
      m1 = m2;
    }
    while(m1){
      m2 = *(char**)m1;
      free(m1);
      m1 = m2;
    }
    m1 = malloc(1024*20);
    if(m1 == 0){
      printf("couldn't allocate mem?!!\n");
      kill(ppid);
      exit(0);
    }
    free(m1);
    printf("mem ok\n");
    exit(0);
  } else {
    wait(NULL);
  }
}

// More file system tests

// two processes write to the same file descriptor
// is the offset shared? does inode locking work?
void
sharedfd(void)
{
  int fd, pid, i, n, nc, np;
  char buf[10];

  printf("sharedfd test\n");

  fd = open("sharedfd", O_CREATE|O_RDWR);
  if(fd < 0)
    err(fd, "fstests: cannot open sharedfd for writing");
  pid = fork();
  memset(buf, pid==0?'c':'p', sizeof(buf));
  for(i = 0; i < 1000; i++){
    if(write(fd, buf, sizeof(buf)) != sizeof(buf))
      errx(-1, "fstests: write sharedfd failed\n");
  }
  if(pid == 0)
    exit(0);
  else
    wait(NULL);
  close(fd);
  fd = open("sharedfd", 0);
  if(fd < 0)
    err(fd, "fstests: cannot open sharedfd for reading\n");
  nc = np = 0;
  while((n = read(fd, buf, sizeof(buf))) > 0){
    for(i = 0; i < sizeof(buf); i++){
      if(buf[i] == 'c')
        nc++;
      if(buf[i] == 'p')
        np++;
    }
  }
  close(fd);
  unlink("sharedfd");
  if(nc == 10000 && np == 10000){
    printf("sharedfd ok\n");
  } else {
    printf("sharedfd oops %d %d\n", nc, np);
    exit(0);
  }
}

// four processes write different files at the same
// time, to test block allocation.
void
fourfiles(void)
{
  int fd, pid, i, j, n, total, pi;
  char *names[] = { "f0", "f1", "f2", "f3" };
  char *fname;

  printf("fourfiles test\n");

  for(pi = 0; pi < 4; pi++){
    fname = names[pi];
    unlink(fname);
      
    pid = fork();
    if(pid < 0){
      printf("fork failed\n");
      exit(0);
    }

    if(pid == 0){
      fd = open(fname, O_CREATE | O_RDWR);
      if(fd < 0){
        printf("create failed\n");
        exit(0);
      }
      
      memset(buf, '0'+pi, BSIZE);
      for(i = 0; i < 12; i++){
        if((n = write(fd, buf, BSIZE)) != BSIZE){
          printf("write failed %d\n", n);
          exit(0);
        }
      }
      exit(0);
    }
  }

  for(pi = 0; pi < 4; pi++){
    wait(NULL);
  }

  for(i = 0; i < 4; i++){
    fname = names[i];
    fd = open(fname, 0);
    total = 0;
    while((n = read(fd, buf, sizeof(buf))) > 0){
      for(j = 0; j < n; j++){
        if(buf[j] != '0'+i){
          printf("wrong char\n");
          exit(0);
        }
      }
      total += n;
    }
    close(fd);
    if(total != 12*BSIZE){
      printf("wrong length %d\n", total);
      exit(0);
    }
    unlink(fname);
  }

  printf("fourfiles ok\n");
}

// four processes create and delete different files in same directory
void
createdelete(void)
{
  enum { N = 20 };
  int pid, i, fd, pi;
  char name[32];

  printf("createdelete test\n");

  for(pi = 0; pi < 4; pi++){
    pid = fork();
    if(pid < 0){
      printf("fork failed\n");
      exit(0);
    }

    if(pid == 0){
      name[0] = 'p' + pi;
      name[2] = '\0';
      for(i = 0; i < N; i++){
        name[1] = '0' + i;
        fd = open(name, O_CREATE | O_RDWR);
        if(fd < 0){
          printf("create failed\n");
          exit(0);
        }
        close(fd);
        if(i > 0 && (i % 2 ) == 0){
          name[1] = '0' + (i / 2);
          if(unlink(name) < 0){
            printf("unlink failed\n");
            exit(0);
          }
        }
      }
      exit(0);
    }
  }

  for(pi = 0; pi < 4; pi++){
    wait(NULL);
  }

  name[0] = name[1] = name[2] = 0;
  for(i = 0; i < N; i++){
    for(pi = 0; pi < 4; pi++){
      name[0] = 'p' + pi;
      name[1] = '0' + i;
      fd = open(name, 0);
      if((i == 0 || i >= N/2) && fd < 0){
        printf("oops createdelete %s didn't exist\n", name);
        exit(0);
      } else if((i >= 1 && i < N/2) && fd >= 0){
        printf("oops createdelete %s did exist\n", name);
        exit(0);
      }
      if(fd >= 0)
        close(fd);
    }
  }

  for(i = 0; i < N; i++){
    for(pi = 0; pi < 4; pi++){
      name[0] = 'p' + i;
      name[1] = '0' + i;
      unlink(name);
    }
  }

  printf("createdelete ok\n");
}

// can I unlink a file and still read it?
void
unlinkread(void)
{
  int fd, fd1;

  printf("unlinkread test\n");
  fd = open("unlinkread", O_CREATE | O_RDWR);
  if(fd < 0){
    printf("create unlinkread failed\n");
    exit(0);
  }
  write(fd, "hello", 5);
  close(fd);

  fd = open("unlinkread", O_RDWR);
  if(fd < 0){
    printf("open unlinkread failed\n");
    exit(0);
  }
  if(unlink("unlinkread") != 0){
    printf("unlink unlinkread failed\n");
    exit(0);
  }

  fd1 = open("unlinkread", O_CREATE | O_RDWR);
  write(fd1, "yyy", 3);
  close(fd1);

  if(read(fd, buf, sizeof(buf)) != 5){
    printf("unlinkread read failed");
    exit(0);
  }
  if(buf[0] != 'h'){
    printf("unlinkread wrong data\n");
    exit(0);
  }
  if(write(fd, buf, 10) != 10){
    printf("unlinkread write failed\n");
    exit(0);
  }
  close(fd);
  unlink("unlinkread");
  printf("unlinkread ok\n");
}

void
linktest(void)
{
  int fd;

  printf("linktest\n");

  unlink("lf1");
  unlink("lf2");

  fd = open("lf1", O_CREATE|O_RDWR);
  if(fd < 0){
    printf("create lf1 failed\n");
    exit(0);
  }
  if(write(fd, "hello", 5) != 5){
    printf("write lf1 failed\n");
    exit(0);
  }
  close(fd);

  if(link("lf1", "lf2") < 0){
    printf("link lf1 lf2 failed\n");
    exit(0);
  }
  unlink("lf1");

  if(open("lf1", 0) >= 0){
    printf("unlinked lf1 but it is still there!\n");
    exit(0);
  }

  fd = open("lf2", 0);
  if(fd < 0){
    printf("open lf2 failed\n");
    exit(0);
  }
  if(read(fd, buf, sizeof(buf)) != 5){
    printf("read lf2 failed\n");
    exit(0);
  }
  close(fd);

  if(link("lf2", "lf2") >= 0){
    printf("link lf2 lf2 succeeded! oops\n");
    exit(0);
  }

  unlink("lf2");
  if(link("lf2", "lf1") >= 0){
    printf("link non-existant succeeded! oops\n");
    exit(0);
  }

  if(link(".", "lf1") >= 0){
    printf("link . lf1 succeeded! oops\n");
    exit(0);
  }

  printf("linktest ok\n");
}

struct  __attribute__((packed)) dirent_t {
#define NMAX	14
       char	name[NMAX];
       ulong	inum;
};

struct dirdata_t {
#define NDIRENTS	(BSIZE/sizeof(struct dirent_t))
       struct dirent_t de[NDIRENTS];
};

// test concurrent create/link/unlink of the same file
void
concreate(void)
{
  char file[3];
  int i, pid, n, fd;
  char fa[40];
  char buf[BSIZE];

  printf("concreate test\n");
  file[0] = 'C';
  file[2] = '\0';
  for(i = 0; i < 40; i++){
    file[1] = '0' + i;
    unlink(file);
    pid = fork();
    if(pid && (i % 3) == 1){
      link("C0", file);
    } else if(pid == 0 && (i % 5) == 1){
      link("C0", file);
    } else {
      fd = open(file, O_CREATE | O_RDWR);
      if(fd < 0){
        printf("concreate create %s failed\n", file);
        exit(0);
      }
      close(fd);
    }
    if(pid == 0)
      exit(0);
    else
      wait(NULL);
  }

  memset(fa, 0, sizeof(fa));
  fd = open(".", 0);
  if (fd < 0)
	err(fd, "open");
  n = 0;
  while(read(fd, buf, sizeof(buf)) == sizeof(buf)){
    struct dirdata_t *dd = (struct dirdata_t *)buf;
    int j;
    for (j = 0; j < NDIRENTS; j++) {
      if(dd->de[j].inum == 0)
        continue;
      if(dd->de[j].name[0] == 'C' && dd->de[j].name[2] == '\0'){
        i = dd->de[j].name[1] - '0';
        if(i < 0 || i >= sizeof(fa)){
          printf("concreate weird file %s\n", dd->de[j].name);
          exit(0);
        }
        if(fa[i]){
          printf("concreate duplicate file %s\n", dd->de[j].name);
          exit(0);
        }
        fa[i] = 1;
        n++;
      }
    }
  }
  close(fd);

  if(n != 40){
    printf("concreate not enough files in directory listing (%d)\n", n);
    exit(0);
  }

  for(i = 0; i < 40; i++){
    file[1] = '0' + i;
    pid = fork();
    if(pid < 0){
      printf("fork failed\n");
      exit(0);
    }
    if(((i % 3) == 0 && pid == 0) ||
       ((i % 3) == 1 && pid != 0)){
      close(open(file, 0));
      close(open(file, 0));
      close(open(file, 0));
      close(open(file, 0));
    } else {
      unlink(file);
      unlink(file);
      unlink(file);
      unlink(file);
    }
    if(pid == 0)
      exit(0);
    else
      wait(NULL);
  }

  printf("concreate ok\n");
}

// another concurrent link/unlink/create test,
// to look for deadlocks.
void
linkunlink()
{
  int pid, i;

  printf("linkunlink test\n");

  unlink("x");
  pid = fork();
  if(pid < 0){
    printf("fork failed\n");
    exit(0);
  }

  unsigned int x = (pid ? 1 : 97);
  for(i = 0; i < 1000; i++){
    x = x * 1103515245 + 12345;
    if((x % 3) == 0){
      close(open("x", O_RDWR | O_CREATE));
    } else if((x % 3) == 1){
      link("/bin/cat", "x");
    } else {
      unlink("x");
    }
  }

  if(pid)
    wait(NULL);
  else 
    exit(0);

  printf("linkunlink ok\n");
}

// directory that uses indirect blocks
void
bigdir(void)
{
  int i, fd;
  char name[10];

  printf("bigdir test\n");
  unlink("bd");

  fd = open("bd", O_CREATE);
  if(fd < 0){
    printf("bigdir create failed\n");
    exit(0);
  }
  close(fd);

  for(i = 0; i < 500; i++){
    name[0] = 'x';
    name[1] = '0' + (i / 64);
    name[2] = '0' + (i % 64);
    name[3] = '\0';
    if(link("bd", name) != 0){
      printf("bigdir link failed\n");
      exit(0);
    }
  }

  unlink("bd");
  for(i = 0; i < 500; i++){
    name[0] = 'x';
    name[1] = '0' + (i / 64);
    name[2] = '0' + (i % 64);
    name[3] = '\0';
    if(unlink(name) != 0){
      printf("bigdir unlink failed");
      exit(0);
    }
  }

  printf("bigdir ok\n");
}

void
subdir(void)
{
  int fd, cc;

  printf("subdir test\n");

  rmdir("ff");
  if(mkdir("dd") != 0){
    printf("subdir mkdir dd failed\n");
    exit(0);
  }

  fd = open("dd/ff", O_CREATE | O_RDWR);
  if(fd < 0){
    printf("create dd/ff failed\n");
    exit(0);
  }
  write(fd, "ff", 2);
  close(fd);
  
  if(rmdir("dd") >= 0){
    printf("unlink dd (non-empty dir) succeeded!\n");
    exit(0);
  }

  if(mkdir("/dd/dd") != 0){
    printf("subdir mkdir dd/dd failed\n");
    exit(0);
  }

  fd = open("dd/dd/ff", O_CREATE | O_RDWR);
  if(fd < 0){
    printf("create dd/dd/ff failed\n");
    exit(0);
  }
  write(fd, "FF", 2);
  close(fd);

  fd = open("dd/dd/../ff", 0);
  if(fd < 0){
    printf("open dd/dd/../ff failed\n");
    exit(0);
  }
  cc = read(fd, buf, sizeof(buf));
  if(cc != 2 || buf[0] != 'f'){
    printf("dd/dd/../ff wrong content\n");
    exit(0);
  }
  close(fd);

  if(link("dd/dd/ff", "dd/dd/ffff") != 0){
    printf("link dd/dd/ff dd/dd/ffff failed\n");
    exit(0);
  }

  if(unlink("dd/dd/ff") != 0){
    printf("unlink dd/dd/ff failed\n");
    exit(0);
  }
  if(open("dd/dd/ff", O_RDONLY) >= 0){
    printf("open (unlinked) dd/dd/ff succeeded\n");
    exit(0);
  }

  if(chdir("dd") != 0){
    printf("chdir dd failed\n");
    exit(0);
  }
  if(chdir("dd/../../dd") != 0){
    printf("chdir dd/../../dd failed\n");
    exit(0);
  }
  if(chdir("dd/../../../dd") != 0){
    printf("chdir dd/../../dd failed\n");
    exit(0);
  }
  if(chdir("./..") != 0){
    printf("chdir ./.. failed\n");
    exit(0);
  }

  fd = open("dd/dd/ffff", 0);
  if(fd < 0){
    printf("open dd/dd/ffff failed\n");
    exit(0);
  }
  if(read(fd, buf, sizeof(buf)) != 2){
    printf("read dd/dd/ffff wrong len\n");
    exit(0);
  }
  close(fd);

  if(open("dd/dd/ff", O_RDONLY) >= 0){
    printf("open (unlinked) dd/dd/ff succeeded!\n");
    exit(0);
  }

  if(open("dd/ff/ff", O_CREATE|O_RDWR) >= 0){
    printf("create dd/ff/ff succeeded!\n");
    exit(0);
  }
  if(open("dd/xx/ff", O_CREATE|O_RDWR) >= 0){
    printf("create dd/xx/ff succeeded!\n");
    exit(0);
  }
  if(open("dd", O_CREATE) >= 0){
    printf("create dd succeeded!\n");
    exit(0);
  }
  if(open("dd", O_RDWR) >= 0){
    printf("open dd rdwr succeeded!\n");
    exit(0);
  }
  if(open("dd", O_WRONLY) >= 0){
    printf("open dd wronly succeeded!\n");
    exit(0);
  }
  if(link("dd/ff/ff", "dd/dd/xx") == 0){
    printf("link dd/ff/ff dd/dd/xx succeeded!\n");
    exit(0);
  }
  if(link("dd/xx/ff", "dd/dd/xx") == 0){
    printf("link dd/xx/ff dd/dd/xx succeeded!\n");
    exit(0);
  }
  if(link("dd/ff", "dd/dd/ffff") == 0){
    printf("link dd/ff dd/dd/ffff succeeded!\n");
    exit(0);
  }
  if(mkdir("dd/ff/ff") == 0){
    printf("mkdir dd/ff/ff succeeded!\n");
    exit(0);
  }
  if(mkdir("dd/xx/ff") == 0){
    printf("mkdir dd/xx/ff succeeded!\n");
    exit(0);
  }
  if(mkdir("dd/dd/ffff") == 0){
    printf("mkdir dd/dd/ffff succeeded!\n");
    exit(0);
  }
  if(rmdir("dd/xx/ff") == 0){
    printf("rmdir dd/xx/ff succeeded!\n");
    exit(0);
  }
  if(rmdir("dd/ff/ff") == 0){
    printf("rmdir dd/ff/ff succeeded!\n");
    exit(0);
  }
  if(chdir("dd/ff") == 0){
    printf("chdir dd/ff succeeded!\n");
    exit(0);
  }
  if(chdir("dd/xx") == 0){
    printf("chdir dd/xx succeeded!\n");
    exit(0);
  }

  if(unlink("dd/dd/ffff") != 0){
    printf("unlink dd/dd/ff failed\n");
    exit(0);
  }
  if(unlink("dd/ff") != 0){
    printf("unlink dd/ff failed\n");
    exit(0);
  }
  if(rmdir("dd") == 0){
    printf("rmdir non-empty dd succeeded!\n");
    exit(0);
  }
  if(rmdir("dd/dd") < 0){
    printf("rmdir dd/dd failed\n");
    exit(0);
  }
  if(rmdir("dd") < 0){
    printf("rmdir dd failed\n");
    exit(0);
  }

  printf("subdir ok\n");
}

// test writes that are larger than the log.
void
bigwrite(void)
{
  int fd, sz;

  printf("bigwrite test\n");

  unlink("bigwrite");
  for(sz = 499; sz < 12*BSIZE; sz += 471){
    fd = open("bigwrite", O_CREATE | O_RDWR);
    if(fd < 0){
      printf("cannot create bigwrite\n");
      exit(0);
    }
    int i;
    char *b = malloc(sz);
    if (b == NULL) {
      printf("malloc failed\n");
      exit(0);
    }
    for(i = 0; i < 2; i++){
      int cc = write(fd, b, sz);
      if(cc != sz){
        printf("write(%d) ret %d\n", sz, cc);
        exit(0);
      }
    }
    free(b);
    close(fd);
    unlink("bigwrite");
  }

  printf("bigwrite ok\n");
}

void
bigfile(void)
{
  int fd, i, total, cc;
  #define NBLOCK 5000
  #define SZ 8000

  printf("bigfile test\n");

  unlink("bigfile");
  fd = open("bigfile", O_CREATE | O_RDWR);
  if(fd < 0){
    printf("cannot create bigfile");
    exit(0);
  }
  for(i = 0; i < NBLOCK; i++){
    memset(buf, i%10, SZ);
    if(write(fd, buf, SZ) != SZ){
      printf("write bigfile failed\n");
      exit(0);
    }
  }
  fsync(fd);
  close(fd);

  
  fd = open("bigfile", 0);
  if(fd < 0){
    printf("cannot open bigfile\n");
    exit(0);
  }
  total = 0;
  for(i = 0; ; i++){
    cc = read(fd, buf, SZ/2);
    if(cc < 0){
      printf("read bigfile failed\n");
      exit(0);
    }
    if(cc == 0)
      break;
    if(cc != SZ/2){
      printf("short read bigfile\n");
      exit(0);
    }
    if(buf[0] != (i/2)%10 || buf[SZ/2-1] != (i/2)%10){
      printf("read bigfile wrong data %d %c %c\n", i, buf[0], buf[SZ/2-1]);
      exit(0);
    }
    total += cc;
  }
  close(fd);
  if(total != NBLOCK*SZ){
    printf("read bigfile wrong total\n");
    exit(0);
  }
  unlink("bigfile");

  printf("bigfile test ok\n");
}

void
fourteen(void)
{
  int fd;

  printf("not checking for filename truncation...\n");
  // DIRSIZ is 14.
  printf("fourteen test\n");

  if(mkdir("12345678901234") != 0){
    printf("mkdir 12345678901234 failed\n");
    exit(0);
  }
  //if(mkdir("12345678901234/123456789012345") != 0){
  if(mkdir("12345678901234/12345678901234") != 0){
    //printf("mkdir 12345678901234/123456789012345 failed\n");
    printf("mkdir 12345678901234/12345678901234 failed\n");
    exit(0);
  }
  //fd = open("123456789012345/123456789012345/123456789012345", O_CREATE);
  fd = open("12345678901234/12345678901234/12345678901234", O_CREATE);
  if(fd < 0){
    //printf("create 123456789012345/123456789012345/123456789012345 failed\n");
    printf("create 12345678901234/12345678901234/12345678901234 failed\n");
    exit(0);
  }
  close(fd);
  fd = open("12345678901234/12345678901234/12345678901234", 0);
  if(fd < 0){
    printf("open 12345678901234/12345678901234/12345678901234 failed\n");
    exit(0);
  }
  close(fd);

  if(mkdir("12345678901234/12345678901234") == 0){
    printf("mkdir 12345678901234/12345678901234 succeeded!\n");
    exit(0);
  }
  if(mkdir("123456789012345/12345678901234") == 0){
    printf("mkdir 12345678901234/123456789012345 succeeded!\n");
    exit(0);
  }

  printf("fourteen ok\n");
}

void
rmdot(void)
{
  printf("rmdot test\n");
  if(mkdir("dots") != 0){
    printf("mkdir dots failed\n");
    exit(0);
  }
  if(chdir("dots") != 0){
    printf("chdir dots failed\n");
    exit(0);
  }
  if(rmdir(".") == 0){
    printf("rm . worked!\n");
    exit(0);
  }
  if(rmdir("..") == 0){
    printf("rm .. worked!\n");
    exit(0);
  }
  if(chdir("/") != 0){
    printf("chdir / failed\n");
    exit(0);
  }
  if(rmdir("dots/.") == 0){
    printf("rmdir dots/. worked!\n");
    exit(0);
  }
  if(rmdir("dots/..") == 0){
    printf("rmdir dots/.. worked!\n");
    exit(0);
  }
  if(rmdir("dots") != 0){
    printf("rmdir dots failed!\n");
    exit(0);
  }
  printf("rmdot ok\n");
}

void
dirfile(void)
{
  int fd;

  printf("dir vs file\n");

  fd = open("dirfile", O_CREATE);
  if(fd < 0){
    printf("create dirfile failed\n");
    exit(0);
  }
  close(fd);
  if(chdir("dirfile") == 0){
    printf("chdir dirfile succeeded!\n");
    exit(0);
  }
  fd = open("dirfile/xx", 0);
  if(fd >= 0){
    printf("create dirfile/xx succeeded!\n");
    exit(0);
  }
  fd = open("dirfile/xx", O_CREATE);
  if(fd >= 0){
    printf("create dirfile/xx succeeded!\n");
    exit(0);
  }
  if(mkdir("dirfile/xx") == 0){
    printf("mkdir dirfile/xx succeeded!\n");
    exit(0);
  }
  if(unlink("dirfile/xx") == 0){
    printf("unlink dirfile/xx succeeded!\n");
    exit(0);
  }
  if(link("README", "dirfile/xx") == 0){
    printf("link to dirfile/xx succeeded!\n");
    exit(0);
  }
  if(unlink("dirfile") != 0){
    printf("unlink dirfile failed!\n");
    exit(0);
  }

  fd = open(".", O_RDWR);
  if(fd >= 0){
    printf("open . for writing succeeded!\n");
    exit(0);
  }
  fd = open(".", 0);
  if(write(fd, "x", 1) > 0){
    printf("write . succeeded!\n");
    exit(0);
  }
  close(fd);

  printf("dir vs file OK\n");
}

// test that iput() is called at the end of _namei()
void
iref(void)
{
  int i, fd;

  printf("empty file name\n");

  // the 50 is NINODE
  for(i = 0; i < 50 + 1; i++){
    if(mkdir("irefd") != 0){
      printf("mkdir irefd failed\n");
      exit(0);
    }
    if(chdir("irefd") != 0){
      printf("chdir irefd failed\n");
      exit(0);
    }

    mkdir("");
    link("README", "");
    fd = open("", O_CREATE);
    if(fd >= 0)
      close(fd);
    fd = open("xx", O_CREATE);
    if(fd >= 0)
      close(fd);
    unlink("xx");
  }

  chdir("/");
  printf("empty file name OK\n");
}

void *
_ptexit(void *a)
{
	return NULL;
}

void
forktest(void)
{
  int n, pid;
  // XXX use getrlimit
  int ulim = 1025;

  printf("fork test\n");

  for(n=0; n<ulim; n++){
    pid = fork();
    if(pid < 0)
      break;
    if(pid == 0)
      exit(0);
  }
  
  if(n == ulim){
    printf("fork claimed to work %d times!\n", ulim);
    exit(0);
  }

  for(; n > 0; n--){
    if(wait(NULL) < 0){
      printf("wait stopped early\n");
      exit(0);
    }
  }

  if(wait(NULL) != -1 || errno != ECHILD){
    printf("wait got too many\n");
    exit(0);
  }

  pthread_t t[ulim];
  for(n=0; n<ulim; n++) {
    if (pthread_create(&t[n], NULL, _ptexit, NULL))
      break;
  }
  
  if(n == ulim)
    errx(-1, "pthread_create claimed to work %d times!\n", ulim);

  for(; n > 0; n--){
    void *st;
    if(pthread_join(t[n - 1], &st))
      errx(-1, "too few joins");
    if (st != NULL)
      errx(-1, "bad status");
  }

  printf("fork test OK\n");
}

//void
//sbrktest(void)
//{
//  int fds[2], pid, pids[10], ppid;
//  char *a, *b, *c, *lastaddr, *oldbrk, *p, scratch;
//  uint amt;
//
//  printf("sbrk test\n");
//  oldbrk = sbrk(0);
//
//  // can one sbrk() less than a page?
//  a = sbrk(0);
//  int i;
//  for(i = 0; i < 5000; i++){ 
//    b = sbrk(1);
//    if(b != a){
//      printf("sbrk test failed %d %x %x\n", i, a, b);
//      exit(0);
//    }
//    *b = 1;
//    a = b + 1;
//  }
//  pid = fork();
//  if(pid < 0){
//    printf("sbrk test fork failed\n");
//    exit(0);
//  }
//  c = sbrk(1);
//  c = sbrk(1);
//  if(c != a + 1){
//    printf("sbrk test failed post-fork\n");
//    exit(0);
//  }
//  if(pid == 0)
//    exit(0);
//  wait(NULL);
//
//  // can one grow address space to something big?
//#define BIG (100*1024*1024)
//  a = sbrk(0);
//  amt = (BIG) - (uint)a;
//  p = sbrk(amt);
//  if (p != a) { 
//    printf("sbrk test failed to grow big address space; enough phys mem?\n");
//    exit(0);
//  }
//  lastaddr = (char*) (BIG-1);
//  *lastaddr = 99;
//
//  // can one de-allocate?
//  a = sbrk(0);
//  c = sbrk(-4096);
//  if(c == (char*)0xffffffff){
//    printf("sbrk could not deallocate\n");
//    exit(0);
//  }
//  c = sbrk(0);
//  if(c != a - 4096){
//    printf("sbrk deallocation produced wrong address, a %x c %x\n", a, c);
//    exit(0);
//  }
//
//  // can one re-allocate that page?
//  a = sbrk(0);
//  c = sbrk(4096);
//  if(c != a || sbrk(0) != a + 4096){
//    printf("sbrk re-allocation failed, a %x c %x\n", a, c);
//    exit(0);
//  }
//  if(*lastaddr == 99){
//    // should be zero
//    printf("sbrk de-allocation didn't really deallocate\n");
//    exit(0);
//  }
//
//  a = sbrk(0);
//  c = sbrk(-(sbrk(0) - oldbrk));
//  if(c != a){
//    printf("sbrk downsize failed, a %x c %x\n", a, c);
//    exit(0);
//  }
//  
//  // can we read the kernel's memory?
//  for(a = (char*)(KERNBASE); a < (char*) (KERNBASE+2000000); a += 50000){
//    ppid = getpid();
//    pid = fork();
//    if(pid < 0){
//      printf("fork failed\n");
//      exit(0);
//    }
//    if(pid == 0){
//      printf("oops could read %x = %x\n", a, *a);
//      kill(ppid);
//      exit(0);
//    }
//    wait(NULL);
//  }
//
//  // if we run the system out of memory, does it clean up the last
//  // failed allocation?
//  if(pipe(fds) != 0){
//    printf("pipe() failed\n");
//    exit(0);
//  }
//  for(i = 0; i < sizeof(pids)/sizeof(pids[0]); i++){
//    if((pids[i] = fork()) == 0){
//      // allocate a lot of memory
//      sbrk(BIG - (uint)sbrk(0));
//      write(fds[1], "x", 1);
//      // sit around until killed
//      for(;;) sleep(1000);
//    }
//    if(pids[i] != -1)
//      read(fds[0], &scratch, 1);
//  }
//  // if those failed allocations freed up the pages they did allocate,
//  // we'll be able to allocate here
//  c = sbrk(4096);
//  for(i = 0; i < sizeof(pids)/sizeof(pids[0]); i++){
//    if(pids[i] == -1)
//      continue;
//    kill(pids[i]);
//    wait(NULL);
//  }
//  if(c == (char*)0xffffffff){
//    printf("failed sbrk leaked memory\n");
//    exit(0);
//  }
//
//  if(sbrk(0) > oldbrk)
//    sbrk(-(sbrk(0) - oldbrk));
//
//  printf("sbrk test OK\n");
//}

void
validateint(int *p)
{
	ulong ret;
	asm volatile(
		"movq	%%rsp, %%r10\n"
		"leaq	2(%%rip), %%r11\n"
		"sysenter\n"
		: "=a"(ret)
#define SYS_PIPE2         293
		: "0"(SYS_PIPE2), "D"(p)
		: "cc", "memory");
	if (ret == 0)
		errx(-1, "bad int passed?");
}

void
validatetest(void)
{
  int hi, pid;
  ulong p;

  printf("validate test\n");
  hi = 1100*1024;

  for(p = 0; p <= (uint)hi; p += 4096){
    if((pid = fork()) == 0){
      // try to crash the kernel by passing in a badly placed integer
      validateint((int*)p);
      exit(0);
    }
    //sleep(0);
    //sleep(0);
    //kill(pid);
    int status;
    wait(&status);
    if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
      errx(-1, "validate failed");

    // try to crash the kernel by passing in a bad string pointer
    if(link("nosuchfile", (char*)p) == 0){
      printf("link should not succeed\n");
      exit(0);
    }
  }

  printf("validate ok\n");
}

// does unintialized data start out zero?
char uninit[10000];
void
bsstest(void)
{
  int i;

  printf("bss test\n");
  for(i = 0; i < sizeof(uninit); i++){
    if(uninit[i] != '\0'){
      printf("bss test failed\n");
      exit(0);
    }
  }
  printf("bss test ok\n");
}

// does exec return an error if the arguments
// are larger than a page? or does it write
// below the stack and wreck the instructions/data?
void
bigargtest(void)
{
  int pid, fd;

  unlink("bigarg-ok");
  pid = fork();
  if(pid == 0){
#define MAXARG	32
    static char *args[MAXARG];
    int i;
    for(i = 0; i < MAXARG-1; i++)
      args[i] = "bigargs test: failed\n                                                                                                                                                                                                       ";
    args[MAXARG-1] = 0;
    printf("bigarg test\n");
    exec("echo", args);
    printf("bigarg test ok\n");
    fd = open("bigarg-ok", O_CREATE);
    close(fd);
    exit(0);
  } else if(pid < 0){
    printf("bigargtest: fork failed\n");
    exit(0);
  }
  wait(NULL);
  fd = open("bigarg-ok", 0);
  if(fd < 0){
    printf("bigarg test failed!\n");
    exit(0);
  }
  close(fd);
  unlink("bigarg-ok");
}

// what happens when the file system runs out of blocks?
// answer: balloc panics, so this test is not useful.
void
fsfull()
{
  int nfiles;
  int fsblocks = 0;

  printf("fsfull test\n");

  for(nfiles = 0; ; nfiles++){
    char name[64];
    name[0] = 'f';
    name[1] = '0' + nfiles / 1000;
    name[2] = '0' + (nfiles % 1000) / 100;
    name[3] = '0' + (nfiles % 100) / 10;
    name[4] = '0' + (nfiles % 10);
    name[5] = '\0';
    printf("writing %s\n", name);
    int fd = open(name, O_CREATE|O_RDWR);
    if(fd < 0){
      printf("open %s failed\n", name);
      break;
    }
    int total = 0;
    while(1){
      int cc = write(fd, buf, BSIZE);
      if(cc < BSIZE)
        break;
      total += cc;
      fsblocks++;
    }
    printf("wrote %d bytes\n", total);
    close(fd);
    if(total == 0)
      break;
  }

  while(nfiles >= 0){
    char name[64];
    name[0] = 'f';
    name[1] = '0' + nfiles / 1000;
    name[2] = '0' + (nfiles % 1000) / 100;
    name[3] = '0' + (nfiles % 100) / 10;
    name[4] = '0' + (nfiles % 10);
    name[5] = '\0';
    unlink(name);
    nfiles--;
  }

  printf("fsfull test finished\n");
}

unsigned long randstate = 1;
unsigned int
_rand()
{
  randstate = randstate * 1664525 + 1013904223;
  return randstate;
}

void
rshuffle(char *f1, char *f2, char *n1, char *n2)
{
	int i;
	for (i = 0; i < 100; i++) {
		while (rename(f1, f2) < 0)
			usleep(1);
		while (rename(n1, n2) < 0)
			usleep(1);
	}
	exit(0);
}

void
collectn(int n)
{
	int i;
	for (i = 0; i < n; i++) {
		int status;
		wait(&status);
		if (WEXITSTATUS(status) != 0)
			errx(status, "child failed: %d", status);
	}
}

void
_rename()
{
	int ret;
	if ((ret = open("a", O_RDONLY | O_CREAT)) < 0)
		err(ret, "open");
	close(ret);
	if ((ret = open("f", O_RDONLY | O_CREAT)) < 0)
		err(ret, "open");
	close(ret);

	if ((ret = mkdir("renamed")))
		err(ret, "mkdir");

	if ((ret = rename("a", "renamed/a")))
		err(ret, "rename");
	if ((ret = rename("f", "renamed/f")))
		err(ret, "rename");

	if ((ret = chdir("renamed")))
		err(ret, "chdir");

	if ((ret = rename("f", "f")))
		err(ret, "rename");

	if (fork() == 0)
		rshuffle("a", "b", "e", "f");
	if (fork() == 0)
		rshuffle("b", "c", "f", "g");
	if (fork() == 0)
		rshuffle("c", "a", "g", "e");
	collectn(3);

	unlink("a");
	unlink("b");
	unlink("c");
	unlink("e");
	unlink("f");
	unlink("g");

	if ((ret = mkdir("d1")))
		err(ret, "mkdir");
	if ((ret = mkdir("d2")))
		err(ret, "mkdir");
	if ((ret = mkdir("d3")))
		err(ret, "mkdir");

	if ((ret = open("d1/a", O_RDONLY | O_CREAT)) < 0)
		err(ret, "open");
	close(ret);
	if ((ret = open("d2/e", O_RDONLY | O_CREAT)) < 0)
		err(ret, "open");
	close(ret);

	if (fork() == 0)
		rshuffle("d1/a", "d2/b", "d1/f", "d3/g");
	if (fork() == 0)
		rshuffle("d2/b", "d3/c", "d2/e", "d1/f");
	if (fork() == 0)
		rshuffle("d3/c", "d1/a", "d3/g", "d2/e");
	collectn(3);

	exit(0);
}

void
renametest()
{
	printf("rename test\n");

	int pid = fork();
	if (!pid)
		_rename();
	int status;
	wait(&status);

	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(status, "rename test failed");

	printf("rename test finished\n");
}

void *
_bigstack(void *no)
{
	uintptr_t stktop = (uintptr_t)&no;
	stktop = (stktop & ~((1 << 12) - 1)) + (1 << 12);
	char *test = (char *)stktop - 4096*30;
	*test = 0;
	asm volatile ("" ::"r"(test) : "memory");
	return NULL;
}

void
posixtest()
{
	printf("posix test\n");

	char *of = "poutfile";
	char *inf = "pinfile";

	// create input for child lsh
	int infd;
	if ((infd = open(inf, O_CREAT | O_WRONLY)) < 0)
		err(infd, "open");

	char *inmsg = "echo posix spawn worked\n";
	size_t ilen = strlen(inmsg);

	int ret;
	if ((ret = write(infd, inmsg, ilen)) != ilen)
		err(ret, "write");

	// reopen input file so child has correct offset
	close(infd);
	if ((infd = open(inf, O_RDONLY)) < 0)
		err(infd, "open");

	int outfd;
	if ((outfd = open(of, O_CREAT | O_WRONLY)) < 0)
		err(outfd, "open");

	posix_spawn_file_actions_t fa;
	if ((ret = posix_spawn_file_actions_init(&fa)) < 0)
		err(ret, "posix fa init");

	if ((ret = posix_spawn_file_actions_adddup2(&fa, outfd, 1)) < 0)
		err(ret, "posix addup2");

	if ((ret = posix_spawn_file_actions_adddup2(&fa, infd, 0)) < 0)
		err(ret, "posix addup2");

	char *args[] = {"/bin/lsh", NULL};
	pid_t c;
	if ((ret = posix_spawn(&c, args[0], &fa, NULL, args, NULL)))
		err(ret, "posix_spawn");

	if ((ret = posix_spawn_file_actions_destroy(&fa)) < 0)
		err(ret, "posix fa destroy");

	close(outfd);
	close(infd);

	int status;
	pid_t got = wait(&status);
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(status, "child failed");
	if (got != c)
		errx(-1, "wrong pid %ld %ld", c, got);

	if ((outfd = open(of, O_RDONLY)) < 0)
		err(outfd, "open");

	char *omsg = "# posix spawn worked\n# ";
	size_t olen = strlen(omsg);

	char buf[128];
	if ((ret = read(outfd, buf, sizeof(buf))) < 0)
		err(ret, "read");
	else if (ret != olen)
		errx(-1, "unexpected read len: %d %lu", ret, olen);

	if (strncmp(omsg, buf, olen))
		errx(-1, "unexpected child output");
	close(outfd);

	pthread_attr_t pa;
	pthread_attr_init(&pa);
	pthread_attr_setstacksize(&pa, 4096*30);
	pthread_t ct;
	if (pthread_create(&ct, &pa, _bigstack, NULL))
		errx(-1, "pthread_create");
	void *threadret;
	if (pthread_join(ct, &threadret))
		errx(-1, "pthread_join");
	if (threadret != NULL)
		errx(-1, "thread failed");
	pthread_attr_destroy(&pa);

	unlink(inf);
	unlink(of);

	printf("posix test ok\n");
}

void
lseektest()
{
	printf("lseek test\n");

	char *f = "lseekfile";

	int fd = open(f, O_CREAT | O_RDWR);
	if (fd < 0)
		err(fd, "open");

	unlink(f);

	char *msg = "duhee hi";
	int ret = write(fd, msg, strlen(msg));
	if (ret < 0)
		err(ret, "write");
	else if (ret != strlen(msg))
		errx(-1, "bad len");

	if ((ret = lseek(fd, 10240, SEEK_SET)) < 0)
		err(ret, "lseek");

	if ((ret = write(fd, msg, strlen(msg))) < 0)
		err(ret, "write");
	else if (ret != strlen(msg))
		errx(-1, "bad len");

	if ((ret = lseek(fd, 0, SEEK_SET)) < 0)
		err(ret, "lseek");

	long tot = 0;
	char buf[1024];
	while ((ret = read(fd, buf, sizeof(buf))) > 0)
		tot += ret;
	if (ret < 0)
		err(ret, "read");

	if (tot != 10240 + strlen(msg))
		errx(-1, "bad total lens");

	printf("lseek test ok\n");
}

void
_ruchild()
{
	int i;
	for (i = 0; i < 1e8; i++)
		asm volatile("":::"memory");
	exit(0);
}

void *
_ruthread(void *p)
{
	volatile int *done = (int *)p;
	while (*done == 0)
		;
	return NULL;
}

ulong
tvtot(struct timeval *t)
{
	return t->tv_sec * 1e6 + t->tv_usec;
}

void
rusagetest()
{
	printf("rusage test\n");

	struct rusage r;

	const int n = 3;
	pthread_t t[n];
	int i;
	volatile int done = 0;
	for (i = 0; i < n; i++)
		if (pthread_create(&t[i], NULL, _ruthread, (void *)&done))
			errx(-1, "pthread_create");
	sleep(1);
	done = 1;
	for (i = 0; i < n; i++)
		pthread_join(t[i], NULL);

	memset(&r, 0, sizeof(struct rusage));
	int ret;
	if ((ret = getrusage(RUSAGE_SELF, &r)) < 0)
		err(ret, "getrusage");

	ulong utime = tvtot(&r.ru_utime);
	if (utime < 1000000)
		errx(-1, "weird utime: %lu", utime);

	//printf("my user us: %lu\n", tvtot(&r.ru_utime));
	//printf("my sys  us: %lu\n", tvtot(&r.ru_stime));

	int pid = fork();
	if (!pid)
		_ruchild();
	int status;
	memset(&r, 0, sizeof(struct rusage));
	ret = wait4(WAIT_ANY, &status, 0, &r);
	if (ret < 0)
		err(ret, "wait4 rusage");
	if (ret != pid)
		errx(-1, "wrong pid %d %d", ret, pid);

	if (tvtot(&r.ru_stime) > 500)
		errx(-1, "more system time than expected");
	if (tvtot(&r.ru_stime) > tvtot(&r.ru_utime))
		errx(-1, "more system time than user time");
	printf("rusage test ok\n");

	//printf("user us: %lu\n", tvtot(&r.ru_utime));
	//printf("sys  us: %lu\n", tvtot(&r.ru_stime));
}

void *bart(void *arg)
{
	pthread_barrier_t *b = (pthread_barrier_t *)arg;

	int i;
	for (i = 0; i < 100; i++)
		pthread_barrier_wait(b);
	return NULL;
}

void
barriertest()
{
	printf("barrier test\n");

	const int nthreads = 3;
	pthread_barrier_t b;
	pthread_barrier_init(&b, NULL, nthreads);

	int i;
	pthread_t t[nthreads];
	for (i = 0; i < nthreads; i++)
		if (pthread_create(&t[i], NULL, bart, &b))
			errx(-1, "pthread create");
	for (i = 0; i < nthreads; i++)
		if (pthread_join(t[i], NULL))
			errx(-1, "pthread join");

	printf("barrier test ok\n");
}

void *_waitany(void *a)
{
	waitpid(WAIT_ANY, NULL, 0);
	printf("waitany woke\n");
	return NULL;
}

void *_waitchild(void *a)
{
	long pid = (pid_t)a;
	waitpid(pid, NULL, 0);
	printf("waitchild woke\n");
	return NULL;
}

void
threadwait()
{
	printf("threadwait test\n");
	long pid;
	if ((pid = fork()) == 0) {
		sleep(1);
		exit(0);
	} else if (pid < 0)
		err(pid, "fork");
	int ret;
	pthread_t t[2];
	if ((ret = pthread_create(&t[0], NULL, _waitany, NULL)))
		errx(ret, "pthread_create");
	if ((ret = pthread_create(&t[1], NULL, _waitchild, (void *)pid)))
		errx(ret, "pthread_create");
	if ((ret = pthread_join(t[0], NULL)))
		errx(ret, "pthread_join");
	if ((ret = pthread_join(t[1], NULL)))
		errx(ret, "pthread_join");

	if ((pid = fork()) == 0) {
		sleep(1);
		exit(0);
	} else if (pid < 0)
		errx(pid, "fork");

	if ((ret = pthread_create(&t[0], NULL, _waitany, NULL)))
		errx(ret, "pthread_create");
	if ((ret = pthread_create(&t[1], NULL, _waitany, NULL)))
		errx(ret, "pthread_create");
	if ((ret = pthread_join(t[0], NULL)))
		errx(ret, "pthread_join");
	if ((ret = pthread_join(t[1], NULL)))
		errx(ret, "pthread_join");

	if ((pid = fork()) == 0) {
		sleep(1);
		exit(0);
	} else if (pid < 0)
		errx(pid, "fork");

	if ((ret = pthread_create(&t[0], NULL, _waitchild, (void *)pid)))
		errx(ret, "pthread_create");
	if ((ret = pthread_create(&t[1], NULL, _waitchild, (void *)pid)))
		errx(ret, "pthread_create");
	if ((ret = pthread_join(t[0], NULL)))
		errx(ret, "pthread_join");
	if ((ret = pthread_join(t[1], NULL)))
		errx(ret, "pthread_join");

	printf("threadwait ok\n");
}

// ... = is processed in groups of three: int fd, short events, short expected
void _pchk(const int timeout, const int expto, const int numfds, ...)
{
	struct pollfd pfds[numfds];
	short expects[numfds];

	va_list ap;
	va_start(ap, numfds);
	int i;
	for (i = 0; i < numfds; i++) {
		int fd = va_arg(ap, int);
		short events = va_arg(ap, int);
		short expect = va_arg(ap, int);
		pfds[i].fd = fd;
		pfds[i].events = events;
		expects[i] = expect;
	}
	va_end(ap);

	int ret = poll(pfds, numfds, timeout);
	// verify return value
	if (expto && ret != 0)
		errx(-1, "expected timeout");
	int readyfds = 0;
	for (i = 0; i < numfds; i++) {
		if (expects[i] != 0)
			readyfds++;
		if (expects[i] != pfds[i].revents)
			errx(-1, "status mismatch: got %x, expected %x for "
			    "fd %d", pfds[i].revents, expects[i], pfds[i].fd);
	}
	if (readyfds != ret)
		errx(-1, "ready fd mismatch: %d != %d", readyfds, ret);
}

void polltest()
{
	printf("poll test\n");

	int f1 = open("/tmp/hello1", O_CREAT | O_RDWR);
	if (f1 < 0)
		err(-1, "create");
	int f2 = open("/tmp/hello2", O_CREAT | O_RDWR);
	if (f2 < 0)
		err(-1, "create");

	const int rwfl = POLLIN | POLLOUT;
	const int toyes = 1, tono = 0;
	// make sure they are {read,write}able
	_pchk(-1, tono, 1, f1, rwfl, rwfl);
	_pchk(-1, tono, 2, f1, rwfl, rwfl,
			f2, rwfl, rwfl);
	_pchk(INT_MAX, tono, 2, f1, POLLIN, POLLIN,
			f2, POLLOUT, POLLOUT);
	_pchk(0, tono,  2, f1, rwfl, rwfl,
			f2, rwfl, rwfl);
	// and that no error/hup is reported for non-blocking and timeouts
	_pchk(0, toyes,  2, f1, POLLERR | POLLHUP, 0,
			f2, POLLERR | POLLHUP, 0);
	_pchk(500, toyes,  2, f1, POLLERR | POLLHUP, 0,
			f2, POLLERR | POLLHUP, 0);

	// pipe initially has no data to read
	int pp[2];
	if (pipe(pp) < 0)
		err(-1, "pipe");
	_pchk(-1, tono, 4,
		f1, rwfl, rwfl,
		f2, rwfl, rwfl,
		pp[0], rwfl, 0,
		pp[1], rwfl, POLLOUT);

	// notice reads
	char buf[4096];
	long ret;
	ret = write(pp[1], buf, sizeof(buf));
	if (ret < 0)
		err(-1, "write");
	else if (ret != sizeof(buf))
		errx(-1, "short write");

	_pchk(-1, tono, 2,
		pp[0], rwfl, POLLIN,
		pp[1], rwfl, 0);

	// make sure pipe empties
	ret = read(pp[0], buf, sizeof(buf)/2);
	if (ret < 0)
		err(-1, "read");
	else if (ret != (sizeof(buf)/2))
		errx(-1, "short read");

	_pchk(10, tono, 2,
		pp[0], rwfl, POLLIN,
		pp[1], rwfl, POLLOUT);

	ret = read(pp[0], buf, sizeof(buf)/2);
	if (ret < 0)
		err(-1, "read");
	else if (ret != (sizeof(buf)/2))
		errx(-1, "short read");

	_pchk(-1, tono, 2,
		pp[0], rwfl, 0,
		pp[1], rwfl, POLLOUT);

	int unbound = socket(AF_UNIX, SOCK_DGRAM, 0);
	int sock = socket(AF_UNIX, SOCK_DGRAM, 0);
	if (sock < 0)
		err(-1, "socket");
	char *spath = "/tmp/pollsock";
	struct sockaddr_un sa;
	sa.sun_family = AF_UNIX;
	snprintf(sa.sun_path, sizeof(sa.sun_path), "%s", spath);

	unlink(spath);
	if (bind(sock, (struct sockaddr *)&sa, SUN_LEN(&sa)) < 0) {
		err(-1, "bind");
	}

	_pchk(-1, tono, 4,
		pp[0], rwfl, 0,
		pp[1], rwfl, POLLOUT,
		unbound, rwfl, POLLERR,
		sock, rwfl, POLLOUT);

	_pchk(10, toyes, 1,
		sock, POLLIN, 0);

	pid_t pid = fork();
	if (pid < 0)
		err(-1, "fork");
	if (pid == 0) {
		close(f1);
		close(f2);
		close(pp[0]);
		close(sock);

		struct sockaddr_un ua;
		ua.sun_family = AF_UNIX;
		snprintf(ua.sun_path, sizeof(ua.sun_path), "/tmp/pollsock");
		char *msg = "foobar";
		ret = sendto(unbound, msg, strlen(msg), 0,
		    (struct sockaddr *)&ua, SUN_LEN(&ua));
		if (ret < 0)
			err(-1, "sendto");
		else if (ret != strlen(msg))
			errx(-1, "short write");
		printf("child sent\n");
		exit(0);
	}

	_pchk(-1, tono, 1,
		sock, POLLIN, POLLIN);
	_pchk(0, tono, 1,
		sock, rwfl, rwfl);
	_pchk(10, toyes, 1,
		sock, POLLERR, 0);

	ret = recv(sock, buf, sizeof(buf), 0);
	if (ret < 0)
		err(-1, "recv");
	else if (ret == 0)
		errx(-1, "short read");

	_pchk(10, tono, 1,
		sock, rwfl, POLLOUT);
	_pchk(10, toyes, 1,
		sock, POLLIN, 0);

	close(f1);
	close(f2);
	close(pp[0]);
	close(pp[1]);
	close(sock);
	close(unbound);

	int status;
	if (wait(&status) != pid)
		errx(-1, "wrong child");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
	printf("polltest ok\n");
}

void
runsockettest(void)
{
	pid_t child = fork();
	if (child < 0)
		err(-1, "fork");
	if (!child) {
		char *args[] = {"sockettest", NULL};
		execvp(args[0], args);
		err(-1, "exec failed");
	}

	int status;
	if (wait(&status) != child)
		errx(-1, "wrong child");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
}

void _testnoblk(int rfd, int wfd)
{
	char buf[BSIZE];
	memset(buf, 'A', sizeof(buf));
	ssize_t ret, tot = 0;
	while ((ret = write(wfd, buf, 10)) > 0)
		tot += ret;
	if (ret != -1 || errno != EWOULDBLOCK)
		errx(-1, "weird write ret: %ld %d", ret, errno);
	ssize_t tot2 = 0;
	while ((ret = read(rfd, buf, 32)) > 0)
		tot2 += ret;
	if (ret != -1 || errno != EWOULDBLOCK)
		errx(-1, "weird read ret: %ld %d", ret, errno);
	if (tot != tot2)
		errx(-1, "len mismatch %zd %zd", tot, tot2);
}

void fnonblock(void)
{
	printf("non-blocking test\n");
	int p[2];
	if (pipe2(p, O_NONBLOCK))
		err(-1, "pipe");
	_testnoblk(p[0], p[1]);
	close(p[0]);
	close(p[1]);

	if (pipe2(p, 0))
		err(-1, "pipe");
	int flags;
	if ((flags = fcntl(p[0], F_GETFL)) == -1)
		err(-1, "fcntlg");
	if (fcntl(p[0], F_SETFL, flags | O_NONBLOCK) == -1)
		err(-1, "fcntls");
	if ((flags = fcntl(p[1], F_GETFL)) == -1)
		err(-1, "fcntlg");
	if (fcntl(p[1], F_SETFL, flags | O_NONBLOCK) == -1)
		err(-1, "fcntls");
	_testnoblk(p[0], p[1]);
	close(p[0]);
	close(p[1]);

	// XXX test unix stream sockets once we have socketpair(2)
	printf("non-blocking test ok\n");
}

void preadwrite(void)
{
	printf ("preadwrite test\n");

	char *tfile = "/tmp/prdwrfile";
	int fd = open(tfile, O_WRONLY | O_CREAT | O_TRUNC);
	if (fd < 0)
		err(-1, "open trunc");

	const size_t fsz = 1024;
	char fbuf[fsz];
	int i;
	for (i = 0; i < sizeof(fbuf); i++)
		fbuf[i] = i * (i+1);
	ssize_t r;
	if ((r = write(fd, fbuf, sizeof(fbuf))) != sizeof(fbuf))
		err(-1, "write");
	close(fd);

	if ((fd = open(tfile, O_RDONLY)) < 0)
		err(-1, "open read");
	char pbuf[fsz];
	char *f = &fbuf[0];
	char *fend = f + sizeof(fbuf);
	srandom(time(NULL));

	// interleave preads with reads to make sure offsets aren't screwed up
	while (((r = read(fd, f, fend - f)) > 0)) {
		f += r;
		off_t off = random() % sizeof(fbuf);
		if ((r = pread(fd, pbuf, sizeof(pbuf) - off, off)) < 0)
			err(-1, "pread");
	}
	if (r < 0)
		err(-1, "read");
	if (f != fend)
		errx(-1, "short length");

	for (i = 0; i < sizeof(fbuf); i++)
		if (fbuf[i] != (char)(i * (i+1)))
			errx(-1, "byte mismatch");
	close(fd);

	// now read file forwards and backwards simultaneously to make sure
	// data read is sane
	f = &fbuf[0];
	char *pend = &pbuf[0] + sizeof(pbuf);
	char *p = pend;
	const size_t ch = 10;

	if ((fd = open(tfile, O_RDONLY)) < 0)
		err(-1, "open");
	while (((r = read(fd, f, MIN(ch, fend - f))) > 0)) {
		f += r;
		size_t take = MIN(ch, p - &pbuf[0]);
		p -= take;
		off_t off = sizeof(pbuf) - (pend - p);
		if ((r = pread(fd, p, take, off)) < 0)
			err(-1, "pread");
	}
	if (r < 0)
		err(-1, "read");
	if (p != &pbuf[0] || f != fend)
		errx(-1, "short length");
	if (strncmp(fbuf, pbuf, fsz) != 0)
		errx(-1, "byte mismatch");
	close(fd);

	printf ("preadwrite test ok\n");
}

static char _expfile[BUFSIZ*2];
static char gotfile[BUFSIZ*2];

void stdiotest(void)
{
	printf("stdio test\n");

	FILE *f = fopen("/bigfile.txt", "r");
	if (f == NULL)
		err(-1, "fopen");
	char buf[BSIZE];
	ulong cksum = 0;
	size_t tot = 0, r;
	while ((r = fread(buf, 1, sizeof(buf), f)) > 0) {
		tot += r;
		int i;
		for (i = 0; i < r; i++)
			cksum += buf[i];
	}
	if (ferror(f))
		err(-1, "fread");
	if (!feof(f))
		errx(-1, "exptected eof");
	ulong ckexp = 0x1fbd000;
	if (cksum != ckexp)
		errx(-1, "cksum mismatch: %lx != %lx", cksum, ckexp);
	fclose(f);

	srandom(time(NULL));
	int i;
	for (i = 0; i < sizeof(_expfile); i++)
		_expfile[i] = random();
	const char *tfile = "/tmp/stdiofile";
	if ((f = fopen(tfile, "w")) == NULL)
		err(-1, "fopen");
	if ((r = fwrite(_expfile, sizeof(_expfile), 1, f)) != 1)
		err(-1, "fwrite");
	fclose(f);

	if ((f = fopen(tfile, "r")) == NULL)
		err(-1, "fopen");

	const size_t ch = 10;
	char *p = &gotfile[0];
	char * const pend = p + sizeof(gotfile);
	while (p < pend) {
		if (feof(f))
			errx(-1, "early eof: %ld", p - &gotfile[0]);
		if (ferror(f))
			errx(-1, "ferror: %d", ferror(f));
		size_t l = MIN(ch, pend - p);
		r = fread(p, 1, l, f);
		if (r != l)
			err(-1, "fread %zu != %zu", r, l);
		p += r;
	}
	fclose(f);

	if (strncmp(gotfile, _expfile, sizeof(_expfile)) != 0)
		errx(-1, "byte mismatch");

	// test ungetc
	if (!(f = fopen("/hi.txt", "r")))
		err(-1, "fopen");
	char buf1[64];
	char buf2[64];
	if ((r = fread(buf1, 1, sizeof(buf1), f)) < 1)
		err(-1, "fread");
	for (i = 0; i < r; i++)
		if (ungetc(buf1[i], f) != buf1[i])
			errx(-1, "ungetc");
	if ((r = fread(buf2, 1, sizeof(buf2), f)) < 1)
		err(-1, "fread");
	for (i = 0; i < r; i++)
		if (buf1[i] != buf2[r-1-i])
			errx(-1, "ungetc mismatch");

	printf("stdio test ok\n");
}

void realloctest(void)
{
	printf("realloc test\n");

	const size_t size = 13;
	int i;
	uchar *p = NULL;
	for (i = 1; i < 10; i++) {
		size_t nsize = i*size;
		p = realloc(p, nsize);
		if (p == NULL)
			errx(-1, "malloc");
		size_t osize = (i-1)*size;
		int j;
		for (j = 0; j < osize; j++)
			if (p[j] != j)
				errx(-1, "byte mismatch");
		for (j = osize; j < nsize; j++)
			p[j] = j;
	}
	free(p);

	printf("realloc test ok\n");
}

__attribute__((noreturn))
void _cwdtest(void)
{
	const char *p = "/";
	if (chdir(p))
		err(-1, "chdir");
	char buf[128];
	if (!getcwd(buf, sizeof(buf)))
		err(-1, "getcwd");
	if (strcmp(p, buf))
		errx(-1, "pwd mismatch");

	p = "/another/../another/../another/../another/../boot/uefi/";
	if (chdir(p))
		err(-1, "chdir");
	if (!getcwd(buf, sizeof(buf)))
		err(-1, "getcwd");
	if (strcmp(buf, "/boot/uefi"))
		errx(-1, "pwd mismatch");

	exit(0);
}

void cwdtest(void)
{
	printf("cwd test\n");

	pid_t c = fork();
	if (!c)
		_cwdtest();
	int status;
	if (wait(&status) != c)
		errx(-1, "wrong child");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");

	printf("cwd test ok\n");
}

void trunctest(void)
{
	printf("trunc test\n");

	const char *fp = "/tmp/truncfile";
	int fd = open(fp, O_RDWR | O_CREAT);
	if (fd < 0)
		err(-1, "open");
	char buf[256];
	int i;
	for (i = 0; i < sizeof(buf); i++)
		buf[i] = 'A';
	if (write(fd, buf, sizeof(buf)) != sizeof(buf))
		err(-1, "short write");
	if (lseek(fd, 0, SEEK_SET) != 0)
		err(-1, "lseek");
	off_t newsz = 15;
	if (ftruncate(fd, newsz))
		err(-1, "ftruncate");
	ssize_t r;
	if ((r = read(fd, buf, sizeof(buf))) != newsz)
		err(-1, "expected %lu bytes, got %zd", newsz, r);
	close(fd);

	newsz = 7;
	if (truncate(fp, newsz))
		err(-1, "truncate");
	fd = open(fp, O_RDONLY | O_CREAT);
	if (fd < 0)
		err(-1, "open");
	if ((r = read(fd, buf, sizeof(buf))) != newsz)
		err(-1, "expected %lu bytes, got %zd", newsz, r);
	close(fd);

	// 8192, so file spans two pages in page cache
	newsz = sizeof(buf)*32;
	if (truncate(fp, newsz))
		err(-1, "truncate");
	fd = open(fp, O_RDONLY | O_CREAT);
	if (fd < 0)
		err(-1, "open");
	for (i = 0; i < newsz; i += sizeof(buf))
		if ((r = read(fd, buf, sizeof(buf))) != sizeof(buf))
			err(-1, "expected %lu bytes, got %zd", newsz, r);
	close(fd);

	printf("trunc test ok\n");
}

void accesstest(void)
{
	// XXX write better test once we actually have permissions
	printf("access test\n");

	if (access("/", R_OK | X_OK | W_OK))
	       err(-1, "access");
	if (access("/tmp", R_OK | X_OK | W_OK))
	       err(-1, "access");
	if (access("//bin", R_OK | X_OK | W_OK))
	       err(-1, "access");
	if (access("//bin/cat", R_OK | X_OK))
		err(-1, "access");

	if (access("//does/not/exist/ever", R_OK | X_OK) == 0)
		errx(-1, "access for bad file");

	char *f = "/tmp/accfile";
	int fd;
	if ((fd = open(f, O_CREAT | O_WRONLY)) < 0)
		err(-1, "creat");
	close(fd);

	if (access(f, R_OK | X_OK | W_OK))
	       err(-1, "access");

	printf("access test OK\n");
}

//static const int btimes = 10000;
//static const int bctimes= 2500;
//static const int ltimes = 1000000;
static const int btimes = 100;
static const int bctimes= 100;
static const int ltimes = 100000;
static int lcounter;
static volatile int go;

static void *_locker(void *v)
{
	while (go != 1)
		asm volatile("pause\n":::"memory");
	pthread_mutex_t *m = (pthread_mutex_t *)v;
	int i;
	for (i = 0; i < ltimes; i++) {
		if (pthread_mutex_lock(m))
			err(-1, "m lock");
		lcounter++;
		if (pthread_mutex_unlock(m))
			err(-1, "m unlock");
	}
	return NULL;
}

struct conds_t {
	pthread_mutex_t *m;
	pthread_cond_t *yours;
	pthread_cond_t *theirs;
};

void _mutextest(const int nt)
{
	pthread_mutex_t m = PTHREAD_MUTEX_INITIALIZER;

	pthread_t t[nt];
	int i;
	for (i = 0; i < nt; i++)
		if (pthread_create(&t[i], NULL, _locker, &m))
			errx(-1, "pthread_create");
	go = 1;
	for (i = 0; i < nt; i++)
		if (pthread_join(t[i], NULL))
			err(-1, "pthread_join");
	int exp = nt * ltimes;
	if (lcounter != exp)
		errx(-1, "uh oh %d != %d", exp, lcounter);

	if (pthread_mutex_destroy(&m))
		err(-1, "m destroy");
}

static void *_condsleep(void *v)
{
	struct conds_t *ca = (struct conds_t *)v;

	pthread_mutex_t *m = ca->m;
	pthread_cond_t *mine = ca->yours;
	pthread_cond_t *theirs = ca->theirs;

	if (pthread_mutex_lock(m))
		err(-1, "lock");
	go = 1;

	int i;
	for (i = 0; i < btimes; i++) {
		lcounter++;
		if (pthread_cond_signal(theirs))
			err(-1, "cond sig");
		if (pthread_cond_wait(mine, m))
			err(-1, "cond wait");
	}
	if (pthread_cond_signal(theirs))
		err(-1, "cond sig");

	if (pthread_mutex_unlock(m))
		err(-1, "unlock");
	return NULL;
}

static void *_condsleepbc(void *v)
{
	struct conds_t *ca = (struct conds_t *)v;
	pthread_mutex_t *m = ca->m;
	pthread_cond_t *mine = ca->yours;

	if (pthread_mutex_lock(m))
		err(-1, "lock");

	while (go) {
		lcounter++;
		if (pthread_cond_wait(mine, m))
			err(-1, "cond wait");
	}
	if (pthread_mutex_unlock(m))
		err(-1, "unlock");
	return NULL;
}

static void _condtest(const int nt)
{
	if (nt < 0)
		errx(-1, "bad nthreads");

	lcounter = 0;

	pthread_mutex_t m;
	if (pthread_mutex_init(&m, NULL))
		err(-1, "m init");
	pthread_cond_t cs[nt];
	int i;
	for (i = 0; i < nt; i++)
		if (pthread_cond_init(&cs[i], NULL))
			err(-1, "c init");
	pthread_t t[nt];
	struct conds_t args[nt];
	for (i = 0; i < nt; i++) {
		args[i].m = &m;
		args[i].yours = &cs[i];
		const int nid = i + 1 == nt ? 0 : i + 1;
		args[i].theirs = &cs[nid];
		// make sure each thread goes to sleep before we create their
		// neighbor to guarantee that the threads terminate in order.
		go = 0;
		if (pthread_create(&t[i], NULL, _condsleep, &args[i]))
			errx(-1, "pthread_ create");
		while (go == 0)
			asm volatile("pause\n":::"memory");
	}

	for (i = 0; i < nt; i++)
		if (pthread_join(t[i], NULL))
			err(-1, "pthread_join");

	int exp = btimes*nt;
	if (lcounter != exp)
		errx(-1, "uh oh! %d != %d", lcounter, exp);

	if (pthread_mutex_destroy(&m))
		err(-1, "m destroy");
	for (i = 0; i < nt; i++)
		if (pthread_cond_destroy(&cs[i]))
			err(-1, "cond destroy");
}

static void _condbctest(const int nt)
{
	if (nt < 0)
		errx(-1, "bad nthreads");

	lcounter = 0;
	go = 1;

	pthread_mutex_t m;
	if (pthread_mutex_init(&m, NULL))
		err(-1, "m init");
	pthread_cond_t c;
	if (pthread_cond_init(&c, NULL))
		err(-1, "c init");

	pthread_t t[nt];
	struct conds_t args[nt];
	int i;
	for (i = 0; i < nt; i++) {
		args[i].m = &m;
		args[i].yours = &c;
		if (pthread_create(&t[i], NULL, _condsleepbc, &args[i]))
			errx(-1, "pthread_ create");
	}

	srandom(time(NULL));

	int enext = nt;
	for (i = 0; i < bctimes; i++) {
		volatile int *p = &lcounter;
		while (*p < enext)
			asm volatile("pause\n":::"memory");
		if (pthread_mutex_lock(&m))
			err(-1, "lock");
		if (i == bctimes - 1)
			go = 0;
		if (rand() < RAND_MAX / 1000) {
			struct timespec ts = {time(NULL) + 1, 0};
			if (pthread_cond_timedwait(&c, &m, &ts))
				err(-1, "timedwait");
		}
		if (pthread_cond_broadcast(&c))
			err(-1, "broadcast");
		if (pthread_mutex_unlock(&m))
			err(-1, "unlock");
		enext += nt;
	}

	for (i = 0; i < nt; i++)
		if (pthread_join(t[i], NULL))
			err(-1, "pthread_join");

	if (pthread_mutex_destroy(&m))
		err(-1, "m destroy");
	if (pthread_cond_destroy(&c))
		err(-1, "cond destroy");
}

int pthreadsharedfd;

void *threadfd(void *arg)
{
  long n = (long) arg;
  
  for (int i = 0; i < 1000; i++) {
    if (n == 0) {
      pthreadsharedfd = open("sharedfdf", O_CREATE|O_RDWR);
      if(pthreadsharedfd < 0)
	err(pthreadsharedfd, "fstests: cannot open sharedfd for writing");
      int n = write(pthreadsharedfd, "aaaaaaaaaa", 10);
      if(n != 10 && errno != EBADF) {
	err(-1, "error: write should have returned 10 or error EBADF %d\n", n);
      }
    } else {
      unlink("sharedfdf");
      close(pthreadsharedfd);
    }
  }
  return NULL;
}

void _pthreadfd(void) {
  const int nthreads = 2;

  printf("pthread shared fd\n");
	
  int i;
  pthread_t t[nthreads];
  for (int i = 0; i < nthreads; i++) {
    if (pthread_create(&t[i], NULL, threadfd, (void *) (long) i))
      errx(-1, "pthread create");
  }    
  for (i = 0; i < nthreads; i++) {
    if (pthread_join(t[i], NULL))
      errx(-1, "pthread join");
  }
  unlink("sharedfdf");
  printf("pthread shared fd ok\n");
  exit(0);
}

void pthreadfd(void) {
	pid_t c;
	switch ((c = fork())) {
	case -1:
		err(-1, "fork");
	case 0:
		_pthreadfd();
	}
	int status;
	if (wait(&status) != c)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "pthreadfd failed");
}

void futextest(void)
{
	printf("futex test\n");

	_mutextest(8);

	printf("cond test\n");
	_condtest(8);

	printf("cond bc test\n");
	_condbctest(8);

	printf("futex test ok\n");
}

__attribute__((aligned(4096)))
char _pagebuf[4096];

void lazyfaults(void)
{
	printf("lazy fault test\n");
	int fd = open("/tmp/lfault.txt", O_CREAT | O_WRONLY);
	if (fd == -1)
		err(-1, "open");
	if (write(fd, _pagebuf, 20) == -1)
		err(-1, "read");
	close(fd);
	printf("lazy fault ok\n");
}

__attribute__((aligned(4096)))
volatile long _observe;

void forktlb(void)
{
	printf("fork shootdown\n");
	_observe = 0;
	pid_t c = fork();
	if (c == -1) {
		err(-1, "fork");
	} else if (c != 0) {
		_observe = -1;
	} else {
		if (_observe != 0)
			errx(-1, "fork didn't shoot down %ld", _observe);
		exit(0);
	}
	int status;
	if (wait(&status) != c)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
	printf("fork shootdown success\n");
}

void mapshared(void)
{
	printf("mmap shared test\n");
        size_t sz = 4096;
        pid_t *p = mmap(NULL, sz, PROT_READ | PROT_WRITE,
            MAP_ANON | MAP_SHARED, -1, 0);
        if (p == MAP_FAILED)
                err(-1, "mmap");
        int i;
        for (i = 0; i < sz/sizeof(pid_t); i++) {
                pid_t c = fork();
                if (c == -1)
                        err(-1, "fork");
                if (c == 0) {
                        p[i] = getpid();
                        exit(0);
                } else {
                        int status;
                        pid_t got = wait(&status);
                        if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
                                errx(-1, "child failed");
                        if (got != c || p[i] != got)
                                errx(-1, "wrong pid");
                }
        }
	memset(p, 0, sz);
	int pp[2];
	if (pipe(pp) == -1)
		err(-1, "pipe");
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	if (c == 0) {
		if (close(pp[0]) == -1)
			err(-1, "close");
		for (i = 0; i < sz/sizeof(pid_t); i++) {
			p[i] = (pid_t)i;
			c = fork();
			if (c == -1)
				err(-1, "fork");
			if (c != 0)
				exit(0);
		}
		if (write(pp[1], "A", 1) != 1)
			errx(-1, "write");
		exit(0);
	}
	if (close(pp[1]) == -1)
		err(-1, "close");
	if (wait(NULL) != c)
		errx(-1, "wait");
	char buf;
	if (read(pp[0], &buf, 1) != 1)
		errx(-1, "read");
	for (i = 0; i < sz/sizeof(pid_t); i++)
		if (p[i] != i)
			errx(-1, "page mismatch");
	printf("mmap shared ok\n");
}

void spair(void)
{
	printf("socketpair test\n");

	int s[2];
	if (socketpair(AF_UNIX, SOCK_STREAM, 0, s) == -1)
		err(-1, "socketpair");

	const char *fn = "/bin/usertests";
	char buf[BSIZE];
	ssize_t r;
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	else if (c == 0) {
		close(s[0]);
		int sfd = open(fn, O_RDONLY);
		if (sfd == -1)
			err(-1, "open");
		while ((r = read(sfd, buf, sizeof(buf))) > 0) {
			if (write(s[1], buf, r) != r)
				err(-1, "write");
		}
		if (r == -1)
			err(-1, "read");
		exit(0);
	}
	close(s[1]);
	int rfd = open(fn, O_RDONLY);
	if (rfd == -1)
		err(-1, "open");
	char buf2[sizeof(buf)];
	while ((r = read(s[0], buf, sizeof(buf))) > 0) {
		if (read(rfd, buf2, sizeof(buf2)) != r)
			err(-1, "read");
		if (memcmp(buf, buf2, r) != 0)
			errx(-1, "data mismatch");
	}
	if (r == -1)
		err(-1, "read");
	if (close(rfd) == -1)
		err(-1, "close");
	int status;
	if (wait(&status) != c)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");

	printf("socketpair ok\n");
}

void iovtest(void)
{
	printf("iov test\n");
	int ffd = open("/bin/usertests", O_RDONLY);
	int bfd = open("/bin/usertests", O_RDONLY);
	int ofd = open("/tmp/dump", O_CREAT | O_TRUNC | O_RDWR);
	if (ffd == -1 || bfd == -1 || ofd == -1)
		err(-1, "open");
	const size_t bsz = BSIZE;
	char fbuf[bsz];
	char bbuf[bsz];
	ssize_t r;
	while ((r = read(ffd, fbuf, bsz)) == bsz) {
		struct iovec iovs[4];
		size_t fourth = BSIZE / 4;
		int i;
		char *end = &bbuf[bsz];
		for (i = 0; i < 4; i++) {
			iovs[i].iov_base = end - (i + 1)*fourth;
			iovs[i].iov_len = fourth;
		}
		if (readv(bfd, iovs, 4) != bsz)
			err(-1, "readv");
		char *a = &fbuf[0];
		for (i = 0; i < 4; i++) {
			char *b = end - (i + 1)*fourth;
			if (memcmp(a, b, fourth) != 0)
				errx(-1, "data mismatch");
			a += fourth;
		}
		for (i = 0; i < 4; i++) {
			iovs[i].iov_base = end - (i + 1)*fourth;
			iovs[i].iov_len = fourth;
		}
		if (writev(ofd, iovs, 4) != bsz)
			err(-1, "writev");
	}
	if (lseek(ffd, 0, SEEK_SET) == -1)
		err(-1, "lseek");
	if (lseek(ofd, 0, SEEK_SET) == -1)
		err(-1, "lseek");
	while ((r = read(ffd, fbuf, bsz)) == bsz) {
		if (read(ofd, bbuf, bsz) != bsz)
			err(-1, "read");
		if (memcmp(fbuf, bbuf, bsz) != 0)
			errx(-1, "data mismatch");
	}
	if (r == -1)
		err(-1, "read");
	if (close(ffd) == -1)
		err(-1, "close");
	if (close(bfd) == -1)
		err(-1, "close");
	if (close(ofd) == -1)
		err(-1, "close");
	printf("iov ok\n");
}

void _scmchald(int s)
{
	char buf[CMSG_SPACE(sizeof(int))];
	struct msghdr msg;
	struct iovec iov;
	memset(&msg, 0, sizeof(msg));
	memset(&iov, 0, sizeof(iov));

	char dur;
	iov.iov_base = &dur;
	iov.iov_len = 1;
	msg.msg_iov = &iov;
	msg.msg_iovlen = 1;
	msg.msg_control = buf;
	msg.msg_controllen = sizeof(buf);
	if (recvmsg(s, &msg, 0) != 1)
		err(-1, "recvmsg");
	if (dur != 4)
		errx(-1, "wrong data");
	struct cmsghdr *cmsg;
	for (cmsg = CMSG_FIRSTHDR(&msg); cmsg; cmsg = CMSG_NXTHDR(&msg, cmsg)) {
		if (cmsg->cmsg_len == CMSG_LEN(sizeof(int)) &&
		    cmsg->cmsg_level == SOL_SOCKET &&
		    cmsg->cmsg_type == SCM_RIGHTS) {
			int *fdp = (int *)CMSG_DATA(cmsg);
			int fd = *fdp;
			char chmsg[] = "chald message";
			size_t chlen = sizeof(chmsg) - 1;
			if (write(fd, chmsg, chlen) != chlen)
				err(-1, "write");
			close(fd);
		}
	}
	exit(0);
}

void scmtest(void)
{
	printf("scm rights test\n");

	int s[2];
	if (socketpair(AF_UNIX, SOCK_STREAM, 0, s) == -1)
		err(-1, "socketpair");
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	if (c == 0) {
		if (close(s[0]) == -1)
			err(-1, "close");
		_scmchald(s[1]);
	}
	if (close(s[1]) == -1)
		err(-1, "close");

	int fd;
	if ((fd = open("/tmp/wtf", O_CREAT | O_TRUNC | O_RDWR)) == -1)
		err(-1, "open");

	char buf[CMSG_SPACE(sizeof(int))];
	struct msghdr msg;
	struct iovec iov;
	memset(&msg, 0, sizeof(msg));
	memset(&iov, 0, sizeof(iov));
	char dur = 4;
	iov.iov_base = &dur;
	iov.iov_len = 1;
	msg.msg_iov = &iov;
	msg.msg_iovlen = 1;
	msg.msg_control = buf;
	msg.msg_controllen = sizeof(buf);
	struct cmsghdr *cmsg = CMSG_FIRSTHDR(&msg);
	cmsg->cmsg_len = CMSG_LEN(sizeof(int));
	cmsg->cmsg_level = SOL_SOCKET;
	cmsg->cmsg_type = SCM_RIGHTS;
	int *fdp = (int *)CMSG_DATA(cmsg);
	*fdp = fd;
	ssize_t r;
	if ((r = sendmsg(s[0], &msg, 0)) != 1)
		err(-1, "sendmsg %zd", r);
	char pmsg[] = "parent message";
	size_t plen = sizeof(pmsg) - 1;
	if (write(fd, pmsg, plen) != plen)
		err(-1, "write");
	int status;
	if (wait(&status) != c)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
	if (lseek(fd, 0, SEEK_SET) == -1)
		err(-1, "lseek");
	char chmsg[] = "chald message";
	size_t chlen = sizeof(chmsg) - 1;
	char rbuf[BSIZE];
	if ((r = read(fd, rbuf, sizeof(rbuf))) != plen + chlen)
		errx(-1, "par read %zd %zu", r, plen + chlen);
	char ok1[] = "chald messageparent message";
	char ok2[] = "parent messagechald message";
	if (strncmp(rbuf, ok1, sizeof(ok1) - 1) != 0 &&
	    strncmp(rbuf, ok2, sizeof(ok2) - 1) != 0)
		errx(-1, "bad data");
	if (close(fd) == -1)
		err(-1, "close");
	if (close(s[0]) == -1)
		err(-1, "close");

	printf("scm rights ok\n");
}

void mkstemptest(void)
{
	printf("mkstemp test\n");
	char buf[64];

	snprintf(buf, sizeof(buf), "/tmp/wtfXXXXX");
	if (mkstemp(buf) != -1 || errno != EINVAL)
		errx(-1, "mkstemp suceeded");
	snprintf(buf, sizeof(buf), "/tmp/wtfXXXXXX");
	int fd;
	if ((fd = mkstemp(buf)) == -1)
		err(-1, "mkstemp");
	char msg[] = "hulloo!";
	if (write(fd, msg, sizeof(msg)) != sizeof(msg))
		err(-1, "write");
	int fd2;
	if ((fd2 = open(buf, O_RDONLY)) == -1)
		err(-1, "open");
	if (read(fd2, buf, sizeof(buf)) != sizeof(msg))
		err(-1, "read");
	if (strncmp(buf, msg, sizeof(msg)) != 0)
		errx(-1, "msg mismatch");
	close(fd2);
	close(fd);

	snprintf(buf, sizeof(buf), "/tmp/dirXXXXXX");
	if (mkdtemp(buf) == NULL)
		err(-1, "mkdtemp");
	struct stat st;
	if (stat(buf, &st) == -1)
		err(-1, "stat");
	if (!S_ISDIR(st.st_mode))
		errx(-1, "should be dir");
	if (S_ISREG(st.st_mode))
		errx(-1, "should be dir");
	if (rmdir(buf) == -1)
		err(-1, "rmdir");

	printf("mkstemp test ok\n");
}

void getppidtest(void)
{
	printf("getppid test\n");
	int p[2];
	if (pipe(p) == -1)
		err(-1, "pipe");
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	if (c == 0) {
		if (close(p[0]) == -1)
			err(-1, "close");
		pid_t me = getpid();
		pid_t gc = fork();
		if (gc == -1)
			err(-1, "fork");
		if (gc != 0)
			exit(0);
		int ok = 0;
		int i;
		for (i = 0; i < 1000; i++) {
			if (getppid() != me) {
				ok = 1;
				break;
			}
			usleep(1000);
		}
		if (!ok) {
			errx(-1, "still getting old ppid %ld",
			    (long)getppid());
		}
		int ret = 0;
		if (write(p[1], &ret, sizeof(ret)) != sizeof(ret))
			err(-1, "write");
		exit(0);
	}
	if (close(p[1]) == -1)
		err(-1, "close");
	int ret;
	if (wait(&ret) != c)
		err(-1, "wrong child");
	if (!WIFEXITED(ret) || WEXITSTATUS(ret) != 0)
		errx(-1, "child failed");
	if (read(p[0], &ret, sizeof(int)) != sizeof(int) || ret != 0)
		err(-1, "grand child failed");
	if (close(p[0]) == -1)
		err(-1, "close");
	printf("getppid test ok\n");
}

static void _v1(char *p)
{
	int i;
	for (i = 0; i < 4096*2; i++) {
		if (i < 4096 + (4096/2)) {
			if (p[i] != 'A')
				errx(-1, "mismatch");
		} else {
			if (p[i] != 0)
				errx(-1, "mismatch");
		}
	}
}

void mmaptest(void)
{
	printf("mmap test\n");
	const char * const f = "/tmp/mmap.dur";
	unlink(f);
	int fd = open(f, O_WRONLY | O_CREAT | O_EXCL);
	if (fd == -1)
		err(-1, "open");
	int i;
	for (i = 0; i < 4096 + (4096/2); i++)
		if (write(fd, "A", 1) != 1)
			err(-1, "write");
	if (close(fd) == -1)
		err(-1, "close");
	if ((fd = open(f, O_RDONLY)) == -1)
		err(-1, "open");
	char *p = mmap(0, 4096*2, PROT_READ, MAP_PRIVATE, fd, 0);
	if (p == MAP_FAILED)
		err(-1, "mmap");
	_v1(p);
	if (munmap(p, 4096*2) == -1)
		err(-1, "munmap");
	// should be able to map file opened read-only with private writable
	// mapping
	p = mmap(0, 4096*2, PROT_READ | PROT_WRITE, MAP_PRIVATE, fd, 0);
	if (p == MAP_FAILED)
		err(-1, "mmap");
	if (close(fd) == -1)
		err(-1, "close");
	_v1(p);
	for (i = 0; i < 4096*2; i++)
		p[i] = 'Z';
	if (munmap(p, 4096*2) == -1)
		err(-1, "munmap");

	if ((fd = open(f, O_RDONLY)) == -1)
		err(-1, "open");
	p = mmap(0, 4096*3, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
	if (p != MAP_FAILED)
		err(-1, "mmap succeeded");
	if (close(fd) == -1)
		err(-1, "close");

	if ((fd = open(f, O_RDWR)) == -1)
		err(-1, "open");
	p = mmap(0, 4096*3, PROT_READ | PROT_WRITE, MAP_SHARED, fd, 0);
	if (p == MAP_FAILED)
		err(-1, "mmap");
	if (close(fd) == -1)
		err(-1, "close");

	_v1(p);
	for (i = 0; i < 4096*2; i++)
		p[i] = 'Z';
	if (munmap(p, 4096*2) == -1)
		err(-1, "munmap");

	if ((fd = open(f, O_RDWR)) == -1)
		err(-1, "open");
	char b;
	for (i = 0; i < 4096 + (4096/2); i++)
		if (read(fd, &b, 1) != 1 || b != 'Z')
			err(-1, "read");
	if (close(fd) == -1)
		err(-1, "close");

	if ((fd = open(f, O_RDONLY)) == -1)
		err(-1, "open");
	p = mmap(0, 4096, PROT_READ, MAP_SHARED, fd, 4096);
	if (p == MAP_FAILED)
		err(-1, "mmap");
	if (close(fd) == -1)
		err(-1, "close");
	pid_t c = fork();
	if (c == -1)
		err(-1, "fork");
	else if (c == 0) {
		if ((fd = open(f, O_WRONLY)) == -1)
			err(-1, "open");
		if (lseek(fd, 4096, SEEK_SET) == -1)
			err(-1, "lseek");
		char b = 'B';
		for (i = 0; i < 4096; i++)
			if (write(fd, &b, 1) != 1)
				err(-1, "write");
		if (close(fd) == -1)
			err(-1, "close");
		if (unlink(f) == -1)
			err(-1, "unlink");
		if ((fd = open(f, O_WRONLY | O_CREAT | O_EXCL)) == -1)
			err(-1, "open");
		b = 'X';
		for (i = 0; i < 4096*4; i++)
			if (write(fd, &b, 1) != 1)
				err(-1, "write");
		if (close(fd) == -1)
			err(-1, "close");
		if (unlink(f) == -1)
			err(-1, "unlink");
		exit(0);
	}
	int status;
	if (wait(&status) != c)
		err(-1, "wait");
	if (!WIFEXITED(status) || WEXITSTATUS(status) != 0)
		errx(-1, "child failed");
	for (i = 0; i < 4096; i++)
		if (p[i] != 'B')
			errx(-1, "mismatch");
	if (munmap(p, 4096*2) != -1)
		err(-1, "munmap succeeded");
	if (munmap(p, 4096) == -1)
		err(-1, "munmap");
	printf("mmap test ok\n");
}


void
logtest()
{
  #define N 1000
  #define NSYNC 20
  char *buf = malloc(BSIZE*N);
  #define NPROC 5

  printf("log test\n");

  char *names[NPROC] = { "f0", "f1", "f2", "f3", "f4" };

  for(int pi = 0; pi < NPROC; pi++){
    int pid = fork();
    if(pid < 0){
      err(-1, "fork failed\n");
    }
    if(pid == 0){
      char *fname = names[pi];
      int fd = open(fname, O_CREATE | O_RDWR);
      if(fd < 0){
	err(-1, "create failed\n");
      }
      unlink(fname);

      for (int i = 0; i < N; i ++) {
	if (write(fd, buf+i*BSIZE, BSIZE) != BSIZE)
	  err(-1, "write");
	if (i % NSYNC == 0) {
	  fsync(fd);
	}
      }

      exit(0);
    }
  }

  for(int pi = 0; pi < NPROC; pi++){
    wait(NULL);
  }

  free(buf);
    
  printf("log test OK\n");
}

enum {
	KREAD,
	KWRITE,
	KACCEPT,
	KFUTEX,
	KWAIT,
	KPOLL,
};

static void guineapig(int fd, int op)
{
	char buf[4096*2];
	struct sockaddr_in saddr;
	socklen_t slen = sizeof(saddr);
	pthread_mutex_t mut = PTHREAD_MUTEX_INITIALIZER;
	pid_t par;

	switch (op) {
	case KREAD:
		read(fd, buf, sizeof buf);
		break;
	case KWRITE:
		write(fd, buf, sizeof buf);
		break;
	case KACCEPT:
		accept(fd, (struct sockaddr *)&saddr, &slen);
		break;
	case KFUTEX:
		pthread_mutex_lock(&mut);
		pthread_mutex_lock(&mut);
		break;
	case KWAIT:
		par = getpid();
		switch (fork()) {
		case -1:
			err(-1, "fork");
		case 0:
			sleep(1);
			if (kill(par) == -1)
				err(-1, "kill");
			sleep(5);
			exit(0);
		}
		wait(NULL);
		break;
	case KPOLL:
		if (poll(NULL, 0, -1) == -1)
			err(-1, "poll");
		break;
	default:
		errx(-1, "no op");
	}

	errx(-1, "child op returned %d", op);
}

static void kill1(int fd, int op)
{
	pid_t kc = fork();
	switch (kc) {
	case -1:
		err(-1, "fork");
	case 0:
		guineapig(fd, op);
	default:
		{
		// wait for child to hopefully block in syscall
		usleep(500000);
		if (op != KWAIT)
			kill(kc);

		int died = 0;
		for (int i = 0; i < 3000/10; i++) {
			int status;
			pid_t r = waitpid(kc, &status, WNOHANG);
			if (r == -1)
				err(-1, "waitpid");
			if (r == kc) {
				if (WIFEXITED(status))
					errx(-1, "exited?");
				died = 1;
				break;
			}
			usleep(10000);
		}
		if (!died)
			errx(-1, "killed child still lives!");
		}
	}
}

void killtest(void)
{
	printf("kill test\n");

	int pip[2];
	if (pipe(pip) == -1)
		err(-1, "pipe");

	kill1(pip[0], KREAD);
	kill1(pip[1], KWRITE);
	close(pip[0]);
	close(pip[1]);

	int sp[2];
	if (socketpair(AF_UNIX, SOCK_STREAM, 0, sp) == -1)
		err(-1, "socketpair");

	kill1(sp[0], KREAD);
	kill1(sp[1], KWRITE);
	close(sp[0]);
	close(sp[1]);

	int s = socket(AF_INET, SOCK_STREAM, 0);
	if (s == -1)
		err(-1, "socket");
	struct sockaddr_in saddr = {};
	saddr.sin_family = AF_INET;
	saddr.sin_addr.s_addr = INADDR_ANY;
	saddr.sin_port = htons(8080);
	if (bind(s, (struct sockaddr *)&saddr, sizeof saddr) == -1)
		err(-1, "bind");
	if (listen(s, 10) == -1)
		err(-1, "listen");

	kill1(s, KACCEPT);
	close(s);

	kill1(-1, KFUTEX);
	kill1(-1, KWAIT);

	printf("kill test passed\n");
}

void lstats(void)
{
	printf("lstat test\n");
	char temp[] = "/tmp/wtfXXXXXX";
	int fd = mkstemp(temp);
	if (fd == -1)
		err(-1, "mkstemp");
	struct stat st1 = {0}, st2 = {0};
	if (fstat(fd, &st1) == -1)
		err(-1, "fstat");
	if (lstat(temp, &st2) == -1)
		err(-1, "fstat");
	if (memcmp(&st1, &st2, sizeof st1) != 0)
		errx(-1, "not equal");
	close(fd);
	printf("lstat test passed\n");
}

int
main(int argc, char *argv[])
{
  printf("usertests starting\n");

  if(open("usertests.ran", 0) >= 0){
    printf("already ran user tests -- rebuild fs.img\n");
    exit(0);
  }
  close(open("usertests.ran", O_CREATE));

  logtest();
  createdelete();
  linkunlink();
  concreate();
  fourfiles();
  sharedfd();
  renametest();
  lseektest();
  dirtest();

  bigargtest();
  bigwrite();
  bigargtest();
  bsstest();
  printf("skipping sbrk test\n");
  //sbrktest();
  validatetest();

  //rusagetest();

  opentest();
  writetest();
  // writetest1();    // make sure disk is bigger then MAXFILE blocks
  createtest();

  openiputtest();
  exitiputtest();
  iputtest();

  mem();
  pipe1();
  preempt();
  exitwait();

  rmdot();
  fourteen();
  bigfile();
  subdir();
  linktest();
  unlinkread();
  dirfile();
  iref();
  forktest();
  bigdir(); // slow

  posixtest();
  barriertest();
  threadwait();
  
  pthreadfd();
  
  fnonblock();
  preadwrite();
  stdiotest();
  realloctest();
  trunctest();

  cwdtest();

  polltest();
  runsockettest();
  accesstest();
  futextest();

  lazyfaults();
  forktlb();
  mapshared();
  spair();
  iovtest();
  scmtest();
  mkstemptest();
  getppidtest();
  mmaptest();

  killtest();
  lstats();

  exectest();

  return 0;
}
