#include "max_align.h"

//#include "amd64.h"
#include "distribution.hh"
#include "spinbarrier.hh"
#include "libutil.h"
//#include "xsys.h"

//#include <litc.h>

#include <fcntl.h>
#include <spawn.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <sys/stat.h>
#include <sys/wait.h>

#include <string>
#include <thread>
#include <vector>

// Set to 1 to manage the queue manager's life time from this program.
// Set to 0 if the queue manager is started and stopped outside of
// mailbench.
#define START_QMAN 1

using std::string;

enum { warmup_secs = 1 };
//enum { duration = 5 };
int duration = 5;

const char *message =
  "Received: from incoming.csail.mit.edu (incoming.csail.mit.edu [128.30.2.16])\n"
  "        by metroplex (Cyrus v2.2.13-Debian-2.2.13-14+lenny5) with LMTPA;\n"
  "        Tue, 19 Mar 2013 22:45:50 -0400\n"
  "X-Sieve: CMU Sieve 2.2\n"
  "Received: from mailhub-auth-3.mit.edu ([18.9.21.43])\n"
  "        by incoming.csail.mit.edu with esmtps\n"
  "        (TLS1.0:DHE_RSA_AES_256_CBC_SHA1:32)\n"
  "        (Exim 4.72)\n"
  "        (envelope-from <xxxxxxxx@MIT.EDU>)\n"
  "        id 1UI92E-0007D2-7N\n"
  "        for xxxxxxxxx@csail.mit.edu; Tue, 19 Mar 2013 22:45:50 -0400\n"
  "Received: from outgoing.mit.edu (OUTGOING-AUTH-1.MIT.EDU [18.9.28.11])\n"
  "        by mailhub-auth-3.mit.edu (8.13.8/8.9.2) with ESMTP id r2K2jnO5025684\n"
  "        for <xxxxxxxx@mit.edu>; Tue, 19 Mar 2013 22:45:49 -0400\n"
  "Received: from xxxxxxxxx.csail.mit.edu (xxxxxxxxx.csail.mit.edu [18.26.4.91])\n"
  "        (authenticated bits=0)\n"
  "        (User authenticated as xxxxxxxx@ATHENA.MIT.EDU)\n"
  "        by outgoing.mit.edu (8.13.8/8.12.4) with ESMTP id r2K2jmc7032022\n"
  "        (version=TLSv1/SSLv3 cipher=DHE-RSA-AES128-SHA bits=128 verify=NOT)\n"
  "        for <xxxxxxxx@mit.edu>; Tue, 19 Mar 2013 22:45:49 -0400\n"
  "Received: from xxxxxxx by xxxxxxxxx.csail.mit.edu with local (Exim 4.80)\n"
  "        (envelope-from <xxxxxxxx@mit.edu>)\n"
  "        id 1UI92C-0000il-4L\n"
  "        for xxxxxxxx@mit.edu; Tue, 19 Mar 2013 22:45:48 -0400\n"
  "From: Austin Clements <xxxxxxxx@MIT.EDU>\n"
  "To: xxxxxxxx@mit.edu\n"
  "Subject: Test message\n"
  "User-Agent: Notmuch/0.15+6~g7d4cb73 (http://notmuchmail.org) Emacs/23.4.1\n"
  "        (i486-pc-linux-gnu)\n"
  "Date: Tue, 19 Mar 2013 22:45:48 -0400\n"
  "Message-ID: <874ng6vler.fsf@xxxxxxxxx.csail.mit.edu>\n"
  "MIME-Version: 1.0\n"
  "Content-Type: text/plain; charset=us-ascii\n"
  "\n"
  "Hello.\n";

extern char **environ;

static spin_barrier bar;

static concurrent_distribution<uint64_t> start_tsc, stop_tsc;
static concurrent_distribution<uint64_t> start_usec, stop_usec;
static concurrent_distribution<uint64_t> count;

static volatile bool stop __mpalign__;
static volatile bool warmup;
static __padout__ __attribute__((unused));

static void
timer_thread(void)
{
  warmup = true;
  bar.join();
  sleep(warmup_secs);
  warmup = false;
  sleep(duration);
  stop = true;
}

static void
xwaitpid(int pid, const char *cmd)
{
  int status;
  if (waitpid(pid, &status, 0) < 0)
    edie("waitpid %s failed", cmd);
  if (!WIFEXITED(status) || WEXITSTATUS(status))
    die("status %d from %s", WEXITSTATUS(status), cmd);
}

