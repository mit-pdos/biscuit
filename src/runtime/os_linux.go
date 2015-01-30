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
func Memmove(unsafe.Pointer, unsafe.Pointer, int)
func Outb(int32, int32)
func Pnum(int)
func Procadd(tf *[23]int, uc int, p_pmap int)
func Proccontinue()
func Prockill(int)
func Procrunnable(int)
func Procyield()
func Rcr2() int
func Rrsp() int
func Sgdt(*int)
func Sidt(*int)
func Sti()
func Vtop(*[512]int) int

func Newlines(int64)
func Fnaddr(func()) int
func Fnaddri(func(int)) int
func Turdyprog()
