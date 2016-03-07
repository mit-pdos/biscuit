// Copyright 2014 The Go Authors.  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import "unsafe"

func futex(addr unsafe.Pointer, op int32, val uint32, ts, addr2 unsafe.Pointer, val3 uint32) int32
func clone(flags int32, stk, mm, gg, fn unsafe.Pointer) int32
func rt_sigaction(sig uintptr, new, old unsafe.Pointer, size uintptr) int32
func sigaltstack(new, old unsafe.Pointer)
func setitimer(mode int32, new, old unsafe.Pointer)
func rtsigprocmask(sig int32, new, old unsafe.Pointer, size int32)
func getrlimit(kind int32, limit unsafe.Pointer) int32
func raise(sig int32)
func sched_getaffinity(pid, len uintptr, buf *uintptr) int32
func trapsched_m(*g)
func trapinit_m(*g)

func Ap_setup(int)
func Cli()
func Cpuid(uint32, uint32) (uint32, uint32, uint32, uint32)
func Install_traphandler(func(tf *[24]int))
func Invlpg(unsafe.Pointer)
func Kpmap() *[512]int
func Kpmap_p() int
func Lcr3(uintptr)
func Nanotime() int
func Inb(int) int
func Inl(int) int
func Insl(int, unsafe.Pointer, int)
func Outb(int, int)
func Outw(int, int)
func Outl(int, int)
func Outsl(int, unsafe.Pointer, int)
func Pmsga(*uint8, int, int8)
func Pnum(int)
func Kreset()
func Ktime() int
func Rdtsc() uint64
func Rcr2() uintptr
func Rcr3() uintptr
func Rcr4() uintptr
func Rrsp() int
func Sgdt(*uintptr)
func Sidt(*uintptr)
func Sti()
func Tlbadmit(int, int, int, int) uint
func Tlbwait(uint)
func Vtop(*[512]int) int

func Crash()
func Fnaddr(func()) uintptr
func Fnaddri(func(int)) uintptr
func Tfdump(*[24]int)
func Rflags() int
func Resetgcticks() uint64
func Gcticks() uint64
func Trapwake()

func inb(int) int
func htpause()
func finit()
func fs_null()
func fxsave(*[fxwords]uintptr)
func gs_null()
func lgdt(pdesc_t)
func lidt(pdesc_t)
func cli()
func sti()
func ltr(uint)
func lap_id() int
func rcr0() uintptr
func rcr4() uintptr
func lcr0(uintptr)
func lcr4(uintptr)

// os_linux.c
var gcticks uint64
var No_pml4 int

// we have to carefully write go code that may be executed early (during boot)
// or in interrupt context. such code cannot allocate or call functions that
// that have the stack splitting prologue. the following is a list of go code
// that could result in code that allocates or calls a function with a stack
// splitting prologue.
// - function with interface argument (calls convT2E)
// - taking address of stack variable (may allocate)
// - using range to iterate over a string (calls stringiter*)

type cpu_t struct {
	this		uint
	mythread	uint
	rsp		uintptr
	num		uint
	pmap		*[512]int
	pms		[]*[512]int
	//pid		uintptr
}

var Cpumhz uint

const MAXCPUS int = 32

var cpus [MAXCPUS]cpu_t

type tuser_t struct {
	tf	uintptr
	fxbuf	uintptr
}

type prof_t struct {
	enabled		int
	totaltime	int
	stampstart	int
}

type thread_t struct {
	tf		[24]int
	fx		[64]int
	user		tuser_t
	sigtf		[24]int
	sigfx		[64]int
	sigstatus	int
	siglseepfor	int
	status		int
	doingsig	int
	sigstack	int
	prof		prof_t
	sleepfor	int
	sleepret	int
	futaddr		int
	pmap		int
	//_pad		int
}

const(
  TFSIZE       = 24
  TFREGS       = 17
  TF_SYSRSP    = 0
  TF_FSBASE    = 1
  TF_R8        = 9
  TF_RBP       = 10
  TF_RSI       = 11
  TF_RDI       = 12
  TF_RDX       = 13
  TF_RCX       = 14
  TF_RBX       = 15
  TF_RAX       = 16
  TF_TRAP      = TFREGS
  TF_RIP       = TFREGS + 2
  TF_CS        = TFREGS + 3
  TF_RSP       = TFREGS + 5
  TF_SS        = TFREGS + 6
  TF_RFLAGS    = TFREGS + 4
    TF_FL_IF     = 1 << 9
)

var Pspercycle uint

func Pushcli() int
func Popcli(int)
func Gscpu() *cpu_t
func Rdmsr(int) int
func Wrmsr(int, int)
func _Userrun(*[24]int, bool) (int, int)

func Cprint(byte, int)

