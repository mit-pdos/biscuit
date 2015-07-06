package main

import "fmt"
import "math/rand"
import "runtime"
import "sync"
import "time"
import "unsafe"

const(
  TFSIZE       = 23
  TFREGS       = 16
  TF_FSBASE    = 0
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
  ESPIPE       = 29
  EPIPE        = 32
  ENAMETOOLONG = 36
  ENOSYS       = 38
  EMSGSIZE     = 90
  ECONNREFUSED = 111
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
    O_TRUNC       = 0x200
    O_APPEND      = 0x400
    O_NONBLOCK    = 0x800
    O_DIRECTORY   = 0x10000
    O_CLOEXEC     = 0x80000
  SYS_CLOSE    = 3
  SYS_STAT     = 4
  SYS_FSTAT    = 5
  SYS_LSEEK    = 8
    SEEK_SET      = 0x1
    SEEK_CUR      = 0x2
    SEEK_END      = 0x4
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
  SYS_DUP2     = 33
  SYS_PAUSE    = 34
  SYS_GETPID   = 39
  SYS_SOCKET   = 41
    // domains
    AF_UNIX       = 1
    // types
    SOCK_STREAM   = 1
    SOCK_DGRAM    = 2
  SYS_SENDTO   = 44
  SYS_RECVFROM = 45
  SYS_BIND     = 49
  SYS_FORK     = 57
    FORK_PROCESS = 0x1
    FORK_THREAD  = 0x2
  SYS_EXECV    = 59
  SYS_EXIT     = 60
    CONTINUED    = 1 << 9
    EXITED       = 1 << 10
    SIGNALED     = 1 << 11
  SYS_WAIT4    = 61
    WAIT_ANY     = -1
    WAIT_MYPGRP  = 0
  SYS_KILL     = 62
  SYS_CHDIR    = 80
  SYS_RENAME   = 82
  SYS_MKDIR    = 83
  SYS_LINK     = 86
  SYS_UNLINK   = 87
  SYS_GETTOD   = 96
  SYS_MKNOD    = 133
  SYS_NANOSLEEP= 230
  SYS_PIPE2    = 293
  SYS_FAKE     = 31337
  SYS_THREXIT  = 31338
)

const(
  SIGKILL = 9
)

// lowest userspace address
const USERMIN	int = VUSER << 39

func syscall(pid int, tid tid_t, tf *[TFSIZE]int) {

	p := proc_get(pid)

	p.Lock()
	talive, ok := p.threadi.alive[tid]
	if !ok {
		panic("bad thread")
	}
	if !talive {
		panic("thread not alive")
	}
	p.Unlock()

	if p.doomed {
		// this process has been killed
		reap_doomed(pid, tid)
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
	case SYS_STAT:
		ret = sys_stat(p, a1, a2)
	case SYS_FSTAT:
		ret = sys_fstat(p, a1, a2)
	case SYS_LSEEK:
		ret = sys_lseek(p, a1, a2, a3)
	case SYS_MMAP:
		ret = sys_mmap(p, a1, a2, a3, a4, a5)
	case SYS_MUNMAP:
		ret = sys_munmap(p, a1, a2)
	case SYS_DUP2:
		ret = sys_dup2(p, a1, a2)
	case SYS_PAUSE:
		ret = sys_pause(p)
	case SYS_GETPID:
		// XXX
		runtime.Resetgcticks()
		ret = sys_getpid(p)
	case SYS_SOCKET:
		ret = sys_socket(p, a1, a2, a3);
	case SYS_SENDTO:
		ret = sys_sendto(p, a1, a2, a3, a4, a5);
	case SYS_RECVFROM:
		ret = sys_recvfrom(p, a1, a2, a3, a4, a5);
	case SYS_BIND:
		ret = sys_bind(p, a1, a2, a3);
	case SYS_FORK:
		ret = sys_fork(p, tf, a1, a2)
	case SYS_EXECV:
		ret = sys_execv(p, tf, a1, a2)
	case SYS_EXIT:
		status := a1 & 0xff
		status |= EXITED
		sys_exit(p, tid, status)
	case SYS_WAIT4:
		ret = sys_wait4(p, a1, a2, a3, a4, a5)
	case SYS_KILL:
		ret = sys_kill(p, a1, a2)
	case SYS_CHDIR:
		ret = sys_chdir(p, a1)
	case SYS_RENAME:
		ret = sys_rename(p, a1, a2)
	case SYS_MKDIR:
		ret = sys_mkdir(p, a1, a2)
	case SYS_LINK:
		ret = sys_link(p, a1, a2)
	case SYS_UNLINK:
		ret = sys_unlink(p, a1)
	case SYS_GETTOD:
		ret = sys_gettimeofday(p, a1)
	case SYS_MKNOD:
		ret = sys_mknod(p, a1, a2, a3)
	case SYS_NANOSLEEP:
		ret = sys_nanosleep(p, a1, a2)
	case SYS_PIPE2:
		ret = sys_pipe2(p, a1, a2)
	case SYS_FAKE:
		ret = sys_fake(p, a1)
	case SYS_THREXIT:
		sys_threxit(p, tid, a1)
	default:
		fmt.Printf("unexpected syscall %v\n", sysno)
	}

	tf[TF_RAX] = ret
	if p.resched(tid) {
		p.sched_runnable(tf, tid)
	}
}

