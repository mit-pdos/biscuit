#pragma once

typedef unsigned char 	uchar;
typedef unsigned short 	ushort;
typedef unsigned int 	uint;
typedef unsigned long 	ulong;

typedef unsigned char 	u_char;
typedef unsigned short 	u_short;
typedef unsigned int 	u_int;
typedef unsigned long 	u_long;

typedef unsigned long 	size_t;
typedef long 		ssize_t;
typedef long 		ptrdiff_t;

typedef long 		intptr_t;
typedef unsigned long 	uintptr_t;

typedef char 		int8_t;
typedef short 		int16_t;
typedef int 		int32_t;
typedef long 		int64_t;
typedef unsigned char 	uint8_t;
typedef unsigned short 	uint16_t;
typedef unsigned int 	uint32_t;
typedef unsigned long 	uint64_t;

typedef char 		int_least8_t;
typedef short 		int_least16_t;
typedef int 		int_least32_t;
typedef long 		int_least64_t;
typedef unsigned char 	uint_least8_t;
typedef unsigned short 	uint_least16_t;
typedef unsigned int 	uint_least32_t;
typedef unsigned long 	uint_least64_t;

// OpenBSD uses int32_t for all fastN_t except 64
typedef int 		int_fast8_t;
typedef int 		int_fast16_t;
typedef int 		int_fast32_t;
typedef long 		int_fast64_t;
typedef unsigned int 	uint_fast8_t;
typedef unsigned int 	uint_fast16_t;
typedef unsigned int 	uint_fast32_t;
typedef unsigned long 	uint_fast64_t;

typedef long 		intmax_t;
typedef unsigned long 	uintmax_t;

typedef unsigned char 	u8;

typedef long 		time_t;
typedef unsigned long 	nfds_t;
typedef unsigned long 	mode_t;
typedef long 		dev_t;
typedef ulong 		ino_t;
typedef long 		off_t;
typedef long 		pid_t;
typedef long 		uid_t;
typedef long 		gid_t;
typedef unsigned long 	socklen_t;
typedef unsigned long 	rlim_t;
typedef unsigned long 	sigset_t;
typedef volatile long 	sig_atomic_t;
typedef long 		blkcnt_t;
typedef char * 		caddr_t;

#define NULL   ((void *)0)

#define va_start(ap, last) __builtin_va_start(ap, last)
#define va_arg(ap, type)   __builtin_va_arg(ap, type)
#define va_end(ap)         __builtin_va_end(ap)
#define va_copy(d, s)      __builtin_va_copy(d, s)

typedef __builtin_va_list va_list;

#define INT8_MIN	(-128)
#define INT16_MIN	(-32768)
#define INT32_MIN	(-2147483648)
#define INT8_MAX	127
#define INT16_MAX	32767
#define INT32_MAX	2147483647

#define INT_MIN		(1 << 31)
#define INT_MAX		2147483647

#define UINT_MAX	0xffffffff

#define LONG_MIN	(1ll << 63)
#define LLONG_MIN	(1ll << 63)

#define LONG_MAX	9223372036854775807ll
#define LLONG_MAX	9223372036854775807ll

#define UCHAR_MAX	0xff
#define UINT16_MAX	0xffff

#define ULONG_MAX	0xffffffffffffffffull
#define ULLONG_MAX	0xffffffffffffffffull

#define INT8_C(x)	x
#define INT16_C(x)	x
#define INT32_C(x)	x
#define INT64_C(x)	x ## ll
#define UINT8_C(x)	x ## u
#define UINT16_C(x)	x ## u
#define UINT32_C(x)	x ## u
#define UINT64_C(x)	x ## ull
