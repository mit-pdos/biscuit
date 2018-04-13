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
enable_paging_wp()
{
	asm volatile(
		"movl	%%cr0, %%eax\n"
		"orl	$(1 << 31), %%eax\n"
		"orl	$(1 << 16), %%eax\n"
		"movl	%%eax, %%cr0\n"
		:
		:
		: "memory", "eax", "cc");
}

static __inline void
enable_pae()
{
	asm volatile(
		"movl	%%cr4, %%eax\n"
		"orl	$(1 << 5), %%eax\n"
		"movl	%%eax, %%cr4\n"
		:
		:
		: "memory", "eax", "cc");
}

static __inline void
enable_global()
{
	asm volatile(
		"movl	%%cr4, %%eax\n"
		"orl	$(1 << 7), %%eax\n"
		"movl	%%eax, %%cr4\n"
		:
		:
		: "memory", "eax", "cc");
}

static __inline void
cpuid(uint32_t n, uint32_t *a, uint32_t *d)
{
	asm volatile(
		"pushl	%%ebx\n"
		"cpuid\n"
		"popl	%%ebx\n"
		: "=a"(*a), "=d"(*d)
		: "0"(n)
		: "memory", "ecx");
}

static __inline uint64_t
rdmsr(uint32_t reg)
{
	uint32_t high, low;

	asm volatile(
		"rdmsr\n"
		: "=a"(low), "=d"(high)
		: "c"(reg)
		: "memory");
	return ((uint64_t)high << 32) | low;
}

static __inline void
wrmsr(uint32_t reg, uint64_t v)
{
	uint32_t low = (uint32_t)v;
	uint32_t high = (uint32_t)(v >> 32);

	asm volatile(
		"wrmsr\n"
		:
		: "a"(low), "d"(high), "c"(reg)
		: "memory");
}

static __inline void
ljmp(uint16_t sel, uint32_t entry, uint32_t a1, uint32_t a2, uint32_t sp)
{
	struct __attribute__((packed)) {
		uint32_t addr;
		uint16_t seg;
	} fa;

	fa.seg = sel << 3;
	fa.addr = entry;

	// 64bit code segment is setup in boot.S
	asm volatile(
		"movl	$0, %%ebp\n"
		"movl	%%ecx, %%esp\n"
		"ljmp	*(%%eax)\n"
		:
		: "a"(&fa), "D"(a1), "S"(a2), "c"(sp)
		: "memory");
}

//static uint64_t
//rdtsc(void)
//{
//	uint32_t lo, hi;
//	asm volatile(
//		"rdtsc\n"
//		: "=a"(lo), "=d"(hi)
//		:
//		: "memory");
//	return ((uint64_t)hi << 32) | lo;
//}

#define PGSIZE          (1UL << 12)
#define PGOFFMASK       (PGSIZE - 1)
#define PGMASK          (~PGOFFMASK)

#define ROUNDDOWN(x, y) ((x) & ~((y) - 1))
#define ROUNDUP(x, y)   (((x) + ((y) - 1)) & ~((y) - 1))

#define PML4X(x)        (((x) >> 39) & 0x1ff)
#define PDPTX(x)        (((x) >> 30) & 0x1ff)
#define PDX(x)          (((x) >> 21) & 0x1ff)
#define PTX(x)          (((x) >> 12) & 0x1ff)

#define PTE_W           (1UL << 1)
#define PTE_U           (1UL << 2)
#define PTE_P           (1UL << 0)
#define PTE_PS          (1UL << 7)
#define PTE_G           (1UL << 8)
#define PTE_PCD         (1UL << 4)

#define PTE_ADDR(x)     ((x) & ~0x3ff)

#define IA32_EFER       (0xc0000080)
#define IA32_EFER_LME   (1UL << 8)

#endif
