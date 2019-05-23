// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"runtime/internal/atomic"
	"runtime/internal/sys"
	"unsafe"
)

type mOS struct{}

//go:noescape
func futex(addr unsafe.Pointer, op int32, val uint32, ts, addr2 unsafe.Pointer, val3 uint32) int32

// Linux futex.
//
//	futexsleep(uint32 *addr, uint32 val)
//	futexwakeup(uint32 *addr)
//
// Futexsleep atomically checks if *addr == val and if so, sleeps on addr.
// Futexwakeup wakes up threads sleeping on addr.
// Futexsleep is allowed to wake up spuriously.

const (
	_FUTEX_PRIVATE_FLAG = 128
	_FUTEX_WAIT_PRIVATE = 0 | _FUTEX_PRIVATE_FLAG
	_FUTEX_WAKE_PRIVATE = 1 | _FUTEX_PRIVATE_FLAG
)

// Atomically,
//	if(*addr == val) sleep
// Might be woken up spuriously; that's allowed.
// Don't sleep longer than ns; ns < 0 means forever.
//go:nosplit
func futexsleep(addr *uint32, val uint32, ns int64) {
	var ts timespec

	// Some Linux kernels have a bug where futex of
	// FUTEX_WAIT returns an internal error code
	// as an errno. Libpthread ignores the return value
	// here, and so can we: as it says a few lines up,
	// spurious wakeups are allowed.
	if ns < 0 {
		futex(unsafe.Pointer(addr), _FUTEX_WAIT_PRIVATE, val, nil, nil, 0)
		return
	}

	// It's difficult to live within the no-split stack limits here.
	// On ARM and 386, a 64-bit divide invokes a general software routine
	// that needs more stack than we can afford. So we use timediv instead.
	// But on real 64-bit systems, where words are larger but the stack limit
	// is not, even timediv is too heavy, and we really need to use just an
	// ordinary machine instruction.
	if sys.PtrSize == 8 {
		ts.set_sec(ns / 1000000000)
		ts.set_nsec(int32(ns % 1000000000))
	} else {
		ts.tv_nsec = 0
		ts.set_sec(int64(timediv(ns, 1000000000, (*int32)(unsafe.Pointer(&ts.tv_nsec)))))
	}
	futex(unsafe.Pointer(addr), _FUTEX_WAIT_PRIVATE, val, unsafe.Pointer(&ts), nil, 0)
}

// If any procs are sleeping on addr, wake up at most cnt.
//go:nosplit
func futexwakeup(addr *uint32, cnt uint32) {
	ret := futex(unsafe.Pointer(addr), _FUTEX_WAKE_PRIVATE, cnt, nil, nil, 0)
	if ret >= 0 {
		return
	}

	// I don't know that futex wakeup can return
	// EAGAIN or EINTR, but if it does, it would be
	// safe to loop and call futex again.
	systemstack(func() {
		print("futexwakeup addr=", addr, " returned ", ret, "\n")
	})

	*(*int32)(unsafe.Pointer(uintptr(0x1006))) = 0x1006
}

func getproccount() int32 {
	// This buffer is huge (8 kB) but we are on the system stack
	// and there should be plenty of space (64 kB).
	// Also this is a leaf, so we're not holding up the memory for long.
	// See golang.org/issue/11823.
	// The suggested behavior here is to keep trying with ever-larger
	// buffers, but we don't have a dynamic memory allocator at the
	// moment, so that's a bit tricky and seems like overkill.
	const maxCPUs = 64 * 1024
	var buf [maxCPUs / 8]byte
	r := sched_getaffinity(0, unsafe.Sizeof(buf), &buf[0])
	if r < 0 {
		return 1
	}
	n := int32(0)
	for _, v := range buf[:r] {
		for v != 0 {
			n += int32(v & 1)
			v >>= 1
		}
	}
	if n == 0 {
		n = 1
	}
	return n
}

// Clone, the Linux rfork.
const (
	_CLONE_VM             = 0x100
	_CLONE_FS             = 0x200
	_CLONE_FILES          = 0x400
	_CLONE_SIGHAND        = 0x800
	_CLONE_PTRACE         = 0x2000
	_CLONE_VFORK          = 0x4000
	_CLONE_PARENT         = 0x8000
	_CLONE_THREAD         = 0x10000
	_CLONE_NEWNS          = 0x20000
	_CLONE_SYSVSEM        = 0x40000
	_CLONE_SETTLS         = 0x80000
	_CLONE_PARENT_SETTID  = 0x100000
	_CLONE_CHILD_CLEARTID = 0x200000
	_CLONE_UNTRACED       = 0x800000
	_CLONE_CHILD_SETTID   = 0x1000000
	_CLONE_STOPPED        = 0x2000000
	_CLONE_NEWUTS         = 0x4000000
	_CLONE_NEWIPC         = 0x8000000

	cloneFlags = _CLONE_VM | /* share memory */
		_CLONE_FS | /* share cwd, etc */
		_CLONE_FILES | /* share fd table */
		_CLONE_SIGHAND | /* share sig handler table */
		_CLONE_SYSVSEM | /* share SysV semaphore undo lists (see issue #20763) */
		_CLONE_THREAD /* revisit - okay for now */
)

//go:noescape
func clone(flags int32, stk, mp, gp, fn unsafe.Pointer) int32

// May run with m.p==nil, so write barriers are not allowed.
//go:nowritebarrier
func newosproc(mp *m, stk unsafe.Pointer) {
	/*
	 * note: strace gets confused if we use CLONE_PTRACE here.
	 */
	if false {
		print("newosproc stk=", stk, " m=", mp, " g=", mp.g0, " clone=", funcPC(clone), " id=", mp.id, " ostk=", &mp, "\n")
	}

	// Disable signals during clone, so that the new thread starts
	// with signals disabled. It will enable them in minit.
	var oset sigset
	sigprocmask(_SIG_SETMASK, &sigset_all, &oset)
	ret := clone(cloneFlags, stk, unsafe.Pointer(mp), unsafe.Pointer(mp.g0), unsafe.Pointer(funcPC(mstart)))
	sigprocmask(_SIG_SETMASK, &oset, nil)

	if ret < 0 {
		print("runtime: failed to create new OS thread (have ", mcount(), " already; errno=", -ret, ")\n")
		if ret == -_EAGAIN {
			println("runtime: may need to increase max user processes (ulimit -u)")
		}
		throw("newosproc")
	}
}

// Version of newosproc that doesn't require a valid G.
//go:nosplit
func newosproc0(stacksize uintptr, fn unsafe.Pointer) {
	stack := sysAlloc(stacksize, &memstats.stacks_sys)
	if stack == nil {
		write(2, unsafe.Pointer(&failallocatestack[0]), int32(len(failallocatestack)))
		exit(1)
	}
	ret := clone(cloneFlags, unsafe.Pointer(uintptr(stack)+stacksize), nil, nil, fn)
	if ret < 0 {
		write(2, unsafe.Pointer(&failthreadcreate[0]), int32(len(failthreadcreate)))
		exit(1)
	}
}

var failallocatestack = []byte("runtime: failed to allocate stack for the new OS thread\n")
var failthreadcreate = []byte("runtime: failed to create new OS thread\n")

const (
	_AT_NULL   = 0  // End of vector
	_AT_PAGESZ = 6  // System physical page size
	_AT_HWCAP  = 16 // hardware capability bit vector
	_AT_RANDOM = 25 // introduced in 2.6.29
	_AT_HWCAP2 = 26 // hardware capability bit vector 2
)

var procAuxv = []byte("/proc/self/auxv\x00")

var addrspace_vec [1]byte

func mincore(addr unsafe.Pointer, n uintptr, dst *byte) int32

func sysargs(argc int32, argv **byte) {
	n := argc + 1

	// skip over argv, envp to get to auxv
	for argv_index(argv, n) != nil {
		n++
	}

	// skip NULL separator
	n++

	// now argv+n is auxv
	auxv := (*[1 << 28]uintptr)(add(unsafe.Pointer(argv), uintptr(n)*sys.PtrSize))
	if sysauxv(auxv[:]) != 0 {
		return
	}
	// In some situations we don't get a loader-provided
	// auxv, such as when loaded as a library on Android.
	// Fall back to /proc/self/auxv.
	fd := open(&procAuxv[0], 0 /* O_RDONLY */, 0)
	if fd < 0 {
		// On Android, /proc/self/auxv might be unreadable (issue 9229), so we fallback to
		// try using mincore to detect the physical page size.
		// mincore should return EINVAL when address is not a multiple of system page size.
		const size = 256 << 10 // size of memory region to allocate
		p, err := mmap(nil, size, _PROT_READ|_PROT_WRITE, _MAP_ANON|_MAP_PRIVATE, -1, 0)
		if err != 0 {
			return
		}
		var n uintptr
		for n = 4 << 10; n < size; n <<= 1 {
			err := mincore(unsafe.Pointer(uintptr(p)+n), 1, &addrspace_vec[0])
			if err == 0 {
				physPageSize = n
				break
			}
		}
		if physPageSize == 0 {
			physPageSize = size
		}
		munmap(p, size)
		return
	}
	var buf [128]uintptr
	n = read(fd, noescape(unsafe.Pointer(&buf[0])), int32(unsafe.Sizeof(buf)))
	closefd(fd)
	if n < 0 {
		return
	}
	// Make sure buf is terminated, even if we didn't read
	// the whole file.
	buf[len(buf)-2] = _AT_NULL
	sysauxv(buf[:])
}

func sysauxv(auxv []uintptr) int {
	var i int
	for ; auxv[i] != _AT_NULL; i += 2 {
		tag, val := auxv[i], auxv[i+1]
		switch tag {
		case _AT_RANDOM:
			// The kernel provides a pointer to 16-bytes
			// worth of random data.
			startupRandomData = (*[16]byte)(unsafe.Pointer(val))[:]

		case _AT_PAGESZ:
			physPageSize = val
		}

		archauxv(tag, val)
		vdsoauxv(tag, val)
	}
	return i / 2
}

func osinit() {
	if hackmode != 0 {
		// the kernel uses Setncpu() to update ncpu to the number of
		// booted CPUs on startup
		ncpu = 1
	} else {
		ncpu = getproccount()
	}
	physPageSize = 4096
}

func Setncpu(n int32) {
	ncpu = n
}

var urandom_dev = []byte("/dev/urandom\x00")

func getRandomData(r []byte) {
	if hackmode != 0 {
		pmsg("no random!\n")
		return
	}
	if startupRandomData != nil {
		n := copy(r, startupRandomData)
		extendRandom(r, n)
		return
	}
	fd := open(&urandom_dev[0], 0 /* O_RDONLY */, 0)
	n := read(fd, unsafe.Pointer(&r[0]), int32(len(r)))
	closefd(fd)
	extendRandom(r, int(n))
}

func goenvs() {
	if hackmode == 0 {
		goenvs_unix()
	}
}

// Called to do synchronous initialization of Go code built with
// -buildmode=c-archive or -buildmode=c-shared.
// None of the Go runtime is initialized.
//go:nosplit
//go:nowritebarrierrec
func libpreinit() {
	initsig(true)
}

// Called to initialize a new m (including the bootstrap m).
// Called on the parent thread (main thread in case of bootstrap), can allocate memory.
func mpreinit(mp *m) {
	mp.gsignal = malg(32 * 1024) // Linux wants >= 2K
	mp.gsignal.m = mp
}

func gettid() uint32

// Called to initialize a new m (including the bootstrap m).
// Called on the new thread, cannot allocate memory.
func minit() {
	minitSignals()

	// for debuggers, in case cgo created the thread
	if hackmode == 0 {
		getg().m.procid = uint64(gettid())
	}
}

// Called from dropm to undo the effect of an minit.
//go:nosplit
func unminit() {
	unminitSignals()
}

//#ifdef GOARCH_386
//#define sa_handler k_sa_handler
//#endif

func sigreturn()
func sigtramp(sig uint32, info *siginfo, ctx unsafe.Pointer)
func cgoSigtramp()

//go:noescape
func sigaltstack(new, old *stackt)

//go:noescape
func setitimer(mode int32, new, old *itimerval)

//go:noescape
func rtsigprocmask(how int32, new, old *sigset, size int32)

//go:nosplit
//go:nowritebarrierrec
func sigprocmask(how int32, new, old *sigset) {
	rtsigprocmask(how, new, old, int32(unsafe.Sizeof(*new)))
}

func raise(sig uint32)
func raiseproc(sig uint32)

//go:noescape
func sched_getaffinity(pid, len uintptr, buf *byte) int32
func osyield()

// runtime/asm_amd64.s
func Cli()
func clone_call(uintptr)
func cpu_halt(uintptr)
func Cpuid(uint32, uint32) (uint32, uint32, uint32, uint32)
func fakesig(int32, unsafe.Pointer, *ucontext_t)
func finit()
func fs_null()
func fxrstor(*[FXREGS]uintptr)
func fxsave(*[FXREGS]uintptr)
func _Gscpu() *cpu_t
func gs_null()
func gs_set(*cpu_t)
func Htpause()
func invlpg(uintptr)
func Inb(uint16) uint
func Inl(int) int
func Insl(uint16, unsafe.Pointer, uint)
func Invlpg(unsafe.Pointer)
func Lcr0(uintptr)
func Lcr3(uintptr)
func Lcr4(uintptr)
func lgdt(pdesc_t)
func lidt(pdesc_t)
func ltr(uint)
func mktrap(int)
func Outb(uint16, uint8)
func Outl(int, int)
func Outsl(int, unsafe.Pointer, int)
func Outw(int, int)
func Popcli(int)
func Pushcli() int
func rflags() uintptr
func Rcr0() uintptr
func Rcr2() uintptr
func Rcr3() uintptr
func Rcr4() uintptr
func Rdmsr(int) uintptr
func Rdtsc() uint64
func Sgdt(*uintptr)
func Sidt(*uintptr)
func Store32(*uint32, uint32)
func Store64(*uint64, uint64)
func Or64(*uint64, uint64)
func And64(*uint64, uint64)
func stackcheck()
func Sti()
func _sysentry()
func _trapret(*[TFSIZE]uintptr)
func trapret(*[TFSIZE]uintptr, uintptr)
func _userint()
func _userret()
func _Userrun(*[TFSIZE]uintptr, bool, *cpu_t) (int, int)
//func Userrun(tf *[TFSIZE]uintptr, fxbuf *[FXREGS]uintptr,
//    p_pmap uintptr, fastret bool, pmap_ref *int32) (int, int, uintptr, bool)
func Wrmsr(int, int)