func Userrun(tf *[24]int, fxbuf *[64]int, pmap *[512]int, p_pmap uintptr,
    pms []*[512]int, fastret bool) (int, int) {

	// {enter,exit}syscall() may not be worth the overhead. i believe the
	// only benefit for biscuit is that cpus running in the kernel could GC
	// while other cpus execute user programs.
	entersyscall()
	fl := Pushcli()
	ct := (*thread_t)(unsafe.Pointer(uintptr(Gscpu().mythread)))

	// set shadow pointers for user pmap so it isn't free'd out from under
	// us if the process terminates soon
	cpu := Gscpu()
	cpu.pmap = pmap
	cpu.pms = pms
	//cpu.pid = uintptr(pid)
	if Rcr3() != p_pmap {
		Lcr3(p_pmap)
	}

	// if doing a fast return after a syscall, we need to restore some user
	// state manually
	ia32_fs_base := 0xc0000100
	kfsbase := Rdmsr(ia32_fs_base)
	Wrmsr(ia32_fs_base, tf[TF_FSBASE])

	// we only save/restore SSE registers on cpu exception/interrupt, not
	// during syscall exit/return. this is OK since sys5ABI defines the SSE
	// registers to be caller-saved.
	ct.user.tf = uintptr(unsafe.Pointer(tf))
	ct.user.fxbuf = uintptr(unsafe.Pointer(fxbuf))
	intno, aux := _Userrun(tf, fastret)

	Wrmsr(ia32_fs_base, kfsbase)
	ct.user.tf = 0
	ct.user.fxbuf = 0
	Popcli(fl)
	exitsyscall()
	return intno, aux
}

// caller must have interrupts cleared
//go:nosplit
func shadow_clear() {
	cpu := Gscpu()
	cpu.pmap = nil
	cpu.pms = nil
}

type nmiprof_t struct {
	buf		[]uintptr
	bufidx		uint64
	LVTmask		bool
	evtsel		int
	evtmin		uint
	evtmax		uint
}

var _nmibuf [4096]uintptr
var nmiprof = nmiprof_t{buf: _nmibuf[:]}

