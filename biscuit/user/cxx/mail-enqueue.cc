// mail-enqueue - queue a mail message for delivery

#include "max_align.h"

#include "libutil.h"
#include "shutil.h"

#include <fcntl.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/types.h>
#include <sys/un.h>

#include <string>

using std::string;

class spool_writer
{
  string spooldir_;
  int notifyfd_;
  struct sockaddr_un notify_addr_;
  socklen_t notify_len_;

public:
  spool_writer(const string &spooldir) : spooldir_(spooldir), notify_addr_{}
  {
    notifyfd_ = socket(AF_UNIX, SOCK_DGRAM, 0);
    if (notifyfd_ < 0)
      edie("socket failed");

    notify_addr_.sun_family = AF_UNIX;
    snprintf(notify_addr_.sun_path, sizeof notify_addr_.sun_path,
             "%s/notify", spooldir.c_str());
    notify_len_ = SUN_LEN(&notify_addr_);
  }

  void queue(int msgfd, const char *recipient, size_t limit = (size_t)-1)
  {
    // Create temporary message
    char tmpname[256];
    snprintf(tmpname, sizeof tmpname,
             "%s/pid/%d", spooldir_.c_str(), (int)getpid());
    int tmpfd = open(tmpname, O_CREAT|O_EXCL|O_WRONLY, 0600);
    if (tmpfd < 0)
      edie("open %s failed", tmpname);

    if (copy_fd_n(tmpfd, msgfd, limit) < 0)
      edie("copy_fd message failed");

    struct stat st;
    if (fstatx(tmpfd, &st, STAT_OMIT_NLINK) < 0)
      edie("fstat failed");

    fsync(tmpfd);
    close(tmpfd);

    // Create envelope
    char envname[256];
    snprintf(envname, sizeof envname,
             "%s/todo/%lu", spooldir_.c_str(), (unsigned long)st.st_ino);
    int envfd = open(envname, O_CREAT|O_EXCL|O_WRONLY, 0600);
    if (envfd < 0)
      edie("open %s failed", envname);
    xwrite(envfd, recipient, strlen(recipient));
    fsync(envfd);
    close(envfd);

    // Move message into spool
    char msgname[256];
    snprintf(msgname, sizeof msgname,
             "%s/mess/%lu", spooldir_.c_str(), (unsigned long)st.st_ino);
    if (rename(tmpname, msgname) < 0)
      edie("rename %s %s failed", tmpname, msgname);

    // Notify queue manager.  We don't need an acknowledgment because
    // we've already "durably" queued the message.
    char notif[16];
    snprintf(notif, sizeof notif, "%lu", (unsigned long)st.st_ino);
    if (sendto(notifyfd_, notif, strlen(notif), 0,
               (struct sockaddr*)&notify_addr_, notify_len_) < 0)
      edie("send failed");
  }

  void queue_exit()
  {
    const char *msg = "EXIT";
    if (sendto(notifyfd_, msg, strlen(msg), 0,
               (struct sockaddr*)&notify_addr_, notify_len_) < 0)
      edie("send exit failed");
  }
};

static void
do_batch_mode(int fd, int resultfd, const char *recip, spool_writer *spool)
{
  while (1) {
    uint64_t msg_len;
    size_t len = xread(fd, &msg_len, sizeof msg_len);
    if (len == 0)
      break;
    if (len != sizeof msg_len)
      die("short read of message length");
    spool->queue(fd, recip, msg_len);

    // Indicate success
    uint64_t res = 0;
    xwrite(resultfd, &res, sizeof res);
  }
}

static void
usage(const char *argv0)
{
  fprintf(stderr, "Usage: %s spooldir recipient <message\n", argv0);
  fprintf(stderr, "       %s -b spooldir recipient <message-stream\n", argv0);
  fprintf(stderr, "       %s --exit spooldir\n", argv0);
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
  if (argc == 3 && strcmp(argv[1], "--exit") == 0) {
    spool_writer spool{argv[2]};
    spool.queue_exit();
    return 0;
  }

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

  const char *spooldir = argv[optind];
  const char *recip = argv[optind + 1];

  spool_writer spool{spooldir};
  if (batch_mode)
    do_batch_mode(0, 1, recip, &spool);
  else
    spool.queue(0, recip);

  return 0;
}
