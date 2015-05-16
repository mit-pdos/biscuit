#pragma once

#include <cstddef>
#include <initializer_list>
#include <stdexcept>
#include <type_traits>
#include <utility>
#include <new>

/// A statically allocated vector of at most N T's.
template<class T, std::size_t N>
class static_vector
{
  typename std::aligned_storage<sizeof(T),
                                std::alignment_of<T>::value>::type data_[N];
  std::size_t size_;

public:
  typedef T value_type;
  typedef value_type& reference;
  typedef const value_type& const_reference;
  typedef T* iterator;
  typedef const T* const_iterator;
  typedef std::size_t size_type;
  typedef std::size_t difference_type;

  constexpr static_vector() : data_{}, size_(0) { }

  template<class InputIterator>
  static_vector(InputIterator first, InputIterator last)
    : size_(0)
  {
    for (; first != last; ++first)
      push_back(*first);
  }

  static_vector(std::initializer_list<T> elts)
    : size_(0)
  {
    for (auto it = elts.begin(), last = elts.end(); it != last; ++it)
      push_back(*it);
  }

  ~static_vector()
  {
    clear();
  }

  iterator begin() noexcept
  {
    return reinterpret_cast<T*>(&data_[0]);
  }

  const_iterator begin() const noexcept
  {
    return reinterpret_cast<const T*>(&data_[0]);
  }

  iterator end() noexcept
  {
    return reinterpret_cast<T*>(&data_[size_]);
  }

  const_iterator end() const noexcept
  {
    return reinterpret_cast<const T*>(&data_[size_]);
  }

  const_iterator cbegin() const noexcept
  {
    return reinterpret_cast<const T*>(&data_[0]);
  }

  const_iterator cend() const noexcept
  {
    return reinterpret_cast<const T*>(&data_[size_]);
  }

  size_type size() const noexcept
  {
    return size_;
  }

  size_type max_size() const noexcept
  {
    return N;
  }

  size_type capacity() const noexcept
  {
    return N;
  }

  bool empty() const noexcept
  {
    return size_ == 0;
  }

  bool full() const noexcept
  {
    return size_ == N;
  }

  reference operator[](size_type n)
  {
    return *(begin() + n);
  }

  const_reference operator[](size_type n) const
  {
    return *(begin() + n);
  }

  const_reference at(size_type n) const
  {
    if (n >= size_)
      throw std::out_of_range("index exceeds size");
    return *(begin() + n);
  }

  reference at(size_type n)
  {
    if (n >= size_)
      throw std::out_of_range("index exceeds size");
    return *(begin() + n);
  }

  reference front()
  {
    return *begin();
  }

  const_reference front() const
  {
    return *begin();
  }

  reference back()
  {
    return *(end() - 1);
  }

  const_reference back() const
  {
    return *(end() - 1);
  }

  T* data() noexcept
  {
    return &front();
  }

  const T* data() const noexcept
  {
    return &front();
  }

  template <class... Args>
  void emplace_back(Args&&... args)
  {
    if (full())
      throw std::out_of_range("static_vector is full");
    new (end()) T(std::forward<Args>(args)...);
    ++size_;
  }

  void push_back(const T& x)
  {
    emplace_back(x);
  }

  void push_back(T&& x)
  {
    emplace_back(std::move(x));
  }

  void pop_back()
  {
    if (!empty()) {
      back().~T();
      --size_;
    }
  }

  template <class... Args>
  iterator emplace(const_iterator position, Args&&... args)
  {
    auto it = end();
    if (it == position) {
      // The general logic is wrong for end emplacement
      emplace_back(std::forward<Args>(args)...);
    } else {
      if (full())
        throw std::out_of_range("static_vector is full");
      // Construct a new last element
      new (it) T(std::move(*(it - 1)));
      ++size_;
      // Shift all of the elements between position and end
      for (--it; it > position; --it)
        *it = std::move(*(it - 1));
      // Construct and move-assign new element.
      *it = T(std::forward<Args>(args)...);
    }
    return it;
  }

  iterator insert(const_iterator position, const T& x)
  {
    return emplace(position, x);
  }

  iterator insert(const_iterator position, T&& x)
  {
    return emplace(position, std::move(x));
  }

  iterator erase(const_iterator position)
  {
    // Shift everything between position and end down
    auto pos = begin() + (position - begin());
    for (auto it = pos; it < end() - 1; ++it)
      *it = std::move(*(it + 1));
    // Destruct last element
    (end() - 1)->~T();
    --size_;
    return pos;
  }

  void clear() noexcept
  {
    for (std::size_t i = 0; i < size_; ++i)
      (*this)[i].~T();
    size_ = 0;
  }
};
