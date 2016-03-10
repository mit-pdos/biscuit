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
// Futexwakeup wakes up ·threads sleeping on addr.
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
void ·cli(void);
void ·finit(void);
void ·fxsave(uint64 *);
void ·htpause(void);
struct cpu_t* ·Gscpu(void);
uint64 ·Inb(int16);
void ·lcr0(uint64);
void lcr3(uint64);
void ·lcr4(uint64);
void ·Outb(uint16, uint8);
int64 ·Pushcli(void);
void ·Popcli(int64);
uint64 ·rcr0(void);
uint64 ·Rcr2(void);
uint64 rcr3(void);
uint64 ·rcr4(void);
uint64 ·Rdmsr(uint64);
uint64 ·rflags(void);
uint64 rrsp(void);
void ·sti(void);
void tlbflush(void);
void _trapret(uint64 *);
void ·Wrmsr(uint64, uint64);
void ·mktrap(uint64);
void ·fs_null(void);
void ·gs_null(void);

uint64 ·Rdtsc(void);

// src/runtime/sys_linux_amd64.s
void fakesig(int32, Siginfo *, void *);
void intsigret(void);

// src/runtime/os_linux.go
void runtime·cls(void);
void runtime·perfgather(uint64 *);
void runtime·perfmask(void);
void runtime·putch(int8);
void runtime·putcha(int8, int8);
void runtime·shadow_clear(void);

// src/runtime/proc.c
struct spinlock_t {
	volatile uint32 v;
};

void ·splock(struct spinlock_t *);
void ·spunlock(struct spinlock_t *);

// this file
void ·lap_eoi(void);
void runtime·deray(uint64);

void runtime·stackcheck(void);
void ·invlpg(void *);

extern struct spinlock_t *·pmsglock;

void ·_pnum(uint64 n);
void ·pnum(uint64 n);

#pragma textflag NOSPLIT
void
·_pmsg(int8 *msg)
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
	int64 fl = ·Pushcli();
	·splock(·pmsglock);
	·_pmsg(msg);
	·spunlock(·pmsglock);
	·Popcli(fl);
}

uint32 ·Halt;

void runtime·pancake(void *msg, int64 addr);

#define assert(x, y, z)        do { if (!(x)) runtime·pancake(y, z); } while (0)

#pragma textflag NOSPLIT
static void
bw(uint8 *d, uint64 data, uint64 off)
{
	*d = (data >> off*8) & 0xff;
}

#define MAXCPUS 32

#define	CODE_SEG        1

// physical address of current pmap, given to us by bootloader
extern uint64 ·p_kpmap;

int8 gostr[] = "go";

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

#define	VUMAX   0x42ULL		// highest runtime mapping

#define CADDR(m, p, d, t) ((uint64 *)(m << 39 | p << 30 | d << 21 | t << 12))

uint64 * ·pgdir_walk(void *va, uint8 create);

#pragma textflag NOSPLIT
void
stack_dump(uint64 rsp)
{
	uint64 *pte = ·pgdir_walk((void *)rsp, 0);
	·_pmsg("STACK DUMP\n");
	if (pte && *pte & PTE_P) {
		int32 i, pc = 0;
		uint64 *p = (uint64 *)rsp;
		for (i = 0; i < 70; i++) {
			pte = ·pgdir_walk(p, 0);
			if (pte && *pte & PTE_P) {
				·_pnum(*p++);
				if (((pc++ + 1) % 4) == 0)
					·_pmsg("\n");
			}
		}
	} else {
		pmsg("bad stack");
		·_pnum(rsp);
		if (pte) {
			·_pmsg("pte:");
			·_pnum(*pte);
		} else
			·_pmsg("null pte");
	}
}

#define TRAP_NMI	2
#define TRAP_PGFAULT	14
#define TRAP_SYSCALL	64
#define TRAP_TIMER	32
#define TRAP_DISK	(32 + 14)
#define TRAP_SPUR	48
#define TRAP_YIELD	49
#define TRAP_TLBSHOOT	70
#define TRAP_SIGRET	71
#define TRAP_PERFMASK	72

#define IRQ_BASE	32
#define IS_IRQ(x)	(x > IRQ_BASE && x <= IRQ_BASE + 15)
#define IS_CPUEX(x)	(x < IRQ_BASE)

