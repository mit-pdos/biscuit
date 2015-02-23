package main

import "fmt"
import "runtime"
import "unsafe"

const(
  TFSIZE       = 23
  TFREGS       = 16
  TF_R8        = 8
  TF_RSI       = 10
  TF_RDI       = 11
  TF_RDX       = 12
  TF_RCX       = 13
  TF_RBX       = 14
  TF_RAX       = 15
  TF_TRAP      = TFREGS
  TF_RIP       = TFREGS + 2
  TF_RSP       = TFREGS + 5
  TF_RFLAGS    = TFREGS + 4
)

const(
  ENOENT       = 2
  EBADF        = 9
  EFAULT       = 14
  EEXIST       = 17
  ENOTDIR      = 20
  EINVAL       = 22
  ENAMETOOLONG = 36
  ENOSYS       = 38
)

const(
  SYS_READ     = 0
  SYS_WRITE    = 1
  SYS_OPEN     = 2
    O_RDONLY      = 0
    O_WRONLY      = 1
    O_RDWR        = 2
    O_CREAT       = 0x80
    O_APPEND      = 0x400
  SYS_GETPID   = 39
  SYS_FORK     = 57
  SYS_EXIT     = 60
  SYS_MKDIR    = 83
)

// lowest userspace address
const USERMIN      int = 0xf1000000

// story for concurrent system calls: any syscall that touches any proc_t, p,
// which is not the calling process, needs to lock p. access to globals (maps)
// also need to be locked. obviously, a process cannot have two or more
// syscalls/pagefaults running concurrently since syscalls/pagefaults are not
// batched.
func syscall(pid int, tf *[TFSIZE]int) {

	p := proc_get(pid)
	trap := tf[TF_RAX]
	a1 := tf[TF_RDI]
	a2 := tf[TF_RSI]
	a3 := tf[TF_RDX]
	//a4 := tf[TF_RCX]
	//a5 := tf[TF_R8]

	ret := -ENOSYS
	switch trap {
	case SYS_READ:
		ret = sys_read(p, a1, a2, a3)
	case SYS_WRITE:
		ret = sys_write(p, a1, a2, a3)
	case SYS_OPEN:
		ret = sys_open(p, a1, a2, a3)
	case SYS_GETPID:
		ret = sys_getpid(p)
	case SYS_FORK:
		ret = sys_fork(p, tf)
	case SYS_EXIT:
		sys_exit(p, a1)
	case SYS_MKDIR:
		ret = sys_mkdir(p, a1, a2)
	}

	tf[TF_RAX] = ret
	if !p.dead {
		runtime.Procrunnable(pid, tf)
	}
}

func sys_read(proc *proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	if !is_mapped(proc.pmap, bufp, sz) {
		fmt.Printf("%#x not mapped\n", bufp)
		return -EFAULT
	}
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	vtop := func(va int) int {
		pte := pmap_walk(proc.pmap, va, false, 0, nil)
		ret := *pte & PTE_ADDR
		ret += va & PGOFFSET
		return ret
	}
	fd.file.filelock()
	defer fd.file.fileunlock()
	c := 0
	for c < sz {
		dst := dmap8(vtop(bufp + c))
		left := sz - c
		if len(dst) > left {
			dst = dst[:left]
		}
		ret, err := fs_read(dst, fd.file, fd.offset + c)
		if err != 0 {
			return err
		}
		c += ret
		if ret != len(dst) {
			// no more to read
			break
		}
	}
	fd.offset += c
	return c
}

func sys_write(proc *proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	if !is_mapped(proc.pmap, bufp, sz) {
		fmt.Printf("%#x not mapped\n", bufp)
		return -EFAULT
	}
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	vtop := func(va int) int {
		pte := pmap_walk(proc.pmap, va, false, 0, nil)
		ret := *pte & PTE_ADDR
		ret += va & PGOFFSET
		return ret
	}
	wrappy := fs_write
	// stdout/stderr hack
	if fd == &fd_stdout || fd == &fd_stderr {
		wrappy = func(p []uint8, f *file_t, off int,
		    app bool) (int,int) {
			utext := int8(0x17)
			for _, c := range p {
				runtime.Putcha(int8(c), utext)
			}
			return len(p), 0
		}
	}
	fd.file.filelock()
	defer fd.file.fileunlock()
	append := fd.perms & O_APPEND != 0
	c := 0
	for c < sz {
		p_bufp := vtop(bufp + c)
		src := dmap8(p_bufp)
		left := sz - c
		if len(src) > left {
			src = src[:left]
		}
		ret, err := wrappy(src, fd.file, fd.offset + c, append)
		if err != 0 {
			return err
		}
		c += ret
		if ret != len(src) {
			// short write
			break
		}
	}
	fd.offset += c
	return c
}

