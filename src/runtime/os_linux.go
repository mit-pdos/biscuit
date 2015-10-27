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
func Lcr3(int)
func Memmove(unsafe.Pointer, unsafe.Pointer, int)
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
func Rcr2() int
func Rcr3() int
func Rrsp() int
func Sgdt(*int)
func Sidt(*int)
func Sti()
func Tlbadmit(int, int, int, int) uint
func Tlbwait(uint)
func Vtop(*[512]int) int

func Crash()
func Fnaddr(func()) int
func Fnaddri(func(int)) int
func Tfdump(*[24]int)
func Stackdump(int)
func Usleep(int)
func Rflags() int
func Resetgcticks() uint64
func Gcticks() uint64
func Trapwake()

func inb(int) int
// os_linux.c
var gcticks uint64
var No_pml4 int

type cpu_t struct {
	this		uint
	mythread	uint
	rsp		uint
	num		uint
	pmap		*[512]int
	pms		[]*[512]int
}

var cpus [32]cpu_t

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

func Pushcli() int
func Popcli(int)
func Gscpu() *cpu_t
func Rdmsr(int) int
func Wrmsr(int, int)
func _Userrun(*[24]int, bool) (int, int)

func Cprint(byte, int)

func Userrun(tf *[24]int, fxbuf *[64]int, pmap *[512]int, p_pmap int,
    pms []*[512]int, fastret bool) (int, int) {

	entersyscall()
	fl := Pushcli()
	ct := (*thread_t)(unsafe.Pointer(uintptr(Gscpu().mythread)))

	// set shadow pointers for user pmap so it isn't free'd out from under
	// us if the process terminates soon
	cpu := Gscpu()
	cpu.pmap = pmap
	cpu.pms = pms
	if Rcr3() != p_pmap {
		Lcr3(p_pmap)
	}

	// if doing a fast return after a syscall, we need to restore some user
	// state manually
	ia32_fs_base := 0xc0000100
	kfsbase := Rdmsr(ia32_fs_base)
	Wrmsr(ia32_fs_base, tf[TF_FSBASE])

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

//go:nosplit
func sc_setup() {
	com1  := 0x3f8
	data  := 0
	intr  := 1
	ififo := 2
	lctl  := 3
	mctl  := 4

	Outb(com1 + ififo, 0x0)
	Outb(com1 + lctl, 0x80)
	Outb(com1 + data, 115200/9600)
	Outb(com1 + intr, 0x0)
	Outb(com1 + lctl, 0x03)
	Outb(com1 + mctl, 0x0)
	Outb(com1 + intr, 0x01)

	inb(com1 + ififo)
	inb(com1 + data)
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