// adds src to dst
func Objsadd(src *Resobjs_t, dst *Resobjs_t)
// subs src from dst
func Objssub(src *Resobjs_t, dst *Resobjs_t)
// returns a bit mask which is set when the corresponding element of a is
// larger than b.
func Objscmp(a *Resobjs_t, b *Resobjs_t) uint

func Lfence()
func Sfence()
func Mfence()

// we have to carefully write go code that may be executed early (during boot)
// or in interrupt context. such code cannot allocate or call functions that
// that have the stack splitting prologue. the following is a list of go code
// that could result in code that allocates or calls a function with a stack
// splitting prologue.
// - function with interface argument (calls convT2E)
// - taking address of stack variable (may allocate)
// - using range to iterate over a string (calls stringiter*)

type cpu_t struct {
	this		*cpu_t
	mythread	*thread_t
	rsp		uintptr
	num		uint
	sysrsp		uintptr
	shadowcr3	uintptr
	shadowfs	uintptr
	tf	*[TFSIZE]uintptr
	fxbuf	*[FXREGS]uintptr
	// APIC id is nice because it is a reliable CPU identifier, even during
	// NMI interrupts (which may occur during a swapgs pair, making Gscpu()
	// return garbage during the NMI handler).
	apicid		uint32
	_		[56]uint8
}

func (c *cpu_t) _init(p *cpu_t) {
	*(*uintptr)(unsafe.Pointer(&c.this)) = (uintptr)(unsafe.Pointer(p))
}

func (c *cpu_t) setthread(t *thread_t) {
	*(*uintptr)(unsafe.Pointer(&c.mythread)) = (uintptr)(unsafe.Pointer(t))
}

var Cpumhz uint
var Pspercycle uint

const MAXCPUS int = 64

var cpus [MAXCPUS]cpu_t

type prof_t struct {
	enabled		int
	totaltime	int
	stampstart	int
}

// XXX rearrange these for better spatial locality; p_pmap should probably be
// near front
type thread_t struct {
	tf		[TFSIZE]uintptr
	//_pad		int
	fx		[FXREGS]uintptr
	sigtf		[TFSIZE]uintptr
	sigfx		[FXREGS]uintptr
	status		int
	doingsig	int
	sigstack	uintptr
	sigsize		uintptr
	prof		prof_t
	sleepfor	int
	sleepret	int
	futaddr		uintptr
	p_pmap		uintptr
	_pad2		int
}

//var DUR uintptr

// XXX fix these misleading names
const(
  TFSIZE       = 24
  FXREGS       = 64
  TFREGS       = 17
  TF_GSBASE    = 0
  TF_FSBASE    = 1
  TF_R8        = 9
  TF_RBP       = 10
  TF_RSI       = 11
  TF_RDI       = 12
  TF_RDX       = 13
  TF_RCX       = 14
  TF_RBX       = 15
  TF_RAX       = 16
  TF_TRAPNO    = TFREGS
  TF_RIP       = TFREGS + 2
  TF_CS        = TFREGS + 3
  TF_RSP       = TFREGS + 5
  TF_SS        = TFREGS + 6
  TF_RFLAGS    = TFREGS + 4
    TF_FL_IF     uintptr = 1 << 9
)

//go:nosplit
func Gscpu() *cpu_t {
	if rflags() & TF_FL_IF != 0 {
		pancake("must not be interruptible", 0)
	}
	return _Gscpu()
}

//go:nosplit
func NMI_Gscpu() *cpu_t {
	if rflags() & TF_FL_IF != 0 {
		pancake("must not be interruptible", 0)
	}
	me := lap_id()
	for i := range cpus {
		if cpus[i].apicid == me {
			return &cpus[i]
		}
	}
	pancake("apicid not found", 0)
	return nil
}

// returns the logical CPU identifier on which the calling thread was executing
// at some point.
func CPUHint() int {
	fl := Pushcli()
	ret := int(_Gscpu().num)
	Popcli(fl)
	return ret
}

//var Slows int

//go:nowritebarrierrec
//go:nosplit
func Userrun(tf *[TFSIZE]uintptr, fxbuf *[FXREGS]uintptr,
    p_pmap uintptr, fastret bool, pmap_ref *int32, tlb_ref *uint64) (int, int, uintptr, bool, uint64) {

	// {enter,exit}syscall() may not be worth the overhead. i believe the
	// only benefit for biscuit is that cpus running in the kernel could GC
	// while other cpus execute user programs.
	//entersyscall(0)
	Cli()
	//Slows++
	cpu := _Gscpu()
	mynum := uint64(cpu.num)

	var opmap uintptr
	var dopdec bool
	if cpu.shadowcr3 != p_pmap {
		sh := cpu.shadowcr3
		dopdec = sh != 0
		opmap = cpu.shadowcr3
		dur := (*uint32)(unsafe.Pointer(pmap_ref))
		atomic.Xadd(dur, 1)
		Lcr3(p_pmap)
		cpu.shadowcr3 = p_pmap
		Or64(tlb_ref, 1 << mynum)
	}

	// if doing a fast return after a syscall, we need to restore some user
	// state manually
	if cpu.shadowfs != tf[TF_FSBASE] {
		ia32_fs_base := 0xc0000100
		Wrmsr(ia32_fs_base, int(tf[TF_FSBASE]))
		cpu.shadowfs = tf[TF_FSBASE]
	}
	// save the user buffers in case an interrupt arrives while in user
	// mode
	if cpu.tf != tf {
		// we only save/restore SSE registers on cpu
		// exception/interrupt, not during syscall exit/return. this is
		// OK since sys5ABI defines the SSE registers to be
		// caller-saved.

		// disable these write barriers to prevent a CPU being
		// preempted with interrupts cleared.
		*(*uintptr)(unsafe.Pointer(&cpu.tf)) = uintptr(unsafe.Pointer(tf))
		*(*uintptr)(unsafe.Pointer(&cpu.fxbuf)) = uintptr(unsafe.Pointer(fxbuf))
	}

	if !fastret {
		fxrstor(fxbuf)
	}
	intno, aux := _Userrun(tf, fastret, cpu)

	Sti()
	//exitsyscall(0)
	return intno, aux, opmap, dopdec, mynum
}

type nmiprof_t struct {
	buf		[]uintptr
	bufidx		uint64
	LVTmask		bool
	evtsel		int
	evtmin		uint
	evtmax		uint
	gctrl		int
	backtracing	bool
	percpu [MAXCPUS]struct {
		scratch	[64]uintptr
		tfx	[FXREGS]uintptr
		_	[64]uint8
	}
}

var _nmibuf [4096*10]uintptr
var nmiprof = nmiprof_t{buf: _nmibuf[:]}

func SetNMI(mask bool, evtsel int, min, max uint, backtrace bool) {
	nmiprof.LVTmask = mask
	nmiprof.evtsel = evtsel
	nmiprof.evtmin = min
	nmiprof.evtmax = max
	nmiprof.backtracing = backtrace
	// create value for ia32_perf_global_ctrl, to easily enable pmcs. does
	// not enable fixed function counters.
	ax, _, _, _ := Cpuid(0xa, 0)
	npmc := (ax >> 8) & 0xff
	for i := uint32(0); i < npmc; i++ {
		nmiprof.gctrl |= 1 << i
	}
}

func TakeNMIBuf() ([]uintptr, bool) {
	ret := nmiprof.buf
	l := int(nmiprof.bufidx)
	if l > len(ret) {
		l = len(ret)
	}
	ret = ret[:l]
	full := false
	if l == len(_nmibuf) {
		full = true
	}

	nmiprof.bufidx = 0
	return ret, full
}

var _seed uint

//go:nosplit
func dumrand(low, high uint) uint {
	ra := high - low
	if ra == 0 {
		return low
	}
	_seed = _seed * 1103515245 + 12345
	ret := _seed & 0x7fffffffffffffff
	return low + (ret % ra)
}

//go:nosplit
func _consumelbr() {
	//lastbranch_tos := 0x1c9
	lastbranch_0_from_ip := 0x680
	//lastbranch_0_to_ip := 0x6c0

	// top of stack
	//last := Rdmsr(lastbranch_tos) & 0xf
	// XXX stacklen
	lbrlen := 16
	//l := 16 * 2
	l := 0
	for i := 0; i < lbrlen; i++ {
		from := Rdmsr(lastbranch_0_from_ip + i)
		mispred := uintptr(1 << 63)
		if from & mispred != 0 {
			l++
		}
	}
	if int(nmiprof.bufidx) + l >= len(nmiprof.buf) {
		return
	}
	idx := int(atomic.Xadd64(&nmiprof.bufidx, int64(l)))
	idx -= l
	for i := 0; i < 16; i++ {
		from := Rdmsr(lastbranch_0_from_ip + i)
		Wrmsr(lastbranch_0_from_ip + i, 0)
		mispred := uintptr(1 << 63)
		if from & mispred == 0 {
			continue
		}
		if idx >= len(nmiprof.buf) {
			Cpuprint('!', 1)
			break
		}
		nmiprof.buf[idx] = from
		idx++
	}
}

//go:nosplit
func _lbrreset(en bool) {
	ia32_debugctl := 0x1d9
	if !en {
		Wrmsr(ia32_debugctl, 0)
		return
	}

	// enable last branch records. filter every branch but to direct
	// calls/jmps (sandybridge onward has better filtering)
	lbr_select := 0x1c8
	nocplgt0 := 1 << 1
	//nojcc := 1 << 2
	norelcall := 1 << 3
	noindcall := 1 << 4
	nonearret := 1 << 5
	noindjmp := 1 << 6
	noreljmp := 1 << 7
	nofarbr := 1 << 8
	dv := nocplgt0 | norelcall | noindcall | nonearret | noindjmp |
	    noreljmp | nofarbr
	Wrmsr(lbr_select, dv)

	freeze_lbrs_on_pmi := 1 << 11
	lbrs := 1 << 0
	dv = lbrs | freeze_lbrs_on_pmi
	Wrmsr(ia32_debugctl, dv)
}

func backtracetramp(uintptr, *[TFSIZE]uintptr, *g)

var Lost struct {
	Go uint
	Full uint
	Gs uint
	User uint
}

type Tab_t struct {
	Rows [1000]struct {
		Count	int
		Rips	[8]uintptr
	}
	Dropped	int
}

var Tab Tab_t
var _ztab Tab_t

func (t *Tab_t) clear() {
	*t = _ztab
}

func (t *Tab_t) add(prof []uintptr) {
	if prof[0] == 0 {
		panic("no")
	}
outter:
	for ri := 0; ri < len(t.Rows); ri++ {
		if t.Rows[ri].Rips[0] == 0 {
			copy(t.Rows[ri].Rips[:], prof)
			t.Rows[ri].Count = 1
			return
		}
		ub := len(prof)
		if tl := len(t.Rows[ri].Rips); tl < ub {
			ub = tl
		}
		for i := 0; i < ub; i++ {
			if t.Rows[ri].Rips[i] != prof[i] {
				continue outter
			}
		}
		t.Rows[ri].Count++
		return
	}
	t.Dropped++
}

func Tabclear() {
	Tab.clear()
}

var Freq uint = (1 << 40)

func Bluh() {
	if hackmode == 0 || dumrand(0, Freq) != 0 {
		return
	}
	buf := make([]uintptr, 8)
	got := callers(1, buf)
	buf = buf[:got]

	Tab.add(buf)
	//print("--\n")
	//for i := range buf {
	//	print(hex(buf[i]), "\n")
	//}
}

//go:nosplit
//go:nowritebarrierrec
func _addone(rip uintptr) {
	idx := atomic.Xadd64(&nmiprof.bufidx, 2) - 2
	if idx + 2 < uint64(len(nmiprof.buf)) {
		cid := uintptr(NMI_Gscpu().num)
		nmiprof.buf[idx] = 0xfeedfacefeed0000 | cid
		nmiprof.buf[idx+1] = rip
	}
}

// runs on gsignal stack so that we can use gentraceback(). 0xdeadbeefdeadbeef
// and 0xfeedfacefeedface are sentinel values to indicate distinct backtraces.
// 0xfeedfacefeedface and 0xdeadbeefdeadbeef indicate that a backtrace failed
// (and thus only the RIP was recorded) or succeeded, respectively.
//go:nowritebarrierrec
func nmibacktrace1(tf *[TFSIZE]uintptr, gp *g) {
	pc := tf[TF_RIP]
	sp := tf[TF_RSP]

	cpu := NMI_Gscpu()
	buf := nmiprof.percpu[cpu.num].scratch[:]
	// similar to sigprof()
	if gp == nil || sp < gp.stack.lo || gp.stack.hi < sp || setsSP(pc) {
		_addone(tf[TF_RIP])
		//Lost.Go++
		return
	}

	did := gentraceback(pc, sp, 0, gp, 0, &buf[0], len(buf), nil,
	    nil, _TraceTrap|_TraceJumpStack)
	buf = buf[:did]
	need := uint64(len(buf) + 1)
	last := atomic.Xadd64(&nmiprof.bufidx, int64(need))
	idx := last - need
	if last < uint64(len(nmiprof.buf)) {
		dst := nmiprof.buf[idx:]
		cid := uintptr(NMI_Gscpu().num)
		dst[0] = 0xdeadbeefdead0000 | cid
		dst = dst[1:]
		copy(dst, buf)
	} else {
		Lost.Full++
	}
}