func sys_open(proc *proc_t, pathn int, flags int, mode int) int {
	path, ok, toolong := is_mapped_str(proc.pmap, pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	temp := flags & (O_RDONLY | O_WRONLY | O_RDWR)
	if temp != O_RDONLY && temp != O_WRONLY && temp != O_RDWR {
		return -EINVAL
	}
	parts, badp := path_sanitize(proc.cwd + path)
	if badp {
		return -ENOENT
	}
	file, err := fs_open(parts, flags, mode)
	if err != 0 {
		return err
	}
	fdn, fd := proc.fd_new()
	fd.perms = temp
	switch {
	case flags & O_APPEND != 0:
		fd.perms |= O_APPEND
	}
	fd.file = file
	return fdn
}

func sys_mkdir(proc *proc_t, pathn int, mode int) int {
	path, ok, toolong := is_mapped_str(proc.pmap, pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	parts, badp := path_sanitize(proc.cwd + path)
	if badp {
		return -ENOENT
	}
	return fs_mkdir(parts, mode)
}

func sys_getpid(proc *proc_t) int {
	return proc.pid
}

func sys_fork(parent *proc_t, ptf *[TFSIZE]int) int {

	child := proc_new(fmt.Sprintf("%s's child", parent.name))

	// mark writable entries as read-only and cow
	mk_cow := func(pte int) (int, int) {

		// don't mess with or track kernel pages
		if pte & PTE_U == 0 {
			return pte, pte
		}
		if pte & PTE_W != 0 {
			pte = (pte | PTE_COW) & ^PTE_W
		}

		// reference the mapped page in child's tracking maps too
		p_pg := pte & PTE_ADDR
		pg, ok := parent.pages[p_pg]
		if !ok {
			panic(fmt.Sprintf("parent not tracking " +
			    "page %#x", p_pg))
		}
		upg, ok := parent.upages[p_pg]
		if !ok {
			panic(fmt.Sprintf("parent not tracking " +
			    "upage %#x", p_pg))
		}
		child.pages[p_pg] = pg
		child.upages[p_pg] = upg
		return pte, pte
	}
	pmap, p_pmap, _ := copy_pmap(mk_cow, parent.pmap, child.pages)

	// tlb invalidation is not necessary for the parent because its pmap
	// cannot be in use now (well, it may be in use on the CPU that took
	// the syscall interrupt which may not have switched pmaps yet, but
	// that CPU will not touch these invalidated user-space addresses).

	child.pmap = pmap
	child.p_pmap = p_pmap

	chtf := [TFSIZE]int{}
	chtf = *ptf
	chtf[TF_RAX] = 0

	child.sched_add(&chtf)

	return child.pid
}

func sys_pgfault(proc *proc_t, pte *int, faultaddr int, tf *[TFSIZE]int) {
	// copy page
	dst, p_dst := pg_new(proc.pages)
	p_src := *pte & PTE_ADDR
	src := dmap(p_src)
	for i, c := range src {
		dst[i] = c
	}

	// insert new page into pmap
	va := faultaddr & PGMASK
	perms := (*pte & PTE_FLAGS) & ^PTE_COW
	perms |= PTE_W
	proc.page_insert(va, dst, p_dst, perms, false)

	// set process as runnable again
	runtime.Procrunnable(proc.pid, nil)
}

func sys_exit(proc *proc_t, status int) {
	fmt.Printf("%v exited with status %v\n", proc.name, status)
	proc_kill(proc.pid)
}

type elf_t struct {
	data	[]uint8
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
var ELF_HALF    = 4
var ELF_OFF     = 8
var ELF_ADDR    = 8
var ELF_XWORD   = 8

func readn(a []uint8, n int, off int) int {
	ret := 0
	for i := 0; i < n; i++ {
		ret |= int(a[off + i]) << (uint(i)*8)
	}
	return ret
}

func writen(a []uint8, n int, off int, val int) {
	v := uint(val)
	for i := 0; i < n; i++ {
		a[off + i] = uint8((v >> (uint(i)*8)) & 0xff)
	}
}

func (e *elf_t) npheaders() int {
	mag := readn(e.data, ELF_HALF, 0)
	if mag != 0x464c457f {
		panic("bad elf magic")
	}
	e_phnum := 0x38
	return readn(e.data, ELF_QUARTER, e_phnum)
}

func (e *elf_t) header(c int, ret *elf_phdr) {
	if ret == nil {
		panic("nil elf_t")
	}

	nph := e.npheaders()
	if c >= nph {
		panic("bad elf header")
	}
	d := e.data
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
	return readn(e.data, ELF_ADDR, e_entry)
}

func elf_segload(p *proc_t, hdr *elf_phdr) {
	perms := PTE_U
	//PF_X := 1
	PF_W := 2
	if hdr.flags & PF_W != 0 {
		perms |= PTE_W
	}
	sz := roundup(hdr.vaddr + hdr.memsz, PGSIZE)
	sz -= rounddown(hdr.vaddr, PGSIZE)
	rsz := hdr.filesz
	for i := 0; i < sz; i += PGSIZE {
		// go allocator zeros all pages for us, thus bss is already
		// initialized
		pg, p_pg := pg_new(p.pages)
		//pg, p_pg := pg_new(&p.pages)
		if i < len(hdr.sdata) {
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
		if hdr.etype == PT_LOAD && hdr.vaddr >= USERMIN {
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

func sys_test(program string) {
	fmt.Printf("add 'user' prog\n")

	var tf [23]int
	tfregs    := 16
	tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	tf_rflags := tfregs + 4
	fl_intf   := 1 << 9
	tf_ss     := tfregs + 6
	tf_cs     := tfregs + 3

	proc := proc_new(program + "test")

	elf, ok := allbins[program]
	if !ok {
		panic("no such program: " + program)
	}

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
	upmap, p_upmap, _ := copy_pmap(nil, kpmap(), proc.pages)
	proc.pmap, proc.p_pmap = upmap, p_upmap
	proc.page_insert(stackva - PGSIZE, stack,
	    p_stack, PTE_U | PTE_W, true)

	elf_load(proc, elf)

	// since kernel and user programs share pml4[0], need to mark shared
	// pages user
	pmap_cperms(upmap, elf.entry(), PTE_U)
	pmap_cperms(upmap, stackva, PTE_U)

	proc.sched_add(&tf)
}