// HZ timer interrupts/sec
#define HZ		100
static uint32 lapic_quantum;
// picoseconds per CPU cycle
uint64 ·Pspercycle;

struct thread_t {
// ======== don't forget to update the go definition too! ======
#define TFREGS       17
#define TFHW         7
#define TFSIZE       ((TFREGS + TFHW)*8)
	// general context
	uint64 tf[TFREGS + TFHW];
#define FXSIZE       512
#define FXREGS       (FXSIZE/8)
	// MMX/SSE state; must be 16 byte aligned or fx{save,rstor} will
	// generate #GP
	//uint64 _pad1;
	uint64 fx[FXREGS];
	// these both are pointers to go allocated memory, but they are only
	// non-nil during user program execution thus the GC will always find
	// tf and fxbuf via the G's syscallsp.
	struct user_t {
		uint64 tf;
		uint64 fxbuf;
	} user;
	// we could put this on the signal stack instead
	uint64 sigtf[TFREGS + TFHW];
	// don't care whether sigfx is 16 byte aligned since we never call
	// fx{save,rstor} on it directly.
	uint64 sigfx[FXREGS];
	uint64 sigstatus;
	uint64 sigsleepfor;
#define TF_RSP       (TFREGS + 5)
#define TF_RIP       (TFREGS + 2)
#define TF_CS        (TFREGS + 3)
#define TF_RFLAGS    (TFREGS + 4)
	#define		TF_FL_IF	(1 << 9)
#define TF_SS        (TFREGS + 6)
#define TF_TRAPNO    TFREGS
#define TF_RAX       16
#define TF_RBX       15
#define TF_RCX       14
#define TF_RDX       13
#define TF_RDI       12
#define TF_RSI       11
#define TF_RBP       10
#define TF_FSBASE    1
#define TF_SYSRSP    0

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
		uint64 enabled;
		uint64 totaltime;
		uint64 stampstart;
	} prof;

	uint64 sleepfor;
	uint64 sleepret;
#define ETIMEDOUT   110
	uint64 futaddr;
	uint64 p_pmap;
	//uint64 _pad2;
};

struct cpu_t {
// ======== don't forget to update the go definition too! ======
	// XXX missing go type info
	//struct thread_t *mythread;

	// if you add fields before rsp, asm in ·mktrap() needs to be updated

	// a pointer to this cpu_t
	uint64 this;
	uint64 mythread;
	uint64 rsp;
	uint64 num;
	// these are used only by Go code
	void *pmap;
	Slice pms;
	//uint64 pid;
};

#define NTHREADS        64
//static struct thread_t ·threads[NTHREADS];
extern struct thread_t ·threads[NTHREADS];
// index is lapic id
extern struct cpu_t ·cpus[MAXCPUS];

extern struct spinlock_t *·threadlock;
extern struct spinlock_t *·futexlock;

static uint64 _gimmealign;
extern uint64 ·fxinit[512/8];

void ·fpuinit(int8);

static uint16 cpuattrs[MAXCPUS];
#pragma textflag NOSPLIT
void
cpuprint(uint8 n, int32 row)
{
	uint16 *p = (uint16*)0xb8000;
	uint64 num = ·Gscpu()->num;
	p += num + row*80;
	uint16 attr = cpuattrs[num];
	cpuattrs[num] += 0x100;
	*p = attr | n;
}

#pragma textflag NOSPLIT
void
·Cprint(byte n, int64 row)
{
	cpuprint((uint8)n, (int32)row);
}

#pragma textflag NOSPLIT
static void
cpupnum(uint64 rip)
{
	uint64 i;
	for (i = 0; i < 16; i++) {
		uint8 c = (rip >> i*4) & 0xf;
		if (c < 0xa)
			c += '0';
		else
			c = 'a' + (c - 0xa);
		cpuprint(c, i);
	}
}

void ·sched_halt(void);

uint64 ·hack_nanotime(void);

void ·sched_run(struct thread_t *t);

void ·yieldy(void);