static void
do_mua(int cpu, string spooldir, string msgpath, size_t batch_size)
{
  std::vector<const char*> argv{EP("mail-enqueue")};
#if defined(XV6_USER)
  int errno;
#endif
  //setaffinity(cpu);

  // Open message file (alternatively, we could use an open spawn
  // action)
  int O_ANYFD = 0;
  int msgfd = open(msgpath.c_str(), O_RDONLY|O_CLOEXEC|O_ANYFD);
  if (msgfd < 0)
    edie("open %s failed", msgpath.c_str());

  // Construct command line
  if (batch_size)
    argv.push_back("-b");
  argv.push_back(spooldir.c_str());
  argv.push_back("user");
  argv.push_back(nullptr);

  bar.join();

  bool mywarmup = true;
  uint64_t mycount = 0;
  pid_t pid = 0;
  int msgpipe[2], respipe[2];
  while (!stop) {
    if (__builtin_expect(warmup != mywarmup, 0)) {
      mywarmup = warmup;
      mycount = 0;
      start_usec.add(now_usec());
      start_tsc.add(rdtsc());
    }

    if (pid == 0) {
      posix_spawn_file_actions_t actions;
      if ((errno = posix_spawn_file_actions_init(&actions)))
        edie("posix_spawn_file_actions_init failed");
      if (batch_size) {
        if (pipe2(msgpipe, O_CLOEXEC|O_ANYFD) < 0)
          edie("pipe msgpipe failed");
        if ((errno = posix_spawn_file_actions_adddup2(&actions, msgpipe[0], 0)))
          edie("posix_spawn_file_actions_adddup2 msgpipe failed");
        if (pipe2(respipe, O_CLOEXEC|O_ANYFD) < 0)
          edie("pipe respipe failed");
        if ((errno = posix_spawn_file_actions_adddup2(&actions, respipe[1], 1)))
          edie("posix_spawn_file_actions_adddup2 respipe failed");
      } else {
        if (lseek(msgfd, 0, SEEK_SET) < 0)
          edie("lseek failed");
        if ((errno = posix_spawn_file_actions_adddup2(&actions, msgfd, 0)))
          edie("posix_spawn_file_actions_adddup2 failed");
      }
      if ((errno = posix_spawn(&pid, argv[0], &actions, nullptr,
                               const_cast<char *const*>(argv.data()), environ)))
        edie("posix_spawn failed");
      if ((errno = posix_spawn_file_actions_destroy(&actions)))
        edie("posix_spawn_file_actions_destroy failed");

      if (batch_size) {
        close(msgpipe[0]);
        close(respipe[1]);
      }
    }

    if (batch_size) {
      // Send message in batch mode
      uint64_t msg_len = strlen(message);
      xwrite(msgpipe[1], &msg_len, sizeof msg_len);
      xwrite(msgpipe[1], message, msg_len);

      // Get batch-mode response
      uint64_t res;
      if (xread(respipe[0], &res, sizeof res) != sizeof res)
        die("short read of result code");
      if (res != 0)
        die("%s returned status %d", argv[0], (int)res);
    }

    ++mycount;

    if (batch_size == 0 || mycount % batch_size == 0) {
      if (batch_size) {
        close(msgpipe[1]);
        close(respipe[0]);
      }
      xwaitpid(pid, argv[0]);
      pid = 0;
    }
  }

  stop_usec.add(now_usec());
  stop_tsc.add(rdtsc());
  count.add(mycount);
}

static void
xmkdir(const string &d)
{
  if (mkdir(d.c_str(), 0777) < 0)
    edie("failed to mkdir %s", d.c_str());
}

static void
create_spool(const string &base)
{
  xmkdir(base);
  xmkdir(base + "/pid");
  xmkdir(base + "/todo");
  xmkdir(base + "/mess");
  sync();
}

static void
create_maildir(const string &base)
{
  xmkdir(base);
  xmkdir(base + "/tmp");
  xmkdir(base + "/new");
  xmkdir(base + "/cur");
  sync();
}

void
usage(const char *argv0)
{
  fprintf(stderr, "Usage: %s [options] basedir nthreads\n", argv0);
  fprintf(stderr, "  -a none   Use regular APIs (default)\n");
  fprintf(stderr, "     all    Use alternate APIs\n");
  fprintf(stderr, "  -b 0      Do not use batch spooling (default)\n");
  fprintf(stderr, "     N      Spool in batches of size N\n");
  fprintf(stderr, "     inf    Spool in unbounded batches\n");
  fprintf(stderr, "  -p        Use delivery process pooling\n");
  fprintf(stderr, "  -d int    Set duration to int\n");
  exit(2);
}

