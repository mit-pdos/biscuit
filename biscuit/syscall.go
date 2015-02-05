package main

import "fmt"
import "runtime"
import "unsafe"

const TFSIZE       int = 23
const TFREGS       int = 16
const TF_R8        int = 8
const TF_RSI       int = 10
const TF_RDI       int = 11
const TF_RDX       int = 12
const TF_RCX       int = 13
const TF_RAX       int = 15
const TF_TRAP      int = TFREGS
const TF_RIP       int = TFREGS + 2
const TF_RSP       int = TFREGS + 5
const TF_RFLAGS    int = TFREGS + 4

const EFAULT       int = 14
const ENOSYS       int = 38

const SYS_WRITE    int = 1

func syscall(pid int, tf *[TFSIZE]int) {

	p := allprocs[pid]
	trap := tf[TF_RAX]
	a1 := tf[TF_RDI]
	a2 := tf[TF_RSI]
	a3 := tf[TF_RDX]
	//a4 := tf[TF_RCX]
	//a5 := tf[TF_R8]

	ret := -ENOSYS
	switch trap {
	case SYS_WRITE:
		ret = sys_write(p, a1, a2, a3)
	}

	tf[TF_RAX] = ret
	runtime.Procrunnable(pid, tf)
}

func sys_write(proc *proc_t, fd int, bufp int, c int) int {

	if c == 0 {
		return 0
	}

	if !is_mapped(proc.pmap, bufp, c) {
		fmt.Printf("%#x not mapped\n", bufp)
		return -EFAULT
	}

	if fd != 1 && fd != 2 {
		panic("no imp")
	}

	vtop := func(va int) int {
		pte := pmap_walk(proc.pmap, va, false, 0, nil)
		ret := *pte & PTE_ADDR
		ret += va & PGOFFSET
		return ret
	}

	utext := int8(0x17)
	cnt := 0
	for ; cnt < c; {
		p_bufp := vtop(bufp + cnt)
		p := dmap8(p_bufp)
		left := c - cnt
		if len(p) > left {
			p = p[:left]
		}
		for _, c := range p {
			runtime.Putcha(int8(c), utext)
		}
		cnt += len(p)
	}
	runtime.Putcha('\n', utext)
	return c
}

type elf_t struct {
	data	*[]uint8
	len	int
}

type elf_phdr struct {
	etype   int
	flags   int
	vaddr   int
	filesz  int
	memsz   int
	sdata    []uint8
}

var ELF_QUARTER = 2
var ELF_HALF = 4
var ELF_OFF = 8
var ELF_ADDR = 8
var ELF_XWORD = 8

func readn(a []uint8, n int, off int) int {
	ret := 0
	for i := 0; i < n; i++ {
		ret |= int(a[off + i]) << uint(8*i)
	}
	return ret
}

func (e *elf_t) npheaders() int {
	mag := readn(*e.data, ELF_HALF, 0)
	if mag != 0x464c457f {
		pancake("bad elf magic", mag)
	}
	e_phnum := 0x38
	return readn(*e.data, ELF_QUARTER, e_phnum)
}

func (e *elf_t) header(c int, ret *elf_phdr) {
	if ret == nil {
		panic("nil elf_t")
	}

	nph := e.npheaders()
	if c >= nph {
		pancake("bad elf header", c)
	}
	d := *e.data
	e_phoff := 0x20
	e_phentsize := 0x36
	hoff := readn(d, ELF_OFF, e_phoff)
	hsz  := readn(d, ELF_QUARTER, e_phentsize)

	p_type   := 0x0
	p_flags  := 0x4
	p_offset := 0x8
	p_vaddr  := 0x10
	p_filesz := 0x20
	p_memsz  := 0x28
	f := func(w int, sz int) int {
		return readn(d, sz, hoff + c*hsz + w)
	}
	ret.etype = f(p_type, ELF_HALF)
	ret.flags = f(p_flags, ELF_HALF)
	ret.vaddr = f(p_vaddr, ELF_ADDR)
	ret.filesz = f(p_filesz, ELF_XWORD)
	ret.memsz = f(p_memsz, ELF_XWORD)
	off := f(p_offset, ELF_OFF)
	if off < 0 || off >= len(d) {
		panic(fmt.Sprintf("weird off %v", off))
	}
	if ret.filesz < 0 || off + ret.filesz >= len(d) {
		panic(fmt.Sprintf("weird filesz %v", ret.filesz))
	}
	rd := d[off:off + ret.filesz]
	ret.sdata = rd
}

