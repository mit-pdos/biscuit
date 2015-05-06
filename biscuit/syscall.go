package main

import "fmt"
import "math/rand"
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
  TF_CS        = TFREGS + 3
  TF_RSP       = TFREGS + 5
  TF_SS        = TFREGS + 6
  TF_RFLAGS    = TFREGS + 4
    TF_FL_IF     = 1 << 9
)

const(
  EPERM        = 1
  ENOENT       = 2
  ESRCH	       = 3
  EBADF        = 9
  ECHILD       = 10
  EFAULT       = 14
  EEXIST       = 17
  ENOTDIR      = 20
  EISDIR       = 21
  EINVAL       = 22
  EPIPE        = 32
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
    O_CREAT       = 0x40
    O_EXCL        = 0x80
    O_APPEND      = 0x400
    O_DIRECTORY   = 0x10000
  SYS_CLOSE    = 3
  SYS_FSTAT    = 5
  SYS_MMAP     = 9
    MAP_PRIVATE   = 0x2
    MAP_FIXED     = 0x10
    MAP_ANON      = 0x20
    MAP_FAILED    = -1
    PROT_NONE     = 0x0
    PROT_READ     = 0x1
    PROT_WRITE    = 0x2
    PROT_EXEC     = 0x4
  SYS_MUNMAP   = 11
  SYS_PIPE     = 22
  SYS_PAUSE    = 34
  SYS_GETPID   = 39
  SYS_FORK     = 57
  SYS_EXECV    = 59
  SYS_EXIT     = 60
  // should be wait4
  SYS_WAIT     = 61
  SYS_KILL     = 62
  SYS_CHDIR    = 80
  SYS_MKDIR    = 83
  SYS_LINK     = 86
  SYS_UNLINK   = 87
  SYS_FAKE     = 31337
)

const(
  SIGKILL = 9
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

	if p.doomed {
		// this process has been killed
		reap_doomed(pid)
		return
	}

	sysno := tf[TF_RAX]
	a1 := tf[TF_RDI]
	a2 := tf[TF_RSI]
	a3 := tf[TF_RDX]
	a4 := tf[TF_RCX]
	a5 := tf[TF_R8]

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
	case SYS_MMAP:
		ret = sys_mmap(p, a1, a2, a3, a4, a5)
	case SYS_MUNMAP:
		ret = sys_munmap(p, a1, a2)
	case SYS_PIPE:
		ret = sys_pipe(p, a1)
	case SYS_PAUSE:
		ret = sys_pause(p)
	case SYS_GETPID:
		// XXX
		runtime.Resetgcticks()
		ret = sys_getpid(p)
	case SYS_FORK:
		ret = sys_fork(p, tf)
	case SYS_EXECV:
		ret = sys_execv(p, tf, a1, a2)
	case SYS_EXIT:
		sys_exit(p, a1)
	case SYS_WAIT:
		ret = sys_wait(p, a1)
	case SYS_KILL:
		ret = sys_kill(p, a1, a2)
	case SYS_CHDIR:
		ret = sys_chdir(p, a1)
	case SYS_MKDIR:
		ret = sys_mkdir(p, a1, a2)
	case SYS_LINK:
		ret = sys_link(p, a1, a2)
	case SYS_UNLINK:
		ret = sys_unlink(p, a1)
	case SYS_FAKE:
		ret = sys_fake(p, a1)
	default:
		fmt.Printf("unexpected syscall %v\n", sysno)
	}

	tf[TF_RAX] = ret
	if !p.dead {
		runtime.Procrunnable(pid, tf, p.p_pmap)
	}
}

func reap_doomed(pid int) {
	p := proc_get(pid)
	if !p.doomed {
		panic("p not doomed")
	}
	sys_exit(p, -1)
}

var fdreaders = map[ftype_t]func([][]uint8, *file_t, int) (int, int) {
	CDEV : func(dsts [][]uint8, priv *file_t, offset int) (int, int) {
		sz := 0
		for _, d := range dsts {
			sz += len(d)
		}
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
		if len(kdata) != 0 {
			panic("dropped keys")
		}
		return ret, 0
	},
	INODE : fs_read,
	PIPE : pipe_read,
}

