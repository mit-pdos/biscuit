#include <litc.h>

static long
nowms(void)
{
  struct timeval tv;
  if (gettimeofday(&tv, NULL))
    errx(-1, "nowms");
  return tv.tv_sec*1000 + tv.tv_usec/1000;
}

#define SZ  (1 << 22)
#define N  1024
	    
int main(int argc, char **argv)
{

  printf("mmapbench\n");
  
  const size_t sz = SZ;
  long tot = 0;
  for (int i = 0 ; i < N; i++) {
    char *p = mmap(NULL, sz, PROT_READ | PROT_WRITE,
		   MAP_ANON | MAP_PRIVATE, -1, 0);
    if (p == MAP_FAILED)
      errx(-1, "mmap failed");

    long st = nowms();
    for (int j = 0; j < sz; j += 4096) {
      p[j] = 0xcc;
    }

    tot += nowms() - st;

    if (munmap(p, sz) != 0)
      errx(-1, "munmap failed");
  }
  printf("mmapbench %ld pgfaults in %ld ms\n", (long) (SZ/4096) * N, tot);

  return 0;
}
