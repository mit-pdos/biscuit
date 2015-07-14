// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

#include "runtime.h"
#include "defs_GOOS_GOARCH.h"
#include "os_GOOS.h"
#include "signal_unix.h"
#include "stack.h"
#include "textflag.h"

extern SigTab runtime·sigtab[];

static Sigset sigset_none;
static Sigset sigset_all = { ~(uint32)0, ~(uint32)0 };

// Linux futex.
//
//	futexsleep(uint32 *addr, uint32 val)
//	futexwakeup(uint32 *addr)
//
// Futexsleep atomically checks if *addr == val and if so, sleeps on addr.
// Futexwakeup wakes up threads sleeping on addr.
// Futexsleep is allowed to wake up spuriously.

enum
{
	FUTEX_WAIT = 0,
	FUTEX_WAKE = 1,
};

// Atomically,
//	if(*addr == val) sleep
// Might be woken up spuriously; that's allowed.
// Don't sleep longer than ns; ns < 0 means forever.
#pragma textflag NOSPLIT
void
runtime·futexsleep(uint32 *addr, uint32 val, int64 ns)
{
	Timespec ts;

	// Some Linux kernels have a bug where futex of
	// FUTEX_WAIT returns an internal error code
	// as an errno.  Libpthread ignores the return value
	// here, and so can we: as it says a few lines up,
	// spurious wakeups are allowed.

	if(ns < 0) {
		runtime·futex(addr, FUTEX_WAIT, val, nil, nil, 0);
		return;
	}
	// NOTE: tv_nsec is int64 on amd64, so this assumes a little-endian system.
	ts.tv_nsec = 0;
	ts.tv_sec = runtime·timediv(ns, 1000000000LL, (int32*)&ts.tv_nsec);
	runtime·futex(addr, FUTEX_WAIT, val, &ts, nil, 0);
}

static void badfutexwakeup(void);

// If any procs are sleeping on addr, wake up at most cnt.
#pragma textflag NOSPLIT
void
runtime·futexwakeup(uint32 *addr, uint32 cnt)
{
	int64 ret;
	void (*fn)(void);

	ret = runtime·futex(addr, FUTEX_WAKE, cnt, nil, nil, 0);
	if(ret >= 0)
		return;

	// I don't know that futex wakeup can return
	// EAGAIN or EINTR, but if it does, it would be
	// safe to loop and call futex again.
	g->m->ptrarg[0] = addr;
	g->m->scalararg[0] = (int32)ret; // truncated but fine
	fn = badfutexwakeup;
	if(g == g->m->gsignal)
		fn();
	else
		runtime·onM(&fn);
	*(int32*)0x1006 = 0x1006;
}

static void
badfutexwakeup(void)
{
	void *addr;
	int64 ret;
	
	addr = g->m->ptrarg[0];
	ret = (int32)g->m->scalararg[0];
	runtime·printf("futexwakeup addr=%p returned %D\n", addr, ret);
}

extern runtime·sched_getaffinity(uintptr pid, uintptr len, uintptr *buf);
static int32
getproccount(void)
{
	uintptr buf[16], t;
	int32 r, cnt, i;

	cnt = 0;
	r = runtime·sched_getaffinity(0, sizeof(buf), buf);
	if(r > 0)
	for(i = 0; i < r/sizeof(buf[0]); i++) {
		t = buf[i];
		t = t - ((t >> 1) & 0x5555555555555555ULL);
		t = (t & 0x3333333333333333ULL) + ((t >> 2) & 0x3333333333333333ULL);
		cnt += (int32)((((t + (t >> 4)) & 0xF0F0F0F0F0F0F0FULL) * 0x101010101010101ULL) >> 56);
	}

	return cnt ? cnt : 1;
}

// Clone, the Linux rfork.
enum
{
	CLONE_VM = 0x100,
	CLONE_FS = 0x200,
	CLONE_FILES = 0x400,
	CLONE_SIGHAND = 0x800,
	CLONE_PTRACE = 0x2000,
	CLONE_VFORK = 0x4000,
	CLONE_PARENT = 0x8000,
	CLONE_THREAD = 0x10000,
	CLONE_NEWNS = 0x20000,
	CLONE_SYSVSEM = 0x40000,
	CLONE_SETTLS = 0x80000,
	CLONE_PARENT_SETTID = 0x100000,
	CLONE_CHILD_CLEARTID = 0x200000,
	CLONE_UNTRACED = 0x800000,
	CLONE_CHILD_SETTID = 0x1000000,
	CLONE_STOPPED = 0x2000000,
	CLONE_NEWUTS = 0x4000000,
	CLONE_NEWIPC = 0x8000000,
};

void
runtime·newosproc(M *mp, void *stk)
{
	int32 ret;
	int32 flags;
	Sigset oset;

	/*
	 * note: strace gets confused if we use CLONE_PTRACE here.
	 */
	flags = CLONE_VM	/* share memory */
		| CLONE_FS	/* share cwd, etc */
		| CLONE_FILES	/* share fd table */
		| CLONE_SIGHAND	/* share sig handler table */
		| CLONE_THREAD	/* revisit - okay for now */
		;

	mp->tls[0] = mp->id;	// so 386 asm can find it
	if(0){
		runtime·printf("newosproc stk=%p m=%p g=%p clone=%p id=%d/%d ostk=%p\n",
			stk, mp, mp->g0, runtime·clone, mp->id, (int32)mp->tls[0], &mp);
	}

	// Disable signals during clone, so that the new thread starts
	// with signals disabled.  It will enable them in minit.
	runtime·rtsigprocmask(SIG_SETMASK, &sigset_all, &oset, sizeof oset);
	ret = runtime·clone(flags, stk, mp, mp->g0, runtime·mstart);
	runtime·rtsigprocmask(SIG_SETMASK, &oset, nil, sizeof oset);

	if(ret < 0) {
		runtime·printf("runtime: failed to create new OS thread (have %d already; errno=%d)\n", runtime·mcount(), -ret);
		runtime·throw("runtime.newosproc");
	}
}

int64 runtime·hackmode;

void
runtime·osinit(void)
{
	if (runtime·hackmode) {
		// XXX duur
		runtime·ncpu = 1;
	} else {
		runtime·ncpu = getproccount();
	}
}

// Random bytes initialized at startup.  These come
// from the ELF AT_RANDOM auxiliary vector (vdso_linux_amd64.c).
byte*	runtime·startup_random_data;
uint32	runtime·startup_random_data_len;

#pragma textflag NOSPLIT
void
runtime·get_random_data(byte **rnd, int32 *rnd_len)
{
	if(runtime·startup_random_data != nil) {
		*rnd = runtime·startup_random_data;
		*rnd_len = runtime·startup_random_data_len;
	} else {
		#pragma dataflag NOPTR
		static byte urandom_data[HashRandomBytes];
		int32 fd;
		fd = runtime·open("/dev/urandom", 0 /* O_RDONLY */, 0);
		if(runtime·read(fd, urandom_data, HashRandomBytes) == HashRandomBytes) {
			*rnd = urandom_data;
			*rnd_len = HashRandomBytes;
		} else {
			*rnd = nil;
			*rnd_len = 0;
		}
		runtime·close(fd);
	}
}

void
runtime·goenvs(void)
{
	runtime·goenvs_unix();
}

// Called to initialize a new m (including the bootstrap m).
// Called on the parent thread (main thread in case of bootstrap), can allocate memory.
void
runtime·mpreinit(M *mp)
{
	mp->gsignal = runtime·malg(32*1024);	// OS X wants >=8K, Linux >=2K
	mp->gsignal->m = mp;
}

// Called to initialize a new m (including the bootstrap m).
// Called on the new thread, can not allocate memory.
void
runtime·minit(void)
{
	// Initialize signal handling.
	runtime·signalstack((byte*)g->m->gsignal->stack.lo, 32*1024);
	runtime·rtsigprocmask(SIG_SETMASK, &sigset_none, nil, sizeof(Sigset));
}

// Called from dropm to undo the effect of an minit.
void
runtime·unminit(void)
{
	runtime·signalstack(nil, 0);
}

uintptr
runtime·memlimit(void)
{
	Rlimit rl;
	extern byte runtime·text[], runtime·end[];
	uintptr used;

	if(runtime·getrlimit(RLIMIT_AS, &rl) != 0)
		return 0;
	if(rl.rlim_cur >= 0x7fffffff)
		return 0;

	// Estimate our VM footprint excluding the heap.
	// Not an exact science: use size of binary plus
	// some room for thread stacks.
	used = runtime·end - runtime·text + (64<<20);
	if(used >= rl.rlim_cur)
		return 0;

	// If there's not at least 16 MB left, we're probably
	// not going to be able to do much.  Treat as no limit.
	rl.rlim_cur -= used;
	if(rl.rlim_cur < (16<<20))
		return 0;

	return rl.rlim_cur - used;
}

#ifdef GOARCH_386
#define sa_handler k_sa_handler
#endif

/*
 * This assembler routine takes the args from registers, puts them on the stack,
 * and calls sighandler().
 */
extern void runtime·sigtramp(void);
extern void runtime·sigreturn(void);	// calls rt_sigreturn, only used with SA_RESTORER

void
runtime·setsig(int32 i, GoSighandler *fn, bool restart)
{
	SigactionT sa;

	runtime·memclr((byte*)&sa, sizeof sa);
	sa.sa_flags = SA_ONSTACK | SA_SIGINFO | SA_RESTORER;
	if(restart)
		sa.sa_flags |= SA_RESTART;
	sa.sa_mask = ~0ULL;
	// Although Linux manpage says "sa_restorer element is obsolete and
	// should not be used". x86_64 kernel requires it. Only use it on
	// x86.
#ifdef GOARCH_386
	sa.sa_restorer = (void*)runtime·sigreturn;
#endif
#ifdef GOARCH_amd64
	sa.sa_restorer = (void*)runtime·sigreturn;
#endif
	if(fn == runtime·sighandler)
		fn = (void*)runtime·sigtramp;
	sa.sa_handler = fn;
	if(runtime·rt_sigaction(i, &sa, nil, sizeof(sa.sa_mask)) != 0)
		runtime·throw("rt_sigaction failure");
}

GoSighandler*
runtime·getsig(int32 i)
{
	SigactionT sa;

	runtime·memclr((byte*)&sa, sizeof sa);
	if(runtime·rt_sigaction(i, nil, &sa, sizeof(sa.sa_mask)) != 0)
		runtime·throw("rt_sigaction read failure");
	if((void*)sa.sa_handler == runtime·sigtramp)
		return runtime·sighandler;
	return (void*)sa.sa_handler;
}

void
runtime·signalstack(byte *p, int32 n)
{
	SigaltstackT st;

	st.ss_sp = p;
	st.ss_size = n;
	st.ss_flags = 0;
	if(p == nil)
		st.ss_flags = SS_DISABLE;
	runtime·sigaltstack(&st, nil);
}

void
runtime·unblocksignals(void)
{
	runtime·rtsigprocmask(SIG_SETMASK, &sigset_none, nil, sizeof sigset_none);
}

