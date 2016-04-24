// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"
import "runtime/internal/atomic"

type mOS struct{}

//go:noescape
func futex(addr unsafe.Pointer, op int32, val uint32, ts, addr2 unsafe.Pointer, val3 uint32) int32

//go:noescape
func clone(flags int32, stk, mm, gg, fn unsafe.Pointer) int32

//go:noescape
func rt_sigaction(sig uintptr, new, old *sigactiont, size uintptr) int32

//go:noescape
func sigaltstack(new, old *sigaltstackt)

//go:noescape
func setitimer(mode int32, new, old *itimerval)

//go:noescape
func rtsigprocmask(sig uint32, new, old *sigset, size int32)

//go:noescape
func getrlimit(kind int32, limit unsafe.Pointer) int32
func raise(sig int32)
func raiseproc(sig int32)

//go:noescape
func sched_getaffinity(pid, len uintptr, buf *uintptr) int32

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
func htpause()
func invlpg(uintptr)
func Inb(uint16) uint
func Inl(int) int
func Insl(int, unsafe.Pointer, int)
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
func Rdmsr(int) int
func Rdtsc() uint64
func Sgdt(*uintptr)
func Sidt(*uintptr)
func stackcheck()
func Sti()
func _sysentry()
func _trapret(*[TFSIZE]uintptr)
func trapret(*[TFSIZE]uintptr, uintptr)
func _userint()
func _userret()
func _Userrun(*[TFSIZE]int, bool) (int, int)
func Wrmsr(int, int)

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
	//pmap		*[512]int
	//pms		[]*[512]int
	//pid		uintptr
}

var Cpumhz uint
var Pspercycle uint

const MAXCPUS int = 32

var cpus [MAXCPUS]cpu_t

type tuser_t struct {
	tf	*[TFSIZE]uintptr
	fxbuf	*[FXREGS]uintptr
}

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
	user		tuser_t
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

func Userrun(tf *[TFSIZE]int, fxbuf *[FXREGS]int, pmap *[512]int,
    p_pmap uintptr, pms []*[512]int, fastret bool) (int, int) {

	// {enter,exit}syscall() may not be worth the overhead. i believe the
	// only benefit for biscuit is that cpus running in the kernel could GC
	// while other cpus execute user programs.
	//entersyscall(0)
	fl := Pushcli()
	cpu := _Gscpu()
	ct := cpu.mythread

	if cpu.shadowcr3 != p_pmap {
		Lcr3(p_pmap)
		cpu.shadowcr3 = p_pmap
	}
	// set shadow pointers for user pmap so it isn't free'd out from under
	// us if the process terminates soon.
	//cpu.pmap = pmap
	//cpu.pms = pms
	//cpu.pid = uintptr(pid)
	// avoid write barriers since we are uninterruptible. the caller must
	// also have these references anyway, so skipping them is ok.
	//*(*uintptr)(unsafe.Pointer(&cpu.pmap)) = uintptr(unsafe.Pointer(pmap))
	//*(*[3]uintptr)(unsafe.Pointer(&cpu.pms)) = *(*[3]uintptr)(unsafe.Pointer(&pms))

	// if doing a fast return after a syscall, we need to restore some user
	// state manually
	if cpu.shadowfs != uintptr(tf[TF_FSBASE]) {
		ia32_fs_base := 0xc0000100
		Wrmsr(ia32_fs_base, tf[TF_FSBASE])
		cpu.shadowfs = uintptr(tf[TF_FSBASE])
	}

	// we only save/restore SSE registers on cpu exception/interrupt, not
	// during syscall exit/return. this is OK since sys5ABI defines the SSE
	// registers to be caller-saved.
	// XXX types
	//ct.user.tf = (*[TFSIZE]uintptr)(unsafe.Pointer(tf))
	//ct.user.fxbuf = (*[FXREGS]uintptr)(unsafe.Pointer(fxbuf))

	// avoid write barriers, see note above
	*(*uintptr)(unsafe.Pointer(&ct.user.tf)) = uintptr(unsafe.Pointer(tf))
	*(*uintptr)(unsafe.Pointer(&ct.user.fxbuf)) = uintptr(unsafe.Pointer(fxbuf))

	intno, aux := _Userrun(tf, fastret)

	ct.user.tf = nil
	ct.user.fxbuf = nil
	Popcli(fl)
	//exitsyscall(0)
	return intno, aux
}