#pragma textflag NOSPLIT
static uint64 *
pte_mapped(void *va)
{
	uint64 *pte = ·pgdir_walk(va, 0);
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

void ·wakeup(void);

extern uint64 ·tlbshoot_pmap;
extern uint64 ·tlbshoot_wait;

void ·tlb_shootdown(void);

#pragma textflag NOSPLIT
void
·sigret(struct thread_t *t)
{
	assert(t->status == ST_RUNNING, "uh oh2", 0);

	// restore pre-signal context
	runtime·memmove(t->tf, t->sigtf, TFSIZE);
	runtime·memmove(t->fx, t->sigfx, FXSIZE);

	·splock(·threadlock);
	assert(t->sigstatus == ST_RUNNABLE || t->sigstatus == ST_SLEEPING,
	    "oh nyet", t->sigstatus);

	// allow new signals
	t->doingsig = 0;

	uint64 sf = t->sigsleepfor;
	uint64 st = t->sigstatus;
	t->sigsleepfor = t->sigstatus = 0;

	if (st == ST_WAITING) {
		t->sleepfor = sf;
		t->status = ST_WAITING;
		·yieldy();
	} else {
		// t->status is already ST_RUNNING
		·spunlock(·threadlock);
		·sched_run(t);
	}
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
	//intsigret();
	·mktrap(TRAP_SIGRET);
}

// caller must hold threadlock
#pragma textflag NOSPLIT
void
mksig(struct thread_t *t, int32 signo)
{
	assert(t->sigstack != 0, "no sig stack", t->sigstack);
	// save old context for sigret
	// XXX
	if ((t->tf[TF_RFLAGS] & TF_FL_IF) == 0) {
		assert(t->status == ST_WILLSLEEP, "how the fuke", t->status);
		t->tf[TF_RFLAGS] |= TF_FL_IF;
	}
	runtime·memmove(t->sigtf, t->tf, TFSIZE);
	runtime·memmove(t->sigfx, t->fx, FXSIZE);
	t->sigsleepfor = t->sleepfor;
	t->sigstatus = t->status;
	t->status = ST_RUNNABLE;
	t->doingsig = 1;

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
void
·timetick(struct thread_t *t)
{
	uint64 elapsed = ·hack_nanotime() - t->prof.stampstart;
	t->prof.stampstart = 0;
	t->prof.totaltime += elapsed;
}

// caller must hold threadlock
#pragma textflag NOSPLIT
void
·proftick(void)
{
	const uint64 profns = 10000000;
	static uint64 lastprof;
	uint64 n = ·hack_nanotime();

	if (n - lastprof < profns)
		return;
	lastprof = n;

	int32 i;
	for (i = 0; i < NTHREADS; i++) {
		// if profiling the kernel, do fake SIGPROF if we aren't
		// already
		struct thread_t *t = &·threads[i];
		if (!t->prof.enabled || t->doingsig)
			continue;
		// don't touch running ·threads
		if (t->status != ST_RUNNABLE)
			continue;
		const int32 SIGPROF = 27;
		mksig(t, SIGPROF);
	}
}

#pragma textflag NOSPLIT
void
·kernel_fault(uint64 *tf)
{
	uint64 trapno = tf[TF_TRAPNO];
	·_pmsg("trap frame at");
	·_pnum((uint64)tf);
	·_pmsg("trapno");
	·_pnum(trapno);
	uint64 rip = tf[TF_RIP];
	·_pmsg("rip");
	·_pnum(rip);
	if (trapno == TRAP_PGFAULT) {
		uint64 cr2 = ·Rcr2();
		·_pmsg("cr2");
		·_pnum(cr2);
	}
	uint64 rsp = tf[TF_RSP];
	stack_dump(rsp);
	runtime·pancake("kernel fault", trapno);
}

// exported functions
// XXX remove the NOSPLIT once i've figured out why newstack is being called
// for a program whose stack doesn't seem to grow
#pragma textflag NOSPLIT
void
runtime·Cli(void)
{
	runtime·stackcheck();

	·cli();
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
	return ·p_kpmap;
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
void
runtime·Pnum(uint64 m)
{
	if (runtime·hackmode)
		·pnum(m);
}

#pragma textflag NOSPLIT
void
runtime·Sti(void)
{
	runtime·stackcheck();

	·sti();
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
	volatile uint32 *wtf = &·Halt;
	*wtf = 1;
	while (1);
}

#pragma textflag NOSPLIT
void
runtime·Pmsga(uint8 *msg, int64 len, int8 a)
{
	runtime·stackcheck();

	int64 fl = ·Pushcli();
	·splock(·pmsglock);
	while (len--) {
		runtime·putcha(*msg++, a);
	}
	·spunlock(·pmsglock);
	·Popcli(fl);
}