var fdwriters = map[ftype_t]func([][]uint8, *file_t, int, bool) (int, int) {
	CDEV : func(srcs [][]uint8, f *file_t, off int, ap bool) (int, int) {
			// merge into one buffer to avoid taking the console
			// lock many times.
			utext := int8(0x17)
			big := make([]uint8, 0)
			for _, s := range srcs {
				big = append(big, s...)
			}
			runtime.Pmsga(&big[0], len(big), utext)
			return len(big), 0
	},
	INODE : fs_write,
	PIPE : pipe_write,
}

var fdclosers = map[ftype_t]func(*file_t, int) int {
	CDEV : func(f *file_t, perms int) int {
		return 0
	},
	INODE : fs_close,
	PIPE : pipe_close,
}

// XXX need to serialize per-fd operations
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
	if _, ok := fdreaders[fd.ftype]; !ok {
		panic("no imp")
	}
	if fd.perms & FD_READ == 0 {
		return -EPERM
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

	if fd.ftype == INODE {
		fd.Lock()
		defer fd.Unlock()
	}

	ret, err := fdreaders[fd.ftype](dsts, fd.file, fd.offset)
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
	if fd.perms & FD_WRITE == 0 {
		return -EPERM
	}
	if _, ok := fdwriters[fd.ftype]; !ok {
		panic("no imp")
	}
	if fd.ftype == INODE {
		fd.Lock()
		defer fd.Unlock()
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
	ret, err := fdwriters[fd.ftype](srcs, fd.file, fd.offset, apnd)
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
	fdperms := 0
	switch temp {
	case O_RDONLY:
		fdperms = FD_READ
	case O_WRONLY:
		fdperms = FD_WRITE
	case O_RDWR:
		fdperms = FD_READ | FD_WRITE
	default:
		fdperms = FD_READ
	}
	err := badpath(path)
	if err != 0 {
		return err
	}
	file, err := fs_open(path, flags, mode, proc.cwd)
	if err != 0 {
		return err
	}
	fdn, fd := proc.fd_new(INODE, fdperms)
	switch {
	case flags & O_APPEND != 0:
		fd.perms |= O_APPEND
	}
	fd.file = file
	return fdn
}

func sys_pause(proc *proc_t) int {
	// no signals yet!
	var c chan bool
	<- c
	return -1
}

func sys_close(proc *proc_t, fdn int) int {
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	if _, ok := fdclosers[fd.ftype]; !ok {
		panic("no imp")
	}
	ret := file_close(fd.ftype, fd.file, fd.perms)
	delete(proc.fds, fdn)
	if fdn < proc.fdstart {
		proc.fdstart = fdn
	}
	return ret
}

func file_close(ft ftype_t, f *file_t, perms int) int {
	return fdclosers[ft](f, perms)
}

func sys_mmap(proc *proc_t, addrn, lenn, protflags, fd, offset int) int {
	prot := uint(protflags) >> 32
	flags := uint(uint32(protflags))

	if flags != MAP_PRIVATE | MAP_ANON {
		panic("no imp")
	}
	if flags & MAP_FIXED != 0 && addrn < USERMIN {
		return MAP_FAILED
	}
	if prot == PROT_NONE {
		return proc.mmapi
	}
	perms := PTE_U
	if prot & PROT_WRITE != 0 {
		perms |= PTE_W
	}
	lenn = roundup(lenn, PGSIZE)
	if lenn/PGSIZE + len(proc.pages) > proc.ulim.pages {
		return MAP_FAILED
	}
	addr := mmap_findaddr(proc, addrn, lenn)
	for i := 0; i < lenn; i += PGSIZE {
		pg, p_pg := pg_new(proc.pages)
		proc.page_insert(addr + i, pg, p_pg, perms, true)
	}
	return addr
}

func mmap_findaddr(proc *proc_t, hint, len int) int {
	for proc.mmapi < 256 << 39 {
		found := true
		for i := 0; i < len; i += PGSIZE {
			pte := pmap_walk(proc.pmap, proc.mmapi + i, false, 0,
			    nil)
			if pte != nil && *pte & PTE_P != 0 {
				found = false
				proc.mmapi += i + PGSIZE
				break
			}
		}
		if found {
			ret := proc.mmapi
			proc.mmapi += len
			return ret
		}
	}
	panic("no addr space left")
}

func sys_munmap(proc *proc_t, addrn, len int) int {
	if addrn & PGOFFSET != 0 || addrn < USERMIN {
		return -EINVAL
	}
	len = roundup(len, PGSIZE)
	for i := 0; i < len; i += PGSIZE {
		p := addrn + i
		if !proc.page_remove(p) {
			return -EINVAL
		}
	}
	return 0
}

func sys_fstat(proc *proc_t, fdn int, statn int) int {
	buf := &stat_t{}
	buf.init()
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	err := fs_stat(fd.file, buf)
	if err != 0 {
		return err
	}
	ok = proc.usercopy(buf.data, statn)
	if !ok {
		return -EFAULT
	}
	return 0
}

func sys_pipe(proc *proc_t, pipen int) int {
	ok := proc.usermapped(pipen, 8)
	if !ok {
		return -EFAULT
	}
	rfd, rf := proc.fd_new(PIPE, FD_READ)
	wfd, wf := proc.fd_new(PIPE, FD_WRITE)

	pipef := pipe_new()
	rf.file = pipef
	wf.file = pipef

	proc.userwriten(pipen, 4, rfd)
	proc.userwriten(pipen + 4, 4, wfd)
	return 0
}

type pipe_t struct {
	inret	chan int
	in	chan []uint8
	outsz	chan int
	out	chan []uint8
	readers	chan int
	writers	chan int
}

func pipe_new() *file_t {
	ret := &file_t{}
	inret := make(chan int)
	in := make(chan []uint8)
	out := make(chan []uint8)
	outsz := make(chan int)
	writers := make(chan int)
	readers := make(chan int)
	ret.pipe.inret = inret
	ret.pipe.in = in
	ret.pipe.outsz = outsz
	ret.pipe.out = out
	ret.pipe.readers = readers
	ret.pipe.writers = writers

	go func() {
		pipebufsz := 512
		writec := 1
		readc := 1
		var buf []uint8
		var toutsz chan int
		tin := in
		for writec > 0 || readc > 0 {
			select {
			// writing to pipe
			case d := <- tin:
				if readc == 0 {
					inret <- -EPIPE
					break
				}
				if len(buf) + len(d) > pipebufsz {
					take := pipebufsz - len(buf)
					d = d[:take]
				}
				buf = append(buf, d...)
				inret <- len(d)
			// reading from pipe
			case sz := <- toutsz:
				if len(buf) == 0 && writec != 0 {
					panic("no data")
				}
				if sz > len(buf) {
					sz = len(buf)
				}
				out <- buf[:sz]
				buf = buf[sz:]
			// closing/opening
			case delta := <- writers:
				writec += delta
			case delta := <- readers:
				readc += delta
			}

			// allow more reads if there is data in the pipe or if
			// there are no writers and the pipe is empty.
			if len(buf) > 0 || writec == 0 {
				toutsz = outsz
			} else {
				toutsz = nil
			}
			if len(buf) < pipebufsz || readc == 0 {
				tin = in
			} else {
				tin = nil
			}
		}
	}()

	return ret
}

func pipe_read(dsts [][]uint8, f *file_t, offset int) (int, int) {
	sz := 0
	for _, dst :=  range dsts {
		sz += len(dst)
	}
	pf := f.pipe
	pf.outsz <- sz
	buf := <- pf.out
	ret := buftodests(buf, dsts)
	return ret, 0
}

func pipe_write(srcs [][]uint8, f *file_t, offset int, appnd bool) (int, int) {
	var buf []uint8
	for _, src := range srcs {
		buf = append(buf, src...)
	}
	pf := f.pipe
	ret := 0
	for len(buf) > 0 {
		pf.in <- buf
		c := <- pf.inret
		if c < 0 {
			return 0, c
		}
		buf = buf[c:]
		ret += c
	}
	return ret, 0
}

func pipe_close(f *file_t, perms int) int {
	pf := f.pipe
	var ch chan int
	if perms == FD_READ {
		ch = pf.readers
	} else {
		ch = pf.writers
	}
	ch <- -1
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
	err := badpath(path)
	if err != 0 {
		return err
	}
	return fs_mkdir(path, mode, proc.cwd)
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
	err1 := badpath(old)
	err2 := badpath(new)
	if err1 != 0 {
		return err1
	}
	if err2 != 0 {
		return err2
	}
	return fs_link(old, new, proc.cwd)
}

func sys_unlink(proc *proc_t, pathn int) int {
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	err := badpath(path)
	if err != 0 {
		return err
	}
	return fs_unlink(path, proc.cwd)
}

func sys_getpid(proc *proc_t) int {
	return proc.pid
}

func sys_fork(parent *proc_t, ptf *[TFSIZE]int) int {
	if parent.waitch == nil {
		parent.waitch = make(chan waitmsg_t)
	}
	parent.nchild++
	child := proc_new(fmt.Sprintf("%s's child", parent.name), 0)
	child.pwaitch = parent.waitch
	child.cwd = parent.cwd

	// copy fd table
	for k, v := range parent.fds {
		child.fds[k] = v
		// increment reader/writer count
		switch v.ftype {
		case PIPE:
			var ch chan int
			if v.perms == FD_READ {
				ch = v.file.pipe.readers
			} else {
				ch = v.file.pipe.writers
			}
			ch <- 1
		case INODE:
			fs_memref(v.file, v.perms)
		}
	}

	pmap, p_pmap := pmap_copy_par(parent.pmap, child.pages, parent.pages)

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
	*dst = *src

	// insert new page into pmap
	va := faultaddr & PGMASK
	perms := (*pte & PTE_FLAGS) & ^PTE_COW
	perms |= PTE_W
	proc.page_insert(va, dst, p_dst, perms, false)

	// set process as runnable again
	runtime.Procrunnable(proc.pid, nil, proc.p_pmap)
}

func sys_execv(proc *proc_t, tf *[TFSIZE]int, pathn int, argn int) int {
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
	err := badpath(path)
	if err != 0 {
		return err
	}
	return sys_execv1(proc, tf, path, args)
}

func sys_execv1(proc *proc_t, tf *[TFSIZE]int, paths string,
    args []string) int {
	// XXX close?
	file, err := fs_open(paths, O_RDONLY, 0, proc.cwd)
	if err != 0 {
		return err
	}
	eobj := make([]uint8, 0)
	add := make([]uint8, 4096)
	ret := 1
	c := 0
	for ret != 0 {
		ret, err = fs_read([][]uint8{add}, file, c)
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

	stackva := mkpg(VUSER + 1, 0, 0, 0)

	opages := proc.pages
	oupages := proc.upages
	proc.pages = make(map[int]*[512]int)
	proc.upages = make(map[int]int)

	// copy kernel page table, map new stack
	opmap := proc.pmap
	op_pmap := proc.p_pmap
	proc.pmap, proc.p_pmap, _ = copy_pmap(nil, kpmap(), proc.pages)
	numstkpages := 1
	for i := 0; i < numstkpages; i++ {
		stack, p_stack := pg_new(proc.pages)
		proc.page_insert(stackva - PGSIZE*(i+1), stack, p_stack,
		    PTE_U | PTE_W, true)
	}

	elf_load(proc, elf)
	argc, argv, ok := insertargs(proc, args)
	if !ok {
		// restore old process
		proc.pages = opages
		proc.upages = oupages
		proc.pmap = opmap
		proc.p_pmap = op_pmap
		return -EINVAL
	}

	// commit new image state
	tf[TF_RSP] = stackva - 8
	tf[TF_RIP] = elf.entry()
	tf[TF_RFLAGS] = TF_FL_IF
	ucseg := 4
	udseg := 5
	tf[TF_CS] = ucseg << 3 | 3
	tf[TF_SS] = udseg << 3 | 3
	tf[TF_RDI] = argc
	tf[TF_RSI] = argv
	// XXX duplicated in proc_new
	proc.fds = map[int]*fd_t{0: &fd_stdin, 1: &fd_stdout, 2: &fd_stderr}
	proc.fdstart = 3
	proc.mmapi = USERMIN
	proc.name = paths

	return 0
}

func insertargs(proc *proc_t, sargs []string) (int, int, bool) {
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
			return 0, 0, false
		}
		cnt += len(arg)
	}
	argptrs[len(argptrs) - 1] = 0
	// now put the array of strings
	argstart := uva + cnt
	vdata, ok := proc.userdmap(argstart)
	if !ok || len(vdata) < len(argptrs)*8 {
		fmt.Printf("no room for args")
		return 0, 0, false
	}
	for i, ptr := range argptrs {
		writen(vdata, 8, i*8, ptr)
	}
	return len(args), argstart, true
}

