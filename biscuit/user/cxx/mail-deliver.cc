#include "max_align.h"

#include "libutil.h"
#include "shutil.h"
//#include "xsys.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <sys/stat.h>

#include <string>

using std::string;

class maildir_writer
{
  string maildir_;
  unsigned long seqno_;

public:
  maildir_writer(const string &maildir) : maildir_(maildir), seqno_(0)
  {
    // Check for mailbox
    struct stat st;
    if (stat(maildir.c_str(), &st) < 0 ||
        (st.st_mode & S_IFMT) != S_IFDIR)
      die("No such mailbox: %s", maildir.c_str());
  }

  void deliver(int msgfd, size_t limit = (size_t)-1)
  {
    // Generate unique tmp path
    char unique[16];
    snprintf(unique, sizeof(unique), "%d.%lu", (int)getpid(), seqno_);
    string tmppath(maildir_);
    tmppath.append("/tmp/").append(unique);
    ++seqno_;

    // Write message
    int fd = open(tmppath.c_str(), O_CREAT|O_EXCL|O_WRONLY, 0600);
    if (fd < 0)
      edie("open %s failed", tmppath.c_str());
    if (copy_fd_n(fd, 0, limit) < 0)
      edie("copy_fd failed");
    struct stat st;
    if (fstatx(fd, &st, STAT_OMIT_NLINK) < 0)
      edie("fstat %s failed", tmppath.c_str());
    fsync(fd);
    close(fd);

    // Deliver message
    snprintf(unique, sizeof(unique), "%lu", (unsigned long)st.st_ino);
    string newpath(maildir_);
    newpath.append("/new/").append(unique);
    if (rename(tmppath.c_str(), newpath.c_str()) < 0)
      edie("rename %s %s failed", tmppath.c_str(), newpath.c_str());
  }
};

static void
do_batch_mode(int fd, int resultfd, maildir_writer *writer)
{
  while (1) {
    uint64_t msg_len;
    size_t len = xread(fd, &msg_len, sizeof msg_len);
    if (len == 0)
      break;
    if (len != sizeof msg_len)
      die("short read of message length");
    writer->deliver(fd, msg_len);

    // Indicate success
    uint64_t res = 0;
    xwrite(resultfd, &res, sizeof res);
  }
}

static void
usage(const char *argv0)
{
  fprintf(stderr, "Usage: %s mailroot user <message\n", argv0);
  fprintf(stderr, "Usage: %s -b mailroot user <message-stream\n", argv0);
  fprintf(stderr, "\n");
  fprintf(stderr,
          "In batch mode, message-stream is a sequence of <u64 N><char[N]> and\n"
          "a <u64> return code will be written to stdout after every delivery.\n"
    );
  exit(2);
}

int
main(int argc, char **argv)
{
  int opt;
  bool batch_mode = false;
  while ((opt = getopt(argc, argv, "b")) != -1) {
    switch (opt) {
    case 'b':
      batch_mode = true;
      break;
    default:
      usage(argv[0]);
    }
  }

  if (argc - optind != 2)
    usage(argv[0]);

  const char *mailroot = argv[optind];
  const char *user = argv[optind + 1];

  // Get user's mailbox
  string maildir(mailroot);
  maildir.append("/").append(user);
  maildir_writer writer{maildir};

  // Deliver message(s)
  if (batch_mode)
    do_batch_mode(0, 1, &writer);
  else
    writer.deliver(0);

  return 0;
}
