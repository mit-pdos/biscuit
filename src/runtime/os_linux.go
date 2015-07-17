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
type Ptid_t uint
func Install_traphandler(func(tf *[23]int, ptid Ptid_t, notify int))
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
func Procadd(ptid Ptid_t, tf *[23]int, p_pmap int)
func Proccontinue()
func Prockill(Ptid_t)
func Procnotify(Ptid_t) int
func Procrunnable(ptid Ptid_t, tf *[23]int, p_pmap int)
func Procyield()
func Rdtsc() uint64
func Rcr2() int
func Rcr3() int
func Rrsp() int
func Sgdt(*int)
func Sidt(*int)
func Sti()
func Tlbadmit(int, int, int, int)
func Vtop(*[512]int) int

func Crash()
func Fnaddr(func()) int
func Fnaddri(func(int)) int
func Tfdump(*[23]int)
func Stackdump(int)
func Usleep(int)
func Rflags() int
func Resetgcticks() uint64
func Gcticks() uint64
func Trapwake()

func inb(int) int
// os_linux.c
var gcticks uint64

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
