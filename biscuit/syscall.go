package main

import "fmt"
import "runtime"
import "strings"
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
  TF_CS        = TFREGS + 3
  TF_RSP       = TFREGS + 5
  TF_SS        = TFREGS + 6
  TF_RFLAGS    = TFREGS + 4
    TF_FL_IF     = 1 << 9
)

const(
  EPERM        = 1
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
  SYS_CLOSE    = 3
  SYS_FSTAT    = 5
  SYS_GETPID   = 39
  SYS_FORK     = 57
  SYS_EXECV    = 59
  SYS_EXIT     = 60
  SYS_MKDIR    = 83
  SYS_LINK     = 86
  SYS_UNLINK   = 87
)

// lowest userspace address
const USERMIN	int = VUSER << 39

// story for concurrent system calls: any syscall that touches any proc_t, p,
// which is not the calling process, needs to lock p. access to globals (maps)
// also need to be locked. obviously, a process cannot have two or more
// syscalls/pagefaults running concurrently since syscalls/pagefaults are not
// batched.
func syscall(pid int, tf *[TFSIZE]int) {

	p := proc_get(pid)
	sysno := tf[TF_RAX]
	a1 := tf[TF_RDI]
	a2 := tf[TF_RSI]
	a3 := tf[TF_RDX]
	//a4 := tf[TF_RCX]
	//a5 := tf[TF_R8]

	ret := -ENOSYS
	switch sysno {
	case SYS_READ:
		ret = sys_read(p, a1, a2, a3)
	case SYS_WRITE:
		ret = sys_write(p, a1, a2, a3)
	case SYS_OPEN:
		ret = sys_open(p, a1, a2, a3)
	case SYS_CLOSE:
		ret = sys_close(p, a1)
	case SYS_FSTAT:
		ret = sys_fstat(p, a1, a2)
	case SYS_GETPID:
		ret = sys_getpid(p)
	case SYS_FORK:
		ret = sys_fork(p, tf)
	case SYS_EXECV:
		ret = sys_execv(p, a1, a2)
	case SYS_EXIT:
		sys_exit(p, a1)
	case SYS_MKDIR:
		ret = sys_mkdir(p, a1, a2)
	case SYS_LINK:
		ret = sys_link(p, a1, a2)
	case SYS_UNLINK:
		ret = sys_unlink(p, a1)
	default:
		fmt.Printf("unexpected syscall %v\n", sysno)
	}

	tf[TF_RAX] = ret
	if !p.dead {
		runtime.Procrunnable(pid, tf)
	}
}

func sys_read(proc *proc_t, fdn int, bufp int, sz int) int {
	// sys_read is read-only i.e. doesn't update metadata, thus don't need
	// op_{begin,end}
	if sz == 0 {
		return 0
	}
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	if fd.ftype != INODE && fd.ftype != CDEV {
		panic("no imp")
	}
	c := 0
	// we cannot load the user page map and the buffer to read into may not
	// be contiguous in physical memory. thus we must piece together the
	// buffer.
	dsts := make([][]uint8, 0)
	for c < sz {
		dst, ok := proc.userdmap(bufp + c)
		if !ok {
			return -EFAULT
		}
		left := sz - c
		if len(dst) > left {
			dst = dst[:left]
		}
		dsts = append(dsts, dst)
		c += len(dst)
	}

	// stdout/stderr hack
	if fd.ftype == CDEV {
		kdata := kbd_get(sz)
		ret := len(kdata)
		for _, dst := range dsts {
			ub := len(kdata)
			if ub > len(dst) {
				ub = len(dst)
			}
			for i := 0; i < ub; i++ {
				dst[i] = kdata[i]
			}
			kdata = kdata[ub:]
		}
		return ret
	}

	ret, err := fs_read(dsts, fd.file.priv, fd.offset)
	if err != 0 {
		return err
	}
	fd.offset += ret
	return ret
}

func sys_write(proc *proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	if fd.ftype != INODE && fd.ftype != CDEV {
		panic("no imp")
	}
	wrappy := fs_write
	// stdout/stderr hack
	if fd.ftype == CDEV {
		wrappy = func(srcs [][]uint8, priv inum, off int,
		    ap bool) (int, int) {
			// merge into one buffer to avoid taking the console
			// lock many times.
			utext := int8(0x17)
			big := make([]uint8, 0)
			for _, s := range srcs {
				big = append(big, s...)
			}
			runtime.Pmsga(&big[0], len(big), utext)
			return len(big), 0
		}
	}

	apnd := fd.perms & O_APPEND != 0
	c := 0
	srcs := make([][]uint8, 0)
	for c < sz {
		src, ok := proc.userdmap(bufp + c)
		if !ok {
			return -EFAULT
		}
		left := sz - c
		if len(src) > left {
			src = src[:left]
		}
		srcs = append(srcs, src)
		c += len(src)
	}
	ret, err := wrappy(srcs, fd.file.priv, fd.offset, apnd)
	if err != 0 {
		return err
	}
	fd.offset += ret
	return ret
}