#pragma textflag NOSPLIT
int8*
runtime·signame(int32 sig)
{
	return runtime·sigtab[sig].name;
}

// src/runtime/asm_amd64.s
void atomic_dec(uint64 *);
void cli(void);
void cpu_halt(uint64);
void hack_yield(void);
void finit(void);
void fxsave(uint64 *);
void fxrstor(uint64 *);
int64 inb(int32);
uint64 lap_id(void);
void lcr0(uint64);
void lcr3(uint64);
void lcr4(uint64);
void lidt(struct pdesc_t *);
void ltr(uint64);
void outb(int64, int64);
int64 pushcli(void);
void popcli(int64);
uint64 rcr0(void);
uint64 rcr2(void);
uint64 rcr3(void);
uint64 rcr4(void);
uint64 rflags(void);
uint64 rrsp(void);
void sti(void);
void tlbflush(void);
void trapret(uint64 *, uint64);
void _trapret(uint64 *);
void wlap(uint32, uint32);

uint64 runtime·Rdtsc(void);

// src/runtime/sys_linux_amd64.s
void fakesig(int32, Siginfo *, void *);
void intsigret(void);

// src/runtime/os_linux.go
void runtime·cls(void);
void runtime·putch(int8);
extern void runtime·putcha(int8, int8);

// src/runtime/proc.c
struct spinlock_t {
	volatile uint32 lock;
};

void splock(struct spinlock_t *);
void spunlock(struct spinlock_t *);

// this file
void lap_eoi(void);
void runtime·deray(uint64);

void runtime·stackcheck(void);
void invlpg(void *);

// interrupts must be cleared before attempting to take this lock. the reason
// is that access to VGA buffer/serial console must be exclusive, yet i want to
// be able to print from an interrupt handler. thus any code that takes this
// lock cannot be interruptible (suppose such code is interruptible, it takes
// the pmsglock and is then interrupted. the interrupt handler then panicks and
// tries to print and deadlocks).
struct spinlock_t pmsglock;

#pragma textflag NOSPLIT
void
_pnum(uint64 n)
{
	uint64 nn = (uint64)n;
	int64 i;

	runtime·putch(' ');
	for (i = 60; i >= 0; i -= 4) {
		uint64 cn = (nn >> i) & 0xf;

		if (cn <= 9)
			runtime·putch('0' + cn);
		else
			runtime·putch('A' + cn - 10);
	}
}

#pragma textflag NOSPLIT
void
pnum(uint64 n)
{
	int64 fl = pushcli();
	splock(&pmsglock);
	_pnum(n);
	spunlock(&pmsglock);
	popcli(fl);
}

#pragma textflag NOSPLIT
void
runtime·pnum(uint64 n)
{
	if (!runtime·hackmode)
		return;
	pnum(n);
}

#pragma textflag NOSPLIT
void
_pmsg(int8 *msg)
{
	runtime·putch(' ');
	if (msg)
		while (*msg)
			runtime·putch(*msg++);
}

#pragma textflag NOSPLIT
void
pmsg(int8 *msg)
{
	int64 fl = pushcli();
	splock(&pmsglock);
	_pmsg(msg);
	spunlock(&pmsglock);
	popcli(fl);
}

static int32 halt;

#pragma textflag NOSPLIT
void
runtime·pancake(void *msg, int64 addr)
{
	{
	uint16 *p = (uint16 *)0xb8002;
	*p = 0x1400 | 'F';
	}
	cli();

	pmsg(msg);
	pnum(addr);
	pmsg(" PANCAKE");
	volatile int32 *wtf = &halt;
	*wtf = 1;
	//void stack_dump(uint64);
	//stack_dump(rrsp());
	//pmsg("TWO");
	//stack_dump(g->m->curg->sched.sp);
	//while (1);
	while (1) {
		uint16 *p = (uint16 *)0xb8002;
		*p = 0x1400 | 'F';
	}
}

#define assert(x, y, z)        do { if (!(x)) runtime·pancake(y, z); } while (0)

#pragma textflag NOSPLIT
static void
bw(uint8 *d, uint64 data, uint64 off)
{
	*d = (data >> off*8) & 0xff;
}

// gee i wish i could pack structs with plan9 compiler
struct pdesc_t {
	uint8 dur[10];
};

struct seg64_t {
	uint8 dur[8];
};

//#define	G       0x80
#define	G       0x00
#define	D       0x40
#define	L       0x20

#define	CODE    0xa
#define	DATA    0x2
#define	TSS     0x9
#define	USER    0x60

#define MAXCPUS 128

// 2 tss segs for each CPU
static struct seg64_t segs[6 + 2*MAXCPUS] = {
	// NULL seg
	{0, 0, 0, 0, 0, 0, 0, 0},

	// limits and base are ignored for CS, DS, and ES in long mode.

	// 64 bit code
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x90 | CODE,	// p, dpl, s, type
	 G | L,		// g, d/b, l, avail, mid limit
	 0},		// base high

	// data
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x90 | DATA,	// p, dpl, s, type
	 G | D,	// g, d/b, l, avail, mid limit
	 0},		// base high

	// fs seg
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x90 | DATA,	// p, dpl, s, type
	 G | D,		// g, d/b, l, avail, mid limit
	 0},		// base high

	// 4 - 64 bit user code
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x90 | CODE | USER,	// p, dpl, s, type
	 G | L,		// g, d/b, l, avail, mid limit
	 0},		// base high

	// 5 - user data
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x90 | DATA | USER,	// p, dpl, s, type
	 G | D,	// g, d/b, l, avail, mid limit
	 0},		// base high

	// 6 - tss seg
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x80 | TSS,	// p, dpl, s, type
	 G,		// g, d/b, l, avail, mid limit
	 0},		// base high
	// 64 bit tss takes up two segment descriptor entires. the high 32bits
	// of the base are written in this seg desc.
	{0, 0, 0, 0, 0, 0, 0, 0},
};

static struct pdesc_t pd;

#define	CODE_SEG        1
#define	FS_SEG          3
// 64bit tss uses two seg descriptors
#define	TSS_SEG(x)      (6 + 2*x)

#pragma textflag NOSPLIT
static void
seg_set(struct seg64_t *seg, uint32 base, uint32 lim, uint32 data)
{
	uint8 b1 = base & 0xff;
	uint8 b2 = (base >>  8) & 0xff;
	uint8 b3 = (base >> 16) & 0xff;
	uint8 b4 = (base >> 24) & 0xff;

	// correct limit
	uint8 l1 = lim & 0xff;
	uint8 l2 = (lim >>  8) & 0xff;
	uint8 l3 = (lim >> 16) & 0xf;

	seg->dur[0] = l1;
	seg->dur[1] = l2;
	if (data)
		seg->dur[6] = l3 | G | D;
	else
		seg->dur[6] = l3 | G;

	seg->dur[2] = b1;
	seg->dur[3] = b2;
	seg->dur[4] = b3;
	seg->dur[7] = b4;
}

#pragma textflag NOSPLIT
static void
tss_seg_set(int32 tn, uint64 base, uint32 lim, uint32 data)
{
	struct seg64_t *seg = &segs[TSS_SEG(tn)];
	uint32 basel = (uint64)base;

	seg->dur[5] = 0x80 | TSS;
	seg->dur[6] = G;
	seg_set(seg, basel, lim, data);

	// set high bits (TSS64 uses two segment descriptors
	uint32 haddr = base >> 32;
	bw(&segs[TSS_SEG(tn) + 1].dur[0], haddr, 0);
	bw(&segs[TSS_SEG(tn) + 1].dur[1], haddr, 1);
	bw(&segs[TSS_SEG(tn) + 1].dur[2], haddr, 2);
	bw(&segs[TSS_SEG(tn) + 1].dur[3], haddr, 3);
}

#undef DATA
#undef CODE
#undef TSS
#undef G
#undef D
#undef L

#define	CHECK32(x)     (x & ~((1ULL << 32) - 1))

#pragma textflag NOSPLIT
static void
pdsetup(struct pdesc_t *pd, uint64 s, uint64 lim)
{
	uint64 addr = (uint64)s;

	pd->dur[0] = lim & 0xff;
	pd->dur[1] = (lim >> 8) & 0xff;

	pd->dur[2] = addr & 0xff;
	pd->dur[3] = (addr >>  8) & 0xff;
	pd->dur[4] = (addr >> 16) & 0xff;
	pd->dur[5] = (addr >> 24) & 0xff;
	pd->dur[6] = (addr >> 32) & 0xff;
	pd->dur[7] = (addr >> 40) & 0xff;
	pd->dur[8] = (addr >> 48) & 0xff;
	pd->dur[9] = (addr >> 56) & 0xff;
}

#pragma textflag NOSPLIT
uint64
segsetup(void *tls0)
{
	uint64 tlsaddr = (uint64)tls0;

	// TLS assembles to -16(%fs)
	tlsaddr += 16;

	if (sizeof(struct pdesc_t) != 10)
		runtime·pancake("pdesc not packed", sizeof(struct pdesc_t));

	if (sizeof(struct seg64_t) != 8)
		runtime·pancake("seg64 not packed", sizeof(struct seg64_t));
	if (sizeof(struct seg64_t)*4 != 32)
		runtime·pancake("wut?", sizeof(struct seg64_t)*4);

	// gee i wish i could align data with plan9 compiler
	if ((uint64)&pd & 0x7)
		runtime·pancake("pdesc not aligned", (uint64)&pd);

	if (CHECK32(tlsaddr))
		runtime·pancake("tlsaddr > 32bits, use msrs", tlsaddr);

	seg_set(&segs[FS_SEG], (uint32)tlsaddr, 15, 1);
	pdsetup(&pd, (uint64)segs, sizeof(segs) - 1);

	return (uint64)&pd;
}

struct idte_t {
	uint8 dur[16];
};

#define	INT     0xe
#define	TRAP    0xf

#define	NIDTE   128
struct idte_t idt[NIDTE];

#pragma textflag NOSPLIT
static void
int_set(struct idte_t *i, uint64 addr, uint32 trap, int32 user, int32 ist)
{
	/*
	{0, 0,		// 0-1   low offset
	 CODE_SEG, 0,	// 2-3   segment
	 0,		// 4     ist
	 0x80 | INT,	// 5     p, dpl, type
	 0, 0,		// 6-7   mid offset
	 0, 0, 0, 0,	// 8-11  high offset
	 0, 0, 0, 0},	// 12-15 resreved
	 */

	uint16 lowoff  = (uint16)addr;
	uint16 midoff  = (addr >> 16) & 0xffff;
	uint32 highoff = addr >> 32;

	bw(&i->dur[0],  lowoff, 0);
	bw(&i->dur[1],  lowoff, 1);
	bw(&i->dur[2], CODE_SEG << 3, 0);
	bw(&i->dur[3], CODE_SEG << 3, 1);
	i->dur[4] = ist;
	uint8 t = 0x80;
	t |= trap ? TRAP : INT;
	t |= user ? USER : 0;
	i->dur[5] = t;
	bw(&i->dur[6],  midoff, 0);
	bw(&i->dur[7],  midoff, 1);
	bw(&i->dur[8],  highoff, 0);
	bw(&i->dur[9],  highoff, 1);
	bw(&i->dur[10], highoff, 2);
	bw(&i->dur[11], highoff, 3);
}

