#ifndef _LITTYPES_H
#define _LITTYPES_H

typedef unsigned short ushort;
typedef unsigned int uint;
typedef unsigned long ulong;
typedef unsigned long size_t;
typedef unsigned long time_t;

struct timeval {
	time_t tv_sec;
	time_t tv_usec;
};

#define NULL   ((void *)0)

#define va_start(ap, last) __builtin_va_start(ap, last)
#define va_arg(ap, type)   __builtin_va_arg(ap, type)
#define va_end(ap)         __builtin_va_end(ap)

typedef __builtin_va_list va_list;

#endif
