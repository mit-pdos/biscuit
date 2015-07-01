#pragma once

#include "compiler.h"

#include <atomic>
#include <cstdint>

// A reusable spinning barrier.
class spin_barrier
{
  uint32_t init_ __mpalign__;
  // Bit 63 is 0 for enter phase, 1 for exit phase.  Remaining bits
  // are the reached count.
  std::atomic<uint64_t> entered_;
  __padout__;

  static constexpr uint64_t phase_mask = 1ull << 63;
  static constexpr uint64_t reached_mask = ~phase_mask;

public:
  spin_barrier() : init_(0), entered_(0) { }
  spin_barrier(unsigned val) : init_(val), entered_(0) { }
  spin_barrier(const spin_barrier&) = delete;
  ~spin_barrier()
  {
    // Wait if the barrier is in the exit phase, since it's not safe
    // to delete until all completed waiters have exited
    while (entered_ & phase_mask)
      /* spin */;
  }

  void init(unsigned val)
  {
    init_ = val;
    entered_ = 0;
  }

  void join()
  {
    // Wait if the barrier is in the exit phase
    while (entered_ & phase_mask)
      asm volatile("pause":::);

    // Enter the barrier
    auto v = ++entered_;
    if (v == init_) {
      // This is the last thread to reach the barrier.  Switch to the
      // exit phase and exit the barrier.
      entered_ = (v | phase_mask) - 1;
      return;
    }

    // Wait until the barrier switches to the exit phase
    while (!(entered_.load(std::memory_order_relaxed) & phase_mask))
      asm volatile("pause":::);

    // Exit the batter
    if ((v = --entered_) == phase_mask)
      // Reached count is now zero.  Switch to the enter phase.
      entered_ = 0;
  }
};