#undef INT
#undef TRAP
#undef USER

struct tss_t {
	uint8 dur[26*4 + 8]; // +8 to preserve 16 byte alignment
};

struct tss_t tss[MAXCPUS];

#pragma textflag NOSPLIT
static void
tss_set(struct tss_t *tss, uint64 rsp0)
{
	uint32 off = 4;		// offset to rsp0 field

	// set rsp0
	bw(&tss->dur[off + 0], rsp0, 0);
	bw(&tss->dur[off + 1], rsp0, 1);
	bw(&tss->dur[off + 2], rsp0, 2);
	bw(&tss->dur[off + 3], rsp0, 3);
	bw(&tss->dur[off + 4], rsp0, 4);
	bw(&tss->dur[off + 5], rsp0, 5);
	bw(&tss->dur[off + 6], rsp0, 6);
	bw(&tss->dur[off + 7], rsp0, 7);

	// set ist1
	off = 36;
	bw(&tss->dur[off + 0], rsp0, 0);
	bw(&tss->dur[off + 1], rsp0, 1);
	bw(&tss->dur[off + 2], rsp0, 2);
	bw(&tss->dur[off + 3], rsp0, 3);
	bw(&tss->dur[off + 4], rsp0, 4);
	bw(&tss->dur[off + 5], rsp0, 5);
	bw(&tss->dur[off + 6], rsp0, 6);
	bw(&tss->dur[off + 7], rsp0, 7);

	// disable io bitmap
	uint64 d = sizeof(struct tss_t);
	bw(&tss->dur[102], d, 0);
	bw(&tss->dur[103], d, 1);
}

#pragma textflag NOSPLIT
void
int_setup(void)
{
	struct pdesc_t p;

	if (sizeof(struct idte_t) != 16)
		runtime·pancake("idte not packed", sizeof(struct idte_t));
	if (sizeof(idt) != 16*NIDTE)
		runtime·pancake("idt not packed", sizeof(idt));

	if ((uint64)idt & 0x7)
		runtime·pancake("idt not aligned", (uint64)idt);

	extern void Xdz (void);
	extern void Xrz (void);
	extern void Xnmi(void);
	extern void Xbp (void);
	extern void Xov (void);
	extern void Xbnd(void);
	extern void Xuo (void);
	extern void Xnm (void);
	extern void Xdf (void);
	extern void Xrz2(void);
	extern void Xtss(void);
	extern void Xsnp(void);
	extern void Xssf(void);
	extern void Xgp (void);
	extern void Xpf (void);
	extern void Xrz3(void);
	extern void Xmf (void);
	extern void Xac (void);
	extern void Xmc (void);
	extern void Xfp (void);
	extern void Xve (void);
	extern void Xtimer(void);
	extern void Xspur(void);
	extern void Xyield(void);
	extern void Xsyscall(void);
	extern void Xtlbshoot(void);
	extern void Xsigret(void);

	extern void Xirq1(void);
	extern void Xirq2(void);
	extern void Xirq3(void);
	extern void Xirq4(void);
	extern void Xirq5(void);
	extern void Xirq6(void);
	extern void Xirq7(void);
	extern void Xirq8(void);
	extern void Xirq9(void);
	extern void Xirq10(void);
	extern void Xirq11(void);
	extern void Xirq12(void);
	extern void Xirq13(void);
	extern void Xirq14(void);
	extern void Xirq15(void);

	// any interrupt that may be generated during go code needs to use ist1
	// to force a stack switch unless there is some mechanism that prevents
	// a CPU that took an interrupt from getting its stack clobbered by
	// another CPU handling the interrupt, scheduling/running the go code,
	// and taking another interrupt on the same stack (before the first CPU
	// can context switch to a new thread).
	int_set(&idt[ 0], (uint64) Xdz , 0, 0, 0);
	int_set(&idt[ 1], (uint64) Xrz , 0, 0, 0);
	int_set(&idt[ 2], (uint64) Xnmi, 0, 0, 0);
	int_set(&idt[ 3], (uint64) Xbp , 0, 0, 0);
	int_set(&idt[ 4], (uint64) Xov , 0, 0, 0);
	int_set(&idt[ 5], (uint64) Xbnd, 0, 0, 0);
	int_set(&idt[ 6], (uint64) Xuo , 0, 0, 0);
	int_set(&idt[ 7], (uint64) Xnm , 0, 0, 0);
	// use ist1 for double fault handler
	int_set(&idt[ 8], (uint64) Xdf , 0, 0, 1);
	int_set(&idt[ 9], (uint64) Xrz2, 0, 0, 0);
	int_set(&idt[10], (uint64) Xtss, 0, 0, 0);
	int_set(&idt[11], (uint64) Xsnp, 0, 0, 0);
	int_set(&idt[12], (uint64) Xssf, 0, 0, 0);
	int_set(&idt[13], (uint64) Xgp , 0, 0, 0);
	int_set(&idt[14], (uint64) Xpf , 0, 0, 1);
	int_set(&idt[15], (uint64) Xrz3, 0, 0, 0);
	int_set(&idt[16], (uint64) Xmf , 0, 0, 0);
	int_set(&idt[17], (uint64) Xac , 0, 0, 0);
	int_set(&idt[18], (uint64) Xmc , 0, 0, 0);
	int_set(&idt[19], (uint64) Xfp , 0, 0, 0);
	int_set(&idt[20], (uint64) Xve , 0, 0, 0);

#define IRQBASE 32
	int_set(&idt[IRQBASE+ 0], (uint64) Xtimer,  0, 0, 1);
	int_set(&idt[IRQBASE+ 1], (uint64) Xirq1 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 2], (uint64) Xirq2 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 3], (uint64) Xirq3 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 4], (uint64) Xirq4 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 5], (uint64) Xirq5 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 6], (uint64) Xirq6 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 7], (uint64) Xirq7 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 8], (uint64) Xirq8 ,  0, 0, 1);
	int_set(&idt[IRQBASE+ 9], (uint64) Xirq9 ,  0, 0, 1);
	int_set(&idt[IRQBASE+10], (uint64) Xirq10 , 0, 0, 1);
	int_set(&idt[IRQBASE+11], (uint64) Xirq11 , 0, 0, 1);
	int_set(&idt[IRQBASE+12], (uint64) Xirq12 , 0, 0, 1);
	int_set(&idt[IRQBASE+13], (uint64) Xirq13 , 0, 0, 1);
	int_set(&idt[IRQBASE+14], (uint64) Xirq14 , 0, 0, 1);
	int_set(&idt[IRQBASE+15], (uint64) Xirq15 , 0, 0, 1);

	int_set(&idt[48], (uint64) Xspur,    0, 0, 1);
	int_set(&idt[49], (uint64) Xyield,   0, 0, 1);
	int_set(&idt[64], (uint64) Xsyscall, 0, 1, 1);

	int_set(&idt[70], (uint64) Xtlbshoot, 0, 0, 1);
	int_set(&idt[71], (uint64) Xsigret,   0, 0, 1);

	pdsetup(&p, (uint64)idt, sizeof(idt) - 1);
	lidt(&p);
}

#pragma textflag NOSPLIT
void
wemadeit(void)
{
	runtime·pancake(" We made it! ", 0xc001d00dc001d00dULL);
}

// physical address of current pmap, given to us by bootloader
uint64 kpmap;

int8 gostr[] = "go";

#pragma textflag NOSPLIT
void
exam(uint64 cr0)
{
	USED(cr0);
	//pmsg(" first free ");
	//pnum(first_free);

	pmsg("inspect cr0");

	if (cr0 & (1UL << 30))
		pmsg("CD set ");
	if (cr0 & (1UL << 29))
		pmsg("NW set ");
	if (cr0 & (1UL << 16))
		pmsg("WP set ");
	if (cr0 & (1UL << 5))
		pmsg("NE set ");
	if (cr0 & (1UL << 3))
		pmsg("TS set ");
	if (cr0 & (1UL << 2))
		pmsg("EM set ");
	if (cr0 & (1UL << 1))
		pmsg("MP set ");
}

static uint64 _gimmealign;
static uint64 fxinit[512/8];

#pragma textflag NOSPLIT
void
fpuinit(void)
{
	finit();

	uint64 cr0 = rcr0();
	uint64 cr4 = rcr4();

	// for VEX prefixed instructions
	//// set NE and MP
	//cr0 |= 1 << 5;
	//cr0 |= 1 << 1;
	// TS to catch SSE
	//cr0 |= 1 << 3;

	//// set OSXSAVE
	//cr4 |= 1 << 18;

	// clear EM
	cr0 &= ~(1 << 2);

	// set OSFXSR
	cr4 |= 1 << 9;
	// set MP
	cr0 |= 1 << 1;

	lcr0(cr0);
	lcr4(cr4);

	uint64 n = (uint64)fxinit;
	assert((n & 0xf) == 0, "fxinit not aligned", n);
	static int32 once;
	// XXX XXX XXX XXX XXX XXX XXX XXX 
	//if (runtime·cas(&once, 0, 1))
	//	fxsave(fxinit);
	if (once == 0) {
		once = 1;
		fxsave(fxinit);
	}
}

#define PGSIZE          (1ULL << 12)
#define PGOFFMASK       (PGSIZE - 1)
#define PGMASK          (~PGOFFMASK)

#define ROUNDDOWN(x, y) ((x) & ~((y) - 1))
#define ROUNDUP(x, y)   (((x) + ((y) - 1)) & ~((y) - 1))

#define PML4X(x)        (((uint64)(x) >> 39) & 0x1ff)
#define PDPTX(x)        (((uint64)(x) >> 30) & 0x1ff)
#define PDX(x)          (((uint64)(x) >> 21) & 0x1ff)
#define PTX(x)          (((uint64)(x) >> 12) & 0x1ff)

#define PTE_W           (1ULL << 1)
#define PTE_U           (1ULL << 2)
#define PTE_P           (1ULL << 0)
#define PTE_PCD         (1ULL << 4)

#define PTE_ADDR(x)     ((x) & ~0x3ff)

// slot for recursive mapping
#define	VREC    0x42ULL
#define	VTEMP   0x43ULL
// vdirect is 44
#define	VREC2   0x45ULL

#define	VUMAX   0x42ULL		// highest "user" mapping

#define CADDR(m, p, d, t) ((uint64 *)(m << 39 | p << 30 | d << 21 | t << 12))
#define SLOTNEXT(v)       ((v << 9) & ((1ULL << 48) - 1))

struct secret_t {
	uint64 dur[4];
#define	SEC_E820        0
#define	SEC_PMAP        1
#define	SEC_FREEPG      2
};

// XXX allocator should be in go

// regions of memory not included in the e820 map, into which we cannot
// allocate
static struct badregion_t {
	uint64 start;
	uint64 end;
} badregs[] = {
	// VGA
	{0xa0000, 0x100000},
	// secret storage
	{ROUNDDOWN(0x7c00, PGSIZE), ROUNDUP(0x7c00, PGSIZE)},
};

