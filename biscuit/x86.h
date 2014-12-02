#ifndef _X86_H
#define _X86_H

#include <types.h>

static __inline void
outb(int port, uint8_t data)
{
	__asm __volatile("outb %0,%w1" : : "a" (data), "d" (port));
}

static __inline void
outw(int port, uint16_t data)
{
	__asm __volatile("outw %0,%w1" : : "a" (data), "d" (port));
}

static __inline uint8_t
inb(int port)
{
	uint8_t data;
	__asm __volatile("inb %w1,%0" : "=a" (data) : "d" (port));
	return data;
}

static __inline void
insl(int port, void *addr, int cnt)
{
	__asm __volatile("cld\n\trepne\n\tinsl"			:
			 "=D" (addr), "=c" (cnt)		:
			 "d" (port), "0" (addr), "1" (cnt)	:
			 "memory", "cc");
}

static __inline void
lcr3(void *pg)
{
	asm volatile(
		"movl	%0, %%cr3\n"
		:
		: "a"(pg)
		: "memory");
}

static __inline void
enable_paging()
{
	asm volatile(
		"movl	%%cr0, %%eax\n"
		"orl	$(1 << 31), %%eax\n"
		"movl	%%eax, %%cr0\n"
		:
		:
		: "memory", "eax", "cc");
}

#define PGSIZE          (1UL << 12)
#define PGOFFMASK       (PGSIZE - 1)
#define PGMASK          (~PGOFFMASK)

#define ROUNDDOWN(x, y) ((x) & ~((y) - 1))
#define ROUNDUP(x, y)   (((x) + ((y) - 1)) & ~((y) - 1))

#define PDX(x)          (((x) >> 22) & 0x3ff)
#define PTX(x)          (((x) >> 12) & 0x3ff)

#define PTE_P           (1 << 0)
#define PTE_W           (1 << 1)
#define PTE_U           (1 << 2)

#define PTE_ADDR(x)     ((x) & ~0x3ff)

#endif
