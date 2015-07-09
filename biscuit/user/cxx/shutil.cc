#include "shutil.h"
#include "libutil.h"

#include <err.h>
#include <errno.h>
#include <unistd.h>
#include <sys/stat.h>
#include <sys/types.h>

ssize_t
writeall(int fd, const void *buf, size_t n)
{
  size_t pos = 0;
  while (pos < n) {
    ssize_t r = write(fd, (char*)buf + pos, n - pos);
    assert(r != 0);
    if (r < 0) {
#if !defined(XV6_USER)
      if (errno == EINTR)
        continue;
#endif
      break;
    }
    pos += r;
  }
  return pos;
}

ssize_t
readall(int fd, void *buf, size_t n)
{
  size_t pos = 0;
  while (pos < n) {
    ssize_t r = read(fd, (char*)buf + pos, n - pos);
    if (r < 0) {
#if !defined(XV6_USER)
      if (errno == EINTR)
        continue;
#endif
      if (pos == 0)
        return -1;
      break;
    }
    if (r == 0)
      break;
    pos += r;
  }
  return pos;
}

// Read from src until EOF, write to dst.  Returns number of bytes
// copied on success, < 0 on failure.
ssize_t
copy_fd(int dst, int src)
{
  return copy_fd_n(dst, src, (size_t)-1);
}

// Read from src until EOF or limit bytes, write to dst.  Returns
// number of bytes copied on success, < 0 on failure.
ssize_t
copy_fd_n(int dst, int src, size_t limit)
{
  size_t res = 0;
  char buf[4096];
  while (res < limit) {
    int r = read(src, buf, limit - res < sizeof buf ? limit - res : sizeof buf);
    if (r < 0) {
#if !defined(XV6_USER)
      if (errno == EINTR)
        continue;
#endif
      return r;
    }
    if (r == 0)
      return res;
    int r2 = writeall(dst, buf, r);
    if (r != r2)
      return -1;
    res += r;
  }
  return res;
}

int
mkdir_if_noent(const char *path, mode_t mode)
{
  int r = mkdir(path, mode);
  if (r < 0) {
#if !defined(XV6_USER)
    // xv6 doesn't have errno
    if (errno != EEXIST)
      return -1;
#endif
  }
  return 0;
}

// Return the length of the file referred to by fd.  Returns < 0 on
// failure.
ssize_t
fd_len(int fd)
{
  // Could also do this with fstat, but that returns way more
  // information than we need
  off_t pos = lseek(fd, 0, SEEK_CUR);
  if (pos < 0)
    return -1;
  off_t end = lseek(fd, 0, SEEK_END);
  if (end < 0)
    return -1;
  if (lseek(fd, pos, SEEK_SET))
    edie("failed to return file offset to original position");
  return end;
}