#pragma textflag NOSPLIT
uint64
skip_bad(uint64 cur)
{
	int32 num = sizeof(badregs)/sizeof(badregs[0]);
	int32 i;
	for (i = 0; i < num; i++) {
		if (cur >= badregs[i].start && cur < badregs[i].end)
			return badregs[i].end;
	}

	return cur;
}

// given to us by bootloader
uint64 pgfirst;
// determined by e820 map
uint64 pglast;

#pragma textflag NOSPLIT
void
init_pgfirst(void)
{
	// find e820 entry for the current page
	struct secret_t *secret = (struct secret_t *)0x7c00;
	struct e8e {
		uint64 dur[2];
	} *ep;
	// secret storage
	uint64 base = secret->dur[SEC_E820];
	int32 i;
	// bootloader provides 15 e820 entries at most (boot loader will panick
	// if the PC provides more)
	for (i = 0; i < 15; i++) {
		ep = (struct e8e *)(base + i * 28);
		// non-zero len
		if (!ep->dur[1])
			continue;

		uint64 end = ep->dur[0] + ep->dur[1];
		if (pgfirst >= ep->dur[0] && pgfirst < end) {
			pglast = end;
			break;
		}
	}
	//pmsg("kernel allocate from");
	//pnum(pgfirst);
	//pmsg("to");
	//pnum(pglast);
	assert(pglast, "no e820 seg for pgfirst?", pgfirst);
	assert((pgfirst & PGOFFMASK) == 0, "pgfirst not aligned", pgfirst);
}

#pragma textflag NOSPLIT
void
pgcheck(uint64 phys)
{
	//uint64 poison = 0xcafebabeb00bbeefULL;
	uint64 poison = 0xfefefffefffefeffULL;
	uint64 *recva = CADDR(VREC, VREC, VREC, VREC);
	if (recva[VTEMP] & PTE_P)
		runtime·pancake("not empty", 0);
	recva[VTEMP] = phys | PTE_P | PTE_W;
	int32 i;
	uint64 *p = (uint64 volatile *)CADDR(VREC, VREC, VREC, VTEMP);
	for (i = 0; i < PGSIZE/8; i++)
		p[i] = poison;
	for (i = 0; i < PGSIZE/8; i++)
		if (p[i] != poison)
			runtime·pancake("poison mismatch", p[i]);
	for (i = 0; i < PGSIZE/8; i++)
		p[i] = 0;
	recva[VTEMP] = 0;
	invlpg(p);
}

#pragma textflag NOSPLIT
uint64
get_pg(void)
{
	if (!pglast)
		init_pgfirst();

	// pgfirst is given by the bootloader
	pgfirst = skip_bad(pgfirst);

	if (pgfirst >= pglast)
		runtime·pancake("oom?", pglast);

	uint64 ret = pgfirst;
	pgfirst += PGSIZE;

	return ret;
}

#pragma textflag NOSPLIT
void
memset(void *va, uint32 c, uint64 sz)
{
	uint8 b = c;
	uint8 *p = (uint8 *)va;
	while (sz--)
		*p++ = b;
}

#pragma textflag NOSPLIT
void
memmov(void *dst, void *src, uint64 sz)
{
	uint8 *d = (uint8 *)dst;
	uint8 *s = (uint8 *)src;
	if (dst == src || sz == 0)
		return;
	if (d > s && d <= s + sz) {
		// copy backwards
		s += sz;
		d += sz;
		while (sz--)
			*--d = *--s;
		return;
	}
	while (sz--)
		*d++ = *s++;
}

#pragma textflag NOSPLIT
void
zero_phys(uint64 phys)
{
	phys = ROUNDDOWN(phys, PGSIZE);

	uint64 *recva = CADDR(VREC, VREC, VREC, VREC);
	if (recva[VTEMP] & PTE_P)
		runtime·pancake("vtemp in use?", recva[VTEMP]);
	recva[VTEMP] = phys | PTE_P | PTE_W;

	uint64 *va = CADDR(VREC, VREC, VREC, VTEMP);

	memset(va, 0, PGSIZE);

	recva[VTEMP] = 0;
	invlpg(va);
}

#pragma textflag NOSPLIT
static uint64 *
pgdir_walk1(uint64 *slot, uint64 van, int32 create)
{
	uint64 *ns = (uint64 *)SLOTNEXT((uint64)slot);
	ns += PML4X(van);
	if (PML4X(ns) != VREC)
		return slot;

	if (!(*slot & PTE_P)) {
		if (!create)
			return nil;
		uint64 np = get_pg();
		zero_phys(np);
		*slot = np | PTE_P | PTE_W;
	}

	return pgdir_walk1(ns, SLOTNEXT(van), create);
}

#pragma textflag NOSPLIT
uint64 *
pgdir_walk(void *va, int32 create)
{
	uint64 v = ROUNDDOWN((uint64)va, PGSIZE);
	if (ROUNDDOWN(v, PGSIZE) == 0 && create)
		runtime·pancake("mapping page 0", v);

	if (PML4X(v) == VREC)
		runtime·pancake("va collides w/VREC", v);

	uint64 *pml4 = CADDR(VREC, VREC, VREC, VREC);
	pml4 += PML4X(v);
	return pgdir_walk1(pml4, SLOTNEXT(v), create);
}

#pragma textflag NOSPLIT
uint64 *
pgdir_walk_other(uint64 p_pmap, void *va, int32 create)
{
	uint64 v = ROUNDDOWN((uint64)va, PGSIZE);

	if (PML4X(v) == VREC)
		runtime·pancake("va collides w/VREC", v);

	uint64 *pml4 = CADDR(VREC, VREC, VREC, VREC);
	uint64 old = pml4[VREC];
	pml4[VREC2] = old;
	pml4[VREC] = p_pmap | PTE_P;
	tlbflush();

	uint64 *ret = pgdir_walk(va, create);

	pml4 = CADDR(VREC2, VREC2, VREC2, VREC2);
	pml4[VREC] = old;
	tlbflush();

	return ret;
}

#pragma textflag NOSPLIT
static void
alloc_map(void *va, int32 perms, int32 fempty)
{
	uint64 *pte = pgdir_walk(va, 1);
	uint64 old = 0;
	if (pte)
		old = *pte;
	if (old & PTE_P && fempty)
		runtime·pancake("was not empty", (uint64)va);

	// XXX goodbye, memory
	uint64 np = get_pg();
	zero_phys(np);
	*pte = np | perms | PTE_P;
	if (old & PTE_P)
		invlpg(va);
}

#pragma textflag NOSPLIT
void *
find_empty(uint64 sz)
{
	uint8 *v = (uint8 *)CADDR(0, 0, 0, 1);
	uint64 *pte;
	// XXX sweet
	while (1) {
		pte = pgdir_walk(v, 0);
		if (!pte) {
			int32 i, failed = 0;
			for (i = 0; i < sz; i += PGSIZE) {
				pte = pgdir_walk(v + i, 0);
				if (pte) {
					failed = 1;
					v = v + i;
					break;
				}
			}

			if (!failed)
				return v;
		}
		v += PGSIZE;
	}
}

struct spinlock_t maplock;

#pragma textflag NOSPLIT
void
prot_none(uint8 *v, uint64 sz)
{
	int32 i;
	for (i = 0; i < sz; i += PGSIZE) {
		uint64 *pte = pgdir_walk(v + i, 1);
		if (pte != nil) {
			*pte = *pte & ~PTE_P;
			invlpg(v + i);
		}
	}
}

#pragma textflag NOSPLIT
void*
hack_mmap(void *va, uint64 sz, int32 prot, int32 flags, int32 fd, uint32 offset)
{
	splock(&maplock);

	USED(fd);
	USED(offset);
	uint8 *v = va;

	if (ROUNDUP(sz, PGSIZE)/PGSIZE > pglast - pgfirst) {
		spunlock(&maplock);
		return (void *)-1;
	}

	sz = ROUNDUP((uint64)v+sz, PGSIZE);
	sz -= ROUNDDOWN((uint64)v, PGSIZE);
	if (v == nil)
		v = find_empty(sz);

	if ((uint64)v >= (uint64)CADDR(VUMAX, 0, 0, 0))
		runtime·pancake("high addr?", (uint64)v);
	if ((uint64)v + sz >= (uint64)CADDR(VUMAX, 0, 0, 0))
		runtime·pancake("high addr2?", (uint64)v + sz);

	//pmsg("map\n");
	//pnum((uint64)v);
	//pnum(sz);
	//pmsg("\n");

	if (!(flags & MAP_ANON))
		runtime·pancake("not anon?", flags);
	if (!(flags & MAP_PRIVATE))
		runtime·pancake("not private?", flags);

	int32 perms = PTE_P;
	if (prot == PROT_NONE) {
		prot_none(v, sz);
		spunlock(&maplock);
		return v;
	}

	if (prot & PROT_WRITE)
		perms |= PTE_W;

	int32 i;
	for (i = 0; i < sz ; i += PGSIZE)
		alloc_map(v + i, perms, 1);

	spunlock(&maplock);
	return v;
}

#pragma textflag NOSPLIT
int32
hack_munmap(void *va, uint64 sz)
{
	splock(&maplock);

	// XXX TLB shootdowns?
	uint8 *v = (uint8 *)va;
	int32 i;
	sz = ROUNDUP(sz, PGSIZE);
	for (i = 0; i < sz; i+= PGSIZE) {
		uint64 *pte = pgdir_walk(v + i, 0);
		if (PML4X(v + i) >= VUMAX)
			runtime·pancake("unmap too high", (uint64)(v + i));
		// XXX goodbye, memory
		if (pte && *pte & PTE_P) {
			*pte = 0;
			invlpg(v + i);
		}
	}
	pmsg("POOF\n");

	spunlock(&maplock);
	return 0;
}

#pragma textflag NOSPLIT
void
stack_dump(uint64 rsp)
{
	uint64 *pte = pgdir_walk((void *)rsp, 0);
	pmsg("STACK DUMP      ");
	if (pte && *pte & PTE_P) {
		int32 i;
		uint64 *p = (uint64 *)rsp;
		for (i = 0; i < 70; i++) {
			pte = pgdir_walk(p, 0);
			if (pte && *pte & PTE_P)
				pnum(*p++);
		}
	} else {
		pmsg("bad stack");
		pnum(rsp);
		if (pte) {
			pmsg("pte:");
			pnum(*pte);
		} else
			pmsg("null pte");
	}
}

#pragma textflag NOSPLIT
void
runtime·Stackdump(uint64 rsp)
{
	stack_dump(rsp);
}

#pragma textflag NOSPLIT
int64
hack_write(int64 fd, const void *buf, uint32 c)
{
	if (fd != 1 && fd != 2)
		runtime·pancake("weird fd", (uint64)fd);

	int64 fl = pushcli();
	splock(&pmsglock);
	int64 ret = (int64)c;
	byte *p = (byte *)buf;
	while(c--)
		runtime·putch(*p++);
	spunlock(&pmsglock);
	popcli(fl);

	return ret;
}