//go:nosplit
//go:nowritebarrierrec
func nmibacktrace(tf *[TFSIZE]uintptr) {
	if tf[TF_GSBASE] == 0 {
		_pmsg("!")
	}

	// if the nmi occurred between swapgs pair, getg() will return garbage.
	// detect this case by making sure gs does not point to the cpu_t
	cpu := NMI_Gscpu()
	if Gscpu() != cpu {
		_addone(tf[TF_RIP])
		//Lost.Gs++
		return
	}

	// XXX
	if (tf[TF_CS] & 3) != 0 || tf[TF_RFLAGS] & TF_FL_IF == 0 ||
	    cpu.mythread == nil {
		_addone(tf[TF_RIP])
		//Lost.User++
		return
	}

	og := getg()
	if og.m == nil || og.m.gsignal == nil {
		_addone(tf[TF_RIP])
		//Lost.Go++
		return
	}
	if og == og.m.gsignal {
		pancake("recursive NMIs?", 0)
	}
	if og.m != og.m.gsignal.m {
		pancake("oh shite", 0)
	}
	setg(og.m.gsignal)
	// indirectly call nmibacktrace1()...
	backtracetramp(og.m.gsignal.stack.hi, tf, og)
	setg(og)
}

func checky() {
	if hackmode != 0 {
		if rflags() & TF_FL_IF == 0 {
			pancake("must be interruptible", 0)
		}
	}
}

//go:nowritebarrierrec
//go:nosplit
func perfgather(tf *[TFSIZE]uintptr) {
	idx := atomic.Xadd64(&nmiprof.bufidx, 1) - 1
	if idx < uint64(len(nmiprof.buf)) {
		v := tf[TF_RIP]
		id := uintptr(NMI_Gscpu().num)
		v |= id << 56
		nmiprof.buf[idx] = v
	}
	//_consumelbr()
}

//go:nosplit
func perfmask() {
	lapaddr := 0xfee00000
	lap := (*[PGSIZE/4]uint32)(unsafe.Pointer(uintptr(lapaddr)))

	perfmonc := 208
	if nmiprof.LVTmask {
		mask := uint32(1 << 16)
		lap[perfmonc] = mask
		_pmcreset(false)
	} else {
		// unmask perf LVT, reset pmc
		nmidelmode :=  uint32(0x4 << 8)
		lap[perfmonc] = nmidelmode
		_pmcreset(true)
	}
}

//go:nosplit
func _pmcreset(en bool) {
	ia32_pmc0 := 0xc1
	ia32_perfevtsel0 := 0x186
	ia32_perf_global_ovf_ctrl := 0x390
	ia32_debugctl := 0x1d9
	ia32_global_ctrl := 0x38f
	//ia32_perf_global_status := uint32(0x38e)

	// disable perf counter before clearing
	Wrmsr(ia32_perfevtsel0, 0)

	if !en {
		return
	}

	// clear overflow
	Wrmsr(ia32_perf_global_ovf_ctrl, 1)

	r := dumrand(nmiprof.evtmin, nmiprof.evtmax)
	Wrmsr(ia32_pmc0, -int(r))

	freeze_pmc_on_pmi := 1 << 12
	Wrmsr(ia32_debugctl, freeze_pmc_on_pmi)
	// cpu clears global_ctrl on PMI if freeze-on-pmi is set.
	// re-enable
	Wrmsr(ia32_global_ctrl, nmiprof.gctrl)

	v := nmiprof.evtsel
	Wrmsr(ia32_perfevtsel0, v)

	// the write to debugctl enabling LBR must come after clearing overflow
	// via global_ovf_ctrl; otherwise the processor instantly clears lbr...
	//_lbrreset(en)
}

//go:nosplit
func sc_setup() {
	// disable interrupts
	Outb(com1 + 1, 0)

	// set divisor latch bit to set divisor bytes
	Outb(com1 + 3, 0x80)

	// set both bytes for divisor baud rate (the denominator in the
	// expression below is the baud rate)
	//Outb(com1 + 0, 115200/9600)
	Outb(com1 + 0, 115200/115200)
	Outb(com1 + 1, 0)

	// 8 bit words, one stop bit, no parity
	Outb(com1 + 3, 0x03)

	// configure FIFO for interrupts: maximum FIFO size, clear
	// transmit/receive FIFOs, and enable FIFOs.
	Outb(com1 + 2, 0xc7)
	Outb(com1 + 4, 0x0b)
	Outb(com1 + 1, 1)
}

const com1 = uint16(0x3f8)

//go:nosplit
func sc_put_(c int8) {
	lstatus := uint16(5)
	for Inb(com1 + lstatus) & 0x20 == 0 {
	}
	Outb(com1, uint8(c))
}

var _scx int

//go:nosplit
func sc_put(c int8) {
	if c == '\n' {
		sc_put_('\r')
		sc_put_(c)
		_scx = 0
	} else if _scx == 80 {
		sc_put_(c)
		sc_put_('\r')
		sc_put_('\n')
		_scx = 0
	} else if c == '\b' {
		if _scx > 0 {
			// clear the previous character
			sc_put_('\b')
			sc_put_(' ')
			sc_put_('\b')
			_scx--
		}
	} else {
		sc_put_(c)
		_scx++
	}
}

type put_t struct {
	vx		int
	vy		int
	fakewrap	bool
}

var put put_t

//go:nosplit
func vga_put(c int8, attr int8) {
	p := (*[1999]int16)(unsafe.Pointer(uintptr(0xb8000)))
	if c != '\n' {
		// erase the previous line
		a := int16(attr) << 8
		backspace := c == '\b'
		if backspace {
			put.vx--
			c = ' '
		}
		v := a | int16(c)
		p[put.vy * 80 + put.vx] = v
		if !backspace {
			put.vx++
		}
		put.fakewrap = false
	} else {
		// if we wrapped the text because of a long line in the
		// immediately previous call to vga_put, don't add another
		// newline if we asked to print '\n'.
		if put.fakewrap {
			put.fakewrap = false
		} else {
			put.vx = 0
			put.vy++
		}
	}
	if put.vx >= 79 {
		put.vx = 0
		put.vy++
		put.fakewrap = true
	}

	if put.vy >= 25 {
		put.vy = 0
	}
	if put.vx == 0 {
		for i := 0; i < 79; i++ {
			p[put.vy * 80 + put.vx + i] = 0
		}
	}
}

var SCenable bool = true

//go:nosplit
func putch(c int8) {
	vga_put(c, 0x7)
	if SCenable {
		sc_put(c)
	}
}

//go:nosplit
func putcha(c int8, a int8) {
	vga_put(c, a)
	if SCenable {
		sc_put(c)
	}
}

//go:nosplit
func cls() {
	for i:= 0; i < 1974; i++ {
		vga_put(' ', 0x7)
	}
	sc_put('c')
	sc_put('l')
	sc_put('s')
	sc_put('\n')
}

func Hmode() bool {
	return hackmode != 0
}

var hackmode int64
var Halt uint32

type Spinlock_t struct {
	v	uint32
}

//go:nosplit
func Splock(l *Spinlock_t) {
	for {
		if atomic.Xchg(&l.v, 1) == 0 {
			break
		}
		for l.v != 0 {
			Htpause()
		}
	}
}

//go:nosplit
func Spunlock(l *Spinlock_t) {
	atomic.Store(&l.v, 0)
	//l.v = 0
}

// since this lock may be taken during an interrupt (only under fatal error
// conditions), interrupts must be cleared before attempting to take this lock.
var pmsglock = &Spinlock_t{}

//go:nosplit
func _pmsg(msg string) {
	putch(' ');
	// can't use range since it results in calls stringiter2 which has the
	// stack splitting proglogue
	for i := 0; i < len(msg); i++ {
		putch(int8(msg[i]))
	}
}

// msg must be utf-8 string
//go:nosplit
func pmsg(msg string) {
	fl := Pushcli()
	Splock(pmsglock)
	_pmsg(msg)
	Spunlock(pmsglock)
	Popcli(fl)
}

//go:nosplit
func _pnum(n uintptr) {
	putch(' ')
	for i := 60; i >= 0; i -= 4 {
		cn := (n >> uint(i)) & 0xf
		if cn <= 9 {
			putch(int8('0' + cn))
		} else {
			putch(int8('A' + cn - 10))
		}
	}
}

//go:nosplit
func pnum(n uintptr) {
	fl := Pushcli()
	Splock(pmsglock)
	_pnum(n)
	Spunlock(pmsglock)
	Popcli(fl)
}

func Pmsga(_p *uint8, c int, attr int8) {
	pn := uintptr(unsafe.Pointer(_p))
	fl := Pushcli()
	Splock(pmsglock)
	for i := uintptr(0); i < uintptr(c); i++ {
		p := (*int8)(unsafe.Pointer(pn+i))
		putcha(*p, attr)
	}
	Spunlock(pmsglock)
	Popcli(fl)
}

var _cpuattrs [MAXCPUS]uint16

//go:nosplit
func Cpuprint(n uint16, row uintptr) {
	p := uintptr(0xb8000)
	num := Gscpu().num
	p += (uintptr(num) + row*80)*2
	attr := _cpuattrs[num]
	_cpuattrs[num] += 0x100
	*(*uint16)(unsafe.Pointer(p)) = attr | n
}

//go:nosplit
func cpupnum(rip uintptr) {
	for i := uintptr(0); i < 16; i++ {
		c := uint16((rip >> i*4) & 0xf)
		if c < 0xa {
			c += '0'
		} else {
			c = 'a' + (c - 0xa)
		}
		Cpuprint(c, i)
	}
}

//go:nosplit
func pancake(msg string, addr uintptr) {
	Pushcli()
	atomic.Store(&Halt, 1)
	_pmsg(msg)
	_pnum(addr)
	_pmsg("PANCAKE")
	a := 0
	stack_dump(uintptr(unsafe.Pointer(&a)))
	for {
		p := (*uint16)(unsafe.Pointer(uintptr(0xb8002)))
		*p = 0x1400 | 'F'
	}
}

var gostr = []int8{'g', 'o', 0}

//go:nosplit
func chkalign(_p unsafe.Pointer, n uintptr) {
	p := uintptr(_p)
	if p & (n - 1) != 0 {
		pancake("not aligned", p)
	}
}

//go:nosplit
func chksize(n uintptr, exp uintptr) {
	if n != exp {
		pancake("size mismatch", n)
	}
}

type pdesc_t struct {
	limit	uint16
	addrlow uint16
	addrmid	uint32
	addrhi	uint16
	_res1	uint16
	_res2	uint32
}

type seg64_t struct {
	lim	uint16
	baselo	uint16
	rest	uint32
}

type tss_t struct {
	_res0 uint32

	rsp0l uint32
	rsp0h uint32
	rsp1l uint32
	rsp1h uint32
	rsp2l uint32
	rsp2h uint32

	_res1 [2]uint32

	ist1l uint32
	ist1h uint32
	ist2l uint32
	ist2h uint32
	ist3l uint32
	ist3h uint32
	ist4l uint32
	ist4h uint32
	ist5l uint32
	ist5h uint32
	ist6l uint32
	ist6h uint32
	ist7l uint32
	ist7h uint32

	_res2 [2]uint32

	_res3 uint16
	iobmap uint16
	_align uint64
}

const (
	P 	uint32 = (1 << 15)
	PS	uint32 = (P | (1 << 12))
	G 	uint32 = (0 << 23)
	D 	uint32 = (1 << 22)
	L 	uint32 = (1 << 21)
	CODE	uint32 = (0x0a << 8)
	DATA	uint32 = (0x02 << 8)
	TSS	uint32 = (0x09 << 8)
	USER	uint32 = (0x60 << 8)
	INT	uint16 = (0x0e << 8)

	KCODE64		= 1
)

var _segs = [7 + 2*MAXCPUS]seg64_t{
	// 0: null segment
	{0, 0, 0},
	// 1: 64 bit kernel code
	{0, 0, PS | CODE | G | L},
	// 2: 64 bit kernel data
	{0, 0, PS | DATA | G | D},
	// 3: FS segment
	{0, 0, PS | DATA | G | D},
	// 4: GS segment. the sysexit instruction also requires that the
	// difference in indicies for the user code segment descriptor and the
	// kernel code segment descriptor is 4.
	{0, 0, PS | DATA | G | D},
	// 5: 64 bit user code
	{0, 0, PS | CODE | USER | G | L},
	// 6: 64 bit user data
	{0, 0, PS | DATA | USER | G | D},
	// 7: 64 bit TSS segment (occupies two segment descriptor entries)
	{0, 0, P | TSS | G},
	{0, 0, 0},
}

var _tss [MAXCPUS]tss_t

//go:nosplit
func tss_set(id uint, rsp, nmi uintptr) *tss_t {
	sz := unsafe.Sizeof(_tss[id])
	if sz != 104 + 8 {
		panic("bad tss_t")
	}
	p := &_tss[id]
	p.rsp0l = uint32(rsp)
	p.rsp0h = uint32(rsp >> 32)

	p.ist1l = uint32(rsp)
	p.ist1h = uint32(rsp >> 32)

	p.ist2l = uint32(nmi)
	p.ist2h = uint32(nmi >> 32)

	p.iobmap = uint16(sz)

	up := unsafe.Pointer(p)
	chkalign(up, 16)
	return p
}

