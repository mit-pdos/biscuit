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
void cli(void);
void lcr0(uint64);
void lcr3(uint64);
void lcr4(uint64);
void lidt(struct pdesc_t *);
void ltr(uint64);
void outb(uint32, uint32);
void runtime·stackcheck(void);
uint64 rcr2(void);
uint64 rflags(void);
uint64 rrsp(void);
void sti(void);
void trapret(uint64 *);

// this file
void lap_eoi(void);

#pragma textflag NOSPLIT
static void
putch(int8 x, int8 y, int8 c)
{
        volatile int16 *cons = (int16 *)0xb8000;
        cons[y*80 + x] = 0x07 << 8 | c;
}

#pragma textflag NOSPLIT
void
runtime·doc(int64 mark)
{
        static int8 x;
        static int8 y;

        putch(x++, y, mark & 0xff);
        //putch(x++, y, ' ');
        if (x >= 79) {
                x = 0;
                y++;
        }

	if (y >= 25)
		y = 0;
}

#pragma textflag NOSPLIT
void
pnum(uint64 n)
{
	uint64 nn = (uint64)n;
	int64 i;

	runtime·doc(' ');
	for (i = 60; i >= 0; i -= 4) {
		uint64 cn = (nn >> i) & 0xf;

		if (cn <= 9)
			runtime·doc('0' + cn);
		else
			runtime·doc('A' + cn - 10);
	}
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
pmsg(int8 *msg)
{
	runtime·doc(' ');
	if (msg)
		while (*msg)
			runtime·doc(*msg++);
}

#pragma textflag NOSPLIT
void
runtime·nmsg(int8 *msg)
{
	if (!runtime·hackmode)
		return;

	pmsg(msg);
}

#pragma textflag NOSPLIT
void
runtime·nmsg2(int8 *msg)
{
	runtime·nmsg(msg);
}

#pragma textflag NOSPLIT
void
runtime·pancake(void *msg, int64 addr)
{
	cli();
	pmsg(msg);

	pnum(addr);
	pmsg(" PANCAKE");
	while (1);
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

#define	G       0x80
#define	D       0x40
#define	L       0x20

#define	CODE    0xa
#define	DATA    0x2
#define	TSS     0x9
#define	USER    0x60

static struct seg64_t segs[8] = {
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

	// tss seg
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x80 | TSS,	// p, dpl, s, type
	 G,		// g, d/b, l, avail, mid limit
	 0},		// base high
	// 64 bit tss takes up two segment descriptor entires. the high 32bits
	// of the base are written in this seg desc.
	{0, 0, 0, 0, 0, 0, 0, 0},

	// 6 - 64 bit user code
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x90 | CODE | USER,	// p, dpl, s, type
	 G | L,		// g, d/b, l, avail, mid limit
	 0},		// base high

	// 7 - user data
	{0, 0,		// limit
	 0, 0, 0,	// base
	 0x90 | DATA | USER,	// p, dpl, s, type
	 G | D,	// g, d/b, l, avail, mid limit
	 0},		// base high
};

static struct pdesc_t pd;

#define	CODE_SEG        1
#define	FS_SEG          3
#define	TSS_SEG         4

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

extern void runtime·deray(uint64);
#pragma textflag NOSPLIT
void
marksleep(int8 *msg)
{
	pmsg(msg);
	runtime·deray(5000000);
}

struct idte_t {
	uint8 dur[16];
};

#define	INT     0xe
#define	TRAP    0xf

#define	NIDTE   65
struct idte_t idt[NIDTE];

#pragma textflag NOSPLIT
static void
int_set(struct idte_t *i, uint64 addr, uint32 trap, int32 user)
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
	i->dur[4] = 0;
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
	uint8 dur[26];
};

struct tss_t tss;

