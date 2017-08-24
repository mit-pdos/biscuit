// mail-qman - deliver mail messages from the queue

// The spool directory has the following structure:
// * pid/<pid> - message files under construction
// * mess/<inumber> - message files
// * todo/<message inumber> - envelope files
// * notify - a UNIX socket that receives an <inumber> when a message
//   is added to the spool


#include "max_align.h"

#include "libutil.h"
#include "shutil.h"
//#include "xsys.h"

#include <err.h>
#include <fcntl.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <unistd.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <sys/wait.h>

#include <stdexcept>
#include <string>
#include <thread>

#define SOCK_DGRAM_UNORDERED	(-1)
#define O_ANYFD			(0)

using std::string;
using std::thread;

extern char **environ;

static bool alt;

class spool_reader
{
  string spooldir_;
  int notifyfd_;
  struct sockaddr_un notify_sun_;

public:
  spool_reader(const string &spooldir) : spooldir_(spooldir)
  {
    // Create notification socket
    notifyfd_ = socket(AF_UNIX, alt ? SOCK_DGRAM_UNORDERED : SOCK_DGRAM, 0);
    if (notifyfd_ < 0)
      edie("socket failed");
    struct sockaddr_un sun{};
    sun.sun_family = AF_UNIX;
    snprintf(sun.sun_path, sizeof sun.sun_path, "%s/notify", spooldir.c_str());

    // Normally we would just unlink(sun.sun_path), but since there's
    // no way to kill mail-qman on xv6 right now, if it exists, it
    // means this is a duplicate.
    struct stat st;
    if (stat(sun.sun_path, &st) == 0)
      die("%s exists; mail-qman already running?", sun.sun_path);

    unlink(sun.sun_path);
    if (bind(notifyfd_, (struct sockaddr*)&sun, SUN_LEN(&sun)) < 0)
      edie("bind failed");

    notify_sun_ = sun;
  }

  string dequeue()
  {
    char buf[256];
    ssize_t r = recv(notifyfd_, buf, sizeof buf, 0);
    if (r < 0)
      edie("recv failed");
    return {buf, (size_t)r};
  }

  string get_recipient(const string &id)
  {
    char path[256];
    snprintf(path, sizeof path, "%s/todo/%s", spooldir_.c_str(), id.c_str());
    int fd = open(path, O_RDONLY|O_CLOEXEC|(alt ? O_ANYFD : 0));
    if (fd < 0)
      edie("open %s failed", path);
    struct stat st;
    // We don't use "alt" here, even though this is an alternate
    // interface, because this commutes regardless.  Passing
    // STAT_OMIT_NLINK is just for performance.
    // XXX Maybe we should use a resizing read rather than fstat.
    if (fstatx(fd, &st, STAT_OMIT_NLINK) < 0)
      edie("fstat %s failed", path);
    string res(st.st_size, 0);
    if (readall(fd, &res.front(), res.size()) != (ssize_t)res.size())
      edie("readall %s failed", path);
    close(fd);
    return res;
  }

  int open_message(const string &id)
  {
    char path[256];
    snprintf(path, sizeof path, "%s/mess/%s", spooldir_.c_str(), id.c_str());
    int fd = open(path, O_RDONLY|O_CLOEXEC|(alt ? O_ANYFD : 0));
    if (fd < 0)
      edie("open %s failed", path);
    return fd;
  }

  void remove(const string &id)
  {
    string x;
    x.append(spooldir_).append("/todo/").append(id);
    unlink(x.c_str());
    x.clear();
    x.append(spooldir_).append("/mess/").append(id);
    unlink(x.c_str());
  }

  void exit_others(int mycpu, int nthread)
  {
    // xv6 doesn't have an easy way to kill processes, so cascade the
    // exit message to all other threads. We have to affinitize
    // ourselves around in case socket load balancing is off.
    int notifyfd = socket(AF_UNIX, SOCK_DGRAM, 0);
    if (notifyfd < 0)
      edie("%s: socket failed", __func__);

    const char *killmsg = "EXIT2";
    for (int i = 0; i < nthread; ++i) {
      if (i == mycpu)
        continue;
      //setaffinity(i);
      if (sendto(notifyfd, killmsg, strlen(killmsg), 0,
                 (struct sockaddr*)&notify_sun_, SUN_LEN(&notify_sun_)) < 0)
        edie("%s: sendto failed", __func__);
    }

    close(notifyfd);

    //setaffinity(mycpu);
  }
};

class deliverer
{
  pid_t pid_;
  string mailroot_;
  bool pool_;
  string pool_recipient_;
  int msgpipe[2], respipe[2];

  void start_child(const char *argv[], int stdin, int stdout)
  {
    if (alt) {
#if defined(XV6_USER)
      // xv6 doesn't define errno.
      int errno = 0;
#endif
      posix_spawn_file_actions_t actions;
      if ((errno = posix_spawn_file_actions_init(&actions)))
        edie("posix_spawn_file_actions_init failed");
      if (stdin >= 0)
        if ((errno = posix_spawn_file_actions_adddup2(&actions, stdin, 0)))
          edie("posix_spawn_file_actions_adddup2 failed");
      if (stdout >= 0)
        if ((errno = posix_spawn_file_actions_adddup2(&actions, stdout, 1)))
          edie("posix_spawn_file_actions_adddup2 failed");
      if ((errno = posix_spawn(&pid_, argv[0], &actions, nullptr,
                               const_cast<char *const*>(argv), environ)))
        edie("posix_spawn failed");
      if ((errno = posix_spawn_file_actions_destroy(&actions)))
        edie("posix_spawn_file_actions_destroy failed");
    } else {
      pid_ = fork();
      if (pid_ < 0)
        edie("fork failed");
      if (pid_ == 0) {
        // Note that this doesn't handle the case where stdin/stdout are
        // 0/1 and have O_CLOEXEC set, but that never happens here.
        if (stdin >= 0 && dup2(stdin, 0) < 0)
          edie("dup2 stdin failed");
        if (stdout >= 0 && dup2(stdout, 1) < 0)
          edie("dup2 stdout failed");
        execv(argv[0], const_cast<char *const*>(argv));
        edie("execv %s failed", argv[0]);
      }
    }
  }

