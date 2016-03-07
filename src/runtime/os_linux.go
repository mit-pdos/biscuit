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
func fs_null()
func gs_null()
func cli()
func sti()
func ltr(int)
func lap_id() int

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
func alloc_map(uintptr, uint, int)

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
func _pnum(n uint) {
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
func pancake(msg *int8, addr uint) {
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
func G_pancake(msg string, addr uint) {
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
func chkalign(_p unsafe.Pointer, n uint) {
	p := uint(uintptr(_p))
	if p & (n - 1) != 0 {
		G_pancake("not aligned", n)
	}
}

//go:nosplit
func chksize(n uintptr, exp uintptr) {
	if n != exp {
		G_pancake("size mismatch", uint(n))
	}
}

type pdesc_t struct {
	limit	uint16
	addr	[8]uint8
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
)

var _pgdt pdesc_t

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
func tss_set(id int, rsp, nmi uintptr) *tss_t {
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

// maps cpu number to TSS segment descriptor in the GDT
//go:nosplit
func segnum(cpunum int) int {
	return 7 + 2*cpunum
}

//go:nosplit
func tss_seginit(cpunum int, _tssaddr *tss_t, lim uintptr) {
	seg := &_segs[segnum(cpunum)]
	seg.rest |= P | TSS | G

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
func tss_init(cpunum int) uintptr {
	intstk := uintptr(0xa100001000 + cpunum*4*PGSIZE)
	nmistk := uintptr(0xa100003000 + cpunum*4*PGSIZE)
	// BSP maps AP's stack for them
	if cpunum == 0 {
		alloc_map(intstk - 1, PTE_W, 1)
		alloc_map(nmistk - 1, PTE_W, 1)
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
	addr := uintptr(_addr)
	pd.limit = uint16(lim)
	for i := uint(0); i < 8; i++ {
		pd.addr[i] = uint8((addr >> (i*8)) & 0xff)
	}
}

//go:nosplit
func dur(_p unsafe.Pointer, sz uintptr) {
	for i := uintptr(0); i < sz; i++ {
		p := (*uint8)(unsafe.Pointer(uintptr(_p) + i))
		_pnum(uint(*p))
	}
}

//go:nosplit
func segsetup() *pdesc_t {
	chksize(unsafe.Sizeof(_pgdt), 10)
	chksize(unsafe.Sizeof(seg64_t{}), 8)
	chkalign(unsafe.Pointer(&_pgdt), 8)
	pdsetup(&_pgdt, unsafe.Pointer(&_segs[0]), unsafe.Sizeof(_segs) - 1)
	return &_pgdt
}

//go:nosplit
func fs0init(tls0 uintptr) {
	// elf tls defines user tls at -16(%fs)
	tlsaddr := int(tls0 + 16)
	// we must set fs/gs, the only segment descriptors in ia32e mode, at
	// least once before we use the MSRs to change their base address. the
	// MSRs write directly to hidden segment descriptor cache, and if we
	// don't explicitly fill the segment descriptor cache, the writes to
	// the MSRs are thrown out (presumably because the caches are thought
	// to be invalid).
	fs_null()
	ia32_fs_base := 0xc0000100
	Wrmsr(ia32_fs_base, tlsaddr)
}

const (
	PGSIZE	= 1 << 12
	PTE_P	= 1 << 0
	PTE_W	= 1 << 1
	PTE_U	= 1 << 2
	PTE_PCD	= 1 << 4
)
