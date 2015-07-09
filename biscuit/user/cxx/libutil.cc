// Miscellaneous utilities

#include "libutil.h"

#include <stdarg.h>
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdarg.h>
#include <string.h>
#include <unistd.h>
#include <sys/time.h>

static void __attribute__((noreturn))
vdie(const char* errstr, va_list ap)
{
  vfprintf(stderr, errstr, ap);
  fprintf(stderr, "\n");
  exit(1);
}

void __attribute__((noreturn))
die(const char* errstr, ...)
{
  va_list ap;

  va_start(ap, errstr);
  vdie(errstr, ap);
}

void
edie(const char* errstr, ...)
{
  va_list ap;

  va_start(ap, errstr);
#ifdef XV6_USER
  // There is no errno on xv6
  vdie(errstr, ap);
#else
  vfprintf(stderr, errstr, ap);
  va_end(ap);
  fprintf(stderr, ": %s\n", strerror(errno));
  exit(1);
#endif
}

size_t
xread(int fd, void *buf, size_t n)
{
  size_t pos = 0;
  while (pos < n) {
    int r = read(fd, (char*)buf + pos, n - pos);
    if (r < 0)
      edie("read failed");
    if (r == 0)
      break;
    pos += r;
  }
  return pos;
}

void
xwrite(int fd, const void *buf, size_t n)
{
  int r;

  while (n) {
    r = write(fd, buf, n);
    if (r < 0 || r == 0)
      edie("write failed");
    buf = (char *) buf + r;
    n -= r;
  }
}

uint64_t
now_usec(void)
{
  struct timeval tv;
  if (gettimeofday(&tv, nullptr) < 0)
    edie("gettimeofday");
  return tv.tv_sec * 1000000ull + tv.tv_usec;
}

#if !defined(XV6_USER)
// setaffinity is a syscall in xv6, but not standard in Linux
int
setaffinity(int c)
{
  cpu_set_t cpuset;
  CPU_ZERO(&cpuset);
  CPU_SET(c, &cpuset);
  if (sched_setaffinity(0, sizeof(cpuset), &cpuset) < 0)
    edie("setaffinity, sched_setaffinity failed");
  return 0;
}

ulong
rdtsc(void)
{
	ulong low, hi;
	asm volatile(
	    "rdtsc\n"
	    : "=a"(low), "=d"(hi)
	    :
	    :);
	return hi << 32 | low;
}
#endif