#pragma textflag NOSPLIT
void
pgtest1(uint64 v)
{
	uint64 *va = (uint64 *)v;
	uint64 phys = get_pg();
	zero_phys(phys);

	uint64 *pte = pgdir_walk(va, 0);
	if (pte && *pte & PTE_P) {
		runtime·pancake("something mapped?", (uint64)pte);
	}

	pmsg("no mapping");
	pte = pgdir_walk(va, 1);
	*pte = phys | PTE_P | PTE_W;
	int32 i;
	for (i = 0; i < 512; i++)
		if (va[i] != 0)
			runtime·pancake("new page not zero?", va[i]);

	pmsg("zeroed");
	va[0] = 31337;
	va[256] = 31337;
	va[511] = 31337;

	//*pte = phys | PTE_P;
	//invlpg(va);
	//va[0] = 31337;

	//*pte = 0;
	//invlpg(va);
	//pnum(va[0]);
}

#pragma textflag NOSPLIT
void
pgtest(void)
{
	uint64 va[] = {0xc001d00d000ULL, 0xfffffffffffff000ULL,
	    0x1000ULL, 0x313371000ULL, 0x7f0000000000ULL,
	    (uint64)CADDR(0, 0xc, 0, 0) };

	int32 i;
	for (i = 0; i < sizeof(va)/sizeof(va[0]); i++)
		pgtest1(va[i]);
	runtime·pancake("GUT GUT GUT", 0);
}

#pragma textflag NOSPLIT
void
mmap_test(void)
{
	pmsg("mmap TEST");

	uint64 *va = (uint64 *)0xc001d00d000ULL;

	uint64 *ret = hack_mmap(va, 100*PGSIZE, PROT_READ|PROT_WRITE,
	    MAP_ANON | MAP_PRIVATE, -1, 0);

	if (ret != va)
		runtime·pancake("mmap failed?", (uint64)ret);

	int32 i;
	for (i = 0; i < 100*PGSIZE/sizeof(uint64); i++)
		ret[i] = 0;

	pmsg("mmap passed");
}

#define TRAP_PGFAULT    14
#define TRAP_SYSCALL    64
#define TRAP_TIMER      32
#define TRAP_DISK       (32 + 14)
#define TRAP_SPUR       48
#define TRAP_YIELD      49
#define TRAP_TLBSHOOT   70
#define TRAP_SIGRET     71

// HZ timer interrupts/sec
#define HZ		100
static uint32 lapic_quantum;
// picoseconds per CPU cycle
static uint64 pspercycle;

struct thread_t {
#define TFREGS       16
#define TFHW         7
#define TFSIZE       ((TFREGS + TFHW)*8)
	uint64 tf[TFREGS + TFHW];
	uint64 sigtf[TFREGS + TFHW];
	uint64 sigfx[512/8];
#define TF_RSP       (TFREGS + 5)
#define TF_RIP       (TFREGS + 2)
#define TF_CS        (TFREGS + 3)
#define TF_RFLAGS    (TFREGS + 4)
	#define		TF_FL_IF	(1 << 9)
#define TF_SS        (TFREGS + 6)
#define TF_TRAPNO    TFREGS
#define TF_RAX       15
#define TF_RBX       14
#define TF_RCX       13
#define TF_RDX       12
#define TF_RDI       11
#define TF_RSI       10
#define TF_FSBASE    0

	int64 status;
#define ST_INVALID   0
#define ST_RUNNABLE  1
#define ST_RUNNING   2
#define ST_WAITING   3	// waiting for a trap to be serviced
#define ST_SLEEPING  4
#define ST_WILLSLEEP 5
	int32 doingsig;
	// stack for signals, provided by the runtime via sigaltstack.
	// sigtramp switches m->g to the signal g so that stack checks
	// pass and thus we don't try to grow the stack.
	uint64 sigstack;
	struct prof_t {
		// nanoseconds until we fake SIGPROF
		int32 enabled;
		uint64 time;
		int64 curtime;
		uint64 stampstart;
	} prof;

	uint64 sleepfor;
	uint64 sleepret;
#define ETIMEDOUT   110
	uint64 futaddr;
	int64 pid;
	uint64 pmap;
	int64 notify;
};

struct cpu_t {
	// XXX missing go type info
	//struct thread_t *mythread;
	uint64 mythread;
	uint64 rsp;
	int32 num;
};

#define NTHREADS        64
// SSE state
// XXX move into thread_t
static uint64 fxstates[NTHREADS][512/8];
static struct thread_t threads[NTHREADS];
// index is lapic id
static struct cpu_t cpus[MAXCPUS];

#define curcpu               (cpus[lap_id()])
#define curthread            ((struct thread_t *)(curcpu.mythread))
#define setcurthread(x)      (curcpu.mythread = (uint64)x)

struct spinlock_t threadlock;
struct spinlock_t futexlock;

// newtrap is a function pointer to a user provided trap handler. alltraps
// jumps to newtrap if it is non-zero.
uint64 newtrap;

#pragma textflag NOSPLIT
static void
sched_halt(void)
{
	//if (lap_id() == 0) {
	//	int32 i;
	//	for (i = 0; i < NTHREADS; i++) {
	//		pnum(i);
	//		int8 *msg = "wtf";
	//		switch (threads[i].status) {
	//		case ST_INVALID:
	//			msg = "invalid";
	//			break;
	//		case ST_RUNNABLE:
	//			msg = "runnable";
	//			break;
	//		case ST_RUNNING:
	//			msg = "running";
	//			break;
	//		case ST_SLEEPING:
	//			msg = "sleeping";
	//			break;
	//		case ST_WAITING:
	//			msg = "waiting";
	//			break;
	//		case ST_SKIPSLEEP:
	//			msg = "skip sleep";
	//			break;
	//		}
	//		pmsg(msg);
	//		pmsg("\n");
	//	}
	//}

	//pmsg("hlt");
	cpu_halt(curcpu.rsp);
}

#pragma textflag NOSPLIT
uint64
hack_nanotime(void)
{
	uint64 cyc = runtime·Rdtsc();
	return (cyc * pspercycle)/1000;
}

#pragma textflag NOSPLIT
static void
sched_run(struct thread_t *t)
{
	assert(t->tf[TF_RFLAGS] & TF_FL_IF, "no interrupts?", 0);
	setcurthread(t);

	// if profiling, get a start time stamp
	if (t->prof.enabled && !t->doingsig)
		t->prof.stampstart = hack_nanotime();

	int64 idx = t - &threads[0];
	fxrstor(&fxstates[idx][0]);
	trapret(t->tf, t->pmap);
}

#pragma textflag NOSPLIT
static void
tcount(void)
{
	uint64 run, sleep, wait;
	run = sleep = wait = 0;
	int32 i;
	for (i = 0; i < NTHREADS; i++) {
		struct thread_t *t = &threads[i];
		if (t->status == ST_RUNNABLE || t->status == ST_RUNNING)
			run++;
		else if (t->status == ST_WAITING)
			wait++;
		else if (t->status == ST_SLEEPING || t->status == ST_WILLSLEEP)
			sleep++;
	}

	uint64 tot = run + sleep + wait;
	int32 bits = 64/4;
	uint64 v = tot << (3*bits) | run << (2*bits) | sleep << (1*bits) |
	    wait << (0*bits);
	pnum(v);
}

#pragma textflag NOSPLIT
static void
yieldy(void)
{
	//tcount();
	int32 start = curthread ? curthread - &threads[0] : 0;
	int32 i;
	for (i = (start + 1) % NTHREADS;
	     threads[i].status != ST_RUNNABLE;
	     i = (i + 1) % NTHREADS) {
	     	if (i == start) {
			setcurthread(0);
			spunlock(&threadlock);
			// does not return
			sched_halt();
		}
	}

	struct thread_t *tnext = &threads[i];
	tnext->status = ST_RUNNING;
	spunlock(&threadlock);

	sched_run(tnext);
}

#pragma textflag NOSPLIT
static void
yieldy_lock(void)
{
	assert((rflags() & TF_FL_IF) == 0, "interrupts enabled", 0);
	splock(&threadlock);
	yieldy();
}

#pragma textflag NOSPLIT
static
int32
thread_avail(void)
{
	runtime·stackcheck();

	assert((rflags() & TF_FL_IF) == 0, "thread state race", 0);
	assert(threadlock.lock, "threadlock not taken", 0);

	int32 i;
	for (i = 0; i < NTHREADS; i++) {
		if (threads[i].status == ST_INVALID)
			return i;
	}

	assert(0, "no available threads", 0);

	return -1;
}

#pragma textflag NOSPLIT
struct thread_t *
thread_find(int64 uc)
{
	runtime·stackcheck();

	assert((rflags() & TF_FL_IF) == 0, "interrupts enabled", 0);
	assert(threadlock.lock, "threadlock not taken", 0);

	int32 i;
	for (i = 0; i < NTHREADS; i++)
		if (threads[i].pid == uc)
			return &threads[i];
	return nil;
}

#pragma textflag NOSPLIT
void
runtime·Procadd(int64 pid, uint64 *tf, uint64 pmap)
{
	runtime·stackcheck();

	cli();
	splock(&threadlock);

	assert(pid != 0, "bad pid", pid);
	int32 nt = thread_avail();
	int32 i;
	for (i = 0; i < NTHREADS; i++)
		assert(threads[i].pid != pid, "uc exists", pid);

	struct thread_t *t = &threads[nt];
	memset(t, 0, sizeof(struct thread_t));

	memmov(t->tf, tf, TFSIZE);
	memmov(&fxstates[nt][0], fxinit, sizeof(fxinit));

	t->status = ST_RUNNABLE;
	t->pid = pid;
	t->pmap = pmap;
	t->doingsig = 0;
	t->prof.enabled = 0;

	spunlock(&threadlock);
	sti();
}

#pragma textflag NOSPLIT
static uint64 *
pte_mapped(void *va)
{
	uint64 *pte = pgdir_walk(va, 0);
	if (!pte || (*pte & PTE_P) == 0)
		return nil;
	return pte;
}

#pragma textflag NOSPLIT
static void
assert_mapped(void *va, int64 size, int8 *msg)
{
	byte *p = (byte *)ROUNDDOWN((uint64)va, PGSIZE);
	byte *end = (byte *)ROUNDUP((uint64)va + size, PGSIZE);
	for (; p < end; p += PGSIZE)
		if (pte_mapped(p) == nil)
			runtime·pancake(msg, (uint64)va);
}

#pragma textflag NOSPLIT
void
runtime·Procrunnable(int64 pid, uint64 *tf, uint64 pmap)
{
	runtime·stackcheck();

	cli();
	splock(&threadlock);
	struct thread_t *t = thread_find(pid);
	assert(t, "pid not found", pid);
	assert(t->status == ST_WAITING, "thread not waiting", t->status);

	t->status = ST_RUNNABLE;
	if (tf) {
		//assert_mapped(tf, TFSIZE, "bad tf");
		memmov(t->tf, tf, TFSIZE);
	}
	if (pmap) {
		t->pmap = pmap;
	}

	spunlock(&threadlock);
	sti();
}