// maps cpu number to the per-cpu TSS segment descriptor in the GDT
//go:nosplit
func segnum(cpunum uint) uint {
	return 7 + 2*cpunum
}

//go:nosplit
func tss_seginit(cpunum uint, _tssaddr *tss_t, lim uintptr) {
	seg := &_segs[segnum(cpunum)]
	seg.rest = P | TSS | G

	seg.lim = uint16(lim)
	seg.rest |= uint32((lim >> 16) & 0xf) << 16

	base := uintptr(unsafe.Pointer(_tssaddr))
	seg.baselo = uint16(base)
	seg.rest |= uint32(uint8(base >> 16))
	seg.rest |= uint32(uint8(base >> 24) << 24)

	seg = &_segs[segnum(cpunum) + 1]
	seg.lim = uint16(base >> 32)
	seg.baselo = uint16(base >> 48)
}

//go:nosplit
func tss_init(cpunum uint) uintptr {
	intstk := 0xa100001000 + uintptr(cpunum)*4*PGSIZE
	nmistk := 0xa100003000 + uintptr(cpunum)*4*PGSIZE
	// BSP maps AP's stack for them
	if cpunum == 0 {
		alloc_map(intstk - 1, PTE_W, true)
		alloc_map(nmistk - 1, PTE_W, true)
	}
	rsp := intstk
	rspnmi := nmistk
	tss := tss_set(cpunum, rsp, rspnmi)
	tss_seginit(cpunum, tss, unsafe.Sizeof(tss_t{}) - 1)
	segselect := segnum(cpunum) << 3
	ltr(segselect)
	cpus[cpunum].rsp = rsp
	return rsp
}

//go:nosplit
func pdsetup(pd *pdesc_t, _addr unsafe.Pointer, lim uintptr) {
	chkalign(_addr, 8)
	addr := uintptr(_addr)
	pd.limit = uint16(lim)
	pd.addrlow = uint16(addr)
	pd.addrmid = uint32(addr >> 16)
	pd.addrhi = uint16(addr >> 48)
}

//go:nosplit
func hexdump(_p unsafe.Pointer, sz uintptr) {
	for i := uintptr(0); i < sz; i++ {
		p := (*uint8)(unsafe.Pointer(uintptr(_p) + i))
		_pnum(uintptr(*p))
	}
}

// must be nosplit since stack splitting prologue uses FS which this function
// initializes.
//go:nosplit
func seg_setup() {
	p := pdesc_t{}
	chksize(unsafe.Sizeof(p), 16)
	chksize(unsafe.Sizeof(seg64_t{}), 8)
	pdsetup(&p, unsafe.Pointer(&_segs[0]), unsafe.Sizeof(_segs) - 1)
	lgdt(p)

	// now that we have a GDT, setup tls for the first thread.
	// elf tls specification defines user tls at -16(%fs). go1.5 uses
	// -8(%fs) though.
	t := uintptr(unsafe.Pointer(&m0.tls[0]))
	tlsaddr := int(t + 8)
	// we must set fs/gs at least once before we use the MSRs to change
	// their base address. the MSRs write directly to hidden segment
	// descriptor cache, and if we don't explicitly fill the segment
	// descriptor cache, the writes to the MSRs are thrown out (presumably
	// because the caches are thought to be invalid).
	fs_null()
	gs_null()
	ia32_gs_base := 0xc0000101
	Wrmsr(ia32_gs_base, tlsaddr)
}

// interrupt entries, defined in runtime/asm_amd64.s
func Xdz()
func Xrz()
func Xnmi()
func Xbp()
func Xov()
func Xbnd()
func Xuo()
func Xnm()
func Xdf()
func Xrz2()
func Xtss()
func Xsnp()
func Xssf()
func Xgp()
func Xpf()
func Xrz3()
func Xmf()
func Xac()
func Xmc()
func Xfp()
func Xve()
func Xtimer()
func Xspur()
func Xtlbshoot()
func Xperfmask()
func Xirq1()
func Xirq2()
func Xirq3()
func Xirq4()
func Xirq5()
func Xirq6()
func Xirq7()
func Xirq8()
func Xirq9()
func Xirq10()
func Xirq11()
func Xirq12()
func Xirq13()
func Xirq14()
func Xirq15()
func Xirq16()
func Xirq17()
func Xirq18()
func Xirq19()
func Xirq20()
func Xirq21()
func Xirq22()
func Xirq23()
func Xmsi0()
func Xmsi1()
func Xmsi2()
func Xmsi3()
func Xmsi4()
func Xmsi5()
func Xmsi6()
func Xmsi7()

type idte_t struct {
	baselow	uint16
	segsel	uint16
	details	uint16
	basemid	uint16
	basehi	uint32
	_res	uint32
}

const idtsz uintptr = 128
var _idt [idtsz]idte_t

//go:nosplit
func int_set(idx int, intentry func(), istn int) {
	entry := funcPC(intentry)

	p := &_idt[idx]
	p.baselow = uint16(entry)
	p.basemid = uint16(entry >> 16)
	p.basehi = uint32(entry >> 32)

	p.segsel = uint16(KCODE64 << 3)

	p.details = uint16(P) | INT | uint16(istn & 0x7)
}

//go:nosplit
func int_setup() {
	chksize(unsafe.Sizeof(idte_t{}), 16)
	chksize(unsafe.Sizeof(_idt), idtsz*16)
	chkalign(unsafe.Pointer(&_idt[0]), 8)

	// cpu exceptions
	int_set(0,   Xdz,  0)
	int_set(1,   Xrz,  0)
	int_set(2,   Xnmi, 2)
	int_set(3,   Xbp,  0)
	int_set(4,   Xov,  0)
	int_set(5,   Xbnd, 0)
	int_set(6,   Xuo,  0)
	int_set(7,   Xnm,  0)
	int_set(8,   Xdf,  1)
	int_set(9,   Xrz2, 0)
	int_set(10,  Xtss, 0)
	int_set(11,  Xsnp, 0)
	int_set(12,  Xssf, 0)
	int_set(13,  Xgp,  1)
	int_set(14,  Xpf,  1)
	int_set(15,  Xrz3, 0)
	int_set(16,  Xmf,  0)
	int_set(17,  Xac,  0)
	int_set(18,  Xmc,  0)
	int_set(19,  Xfp,  0)
	int_set(20,  Xve,  0)

	// IRQs
	int_set(32,  Xtimer,  1)
	int_set(33,  Xirq1,   1)
	int_set(34,  Xirq2,   1)
	int_set(35,  Xirq3,   1)
	int_set(36,  Xirq4,   1)
	int_set(37,  Xirq5,   1)
	int_set(38,  Xirq6,   1)
	int_set(39,  Xirq7,   1)
	int_set(40,  Xirq8,   1)
	int_set(41,  Xirq9,   1)
	int_set(42,  Xirq10,  1)
	int_set(43,  Xirq11,  1)
	int_set(44,  Xirq12,  1)
	int_set(45,  Xirq13,  1)
	int_set(46,  Xirq14,  1)
	int_set(47,  Xirq15,  1)
	int_set(48,  Xirq16,  1)
	int_set(49,  Xirq17,  1)
	int_set(50,  Xirq18,  1)
	int_set(51,  Xirq19,  1)
	int_set(52,  Xirq20,  1)
	int_set(53,  Xirq21,  1)
	int_set(54,  Xirq22,  1)
	int_set(55,  Xirq23,  1)

	// MSI interrupts
	int_set(56,  Xmsi0,  1)
	int_set(57,  Xmsi1,  1)
	int_set(58,  Xmsi2,  1)
	int_set(59,  Xmsi3,  1)
	int_set(60,  Xmsi4,  1)
	int_set(61,  Xmsi5,  1)
	int_set(62,  Xmsi6,  1)
	int_set(63,  Xmsi7,  1)

	int_set(64,  Xspur,    1)

	int_set(70,  Xtlbshoot, 1)
	int_set(72,  Xperfmask, 1)

	p := pdesc_t{}
	pdsetup(&p, unsafe.Pointer(&_idt[0]), unsafe.Sizeof(_idt) - 1)
	lidt(p)
}

const (
	PTE_P		uintptr = 1 << 0
	PTE_W		uintptr = 1 << 1
	PTE_U		uintptr = 1 << 2
	PTE_PS		uintptr = 1 << 7
	PTE_G		uintptr = 1 << 8
	PTE_PCD		uintptr = 1 << 4
	PGSIZE		uintptr = 1 << 12
	PGOFFMASK	uintptr = PGSIZE - 1
	PGMASK		uintptr = ^PGOFFMASK

	// special pml4 slots, agreed upon with the bootloader (which creates
	// our pmap).
	// highest runtime heap mapping
	VUEND		uintptr = 0x42
	// recursive mapping
	VREC		uintptr = 0x42
	// available mapping
	VTEMP		uintptr = 0x43
)

// physical address of kernel's pmap, given to us by bootloader
var P_kpmap uintptr

//go:nosplit
func pml4x(va uintptr) uintptr {
	return (va >> 39) & 0x1ff
}

//go:nosplit
func pte_addr(x uintptr) uintptr {
	return x &^ ((1 << 12) - 1)
}

//go:nosplit
func slotnext(va uintptr) uintptr {
	return ((va << 9) & ((1 << 48) - 1))
}

//go:nosplit
func pgroundup(va uintptr) uintptr {
	return (va + PGSIZE - 1) & PGMASK
}

//go:nosplit
func pgrounddown(va uintptr) uintptr {
	return va & PGMASK
}

//go:nosplit
func caddr(l4 uintptr, ppd uintptr, pd uintptr, pt uintptr,
    off uintptr) uintptr {
	ret := l4 << 39 | ppd << 30 | pd << 21 | pt << 12
	ret += off*8
	return uintptr(ret)
}

// XXX get rid of create
//go:nosplit
func pgdir_walk(_va uintptr, create bool) *uintptr {
	v := pgrounddown(_va)
	if v == 0 && create {
		pancake("map zero pg", _va)
	}
	slot0 := pml4x(v)
	if slot0 == VREC {
		pancake("map in VREC", _va)
	}
	pml4 := caddr(VREC, VREC, VREC, VREC, slot0)
	return pgdir_walk1(pml4, slotnext(v), create)
}

//go:nosplit
func pgdir_walk1(slot, van uintptr, create bool) *uintptr {
	ns := slotnext(slot)
	ns += pml4x(van)*8
	if pml4x(ns) != VREC {
		return (*uintptr)(unsafe.Pointer(slot))
	}
	sp := (*uintptr)(unsafe.Pointer(slot))
	if *sp & PTE_P == 0 {
		if !create{
			return nil
		}
		p_pg := get_pg()
		zero_phys(p_pg)
		*sp = p_pg | PTE_P | PTE_W
	}
	if *sp & PTE_PS != 0 {
		pancake("map in PS", *sp)
	}
	return pgdir_walk1(ns, slotnext(van), create)
}

//go:nosplit
func zero_phys(_phys uintptr) {
	rec := caddr(VREC, VREC, VREC, VREC, VTEMP)
	pml4 := (*uintptr)(unsafe.Pointer(rec))
	if *pml4 & PTE_P != 0 {
		pancake("vtemp in use", *pml4)
	}
	phys := pgrounddown(_phys)
	*pml4 = phys | PTE_P | PTE_W
	_tva := caddr(VREC, VREC, VREC, VTEMP, 0)
	tva := unsafe.Pointer(_tva)
	memclrNoHeapPointers(tva, PGSIZE)
	*pml4 = 0
	invlpg(_tva)
}

// this physical allocation code is temporary. biscuit probably shouldn't
// bother resizing its heap, ever. instead of providing a fake mmap to the
// runtime, the runtime should simply mmap its entire heap during
// initialization according to the amount of available memory.
//
// XXX when you write the new code, check and see if we can use ACPI to find
// available memory instead of e820. since e820 is only usable in realmode, we
// have to have e820 code in the bootloader. it would be better to have such
// memory management code in the kernel and not the bootloader.

type e820_t struct {
	start	uintptr
	len	uintptr
}

// "secret structure". created by bootloader for passing info to the kernel.
type secret_t struct {
	e820p	uintptr
	pmap	uintptr
	freepg	uintptr
}

// regions of memory not included in the e820 map, into which we cannot
// allocate
type badregion_t struct {
	start	uintptr
	end	uintptr
}

var badregs = []badregion_t{
	// VGA
	{0xa0000, 0x100000},
	// secret storage
	{0x7000, 0x8000},
}

//go:nosplit
func skip_bad(cur uintptr) uintptr {
	for _, br := range badregs {
		if cur >= br.start && cur < br.end {
			return br.end
		}
	}
	return cur
}

var pgfirst uintptr
var pglast uintptr

//go:nosplit
func phys_init() {
	origfirst := pgfirst
	sec := (*secret_t)(unsafe.Pointer(uintptr(0x7c00)))
	found := false
	base := sec.e820p
	maxfound := uintptr(0)
	// bootloader provides e820 entries
	e820sz := uintptr(28)
	for i := uintptr(0); i < 4096/e820sz; i++ {
		ep := (*e820_t)(unsafe.Pointer(base + i*28))
		if ep.len == 0 {
			continue
		}
		// use largest segment
		if ep.len > maxfound {
			maxfound = ep.len
			_eseg = *ep
			found = true
			// initialize pgfirst/pglast. if the segment contains
			// origfirst, then the bootloader already allocated the
			// the pages from [ep.start, origfirst). thus set
			// pgfirst to origfirst.
			endpg := ep.start + ep.len
			pglast = endpg
			if origfirst >= ep.start && origfirst < endpg {
				pgfirst = origfirst
			} else {
				pgfirst = ep.start
			}
		}
	}
	if !found {
		pancake("e820 problems", pgfirst)
	}
	if pgfirst & PGOFFMASK != 0 {
		pancake("pgfist not aligned", pgfirst)
	}
}

