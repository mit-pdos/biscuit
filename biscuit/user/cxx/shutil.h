#pragma once

#include <sys/types.h>

ssize_t writeall(int fd, const void *buf, size_t n);
ssize_t readall(int fd, void *buf, size_t n);
ssize_t copy_fd(int dst, int src);
ssize_t copy_fd_n(int dst, int src, size_t limit);
int mkdir_if_noent(const char *path, mode_t mode);
ssize_t fd_len(int fd);
