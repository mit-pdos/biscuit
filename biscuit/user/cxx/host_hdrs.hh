// Header files included here will be extracted from the host.

// Freestanding implementation headers [C++11 17.6.1.3/2].  These all
// contain compiler-specific code that we can't simply reproduce
// ourselves.
#include <cstddef>
#include <cstdint>
#include <typeinfo>
#include <exception>
#include <initializer_list>
#include <type_traits>
#include <atomic>

// Likewise for C [C11 4/6].
#include <stddef.h>
#include <stdint.h>