func sys_open(proc *proc_t, pathn int, flags int, mode int) int {
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
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
	parts, err := path_sanitize(proc.cwd, path)
	if err != 0 {
		return err
	}
	file, err := fs_open(parts, flags, mode)
	if err != 0 {
		return err
	}
	fdn, fd := proc.fd_new(INODE)
	fd.perms = temp
	switch {
	case flags & O_APPEND != 0:
		fd.perms |= O_APPEND
	}
	fd.file = file
	return fdn
}

func sys_close(proc *proc_t, fdn int) int {
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	if fd.ftype != INODE {
		panic("no imp")
	}
	// XXX free inode blocks if it has no links and this was the last fd to
	// it.
	delete(proc.fds, fdn)
	return 0
}

func sys_fstat(proc *proc_t, fdn int, statn int) int {
	buf := &stat_t{}
	buf.init()
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	err := fs_stat(fd.file.priv, buf)
	if err != 0 {
		return err
	}
	ok = proc.usercopy(buf.data, statn)
	if !ok {
		return -EFAULT
	}
	return 0
}

func sys_mkdir(proc *proc_t, pathn int, mode int) int {
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	parts, err := path_sanitize(proc.cwd, path)
	if err != 0 {
		return err
	}
	return fs_mkdir(parts, mode)
}

func sys_link(proc *proc_t, oldn int, newn int) int {
	old, ok1, toolong1 := proc.userstr(oldn, NAME_MAX)
	new, ok2, toolong2 := proc.userstr(newn, NAME_MAX)
	if !ok1 || !ok2 {
		return -EFAULT
	}
	if toolong1 || toolong2 {
		return -ENAMETOOLONG
	}
	opath, err1 := path_sanitize(proc.cwd, old)
	npath, err2 := path_sanitize(proc.cwd, new)
	if err1 != 0 {
		return err1
	}
	if err2 != 0 {
		return err2
	}
	return fs_link(opath, npath)
}

func sys_unlink(proc *proc_t, pathn int) int {
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	parts, err := path_sanitize(proc.cwd, path)
	if err != 0 {
		return err
	}
	return fs_unlink(parts)
}

func sys_getpid(proc *proc_t) int {
	return proc.pid
}

func sys_fork(parent *proc_t, ptf *[TFSIZE]int) int {

	child := proc_new(fmt.Sprintf("%s's child", parent.name), 0)

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
		child.pages[p_pg] = pg
		return pte, pte
	}
	pmap, p_pmap, _ := copy_pmap(mk_cow, parent.pmap, child.pages)
	// copy user->phys mappings too
	for k, v := range parent.upages {
		child.upages[k] = v
	}

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

func sys_execv(proc *proc_t, pathn int, argn int) int {
	args, ok := proc.userargs(argn)
	if !ok {
		return -EFAULT
	}
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	parts, err := path_sanitize(proc.cwd, path)
	if err != 0 {
		return err
	}
	// XXX we don't recycle the proc. does it matter?
	return sys_execv1(proc, parts, args)
}

func sys_execv1(proc *proc_t, path []string, args []string) int {
	// XXX close?
	file, err := fs_open(path, O_RDONLY, 0)
	if err != 0 {
		return err
	}
	eobj := make([]uint8, 0)
	add := make([]uint8, 4096)
	ret := 1
	c := 0
	for ret != 0 {
		ret, err = fs_read([][]uint8{add}, file.priv, c)
		c += ret
		if err != 0 {
			return err
		}
		valid := add[0:ret]
		eobj = append(eobj, valid...)
	}
	elf := &elf_t{eobj}
	_, ok := elf.npheaders()
	if !ok {
		return -EPERM
	}

	usepid := 0
	if proc != nil {
		proc_kill(proc.pid)
		usepid = proc.pid
	}
	cmd := "/" + strings.Join(path, "/") + strings.Join(args, " ")
	newproc := proc_new(cmd, usepid)


	stackva := mkpg(VUSER + 1, 0, 0, 0)
	var tf [23]int
	tf[TF_RSP] = stackva - 8
	tf[TF_RIP] = elf.entry()
	tf[TF_RFLAGS] = TF_FL_IF

	ucseg := 4
	udseg := 5
	tf[TF_CS] = ucseg << 3 | 3
	tf[TF_SS] = udseg << 3 | 3

	// copy kernel page table, map new stack
	newproc.pmap, newproc.p_pmap, _ = copy_pmap(nil, kpmap(),
	    newproc.pages)
	numstkpages := 1
	for i := 0; i < numstkpages; i++ {
		stack, p_stack := pg_new(newproc.pages)
		newproc.page_insert(stackva - PGSIZE*(i+1), stack, p_stack,
		    PTE_U | PTE_W, true)
	}

	elf_load(newproc, elf)
	argc, argv := insertargs(newproc, args)
	tf[TF_RDI] = argc
	tf[TF_RSI] = argv
	newproc.sched_add(&tf)

	return 0
}

