#include "max_align.h"

#include <thread>

#include <pthread.h>

#include "libutil.h"

void *
std::thread::vrun_wrapper(void *opaque)
{
  vrun *r = (vrun*)opaque;
  try {
    r->run();
  } catch (...) {
    // XXX terminate
    die("uncaught exception in thread");
  }
  delete r;
  return nullptr;
}

std::thread::thread(vrun *r)
{
  pthread_t thread;
  if (pthread_create(&thread, nullptr, vrun_wrapper, r) != 0)
    // XXX Throw system_error
    die("pthread_create failed");
  id_ = id(thread);
}

std::thread::~thread()
{
  if (joinable())
    // XXX terminate
    die("~thread for joinable thread");
}

std::thread&
std::thread::operator=(std::thread&& x) noexcept
{
  if (joinable())
    // XXX terminate
    die("operator= for joinable thread");
  id_ = x.id_;
  x.id_ = id();
  return *this;
}

void
std::thread::join()
{
  if (get_id() == id())
    // XXX Throw system_error(no_such_process)
    die("attempt to join invalid thread");
  if (!joinable())
    // XXX Throw system_error(invalid_argument)
    die("attempt to join un-joinable thread");
  if (get_id() == std::this_thread::get_id())
    // XXX Throw system_error(resource_deadlock_would_occur)
    die("attempt to join self");

  if (pthread_join(id_.thread_, nullptr) != 0)
    die("pthread_join failed");
  id_ = id();
}

std::thread::id
std::thread::id::__get_this_thread_id() noexcept
{
  return id(pthread_self());
}