#pragma textflag NOSPLIT
void
wakeup(void)
{
	uint64 now = hack_nanotime();
	// wake up timed out futexs
	int32 i;
	for (i = 0; i < NTHREADS; i++) {
		uint64 sf = threads[i].sleepfor;
		if (threads[i].status == ST_SLEEPING &&
				sf != -1 && sf < now) {
			threads[i].status = ST_RUNNABLE;
			threads[i].sleepfor = 0;
			threads[i].futaddr = 0;
			threads[i].sleepret = -ETIMEDOUT;
		}
	}
}

#pragma textflag NOSPLIT
void
runtime·Tfdump(uint64 *tf)
{
	pmsg("RIP:");
	pnum(tf[TF_RIP]);
	pmsg("\nRAX:");
	pnum(tf[TF_RAX]);
	pmsg("\nRDI:");
	pnum(tf[TF_RDI]);
	pmsg("\nRSI:");
	pnum(tf[TF_RSI]);
	pmsg("\nRBX:");
	pnum(tf[TF_RBX]);
	pmsg("\nRCX:");
	pnum(tf[TF_RCX]);
	pmsg("\nRDX:");
	pnum(tf[TF_RDX]);
	pmsg("\nRSP:");
	pnum(tf[TF_RSP]);
	pmsg("\n");
}

uint64 tlbshoot_wait;
uint64 tlbshoot_pg;
uint64 tlbshoot_count;
uint64 tlbshoot_pmap;

#pragma textflag NOSPLIT
void
runtime·Tlbadmit(uint64 pmap, uint64 cpuwait, uint64 pg, uint64 pgcount)
{
	runtime·stackcheck();
	while (!runtime·cas64(&tlbshoot_wait, 0, cpuwait))
		;

	runtime·xchg64(&tlbshoot_pg, pg);
	runtime·xchg64(&tlbshoot_count, pgcount);
	runtime·xchg64(&tlbshoot_pmap, pmap);
}

#pragma textflag NOSPLIT
void
tlb_shootdown(void)
{
	lap_eoi();
	struct thread_t *ct = curthread;

	if (ct && ct->pmap == tlbshoot_pmap) {
		// the TLB is flushed simply by taking an IPI since currently
		// each CPU switches to kernel page map on trap entry. the
		// following code will be necessary if we ever stop switching
		// to kernel page map on each trap.

		//uint64 start = tlbshoot_pg;
		//uint64 end = tlbshoot_pg + tlbshoot_count * PGSIZE;
		//pnum(lap_id() << 56 | start);
		//for (; start < end; start += PGSIZE)
		//	invlpg((uint64 *)start);
	}

	// decrement outstanding tlb shootdown count; if we change to a design
	// that doesn't broadcast shootdowns, only CPUs that the shootdown is
	// sent to should do this
	while (1) {
		uint64 old = runtime·atomicload64(&tlbshoot_wait);
		if (!old)
			runtime·pancake("count is 0", old);
		if (runtime·cas64(&tlbshoot_wait, old, old - 1))
			break;
	}

	if (ct)
		sched_run(ct);
	else
		sched_halt();
}

#pragma textflag NOSPLIT
void
sigret(struct thread_t *t)
{
	assert(t->status == ST_RUNNING, "uh oh2", 0);

	// restore pre-signal context
	runtime·memmove(t->tf, t->sigtf, TFSIZE);
	int32 idx = t - &threads[0];
	runtime·memmove(&fxstates[idx][0], t->sigfx, 512);

	// allow new signals
	t->doingsig = 0;

	sched_run(t);
}

// if sigsim() is used to deliver signals other than SIGPROF, you will need to
// construct siginfo_t and more of context.

// sigsim is executed by the runtime thread directly (ie not in interrupt
// context) on the signal stack. mksig() is used in interrupt context to setup
// and dispatch a signal context. we use an interrupt to restore pre-signal
// context because an interrupt switches to the interrupt stack so we can
// easily mark the task as signal-able again and restore old context (a task
// must be marked as signal-able only after the signal stack is no longer
// used).

// we could probably not use an interrupt and instead switch to pre-signal
// stack, then mark task as signal-able, and finally restore pre-signal
// context. the function implementing this should be not marked no-split
// though.
#pragma textflag NOSPLIT
void
sigsim(int32 signo, Siginfo *si, void *ctx)
{
	// SIGPROF handler doesn't use siginfo_t...
	USED(si);
	fakesig(signo, nil, ctx);
	intsigret();
}

static uint64 stacks[16];
const int64 stackn = sizeof(stacks)/sizeof(stacks[0]);

#pragma textflag NOSPLIT
void
mksig(struct thread_t *t, int32 signo)
{
	// save old context for sigret
	// XXX
	if ((t->tf[TF_RFLAGS] & TF_FL_IF) == 0) {
		assert(t->status == ST_WILLSLEEP, "how the fuke", t->status);
		t->tf[TF_RFLAGS] |= TF_FL_IF;
	}
	runtime·memmove(t->sigtf, t->tf, TFSIZE);
	// XXX duuuur
	int32 idx = t - &threads[0];
	runtime·memmove(t->sigfx, &fxstates[idx][0], 512);

	// these are defined by linux since we lie to the go build system that
	// we are running on linux...
	struct ucontext_t {
		uint64 uc_flags;
		uint64 uc_link;
		struct uc_stack_t {
			void *sp;
			int32 flags;
			uint64 size;
		} uc_stack;
		struct mcontext_t {
			//ulong	greg[23];
			uint64 r8;
			uint64 r9;
			uint64 r10;
			uint64 r11;
			uint64 r12;
			uint64 r13;
			uint64 r14;
			uint64 r15;
			uint64 rdi;
			uint64 rsi;
			uint64 rbp;
			uint64 rbx;
			uint64 rdx;
			uint64 rax;
			uint64 rcx;
			uint64 rsp;
			uint64 rip;
			uint64 eflags;
			uint16 cs;
			uint16 gs;
			uint16 fs;
			uint16 __pad0;
			uint64 err;
			uint64 trapno;
			uint64 oldmask;
			uint64 cr2;
			uint64	fpptr;
			uint64	res[8];
		} uc_mcontext;
		uint64 uc_sigmask;
	};

	uint64 *rsp = (uint64 *)t->sigstack;
	rsp -= sizeof(struct ucontext_t);
	struct ucontext_t *ctxt = (struct ucontext_t *)rsp;

	// the profiler only uses rip and rsp of the context...
	runtime·memclr((byte *)ctxt, sizeof(struct ucontext_t));
	ctxt->uc_mcontext.rip = t->tf[TF_RIP];
	ctxt->uc_mcontext.rsp = t->tf[TF_RSP];

	// simulate call to sigsim with args
	*--rsp = (uint64)ctxt;
	// nil siginfo_t
	*--rsp = 0;
	*--rsp = (uint64)signo;
	// bad return addr; shouldn't be reached
	*--rsp = 0;

	t->tf[TF_RSP] = (uint64)rsp;
	t->tf[TF_RIP] = (uint64)sigsim;
}

#pragma textflag NOSPLIT
static void
proftick(struct thread_t *t)
{
	// if profiling the kernel, do fake SIGPROF if we aren't already
	if (!t->prof.enabled || t->doingsig)
		return;
	assert(t->status == ST_RUNNING || t->status == ST_WILLSLEEP,
	    "not exclusive access", t->status);

	uint64 elapsed = hack_nanotime() - t->prof.stampstart;

	t->prof.stampstart = 0;
	t->prof.curtime -= elapsed;
	if (t->prof.curtime <= 0) {
		t->prof.curtime = t->prof.time;
		t->doingsig = 1;
		const int32 SIGPROF = 27;
		mksig(t, SIGPROF);
	}
}

#pragma textflag NOSPLIT
void
trap(uint64 *tf)
{
	lcr3(kpmap);
	uint64 trapno = tf[TF_TRAPNO];

	struct thread_t *ct = curthread;

	assert((rflags() & TF_FL_IF) == 0, "ints enabled in trap", 0);

	if (halt)
		while (1);

	void (*ntrap)(uint64 *, int64, int64);
	ntrap = (void (*)(uint64 *, int64, int64))newtrap;

	// don't add code before FPU context saving unless you've thought very
	// carefully! it is easy to accidentally and silently corrupt FPU state
	// (ie calling runtime·memmove) before it is saved below.

	// save FPU state immediately before we clobber it
	if (ct) {
		int32 idx = ct - &threads[0];
		fxsave(&fxstates[idx][0]);

		runtime·memmove(ct->tf, tf, TFSIZE);

		// proftick must come after trapframe copy
		proftick(ct);
	}

	int32 yielding = 0;
	// these interrupts are handled specially by the runtime
	if (trapno == TRAP_YIELD) {
		trapno = TRAP_TIMER;
		tf[TF_TRAPNO] = TRAP_TIMER;
		yielding = 1;
	} else if (trapno == TRAP_TLBSHOOT) {
		// does not return
		tlb_shootdown();
	} else if (trapno == TRAP_SIGRET) {
		// does not return
		sigret(ct);
	}

	if (trapno == TRAP_TIMER) {
		splock(&threadlock);
		if (ct) {
			if (ct->status == ST_WILLSLEEP) {
				ct->status = ST_SLEEPING;
				// XXX set IF, unlock
				ct->tf[TF_RFLAGS] |= TF_FL_IF;
				spunlock(&futexlock);
			} else if (ct->notify) {
				ct->notify = 0;
				ct->status = ST_WAITING;
				ntrap(nil, ct->pid, 1);
			} else
				ct->status = ST_RUNNABLE;
		}
		if (!yielding) {
			lap_eoi();
			if (curcpu.num == 0)
				wakeup();
		}
		// yieldy doesn't return
		yieldy();
	}

	// all other interrupts
	// if the interrupt is a CPU exception and not an IRQ during user
	// program execution, wait for kernel to handle it before running the
	// thread again.
	if (ct && ct->pid && (trapno == TRAP_SYSCALL || trapno < TRAP_TIMER))
		ct->status = ST_WAITING;

	if (ntrap) {
		// if ct is nil here, there is a kernel bug
		int64 ptid = ct ? ct->pid : 0;
		// catch kernel faults that occur while trying to handle user
		// traps
		if ((tf[TF_CS] & 3) == 0)
			ptid = 0;
		ntrap(tf, ptid, 0);
		runtime·pancake("newtrap returned!", 0);
	}

	pmsg("trap frame at");
	pnum((uint64)tf);

	pmsg("trapno");
	pnum(trapno);

	uint64 rip = tf[TF_RIP];
	pmsg("rip");
	pnum(rip);

	if (trapno == 14) {
		uint64 rcr2(void);
		uint64 cr2 = rcr2();
		pmsg("cr2");
		pnum(cr2);
	}

	uint64 rsp = tf[TF_RSP];
	stack_dump(rsp);

	runtime·pancake("trap", 0);
}

static uint64 lapaddr;

#pragma textflag NOSPLIT
static void
tss_setup(int32 myid)
{
	// alignment is for performance
	uint64 addr = (uint64)&tss[myid];
	if (addr & (16 - 1))
		runtime·pancake("tss not aligned", addr);

	uint64 *va = (uint64 *)(0xa100001000ULL + myid*(2*PGSIZE));
	// aps already have stack mapped
	if (myid == 0)
		alloc_map(va - 1, PTE_W, 1);

	uint64 rsp = (uint64)va;

	tss_set(&tss[myid], rsp);
	tss_seg_set(myid, addr, sizeof(struct tss_t) - 1, 0);

	ltr(TSS_SEG(myid) << 3);

	assert(lapaddr, "lapic not yet mapped", lapaddr);
	curcpu.rsp = rsp;
}