func bloataddr(p *proc_t, npages int) {
	fmt.Printf("pyumping up\n")
	pn := rand.Intn(0x518000000)
	start := pn << 12
	start += USERMIN
	pg, p_pg := pg_new(p.pages)
	add := 0
	for i := 0; i < npages; i++ {
		//pg, p_pg := pg_new(p.pages)
		for {
			addr := start + add + i*PGSIZE

			if _, ok := p.upages[addr]; ok {
				add += PGSIZE
				fmt.Printf("@")
				continue
			}
			p.page_insert(addr, pg, p_pg, PTE_U | PTE_W, true)
			break
		}
	}
	sz := len(p.pages)
	sz *= 1 << 12
	sz /= 1 << 20
	fmt.Printf("app mem size: %vMB (%v pages)\n", sz, len(p.pages))
}

func sys_exit(proc *proc_t, status int) {
	if proc.pid == 1 {
		panic("killed init")
	}

	// close open fds
	for fdn := range proc.fds {
		sys_close(proc, fdn)
	}

	//fmt.Printf("%v exited with status %v\n", proc.name, status)
	//tot := runtime.Rdtsc() - proc.tstart
	proc_kill(proc.pid)

	// send status to parent
	if proc.pwaitch == nil {
		panic("pwaitch nil")
	}
	go func() {
		proc.pwaitch <- waitmsg_t{proc.pid, status, 0}
	}()

	// drain all child statuses in background
	go func() {
		for i := proc.nreap; i < proc.nchild; i++ {
			<- proc.waitch
		}
	}()

	//fmt.Printf("%v -- %v cycles (%v GC cycles)\n", proc.name, tot,
	//    runtime.Resetgcticks())
	//if runtime.SCenable {
	//	fmt.Printf("SERIAL CONSOLE ENABLED\n")
	//}
}