int
main(int argc, char **argv)
{
  const char *alt_str = "none";
  size_t batch_size = 0;
  bool pool = false;
  int opt;
  while ((opt = getopt(argc, argv, "d:a:b:p")) != -1) {
    switch (opt) {
    case 'a':
      alt_str = optarg;
      break;
    case 'b':
      if (strcmp(optarg, "inf") == 0)
        batch_size = (size_t)-1;
      else
        batch_size = atoi(optarg);
      break;
    case 'p':
      pool = true;
      break;
    case 'd':
      duration = strtol(optarg, nullptr, 0);
      if (duration < 0)
        duration = 5;
      break;
    default:
      usage(argv[0]);
    }
  }

  if (argc - optind != 2)
    usage(argv[0]);

  string basedir(argv[optind]);
  const char *nthreads_str = argv[optind+1];
  int nthreads = atoi(nthreads_str);
  if (nthreads <= 0)
    usage(argv[0]);

  // Create spool and inboxes
  // XXX This terminology is wrong.  The spool is where mail
  // ultimately gets delivered to.
  string spooldir = basedir + "/spool";
  if (START_QMAN)
    create_spool(spooldir);
  string mailroot = basedir + "/mail";
  xmkdir(mailroot);
  create_maildir(mailroot + "/user");

  pid_t qman_pid;
  if (START_QMAN) {
    // Start queue manager
    std::vector<const char*> qman{EP("mail-qman"), "-a", alt_str};
    if (pool)
      qman.push_back("-p");
    qman.push_back(spooldir.c_str());
    qman.push_back(mailroot.c_str());
    qman.push_back(nthreads_str);
    qman.push_back(nullptr);
    if (posix_spawn(&qman_pid, qman[0], nullptr, nullptr,
                    const_cast<char *const*>(qman.data()), environ) != 0)
      die("posix_spawn %s failed", qman[0]);
    sleep(1);
  }

  // Write message to a file
  int fd = open((basedir + "/msg").c_str(), O_CREAT|O_WRONLY, 0666);
  if (fd < 0)
    edie("open");
  xwrite(fd, message, strlen(message));
  close(fd);

  printf("# --cores=%d --duration=%ds --alt=%s",
         nthreads, duration, alt_str);
  if (batch_size == (size_t)-1)
    printf(" --batch-size=inf");
  else
    printf(" --batch-size=%zu", batch_size);
  printf(" --pool=%s\n", pool ? "true" : "false");

  // Run benchmark
  bar.init(nthreads + 1);

  std::thread timer(timer_thread);

  std::thread *threads = new std::thread[nthreads];
  for (int i = 0; i < nthreads; ++i)
    threads[i] = std::thread(do_mua, i, basedir + "/spool", basedir + "/msg",
                             batch_size);

  // Wait
  timer.join();
  for (int i = 0; i < nthreads; ++i)
    threads[i].join();

  if (START_QMAN) {
    // Kill qman and wait for it to exit
    const char *enq[] = {EP("mail-enqueue"), "--exit", spooldir.c_str(), nullptr};
    pid_t enq_pid;
    if (posix_spawn(&enq_pid, enq[0], nullptr, nullptr,
                    const_cast<char *const*>(enq), environ) != 0)
      die("posix_spawn %s failed", enq[0]);
    xwaitpid(enq_pid, "mail-enqueue --exit");
    xwaitpid(qman_pid, "mail-qman");
  }

  // Summarize
  printf("%lu start usec skew\n", start_usec.span());
  printf("%lu stop usec skew\n", stop_usec.span());
  uint64_t usec = stop_usec.mean() - start_usec.mean();
  printf("%lf secs\n", (double)usec / 1e6);
  printf("%lu cycles\n", stop_tsc.mean() - start_tsc.mean());

  uint64_t messages = count.sum();
  printf("%lu messages\n", messages);
  if (messages) {
    printf("%lu cycles/message\n",
           (stop_tsc.sum() - start_tsc.sum()) / messages);
    printf("%lu messages/sec\n", messages * 1000000 / usec);
  }

  printf("\n");
  return 0;
}