#pragma textflag NOSPLIT
uint32
rlap(uint32 reg)
{
	if (!lapaddr)
		runtime·pancake("lapaddr null?", lapaddr);
	volatile uint32 *p = (uint32 *)lapaddr;
	return p[reg];
}

#pragma textflag NOSPLIT
void
wlap(uint32 reg, uint32 val)
{
	if (!lapaddr)
		runtime·pancake("lapaddr null?", lapaddr);
	volatile uint32 *p = (uint32 *)lapaddr;
	p[reg] = val;
}

#pragma textflag NOSPLIT
uint64
lap_id(void)
{
	assert((rflags() & TF_FL_IF) == 0, "ints enabled for lapid", 0);

	if (!lapaddr)
		runtime·pancake("lapaddr null (id)", lapaddr);
	volatile uint32 *p = (uint32 *)lapaddr;

#define IDREG       (0x20/4)
	return p[IDREG] >> 24;
}

#pragma textflag NOSPLIT
void
lap_eoi(void)
{
	assert(lapaddr, "lapaddr null?", lapaddr);

#define EOIREG      (0xb0/4)
	wlap(EOIREG, 0);
}

#pragma textflag NOSPLIT
uint64
ticks_get(void)
{
#define CCREG       (0x390/4)
	return lapic_quantum - rlap(CCREG);
}

int64
pit_ticks(void)
{
#define CNT0		0x40
#define CNTCTL		0x43
	// counter latch command for counter 0
	int64 cmd = 0;
	outb(CNTCTL, cmd);
	int64 low = inb(CNT0);
	int64 high = inb(CNT0);
	return high << 8 | low;
}

// wait until 8254 resets the counter
void
pit_phasewait(void)
{
	// 8254 timers are 16 bits, thus always smaller than last;
	int64 last = 1 << 16;
	for (;;) {
		int64 cur = pit_ticks();
		if (cur > last)
			return;
		last = cur;
	}
}

#pragma textflag NOSPLIT
void
timer_setup(int32 calibrate)
{
	uint64 la = 0xfee00000ULL;

	// map lapic IO mem
	uint64 *pte = pgdir_walk((void *)la, 1);
	*pte = (uint64)la | PTE_W | PTE_P | PTE_PCD;
	lapaddr = la;
#define LVERSION    (0x30/4)
	uint32 lver = rlap(LVERSION);
	if (lver < 0x10)
		runtime·pancake("82489dx not supported", lver);

#define LVTIMER     (0x320/4)
#define DCREG       (0x3e0/4)
#define DIVONE      0xb
#define ICREG       (0x380/4)

#define MASKINT   (1 << 16)

#define LVSPUR     (0xf0/4)
	// enable lapic, set spurious int vector
	wlap(LVSPUR, 1 << 8 | TRAP_SPUR);

	// timer: periodic, int 32
	wlap(LVTIMER, 1 << 17 | TRAP_TIMER);
	// divide by
	wlap(DCREG, DIVONE);

	if (calibrate) {
		// figure out how many lapic ticks there are in a second; first
		// setup 8254 PIT since it has a known clock frequency. openbsd
		// uses a similar technique.
		const uint32 pitfreq = 1193182;
		const uint32 pithz = 100;
		const uint32 div = pitfreq/pithz;
		// rate generator mode, lsb then msb (if square wave mode is
		// used, the PIT uses div/2 for the countdown since div is
		// taken to be the period of the wave)
		outb(CNTCTL, 0x34);
		outb(CNT0, div & 0xff);
		outb(CNT0, div >> 8);

		// start lapic counting
		wlap(ICREG, 0x80000000);
		pit_phasewait();
		uint32 lapstart = rlap(CCREG);
		uint64 cycstart = runtime·Rdtsc();

		int32 i;
		for (i = 0; i < pithz; i++)
			pit_phasewait();

		uint32 lapend = rlap(CCREG);
		if (lapend > lapstart)
			runtime·pancake("lapic timer wrapped?", lapend);
		uint32 lapelapsed = lapstart - lapend;
		uint64 cycelapsed = runtime·Rdtsc() - cycstart;
		pmsg("LAPIC Mhz:");
		pnum(lapelapsed/(1000 * 1000));
		pmsg("\n");
		lapic_quantum = lapelapsed / HZ;

		pmsg("CPU Mhz:");
		pnum(cycelapsed/(1000 * 1000));
		pmsg("\n");
		pspercycle = (1000000000000ull)/cycelapsed;

		// disable PIT: one-shot, lsb then msb
		outb(CNTCTL, 0x32);
		outb(CNT0, div & 0xff);
		outb(CNT0, div >> 8);
	}

	// initial count; the LAPIC's frequency is not the same as the CPU's
	// frequency
	wlap(ICREG, lapic_quantum);

#define LVCMCI      (0x2f0/4)
#define LVINT0      (0x350/4)
#define LVINT1      (0x360/4)
#define LVERROR     (0x370/4)
#define LVPERF      (0x340/4)
#define LVTHERMAL   (0x330/4)

	// mask cmci, lint[01], error, perf counters, and thermal sensor
	wlap(LVCMCI,    MASKINT);
	// masking LVINT0 somewhow results in a GPfault?
	//wlap(LVINT0,    1 << MASKSHIFT);
	wlap(LVINT1,    MASKINT);
	wlap(LVERROR,   MASKINT);
	wlap(LVPERF,    MASKINT);
	wlap(LVTHERMAL, MASKINT);

#define IA32_APIC_BASE   0x1b
	uint64 rdmsr(uint64);
	uint64 reg = rdmsr(IA32_APIC_BASE);
	if (!(reg & (1 << 11)))
		runtime·pancake("lapic disabled?", reg);
	if (reg >> 12 != 0xfee00)
		runtime·pancake("weird base addr?", reg >> 12);

	uint32 lreg = rlap(LVSPUR);
	if (lreg & (1 << 12))
		pmsg("EOI broadcast surpression\n");
	if (lreg & (1 << 9))
		pmsg("focus processor checking\n");
	if (!(lreg & (1 << 8)))
		pmsg("apic disabled\n");
}

#pragma textflag NOSPLIT
void
proc_setup(void)
{
	assert(sizeof(threads[0].tf) == TFSIZE, "weird size",
	    sizeof(threads[0].tf));
	threads[0].status = ST_RUNNING;
	threads[0].pmap = kpmap;

	uint64 la = 0xfee00000ULL;
	uint64 *pte = pgdir_walk((void *)la, 0);
	if (pte && *pte & PTE_P)
		runtime·pancake("lapic mem mapped?", (uint64)pte);

	timer_setup(1);

	// 8259a - mask all irqs. see 2.5.3.6 in piix3 documentation.
	// otherwise an RTC timer interrupt (that turns into a double-fault
	// since the pic has not been programmed yet) comes in immediately
	// after sti.
	outb(0x20 + 1, 0xff);
	outb(0xa0 + 1, 0xff);

	tss_setup(0);
	curcpu.num = 0;
	setcurthread(&threads[0]);
	//pmsg("sizeof thread_t:");
	//pnum(sizeof(struct thread_t));
	//pmsg("\n");
}

#pragma textflag NOSPLIT
void
·Ap_setup(int64 myid)
{
	pmsg("cpu");
	pnum(myid);
	pmsg("joined\n");
	assert(myid >= 0 && myid < MAXCPUS, "id id large", myid);
	assert(lap_id() <= MAXCPUS, "lapic id large", myid);
	timer_setup(0);
	tss_setup(myid);
	fpuinit();
	assert(curcpu.num == 0, "slot taken", curcpu.num);

	int64 test = pushcli();
	assert((test & TF_FL_IF) == 0, "wtf!", test);
	popcli(test);
	assert((rflags() & TF_FL_IF) == 0, "wtf!", test);

	curcpu.num = myid;
	setcurthread(0);
}

#pragma textflag NOSPLIT
void
dummy(void)
{
	while (1) {
		pmsg("child!");
		runtime·deray(500000);
	}
}

#pragma textflag NOSPLIT
static void
clone_wrap(void (*fn)(void))
{
	runtime·stackcheck();
	fn();
	assert(0, "thread returned?", 0);
}

#pragma textflag NOSPLIT
void
hack_clone(int32 flags, void *stack, M *mp, G *gp, void (*fn)(void))
{
	runtime·stackcheck();

	cli();
	splock(&threadlock);

	uint64 *sp = stack;
	uint64 chk = CLONE_VM | CLONE_FS | CLONE_FILES | CLONE_SIGHAND | CLONE_THREAD;

	assert(flags == chk, "weird flags", flags);
	assert(pgdir_walk(sp - 1, 0), "stack slot 1 not mapped", (uint64)(sp - 1));
	assert(pgdir_walk(sp - 2, 0), "stack slot 2 not mapped", (uint64)(sp - 2));

	int32 i = thread_avail();
	sp--;
	*(sp--) = (uint64)fn;	// provide fn as arg
	//*sp-- = (uint64)dummy;
	*sp = 0xf1eaf1ea;	// fake return addr (clone_wrap never returns)

	struct thread_t *mt = &threads[i];
	memset(mt, 0, sizeof(*mt));
	mt->tf[TF_CS] = CODE_SEG << 3;
	mt->tf[TF_RSP] = (uint64)sp;
	mt->tf[TF_RIP] = (uint64)clone_wrap;
	mt->tf[TF_RFLAGS] = rflags() | TF_FL_IF;
	mt->tf[TF_FSBASE] = (uint64)mp->tls + 16;

	gp->m = mp;
	mp->tls[0] = (uintptr)gp;
	mp->procid = i;

	mt->status = ST_RUNNABLE;
	mt->pmap = kpmap;

	memmov(&fxstates[i][0], fxinit, sizeof(fxinit));

	spunlock(&threadlock);
	sti();
}

#pragma textflag NOSPLIT
void
clone_test(void)
{
	// XXX figure out what "missing go type information" means
	static uint8 mdur[sizeof(M)];
	static uint8 gdur[sizeof(G)];
	M *tm = (M *)mdur;
	G *tg = (G *)gdur;

	uint64 flags = CLONE_VM | CLONE_FS | CLONE_FILES | CLONE_SIGHAND | CLONE_THREAD;

	tg->stack.lo = 0x80000000 - PGSIZE;
	tg->stack.hi = 0x80000000;
	hack_clone(flags, (void *)(0x80000000 - (4096/2)), tm, tg, dummy);

	while (1) {
		pmsg("parent!");
		runtime·deray(500000);
	}
}

// use these to pass args because using the stack in ·Syscall causes strange
// runtime behavior. haven't figured out why yet.
int64 dur_sc_trap;
int64 dur_sc_a1;
int64 dur_sc_a2;
int64 dur_sc_a3;