var _eseg e820_t

func Totalphysmem() int {
	return int(pglast - pgfirst)
}

func Get_phys() uintptr {
	return get_pg()
}

func Tcount() (int, int) {
	fl := Pushcli()
	Splock(threadlock)

	r := 0
	nr := 0
	for i := range threads {
		switch (threads[i].status) {
		case ST_RUNNING, ST_RUNNABLE:
			r++
		case ST_INVALID:
		default:
			nr++;
		}
	}

	Spunlock(threadlock)
	Popcli(fl)
	return r, nr
}

//go:nosplit
//go:nowritebarrierrec
func get_pg() uintptr {
	if pglast == 0 {
		phys_init()
	}
	pgfirst = skip_bad(pgfirst)
	if pgfirst >= pglast {
		pancake("oom", pglast)
	}
	ret := pgfirst
	pgfirst += PGSIZE
	return ret
}

func FuncPC(f interface{}) uintptr {
	return funcPC(f)
}

//go:nosplit
//go:nowritebarrierrec
func alloc_map(va uintptr, perms uintptr, fempty bool) {
	pte := pgdir_walk(va, true)
	old := *pte
	if old & PTE_P != 0 && fempty {
		pancake("expected empty pte", old)
	}
	p_pg := get_pg()
	zero_phys(p_pg)
	// XXX goodbye, memory
	*pte = p_pg | perms | PTE_P | PTE_G
	if old & PTE_P != 0 {
		invlpg(va)
	}
}

var Fxinit [FXREGS]uintptr

// nosplit because APs call this function before FS is setup
//go:nosplit
//go:nowritebarrierrec
func fpuinit(amfirst bool) {
	finit()
	cr0 := Rcr0()
	// clear EM
	cr0 &^= (1 << 2)
	// set MP
	cr0 |= 1 << 1
	Lcr0(cr0);

	cr4 := Rcr4()
	// set OSFXSR
	cr4 |= 1 << 9
	Lcr4(cr4);

	if amfirst {
		chkalign(unsafe.Pointer(&Fxinit[0]), 16)
		fxsave(&Fxinit)

		chksize(FXREGS*8, unsafe.Sizeof(threads[0].fx))
		for i := range threads {
			chkalign(unsafe.Pointer(&threads[i].fx), 16)
		}
		for i := range nmiprof.percpu {
			chkalign(unsafe.Pointer(&nmiprof.percpu[i].tfx), 16)
		}
	}
}

//go:nosplit
func fpuinit0() {
	fpuinit(true)
}

// LAPIC registers
const (
	LAPID		= 0x20/4
	LAPEOI		= 0xb0/4
	LAPVER		= 0x30/4
	LAPDCNT		= 0x3e0/4
	LAPICNT		= 0x380/4
	LAPCCNT		= 0x390/4
	LVSPUR		= 0xf0/4
	LVTIMER		= 0x320/4
	LVCMCI		= 0x2f0/4
	LVINT0		= 0x350/4
	LVINT1		= 0x360/4
	LVERROR		= 0x370/4
	LVPERF		= 0x340/4
	LVTHERMAL	= 0x330/4
)

var _lapaddr uintptr

//go:nosplit
func rlap(reg uint) uint32 {
	if _lapaddr == 0 {
		pancake("lapic not init", 0)
	}
	lpg := (*[PGSIZE/4]uint32)(unsafe.Pointer(_lapaddr))
	return atomic.Load(&lpg[reg])
}

//go:nosplit
func wlap(reg uint, val uint32) {
	if _lapaddr == 0 {
		pancake("lapic not init", 0)
	}
	lpg := (*[PGSIZE/4]uint32)(unsafe.Pointer(_lapaddr))
	lpg[reg] = val
}

//go:nosplit
func lap_id() uint32 {
	if rflags() & TF_FL_IF != 0 {
		pancake("interrupts must be cleared", 0)
	}
	if _lapaddr == 0 {
		pancake("lapic not init", 0)
	}
	lpg := (*[PGSIZE/4]uint32)(unsafe.Pointer(_lapaddr))
	return lpg[LAPID] >> 24
}

//go:nosplit
func lap_eoi() {
	if _lapaddr == 0 {
		pancake("lapic not init", 0)
	}
	wlap(LAPEOI, 0)
}

// PIT registers
const (
	CNT0	uint16 = 0x40
	CNTCTL	uint16 = 0x43
	_pitfreq	= 1193182
	_pithz		= 100
	PITDIV		= _pitfreq/_pithz
)

//go:nosplit
func pit_ticks() uint {
	// counter latch command for counter 0
	cmd := uint8(0)
	Outb(CNTCTL, cmd)
	low := Inb(CNT0)
	hi := Inb(CNT0)
	return hi << 8 | low
}

//go:nosplit
func pit_enable() {
	// rate generator mode, lsb then msb (if square wave mode is used, the
	// PIT uses div/2 for the countdown since div is taken to be the period
	// of the wave)
	Outb(CNTCTL, 0x34)
	Outb(CNT0, uint8(PITDIV & 0xff))
	Outb(CNT0, uint8(PITDIV >> 8))
}

func pit_disable() {
	// disable PIT: one-shot, lsb then msb
	Outb(CNTCTL, 0x32);
	Outb(CNT0, uint8(PITDIV & 0xff))
	Outb(CNT0, uint8(PITDIV >> 8))
}

// wait until 8254 resets the counter
//go:nosplit
//go:nowritebarrierrec
func pit_phasewait() {
	// 8254 timers are 16 bits, thus always smaller than last;
	last := uint(1 << 16)
	for {
		cur := pit_ticks()
		if cur > last {
			return
		}
		last = cur
	}
}

var _lapic_quantum uint32

//go:nosplit
//go:nowritebarrierrec
func lapic_setup(calibrate bool) {
	la := uintptr(0xfee00000)

	if calibrate {
		// map lapic IO mem
		pte := pgdir_walk(la, false)
		if pte != nil && *pte & PTE_P != 0 {
			pancake("lapic mem already mapped", 0)
		}
	}

	pte := pgdir_walk(la, true)
	*pte = la | PTE_W | PTE_P | PTE_PCD
	_lapaddr = la

	lver := rlap(LAPVER)
	if (lver & 0xff) < 0x10 {
		pancake("82489dx not supported", uintptr(lver))
	}

	// enable lapic, set spurious int vector
	apicenable := 1 << 8
	wlap(LVSPUR, uint32(apicenable | TRAP_SPUR))

	// timer: periodic, int 32
	periodic := 1 << 17
	wlap(LVTIMER, uint32(periodic | TRAP_TIMER))
	// divide by 1
	divone := uint32(0xb)
	wlap(LAPDCNT, divone)

	if calibrate {
		// figure out how many lapic ticks there are in a second; first
		// setup 8254 PIT since it has a known clock frequency. openbsd
		// uses a similar technique.
		pit_enable()

		// start lapic counting
		wlap(LAPICNT, 0x80000000)
		pit_phasewait()
		lapstart := rlap(LAPCCNT)
		cycstart := Rdtsc()

		frac := 10
		for i := 0; i < _pithz/frac; i++ {
			pit_phasewait()
		}

		lapend := rlap(LAPCCNT)
		if lapend > lapstart {
			pancake("lapic timer wrapped?", uintptr(lapend))
		}
		lapelapsed := (lapstart - lapend)*uint32(frac)
		cycelapsed := (Rdtsc() - cycstart)*uint64(frac)
		pmsg("LAPIC Mhz:")
		pnum(uintptr(lapelapsed/(1000 * 1000)))
		pmsg("\n")
		_lapic_quantum = lapelapsed / HZ

		pmsg("CPU Mhz:")
		Cpumhz = uint(cycelapsed/(1000 * 1000))
		pnum(uintptr(Cpumhz))
		pmsg("\n")
		Pspercycle = uint(1000000000000/cycelapsed)

		pit_disable()
	}

	// initial count; the LAPIC's frequency is not the same as the CPU's
	// frequency
	wlap(LAPICNT, _lapic_quantum)

	maskint := uint32(1 << 16)
	// mask cmci, error, perf counters, and thermal sensor
	wlap(LVCMCI,    maskint)
	// unmask LINT0 and LINT1
	wlap(LVINT0,    rlap(LVINT0) &^ maskint)
	wlap(LVINT1,    rlap(LVINT1) &^ maskint)
	wlap(LVERROR,   maskint)
	wlap(LVPERF,    maskint)
	wlap(LVTHERMAL, maskint)

	ia32_apic_base := 0x1b
	reg := Rdmsr(ia32_apic_base)
	if reg & (1 << 11) == 0 {
		pancake("lapic disabled?", reg)
	}
	//if reg & (1 << 10) == 0 {
	//	pmsg("x2APIC MODE ENABLED\n")
	//}
	if (reg >> 12) != 0xfee00 {
		pancake("weird base addr?", reg >> 12)
	}

	lreg := rlap(LVSPUR)
	if lreg & (1 << 12) != 0 {
		pmsg("EOI broadcast surpression\n")
	}
	if lreg & (1 << 9) != 0 {
		pmsg("focus processor checking\n")
	}
	if lreg & (1 << 8) == 0 {
		pmsg("apic disabled\n")
	}
}

//go:nowritebarrierrec
func proc_setup() {
	_userintaddr = funcPC(_userint)
	//_sigsimaddr = funcPC(sigsim)

	chksize(TFSIZE*8, unsafe.Sizeof(threads[0].tf))
	// initialize the first thread: us
	threads[0].status = ST_RUNNING
	threads[0].p_pmap = P_kpmap

	// initialize GS pointers
	for i := range cpus {
		cpus[i]._init(&cpus[i])
	}

	lapic_setup(true)

	// 8259a - mask all irqs. see 2.5.3.6 in piix3 documentation.
	// otherwise an RTC timer interrupt (that turns into a double-fault
	// since the PIC has not been programmed yet) comes in immediately
	// after sti.
	Outb(0x20 + 1, 0xff)
	Outb(0xa0 + 1, 0xff)

	myrsp := tss_init(0)
	sysc_setup(myrsp)
	gs_set(&cpus[0])
	cpus[0].apicid = lap_id()
	Gscpu().num = 0
	Gscpu().setthread(&threads[0])
}

// XXX to prevent CPUs from calling zero_phys concurrently when allocating pmap
// pages...
var joinlock = &Spinlock_t{}

//go:nosplit
//go:nowritebarrierrec
func Ap_setup(cpunum uint) {
	// interrupts are probably already cleared
	fl := Pushcli()
	Splock(joinlock)

	Splock(pmsglock)
	_pmsg("cpu")
	_pnum(uintptr(cpunum))
	_pmsg("joined\n")
	Spunlock(pmsglock)

	if cpunum >= uint(MAXCPUS) {
		pancake("nice computer!", uintptr(cpunum))
	}
	fpuinit(false)
	lapic_setup(false)
	myrsp := tss_init(cpunum)
	sysc_setup(myrsp)
	mycpu := &cpus[cpunum]
	if mycpu.num != 0 {
		pancake("cpu id conflict", uintptr(mycpu.num))
	}
	me := lap_id()
	// sanity
	for i := range cpus {
		if cpus[i].apicid == me {
			pancake("dup apic id?", uintptr(me))
		}
	}
	mycpu.apicid = me
	fs_null()
	gs_null()
	gs_set(mycpu)
	Gscpu().num = cpunum
	// avoid write barrier before FS is set to TLS (via the "OS"
	// scheduler).
	Gscpu().setthread(nil)

	Spunlock(joinlock)
	Popcli(fl)
}

//go:nosplit
func sysc_setup(myrsp uintptr) {
	// lowest 2 bits are ignored for sysenter, but used for sysexit
	kcode64 := 1 << 3 | 3
	sysenter_cs := 0x174
	Wrmsr(sysenter_cs, kcode64)

	sysenter_eip := 0x176
	// asm_amd64.s
	sysentryaddr := funcPC(_sysentry)
	Wrmsr(sysenter_eip, int(sysentryaddr))

	sysenter_esp := 0x175
	Wrmsr(sysenter_esp, 0)
}

//go:nowritebarrierrec
func Condflush(_refp *int32, p_pmap, va uintptr, pgcount int) bool {
	var refp *uint32
	var refc uint32
	fl := Pushcli()
	cr3 := Rcr3()
	ret := true
	if cr3 != p_pmap {
		ret = false
		goto out
	}
	// runtime internal atomic doesn't have a load for int32...
	refp = (*uint32)(unsafe.Pointer(_refp))
	refc = atomic.Load(refp)
	if refc > 2 {
		ret = false
		goto out
	}
	if pgcount == 1 {
		invlpg(va)
	} else {
		Lcr3(cr3)
	}
out:
	Popcli(fl)
	return ret
}

func TLBflush() {
	// maintaining coherency between cpu.shadowcr3 and cr3 requires that
	// interrupts are cleared before reading cr3 until after loading that
	// value into c3.
	fl := Pushcli()
	Lcr3(Rcr3())
	Popcli(fl)
}

type Tlbstate_t struct {
	P_pmap	uintptr
	Cpum	uint64
}

var Tlbshoots struct {
	States	[MAXCPUS]Tlbstate_t
}

