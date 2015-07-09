#pragma once

#include "compiler.h"

#include <err.h>

#include <atomic>
//#include <cassert>
#include <cstdint>

template<typename T>
class distribution
{
  T min_, max_, sum_;
  uint64_t count_;

public:
  distribution() : min_(), max_(), sum_(), count_(0) { }

  T add(T val)
  {
    if (val < min_ || count_ == 0)
      min_ = val;
    if (val > max_ || count_ == 0)
      max_ = val;
    sum_ += val;
    count_ += 1;
    return val;
  }

  distribution &operator+=(const distribution &o)
  {
    if ((o.min_ < min_ || count_ == 0) && o.count_)
      min_ = o.min_;
    if ((o.max_ > max_ || count_ == 0) && o.count_)
      max_ = o.max_;
    sum_ += o.sum_;
    count_ += o.count_;
    return *this;
  }

  T sum() const
  {
    return sum_;
  }

  T min() const
  {
    return min_;
  }

  T max() const
  {
    return max_;
  }

  T span() const
  {
    return max() - min();
  }

  uint64_t count() const
  {
    return count_;
  }

  T mean() const
  {
    return sum() / count();
  }

  double meand() const
  {
    return sum() / (double)count();
  }
};

template<typename T>
class concurrent_distribution
{
  // XXX Use a locked hash table from thread::id to distribution<T>
  // and cache the per-thread distribution in a thread-local.
  enum { MAX_THREADS = 100 };

  struct
  {
    distribution<T> dist __mpalign__;
    __padout__;
  } dists[MAX_THREADS];
  mutable std::atomic<bool> dirty;
  mutable distribution<T> combined;

  static int getid()
  {
    static std::atomic<int> nextid;
    static __thread int myid = -1;
    if (myid == -1) {
      myid = nextid++;
      //assert(myid < MAX_THREADS);
      if (myid >= MAX_THREADS || myid < 0)
	errx(-1, "myid >= max threads %d\n", myid);
    }
    return myid;
  }

  distribution<T> &collect() const
  {
    if (dirty.load(std::memory_order_relaxed)) {
      dirty = false;

      combined = distribution<T>();
      for (auto &d : dists)
        combined += d.dist;
    }
    return combined;
  }

public:
  concurrent_distribution() : dirty(false) { }

  T add(T val)
  {
    if (!dirty.load(std::memory_order_relaxed))
      dirty.store(true, std::memory_order_relaxed);
    dists[getid()].dist.add(val);
    return val;
  }

  T sum() const
  {
    return collect().sum();
  }

  T min() const
  {
    return collect().min();
  }

  T max() const
  {
    return collect().max();
  }

  T span() const
  {
    return collect().span();
  }

  uint64_t count() const
  {
    return collect().count();
  }

  T mean() const
  {
    return collect().mean();
  }

  double meand() const
  {
    return collect().meand();
  }
};
