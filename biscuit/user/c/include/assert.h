// sadly, assert.h must be its own file so that NDEBUG works
#pragma once

#include <stdlib.h>

#ifdef NDEBUG
#define	assert(ignore)	((void) 0)
#else
#define	assert(x)	__assert(x, __FILE__, __LINE__)
#define	__assert(x, y, z)	\
do {	\
	if (!(x)) { \
		fprintf(stderr, "assertion failed (" #x ") at %s:%d\n", y, z);\
		abort();\
	} \
} while (0)
#endif