// must be nosplit since called at interrupt time
//go:nosplit
//go:nowritebarrierrec
func tlb_shootdown() {
	cpu := Gscpu()
	ct := cpu.mythread

	flushed := false
	mine := Rcr3()
	memask := uint64(1 << cpu.num)
	for i := range Tlbshoots.States {
		st := &Tlbshoots.States[i]
		pm := atomic.Loaduintptr(&st.P_pmap)
		if pm != 0 {
			Or64(&st.Cpum, memask)
			if pm == mine && !flushed {
				flushed = true
				Lcr3(Rcr3())
			}
		}
	}
	sched_resume(ct)
}

// cpu exception/interrupt vectors
const (
	TRAP_NMI	= 2
	TRAP_PGFAULT	= 14
	TRAP_SYSCALL	= 64
	TRAP_TIMER	= 32
	TRAP_DISK	= (32 + 14)
	TRAP_SPUR	= 64
	TRAP_TLBSHOOT	= 70
	TRAP_PERFMASK	= 72
	TRAP_YIELD	= 73
)

var threadlock = &Spinlock_t{}

// maximum # of runtime "OS" threads
const maxthreads = 64
var threads [maxthreads]thread_t

// thread states
const (
	ST_INVALID	= 0
	ST_RUNNABLE	= 1
	ST_RUNNING	= 2
	ST_SLEEPING	= 4
	ST_WILLSLEEP	= 5
)

// scheduler constants
const (
	HZ	= 1000
)

var _userintaddr uintptr
var _sigsimaddr uintptr
var _newtrap func(*[TFSIZE]uintptr)

// XXX remove newtrap?
func Install_traphandler(newtrap func(*[TFSIZE]uintptr)) {
	_newtrap = newtrap
}

//go:nosplit
//go:nowritebarrierrec
func stack_dump(rsp uintptr) {
	pte := pgdir_walk(rsp, false)
	_pmsg("STACK DUMP\n")
	if pte != nil && *pte & PTE_P != 0 {
		pc := 0
		p := rsp
		for i := 0; i < 70; i++ {
			pte = pgdir_walk(p, false)
			if pte != nil && *pte & PTE_P != 0 {
				n := *(*uintptr)(unsafe.Pointer(p))
				p += 8
				_pnum(n)
				if (pc % 4) == 0 {
					_pmsg("\n")
				}
				pc++
			}
		}
	} else {
		_pmsg("bad stack")
		_pnum(rsp)
	}
}

//go:nosplit
//go:nowritebarrierrec
func kernel_fault(tf *[TFSIZE]uintptr) {
	trapno := tf[TF_TRAPNO]
	_pmsg("trap frame at")
	_pnum(uintptr(unsafe.Pointer(tf)))
	_pmsg("trapno")
	_pnum(trapno)
	rip := tf[TF_RIP]
	_pmsg("rip")
	_pnum(rip)
	if trapno == TRAP_PGFAULT {
		cr2 := Rcr2()
		_pmsg("cr2")
		_pnum(cr2)
	}
	rsp := tf[TF_RSP]
	stack_dump(rsp)
	pancake("kernel fault", trapno)
}

// XXX
// may want to only ·wakeup() on most timer ints since now there is more
// overhead for timer ints during user time.
//go:nosplit
//go:nowritebarrierrec
func trap(tf *[TFSIZE]uintptr) {
	trapno := tf[TF_TRAPNO]

	if trapno == TRAP_NMI {
		if nmiprof.backtracing {
			// go's gentraceback() uses SSE instructions (to zero
			// large stack variables), so save and restore SSE regs
			// explicitly
			cpu := NMI_Gscpu()
			fxbuf := &nmiprof.percpu[cpu.num].tfx
			fxsave(fxbuf)
			nmibacktrace(tf)
			perfmask()
			fxrstor(fxbuf)
		} else {
			// prevent SSE corruption: set TS in cr0 to make sure
			// SSE instructions generate a fault
			ts := uintptr(1 << 3)
			Lcr0(Rcr0() | ts)
			perfgather(tf)
			perfmask()
			Lcr0(Rcr0() &^ ts)
		}
		_trapret(tf)
	}

	cpu := Gscpu()

	// CPU exceptions in kernel mode are fatal errors
	if trapno < TRAP_TIMER && (tf[TF_CS] & 3) == 0 {
		kernel_fault(tf)
	}

	ct := cpu.mythread

	if rflags() & TF_FL_IF != 0 {
		pancake("ints enabled in trap", 0)
	}

	if Halt != 0 {
		for {
		}
	}

	// don't add code before SSE context saving unless you've thought very
	// carefully! it is easy to accidentally and silently corrupt SSE state
	// (ie calling memmove indirectly by assignment or declaration of large
	// datatypes) before it is saved below.

	// save SSE state immediately before we clobber it
	if ct != nil {
		// if in user mode, save to user buffers and make it look like
		// Userrun returned. did the interrupt occur while in user
		// mode?
		if tf[TF_CS] & 3 != 0 {
			ufx := cpu.fxbuf
			fxsave(ufx)
			utf := cpu.tf
			*utf = *tf
			ct.tf[TF_RIP] = _userintaddr
			ct.tf[TF_RSP] = cpu.sysrsp
			ct.tf[TF_RAX] = trapno
			ct.tf[TF_RBX] = Rcr2()
			// XXXPANIC
			if trapno == TRAP_YIELD {
				pancake("nyet", trapno)
			}
		} else {
			fxsave(&ct.fx)
			ct.tf = *tf
		}
	}

	yielding := false
	// these interrupts are handled specially by the runtime
	if trapno == TRAP_YIELD {
		trapno = TRAP_TIMER
		tf[TF_TRAPNO] = TRAP_TIMER
		yielding = true
	}

	if trapno == TRAP_TLBSHOOT {
		lap_eoi()
		// does not return
		tlb_shootdown()
	} else if trapno == TRAP_TIMER {
		Splock(threadlock)
		if ct != nil {
			if ct.status == ST_WILLSLEEP {
				ct.status = ST_SLEEPING
				// XXX set IF, unlock
				ct.tf[TF_RFLAGS] |= TF_FL_IF
				Spunlock(futexlock)
			} else {
				ct.status = ST_RUNNABLE
			}
		}
		wakeup()
		if !yielding {
			lap_eoi()
			//if cpu.num == 0 {
			//	//wakeup()
			//	proftick()
			//}
		}
		// yieldy doesn't return
		yieldy()
	} else if is_cpuex(trapno) {
		// we vet out kernel mode CPU exceptions above must be from
		// user program. thus return from Userrun() to kernel.
		sched_run(ct)
	} else if trapno == TRAP_PERFMASK {
		lap_eoi()
		perfmask()
		sched_resume(ct)
	} else {
		if _newtrap != nil {
			// catch kernel faults that occur while trying to
			// handle user traps
			_newtrap(tf)
		} else {
			pancake("IRQ without ntrap", trapno)
		}
		lap_eoi()
		sched_resume(ct)
	}
	// not reached
	pancake("no returning", 0)
}

//go:nosplit
//go:nowritebarrierrec
func is_cpuex(trapno uintptr) bool {
	return trapno < 32
}

//go:nosplit
//go:nowritebarrierrec
func _tchk() {
	if rflags() & TF_FL_IF != 0 {
		pancake("must not be interruptible", 0)
	}
	if threadlock.v == 0 {
		pancake("must hold threadlock", 0)
	}
}

//go:nosplit
//go:nowritebarrierrec
func sched_halt() {
	if rflags() & TF_FL_IF != 0 {
		pancake("must not be interruptible", 0)
	}
	// busy loop waiting for runnable thread without the threadlock
	for {
		Sti()
		now := hack_nanotime()
		sidx := int(dumrand(0, uint(len(threads))))
		for n := 0; n < len(threads); n++ {
			i := (sidx + n) % len(threads)
			t := &threads[i]
			if t.status == ST_RUNNABLE {
				Cli()
				Splock(threadlock)
				_yieldy()
				Sti()
			} else if _waketimeout(now, t) {
				Cli()
				Splock(threadlock)
				wakeup()
				_yieldy()
				Sti()
			}
		}
	}
}

//go:nowritebarrierrec
//go:nosplit
func sched_run(t *thread_t) {
	if t.tf[TF_RFLAGS] & TF_FL_IF == 0 {
		pancake("thread not interurptible", 0)
	}
	// mythread never references a heap allocated object. avoid
	// writebarrier since sched_run can be executed at any time, even when
	// GC invariants do not hold (like when g.m.p == nil).
	Gscpu().setthread(t)
	fxrstor(&t.fx)
	// flush the TLB, otherwise the cpu may use a TLB entry for a page that
	// has since been unmapped
	Lcr3(Rcr3())
	_trapret(&t.tf)
}

//go:nowritebarrierrec
//go:nosplit
func sched_resume(ct *thread_t) {
	if ct != nil {
		sched_run(ct)
	} else {
		sched_halt()
	}
}

//go:nowritebarrierrec
//go:nosplit
func wakeup() {
	_tchk()
	now := hack_nanotime()
	timedout := -110
	for i := range threads {
		t := &threads[i]
		if _waketimeout(now, t) {
			t.status = ST_RUNNABLE
			t.sleepfor = 0
			t.futaddr = 0
			t.sleepret = timedout
		}
	}
}

//go:nowritebarrierrec
//go:nosplit
func _waketimeout(now int, t *thread_t) bool {
	sf := t.sleepfor
	return t.status == ST_SLEEPING && sf != -1 && sf < now
}

//go:nowritebarrierrec
//go:nosplit
func yieldy() {
	_yieldy()
	sched_halt()
}

//go:nowritebarrierrec
//go:nosplit
func _yieldy() {
	_tchk()
	cpu := Gscpu()
	ct := cpu.mythread
	var start int
	if ct != nil {
		_ti := (uintptr(unsafe.Pointer(ct)) -
		    uintptr(unsafe.Pointer(&threads[0])))/
		    unsafe.Sizeof(thread_t{})
		ti := int(_ti)
		start = (ti + 1) % maxthreads
	} else {
		start = int(dumrand(0, uint(len(threads))))
	}
	for i := 0; i < maxthreads; i++ {
		idx := (start + i) % maxthreads
		t := &threads[idx]
		if t.status == ST_RUNNABLE {
			t.status = ST_RUNNING
			Spunlock(threadlock)
			sched_run(t)
		}
	}
	*(*uintptr)(unsafe.Pointer(&cpu.mythread)) =
	    uintptr(unsafe.Pointer(nil))
	Spunlock(threadlock)
}

var _irqv struct {
	// slock protects everything in _irqv
	slock		Spinlock_t
	handlers	[64]struct {
		igp	*g
		started	bool
	}
	// flag indicating whether a thread in schedule() should check for
	// runnable IRQ-handling goroutines
	check		bool
	// bitmask of IRQs that have requested service
	irqs		uintptr
}

// IRQsched yields iff there have been no new IRQs since the last time the
// calling goroutine was scheduled.
func IRQsched(irq uint) {
	gp := getg()
	gp.m.irqn = irq
	gp.waitreason = "waiting for trap"
	mcall(irqsched_m)
}

//go:nowritebarrierrec
func irqsched_m(gp *g) {
	// have new IRQs arrived?
	irq := gp.m.irqn
	gp.m.irqn = 0
	if irq > 63 {
		throw("bad irq " + string(irq))
	}

	// dropg() before taking spinlock since it can split the stack
	pp := gp.m.p.ptr()
	dropg()

	fl := Pushcli()
	Splock(&_irqv.slock)

	status := readgstatus(gp)
	if((status&^_Gscan) != _Grunning){
		pancake("bad g status", uintptr(status))
	}

	var nstatus uint32
	var start bool
	bit := uintptr(1 << irq)
	sleeping := _irqv.irqs & bit == 0
	if sleeping {
		nstatus = _Gwaiting
		if _irqv.handlers[irq].igp != nil {
			pancake("igp exists", uintptr(irq))
		}
		setGNoWB(&_irqv.handlers[irq].igp, gp)
		start = false
	} else {
		nstatus = _Grunnable
		start = true
		// clear flag in order to detect any IRQs that occur before the
		// next IRQsched.
		_irqv.irqs &^= bit
	}

	_irqv.handlers[irq].started = start

	// _Gscan shouldn't be set since gp's status is running
	casgstatus(gp, _Grunning, nstatus)
	Spunlock(&_irqv.slock)
	Popcli(fl)

	// call runqput() only after re-enabling interrupts since runqput() can
	// split the stack
	if !sleeping {
		// casgstatus must happen before runqput
		runqput(pp, gp, true)
	}

	schedule()
}

// called from the CPU interrupt handler. must only be called while interrupts
// are disabled
//go:nosplit
func IRQwake(irq uint) {
	if irq > 63 {
		pancake("bad irq", uintptr(irq))
	}
	Splock(&_irqv.slock)
	_irqv.irqs |= 1 << irq
	_irqv.check = true
	Spunlock(&_irqv.slock)
}