func sys_wait(proc *proc_t, statusp int) int {
	if proc.nreap == proc.nchild {
		return -ECHILD
	}
	ok := proc.usermapped(statusp, 4)
	if !ok {
		return -EFAULT
	}
	wmsg := <- proc.waitch
	proc.nreap++
	proc.userwriten(statusp, 4, wmsg.status)
	return wmsg.pid
}

func sys_kill(proc *proc_t, pid, sig int) int {
	if sig != SIGKILL {
		panic("no imp")
	}
	p, ok := proc_check(pid)
	if !ok {
		return -ESRCH
	}
	p.doomed = true
	if runtime.Procnotify(pid) != 0 {
		fmt.Printf("pid %v already terminated\n", pid)
	}
	return 0
}

func sys_chdir(proc *proc_t, dirn int) int {
	path, ok, toolong := proc.userstr(dirn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	err := badpath(path)
	if err != 0 {
		return err
	}

	file_close(INODE, proc.cwd, 0)
	newcwd, err := fs_open(path, O_RDONLY | O_DIRECTORY, 0, proc.cwd)
	if err != 0 {
		return err
	}
	proc.cwd = newcwd
	return 0
}

func badpath(path string) int {
	if len(path) == 0 {
		return -ENOENT
	}
	return 0
}

func buftodests(buf []uint8, dsts [][]uint8) int {
	ret := 0
	for _, dst := range dsts {
		ub := len(buf)
		if ub > len(dst) {
			ub = len(dst)
		}
		for i := 0; i < ub; i++ {
			dst[i] = buf[i]
		}
		ret += ub
		buf = buf[ub:]
	}
	return ret
}

type obj_t struct {
}

var amap = map[int]obj_t{}

func sys_fake(proc *proc_t, n int) int {
	amap = make(map[int]obj_t)
	for i := 0; i < n; i++ {
		amap[rand.Int()] = obj_t{}
	}

	ms := runtime.MemStats{}
	runtime.ReadMemStats(&ms)
	heapsz := ms.HeapAlloc
	fmt.Printf("Heapsize: %7v MB\n", heapsz/(1<<20))

	//return len(amap)
	return int(runtime.Resetgcticks())
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
		panic("bad field number")
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

func (st *stat_t) mode() int {
	size, off := fieldinfo(st.sizes, 2)
	return readn(st.data, size, off)
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