func insertargs(proc *proc_t, sargs []string) (int, int) {
	// find free page
	uva := 0
	for i := 0; i < 1000; i++ {
		taddr := USERMIN + i*PGSIZE
		if _, ok := proc.upages[taddr]; !ok {
			uva = taddr
			break
		}
	}
	if uva == 0 {
		panic("couldn't find free user page")
	}
	pg, p_pg := pg_new(proc.pages)
	proc.page_insert(uva, pg, p_pg, PTE_U, true)
	var args [][]uint8
	for _, str := range sargs {
		args = append(args, []uint8(str))
	}
	argptrs := make([]int, len(args) + 1)
	// copy strings to arg page
	cnt := 0
	for i, arg := range args {
		argptrs[i] = uva + cnt
		// add null terminators
		arg = append(arg, 0)
		if !proc.usercopy(arg, uva + cnt) {
			// args take up more than a page? the user is on their
			// own.
			return 0, uva
		}
		cnt += len(arg)
	}
	argptrs[len(argptrs) - 1] = 0
	// now put the array of strings
	argstart := uva + cnt
	vdata, ok := proc.userdmap(argstart)
	if !ok || len(vdata) < len(argptrs)*8 {
		return 0, uva
	}
	for i, ptr := range argptrs {
		writen(vdata, 8, i*8, ptr)
	}
	return len(args), argstart
}

func sys_exit(proc *proc_t, status int) {
	//fmt.Printf("%v exited with status %v\n", proc.name, status)
	tot := runtime.Rdtsc() - proc.tstart
	proc_kill(proc.pid)
	fmt.Printf("%v -- %v cycles (%v GC cycles)\n", proc.name, tot,
	    runtime.Resetgcticks())
	if runtime.SCenable {
		fmt.Printf("SERIAL CONSOLE ENABLED\n")
	}
}

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

// returns the byte size/offset of field n. they can be used to read []data.
func fieldinfo(sizes []int, n int) (int, int) {
	if n >= len(sizes) {
		panic("badd field number")
	}
	off := 0
	for i := 0; i < n; i++ {
		off += sizes[i]
	}
	return sizes[n], off
}

type stat_t struct {
	data	[]uint8
	// field sizes
	sizes	[]int
}

func (st *stat_t) init() {
	st.sizes = []int{
		8, // 0 - dev
		8, // 1 - ino
		8, // 2 - mode
		8, // 3 - size
		}
	sz := 0
	for _, c := range st.sizes {
		sz += c
	}
	st.data = make([]uint8, sz)
}

func (st *stat_t) wdev(v int) {
	size, off := fieldinfo(st.sizes, 0)
	writen(st.data, size, off, v)
}

func (st *stat_t) wino(v int) {
	size, off := fieldinfo(st.sizes, 1)
	writen(st.data, size, off, v)
}

func (st *stat_t) wmode(v int) {
	size, off := fieldinfo(st.sizes, 2)
	writen(st.data, size, off, v)
}

func (st *stat_t) wsize(v int) {
	size, off := fieldinfo(st.sizes, 3)
	writen(st.data, size, off, v)
}

type elf_t struct {
	data	[]uint8
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

func (e *elf_t) npheaders() (int, bool) {
	mag := readn(e.data, ELF_HALF, 0)
	if mag != 0x464c457f {
		return 0, false
	}
	e_phnum := 0x38
	return readn(e.data, ELF_QUARTER, e_phnum), true
}

func (e *elf_t) header(c int, ret *elf_phdr) {
	if ret == nil {
		panic("nil elf_t")
	}

	nph, ok := e.npheaders()
	if !ok {
		panic("bad elf")
	}
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
	num, ok := e.npheaders()
	if !ok {
		panic("bad elf")
	}
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

// loads user program from gobins
func sys_test(program string) {
	fmt.Printf("add 'user' prog\n")

	var tf [23]int

	proc := proc_new(program + "test", 0)

	elf, ok := allbins[program]
	if !ok {
		panic("no such program: " + program)
	}

	stack, p_stack := pg_new(proc.pages)
	stackva := mkpg(VUSER + 1, 0, 0, 0)
	tf[TF_RSP] = stackva - 8
	tf[TF_RIP] = elf.entry()
	tf[TF_RFLAGS] = TF_FL_IF

	ucseg := 4
	udseg := 5
	tf[TF_CS] = ucseg << 3 | 3
	tf[TF_SS] = udseg << 3 | 3

	// copy kernel page table, map new stack
	upmap, p_upmap, _ := copy_pmap(nil, kpmap(), proc.pages)
	proc.pmap, proc.p_pmap = upmap, p_upmap
	proc.page_insert(stackva - PGSIZE, stack,
	    p_stack, PTE_U | PTE_W, true)

	elf_load(proc, elf)

	proc.sched_add(&tf)
}