// puts IRQ-handling goroutines for outstanding IRQs on the runq. called by Ms
// from findrunnable() before checking local run queue
func IRQcheck(pp *p) {
	if !_irqv.check {
		return
	}

	fl := Pushcli()
	Splock(&_irqv.slock)

	var gps [64]*g
	var gidx int
	var newirqs uintptr
	irqs := _irqv.irqs

	if !_irqv.check {
		goto out
	}
	_irqv.check = false

	if irqs == 0 {
		goto out
	}

	// wakeup the goroutine for each received IRQ
	for i := 0; i < 64; i++ {
		ibit := uintptr(1 << uint(i))
		if irqs & ibit != 0 {
			gp := _irqv.handlers[i].igp
			// the IRQ-handling goroutine has not yet called
			// IRQsched; keep trying until it does.
			if gp == nil {
				newirqs |= ibit
				continue
			}
			gst := readgstatus(gp)
			if gst &^ _Gscan != _Gwaiting {
				pancake("bad igp status", uintptr(gst))
			}
			setGNoWB(&_irqv.handlers[i].igp, nil)
			_irqv.handlers[i].started = true
			// we cannot set gstatus or put to run queue before we
			// release the spinlock since either operation may
			// block
			gps[gidx] = gp
			gidx++
		}
	}
	_irqv.irqs = newirqs
	if newirqs != 0 {
		_irqv.check = true
	}
out:
	Spunlock(&_irqv.slock)
	Popcli(fl)

	for i := 0; i < gidx; i++ {
		gp := gps[i]
		casgstatus(gp, _Gwaiting, _Grunnable)
		runqput(pp, gp, true)
	}
}

// goprofiling is implemented by simulating the SIGPROF signal. when proftick
// observes that enough time has elapsed, mksig() is used to deliver SIGPROF to
// the runtime and things are setup so the runtime returns to sigsim().
// sigsim() makes sure that, once the "signal" has been delivered, the runtime
// thread restores its pre-signal context (signals must be taken on the
// alternate stack since the signal handler pushes a lot of state to the stack
// and may clobber a goroutine stack since goroutine stacks are small). for
// simpliclity, sigsim() uses a special trap # (TRAP_SIGRET) to restore the
// pre-signal context. it is easier using a (software) trap because a trap
// switches to the interrupt stack where we can easily change the runnability
// of the current thread.

var _lastprof int

// XXX remove signal emulation (old, used for go profiling) crap
//go:nosplit
func proftick() {
	// goprofile period = 10ms
	profns := 10000000
	n := hack_nanotime()

	if n - _lastprof < profns {
		return
	}
	_lastprof = n

	// XXX this is broken; only deliver sigprof to thread that was
	// executing when the timer expired.
	for i := range threads {
		// only do fake SIGPROF if we are already
		t := &threads[i]
		if t.prof.enabled == 0 || t.doingsig != 0 {
			continue
		}
		// don't touch running threads
		if t.status != ST_RUNNABLE {
			continue
		}
		SIGPROF := int32(27)
		mksig(t, SIGPROF)
	}
}

// these are defined by linux since we lie to the go build system that we are
// running on linux...
type ucontext_t struct {
	uc_flags	uintptr
	uc_link		uintptr
	uc_stack struct {
		sp	uintptr
		flags	int32
		size	uint64
	}
	uc_mcontext struct {
		r8	uintptr
		r9	uintptr
		r10	uintptr
		r11	uintptr
		r12	uintptr
		r13	uintptr
		r14	uintptr
		r15	uintptr
		rdi	uintptr
		rsi	uintptr
		rbp	uintptr
		rbx	uintptr
		rdx	uintptr
		rax	uintptr
		rcx	uintptr
		rsp	uintptr
		rip	uintptr
		eflags	uintptr
		cs	uint16
		gs	uint16
		fs	uint16
		__pad0	uint16
		err	uintptr
		trapno	uintptr
		oldmask	uintptr
		cr2	uintptr
		fpptr	uintptr
		res	[8]uintptr
	}
	uc_sigmask	uintptr
}

//go:nosplit
func mksig(t *thread_t, signo int32) {
	if t.sigstack == 0 {
		pancake("no sig stack", t.sigstack)
	}
	// save old context for sigret
	if t.tf[TF_RFLAGS] & TF_FL_IF == 0 {
		pancake("thread uninterruptible?", 0)
	}
	t.sigtf = t.tf
	t.sigfx = t.fx
	t.status = ST_RUNNABLE
	t.doingsig = 1

	rsp := t.sigstack + t.sigsize
	ucsz := unsafe.Sizeof(ucontext_t{})
	rsp -= ucsz
	_ctxt := rsp
	ctxt := (*ucontext_t)(unsafe.Pointer(_ctxt))

	// the profiler only uses rip and rsp of the context...
	memclrNoHeapPointers(unsafe.Pointer(_ctxt), ucsz)
	ctxt.uc_mcontext.rip = t.tf[TF_RIP]
	ctxt.uc_mcontext.rsp = t.tf[TF_RSP]

	// simulate call to sigsim with args
	rsp -= 8
	*(*uintptr)(unsafe.Pointer(rsp)) = _ctxt
	// nil siginfo_t
	rsp -= 8
	*(*uintptr)(unsafe.Pointer(rsp)) = 0
	rsp -= 8
	*(*uintptr)(unsafe.Pointer(rsp)) = uintptr(signo)
	// bad return addr shouldn't be reached
	rsp -= 8
	*(*uintptr)(unsafe.Pointer(rsp)) = 0

	t.tf[TF_RSP] = rsp
	t.tf[TF_RIP] = _sigsimaddr
}

// if sigsim() is used to deliver signals other than SIGPROF, you will need to
// construct siginfo_t and more of context.

func find_empty(sz uintptr) uintptr {
	v := caddr(0, 0, 256, 0, 0)
	cantuse := uintptr(0xf0)
	for {
		pte := pgdir_walk(v, false)
		if pte == nil || (*pte != cantuse && *pte & PTE_P == 0) {
			failed := false
			for i := uintptr(0); i < sz; i += PGSIZE {
				pte = pgdir_walk(v + i, false)
				if pte != nil &&
				    (*pte & PTE_P != 0 || *pte == cantuse) {
					failed = true
					v += i
					break
				}
			}
			if !failed {
				return v
			}
		}
		v += PGSIZE
	}
}

func prot_none(v, sz uintptr) {
	for i := uintptr(0); i < sz; i += PGSIZE {
		pte := pgdir_walk(v + i, true)
		if pte != nil {
			*pte = *pte & ^PTE_P
			invlpg(v + i)
		}
	}
}

var maplock = &Spinlock_t{}

// this flag makes hack_mmap panic if a new pml4 entry is ever added to the
// kernel's pmap. we want to make sure all kernel mappings added after bootup
// fall into the same pml4 entry so that all the kernel mappings can be easily
// shared in user process pmaps.
var _nopml4 bool

func Pml4freeze() {
	_nopml4 = true
}

// this function is dead-code; its purpose is to ensure that the compiler
// generates an error if the arguments/return values of the runtime functions
// do not match the hack hooks...
func test_func_consist() {
	if r1, r2 := hack_mmap(0, 0, 0, 0, 0, 0); r1 == 0 || r2 != 0 {
	}
	if r1, r2 := sysMmap(nil, 0, 0, 0, 0, 0); r1 == nil || r2 != 0 {
	}

	hack_munmap(0, 0)
	sysMunmap(nil, 0)

	hack_exit(0)
	exit(0)

	{
		if r := write(0, nil, 0); r == 0 {
		}
		if r := hack_write(0, 0, 0); r == 0 {
		}
		usleep(0)
		hack_usleep(0)
	}
	if r1 := nanotime(); r1 == 0 {
	}
	if r1 := hack_nanotime(); r1 == 0 {
	}

	if r1 := futex(nil, 0, 0, nil, nil, 0); r1 != 0 {
	}
	if r1 := hack_futex(nil, 0, 0, nil, nil, 0); r1 != 0 {
	}

	if r1 := clone(0, nil, nil, nil, nil); r1 != 0 {
	}
	if r1 := hack_clone(0, 0, nil, nil, 0); r1 != 0 {
	}

	sigaltstack(nil, nil)
	hack_sigaltstack(nil, nil)

	// importing syscall causes build to fail?
	//if a, b, c := syscall.Syscall(0, 0, 0, 0); a == b || c == 0 {
	//}
	if a, b, c := hack_syscall(0, 0, 0, 0); a == b || c == 0 {
	}
}

//var didsz uintptr

func hack_mmap(va, _sz uintptr, _prot uint32, _flags uint32,
    fd int32, offset int32) (uintptr, int) {
	fl := Pushcli()
	Splock(maplock)

	MAP_ANON := uintptr(0x20)
	MAP_PRIVATE := uintptr(0x2)
	PROT_NONE := uintptr(0x0)
	PROT_WRITE := uintptr(0x2)

	prot := uintptr(_prot)
	flags := uintptr(_flags)
	var vaend uintptr
	var perms uintptr
	var ret uintptr
	var err int
	var t uintptr
	pgleft := pglast - pgfirst
	sz := pgroundup(_sz)
	if sz > pgleft {
		ret = ^uintptr(0)
		err = -12 // ENOMEM
		goto out
	}
	sz = pgroundup(va + _sz)
	sz -= pgrounddown(va)
	if va == 0 {
		//_pmsg("ZERO\n")
		va = find_empty(sz)
	}
	vaend = caddr(VUEND, 0, 0, 0, 0)
	//_pmsg("--"); _pnum(didsz); _pmsg("--\n")
	//_pnum(va); _pmsg("\n")
	//_pnum(sz); _pmsg("\n")
	//_pnum(va + sz); _pmsg("\n")
	//_pnum(vaend); _pmsg("\n")
	if va >= vaend || va + sz >= vaend {
		pancake("va space exhausted", va)
	}

	t = MAP_ANON | MAP_PRIVATE
	if flags & t != t {
		pancake("unexpected flags", flags)
	}
	perms = PTE_P
	if prot == PROT_NONE {
		//_pmsg("PROT_NONE\n")
		prot_none(va, sz)
		ret = va
		goto out
	}

	if prot & PROT_WRITE != 0 {
		perms |= PTE_W
	}

	if _nopml4 {
		eidx := pml4x(va + sz - 1)
		for sidx := pml4x(va); sidx <= eidx; sidx++ {
			pml4 := caddr(VREC, VREC, VREC, VREC, sidx)
			pml4e := (*uintptr)(unsafe.Pointer(pml4))
			if *pml4e & PTE_P == 0 {
				pancake("new pml4 entry to kernel pmap", va)
			}
		}
	}

	for i := uintptr(0); i < sz; i += PGSIZE {
		alloc_map(va + i, perms, true)
	}
	ret = va
	//didsz += sz
out:
	Spunlock(maplock)
	Popcli(fl)
	return ret, err
}

func hack_munmap(v, _sz uintptr) {
	fl := Pushcli()
	Splock(maplock)
	sz := pgroundup(_sz)
	cantuse := uintptr(0xf0)
	for i := uintptr(0); i < sz; i += PGSIZE {
		va := v + i
		pte := pgdir_walk(va, false)
		if pml4x(va) >= VUEND {
			pancake("high unmap", va)
		}
		// XXX goodbye, memory
		if pte != nil && *pte & PTE_P != 0 {
			// make sure these pages aren't remapped via
			// hack_munmap
			*pte = cantuse
			invlpg(va)
		}
	}
	pmsg("POOF\n")
	Spunlock(maplock)
	Popcli(fl)
}

func thread_avail() int {
	_tchk()
	for i := range threads {
		if threads[i].status == ST_INVALID {
			return i
		}
	}
	pancake("no available threads", maxthreads)
	return -1
}

func clone_wrap(rip uintptr) {
	clone_call(rip)
	pancake("clone_wrap returned", 0)
}

var _cloneid int32

//go:nowritebarrierrec
func hack_clone(flags uint32, rsp uintptr, mp *m, gp *g, fn uintptr) int32 {
	// _CLONE_SYSVSEM is specified only for strict qemu-arm64 checks; the
	// runtime doesn't use sysv sems, fortunately
	chk := uint32(_CLONE_VM | _CLONE_FS | _CLONE_FILES | _CLONE_SIGHAND |
	    _CLONE_THREAD | _CLONE_SYSVSEM)
	if flags != chk {
		pancake("unexpected clone args", uintptr(flags))
	}
	cloneaddr := funcPC(clone_wrap)

	fl := Pushcli()
	Splock(threadlock)
	_cloneid++
	ret := _cloneid

	ti := thread_avail()
	// provide fn as arg to clone_wrap
	rsp -= 8
	*(*uintptr)(unsafe.Pointer(rsp)) = fn
	rsp -= 8
	// bad return address
	*(*uintptr)(unsafe.Pointer(rsp)) = 0

	mt := &threads[ti]
	memclrNoHeapPointers(unsafe.Pointer(mt), unsafe.Sizeof(thread_t{}))
	mt.tf[TF_CS] = KCODE64 << 3
	mt.tf[TF_RSP] = rsp
	mt.tf[TF_RIP] = cloneaddr
	mt.tf[TF_RFLAGS] = rflags() | TF_FL_IF
	mt.tf[TF_GSBASE] = uintptr(unsafe.Pointer(&mp.tls[0])) + 8

	// avoid write barrier for mp here since we have interrupts clear. Ms
	// are always reachable from allm anyway. see comments in runtime2.go
	//gp.m = mp
	setMNoWB(&gp.m, mp)
	mp.tls[0] = uintptr(unsafe.Pointer(gp))
	mp.procid = uint64(ti)
	mt.status = ST_RUNNABLE
	mt.p_pmap = P_kpmap

	mt.fx = Fxinit

	Spunlock(threadlock)
	Popcli(fl)

	return ret
}

// XXX remove goprof stuff
func hack_setitimer(timer uint32, new, old *itimerval) {
	TIMER_PROF := uint32(2)
	if timer != TIMER_PROF {
		pancake("weird timer", uintptr(timer))
	}

	fl := Pushcli()
	ct := Gscpu().mythread
	nsecs := new.it_interval.tv_sec * 1000000000 +
	    new.it_interval.tv_usec * 1000
	if nsecs != 0 {
		ct.prof.enabled = 1
	} else {
		ct.prof.enabled = 0
	}
	Popcli(fl)
}