int64 dur_sc_r1;
int64 dur_sc_r2;
int64 dur_sc_err;

// func Syscall(trap int64, a1, a2, a3 int64) (r1, r2, err int64);
#pragma textflag NOSPLIT
void
hack_syscall(void)
{
	int64 trap = dur_sc_trap;
	int64 a1 = dur_sc_a1;
	int64 a2 = dur_sc_a2;
	int64 a3 = dur_sc_a3;

	switch (trap) {
		default:
			runtime·pancake("weird syscall:", trap);
			break;
		// open
		case 2:
			{
			// catch unexpected opens; fail only /etc/localtime
			byte *p = (byte *)dur_sc_a1;
			byte l[] = "/etc/localtime";
			if (runtime·strcmp(p, l) == 0)
				dur_sc_err = -2;
			else {
				pmsg((int8 *)p);
				runtime·pancake("unexpected open", 0);
			}
			break;
			}
		// write
		case 1:
			dur_sc_r1 = hack_write((int32)a1, (void *)a2, (uint64)a3);
			dur_sc_r2  = 0;
			dur_sc_err = 0;
			break;
	}
}

struct timespec {
	int64 tv_sec;
	int64 tv_nsec;
};

// int64 futex(int32 *uaddr, int32 op, int32 val,
//	struct timespec *timeout, int32 *uaddr2, int32 val2);
#pragma textflag NOSPLIT
int64
hack_futex(int32 *uaddr, int32 op, int32 val,
    struct timespec *timeout, int32 *uaddr2, int32 val2)
{

	int64 ret = 0;
	USED(uaddr2);
	USED(val2);

	switch (op) {
	case FUTEX_WAIT:
	{
		assert_mapped(uaddr, 8, "futex uaddr");

		cli();
		splock(&futexlock);
		int32 dosleep = *uaddr == val;
		if (dosleep) {
			curthread->futaddr = (uint64)uaddr;
			// can't set to sleeping directly, otherwise another
			// cpu may wake and start executing the same thread
			// before we yield.
			curthread->status = ST_WILLSLEEP;
			curthread->sleepfor = -1;
			if (timeout) {
				assert_mapped(timeout, sizeof(struct timespec),
				    "futex timeout");
				int64 t = timeout->tv_sec * 1000000000;
				t += timeout->tv_nsec;
				curthread->sleepfor = hack_nanotime() + t;
			}
			hack_yield();
			// unlocks futexlock and returns with interrupts
			// enabled...
			cli();
			ret = curthread->sleepret;
			sti();
		} else {
			spunlock(&futexlock);
			sti();
			ret = -EAGAIN; // EAGAIN == EWOULDBLOCK
		}
		break;
	}
	case FUTEX_WAKE:
	{
		int32 i;
		int32 woke = 0;
		cli();
		splock(&futexlock);
		splock(&threadlock);
		for (i = 0; i < NTHREADS && val; i++) {
			uint64 st = threads[i].status;
			if (threads[i].futaddr == (uint64)uaddr &&
			    st == ST_SLEEPING) {
				threads[i].status = ST_RUNNABLE;
				threads[i].sleepfor = 0;
				threads[i].futaddr = 0;
				threads[i].sleepret = 0;
				val--;
				woke++;
			}
		}
		spunlock(&threadlock);
		spunlock(&futexlock);
		sti();
		ret = woke;
		break;
	}
	default:
		runtime·pancake("bad futex op", op);
	}

	return ret;
}

#pragma textflag NOSPLIT
void
hack_usleep(uint64 delay)
{
	struct timespec ts;
	ts.tv_sec = delay/1000000000;
	ts.tv_nsec = delay%1000000000;
	int32 dummy = 0;
	hack_futex(&dummy, FUTEX_WAIT, 0, &ts, nil, 0);
}

#pragma textflag NOSPLIT
void
hack_exit(int32 code)
{
	cli();
	curthread->status = ST_INVALID;

	pmsg("exit with code");
	pnum(code);
	pmsg(".\nhalting\n");
	volatile int32 *wtf = &halt;
	*wtf = 1;
	while(1);
}

// exported functions
// XXX remove the NOSPLIT once i've figured out why newstack is being called
// for a program whose stack doesn't seem to grow
#pragma textflag NOSPLIT
void
runtime·Cli(void)
{
	runtime·stackcheck();

	cli();
}

#pragma textflag NOSPLIT
uint64
runtime·Fnaddr(uint64 *fn)
{
	runtime·stackcheck();

	return *fn;
}

#pragma textflag NOSPLIT
uint64
runtime·Fnaddri(uint64 *fn)
{
	return runtime·Fnaddr(fn);
}

#pragma textflag NOSPLIT
uint64*
runtime·Kpmap(void)
{
	runtime·stackcheck();

	return CADDR(VREC, VREC, VREC, VREC);
}

#pragma textflag NOSPLIT
uint64
runtime·Kpmap_p(void)
{
	return kpmap;
}

#pragma textflag NOSPLIT
void
runtime·Lcr3(uint64 pmap)
{
	lcr3(pmap);
}

#pragma textflag NOSPLIT
uint64
runtime·Rcr3(void)
{
	runtime·stackcheck();

	return rcr3();
}

#pragma textflag NOSPLIT
int64
runtime·Inb(uint64 reg)
{
	runtime·stackcheck();
	int64 ret = inb(reg);
	return ret;
}

#pragma textflag NOSPLIT
void
runtime·Install_traphandler(uint64 *p)
{
	runtime·stackcheck();

	newtrap = *p;
}

#pragma textflag NOSPLIT
void
runtime·Memmove(void *dst, void *src, uintptr len)
{
	runtime·stackcheck();

	memmov(dst, src, len);
}

#pragma textflag NOSPLIT
void
runtime·Pnum(uint64 m)
{
	if (runtime·hackmode)
		pnum(m);
}

#pragma textflag NOSPLIT
void
runtime·Proccontinue(void)
{
	// no stack check because called from interrupt stack
	struct thread_t *ct = curthread;
	if (ct) {
		assert(ct->status == ST_RUNNING, "weird status", ct->status);
		sched_run(ct);
	}
	sched_halt();
}

#pragma textflag NOSPLIT
void
runtime·Prockill(int64 ptid)
{
	runtime·stackcheck();

	cli();
	splock(&threadlock);

	struct thread_t *t = thread_find(ptid);
	assert(t, "no such ptid", ptid);
	assert(t->status == ST_WAITING, "user proc not waiting?", t->status);
	t->status = ST_INVALID;
	t->pid = 0;
	t->notify = 0;
	t->doingsig = 0;
	t->prof.enabled = 0;
	t->sigstack = 0;

	spunlock(&threadlock);
	sti();
}

#pragma textflag NOSPLIT
int64
runtime·Procnotify(int64 ptid)
{
	runtime·stackcheck();

	if (ptid == 0)
		runtime·pancake("notify on 0", ptid);

	cli();
	splock(&threadlock);

	struct thread_t *t = thread_find(ptid);
	if (t == nil) {
		spunlock(&threadlock);
		sti();
		return 1;
	}
	t->notify = ptid;

	spunlock(&threadlock);
	sti();
	return 0;
}

#pragma textflag NOSPLIT
void
runtime·Procyield(void)
{
	// no stack check because called from interrupt stack
	yieldy_lock();
}

#pragma textflag NOSPLIT
uint64
runtime·Rcr2(void)
{
	return rcr2();
}

#pragma textflag NOSPLIT
uint64
runtime·Rrsp(void)
{
	return rrsp();
}

#pragma textflag NOSPLIT
void
runtime·Sti(void)
{
	runtime·stackcheck();

	sti();
}

#pragma textflag NOSPLIT
uint64
runtime·Vtop(void *va)
{
	runtime·stackcheck();

	uint64 van = (uint64)va;
	uint64 *pte = pte_mapped(va);
	if (!pte)
		return 0;
	uint64 base = PTE_ADDR(*pte);

	return base + (van & PGOFFMASK);
}

#pragma textflag NOSPLIT
void
runtime·Crash(void)
{
	pmsg("CRASH!\n");
	volatile int32 *wtf = &halt;
	*wtf = 1;
	while (1);
}

#pragma textflag NOSPLIT
void
runtime·Usleep(uint64 delay)
{
	runtime·stackcheck();
	hack_usleep(delay);
}

#pragma textflag NOSPLIT
void
runtime·Pmsga(uint8 *msg, int64 len, int8 a)
{
	runtime·stackcheck();

	int64 fl = pushcli();
	splock(&pmsglock);
	while (len--) {
		runtime·putcha(*msg++, a);
	}
	spunlock(&pmsglock);
	popcli(fl);
}

#pragma textflag NOSPLIT
uint64
runtime·Rflags(void)
{
	return rflags();
}

uint64 runtime·gcticks;

#pragma textflag NOSPLIT
uint64
runtime·Resetgcticks(void)
{
	runtime·stackcheck();
	uint64 ret = runtime·gcticks;
	runtime·gcticks = 0;
	return ret;
}

#pragma textflag NOSPLIT
uint64
runtime·Gcticks(void)
{
	runtime·stackcheck();
	return runtime·gcticks;
}

#pragma textflag NOSPLIT
void
hack_setitimer(uint32 timer, Itimerval *new, Itimerval *old)
{
	const uint32 TIMER_PROF = 2;
	if (timer != TIMER_PROF)
		runtime·pancake("weird timer", timer);
	USED(old);

	int64 fl = pushcli();

	struct thread_t *ct = curthread;
	uint64 nsecs = new->it_interval.tv_sec * 1000000000 +
	    new->it_interval.tv_usec * 1000;

	if (nsecs) {
		ct->prof.enabled = 1;
		ct->prof.time = nsecs;
		ct->prof.curtime = nsecs;
		ct->prof.stampstart = hack_nanotime();
		assert(ct->sigstack != 0, "no sig stack", 0);
	} else
		ct->prof.enabled = 0;

	popcli(fl);
}

#pragma textflag NOSPLIT
void
hack_sigaltstack(SigaltstackT *new, SigaltstackT *old)
{
	USED(old);

	int64 fl = pushcli();

	struct thread_t *ct = curthread;
	if (new->ss_flags & SS_DISABLE)
		ct->sigstack = 0;
	else
		ct->sigstack = (uint64)new->ss_sp + new->ss_size;

	popcli(fl);
}

uint64 runtime·doint;
int64 runtime·intcnt;
void (*runtime·Goint)(void);

#pragma textflag NOSPLIT
void
_handle_int(void)
{
	int32 biddle;
	runtime·stackcheck();
	// cast to 32 because i don't know how to insert movl in stackcheck
	// prologue.
	uint32 volatile *p = (uint32 volatile *)&runtime·doint;
	int64 doint = runtime·xchg(p, 0);
	if (doint == 0)
		return;
	//if (runtime·Goint == nil) {
	//	int32 *p = (int32 *)0;
	//	*p = 0;
	//}
	pmsg("inty time!");
	pnum((uint64)runtime·getcallerpc(&biddle));
}

// must only be called while interrupts are disabled
#pragma textflag NOSPLIT
void
runtime·Intpreempt(void)
{
	runtime·doint = 1;
}
