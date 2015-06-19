// Simple version of std::bind that doesn't support placeholders

#pragma once

#include <tuple>
#include <utility>

// __bind_call generates a list of integers 0 .. N-1 ...
template <int N, typename F, typename Res, typename TArgs, int... list>
struct __bind_call {
  static Res call(F& f, TArgs& t) {
    return __bind_call<N - 1, F, Res, TArgs, N - 1, list...>::call(f, t);
  }
};

// ... which the __bind_call base case unpacks to get a sequence of
// tuple indexes to pass to std::get to extract the saved arguments
// from the tuple t.
template <typename F, typename Res, typename TArgs, int... list>
struct __bind_call<0, F, Res, TArgs, list...> {
  static Res call(F& f, TArgs& t) {
    return f(std::get<list>(t)...);
  }
};

template <typename F, typename Res, typename... Args>
struct __bind_helper
{
  typedef std::tuple<typename std::decay<Args>::type...> TArgs;

  F f_;
  TArgs args_;

  __bind_helper(F&& f, Args&&... args) : f_(f), args_(args...) { }

  Res operator()()
  {
    return __bind_call<sizeof...(Args), F, Res, TArgs>::
      call(f_, args_);
  }
};

template <typename F, typename... Args>
auto bind_simple(F&& f, Args&&... args)
  -> __bind_helper<F, decltype(f(args...)), Args...>
{
  return __bind_helper<F, decltype(f(args...)), Args...>(
    std::forward<F>(f), std::forward<Args>(args)...);
}
