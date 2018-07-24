#include <litc.h>

static long
nowms(void)
{
  struct timeval tv;
  if (gettimeofday(&tv, NULL)) {
    perror("gettimeofday");
    exit(-1);
  }
  return tv.tv_sec*1000 + tv.tv_usec/1000;
}

int main(int argc, char **argv)
{

  printf("mmapbench\n");
  
  const size_t sz = 1 << 24;
  
  char *p = mmap(NULL, sz, PROT_READ | PROT_WRITE,
		 MAP_ANON | MAP_PRIVATE, -1, 0);
  if (p == MAP_FAILED) {
    perror("map failed");
    exit(-1);
  }
  long st = nowms();
  for (int i = 0; i < sz; i += 4096) {
    p[i] = 0xcc;
  }
  long tot = nowms() - st;
  printf("mmapbench done %ld ms\n", tot);

  return 0;
}