static void
tss_set(struct tss_t *tss, uint64 rsp0)
{
	uint32 off = 4;		// offset to rsp0 field

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
intsetup(void)
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
	extern void Xsyscall(void);

	int_set(&idt[ 0], (uint64) Xdz , 0, 0);
	int_set(&idt[ 1], (uint64) Xrz , 0, 0);
	int_set(&idt[ 2], (uint64) Xnmi, 0, 0);
	int_set(&idt[ 3], (uint64) Xbp , 0, 0);
	int_set(&idt[ 4], (uint64) Xov , 0, 0);
	int_set(&idt[ 5], (uint64) Xbnd, 0, 0);
	int_set(&idt[ 6], (uint64) Xuo , 0, 0);
	int_set(&idt[ 7], (uint64) Xnm , 0, 0);
	int_set(&idt[ 8], (uint64) Xdf , 0, 0);
	int_set(&idt[ 9], (uint64) Xrz2, 0, 0);
	int_set(&idt[10], (uint64) Xtss, 0, 0);
	int_set(&idt[11], (uint64) Xsnp, 0, 0);
	int_set(&idt[12], (uint64) Xssf, 0, 0);
	int_set(&idt[13], (uint64) Xgp , 0, 0);
	int_set(&idt[14], (uint64) Xpf , 0, 0);
	int_set(&idt[15], (uint64) Xrz3, 0, 0);
	int_set(&idt[16], (uint64) Xmf , 0, 0);
	int_set(&idt[17], (uint64) Xac , 0, 0);
	int_set(&idt[18], (uint64) Xmc , 0, 0);
	int_set(&idt[19], (uint64) Xfp , 0, 0);
	int_set(&idt[20], (uint64) Xve , 0, 0);

	int_set(&idt[32], (uint64) Xtimer, 0, 0);
	int_set(&idt[47], (uint64) Xspur, 0, 0);
	int_set(&idt[64], (uint64) Xsyscall, 0, 1);

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

#pragma textflag NOSPLIT
void
fpuinit(uint64 cr0, uint64 cr4)
{
	// for VEX prefixed instructions
	//// set NE and MP
	//cr0 |= 1 << 5;
	//cr0 |= 1 << 1;

	//// set OSXSAVE
	//cr4 |= 1 << 18;

	// clear EM
	cr0 &= ~(1 << 2);

	// set OSFXSR
	cr4 |= 1 << 9;

	lcr0(cr0);
	lcr4(cr4);
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

#define	VUMAX   0x42ULL		// highest "user" mapping

#define CADDR(m, p, d, t) ((uint64 *)(m << 39 | p << 30 | d << 21 | t << 12))
#define SLOTNEXT(v)       ((v << 9) & ((1ULL << 48) - 1))

struct secret_t {
	uint64 dur[3];
#define	SEC_E820	0
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
uint64 runtime·Pgfirst;
// determined by e820 map
uint64 runtime·Pglast;

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
	// bootloader provides 7 e820 entries at most (it will panick if the PC
	// provides more)
	for (i = 0; i < 7; i++) {
		ep = (struct e8e *)(base + i * 28);
		// non-zero len
		if (!ep->dur[1])
			continue;

		uint64 end = ep->dur[0] + ep->dur[1];
		if (runtime·Pgfirst >= ep->dur[0] && runtime·Pgfirst < end) {
			runtime·Pglast = end;
			break;
		}
	}
	assert(runtime·Pglast, "no e820 seg for Pgfirst?", runtime·Pgfirst);
}

#pragma textflag NOSPLIT
uint64
get_pg(void)
{
	if (!runtime·Pglast) {
		init_pgfirst();
	}

	// runtime·Pgfirst is given by the bootloader
	runtime·Pgfirst = skip_bad(runtime·Pgfirst);

	uint64 ret = runtime·Pgfirst;
	if (ret & PGOFFMASK)
		runtime·pancake("ret not aligned?", ret);

	runtime·Pgfirst += PGSIZE;

	if (ret >= runtime·Pglast)
		runtime·pancake("oom?", runtime·Pglast);

	return ret;
}

#pragma textflag NOSPLIT
void
memset(void *va, uint32 c, uint64 sz)
{
	uint8 b = (uint32)c;
	uint8 *p = (uint8 *)va;
	while (sz--)
		*p++ = b;
}

void invlpg(void *);

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

	if (PML4X(v) == VREC)
		runtime·pancake("va collides w/VREC", v);

	uint64 *pml4 = CADDR(VREC, VREC, VREC, VREC);
	pml4 += PML4X(v);
	return pgdir_walk1(pml4, SLOTNEXT(v), create);
}

#pragma textflag NOSPLIT
static void
alloc_map(void *va, int32 perms, int32 fempty)
{
	uint64 *pte = pgdir_walk(va, 1);
	uint64 old = *pte;
	// XXX goodbye, memory
	*pte = get_pg() | perms | PTE_P;
	if (old & PTE_P) {
		invlpg(va);
		if (fempty)
			runtime·pancake("was not empty", (uint64)va);
	}
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

#pragma textflag NOSPLIT
void*
hack_mmap(void *va, uint64 sz, int32 prot, int32 flags, int32 fd, uint32 offset)
{
	USED(fd);
	USED(offset);
	uint8 *v = (uint8 *)va;

	if ((uint64)v >= (uint64)CADDR(VUMAX, 0, 0, 0)) {
		runtime·pancake("high addr?", (uint64)v);
		v = nil;
	}
	sz = ROUNDUP((uint64)v+sz, PGSIZE);
	sz -= ROUNDDOWN((uint64)v, PGSIZE);
	if (v == nil)
		v = find_empty(sz);

	if (!(flags & MAP_ANON))
		runtime·pancake("not anon?", flags);
	if (!(flags & MAP_PRIVATE))
		runtime·pancake("not private?", flags);

	int32 perms = PTE_P;
	if (prot == PROT_NONE) {
		//hack_munmap(va, sz);
		return v;
	}

	if (prot & PROT_WRITE)
		perms |= PTE_W;

	int32 i;
	for (i = 0; i < sz ; i += PGSIZE)
		alloc_map(v + i, perms, 1);

	return v;
}

#pragma textflag NOSPLIT
int32
hack_munmap(void *va, uint64 sz)
{
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
	pmsg("POOF ");

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
		for (i = 0; i < 50; i++) {
			pte = pgdir_walk(p, 0);
			if (pte && *pte & PTE_P)
				pnum(*p++);
		}
	} else {
		pmsg("bad stack");
		pnum(rsp);
	}
}

#pragma textflag NOSPLIT
int64
hack_write(int32 fd, const void *buf, uint64 c)
{
	if (fd != 1 && fd != 2)
		runtime·pancake("weird fd", (uint64)fd);

	//pmsg("C>");
	//pnum(c);
	//pmsg("<C");
	if (c > 10000) {
		stack_dump(rrsp());
		runtime·pancake("weird len (expected)", c);
	}

	int64 ret = (int64)c;
	byte *p = (byte *)buf;
	while(c--) {
		runtime·doc(*p++);
		//if ((++i % 20) == 0)
		//	runtime·deray(5000000);
	}

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

#define TRAP_SYSCALL    64
#define TRAP_TIMER      32

#define TIMER_QUANTUM   100000000UL

struct thread_t {
#define TFREGS       16
#define TFHW         7
#define TFSIZE       ((TFREGS + TFHW)*8)
	uint64 tf[TFREGS + TFHW];
#define TF_RSP       (TFREGS + 5)
#define TF_RIP       (TFREGS + 2)
#define TF_CS        (TFREGS + 3)
#define TF_RFLAGS    (TFREGS + 4)
	#define		TF_FL_IF	(1 << 9)
#define TF_SS        (TFREGS + 6)
#define TF_TRAPNO    TFREGS
#define TF_FSBASE    0

	// free slot in threads?
	int32 valid;
	int32 runnable;
	int64 sleeping;
	uint64 futaddr;
	int64 pid;
	// non-zero if pid != 0
	uint64 pmap;
};

#define NTHREADS        5
struct thread_t threads[NTHREADS];
static int64 th_cur;

uint64 durnanotime;

// newtrap is a function pointer to a user provided trap handler. alltraps
// jumps to newtrap if it is non-zero.
uint64 newtrap;

#pragma textflag NOSPLIT
static void
yieldy(int32 resume)
{
	assert((rflags() & TF_FL_IF) == 0, "interrupts enabled", 0);
	assert(threads[th_cur].valid, "th_cur not valid?", th_cur);

	if (resume) {
		assert(threads[th_cur].runnable, "not runnable?", th_cur);
		trapret(threads[th_cur].tf);
	}

	int32 i;
	for (i = (th_cur + 1) % NTHREADS;
	     !threads[i].runnable;
	     i = (i + 1) % NTHREADS)
		;

	struct thread_t *tnext = &threads[i];
	assert(tnext->tf[TF_RFLAGS] & TF_FL_IF, "no interrupts?", 0);
	th_cur = i;

	lcr3(tnext->pmap);
	trapret(tnext->tf);
}

#pragma textflag NOSPLIT
static
int32
thread_avail(void)
{
	assert((rflags() & TF_FL_IF) == 0, "thread state race", 0);

	int32 i;
	for (i = 0; i < NTHREADS; i++) {
		if (threads[i].valid == 0)
			return i;
	}

	assert(0, "no available threads", 0);

	return -1;
}

#pragma textflag NOSPLIT
void
runtime·Procadd(uint64 *tf, int64 pid, uint64 pmap)
{
	cli();

	assert(pid != 0, "bad pid", pid);
	int32 nt = thread_avail();
	int32 i;
	for (i = 0; i < NTHREADS; i++)
		assert(threads[i].pid != pid, "uc exists", pid);

	struct thread_t *t = &threads[nt];
	memset(t, 0, sizeof(struct thread_t));

	runtime·memmove(t->tf, tf, TFSIZE);
	t->valid = 1;
	t->runnable = 1;
	t->pid = pid;
	t->pmap = pmap;

	sti();
}

#pragma textflag NOSPLIT
struct thread_t *
thread_find(int64 uc)
{
	assert((rflags() & TF_FL_IF) == 0, "interrupts enabled", 0);

	int32 i;
	for (i = 0; i < NTHREADS; i++)
		if (threads[i].pid == uc)
			return &threads[i];
	return nil;
}

#pragma textflag NOSPLIT
void
runtime·Procrunnable(int64 pid)
{
	struct thread_t *t;

	cli();
	t = thread_find(pid);
	assert(t, "pid not found", pid);

	t->runnable = 1;
	sti();
}

#pragma textflag NOSPLIT
void
trap(uint64 *tf)
{
	uint64 trapno = tf[TF_TRAPNO];

	if (trapno == TRAP_TIMER || trapno == TRAP_SYSCALL)
		runtime·memmove(threads[th_cur].tf, tf, TFSIZE);

	if (trapno == TRAP_TIMER) {
		// XXX convert CPU freq to ns
		durnanotime += TIMER_QUANTUM / 3;

		// wake up timed out futexs
		int32 i;
		for (i = 0; i < NTHREADS; i++) {
			if (threads[i].sleeping > 0 &&
			    threads[i].sleeping < durnanotime) {
				threads[i].runnable = 1;
			    	threads[i].sleeping = 0;
				threads[i].futaddr = 0;
			}
		}

		assert(threads[th_cur].valid, "th_cur not valid?", th_cur);

		lap_eoi();
		yieldy(0);
	} else {
		// wait for kernel to handle traps from user programs
		struct thread_t *ct = &threads[th_cur];
		if (ct->pid)
			ct->runnable = 0;
	}

	if (newtrap) {
		int64 uc = threads[th_cur].pid;
		((void (*)(uint64 *, int64))newtrap)(tf, uc);
		runtime·pancake("newtrap returned!", 0);
	}

	pmsg("trap frame at");
	pnum((uint64)tf);

	pmsg("trapno");
	pnum(trapno);

	pmsg("for thread");
	pnum(th_cur);

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

#pragma textflag NOSPLIT
static void
tss_setup(void)
{
	// alignment is for performance
	uint64 addr = (uint64)&tss;
	if (addr & (16 - 1))
		runtime·pancake("tss not aligned", addr);

	// XXX if we ever use a CPL != 0, we need to use a diff stack;
	// otherwise we will overwrite pre-trap stack
	//uint64 rsp = 0x80000000;
	uint64 *va = (uint64 *)0x2100000000ULL;
	alloc_map(va - 1, PTE_W, 1);
	uint64 rsp = (uint64)va;

	tss_set(&tss, rsp);
	seg_set(&segs[TSS_SEG], (uint32)addr, sizeof(tss) - 1, 0);

	// set high bits (TSS64 uses two segment descriptors
	uint32 haddr = addr >> 32;
	bw(&segs[TSS_SEG + 1].dur[0], haddr, 0);
	bw(&segs[TSS_SEG + 1].dur[1], haddr, 1);
	bw(&segs[TSS_SEG + 1].dur[2], haddr, 2);
	bw(&segs[TSS_SEG + 1].dur[3], haddr, 3);

	ltr(TSS_SEG << 3);
}

static uint64 lapaddr;

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
	return TIMER_QUANTUM - rlap(CCREG);
}

#pragma textflag NOSPLIT
void
timersetup(void)
{

	assert(th_cur == 0, "th_cur not zero", th_cur);
	assert(sizeof(threads[0].tf) == TFSIZE, "weird size", sizeof(threads[0].tf));
	threads[th_cur].valid = 1;
	threads[th_cur].runnable = 1;
	threads[th_cur].pmap = kpmap;

	uint64 la = (uint64)0xfee00000;

	// map lapic IO mem
	uint64 *pte = pgdir_walk((void *)la, 0);
	if (pte)
		runtime·pancake("lapic mem mapped?", (uint64)pte);
	pte = pgdir_walk((void *)la, 1);
	*pte = (uint64)la | PTE_W | PTE_P | PTE_PCD;
	lapaddr = la;

#define LVTIMER     (0x320/4)
#define DCREG       (0x3e0/4)
#define DIVONE      0xb
#define ICREG       (0x380/4)

	// timer: periodic, int 32
	wlap(LVTIMER, 1 << 17 | TRAP_TIMER);
	// divide by
	wlap(DCREG, DIVONE);
	// initial count
	wlap(ICREG, TIMER_QUANTUM);

#define LVCMCI      (0x2f0/4)
#define LVINT0      (0x350/4)
#define LVINT1      (0x360/4)
#define LVERROR     (0x370/4)
#define LVPERF      (0x340/4)
#define LVTHERMAL   (0x330/4)

#define MASKSHIFT   16

	// mask cmci, lint[01], error, perf counters, and thermal sensor
	wlap(LVCMCI,    1 << MASKSHIFT);
	// masking LVINT0 somewhow results in a GPfault?
	//wlap(LVINT0,    1 << MASKSHIFT);
	wlap(LVINT1,    1 << MASKSHIFT);
	wlap(LVERROR,   1 << MASKSHIFT);
	wlap(LVPERF,    1 << MASKSHIFT);
	wlap(LVTHERMAL, 1 << MASKSHIFT);

#define IA32_APIC_BASE   0x1b
	uint64 rdmsr(uint64);
	uint64 reg = rdmsr(IA32_APIC_BASE);
	if (!(reg & (1 << 11)))
		runtime·pancake("lapic disabled?", reg);
	if (reg >> 12 != 0xfee00)
		runtime·pancake("weird base addr?", reg >> 12);

#define LVSPUR     (0xf0/4)
	uint32 lreg = rlap(LVSPUR);
	if (lreg & (1 << 12))
		pmsg("EOI broadcast surpression");
	if (lreg & (1 << 9))
		pmsg("focus processor checking");
	if (!(lreg & (1 << 8)))
		pmsg("apic disabled");

	wlap(LVSPUR, 1 << 8 | 47);

	// 8259a - mask all ints. skipping this step results in GPfault too?
	outb(0x20 + 1, 0xff);
	outb(0xa0 + 1, 0xff);
}

#pragma textflag NOSPLIT
void
misc_init(void)
{
	tss_setup();
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
	cli();

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

	mt->valid = 1;
	mt->runnable = 1;
	mt->pmap = kpmap;

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

#pragma textflag NOSPLIT
uint64
hack_nanotime(void)
{
	return durnanotime + ticks_get();
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
			runtime·pancake("weird trap", trap);
			break;
		// write
		case 1:
			dur_sc_r1 = hack_write((int32)a1, (void *)a2, (uint64)a3);
			dur_sc_r2  = 0;
			dur_sc_err = 0;
			break;
	}
}

#pragma textflag NOSPLIT
void
hack_usleep(uint32 delay)
{
	runtime·deray(delay);
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
assert_addr(void *va, int8 *msg)
{
	if (pte_mapped(va) == nil)
		runtime·pancake(msg, (uint64)va);
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

	USED(uaddr2);
	USED(val2);

	switch (op) {
	case FUTEX_WAIT:
	{
		assert_addr(uaddr, "futex uaddr");

		int32 dosleep = *uaddr == val;
		if (dosleep) {
			cli();
			threads[th_cur].futaddr = (uint64)uaddr;
			threads[th_cur].sleeping = -1;
			threads[th_cur].runnable = 0;
			if (timeout) {
				assert_addr(timeout, "futex timeout");
				int64 t = timeout->tv_sec * 1000000000;
				t += timeout->tv_nsec;
				threads[th_cur].sleeping = hack_nanotime() + t;
			}
			sti();
			void hack_yield(void);
			hack_yield();
		} else {
			return -EAGAIN;
		}

		break;
	}
	case FUTEX_WAKE:
	{
		int32 i;
		for (i = 0; i < NTHREADS && val; i++) {
			if (threads[i].valid && threads[i].sleeping &&
			    threads[i].futaddr == (uint64)uaddr) {
			    	threads[i].sleeping = 0;
			    	threads[i].runnable = 1;
			    	threads[i].futaddr = 0;
				val--;
			}
		}
		break;
	}
	default:
		runtime·pancake("bad futex op", op);
	}

	return 0;
}

#pragma textflag NOSPLIT
void
hack_exit(int32 code)
{
	cli();
	pmsg("exit with code");
	pnum(code);
	while(1);
}

#pragma textflag NOSPLIT
void
cls(void)
{
	int32 i;
	for (i = 0; i < 1974; i++)
		runtime·doc(' ');
}

// exported functions
// XXX remove the NOSPLIT once i've figured out why newstack is being called
// for a program whose stack doesn't seem to grow
#pragma textflag NOSPLIT
void
runtime·Cli(void)
{
	cli();
}

#pragma textflag NOSPLIT
uint64
runtime·Fnaddr(uint64 *fn)
{
	return *fn;
}

#pragma textflag NOSPLIT
uint64*
runtime·Kpmap(void)
{
	return CADDR(VREC, VREC, VREC, VREC);
}

#pragma textflag NOSPLIT
void
runtime·Install_traphandler(uint64 *p)
{
	newtrap = *p;
}

#pragma textflag NOSPLIT
void
runtime·Pnum(uint64 m)
{
	pnum(m);
}

#pragma textflag NOSPLIT
uint64
runtime·Rcr2(void)
{
	return rcr2();
}

#pragma textflag NOSPLIT
void
runtime·Sti(void)
{
	sti();
}

#pragma textflag NOSPLIT
void
runtime·Proccontinue(void)
{
	yieldy(1);
}

#pragma textflag NOSPLIT
void
runtime·Prockill(int64 pid)
{
	struct thread_t *t;
	cli();

	t = thread_find(pid);
	t->valid = 0;
	t->runnable = 0;
	t->pid = 0;

	sti();
}

#pragma textflag NOSPLIT
void
runtime·Procyield(void)
{
	yieldy(0);
}

#pragma textflag NOSPLIT
uint64
runtime·Vtop(void *va)
{
	uint64 van = (uint64)va;
	uint64 *pte = pte_mapped(va);
	if (!pte)
		return 0;
	uint64 base = PTE_ADDR(*pte);

	return base + (van & PGOFFMASK);
}

#pragma textflag NOSPLIT
void
runtime·Turdyprog(void)
{
	uint16 *vga = (uint16 *)0xb8000;
	int32 x = 0;
	int32 i = 0;
	while (1) {
		vga[x++] = 'W' | (0x7 << 8);
		vga[x++] = 'o' | (0x7 << 8);
		vga[x++] = 'r' | (0x7 << 8);
		vga[x++] = 'k' | (0x7 << 8);
		vga[x++] = ' ' | (0x7 << 8);
		int32 j;
		for (j = 0; j < 100000000; j++);
		i++;
		if (i >= 20) {
			vga[x++] = 'F' | (0x7 << 8);
			vga[x++] = 'A' | (0x7 << 8);
			vga[x++] = 'U' | (0x7 << 8);
			vga[x++] = 'L' | (0x7 << 8);
			vga[x++] = 'T' | (0x7 << 8);
			int32 *p = (int32 *)0;
			*p = 0;
		} else if ((i % 5) == 0) {
			vga[x++] = 'S' | (0x7 << 8);
			vga[x++] = 'Y' | (0x7 << 8);
			vga[x++] = 'S' | (0x7 << 8);
			vga[x++] = 'C' | (0x7 << 8);
			vga[x++] = 'A' | (0x7 << 8);
			vga[x++] = 'L' | (0x7 << 8);
			vga[x++] = 'L' | (0x7 << 8);
			vga[x++] = ' ' | (0x7 << 8);
			void runtime·Death(void);
			runtime·Death();
		}
	}
}
