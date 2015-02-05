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

func Ap_setup(int)
func Cli()
func Install_traphandler(func(tf *[23]int, uc int))
func Invlpg(unsafe.Pointer)
func Kpmap() *[512]int
func Kpmap_p() int
func Lcr3(int)
func Memmove(unsafe.Pointer, unsafe.Pointer, int)
func Outb(int32, int32)
func Pnum(int)
func Procadd(tf *[23]int, uc int, p_pmap int)
func Proccontinue()
func Prockill(int)
func Procrunnable(int, *[23]int)
func Procyield()
func Rcr2() int
func Rcr3() int
func Rrsp() int
func Sgdt(*int)
func Sidt(*int)
func Sti()
func Vtop(*[512]int) int

func Newlines(int64)
func Fnaddr(func()) int
func Fnaddri(func(int)) int
func Turdyprog()
func Stackdump(int)

func inb(int) int
func outb(int, int)

//go:nosplit
func sc_setup() {
	com1  := 0x3f8
	data  := 0
	intr  := 1
	ififo := 2
	lctl  := 3
	mctl  := 4

	outb(com1 + intr, 0x00)
	outb(com1 + lctl, 0x80)
	outb(com1 + data, 0x03)
	outb(com1 + intr, 0x00)
	outb(com1 + lctl, 0x03)
	outb(com1 + ififo, 0xc7)
	outb(com1 + mctl, 0x0b)
}

//go:nosplit
func sc_put(c int8) {
	com1 := 0x3f8
	lstatus := 5
	for inb(com1 + lstatus) & 0x20 == 0 {
	}
	outb(com1, int(c))
}

type put_t struct {
	vx	int
	vy	int
}

var put put_t

//go:nosplit
func vga_put(c int8, attr int8) {
	if c != '\n' {
		p := (*[1999]int16)(unsafe.Pointer(uintptr(0xb8000)))
		a := int16(attr) << 8
		v := a | int16(c)
		p[put.vy * 80 + put.vx] = v
		put.vx++
	} else {
		put.vx = 0
		put.vy++
	}
	if put.vx >= 79 {
		put.vx = 0
		put.vy++
	}

	if put.vy >= 25 {
		put.vy = 0
	}
}

//go:nosplit
func putch(c int8) {
	vga_put(c, 0x7)
	sc_put(c)
}

func Putcha(c int8, a int8) {
	vga_put(c, a)
	sc_put(c)
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
