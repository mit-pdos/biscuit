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

func Cli()
func Copy_pgt(*[512]int) *[512]int
func Install_traphandler(func(tf *[23]uint64, uc int))
func Install_trapstub(func())
func Kpgdir() *[512]int
func Lapic_eoi()
func Pgdir_walk(va uintptr) int
func Pnum(m uint64)
func Sti()
func Trapret(tf *[23]uint64)
func Useradd(tf *[23]uint64, uc int, pgtbl *[512]int)
func Usercontinue()
func Userrunnable()
func Useryield()

func Death()
func Fnaddr(func()) uint64
func Newstack() uint64
func Turdyprog()