func reap_doomed(pid int, tid tid_t) {
	p := proc_get(pid)
	if !p.doomed {
		panic("p not doomed")
	}
	p.thread_dead(tid, 0, false)
}

func cons_read(dsts [][]uint8, priv *file_t, offset int) (int, int) {
	sz := 0
	for _, d := range dsts {
		sz += len(d)
	}
	kdata := kbd_get(sz)
	ret := len(kdata)
	if buftodests(kdata, dsts) != len(kdata) {
		panic("dropped keys")
	}
	return ret, 0
}

func cons_write(srcs [][]uint8, f *file_t, off int, ap bool) (int, int) {
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

func sock_close(f *file_t, perms int) int {
	if !f.sock.dgram {
		panic("no imp")
	}
	f.Lock()
	f.sock.open--
	if f.sock.open < 0 {
		panic("negative ref count")
	}
	termsund := f.sock.open == 0
	f.Unlock()

	if termsund && f.sock.bound {
		allsunds.Lock()
		delete(allsunds.m, f.sock.sunaddr.id)
		f.sock.sunaddr.sund.finish <- true
		allsunds.Unlock()
	}
	return 0
}

var fdreaders = map[ftype_t]func([][]uint8, *file_t, int) (int, int) {
	DEV : func(dsts [][]uint8, f *file_t, offset int) (int, int) {
		dm := map[int]func([][]uint8, *file_t, int) (int, int) {
			D_CONSOLE: cons_read,
		}
		fn, ok := dm[f.dev.major]
		if !ok {
			panic("bad device major")
		}
		return fn(dsts, f, offset)
	},
	INODE : fs_read,
	PIPE : pipe_read,
}

var fdwriters = map[ftype_t]func([][]uint8, *file_t, int, bool) (int, int) {
	DEV : func(srcs [][]uint8, f *file_t, off int, ap bool) (int, int) {
		dm := map[int]func([][]uint8, *file_t, int, bool) (int, int){
			D_CONSOLE: cons_write,
		}
		fn, ok := dm[f.dev.major]
		if !ok {
			panic("bad device major")
		}
		return fn(srcs, f, off, ap)
	},
	INODE : fs_write,
	PIPE : pipe_write,
}

var fdclosers = map[ftype_t]func(*file_t, int) int {
	DEV : func(f *file_t, perms int) int {
		dm := map[int]func(*file_t, int) int {
			D_CONSOLE:
			    func (f *file_t, perms int) int {
				    return 0
			    },
		}
		fn, ok := dm[f.dev.major]
		if !ok {
			panic("bad device major")
		}
		return fn(f, perms)
	},
	INODE : fs_close,
	PIPE : pipe_close,
	SOCKET : sock_close,
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
	if _, ok := fdreaders[fd.file.ftype]; !ok {
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
		dst, ok := proc.userdmap8(bufp + c)
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

	if fd.file.ftype == INODE {
		fd.file.Lock()
		defer fd.file.Unlock()
	}

	ret, err := fdreaders[fd.file.ftype](dsts, fd.file, fd.file.offset)
	if err != 0 {
		return err
	}
	fd.file.offset += ret
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
	if _, ok := fdwriters[fd.file.ftype]; !ok {
		panic("no imp")
	}
	if fd.file.ftype == INODE {
		fd.file.Lock()
		defer fd.file.Unlock()
	}

	apnd := fd.perms & FD_APPEND != 0
	c := 0
	srcs := make([][]uint8, 0)
	for c < sz {
		src, ok := proc.userdmap8(bufp + c)
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
	ret, err := fdwriters[fd.file.ftype](srcs, fd.file, fd.file.offset,
	    apnd)
	if err != 0 {
		return err
	}
	fd.file.offset += ret
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
	if temp == O_RDONLY && flags & O_TRUNC != 0 {
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
	file, err := fs_open(path, flags, mode, proc.cwd, 0, 0)
	if err != 0 {
		return err
	}
	// not allowed to open UNIX sockets
	if file.ftype == DEV && file.dev.major == D_SUN {
		if err := fs_close(file, 0); err != 0 {
			panic("must succeed")
		}
		return -EPERM
	}
	fdn, fd := proc.fd_new(fdperms)
	if flags & O_APPEND != 0 {
		fd.perms |= FD_APPEND
	}
	if flags & O_CLOEXEC != 0 {
		fd.perms |= FD_CLOEXEC
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
	if _, ok := fdclosers[fd.file.ftype]; !ok {
		panic("no imp")
	}
	ret := file_close(fd.file, fd.perms)
	delete(proc.fds, fdn)
	if fdn < proc.fdstart {
		proc.fdstart = fdn
	}
	return ret
}

func file_close(f *file_t, perms int) int {
	return fdclosers[f.ftype](f, perms)
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
	addr := proc.unusedva(proc.mmapi, lenn)
	proc.mmapi = addr + lenn
	for i := 0; i < lenn; i += PGSIZE {
		pg, p_pg := pg_new(proc.pages)
		proc.page_insert(addr + i, pg, p_pg, perms, true)
	}
	return addr
}

func sys_munmap(proc *proc_t, addrn, len int) int {
	if addrn & PGOFFSET != 0 || addrn < USERMIN {
		return -EINVAL
	}
	len = roundup(len, PGSIZE)
	for i := 0; i < len; i += PGSIZE {
		p := addrn + i
		if p < USERMIN {
			return -EINVAL
		}
		if !proc.page_remove(p) {
			return -EINVAL
		}
	}
	return 0
}

func copyfd(fd *fd_t) *fd_t {
	nfd := &fd_t{}
	*nfd = *fd
	// increment reader/writer count
	switch fd.file.ftype {
	case PIPE:
		var ch chan int
		if fd.perms == FD_READ {
			ch = fd.file.pipe.readers
		} else {
			ch = fd.file.pipe.writers
		}
		ch <- 1
	case INODE:
		fs_memref(fd.file, fd.perms)
	case SOCKET:
		fd.file.Lock()
		fd.file.sock.open++
		fd.file.Unlock()
	}
	return nfd
}

func sys_dup2(proc *proc_t, oldn, newn int) int{
	if oldn == newn {
		return newn
	}
	ofd, ok := proc.fds[oldn]
	if !ok {
		return -EBADF
	}
	_, ok = proc.fds[newn]
	if ok {
		sys_close(proc, newn)
	}
	nfd := copyfd(ofd)
	nfd.perms &^= FD_CLOEXEC
	proc.fds[newn] = nfd
	return newn
}

func sys_stat(proc *proc_t, pathn, statn int) int {
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	buf := &stat_t{}
	buf.init()
	err := fs_stat(path, buf, proc.cwd)
	if err != 0 {
		return err
	}
	ok = proc.usercopy(buf.data, statn)
	if !ok {
		return -EFAULT
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
	err := fs_fstat(fd.file, buf)
	if err != 0 {
		return err
	}
	ok = proc.usercopy(buf.data, statn)
	if !ok {
		return -EFAULT
	}
	return 0
}

func sys_lseek(proc *proc_t, fdn, off, whence int) int {
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	if fd.file.ftype != INODE {
		return -ESPIPE
	}

	fd.file.Lock()
	defer fd.file.Unlock()

	switch whence {
	case SEEK_SET:
		fd.file.offset = off
	case SEEK_CUR:
		fd.file.offset += off
	case SEEK_END:
		st := &stat_t{}
		st.init()
		if err := fs_fstat(fd.file, st); err != 0 {
			panic("must succeed")
		}
		fd.file.offset = st.size() + off
	default:
		return -EINVAL
	}
	if fd.file.offset < 0 {
		fd.file.offset = 0
	}
	return fd.file.offset
}

func sys_pipe2(proc *proc_t, pipen, flags int) int {
	ok := proc.usermapped(pipen, 8)
	if !ok {
		return -EFAULT
	}
	rfd, rf := proc.fd_new(FD_READ)
	wfd, wf := proc.fd_new(FD_WRITE)

	pipef := pipe_new()
	rf.file = pipef
	wf.file = pipef

	if flags & O_CLOEXEC != 0 {
		rf.perms |= FD_CLOEXEC
		wf.perms |= FD_CLOEXEC
	}

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
	ret.ftype = PIPE
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

func sys_rename(proc *proc_t, oldn int, newn int) int {
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
	return fs_rename(old, new, proc.cwd)
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

func sys_gettimeofday(proc *proc_t, timevaln int) int {
	tvalsz := 16
	if !proc.usermapped(timevaln, tvalsz) {
		return -EFAULT
	}
	now := time.Now()
	buf := make([]uint8, tvalsz)
	writen(buf, 8, 0, now.Second())
	writen(buf, 8, 8, now.Nanosecond() / 1000)
	if !proc.usercopy(buf, timevaln) {
		panic("must succeed")
	}
	return 0
}

func mkdev(maj, min int) int {
	return maj << 32 | min
}

func unmkdev(di int) (int, int) {
	d := uint(di)
	return int(d >> 32), int(uint32(d))
}

func sys_mknod(proc *proc_t, pathn, moden, devn int) int {
	dsplit := func(n int) (int, int) {
		a := uint(n)
		maj := a >> 32
		min := uint32(a)
		return int(maj), int(min)
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
	maj, min := dsplit(devn)
	_, err = fs_open(path, O_CREAT, 0, proc.cwd, maj, min)
	if err != 0 {
		return err
	}
	return 0
}

func sys_nanosleep(proc *proc_t, sleeptsn, remaintsn int) int {
	timespecsz := 16
	if !proc.usermapped(sleeptsn, timespecsz) {
		return -EFAULT
	}

	secs, ok1 := proc.userreadn(sleeptsn, 8)
	nsecs, ok2 := proc.userreadn(sleeptsn + 8, 8)
	if !ok1 || !ok2 {
		return -EFAULT
	}
	if secs < 0 || nsecs < 0 {
		return -EINVAL
	}
	tot := time.Duration(secs) * time.Second
	tot += time.Duration(nsecs) * time.Nanosecond
	<- time.After(tot)
	return 0
}

func sys_getpid(proc *proc_t) int {
	return proc.pid
}

func sys_socket(proc *proc_t, domain, typ, proto int) int {
	if domain != AF_UNIX || typ != SOCK_DGRAM {
		fmt.Printf("only AF_UNIX + SOCK_DGRAM is supported for now")
		return -ENOSYS
	}
	// no permissions yet
	fdn, fd := proc.fd_new(0)
	fd.file = &file_t{ftype: SOCKET}
	if typ == SOCK_DGRAM {
		fd.file.sock.dgram = true
	} else {
		fd.file.sock.stream = true
	}
	fd.file.sock.open = 1
	return fdn
}

func sys_sendto(proc *proc_t, fdn, bufn, flaglen, sockaddrn, socklen int) int {
	fd, ok := proc.fds[fdn]
	if !ok || fd.file.ftype != SOCKET {
		return -EBADF
	}
	flags := uint(uint32(flaglen))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	if !proc.usermapped(bufn, buflen) {
		return -EFAULT
	}
	if !proc.usermapped(sockaddrn, socklen) {
		return -EFAULT
	}
	if !fd.file.sock.dgram {
		panic("no imp")
	}
	poff := 2
	plen := socklen - poff
	if plen <= 0 {
		return -ENOENT
	}
	path, ok, toolong := proc.userstr(sockaddrn + poff, plen)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	// get sund id from file
	file, err := fs_open(path, 0, 0, proc.cwd, 0, 0)
	if err != 0 {
		return err
	}
	if file.ftype != DEV || file.dev.major != D_SUN {
		return -EPERM
	}

	// lookup sid, get admission to write. we must get admission in order
	// to ensure that after the socket daemon terminates 1) no writers are
	// left blocking indefinitely and 2) that no new writers attempt to
	// write to a channel that no one is listening on
	allsunds.Lock()
	sund, ok := allsunds.m[sunid_t(file.dev.minor)]
	if ok {
		sund.adm <- true
	}
	allsunds.Unlock()
	if !ok {
		return -ECONNREFUSED
	}

	c := 0
	var data []uint8
	for c < buflen {
		src, ok := proc.userdmap8(bufn + c)
		if !ok {
			sund.in <- sunbuf_t{}
			return -EFAULT
		}
		left := buflen - c
		if len(src) > left {
			src = src[:left]
		}
		data = append(data, src...)
		c += len(src)
	}

	sbuf := sunbuf_t{from: file.sock.sunaddr, data: data}
	sund.in <- sbuf
	return <- sund.inret
}

func sys_recvfrom(proc *proc_t, fdn, bufn, flaglen, sockaddrn,
    socklenn int) int {
	fd, ok := proc.fds[fdn]
	if !ok || fd.file.ftype != SOCKET {
		return -EBADF
	}
	flags := uint(uint32(flaglen))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	if !proc.usermapped(bufn, buflen) {
		return -EFAULT
	}
	fillfrom := sockaddrn != 0
	slen := 0
	if fillfrom {
		l, ok := proc.userreadn(socklenn, 8)
		if !ok {
			return -EFAULT
		}
		if l > 0 {
			slen = l
		}
		if !proc.usermapped(sockaddrn, slen) {
			return -EFAULT
		}
	}

	if !fd.file.sock.dgram {
		panic("no imp")
	}
	sund := fd.file.sock.sunaddr.sund
	sund.outsz <- buflen
	sbuf := <- sund.out

	if !proc.usercopy(sbuf.data, bufn) {
		return -EFAULT
	}

	if fillfrom {
		sa := fd.file.sock.sunaddr.sockaddr_un(slen)
		if !proc.usercopy(sa, sockaddrn) {
			return -EFAULT
		}
		// write actual size
		if !proc.userwriten(socklenn, 8, len(sa)) {
			return -EFAULT
		}
	}

	return len(sbuf.data)
}

func sys_bind(proc *proc_t, fdn, sockaddrn, socklen int) int {
	fd, ok := proc.fds[fdn]
	if !ok {
		return -EBADF
	}
	if fd.file.ftype != SOCKET {
		return -EBADF
	}
	poff := 2
	plen := socklen - poff
	if plen <= 0 {
		return -ENOENT
	}
	path, ok, toolong := proc.userstr(sockaddrn + poff, plen)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	// try to create the specified file as a special device
	sid := sun_new()
	_, err := fs_open(path, O_CREAT | O_EXCL, 0, proc.cwd, D_SUN, int(sid))
	if err != 0 {
		return err
	}
	fd.file.sock.sunaddr.id = sid
	fd.file.sock.sunaddr.path = path
	fd.file.sock.sunaddr.sund = sun_start(sid)
	fd.file.sock.bound = true
	return 0
}

type sock_t struct {
	dgram	bool
	stream	bool
	sunaddr	sunaddr_t
	open	int
	bound	bool
}

type sunid_t int

// globally unique unix socket minor number
var sunid_cur	sunid_t

type sunaddr_t struct {
	id	sunid_t
	path	string
	sund	*sund_t
}

// used by recvfrom to fill in "fromaddr"
func (sa *sunaddr_t) sockaddr_un(sz int) []uint8 {
	if sz < 0 {
		panic("negative size")
	}
	ret := make([]uint8, 2)
	// len
	writen(ret, 1, 0, len(sa.path))
	// family
	writen(ret, 1, 1, AF_UNIX)
	// path
	ret = append(ret, sa.path...)
	ret = append(ret, 0)
	if sz > len(ret) {
		sz = len(ret)
	}
	return ret[:sz]
}

type sunbuf_t struct {
	from	sunaddr_t
	data	[]uint8
}

type sund_t struct {
	adm	chan bool
	finish	chan bool
	outsz	chan int
	out	chan sunbuf_t
	inret	chan int
	in	chan sunbuf_t
}

func sun_new() sunid_t {
	sunid_cur++
	return sunid_cur
}

type allsund_t struct {
	m	map[sunid_t]*sund_t
	sync.Mutex
}

var allsunds = allsund_t{m: map[sunid_t]*sund_t{}}

func sun_start(sid sunid_t) *sund_t {
	if _, ok := allsunds.m[sid]; ok {
		panic("sund exists")
	}
	ns := &sund_t{}
	adm := make(chan bool)
	finish := make(chan bool)
	outsz := make(chan int)
	out := make(chan sunbuf_t)
	inret := make(chan int)
	in := make(chan sunbuf_t)

	ns.adm = adm
	ns.finish = finish
	ns.outsz = outsz
	ns.out = out
	ns.inret = inret
	ns.in = in

	go func() {
		bstart := make([]sunbuf_t, 0, 10)
		buf := bstart
		buflen := func() int {
			ret := 0
			for _, b := range buf {
				ret += len(b.data)
			}
			return ret
		}
		bufadd := func(n sunbuf_t) {
			if len(n.data) == 0 {
				return
			}
			buf = append(buf, n)
		}

		bufdeq := func(sz int) sunbuf_t {
			if sz == 0 {
				return sunbuf_t{}
			}
			ret := buf[0]
			buf = buf[1:]
			if len(buf) == 0 {
				buf = bstart
			}
			return ret
		}

		done := false
		sunbufsz := 512
		admitted := 0
		var toutsz chan int
		tin := in
		for !done {
			select {
			case <- finish:
				done = true
			case <- adm:
				admitted++
			// writing
			case sbuf := <- tin:
				if buflen() >= sunbufsz {
					panic("buf full")
				}
				if len(sbuf.data) > 2*sunbufsz {
					inret <- -EMSGSIZE
					break
				}
				bufadd(sbuf)
				inret <- len(sbuf.data)
				admitted--
			// reading
			case sz := <- toutsz:
				if buflen() == 0 {
					panic("no data")
				}
				d := bufdeq(sz)
				out <- d
			}

			// block writes if socket is full
			// block reads if socket is empty
			if buflen() > 0 {
				toutsz = outsz
			} else {
				toutsz = nil
			}
			if buflen() < sunbufsz {
				tin = in
			} else {
				tin = nil
			}
		}

		for i := admitted; i > 0; i-- {
			<- in
			inret <- -ECONNREFUSED
		}
	}()

	allsunds.Lock()
	allsunds.m[sid] = ns
	allsunds.Unlock()

	return ns
}

func sys_fork(parent *proc_t, ptf *[TFSIZE]int, tforkp int, flags int) int {
	tmp := flags & (FORK_THREAD | FORK_PROCESS)
	if tmp != FORK_THREAD && tmp != FORK_PROCESS {
		return -EINVAL
	}
	sztfork_t := 24
	if tmp == FORK_THREAD && !parent.usermapped(tforkp, sztfork_t) {
		fmt.Printf("no tforkp thingy\n")
		return -EFAULT
	}

	mkproc := flags & FORK_PROCESS != 0
	var child *proc_t
	var childtid tid_t
	var childch chan parmsg_t
	var ret int

	// copy parents trap frame
	chtf := [TFSIZE]int{}
	chtf = *ptf

	if mkproc {
		child = proc_new(fmt.Sprintf("%s's child", parent.name),
		    parent.cwd)
		child.pwaiti = &parent.waiti

		// copy fd table
		for k, v := range parent.fds {
			child.fds[k] = copyfd(v)
		}

		pmap, p_pmap := pmap_copy_par(parent.pmap, child.pages,
		    parent.pages)

		// copy user->phys mappings too
		for k, v := range parent.upages {
			child.upages[k] = v
		}

		// tlb invalidation is not necessary for the parent because
		// trap always switches to the kernel pmap, for now.

		child.pmap = pmap
		child.p_pmap = p_pmap
		childtid = child.tid0
		ret = child.pid
		childch = parent.waiti.getch
	} else {
		// XXX XXX XXX need to copy FPU state from parent thread to
		// child thread

		// validate tfork struct
		tcb, ok1      := parent.userreadn(tforkp + 0, 8)
		tidaddrn, ok2 := parent.userreadn(tforkp + 8, 8)
		stack, ok3    := parent.userreadn(tforkp + 16, 8)
		if !ok1 || !ok2 || !ok3 {
			panic("unexpected unmap")
		}
		writetid := tidaddrn != 0
		if writetid && !parent.usermapped(tidaddrn, 8) {
			fmt.Printf("nonzero tid but bad addr\n")
			return -EFAULT
		}
		if tcb != 0 {
			chtf[TF_FSBASE] = tcb
		}
		if !parent.usermapped(stack - 8, 8) {
			fmt.Printf("stack not mapped\n")
			return -EFAULT
		}

		child = parent
		childtid = parent.tid_new()
		childch = child.twaiti.getch

		v := int(childtid)
		chtf[TF_RSP] = stack
		if writetid && !parent.userwriten(tidaddrn, 8, v) {
			panic("unexpected unmap")
		}
		ret = v
	}

	// inform wait daemon of new process/thread
	var pm parmsg_t
	pm.init(ret, true)
	childch <- pm
	resp := <- pm.ack
	if resp.err != 0 {
		panic("add child")
	}

	chtf[TF_RAX] = 0

	child.sched_add(&chtf, childtid)

	return ret
}

func sys_pgfault(proc *proc_t, tid tid_t, pte *int, faultaddr int,
    tf *[TFSIZE]int) {
	cow := *pte & PTE_COW != 0
	wascow := *pte & PTE_WASCOW != 0
	if cow && wascow {
		panic("invalid state")
	}

	if cow {
		// copy page
		dst, p_dst := pg_new(proc.pages)
		p_src := *pte & PTE_ADDR
		src := dmap(p_src)
		*dst = *src

		// insert new page into pmap
		va := faultaddr & PGMASK
		perms := (*pte & PTE_FLAGS) & ^PTE_COW
		perms |= PTE_W | PTE_WASCOW
		// XXX TLB shootdown
		proc.page_insert(va, dst, p_dst, perms, false)
	} else if wascow {
		// land here if two threads fault on same page
		if wascow && *pte & PTE_W == 0 {
			panic("handled but read-only")
		}
	} else {
		panic("fault on non-cow page")
	}

	// set process as runnable again
	proc.sched_runnable(nil, tid)
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

// XXX a multithreaded process that execs is broken; POSIX2008 says that all
// threads should terminate before exec.
func sys_execv1(proc *proc_t, tf *[TFSIZE]int, paths string,
    args []string) int {
	file, err := fs_open(paths, O_RDONLY, 0, proc.cwd, 0, 0)
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
	file_close(file, 0)
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
	numstkpages := 2
	for i := 0; i < numstkpages; i++ {
		stack, p_stack := pg_new(proc.pages)
		proc.page_insert(stackva - PGSIZE*(i+1), stack, p_stack,
		    PTE_U | PTE_W, true)
	}

	tls, istls := elf_load(proc, elf)
	// XXX make insertargs not fail by using more than a page...
	argc, argv, ok := insertargs(proc, args)
	if !ok {
		// restore old process
		proc.pages = opages
		proc.upages = oupages
		proc.pmap = opmap
		proc.p_pmap = op_pmap
		return -EINVAL
	}

	// create two copies of TLS section: one for the fresh copy and one for
	// thread 0
	freshtls := 0
	t0tls := 0
	if istls {
		l := roundup(tls.addr + tls.size, PGSIZE)
		l -= rounddown(tls.addr, PGSIZE)

		mktls := func(writable bool) int {
			ret := proc.unusedva(0, l)
			perms := PTE_U
			if writable {
				perms |= PTE_W
			}
			for i := 0; i < l; i += PGSIZE {
				pg, p_pg := pg_new(proc.pages)
				proc.page_insert(ret + i, pg, p_pg, perms, true)
			}
			for i := 0; i < l; i += PGSIZE {
				src, ok1 := proc.userdmap(tls.addr + i)
				dst, ok2 := proc.userdmap(ret + i)
				if !ok1 || !ok2 {
					panic("must succeed")
				}
				*dst = *src
			}

			// zero the tbss
			zl := tls.size - tls.copylen
			if zl < 0 {
				panic("bad zero len")
			}
			startva := ret + tls.copylen
			var z []uint8
			for c := 0; c < zl; c += len(z) {
				z, ok = proc.userdmap8(startva + c)
				if !ok {
					panic("must succeed")
				}
				left := zl - c
				if len(z) > left {
					z = z[:left]
				}
				for i := range z {
					z[i] = 0
				}
			}
			return ret
		}
		freshtls = mktls(false)
		t0tls = mktls(true)

		// amd64 sys 5 abi specifies that the tls pointer references to
		// the first invalid word past the end of the tls
		t0tls += tls.size
	}

	// close fds marked with CLOEXEC
	for fdn, fd := range proc.fds {
		if fd.perms & FD_CLOEXEC != 0 {
			if sys_close(proc, fdn) != 0 {
				panic("close")
			}
		}
	}

	// put special struct on stack: fresh tls start, tls len, and tls0
	// pointer
	words := 3
	buf := make([]uint8, words*8)
	writen(buf, 8, 0, freshtls)
	writen(buf, 8, 8, tls.size)
	writen(buf, 8, 16, t0tls)
	bufdest := stackva - words*8
	tls0addr := bufdest + 2*8
	if !proc.usercopy(buf, bufdest) {
		panic("must succeed")
	}

	// commit new image state
	tf[TF_RSP] = bufdest
	tf[TF_RIP] = elf.entry()
	tf[TF_RFLAGS] = TF_FL_IF
	ucseg := 4
	udseg := 5
	tf[TF_CS] = ucseg << 3 | 3
	tf[TF_SS] = udseg << 3 | 3
	tf[TF_RDI] = argc
	tf[TF_RSI] = argv
	tf[TF_RDX] = bufdest
	tf[TF_FSBASE] = tls0addr
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
	vdata, ok := proc.userdmap8(argstart)
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

func sys_exit(proc *proc_t, tid tid_t, status int) {
	// set doomed to all other threads die
	proc.doomall()
	proc.thread_dead(tid, status, true)
}

func sys_threxit(proc *proc_t, tid tid_t, status int) {
	proc.thread_dead(tid, status, false)
}

func sys_wait4(proc *proc_t, wpid, statusp, options, rusagep,
    threadwait int) int {
	wi := proc.waiti

	ok := proc.usermapped(statusp, 4)
	if !ok {
		return -EFAULT
	}

	if wpid == WAIT_MYPGRP {
		panic("no imp")
	}

	ch := wi.getch
	if threadwait != 0 {
		ch = proc.twaiti.getch
	}

	var pm parmsg_t
	pm.init(wpid, false)

	ch <- pm
	resp := <- pm.ack
	if resp.err != 0 {
		return resp.err
	}
	if threadwait == 0 {
		proc.userwriten(statusp, 4, resp.status)
	} else {
		proc.userwriten(statusp, 8, resp.status)
	}
	return resp.pid
}

type childmsg_t struct {
	pid	int
	status	int
	err	int
}

type parmsg_t struct {
	forked	bool
	pid	int
	ack	chan childmsg_t
}

func (pm *parmsg_t) init(pid int, forked bool) {
	pm.pid = pid
	pm.ack = make(chan childmsg_t)
	pm.forked = forked
}

type waitinfo_t struct {
	addch	chan childmsg_t
	getch	chan parmsg_t
}

type cinfo_t struct {
	pid	int
	status 	int
	reaped	bool
	dead	bool
}

func (w *waitinfo_t) init() {
	w.addch = make(chan childmsg_t)
	w.getch = make(chan parmsg_t)
	go w.daemon()
}

func (w *waitinfo_t) finish() {
	close(w.getch)
}

func (w *waitinfo_t) daemon() {
	add := w.addch
	get := w.getch

	reaps := 0
	childs := make(map[int]cinfo_t)
	waiters := make(map[int]parmsg_t)
	anys := make([]parmsg_t, 0)

	addanys := func(pm parmsg_t) {
		anys = append(anys, pm)
	}

	done := false
	for !done {
		select {
		case cm := <- add:
			// child has terminated
			ci, ok := childs[cm.pid]
			if !ok || ci.dead || ci.reaped {
				panic("what")
			}
			ci.dead = true
			ci.status = cm.status
			childs[cm.pid] = ci

		case pm, ok := <- get:
			if !ok {
				// parent terminated, no more waits or forks
				if len(anys) != 0 || len(waiters) != 0 {
					panic("threads?")
				}
				done = true
				break
			}
			// new child spawned?
			if pm.forked {
				var nci cinfo_t
				nci.pid = pm.pid
				nci.reaped = false
				nci.dead = false
				childs[pm.pid] = nci
				pm.ack <- childmsg_t{}
				break
			}

			// get status of child
			if reaps == len(childs) {
				// no unreaped children
				pm.ack <- childmsg_t{err: -ECHILD}
				break
			}

			if pm.pid == WAIT_ANY {
				addanys(pm)
				break
			}

			ci, ok := childs[pm.pid]
			if !ok || ci.reaped {
				// no such child or already waited for
				pm.ack <- childmsg_t{err: -ECHILD}
				break
			}
			waiters[pm.pid] = pm
		}

		// check for new statuses
		var cm childmsg_t
		for pid, ci := range childs {
			if ci.reaped || !ci.dead {
				continue
			}
			cm.pid = ci.pid
			cm.status = ci.status
			if len(anys) > 0 {
				ci.reaped = true
				childs[pid] = ci
				reaps++
				pm := anys[0]
				anys = anys[1:]
				pm.ack <- cm
				continue
			}
			pm, ok := waiters[pid]
			if !ok {
				continue
			}
			ci.reaped = true
			childs[pid] = ci
			reaps++
			pm.ack <- cm
			delete(waiters, pid)
		}
	}

	deadc := 0
	for _, c := range childs {
		if c.dead {
			deadc++
		}
	}

	// parent exited, drain remaining statuses
	for i := deadc; i < len(childs); i++ {
		<- add
	}
}

func sys_kill(proc *proc_t, pid, sig int) int {
	if sig != SIGKILL {
		panic("no imp")
	}
	p, ok := proc_check(pid)
	if !ok {
		return -ESRCH
	}
	p.doomall()
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

	newcwd, err := fs_open(path, O_RDONLY | O_DIRECTORY, 0, proc.cwd, 0, 0)
	if err != 0 {
		return err
	}
	file_close(proc.cwd, 0)
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
		8, // 4 - rdev
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

func (st *stat_t) size() int {
	size, off := fieldinfo(st.sizes, 3)
	return readn(st.data, size, off)
}

func (st *stat_t) wsize(v int) {
	size, off := fieldinfo(st.sizes, 3)
	writen(st.data, size, off, v)
}

func (st *stat_t) rdev() int {
	size, off := fieldinfo(st.sizes, 4)
	return readn(st.data, size, off)
}

func (st *stat_t) wrdev(v int) {
	size, off := fieldinfo(st.sizes, 4)
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

// XXX remove unsafes
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

type etls_t struct {
	addr	int
	size	int
	copylen	int
}

// returns TLS struct and whether ELF has TLS
func elf_load(p *proc_t, e *elf_t) (etls_t, bool) {
	PT_LOAD := 1
	PT_TLS  := 7
	tls := etls_t{}
	istls := false
	for _, hdr := range e.headers() {
		// XXX get rid of worthless user program segments
		if hdr.etype == PT_TLS {
			istls = true
			tls.addr = hdr.vaddr
			tls.size = roundup(hdr.memsz, 8)
			tls.copylen = hdr.filesz
		} else if hdr.etype == PT_LOAD && hdr.vaddr >= USERMIN {
			elf_segload(p, &hdr)
		}
	}
	return tls, istls
}