  void wait_child()
  {
    if (pool_) {
      close(msgpipe[1]);
      close(respipe[0]);
    }

    int status;
    if (waitpid(pid_, &status, 0) < 0)
      edie("waitpid failed");
    if (!WIFEXITED(status) || WEXITSTATUS(status))
      die("deliver failed: status %d", status);
    pid_ = 0;
  }

public:
  deliverer(const string &mailroot, bool pool)
    : pid_(0), mailroot_(mailroot), pool_(pool) { }

  ~deliverer()
  {
    if (pid_)
      wait_child();
  }

  void
  deliver(const string &recipient, int msgfd)
  {
    if (pool_) {
      if (pid_ && recipient != pool_recipient_)
        // Restart child if recipient changed
        wait_child();

      if (!pid_) {
        // Start batch-mode deliver process
        const char *argv[] = {EP("mail-deliver"), "-b", mailroot_.c_str(),
                              recipient.c_str(), nullptr};
        pool_recipient_ = recipient;
        //if (pipe2(msgpipe, O_CLOEXEC|O_ANYFD) < 0)
        if (pipe2(msgpipe, O_CLOEXEC) < 0)
          edie("pipe msgpipe failed");
        //if (pipe2(respipe, O_CLOEXEC|O_ANYFD) < 0)
        if (pipe2(respipe, O_CLOEXEC) < 0)
          edie("pipe respipe failed");
        start_child(argv, msgpipe[0], respipe[1]);
        close(msgpipe[0]);
        close(respipe[1]);
      }

      // Get length of message
      uint64_t msg_len;
      if ((msg_len = fd_len(msgfd)) < 0)
        edie("mail-qman: failed to get length of message");

      // Deliver message to running deliver process
      xwrite(msgpipe[1], &msg_len, sizeof msg_len);
      ssize_t r = copy_fd_n(msgpipe[1], msgfd, msg_len);
      if (r < 0)
        edie("mail-qman: copy_fd_n to mail-deliver failed");
      else if ((uint64_t)r < msg_len)
        die("mail-qman: short write to mail-deliver (%zd < %zu)",
            r, (size_t)msg_len);

      // Get batch-mode response
      uint64_t res;
      if (xread(respipe[0], &res, sizeof res) != sizeof res)
        die("mail-qman: short read of deliver result code");
      if (res != 0)
        die("mail-qman: mail-deliver returned status %d", (int)res);
    } else {
      const char *argv[] = {EP("mail-deliver"), mailroot_.c_str(),
                            recipient.c_str(), nullptr};
      start_child(argv, msgfd, -1);
      wait_child();
    }
  }
};

static void
do_process(spool_reader *spool, const string &mailroot, bool pool,
           int nthread, int cpu)
{
  deliverer d{mailroot, pool};
  while (true) {
    string id = spool->dequeue();
    if (id == "EXIT") {
      spool->exit_others(cpu, nthread);
      return;
    }
    if (id == "EXIT2")
      return;
    string recip = spool->get_recipient(id);
    int msgfd = spool->open_message(id);
    d.deliver(recip, msgfd);
    close(msgfd);
    spool->remove(id);
  }
}

static void
usage(const char *argv0)
{
  fprintf(stderr, "Usage: %s [options] spooldir mailroot nthread\n", argv0);
  fprintf(stderr, "  -a none   Use regular APIs (default)\n");
  fprintf(stderr, "     all    Use alternate APIs\n");
  fprintf(stderr, "  -p        Use pooled mail-deliver\n");
  exit(2);
}

int
main(int argc, char **argv)
{
  int opt;
  bool pool = false;
  while ((opt = getopt(argc, argv, "a:p")) != -1) {
    switch (opt) {
    case 'a':
      if (strcmp(optarg, "all") == 0) {
	errx(-1, "alt interface not supported");
        alt = true;
      } else if (strcmp(optarg, "none") == 0)
        alt = false;
      else
        usage(argv[0]);
      break;
    case 'p':
      pool = true;
      break;
    default:
      usage(argv[0]);
    }
  }

  if (argc - optind != 3)
    usage(argv[0]);

  const char *spooldir = argv[optind];
  const char *mailroot = argv[optind+1];
  int nthread = atoi(argv[optind+2]);
  if (nthread <= 0)
    usage(argv[0]);

  spool_reader reader{spooldir};

  thread *threads = new thread[nthread];

  for (int i = 0; i < nthread; ++i) {
    //setaffinity(i);
    threads[i] = std::move(thread(do_process, &reader, mailroot, pool,
                                  nthread, i));
  }

  for (int i = 0; i < nthread; ++i)
    threads[i].join();
  return 0;
}