func SetNMI(mask bool, evtsel int, min, max uint) {
	nmiprof.LVTmask = mask
	nmiprof.evtsel = evtsel
	nmiprof.evtmin = min
	nmiprof.evtmax = max
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
	idx := int(xadd64(&nmiprof.bufidx, int64(l)))
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
			Cprint('!', 1)
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
	idx := xadd64(&nmiprof.bufidx, 1) - 1
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

	if en {
		// disable perf counter before clearing
		Wrmsr(ia32_perfevtsel0, 0)

		// clear overflow
		Wrmsr(ia32_perf_global_ovf_ctrl, 1)

		r := dumrand(nmiprof.evtmin, nmiprof.evtmax)
		Wrmsr(ia32_pmc0, -int(r))

		freeze_pmc_on_pmi := 1 << 12
		Wrmsr(ia32_debugctl, freeze_pmc_on_pmi)
		// cpu clears global_ctrl on PMI if freeze-on-pmi is set.
		// re-enable
		Wrmsr(ia32_global_ctrl, 1)

		v := nmiprof.evtsel
		Wrmsr(ia32_perfevtsel0, v)
	} else {
		Wrmsr(ia32_perfevtsel0, 0)
	}

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

const com1 = 0x3f8
const lstatus = 5

//go:nosplit
func sc_put_(c int8) {
	for inb(com1 + lstatus) & 0x20 == 0 {
	}
	Outb(com1, int(c))
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

func Trapsched() {
	mcall(trapsched_m)
}

// called only once to setup
func Trapinit() {
	mcall(trapinit_m)
}

// G_ prefix means a function had to have both C and Go versions while the
// conversion is underway. remove prefix afterwards. we need two versions of
// functions that take a string as an argument since string literals are
// different data types in C and Go.
//
// XXX XXX many of these do not need nosplits!
var Halt uint32

// TEMPORARY CRAP
func _pmsg(*int8)
func invlpg(uintptr)

// wait until remove definition from proc.c
//type spinlock_t struct {
//	v	uint32
//}

//go:nosplit
func splock(l *spinlock_t) {
	for {
		if xchg(&l.v, 1) == 0 {
			break
		}
		for l.v != 0 {
			htpause()
		}
	}
}

//go:nosplit
func spunlock(l *spinlock_t) {
	atomicstore(&l.v, 0)
}

// since this lock may be taken during an interrupt (only under fatal error
// conditions), interrupts must be cleared before attempting to take this lock.
var pmsglock = &spinlock_t{}

// msg must be utf-8 string
//go:nosplit
func G_pmsg(msg string) {
	putch(' ');
	// can't use range since it results in calls stringiter2 which has the
	// stack splitting proglogue
	for i := 0; i < len(msg); i++ {
		putch(int8(msg[i]))
	}
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

//go:nosplit
func pancake(msg *int8, addr uintptr) {
	cli()
	atomicstore(&Halt, 1)
	_pmsg(msg)
	_pnum(addr)
	G_pmsg("PANCAKE")
	for {
		p := (*uint16)(unsafe.Pointer(uintptr(0xb8002)))
		*p = 0x1400 | 'F'
	}
}
//go:nosplit
func G_pancake(msg string, addr uintptr) {
	cli()
	atomicstore(&Halt, 1)
	G_pmsg(msg)
	_pnum(addr)
	G_pmsg("PANCAKE")
	for {
		p := (*uint16)(unsafe.Pointer(uintptr(0xb8002)))
		*p = 0x1400 | 'F'
	}
}


//go:nosplit
func chkalign(_p unsafe.Pointer, n uintptr) {
	p := uintptr(_p)
	if p & (n - 1) != 0 {
		G_pancake("not aligned", p)
	}
}

//go:nosplit
func chksize(n uintptr, exp uintptr) {
	if n != exp {
		G_pancake("size mismatch", n)
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
	cpus[lap_id()].rsp = rsp
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

//go:nosplit
func seg_setup() {
	p := pdesc_t{}
	chksize(unsafe.Sizeof(seg64_t{}), 8)
	pdsetup(&p, unsafe.Pointer(&_segs[0]), unsafe.Sizeof(_segs) - 1)
	lgdt(p)

	// now that we have a GDT, setup tls for the first thread.
	// elf tls specification defines user tls at -16(%fs)
	t := uintptr(unsafe.Pointer(&tls0[0]))
	tlsaddr := int(t + 16)
	// we must set fs/gs at least once before we use the MSRs to change
	// their base address. the MSRs write directly to hidden segment
	// descriptor cache, and if we don't explicitly fill the segment
	// descriptor cache, the writes to the MSRs are thrown out (presumably
	// because the caches are thought to be invalid).
	fs_null()
	ia32_fs_base := 0xc0000100
	Wrmsr(ia32_fs_base, tlsaddr)
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
	kcode64 := 1

	p := &_idt[idx]
	p.baselow = uint16(entry)
	p.basemid = uint16(entry >> 16)
	p.basehi = uint32(entry >> 32)

	p.segsel = uint16(kcode64 << 3)

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
	// recursive mapping
	VREC		uintptr = 0x42
	// available mapping
	VTEMP		uintptr = 0x43
)

//go:nosplit
func pml4x(va uintptr) uintptr {
	return (va >> 39) & 0x1ff
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

// XXX XXX XXX get rid of create
//go:nosplit
func pgdir_walk(_va uintptr, create bool) *uintptr {
	v := pgrounddown(_va)
	if v == 0 && create {
		G_pancake("map zero pg", _va)
	}
	slot0 := pml4x(v)
	if slot0 == VREC {
		G_pancake("map in VREC", _va)
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
		G_pancake("vtemp in use", *pml4)
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
	sec := (*secret_t)(unsafe.Pointer(uintptr(0x7c00)))
	found := false
	base := sec.e820p
	// bootloader provides 15 e820 entries at most (it panicks if the PC
	// provides more).
	for i := uintptr(0); i < 15; i++ {
		ep := (*e820_t)(unsafe.Pointer(base + i*28))
		if ep.len == 0 {
			continue
		}
		endpg := ep.start + ep.len
		if pgfirst >= ep.start && pgfirst < endpg {
			pglast = endpg
			found = true
			break
		}
	}
	if !found {
		G_pancake("e820 problems", pgfirst)
	}
	if pgfirst & PGOFFMASK != 0 {
		G_pancake("pgfist not aligned", pgfirst)
	}
}

//go:nosplit
func get_pg() uintptr {
	if pglast == 0 {
		phys_init()
	}
	pgfirst = skip_bad(pgfirst)
	if pgfirst >= pglast {
		G_pancake("oom", pglast)
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
		G_pancake("expected empty pte", old)
	}
	p_pg := get_pg()
	zero_phys(p_pg)
	// XXX goodbye, memory
	*pte = p_pg | perms | PTE_P
	if old & PTE_P != 0 {
		invlpg(va)
	}
}

const fxwords = 512/8
var fxinit [fxwords]uintptr

//go:nosplit
func fpuinit(amfirst bool) {
	finit()
	cr0 := rcr0()
	// clear EM
	cr0 &^= (1 << 2)
	// set MP
	cr0 |= 1 << 1
	lcr0(cr0);

	cr4 := rcr4()
	// set OSFXSR
	cr4 |= 1 << 9
	lcr4(cr4);

	if amfirst {
		chkalign(unsafe.Pointer(&fxinit[0]), 16)
		fxsave(&fxinit)

		// XXX XXX XXX XXX XXX XXX XXX dont forget to do this once
		// thread code is converted to go
		G_pmsg("VERIFY FX FOR THREADS\n")
	}
}