func hack_sigaltstack(new, old *stackt) {
	fl := Pushcli()
	ct := Gscpu().mythread
	SS_DISABLE := int32(2)
	if old != nil {
		if ct.sigstack == 0 {
			old.ss_flags = SS_DISABLE
		} else {
			old.ss_sp = (*byte)(unsafe.Pointer(ct.sigstack))
			old.ss_size = ct.sigsize
			old.ss_flags = 0
		}
	}
	if new != nil {
		if new.ss_flags & SS_DISABLE != 0 {
			ct.sigstack = 0
			ct.sigsize = 0
		} else {
			ct.sigstack = uintptr(unsafe.Pointer(new.ss_sp))
			ct.sigsize = new.ss_size
		}
	}
	Popcli(fl)
}

func hack_write(fd int, bufn uintptr, sz uint32) int64 {
	if fd != 1 && fd != 2 {
		pancake("unexpected fd", uintptr(fd))
	}
	fl := Pushcli()
	Splock(pmsglock)
	c := uintptr(sz)
	for i := uintptr(0); i < c; i++ {
		p := (*int8)(unsafe.Pointer(bufn + i))
		putch(*p)
	}
	Spunlock(pmsglock)
	Popcli(fl)
	return int64(sz)
}

// "/etc/localtime"
var fnwhite = []int8{0x2f, 0x65, 0x74, 0x63, 0x2f, 0x6c, 0x6f, 0x63, 0x61,
    0x6c, 0x74, 0x69, 0x6d, 0x65}

// a is the C string.
func cstrmatch(a uintptr, b []int8) bool {
	for i, c := range b {
		p := (*int8)(unsafe.Pointer(a + uintptr(i)))
		if *p != c {
			return false
		}
	}
	return true
}

func hack_syscall(trap, a1, a2, a3 int64) (int64, int64, int64) {
	switch trap {
	case 1:
		r1 := hack_write(int(a1), uintptr(a2), uint32(a3))
		return r1, 0, 0
	case 2:
		enoent := int64(-2)
		if !cstrmatch(uintptr(a1), fnwhite) {
			pancake("unexpected open", 0)
		}
		return 0, 0, enoent
	default:
		pancake("unexpected syscall", uintptr(trap))
	}
	// not reached
	return 0, 0, -1
}

var futexlock = &Spinlock_t{}

// stack splitting prologue is not ok here, even though hack_futex is not
// called during interrupt context, because notetsleepg thinks hack_futex makes
// a system call and thus calls "entersyscallblock" which sets a flag that
// causes a panic if the stack needs to be split or if the g is preempted
// (though stackcheck() call below makes sure the stack is not overflowed).
//go:nosplit
func hack_futex(uaddr *int32, op, val int32, to *timespec, uaddr2 *int32,
    val2 int32) int64 {
	stackcheck()
	FUTEX_WAIT := int32(0) | _FUTEX_PRIVATE_FLAG
	FUTEX_WAKE := int32(1) | _FUTEX_PRIVATE_FLAG
	uaddrn := uintptr(unsafe.Pointer(uaddr))
	ret := 0
	switch op {
	case FUTEX_WAIT:
		Cli()
		Splock(futexlock)
		dosleep := *uaddr == val
		if dosleep {
			ct := Gscpu().mythread
			ct.futaddr = uaddrn
			ct.status = ST_WILLSLEEP
			ct.sleepfor = -1
			if to != nil {
				t := to.tv_sec * 1000000000
				t += to.tv_nsec
				ct.sleepfor = hack_nanotime() + int(t)
			}
			mktrap(TRAP_YIELD)
			// scheduler unlocks ·futexlock and returns with
			// interrupts enabled...
			Cli()
			ret = Gscpu().mythread.sleepret
			Sti()
		} else {
			Spunlock(futexlock)
			Sti()
			eagain := -11
			ret = eagain
		}
	case FUTEX_WAKE:
		woke := 0
		Cli()
		Splock(futexlock)
		Splock(threadlock)
		for i := 0; i < maxthreads && val > 0; i++ {
			t := &threads[i]
			st := t.status
			if t.futaddr == uaddrn && st == ST_SLEEPING {
				t.status = ST_RUNNABLE
				t.sleepfor = 0
				t.futaddr = 0
				t.sleepret = 0
				val--
				woke++
			}
		}
		Spunlock(threadlock)
		Spunlock(futexlock)
		Sti()
		ret = woke
	default:
		pancake("unexpected futex op", uintptr(op))
	}
	return int64(ret)
}

func hack_usleep(delay int64) {
	ts := timespec{}
	ts.tv_sec = delay/1000000
	ts.tv_nsec = (delay%1000000)*1000
	dummy := int32(0)
	FUTEX_WAIT := int32(0) | _FUTEX_PRIVATE_FLAG
	hack_futex(&dummy, FUTEX_WAIT, 0, &ts, nil, 0)
}

func hack_exit(code int32) {
	Cli()
	Gscpu().mythread.status = ST_INVALID
	pmsg("exit with code")
	pnum(uintptr(code))
	pmsg(".\nhalting\n")
	atomic.Store(&Halt, 1)
	for {
	}
}

// called in interupt context
//go:nosplit
func hack_nanotime() int {
	cyc := uint(Rdtsc())
	return int(cyc*Pspercycle/1000)
}

func Vtop(va unsafe.Pointer) (uintptr, bool) {
	van := uintptr(va)
	pte := pgdir_walk(van, false)
	if pte == nil || *pte & PTE_P == 0 {
		return 0, false
	}
	base := pte_addr(*pte)
	return base + (van & PGOFFMASK), true
}

// XXX also called in interupt context; remove when trapstub is moved into
// runtime
//go:nosplit
func Nanotime() int {
	return hack_nanotime()
}

// useful for basic tests of filesystem durability
func Crash() {
	pmsg("CRASH!\n")
	atomic.Store(&Halt, 1)
	for {
	}
}

// XXX also called in interupt context; remove when trapstub is moved into
// runtime
//go:nosplit
func Pnum(n int) {
	pnum(uintptr(n))
}

func GCmarktime() int {
	return int(work.totaltime)
}

func GCbgsweeptime() int {
	return int(work.bgsweeptime)
}

func GCwbenabledtime() int {
	return int(wbenabledtime)
}

func Heapsz() int {
	return int(memstats.next_gc)
}

func Setheap(n int) {
	heapminimum = uint64(n)
}

// initial res size is huge to not disturb boot; init(1) will set it to correct
// value.
var _maxheap int64 = 1 << 31

//type res_t struct {
//	maxheap		int64
//	ostanding	int64
//	// reservations for ops that have finished; only finished ops must have
//	// their outstanding reservations reduced to actual live by GC
//	fin		int64
//	gclive		int64
//}
//
//var res = res_t{maxheap: _maxheap}

func SetMaxheap(n int) {
	//res.maxheap = int64(n)
	_centralres.maxheap = int64(n)
	GC()
}

// must only be called when the world is stopped at the end of a GC
//func gcrescycle(live uint64) {
//	res.gclive = int64(live)
//	res.fin = 0
//}

func Cacheaccount() {
	//gp := getg()
	//if gp.res.allocs < gp.res.took {
	//	gp.res.allocs = gp.res.took
	//}
	gp := getg()
	gp.allused = true
}

func GCDebug(n int) {
	debug.gctrace = int32(n)
}

func GCDebugToggle() {
	if debug.gctrace != 0 {
		debug.gctrace = 0
	} else {
		debug.gctrace = 1
	}
}

func GCPacerToggle() {
	if debug.gcpacertrace != 0 {
		debug.gcpacertrace = 0
	} else {
		debug.gcpacertrace = 1
	}
}

//type Resobjs_t [_NumSizeClasses]uint32
// at the time of writing, biscuit allocates from 24 size classes.
// Objsadd/Objssub assumes this is an array of 24 uint32s; fix Objsadd if you
// change this type.
type Resobjs_t [24]uint32 // NOTICE ABOVE!

type Res_t struct {
	Objs	Resobjs_t
}

var _centralres = struct {
	avail		Res_t
	tmp		Res_t
	// XXX remove when maxlive supports sizeclasses
	maxheap		int64
}{
	avail: Res_t{Resobjs_t{1: uint32(_maxheap)}},
	maxheap: _maxheap,
}

// must only be called when the world is stopped at the end of a GC
func grescycle(newlastgc *Res_t, livebytes uint64) {
	if hackmode == 0 {
		return
	}
	_centralres.tmp = Res_t{}
	for _, p := range allp {
		mc := p.mcache
		mc.avail = Res_t{}
		Objsadd(&mc.outstand.Objs, &_centralres.tmp.Objs)
	}
	// XXX
	left := uint32(_centralres.maxheap - int64(livebytes))
	left -= _centralres.tmp.Objs[1]
	if int32(left) < 0 {
		left = 0
	}
	_centralres.avail.Objs[1] = left
}

// may steal more credit than asked for
func robcentral(scidx int, want uint32) uint32 {
	p := &_centralres.avail.Objs[scidx]
	for {
		left := atomic.Load(p)
		if want > left {
			return 0
		}
		took := want
		if atomic.Cas(p, left, left - took) {
			return took
		}
	}
}

var Maxa	uint32
var Byuf	[]uintptr

func casbt(ne uint32) {
	for {
		v := atomic.Load(&Maxa)
		if ne <= v {
			return
		}
		if atomic.Cas(&Maxa, v, ne) {
			buf := make([]uintptr, 10)
			got := callers(2, buf)
			buf = buf[:got]
			Byuf = buf
			return
		}
	}
}

var Flushlim uint32 = 1 << 24

func Greserve(want *Res_t) bool {
	mp := acquirem()
	g := getg()
	mc := gomcache()

	//mc.alls++
	if m := Objscmp(&want.Objs, &mc.avail.Objs); m == 0 {
		Objsadd(&want.Objs, &g.res1.Objs)
		Objsadd(&want.Objs, &mc.outstand.Objs)
		Objssub(&want.Objs, &mc.avail.Objs)
	} else {
		//mc.robs++
		for i := 0; i < len(mc.avail.Objs); i++ {
			if m & (1 << uint(i)) != 0 {
				//mc.robi++
				rob := robcentral(i, want.Objs[i])
				if rob == 0 {
					releasem(mp)
					return false
				}
				mc.avail.Objs[i] += rob
			}
		}
	}

	releasem(mp)
	return true
}

func Gresrelease() {
	g := getg()
	mc := gomcache()

	Objssub(&g.res1.Objs, &mc.outstand.Objs)
	// compute unused objects
	if g.used.Objs[1] < g.res1.Objs[1] {
		Objssub(&g.used.Objs, &g.res1.Objs)
	} else {
		// this block is possible due to implicitly reserved
		// allocations (for the first process, one-process exit,
		// logging daemon, etc.)
		g.res1.Objs[1] = 0
	}
	// add unused objects back to credit
	if g.allused {
		g.allused = false
	} else {
		Objsadd(&g.res1.Objs, &mc.avail.Objs)
		// XXX SSE max?
		if mc.avail.Objs[1] > Flushlim {
			mc.avail.Objs[1] -= Flushlim
			atomic.Xadd(&_centralres.avail.Objs[1], int32(Flushlim))
		}
	}

	g.res1 = Res_t{}
	g.used = Res_t{}
}

func Remain() int {
	return int(_centralres.avail.Objs[1])
}

func Gptr() unsafe.Pointer {
	gp := getg()
	return gp.current
}

func Setgptr(p unsafe.Pointer) {
	gp := getg()
	gp.current = p
}

//go:nosplit
//go:nowritebarrierrec
func setsig(i uint32, fn uintptr) {
	var sa sigactiont
	sa.sa_flags = _SA_SIGINFO | _SA_ONSTACK | _SA_RESTORER | _SA_RESTART
	sigfillset(&sa.sa_mask)
	// Although Linux manpage says "sa_restorer element is obsolete and
	// should not be used". x86_64 kernel requires it. Only use it on
	// x86.
	if GOARCH == "386" || GOARCH == "amd64" {
		sa.sa_restorer = funcPC(sigreturn)
	}
	if fn == funcPC(sighandler) {
		if iscgo {
			fn = funcPC(cgoSigtramp)
		} else {
			fn = funcPC(sigtramp)
		}
	}
	sa.sa_handler = fn
	sigaction(i, &sa, nil)
}

//go:nosplit
//go:nowritebarrierrec
func setsigstack(i uint32) {
	var sa sigactiont
	sigaction(i, nil, &sa)
	if sa.sa_flags&_SA_ONSTACK != 0 {
		return
	}
	sa.sa_flags |= _SA_ONSTACK
	sigaction(i, &sa, nil)
}

//go:nosplit
//go:nowritebarrierrec
func getsig(i uint32) uintptr {
	var sa sigactiont
	sigaction(i, nil, &sa)
	return sa.sa_handler
}

// setSignaltstackSP sets the ss_sp field of a stackt.
//go:nosplit
func setSignalstackSP(s *stackt, sp uintptr) {
	*(*uintptr)(unsafe.Pointer(&s.ss_sp)) = sp
}

func (c *sigctxt) fixsigcode(sig uint32) {
}

// sysSigaction calls the rt_sigaction system call.
//go:nosplit
func sysSigaction(sig uint32, new, old *sigactiont) {
	if rt_sigaction(uintptr(sig), new, old, unsafe.Sizeof(sigactiont{}.sa_mask)) != 0 {
		// Use system stack to avoid split stack overflow on ppc64/ppc64le.
		systemstack(func() {
			throw("sigaction failed")
		})
	}
}

// rt_sigaction is implemented in assembly.
//go:noescape
func rt_sigaction(sig uintptr, new, old *sigactiont, size uintptr) int32