// caller must have interrupts cleared
//go:nosplit
func shadow_clear() {
	//cpu := Gscpu()
	//cpu.pmap = nil
	//cpu.pms = nil
}

type nmiprof_t struct {
	buf		[]uintptr
	bufidx		uint64
	LVTmask		bool
	evtsel		int
	evtmin		uint
	evtmax		uint
	gctrl		int
}

var _nmibuf [4096]uintptr
var nmiprof = nmiprof_t{buf: _nmibuf[:]}

func SetNMI(mask bool, evtsel int, min, max uint) {
	nmiprof.LVTmask = mask
	nmiprof.evtsel = evtsel
	nmiprof.evtmin = min
	nmiprof.evtmax = max
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
	lastbranch_tos := 0x1c9
	lastbranch_0_from_ip := 0x680
	lastbranch_0_to_ip := 0x6c0

	last := Rdmsr(lastbranch_tos) & 0xf
	// XXX stacklen
	l := 16 * 2
	l++
	idx := int(atomic.Xadd64(&nmiprof.bufidx, int64(l)))
	idx -= l
	for i := 0; i < 16; i++ {
		cur := (last - i)
		if cur < 0 {
			cur += 16
		}
		from := uintptr(Rdmsr(lastbranch_0_from_ip + cur))
		to := uintptr(Rdmsr(lastbranch_0_to_ip + cur))
		Wrmsr(lastbranch_0_from_ip + cur, 0)
		Wrmsr(lastbranch_0_to_ip + cur, 0)
		if idx + 2*i + 1 >= len(nmiprof.buf) {
			Cpuprint('!', 1)
			break
		}
		nmiprof.buf[idx+2*i] = from
		nmiprof.buf[idx+2*i+1] = to
	}
	idx += l - 1
	if idx < len(nmiprof.buf) {
		nmiprof.buf[idx] = ^uintptr(0)
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
	jcc := 1 << 2
	indjmp := 1 << 6
	//reljmp := 1 << 7
	farbr := 1 << 8
	dv := jcc | farbr | indjmp
	Wrmsr(lbr_select, dv)

	freeze_lbrs_on_pmi := 1 << 11
	lbrs := 1 << 0
	dv = lbrs | freeze_lbrs_on_pmi
	Wrmsr(ia32_debugctl, dv)
}

//go:nosplit
func perfgather(tf *[TFSIZE]uintptr) {
	idx := atomic.Xadd64(&nmiprof.bufidx, 1) - 1
	if idx < uint64(len(nmiprof.buf)) {
		//nmiprof.buf[idx] = tf[TF_RIP]
		//pid := Gscpu().pid
		//v := tf[TF_RIP] | (pid << 56)
		v := tf[TF_RIP]
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

	// set both bytes for divisor baud rate
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

//go:nosplit
func sc_put(c int8) {
	if c == '\n' {
		sc_put_('\r')
	}
	sc_put_(c)
	if c == '\b' {
		// clear the previous character
		sc_put_(' ')
		sc_put_('\b')
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

var hackmode int64
var Halt uint32

// wait until remove definition from proc.c
type spinlock_t struct {
	v	uint32
}

//go:nosplit
func splock(l *spinlock_t) {
	for {
		if atomic.Xchg(&l.v, 1) == 0 {
			break
		}
		for l.v != 0 {
			htpause()
		}
	}
}

//go:nosplit
func spunlock(l *spinlock_t) {
	//atomic.Store(&l.v, 0)
	l.v = 0
}

// since this lock may be taken during an interrupt (only under fatal error
// conditions), interrupts must be cleared before attempting to take this lock.
var pmsglock = &spinlock_t{}

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
	splock(pmsglock)
	_pmsg(msg)
	spunlock(pmsglock)
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
	splock(pmsglock)
	_pnum(n)
	spunlock(pmsglock)
	Popcli(fl)
}

func Pmsga(_p *uint8, c int, attr int8) {
	pn := uintptr(unsafe.Pointer(_p))
	fl := Pushcli()
	splock(pmsglock)
	for i := uintptr(0); i < uintptr(c); i++ {
		p := (*int8)(unsafe.Pointer(pn+i))
		putcha(*p, attr)
	}
	spunlock(pmsglock)
	Popcli(fl)
}

var _cpuattrs [MAXCPUS]uint16

//go:nosplit
func Cpuprint(n uint16, row uintptr) {
	p := uintptr(0xb8000)
	num := Gscpu().num
	p += uintptr(num) * row*80*2
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
func Xyield()
func Xsyscall()
func Xtlbshoot()
func Xsigret()
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
	var f func()
	f = intentry
	entry := **(**uint)(unsafe.Pointer(&f))

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

	// interrupts
	irqbase := 32
	int_set(irqbase+ 0,  Xtimer,  1)
	int_set(irqbase+ 1,  Xirq1,   1)
	int_set(irqbase+ 2,  Xirq2,   1)
	int_set(irqbase+ 3,  Xirq3,   1)
	int_set(irqbase+ 4,  Xirq4,   1)
	int_set(irqbase+ 5,  Xirq5,   1)
	int_set(irqbase+ 6,  Xirq6,   1)
	int_set(irqbase+ 7,  Xirq7,   1)
	int_set(irqbase+ 8,  Xirq8,   1)
	int_set(irqbase+ 9,  Xirq9,   1)
	int_set(irqbase+10,  Xirq10,  1)
	int_set(irqbase+11,  Xirq11,  1)
	int_set(irqbase+12,  Xirq12,  1)
	int_set(irqbase+13,  Xirq13,  1)
	int_set(irqbase+14,  Xirq14,  1)
	int_set(irqbase+15,  Xirq15,  1)

	int_set(48,  Xspur,    1)
	// no longer used
	//int_set(49,  Xyield,   1)
	//int_set(64,  Xsyscall, 1)

	int_set(70,  Xtlbshoot, 1)
	// no longer used
	//int_set(71,  Xsigret,   1)
	int_set(72,  Xperfmask, 1)

	p := pdesc_t{}
	pdsetup(&p, unsafe.Pointer(&_idt[0]), unsafe.Sizeof(_idt) - 1)
	lidt(p)
}

const (
	PTE_P		uintptr = 1 << 0
	PTE_W		uintptr = 1 << 1
	PTE_U		uintptr = 1 << 2
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
var p_kpmap uintptr

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
	memclr(tva, PGSIZE)
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
	// bootloader provides 15 e820 entries at most (it panicks if the PC
	// provides more).
	for i := uintptr(0); i < 15; i++ {
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
	return int(_eseg.start + _eseg.len)
}

func Get_phys() uintptr {
	return get_pg()
}

//go:nosplit
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

//go:nosplit
func alloc_map(va uintptr, perms uintptr, fempty bool) {
	pte := pgdir_walk(va, true)
	old := *pte
	if old & PTE_P != 0 && fempty {
		pancake("expected empty pte", old)
	}
	p_pg := get_pg()
	zero_phys(p_pg)
	// XXX goodbye, memory
	*pte = p_pg | perms | PTE_P
	if old & PTE_P != 0 {
		invlpg(va)
	}
}

var fxinit [FXREGS]uintptr

// nosplit because APs call this function before FS is setup
//go:nosplit
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
		chkalign(unsafe.Pointer(&fxinit[0]), 16)
		fxsave(&fxinit)

		chksize(FXREGS*8, unsafe.Sizeof(threads[0].fx))
		for i := range threads {
			chkalign(unsafe.Pointer(&threads[i].fx), 16)
		}
	}
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
	if lver < 0x10 {
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
	// mask cmci, lint[01], error, perf counters, and thermal sensor
	wlap(LVCMCI,    maskint)
	// unmask LINT0 and LINT1. soon, i will use IO APIC instead.
	wlap(LVINT0,    rlap(LVINT0) &^ maskint)
	wlap(LVINT1,    rlap(LVINT1) &^ maskint)
	wlap(LVERROR,   maskint)
	wlap(LVPERF,    maskint)
	wlap(LVTHERMAL, maskint)

	ia32_apic_base := 0x1b
	reg := uintptr(Rdmsr(ia32_apic_base))
	if reg & (1 << 11) == 0 {
		pancake("lapic disabled?", reg)
	}
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

func proc_setup() {
	var dur func()
	dur = _userint
	_userintaddr = **(**uintptr)(unsafe.Pointer(&dur))
	var dur2 func(int32, unsafe.Pointer, *ucontext_t)
	dur2 = sigsim
	_sigsimaddr = **(**uintptr)(unsafe.Pointer(&dur2))

	chksize(TFSIZE*8, unsafe.Sizeof(threads[0].tf))
	fpuinit(true)
	// initialize the first thread: us
	threads[0].status = ST_RUNNING
	threads[0].p_pmap = p_kpmap

	// initialize GS pointers
	for i := range cpus {
		cpus[i].this = &cpus[i]
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
	Gscpu().num = 0
	Gscpu().mythread = &threads[0]
}

//go:nosplit
func Ap_setup(cpunum uint) {
	// interrupts are probably already cleared
	fl := Pushcli()

	splock(pmsglock)
	_pmsg("cpu")
	_pnum(uintptr(cpunum))
	_pmsg("joined\n")
	spunlock(pmsglock)

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
	fs_null()
	gs_null()
	gs_set(mycpu)
	Gscpu().num = cpunum
	Gscpu().mythread = nil

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
	var dur func()
	dur = _sysentry
	sysentryaddr := **(**uintptr)(unsafe.Pointer(&dur))
	Wrmsr(sysenter_eip, int(sysentryaddr))

	sysenter_esp := 0x175
	Wrmsr(sysenter_esp, 0)
}

var tlbshoot_wait uintptr
var tlbshoot_pg uintptr
var tlbshoot_count uintptr
var tlbshoot_pmap uintptr
var tlbshoot_gen uint64

func Tlbadmit(p_pmap, cpuwait, pg, pgcount uintptr) uint64 {
	for !atomic.Casuintptr(&tlbshoot_wait, 0, cpuwait) {
		preemptok()
	}
	atomic.Xchguintptr(&tlbshoot_pg, pg)
	atomic.Xchguintptr(&tlbshoot_count, pgcount)
	atomic.Xchguintptr(&tlbshoot_pmap, p_pmap)
	atomic.Xadd64(&tlbshoot_gen, 1)
	return tlbshoot_gen
}

func Tlbwait(gen uint64) {
	for atomic.Loaduintptr(&tlbshoot_wait) != 0 {
		if atomic.Load64(&tlbshoot_gen) != gen {
			break
		}
	}
}

// must be nosplit since called at interrupt time
//go:nosplit
func tlb_shootdown() {
	ct := Gscpu().mythread
	if ct != nil && ct.p_pmap == tlbshoot_pmap {
		// the TLB was already invalidated since trap() currently
		// switches to kernel pmap on any exception/interrupt other
		// than NMI.
		//start := tlbshoot_pg
		//end := tlbshoot_pg + tlbshoot_count * PGSIZE
		//for ; start < end; start += PGSIZE {
		//	invlpg(start)
		//}
	}
	dur := (*uint64)(unsafe.Pointer(&tlbshoot_wait))
	v := atomic.Xadd64(dur, -1)
	if v < 0 {
		pancake("shootwait < 0", uintptr(v))
	}
	sched_resume(ct)
}

// this function checks to see if another thread is trying to preempt this
// thread (perhaps to start a GC). this is called when go code is spinning on a
// spinlock in order to avoid a deadlock where the thread that acquired the
// spinlock starts a GC and waits forever for the spinning thread. (go code
// should probably not use spinlocks. tlb shootdown code is the only code
// protected by a spinlock since the lock must both be acquired in go code and
// in interrupt context.)
//
// alternatively, we could make sure that no allocations are made while the
// spinlock is acquired.
func preemptok() {
	gp := getg()
	StackPreempt := uintptr(0xfffffffffffffade)
	if gp.stackguard0 == StackPreempt {
		pmsg("!")
		// call function with stack splitting prologue
		_dummy()
	}
}

var _notdeadcode uint32
func _dummy() {
	if _notdeadcode != 0 {
		_dummy()
	}
	_notdeadcode = 0
}

// cpu exception/interrupt vectors
const (
	TRAP_NMI	= 2
	TRAP_PGFAULT	= 14
	TRAP_SYSCALL	= 64
	TRAP_TIMER	= 32
	TRAP_DISK	= (32 + 14)
	TRAP_SPUR	= 48
	TRAP_YIELD	= 49
	TRAP_TLBSHOOT	= 70
	TRAP_SIGRET	= 71
	TRAP_PERFMASK	= 72
	IRQ_BASE	= 32
)

var threadlock = &spinlock_t{}

// maximum # of runtime "OS" threads
const maxthreads = 64
var threads [maxthreads]thread_t

// thread states
const (
	ST_INVALID	= 0
	ST_RUNNABLE	= 1
	ST_RUNNING	= 2
	ST_WAITING	= 3
	ST_SLEEPING	= 4
	ST_WILLSLEEP	= 5
)

// scheduler constants
const (
	HZ	= 100
)

var _userintaddr uintptr
var _sigsimaddr uintptr
var _newtrap func(*[TFSIZE]uintptr)

// XXX remove newtrap?
func Install_traphandler(newtrap func(*[TFSIZE]uintptr)) {
	_newtrap = newtrap
}

//go:nosplit
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
func trap(tf *[TFSIZE]uintptr) {
	trapno := tf[TF_TRAPNO]

	if trapno == TRAP_NMI {
		perfgather(tf)
		perfmask()
		_trapret(tf)
	}

	Lcr3(p_kpmap)
	cpu := Gscpu()
	cpu.shadowcr3 = p_kpmap

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

	// clear shadow pointers to user pmap
	shadow_clear()

	// don't add code before FPU context saving unless you've thought very
	// carefully! it is easy to accidentally and silently corrupt FPU state
	// (ie calling memmove indirectly by assignment of large datatypes)
	// before it is saved below.

	// save FPU state immediately before we clobber it
	if ct != nil {
		// if in user mode, save to user buffers and make it look like
		// Userrun returned.
		if ct.user.tf != nil {
			ufx := ct.user.fxbuf
			fxsave(ufx)
			utf := ct.user.tf
			*utf = *tf
			ct.tf[TF_RIP] = _userintaddr
			ct.tf[TF_RSP] = cpu.sysrsp
			ct.tf[TF_RAX] = trapno
			ct.tf[TF_RBX] = Rcr2()
			// XXXPANIC
			if trapno == TRAP_YIELD || trapno == TRAP_SIGRET {
				pancake("nyet", trapno)
			}
			// XXX fix this using RIP method
			// if we are unlucky enough for a timer int to come in
			// before we execute the first instruction of the new
			// rip, make sure the state we just saved isn't
			// clobbered
			ct.user.tf = nil
			ct.user.fxbuf = nil
		} else {
			fxsave(&ct.fx)
			ct.tf = *tf
		}
		//timetick(ct)
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
		splock(threadlock)
		if ct != nil {
			if ct.status == ST_WILLSLEEP {
				ct.status = ST_SLEEPING
				// XXX set IF, unlock
				ct.tf[TF_RFLAGS] |= TF_FL_IF
				spunlock(futexlock)
			} else {
				ct.status = ST_RUNNABLE
			}
		}
		if !yielding {
			lap_eoi()
			if cpu.num == 0 {
				wakeup()
				proftick()
			}
		}
		// yieldy doesn't return
		yieldy()
	} else if is_irq(trapno) {
		if _newtrap != nil {
			// catch kernel faults that occur while trying to
			// handle user traps
			_newtrap(tf)
		} else {
			pancake("IRQ without ntrap", trapno)
		}
		sched_resume(ct)
	} else if is_cpuex(trapno) {
		// we vet out kernel mode CPU exceptions above must be from
		// user program. thus return from Userrun() to kernel.
		sched_run(ct)
	} else if trapno == TRAP_SIGRET {
		// does not return
		sigret(ct)
	} else if trapno == TRAP_PERFMASK {
		lap_eoi()
		perfmask()
		sched_resume(ct)
	} else {
		pancake("unexpected int", trapno)
	}
	// not reached
	pancake("no returning", 0)
}

//go:nosplit
func is_irq(trapno uintptr) bool {
	return trapno > IRQ_BASE && trapno <= IRQ_BASE + 15
}

//go:nosplit
func is_cpuex(trapno uintptr) bool {
	return trapno < IRQ_BASE
}

//go:nosplit
func _tchk() {
	if rflags() & TF_FL_IF != 0 {
		pancake("must not be interruptible", 0)
	}
	if threadlock.v == 0 {
		pancake("must hold threadlock", 0)
	}
}

//go:nosplit
func sched_halt() {
	cpu_halt(Gscpu().rsp)
}

//go:nosplit
func sched_run(t *thread_t) {
	if t.tf[TF_RFLAGS] & TF_FL_IF == 0 {
		pancake("thread not interurptible", 0)
	}
	// mythread never references a heap allocated object. avoid
	// writebarrier since sched_run can be executed at any time, even when
	// GC invariants do not hold (like when g.m.p == nil).
	//Gscpu().mythread = t
	*(*uintptr)(unsafe.Pointer(&Gscpu().mythread)) = uintptr(unsafe.Pointer(t))
	fxrstor(&t.fx)
	trapret(&t.tf, t.p_pmap)
}

//go:nosplit
func sched_resume(ct *thread_t) {
	if ct != nil {
		sched_run(ct)
	} else {
		sched_halt()
	}
}

//go:nosplit
func wakeup() {
	_tchk()
	now := hack_nanotime()
	timedout := -110
	for i := range threads {
		t := &threads[i]
		sf := t.sleepfor
		if t.status == ST_SLEEPING && sf != -1 && sf < now {
			t.status = ST_RUNNABLE
			t.sleepfor = 0
			t.futaddr = 0
			t.sleepret = timedout
		}
	}
}

//go:nosplit
func yieldy() {
	_tchk()
	cpu := Gscpu()
	ct := cpu.mythread
	_ti := (uintptr(unsafe.Pointer(ct)) -
	    uintptr(unsafe.Pointer(&threads[0])))/unsafe.Sizeof(thread_t{})
	ti := int(_ti)
	start := (ti + 1) % maxthreads
	if ct == nil {
		start = 0
	}
	for i := 0; i < maxthreads; i++ {
		idx := (start + i) % maxthreads
		t := &threads[idx]
		if t.status == ST_RUNNABLE {
			t.status = ST_RUNNING
			spunlock(threadlock)
			sched_run(t)
		}
	}
	cpu.mythread = nil
	spunlock(threadlock)
	sched_halt()
}

// called only once to setup
func Trapinit() {
	mcall(trapinit_m)
}

func Trapsched() {
	mcall(trapsched_m)
}

// called from the CPU interrupt handler. must only be called while interrupts
// are disabled
 //go:nosplit
 func Trapwake() {
	splock(tlock);
	_nints++
	// only flag the Ps if a handler isn't currently active
	if trapst == IDLE {
		_ptrap = true
	}
	spunlock(tlock)
}
// trap handling goroutines first call. it is not an error if there are no
// interrupts when this is called.
func trapinit_m(gp *g) {
	_trapsched(gp, true)
}

func trapsched_m(gp *g) {
	_trapsched(gp, false)
}

var tlock = &spinlock_t{}

var _initted bool
var _trapsleeper *g
var _nints int

// i think it is ok for these trap sched/wake functions to be nosplit and call
// nosplit functions even though they clear interrupts because we first switch
// to the scheduler stack where preemptions are ignored.
func _trapsched(gp *g, firsttime bool) {
	fl := Pushcli()
	splock(tlock)

	if firsttime {
		if _initted {
			pancake("two inits", 0)
		}
		_initted = true
		// if there are traps already, let a P wake us up.
		//goto bed
	}

	// decrement handled ints
	if !firsttime {
		if _nints <= 0 {
			pancake("neg ints", uintptr(_nints))
		}
		_nints--
	}

	// check if we are done
	if !firsttime && _nints != 0 {
		// keep processing
		tprepsleep(gp, false)
		_g_ := getg()
		runqput(_g_.m.p.ptr(), gp, true)
	} else {
		tprepsleep(gp, true)
		if _trapsleeper != nil {
			pancake("trapsleeper set", 0)
		}
		_trapsleeper = gp
	}

	spunlock(tlock)
	Popcli(fl)

	schedule()
}

var trapst int
const (
	IDLE	= iota
	RUNNING	= iota
)

func tprepsleep(gp *g, done bool) {
	if done {
		trapst = IDLE
	} else {
		trapst = RUNNING
	}

	status := readgstatus(gp)
	if((status&^_Gscan) != _Grunning){
		pancake("bad g status", uintptr(status))
	}
	nst := uint32(_Grunnable)
	if done {
		nst = _Gwaiting
		gp.waitreason = "waiting for trap"
	}
	casgstatus(gp, _Grunning, nst)
	dropg()
}

var _ptrap bool

func trapcheck(pp *p) {
	if !_ptrap {
		return
	}
	fl := Pushcli()
	splock(tlock)

	var trapgp *g
	if !_ptrap {
		goto out
	}
	// don't clear the start flag if the handler goroutine hasn't
	// registered yet.
	if !_initted {
		goto out
	}
	if trapst == RUNNING {
		goto out
	}

	if trapst != IDLE {
		pancake("bad trap status", uintptr(trapst))
	}

	_ptrap = false
	trapst = RUNNING

	trapgp = _trapsleeper
	_trapsleeper = nil

	spunlock(tlock)
	Popcli(fl)

	casgstatus(trapgp, _Gwaiting, _Grunnable)

	// hopefully the trap goroutine is executed soon
	runqput(pp, trapgp, true)
	return
out:
	spunlock(tlock)
	Popcli(fl)
	return
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

//go:nosplit
func proftick() {
	// goprofile period = 10ms
	profns := 10000000
	n := hack_nanotime()

	if n - _lastprof < profns {
		return
	}
	_lastprof = n

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
	memclr(unsafe.Pointer(_ctxt), ucsz)
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

//go:nosplit
func sigsim(signo int32, si unsafe.Pointer, ctx *ucontext_t) {
	// the purpose of fakesig is to enter the runtime's usual signal
	// handler from go code. the signal handler uses sys5abi, so fakesig
	// converts between calling conventions.
	fakesig(signo, nil, ctx)
	mktrap(TRAP_SIGRET)
}

//go:nosplit
func sigret(t *thread_t) {
	if t.status != ST_RUNNING {
		pancake("uh oh!", uintptr(t.status))
	}
	t.tf = t.sigtf
	t.fx = t.sigfx
	t.doingsig = 0
	if t.status != ST_RUNNING {
		pancake("wut", uintptr(t.status))
	}
	sched_run(t)
}

// if sigsim() is used to deliver signals other than SIGPROF, you will need to
// construct siginfo_t and more of context.

func find_empty(sz uintptr) uintptr {
	v := caddr(0, 0, 0, 1, 0)
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

var maplock = &spinlock_t{}

// this flag makes hack_mmap panic if a new pml4 entry is ever added to the
// kernel's pmap. we want to make sure all kernel mappings added after bootup
// fall into the same pml4 entry so that all the kernel mappings can be easily
// shared in user process pmaps.
var _nopml4 bool

func Pml4freeze() {
	_nopml4 = true
}

func hack_mmap(va, _sz uintptr, _prot uint32, _flags uint32,
    fd int32, offset int32) uintptr {
	fl := Pushcli()
	splock(maplock)

	MAP_ANON := uintptr(0x20)
	MAP_PRIVATE := uintptr(0x2)
	PROT_NONE := uintptr(0x0)
	PROT_WRITE := uintptr(0x2)

	prot := uintptr(_prot)
	flags := uintptr(_flags)
	var vaend uintptr
	var perms uintptr
	var ret uintptr
	var t uintptr
	pgleft := pglast - pgfirst
	sz := pgroundup(_sz)
	if sz > pgleft {
		ret = ^uintptr(0)
		goto out
	}
	sz = pgroundup(va + _sz)
	sz -= pgrounddown(va)
	if va == 0 {
		va = find_empty(sz)
	}
	vaend = caddr(VUEND, 0, 0, 0, 0)
	if va >= vaend || va + sz >= vaend {
		pancake("va space exhausted", va)
	}

	t = MAP_ANON | MAP_PRIVATE
	if flags & t != t {
		pancake("unexpected flags", flags)
	}
	perms = PTE_P
	if prot == PROT_NONE {
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
out:
	spunlock(maplock)
	Popcli(fl)
	return ret
}

func hack_munmap(v, _sz uintptr) {
	fl := Pushcli()
	splock(maplock)
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
	spunlock(maplock)
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

func hack_clone(flags uint32, rsp uintptr, mp *m, gp *g, fn uintptr) {
	CLONE_VM := 0x100
	CLONE_FS := 0x200
	CLONE_FILES := 0x400
	CLONE_SIGHAND := 0x800
	CLONE_THREAD := 0x10000
	chk := uint32(CLONE_VM | CLONE_FS | CLONE_FILES | CLONE_SIGHAND |
	    CLONE_THREAD)
	if flags != chk {
		pancake("unexpected clone args", uintptr(flags))
	}
	var dur func(uintptr)
	dur = clone_wrap
	cloneaddr := **(**uintptr)(unsafe.Pointer(&dur))

	fl := Pushcli()
	splock(threadlock)

	ti := thread_avail()
	// provide fn as arg to clone_wrap
	rsp -= 8
	*(*uintptr)(unsafe.Pointer(rsp)) = fn
	rsp -= 8
	// bad return address
	*(*uintptr)(unsafe.Pointer(rsp)) = 0

	mt := &threads[ti]
	memclr(unsafe.Pointer(mt), unsafe.Sizeof(thread_t{}))
	mt.tf[TF_CS] = KCODE64 << 3
	mt.tf[TF_RSP] = rsp
	mt.tf[TF_RIP] = cloneaddr
	mt.tf[TF_RFLAGS] = rflags() | TF_FL_IF
	mt.tf[TF_GSBASE] = uintptr(unsafe.Pointer(&mp.tls[0])) + 8

	// avoid write barrier for mp here since we have interrupts clear. Ms
	// are always reachable from allm anyway. see comments in runtime2.go
	//gp.m = mp
	*(*uintptr)(unsafe.Pointer(&gp.m)) = uintptr(unsafe.Pointer(mp))
	mp.tls[0] = uintptr(unsafe.Pointer(gp))
	mp.procid = uint64(ti)
	mt.status = ST_RUNNABLE
	mt.p_pmap = p_kpmap

	mt.fx = fxinit

	spunlock(threadlock)
	Popcli(fl)
}

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

func hack_sigaltstack(new, old *sigaltstackt) {
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
	splock(pmsglock)
	c := uintptr(sz)
	for i := uintptr(0); i < c; i++ {
		p := (*int8)(unsafe.Pointer(bufn + i))
		putch(*p)
	}
	spunlock(pmsglock)
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

var futexlock = &spinlock_t{}

// XXX not sure why stack splitting prologue is not ok here
//go:nosplit
func hack_futex(uaddr *int32, op, val int32, to *timespec, uaddr2 *int32,
    val2 int32) int64 {
	stackcheck()
	FUTEX_WAIT := int32(0)
	FUTEX_WAKE := int32(1)
	uaddrn := uintptr(unsafe.Pointer(uaddr))
	ret := 0
	switch op {
	case FUTEX_WAIT:
		Cli()
		splock(futexlock)
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
			spunlock(futexlock)
			Sti()
			eagain := -11
			ret = eagain
		}
	case FUTEX_WAKE:
		woke := 0
		Cli()
		splock(futexlock)
		splock(threadlock)
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
		spunlock(threadlock)
		spunlock(futexlock)
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
	FUTEX_WAIT := int32(0)
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

func Kpmap_p() uintptr {
	return p_kpmap
}

func GCworktime() int {
	return int(work.totaltime)
}

func Heapsz() int {
	return int(memstats.next_gc)
}

func Setheap(n int) {
	heapminimum = uint64(n)
}