func (e *elf_t) headers() []elf_phdr {
	num := e.npheaders()
	ret := make([]elf_phdr, num)
	for i := 0; i < num; i++ {
		e.header(i, &ret[i])
	}
	return ret
}

func (e *elf_t) entry() int {
	e_entry := 0x18
	return readn(*e.data, ELF_ADDR, e_entry)
}

func elf_segload(p *proc_t, hdr *elf_phdr) {
	perms := PTE_U
	//PF_X := 1
	PF_W := 2
	if hdr.flags & PF_W != 0 {
		perms |= PTE_W
	}
	sz := roundup(hdr.vaddr + hdr.memsz, PGSIZE)
	sz -= hdr.vaddr
	rsz := hdr.memsz
	for i := 0; i < sz; i += PGSIZE {
		// go allocator zeros all pages for us, thus bss is already
		// initialized
		pg, p_pg := pg_new(p.pages)
		if len(hdr.sdata) > 0 {
			dst := unsafe.Pointer(pg)
			src := unsafe.Pointer(&hdr.sdata[i])
			len := PGSIZE
			left := rsz - i
			if len > left {
				len = left
			}
			runtime.Memmove(dst, src, len)
		}
		p.page_insert(hdr.vaddr + i, pg, p_pg, perms, true)
	}
}

func elf_load(p *proc_t, e *elf_t) {
	PT_LOAD := 1
	for _, hdr := range e.headers() {
		// XXX get rid of worthless user program segments
		if hdr.etype == PT_LOAD && hdr.vaddr >= 0xf1000000 {
			elf_segload(p, &hdr)
		}
	}
}

func sys_test_dump() {
	e := allbins["user/hello"]
	for i, hdr := range e.headers() {
		fmt.Printf("%v -- vaddr %x filesz %x ", i, hdr.vaddr, hdr.filesz)
	}
}

func sys_test() {
	fmt.Printf("add 'user' prog\n")

	var tf [23]int
	tfregs    := 16
	tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	tf_rflags := tfregs + 4
	fl_intf   := 1 << 9
	tf_ss     := tfregs + 6
	tf_cs     := tfregs + 3

	proc := proc_new("test")

	elf := allbins["user/hello"]

	stack, p_stack := pg_new(proc.pages)
	stackva := 0xf4000000
	tf[tf_rsp] = stackva - 8
	tf[tf_rip] = elf.entry()
	tf[tf_rflags] = fl_intf

	ucseg := 4
	udseg := 5
	tf[tf_cs] = ucseg << 3 | 3
	tf[tf_ss] = udseg << 3 | 3

	// copy kernel page table, map new stack
	upmap, p_upmap := copy_pmap(kpmap(), proc.pages)
	proc.pmap, proc.p_pmap = upmap, p_upmap
	proc.page_insert(stackva - PGSIZE, stack,
	    p_stack, PTE_U | PTE_W, true)

	elf_load(proc, elf)

	// since kernel and user programs share pml4[0], need to mark shared
	// pages user
	pmap_cperms(upmap, elf.entry(), PTE_U)
	pmap_cperms(upmap, stackva, PTE_U)

	// VGA too
	pmap_cperms(upmap, 0xb8000, PTE_U)
	pmap_cperms(upmap, 0xb8000, PTE_U)

	runtime.Procadd(&tf, proc.pid, p_upmap)
}
