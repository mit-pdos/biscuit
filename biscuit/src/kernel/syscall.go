package main

import "fmt"
import "math/rand"
import "runtime"
import "runtime/debug"
import "runtime/pprof"
import "sort"
import "sync"
import "sync/atomic"
import "time"
import "unsafe"

import "bnet"
import "bounds"
import "bpath"
import "circbuf"
import "defs"
import "fd"
import "fdops"
import "fs"
import "limits"
import "mem"
import "proc"
import "res"
import "stat"
import "tinfo"
import "ustr"
import "util"
import "vm"

var _sysbounds = []*res.Res_t{
	defs.SYS_READ:       bounds.Bounds(bounds.B_SYS_READ),
	defs.SYS_WRITE:      bounds.Bounds(bounds.B_SYS_WRITE),
	defs.SYS_OPEN:       bounds.Bounds(bounds.B_SYS_OPEN),
	defs.SYS_CLOSE:      bounds.Bounds(bounds.B_SYSCALL_T_SYS_CLOSE),
	defs.SYS_STAT:       bounds.Bounds(bounds.B_SYS_STAT),
	defs.SYS_FSTAT:      bounds.Bounds(bounds.B_SYS_FSTAT),
	defs.SYS_POLL:       bounds.Bounds(bounds.B_SYS_POLL),
	defs.SYS_LSEEK:      bounds.Bounds(bounds.B_SYS_LSEEK),
	defs.SYS_MMAP:       bounds.Bounds(bounds.B_SYS_MMAP),
	defs.SYS_MUNMAP:     bounds.Bounds(bounds.B_SYS_MUNMAP),
	defs.SYS_SIGACT:     bounds.Bounds(bounds.B_SYS_SIGACTION),
	defs.SYS_READV:      bounds.Bounds(bounds.B_SYS_READV),
	defs.SYS_WRITEV:     bounds.Bounds(bounds.B_SYS_WRITEV),
	defs.SYS_ACCESS:     bounds.Bounds(bounds.B_SYS_ACCESS),
	defs.SYS_DUP2:       bounds.Bounds(bounds.B_SYS_DUP2),
	defs.SYS_PAUSE:      bounds.Bounds(bounds.B_SYS_PAUSE),
	defs.SYS_GETPID:     bounds.Bounds(bounds.B_SYS_GETPID),
	defs.SYS_GETPPID:    bounds.Bounds(bounds.B_SYS_GETPPID),
	defs.SYS_SOCKET:     bounds.Bounds(bounds.B_SYS_SOCKET),
	defs.SYS_CONNECT:    bounds.Bounds(bounds.B_SYS_CONNECT),
	defs.SYS_ACCEPT:     bounds.Bounds(bounds.B_SYS_ACCEPT),
	defs.SYS_SENDTO:     bounds.Bounds(bounds.B_SYS_SENDTO),
	defs.SYS_RECVFROM:   bounds.Bounds(bounds.B_SYS_RECVFROM),
	defs.SYS_SOCKPAIR:   bounds.Bounds(bounds.B_SYS_SOCKETPAIR),
	defs.SYS_SHUTDOWN:   bounds.Bounds(bounds.B_SYS_SHUTDOWN),
	defs.SYS_BIND:       bounds.Bounds(bounds.B_SYS_BIND),
	defs.SYS_LISTEN:     bounds.Bounds(bounds.B_SYS_LISTEN),
	defs.SYS_RECVMSG:    bounds.Bounds(bounds.B_SYS_RECVMSG),
	defs.SYS_SENDMSG:    bounds.Bounds(bounds.B_SYS_SENDMSG),
	defs.SYS_GETSOCKOPT: bounds.Bounds(bounds.B_SYS_GETSOCKOPT),
	defs.SYS_SETSOCKOPT: bounds.Bounds(bounds.B_SYS_SETSOCKOPT),
	defs.SYS_FORK:       bounds.Bounds(bounds.B_SYS_FORK),
	defs.SYS_EXECV:      bounds.Bounds(bounds.B_SYS_EXECV),
	defs.SYS_EXIT:       bounds.Bounds(bounds.B_SYSCALL_T_SYS_EXIT),
	defs.SYS_WAIT4:      bounds.Bounds(bounds.B_SYS_WAIT4),
	defs.SYS_KILL:       bounds.Bounds(bounds.B_SYS_KILL),
	defs.SYS_FCNTL:      bounds.Bounds(bounds.B_SYS_FCNTL),
	defs.SYS_TRUNC:      bounds.Bounds(bounds.B_SYS_TRUNCATE),
	defs.SYS_FTRUNC:     bounds.Bounds(bounds.B_SYS_FTRUNCATE),
	defs.SYS_GETCWD:     bounds.Bounds(bounds.B_SYS_GETCWD),
	defs.SYS_CHDIR:      bounds.Bounds(bounds.B_SYS_CHDIR),
	defs.SYS_RENAME:     bounds.Bounds(bounds.B_SYS_RENAME),
	defs.SYS_MKDIR:      bounds.Bounds(bounds.B_SYS_MKDIR),
	defs.SYS_LINK:       bounds.Bounds(bounds.B_SYS_LINK),
	defs.SYS_UNLINK:     bounds.Bounds(bounds.B_SYS_UNLINK),
	defs.SYS_GETTOD:     bounds.Bounds(bounds.B_SYS_GETTIMEOFDAY),
	defs.SYS_GETRLMT:    bounds.Bounds(bounds.B_SYS_GETRLIMIT),
	defs.SYS_GETRUSG:    bounds.Bounds(bounds.B_SYS_GETRUSAGE),
	defs.SYS_MKNOD:      bounds.Bounds(bounds.B_SYS_MKNOD),
	defs.SYS_SETRLMT:    bounds.Bounds(bounds.B_SYS_SETRLIMIT),
	defs.SYS_SYNC:       bounds.Bounds(bounds.B_SYS_SYNC),
	defs.SYS_REBOOT:     bounds.Bounds(bounds.B_SYS_REBOOT),
	defs.SYS_NANOSLEEP:  bounds.Bounds(bounds.B_SYS_NANOSLEEP),
	defs.SYS_PIPE2:      bounds.Bounds(bounds.B_SYS_PIPE2),
	defs.SYS_PROF:       bounds.Bounds(bounds.B_SYS_PROF),
	defs.SYS_THREXIT:    bounds.Bounds(bounds.B_SYS_THREXIT),
	defs.SYS_INFO:       bounds.Bounds(bounds.B_SYS_INFO),
	defs.SYS_PREAD:      bounds.Bounds(bounds.B_SYS_PREAD),
	defs.SYS_PWRITE:     bounds.Bounds(bounds.B_SYS_PWRITE),
	defs.SYS_FUTEX:      bounds.Bounds(bounds.B_SYS_FUTEX),
	defs.SYS_GETTID:     bounds.Bounds(bounds.B_SYS_GETTID),
}

// Implements Syscall_i
type syscall_t struct {
}

var sys = &syscall_t{}

func (s *syscall_t) Syscall(p *proc.Proc_t, tid defs.Tid_t, tf *[defs.TFSIZE]uintptr) int {

	if p.Doomed() {
		// this process has been killed
		p.Reap_doomed(tid)
		return 0
	}

	sysno := int(tf[defs.TF_RAX])

	//lim, ok := _sysbounds[sysno]
	//if !ok {
	//	panic("bad limit")
	//}
	lim := _sysbounds[sysno]
	//if lim == 0 {
	//	panic("bad limit")
	//}
	if !res.Resadd(lim) {
		//fmt.Printf("syscall res failed\n")
		return int(-defs.ENOHEAP)
	}

	a1 := int(tf[defs.TF_RDI])
	a2 := int(tf[defs.TF_RSI])
	a3 := int(tf[defs.TF_RDX])
	a4 := int(tf[defs.TF_RCX])
	a5 := int(tf[defs.TF_R8])

	var ret int
	switch sysno {
	case defs.SYS_READ:
		ret = sys_read(p, a1, a2, a3)
	case defs.SYS_WRITE:
		ret = sys_write(p, a1, a2, a3)
	case defs.SYS_OPEN:
		ret = sys_open(p, a1, a2, a3)
	case defs.SYS_CLOSE:
		ret = s.Sys_close(p, a1)
	case defs.SYS_STAT:
		ret = sys_stat(p, a1, a2)
	case defs.SYS_FSTAT:
		ret = sys_fstat(p, a1, a2)
	case defs.SYS_POLL:
		ret = sys_poll(p, tid, a1, a2, a3)
	case defs.SYS_LSEEK:
		ret = sys_lseek(p, a1, a2, a3)
	case defs.SYS_MMAP:
		ret = sys_mmap(p, a1, a2, a3, a4, a5)
	case defs.SYS_MUNMAP:
		ret = sys_munmap(p, a1, a2)
	case defs.SYS_READV:
		ret = sys_readv(p, a1, a2, a3)
	case defs.SYS_WRITEV:
		ret = sys_writev(p, a1, a2, a3)
	case defs.SYS_SIGACT:
		ret = sys_sigaction(p, a1, a2, a3)
	case defs.SYS_ACCESS:
		ret = sys_access(p, a1, a2)
	case defs.SYS_DUP2:
		ret = sys_dup2(p, a1, a2)
	case defs.SYS_PAUSE:
		ret = sys_pause(p)
	case defs.SYS_GETPID:
		ret = sys_getpid(p, tid)
	case defs.SYS_GETPPID:
		ret = sys_getppid(p, tid)
	case defs.SYS_SOCKET:
		ret = sys_socket(p, a1, a2, a3)
	case defs.SYS_CONNECT:
		ret = sys_connect(p, a1, a2, a3)
	case defs.SYS_ACCEPT:
		ret = sys_accept(p, a1, a2, a3)
	case defs.SYS_SENDTO:
		ret = sys_sendto(p, a1, a2, a3, a4, a5)
	case defs.SYS_RECVFROM:
		ret = sys_recvfrom(p, a1, a2, a3, a4, a5)
	case defs.SYS_SOCKPAIR:
		ret = sys_socketpair(p, a1, a2, a3, a4)
	case defs.SYS_SHUTDOWN:
		ret = sys_shutdown(p, a1, a2)
	case defs.SYS_BIND:
		ret = sys_bind(p, a1, a2, a3)
	case defs.SYS_LISTEN:
		ret = sys_listen(p, a1, a2)
	case defs.SYS_RECVMSG:
		ret = sys_recvmsg(p, a1, a2, a3)
	case defs.SYS_SENDMSG:
		ret = sys_sendmsg(p, a1, a2, a3)
	case defs.SYS_GETSOCKOPT:
		ret = sys_getsockopt(p, a1, a2, a3, a4, a5)
	case defs.SYS_SETSOCKOPT:
		ret = sys_setsockopt(p, a1, a2, a3, a4, a5)
	case defs.SYS_FORK:
		ret = sys_fork(p, tf, a1, a2)
	case defs.SYS_EXECV:
		ret = sys_execv(p, tf, a1, a2)
	case defs.SYS_EXIT:
		status := a1 & 0xff
		status |= defs.EXITED
		s.Sys_exit(p, tid, status)
	case defs.SYS_WAIT4:
		ret = sys_wait4(p, tid, a1, a2, a3, a4, a5)
	case defs.SYS_KILL:
		ret = sys_kill(p, a1, a2)
	case defs.SYS_FCNTL:
		ret = sys_fcntl(p, a1, a2, a3)
	case defs.SYS_TRUNC:
		ret = sys_truncate(p, a1, uint(a2))
	case defs.SYS_FTRUNC:
		ret = sys_ftruncate(p, a1, uint(a2))
	case defs.SYS_GETCWD:
		ret = sys_getcwd(p, a1, a2)
	case defs.SYS_CHDIR:
		ret = sys_chdir(p, a1)
	case defs.SYS_RENAME:
		ret = sys_rename(p, a1, a2)
	case defs.SYS_MKDIR:
		ret = sys_mkdir(p, a1, a2)
	case defs.SYS_LINK:
		ret = sys_link(p, a1, a2)
	case defs.SYS_UNLINK:
		ret = sys_unlink(p, a1, a2)
	case defs.SYS_GETTOD:
		ret = sys_gettimeofday(p, a1)
	case defs.SYS_GETRLMT:
		ret = sys_getrlimit(p, a1, a2)
	case defs.SYS_GETRUSG:
		ret = sys_getrusage(p, a1, a2)
	case defs.SYS_MKNOD:
		ret = sys_mknod(p, a1, a2, a3)
	case defs.SYS_SETRLMT:
		ret = sys_setrlimit(p, a1, a2)
	case defs.SYS_SYNC:
		ret = sys_sync(p)
	case defs.SYS_REBOOT:
		ret = sys_reboot(p)
	case defs.SYS_NANOSLEEP:
		ret = sys_nanosleep(p, a1, a2)
	case defs.SYS_PIPE2:
		ret = sys_pipe2(p, a1, a2)
	case defs.SYS_PROF:
		ret = sys_prof(p, a1, a2, a3, a4)
	case defs.SYS_INFO:
		ret = sys_info(p, a1)
	case defs.SYS_THREXIT:
		sys_threxit(p, tid, a1)
	case defs.SYS_PREAD:
		ret = sys_pread(p, a1, a2, a3, a4)
	case defs.SYS_PWRITE:
		ret = sys_pwrite(p, a1, a2, a3, a4)
	case defs.SYS_FUTEX:
		ret = sys_futex(p, a1, a2, a3, a4, a5)
	case defs.SYS_GETTID:
		ret = sys_gettid(p, tid)
	default:
		fmt.Printf("unexpected syscall %v\n", sysno)
		s.Sys_exit(p, tid, defs.SIGNALED|defs.Mkexitsig(31))
	}
	return ret
}

// Implements Console_i
type console_t struct {
}

var console = &console_t{}

func (c *console_t) Cons_poll(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	cons.pollc <- pm
	return <- cons.pollret, 0
}

func (c *console_t) Cons_read(ub fdops.Userio_i, offset int) (int, defs.Err_t) {
	sz := ub.Remain()
	kdata, err := kbd_get(sz)
	if err != 0 {
		return 0, err
	}
	ret, err := ub.Uiowrite(kdata)
	if err != 0 || ret != len(kdata) {
		fmt.Printf("dropped keys!\n")
	}
	return ret, err
}

func (c *console_t) Cons_write(src fdops.Userio_i, off int) (int, defs.Err_t) {
	// merge into one buffer to avoid taking the console lock many times.
	// what a sweet optimization.
	utext := int8(0x17)
	big := make([]uint8, src.Totalsz())
	read, err := src.Uioread(big)
	if err != 0 {
		return 0, err
	}
	if read != src.Totalsz() {
		panic("short read")
	}
	runtime.Pmsga(&big[0], len(big), utext)
	return len(big), 0
}

func _fd_read(p *proc.Proc_t, fdn int) (*fd.Fd_t, defs.Err_t) {
	f, ok := p.Fd_get(fdn)
	if !ok {
		return nil, -defs.EBADF
	}
	if f.Perms&fd.FD_READ == 0 {
		return nil, -defs.EPERM
	}
	return f, 0
}

func _fd_write(p *proc.Proc_t, fdn int) (*fd.Fd_t, defs.Err_t) {
	f, ok := p.Fd_get(fdn)
	if !ok {
		return nil, -defs.EBADF
	}
	if f.Perms&fd.FD_WRITE == 0 {
		return nil, -defs.EPERM
	}
	return f, 0
}

func sys_read(p *proc.Proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	fd, err := _fd_read(p, fdn)
	if err != 0 {
		return int(err)
	}
	userbuf := p.Vm.Mkuserbuf(bufp, sz)

	ret, err := fd.Fops.Read(userbuf)
	if err != 0 {
		return int(err)
	}
	//proc.Ubpool.Put(userbuf)
	return ret
}

func sys_write(p *proc.Proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	fd, err := _fd_write(p, fdn)
	if err != 0 {
		return int(err)
	}
	userbuf := p.Vm.Mkuserbuf(bufp, sz)

	ret, err := fd.Fops.Write(userbuf)
	if err != 0 {
		return int(err)
	}
	//proc.Ubpool.Put(userbuf)
	return ret
}

func sys_open(p *proc.Proc_t, pathn int, _flags int, mode int) int {
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	flags := defs.Fdopt_t(_flags)
	temp := flags & (defs.O_RDONLY | defs.O_WRONLY | defs.O_RDWR)
	if temp != defs.O_RDONLY && temp != defs.O_WRONLY && temp != defs.O_RDWR {
		return int(-defs.EINVAL)
	}
	if temp == defs.O_RDONLY && flags&defs.O_TRUNC != 0 {
		return int(-defs.EINVAL)
	}
	fdperms := 0
	switch temp {
	case defs.O_RDONLY:
		fdperms = fd.FD_READ
	case defs.O_WRONLY:
		fdperms = fd.FD_WRITE
	case defs.O_RDWR:
		fdperms = fd.FD_READ | fd.FD_WRITE
	default:
		fdperms = fd.FD_READ
	}
	err = badpath(path)
	if err != 0 {
		return int(err)
	}
	file, err := thefs.Fs_open(path, flags, mode, p.Cwd, 0, 0)
	if err != 0 {
		return int(err)
	}
	if flags&defs.O_CLOEXEC != 0 {
		fdperms |= fd.FD_CLOEXEC
	}
	fdn, ok := p.Fd_insert(file, fdperms)
	if !ok {
		lhits++
		fd.Close_panic(file)
		return int(-defs.EMFILE)
	}
	return fdn
}

func sys_pause(p *proc.Proc_t) int {
	// no signals yet!
	var c chan bool
	select {
	case <-c:
	case <-tinfo.Current().Killnaps.Killch:
	}
	return -1
}

func (s *syscall_t) Sys_close(p *proc.Proc_t, fdn int) int {
	fd, ok := p.Fd_del(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	ret := fd.Fops.Close()
	return int(ret)
}

func sys_mmap(p *proc.Proc_t, addrn, lenn, protflags, fdn, offset int) int {
	if lenn == 0 {
		return int(-defs.EINVAL)
	}
	prot := uint(protflags) >> 32
	flags := uint(uint32(protflags))

	mask := defs.MAP_SHARED | defs.MAP_PRIVATE
	if flags&mask == 0 || flags&mask == mask {
		return int(-defs.EINVAL)
	}
	shared := flags&defs.MAP_SHARED != 0
	anon := flags&defs.MAP_ANON != 0
	fdmap := !anon
	if (fdmap && fdn < 0) || (fdmap && offset < 0) || (anon && fdn >= 0) {
		return int(-defs.EINVAL)
	}
	if flags&defs.MAP_FIXED != 0 {
		return int(-defs.EINVAL)
	}
	// OpenBSD allows mappings of only PROT_WRITE and read accesses that
	// fault-in the page cause a segfault while writes do not. Reads
	// following a write do not cause segfault (of course). POSIX
	// apparently requires an implementation to support only proc.PROT_WRITE,
	// but it seems better to disallow permission schemes that the CPU
	// cannot enforce.
	if prot&defs.PROT_READ == 0 {
		return int(-defs.EINVAL)
	}
	if prot == defs.PROT_NONE {
		panic("no imp")
		return p.Mmapi
	}

	var f *fd.Fd_t
	if fdmap {
		var ok bool
		f, ok = p.Fd_get(fdn)
		if !ok {
			return int(-defs.EBADF)
		}
		if f.Perms&fd.FD_READ == 0 ||
			(shared && prot&defs.PROT_WRITE != 0 &&
				f.Perms&fd.FD_WRITE == 0) {
			return int(-defs.EACCES)
		}
	}

	p.Vm.Lock_pmap()

	perms := vm.PTE_U
	if prot&defs.PROT_WRITE != 0 {
		perms |= vm.PTE_W
	}
	lenn = util.Roundup(lenn, mem.PGSIZE)
	// limit checks
	if lenn/int(mem.PGSIZE)+p.Vm.Vmregion.Pglen() > p.Ulim.Pages {
		p.Vm.Unlock_pmap()
		lhits++
		return int(-defs.ENOMEM)
	}
	if p.Vm.Vmregion.Novma >= p.Ulim.Novma {
		p.Vm.Unlock_pmap()
		lhits++
		return int(-defs.ENOMEM)
	}

	addr := p.Vm.Unusedva_inner(p.Mmapi, lenn)
	p.Mmapi = addr + lenn
	switch {
	case anon && shared:
		p.Vm.Vmadd_shareanon(addr, lenn, perms)
	case anon && !shared:
		p.Vm.Vmadd_anon(addr, lenn, perms)
	case fdmap:
		fops := f.Fops
		// vmadd_*file will increase the open count on the file
		if shared {
			p.Vm.Vmadd_sharefile(addr, lenn, perms, fops, offset,
				thefs)
		} else {
			p.Vm.Vmadd_file(addr, lenn, perms, fops, offset)
		}
	}
	tshoot := false
	// eagerly map anonymous pages, lazily-map file pages. our vm system
	// supports lazily-mapped private anonymous pages though.
	var ub int
	failed := false
	if anon {
		for i := 0; i < lenn; i += int(mem.PGSIZE) {
			_, p_pg, ok := physmem.Refpg_new()
			if !ok {
				failed = true
				break
			}
			ns, ok := p.Vm.Page_insert(addr+i, p_pg, perms, true, nil)
			if !ok {
				physmem.Refdown(p_pg)
				failed = true
				break
			}
			ub = i
			tshoot = tshoot || ns
		}
	}
	ret := addr
	if failed {
		for i := 0; i <= ub; i += mem.PGSIZE {
			p.Vm.Page_remove(addr + i)
		}
		// removing this region cannot create any more vm objects than
		// what this call to sys_mmap started with.
		if p.Vm.Vmregion.Remove(addr, lenn, p.Ulim.Novma) != 0 {
			panic("wut")
		}
		ret = int(-defs.ENOMEM)
	}
	// sys_mmap won't replace pages since it always finds unused VA space,
	// so the following TLB shootdown is never used.
	if tshoot {
		p.Vm.Tlbshoot(0, 1)
	}
	p.Vm.Unlock_pmap()
	return ret
}

func sys_munmap(p *proc.Proc_t, addrn, len int) int {
	if addrn&int(vm.PGOFFSET) != 0 || addrn < mem.USERMIN {
		return int(-defs.EINVAL)
	}
	p.Vm.Lock_pmap()
	defer p.Vm.Unlock_pmap()

	vmi1, ok1 := p.Vm.Vmregion.Lookup(uintptr(addrn))
	vmi2, ok2 := p.Vm.Vmregion.Lookup(uintptr(addrn+len) - 1)
	if !ok1 || !ok2 || vmi1.Pgn != vmi2.Pgn {
		return int(-defs.EINVAL)
	}

	err := p.Vm.Vmregion.Remove(addrn, len, p.Ulim.Novma)
	if err != 0 {
		lhits++
		return int(err)
	}
	// addrn must be page-aligned
	len = util.Roundup(len, mem.PGSIZE)
	for i := 0; i < len; i += mem.PGSIZE {
		a := addrn + i
		if a < mem.USERMIN {
			panic("how")
		}
		p.Vm.Page_remove(a)
	}
	pgs := len >> vm.PGSHIFT
	p.Vm.Tlbshoot(uintptr(addrn), pgs)
	return 0
}

func sys_readv(p *proc.Proc_t, fdn, _iovn, iovcnt int) int {
	fd, err := _fd_read(p, fdn)
	if err != 0 {
		return int(err)
	}
	iovn := uint(_iovn)
	iov := &vm.Useriovec_t{}
	if err := iov.Iov_init(&p.Vm, iovn, iovcnt); err != 0 {
		return int(err)
	}
	ret, err := fd.Fops.Read(iov)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_writev(p *proc.Proc_t, fdn, _iovn, iovcnt int) int {
	fd, err := _fd_write(p, fdn)
	if err != 0 {
		return int(err)
	}
	iovn := uint(_iovn)
	iov := &vm.Useriovec_t{}
	if err := iov.Iov_init(&p.Vm, iovn, iovcnt); err != 0 {
		return int(err)
	}
	ret, err := fd.Fops.Write(iov)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_sigaction(p *proc.Proc_t, sig, actn, oactn int) int {
	panic("no imp")
}

func sys_access(p *proc.Proc_t, pathn, mode int) int {
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	if mode == 0 {
		return int(-defs.EINVAL)
	}

	fsf, err := thefs.Fs_open_inner(path, defs.O_RDONLY, 0, p.Cwd, 0, 0)
	if err != 0 {
		return int(err)
	}

	// XXX no permissions yet
	//R_OK := 1 << 0
	//W_OK := 1 << 1
	//X_OK := 1 << 2
	ret := 0

	if thefs.Fs_close(fsf.Inum) != 0 {
		panic("must succeed")
	}
	return ret
}

func sys_dup2(p *proc.Proc_t, oldn, newn int) int {
	if oldn == newn {
		return newn
	}
	ofd, needclose, err := p.Fd_dup(oldn, newn)
	if err != 0 {
		return int(err)
	}
	if needclose {
		fd.Close_panic(ofd)
	}
	return newn
}

func sys_stat(p *proc.Proc_t, pathn, statn int) int {
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	buf := &stat.Stat_t{}
	err = thefs.Fs_stat(path, buf, p.Cwd)
	if err != 0 {
		return int(err)
	}
	return int(p.Vm.K2user(buf.Bytes(), statn))
}

func sys_fstat(p *proc.Proc_t, fdn int, statn int) int {
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	buf := &stat.Stat_t{}
	err := fd.Fops.Fstat(buf)
	if err != 0 {
		return int(err)
	}

	return int(p.Vm.K2user(buf.Bytes(), statn))
}

// converts internal states to poll states
// pokes poll status bits into user memory. since we only use one priority
// internally, mask away any POLL bits the user didn't not request.
func _ready2rev(orig int, r fdops.Ready_t) int {
	inmask := defs.POLLIN | defs.POLLPRI
	outmask := defs.POLLOUT | defs.POLLWRBAND
	pbits := 0
	if r&fdops.R_READ != 0 {
		pbits |= inmask
	}
	if r&fdops.R_WRITE != 0 {
		pbits |= outmask
	}
	if r&fdops.R_HUP != 0 {
		pbits |= defs.POLLHUP
	}
	if r&fdops.R_ERROR != 0 {
		pbits |= defs.POLLERR
	}
	wantevents := ((orig >> 32) & 0xffff) | defs.POLLNVAL | defs.POLLERR | defs.POLLHUP
	revents := wantevents & pbits
	return orig | (revents << 48)
}

func _checkfds(p *proc.Proc_t, tid defs.Tid_t, pm *fdops.Pollmsg_t, wait bool, buf []uint8,
	nfds int) (int, bool, defs.Err_t) {
	inmask := defs.POLLIN | defs.POLLPRI
	outmask := defs.POLLOUT | defs.POLLWRBAND
	readyfds := 0
	writeback := false
	p.Fdl.Lock()
	for i := 0; i < nfds; i++ {
		off := i * 8
		uw := readn(buf, 8, off)
		fdn := int(uint32(uw))
		// fds < 0 are to be ignored
		if fdn < 0 {
			continue
		}
		fd, ok := p.Fd_get_inner(fdn)
		if !ok {
			uw |= defs.POLLNVAL
			writen(buf, 8, off, uw)
			writeback = true
			continue
		}
		var pev fdops.Ready_t
		events := int((uint(uw) >> 32) & 0xffff)
		// one priority
		if events&inmask != 0 {
			pev |= fdops.R_READ
		}
		if events&outmask != 0 {
			pev |= fdops.R_WRITE
		}
		if events&defs.POLLHUP != 0 {
			pev |= fdops.R_HUP
		}
		// poll unconditionally reports ERR, HUP, and NVAL
		pev |= fdops.R_ERROR | fdops.R_HUP
		pm.Pm_set(tid, pev, wait)
		devstatus, err := fd.Fops.Pollone(*pm)
		if err != 0 {
			p.Fdl.Unlock()
			return 0, false, err
		}
		if devstatus != 0 {
			// found at least one ready fd; don't bother having the
			// other fds send notifications. update user revents
			wait = false
			nuw := _ready2rev(uw, devstatus)
			writen(buf, 8, off, nuw)
			readyfds++
			writeback = true
		}
	}
	p.Fdl.Unlock()
	return readyfds, writeback, 0
}

func sys_poll(p *proc.Proc_t, tid defs.Tid_t, fdsn, nfds, timeout int) int {
	if nfds < 0 || timeout < -1 {
		return int(-defs.EINVAL)
	}

	// copy pollfds from userspace to avoid reading/writing overhead
	// (locking pmap and looking up uva mapping).
	pollfdsz := 8
	sz := uint(pollfdsz * nfds)
	// chosen arbitrarily...
	maxsz := uint(4096)
	if sz > maxsz {
		// fall back to holding lock over user pmap if they want to
		// poll so many fds.
		fmt.Printf("poll limit hit\n")
		return int(-defs.EINVAL)
	}
	buf := make([]uint8, sz)
	if err := p.Vm.User2k(buf, fdsn); err != 0 {
		return int(err)
	}

	// first we tell the underlying device to notify us if their fd is
	// ready. if a device is immediately ready, we don't bother to register
	// notifiers with the rest of the devices -- we just ask their status
	// too.
	gimme := bounds.Bounds(bounds.B_SYS_POLL)
	pm := fdops.Pollmsg_t{}
	for {
		// its ok to block for memory here since no locks are held
		if !res.Resadd(gimme) {
			return int(-defs.ENOHEAP)
		}
		wait := timeout != 0
		rfds, writeback, err := _checkfds(p, tid, &pm, wait, buf,
			nfds)
		if err != 0 {
			return int(err)
		}
		if writeback {
			if err := p.Vm.K2user(buf, fdsn); err != 0 {
				return int(err)
			}
		}

		// if we found a ready fd, we are done
		if rfds != 0 || !wait {
			return rfds
		}

		// otherwise, wait for a notification
		timedout, err := pm.Pm_wait(timeout)
		if err != 0 {
			return int(err)
		}
		if timedout {
			return 0
		}
	}
}

func sys_lseek(p *proc.Proc_t, fdn, off, whence int) int {
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}

	ret, err := fd.Fops.Lseek(off, whence)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_pipe2(p *proc.Proc_t, pipen, _flags int) int {
	rfp := fd.FD_READ
	wfp := fd.FD_WRITE

	flags := defs.Fdopt_t(_flags)
	var opts defs.Fdopt_t
	if flags&defs.O_NONBLOCK != 0 {
		opts |= defs.O_NONBLOCK
	}

	if flags&defs.O_CLOEXEC != 0 {
		rfp |= fd.FD_CLOEXEC
		wfp |= fd.FD_CLOEXEC
	}

	// if there is an error, pipe_t.op_reopen() will release the pipe
	// reservation.
	if !limits.Syslimit.Pipes.Take() {
		lhits++
		return int(-defs.ENOMEM)
	}

	pp := &pipe_t{lraise: true}
	pp.pipe_start()
	rops := &pipefops_t{pipe: pp, writer: false, options: opts}
	wops := &pipefops_t{pipe: pp, writer: true, options: opts}
	rpipe := &fd.Fd_t{Fops: rops}
	wpipe := &fd.Fd_t{Fops: wops}
	rfd, wfd, ok := p.Fd_insert2(rpipe, rfp, wpipe, wfp)
	if !ok {
		fd.Close_panic(rpipe)
		fd.Close_panic(wpipe)
		return int(-defs.EMFILE)
	}

	err := p.Vm.Userwriten(pipen, 4, rfd)
	if err != 0 {
		goto bail
	}
	err = p.Vm.Userwriten(pipen+4, 4, wfd)
	if err != 0 {
		goto bail
	}
	return 0

bail:
	err1 := sys.Sys_close(p, rfd)
	err2 := sys.Sys_close(p, wfd)
	if err1 != 0 || err2 != 0 {
		panic("must succeed")
	}
	return int(err)
}

type pipe_t struct {
	sync.Mutex
	cbuf    circbuf.Circbuf_t
	rcond   *sync.Cond
	wcond   *sync.Cond
	readers int
	writers int
	closed  bool
	pollers fdops.Pollers_t
	passfds passfd_t
	// if true, this pipe was allocated against the pipe limit; raise it on
	// termination.
	lraise bool
}

func (o *pipe_t) pipe_start() {
	pipesz := mem.PGSIZE
	o.cbuf.Cb_init(pipesz, mem.Physmem)
	o.readers, o.writers = 1, 1
	o.rcond = sync.NewCond(o)
	o.wcond = sync.NewCond(o)
}

func (o *pipe_t) op_write(src fdops.Userio_i, noblock bool) (int, defs.Err_t) {
	const pipe_buf = 4096
	need := src.Remain()
	if need > pipe_buf {
		if noblock {
			need = 1
		} else {
			need = pipe_buf
		}
	}
	o.Lock()
	for {
		if o.closed {
			o.Unlock()
			return 0, -defs.EBADF
		}
		if o.readers == 0 {
			o.Unlock()
			return 0, -defs.EPIPE
		}
		if o.cbuf.Left() >= need {
			break
		}
		if noblock {
			o.Unlock()
			return 0, -defs.EWOULDBLOCK
		}
		if err := proc.KillableWait(o.wcond); err != 0 {
			o.Unlock()
			return 0, err
		}
	}
	ret, err := o.cbuf.Copyin(src)
	if err != 0 {
		o.Unlock()
		return 0, err
	}
	o.rcond.Signal()
	o.pollers.Wakeready(fdops.R_READ)
	o.Unlock()

	return ret, 0
}

func (o *pipe_t) op_read(dst fdops.Userio_i, noblock bool) (int, defs.Err_t) {
	o.Lock()
	for {
		if o.closed {
			o.Unlock()
			return 0, -defs.EBADF
		}
		if o.writers == 0 || !o.cbuf.Empty() {
			break
		}
		if noblock {
			o.Unlock()
			return 0, -defs.EWOULDBLOCK
		}
		if err := proc.KillableWait(o.rcond); err != 0 {
			o.Unlock()
			return 0, err
		}
	}
	ret, err := o.cbuf.Copyout(dst)
	if err != 0 {
		o.Unlock()
		return 0, err
	}
	o.wcond.Signal()
	o.pollers.Wakeready(fdops.R_WRITE)
	o.Unlock()

	return ret, 0
}

func (o *pipe_t) op_poll(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	o.Lock()

	if o.closed {
		o.Unlock()
		return 0, 0
	}

	var r fdops.Ready_t
	readable := false
	if !o.cbuf.Empty() || o.writers == 0 {
		readable = true
	}
	writeable := false
	if !o.cbuf.Full() || o.readers == 0 {
		writeable = true
	}
	if pm.Events&fdops.R_READ != 0 && readable {
		r |= fdops.R_READ
	}
	if pm.Events&fdops.R_HUP != 0 && o.writers == 0 {
		r |= fdops.R_HUP
	} else if pm.Events&fdops.R_WRITE != 0 && writeable {
		r |= fdops.R_WRITE
	}
	if r != 0 || !pm.Dowait {
		o.Unlock()
		return r, 0
	}
	err := o.pollers.Addpoller(&pm)
	o.Unlock()
	return 0, err
}

func (o *pipe_t) op_reopen(rd, wd int) defs.Err_t {
	o.Lock()
	if o.closed {
		o.Unlock()
		return -defs.EBADF
	}
	o.readers += rd
	o.writers += wd
	if o.writers == 0 {
		o.rcond.Broadcast()
	}
	if o.readers == 0 {
		o.wcond.Broadcast()
	}
	if o.readers == 0 && o.writers == 0 {
		o.closed = true
		o.cbuf.Cb_release()
		o.passfds.closeall()
		if o.lraise {
			limits.Syslimit.Pipes.Give()
		}
	}
	o.Unlock()
	return 0
}

func (o *pipe_t) op_fdadd(nfd *fd.Fd_t) defs.Err_t {
	o.Lock()
	defer o.Unlock()

	for !o.passfds.add(nfd) {
		if err := proc.KillableWait(o.wcond); err != 0 {
			return err
		}
	}
	return 0
}

func (o *pipe_t) op_fdtake() (*fd.Fd_t, bool) {
	o.Lock()
	defer o.Unlock()
	ret, ok := o.passfds.take()
	if !ok {
		return nil, false
	}
	o.wcond.Broadcast()
	return ret, true
}

type pipefops_t struct {
	pipe    *pipe_t
	options defs.Fdopt_t
	writer  bool
}

func (of *pipefops_t) Close() defs.Err_t {
	var ret defs.Err_t
	if of.writer {
		ret = of.pipe.op_reopen(0, -1)
	} else {
		ret = of.pipe.op_reopen(-1, 0)
	}
	return ret
}

func (of *pipefops_t) Fstat(st *stat.Stat_t) defs.Err_t {
	// linux and openbsd give same mode for all pipes
	st.Wdev(0)
	pipemode := uint(3 << 16)
	st.Wmode(pipemode)
	return 0
}

func (of *pipefops_t) Lseek(int, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (of *pipefops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	return nil, -defs.EINVAL
}

func (of *pipefops_t) Pathi() defs.Inum_t {
	panic("pipe cwd")
}

func (of *pipefops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	noblk := of.options&defs.O_NONBLOCK != 0
	return of.pipe.op_read(dst, noblk)
}

func (of *pipefops_t) Reopen() defs.Err_t {
	var ret defs.Err_t
	if of.writer {
		ret = of.pipe.op_reopen(0, 1)
	} else {
		ret = of.pipe.op_reopen(1, 0)
	}
	return ret
}

func (of *pipefops_t) Write(src fdops.Userio_i) (int, defs.Err_t) {
	noblk := of.options&defs.O_NONBLOCK != 0
	c := 0
	for c != src.Totalsz() {
		if !res.Resadd(bounds.Bounds(bounds.B_PIPEFOPS_T_WRITE)) {
			return c, -defs.ENOHEAP
		}
		ret, err := of.pipe.op_write(src, noblk)
		if noblk || err != 0 {
			return ret, err
		}
		c += ret
	}
	return c, 0
}

func (of *pipefops_t) Truncate(uint) defs.Err_t {
	return -defs.EINVAL
}

func (of *pipefops_t) Pread(fdops.Userio_i, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (of *pipefops_t) Pwrite(fdops.Userio_i, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (of *pipefops_t) Accept(fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	return nil, 0, -defs.ENOTSOCK
}

func (of *pipefops_t) Bind([]uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (of *pipefops_t) Connect([]uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (of *pipefops_t) Listen(int) (fdops.Fdops_i, defs.Err_t) {
	return nil, -defs.ENOTSOCK
}

func (of *pipefops_t) Sendmsg(fdops.Userio_i, []uint8, []uint8,
	int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (of *pipefops_t) Recvmsg(fdops.Userio_i, fdops.Userio_i,
	fdops.Userio_i, int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	return 0, 0, 0, 0, -defs.ENOTSOCK
}

func (of *pipefops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	if of.writer {
		pm.Events &^= fdops.R_READ
	} else {
		pm.Events &^= fdops.R_WRITE
	}
	return of.pipe.op_poll(pm)
}

func (of *pipefops_t) Fcntl(cmd, opt int) int {
	switch cmd {
	case defs.F_GETFL:
		return int(of.options)
	case defs.F_SETFL:
		of.options = defs.Fdopt_t(opt)
		return 0
	default:
		panic("weird cmd")
	}
}

func (of *pipefops_t) Getsockopt(int, fdops.Userio_i, int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (of *pipefops_t) Setsockopt(int, int, fdops.Userio_i, int) defs.Err_t {
	return -defs.ENOTSOCK
}

func (of *pipefops_t) Shutdown(read, write bool) defs.Err_t {
	return -defs.ENOTCONN
}

func sys_rename(p *proc.Proc_t, oldn int, newn int) int {
	old, err1 := p.Vm.Userstr(oldn, fs.NAME_MAX)
	new, err2 := p.Vm.Userstr(newn, fs.NAME_MAX)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	err1 = badpath(old)
	err2 = badpath(new)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	err := thefs.Fs_rename(old, new, p.Cwd)
	return int(err)
}

func sys_mkdir(p *proc.Proc_t, pathn int, mode int) int {
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	err = badpath(path)
	if err != 0 {
		return int(err)
	}
	err = thefs.Fs_mkdir(path, mode, p.Cwd)
	return int(err)
}

func sys_link(p *proc.Proc_t, oldn int, newn int) int {
	old, err1 := p.Vm.Userstr(oldn, fs.NAME_MAX)
	new, err2 := p.Vm.Userstr(newn, fs.NAME_MAX)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	err1 = badpath(old)
	err2 = badpath(new)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	err := thefs.Fs_link(old, new, p.Cwd)
	return int(err)
}

func sys_unlink(p *proc.Proc_t, pathn, isdiri int) int {
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	err = badpath(path)
	if err != 0 {
		return int(err)
	}
	wantdir := isdiri != 0
	err = thefs.Fs_unlink(path, p.Cwd, wantdir)
	return int(err)
}

func sys_gettimeofday(p *proc.Proc_t, timevaln int) int {
	tvalsz := 16
	now := time.Now()
	buf := make([]uint8, tvalsz)
	us := int(now.UnixNano() / 1000)
	writen(buf, 8, 0, us/1e6)
	writen(buf, 8, 8, us%1e6)
	if err := p.Vm.K2user(buf, timevaln); err != 0 {
		return int(err)
	}
	return 0
}

var _rlimits = map[int]uint{defs.RLIMIT_NOFILE: defs.RLIM_INFINITY}

func sys_getrlimit(p *proc.Proc_t, resn, rlpn int) int {
	var cur uint
	switch resn {
	case defs.RLIMIT_NOFILE:
		cur = p.Ulim.Nofile
	default:
		return int(-defs.EINVAL)
	}
	max := _rlimits[resn]
	err1 := p.Vm.Userwriten(rlpn, 8, int(cur))
	err2 := p.Vm.Userwriten(rlpn+8, 8, int(max))
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	return 0
}

func sys_setrlimit(p *proc.Proc_t, resn, rlpn int) int {
	// XXX root can raise max
	_ncur, err := p.Vm.Userreadn(rlpn, 8)
	if err != 0 {
		return int(err)
	}
	ncur := uint(_ncur)
	if ncur > _rlimits[resn] {
		return int(-defs.EINVAL)
	}
	switch resn {
	case defs.RLIMIT_NOFILE:
		p.Ulim.Nofile = ncur
	default:
		return int(-defs.EINVAL)
	}
	return 0
}

func sys_getrusage(p *proc.Proc_t, who, rusagep int) int {
	var ru []uint8
	if who == defs.RUSAGE_SELF {
		// user time is gathered at thread termination... report user
		// time as best as we can
		tmp := p.Atime

		p.Threadi.Lock()
		for tid := range p.Threadi.Notes {
			if tid == 0 {
			}
			val := 42
			// tid may not exist if the query for the time races
			// with a thread exiting.
			if val > 0 {
				tmp.Userns += int64(val)
			}
		}
		p.Threadi.Unlock()

		ru = tmp.To_rusage()
	} else if who == defs.RUSAGE_CHILDREN {
		ru = p.Catime.Fetch()
	} else {
		return int(-defs.EINVAL)
	}
	if err := p.Vm.K2user(ru, rusagep); err != 0 {
		return int(err)
	}
	return int(-defs.ENOSYS)
}

func sys_mknod(p *proc.Proc_t, pathn, moden, devn int) int {
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}

	err = badpath(path)
	if err != 0 {
		return int(err)
	}
	maj, min := defs.Unmkdev(uint(devn))
	fsf, err := thefs.Fs_open_inner(path, defs.O_CREAT, 0, p.Cwd, maj, min)
	if err != 0 {
		return int(err)
	}
	if thefs.Fs_close(fsf.Inum) != 0 {
		panic("must succeed")
	}
	return 0
}

func sys_sync(p *proc.Proc_t) int {
	return int(thefs.Fs_sync())
}

func sys_reboot(p *proc.Proc_t) int {
	// mov'ing to cr3 does not flush global pages. if, before loading the
	// zero page into cr3 below, there are just enough TLB entries to
	// dispatch a fault, but not enough to complete the fault handler, the
	// fault handler will recursively fault forever since it uses an IST
	// stack. therefore, flush the global pages too.
	pge := uintptr(1 << 7)
	runtime.Lcr4(runtime.Rcr4() &^ pge)
	// who needs ACPI?
	runtime.Lcr3(uintptr(mem.P_zeropg))
	// poof
	fmt.Printf("what?\n")
	return 0
}

func sys_nanosleep(p *proc.Proc_t, sleeptsn, remaintsn int) int {
	tot, _, err := p.Vm.Usertimespec(sleeptsn)
	if err != 0 {
		return int(err)
	}
	tochan := time.After(tot)
	kn := &tinfo.Current().Killnaps
	select {
	case <-tochan:
		return 0
	case <-kn.Killch:
		if kn.Kerr == 0 {
			panic("no")
		}
		return int(kn.Kerr)
	}
}

func sys_getpid(p *proc.Proc_t, tid defs.Tid_t) int {
	return p.Pid
}

func sys_getppid(p *proc.Proc_t, tid defs.Tid_t) int {
	return p.Pwait.Pid
}

func sys_socket(p *proc.Proc_t, domain, typ, proto int) int {
	var opts defs.Fdopt_t
	if typ&defs.SOCK_NONBLOCK != 0 {
		opts |= defs.O_NONBLOCK
	}
	var clop int
	if typ&defs.SOCK_CLOEXEC != 0 {
		clop = fd.FD_CLOEXEC
	}

	var sfops fdops.Fdops_i
	switch {
	case domain == defs.AF_UNIX && typ&defs.SOCK_DGRAM != 0:
		if opts != 0 {
			panic("no imp")
		}
		sfops = &sudfops_t{open: 1}
	case domain == defs.AF_UNIX && typ&defs.SOCK_STREAM != 0:
		sfops = &susfops_t{options: opts}
	case domain == defs.AF_INET && typ&defs.SOCK_STREAM != 0:
		tfops := &bnet.Tcpfops_t{}
		tfops.Set(&bnet.Tcptcb_t{}, opts)
		sfops = tfops
	default:
		return int(-defs.EINVAL)
	}
	if !limits.Syslimit.Socks.Take() {
		lhits++
		return int(-defs.ENOMEM)
	}
	file := &fd.Fd_t{}
	file.Fops = sfops
	fdn, ok := p.Fd_insert(file, fd.FD_READ|fd.FD_WRITE|clop)
	if !ok {
		fd.Close_panic(file)
		limits.Syslimit.Socks.Give()
		return int(-defs.EMFILE)
	}
	return fdn
}

func sys_connect(p *proc.Proc_t, fdn, sockaddrn, socklen int) int {
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}

	// copy sockaddr to kernel space to avoid races
	sabuf, err := copysockaddr(p, sockaddrn, socklen)
	if err != 0 {
		return int(err)
	}
	err = fd.Fops.Connect(sabuf)
	return int(err)
}

func sys_accept(p *proc.Proc_t, fdn, sockaddrn, socklenn int) int {
	f, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	var sl int
	if socklenn != 0 {
		l, err := p.Vm.Userreadn(socklenn, 8)
		if err != 0 {
			return int(err)
		}
		if l < 0 {
			return int(-defs.EFAULT)
		}
		sl = l
	}
	fromsa := p.Vm.Mkuserbuf(sockaddrn, sl)
	newfops, fromlen, err := f.Fops.Accept(fromsa)
	//proc.Ubpool.Put(fromsa)
	if err != 0 {
		return int(err)
	}
	if fromlen != 0 {
		if err := p.Vm.Userwriten(socklenn, 8, fromlen); err != 0 {
			return int(err)
		}
	}
	newfd := &fd.Fd_t{Fops: newfops}
	ret, ok := p.Fd_insert(newfd, fd.FD_READ|fd.FD_WRITE)
	if !ok {
		fd.Close_panic(newfd)
		return int(-defs.EMFILE)
	}
	return ret
}

func copysockaddr(p *proc.Proc_t, san, sl int) ([]uint8, defs.Err_t) {
	if sl == 0 {
		return nil, 0
	}
	if sl < 0 {
		return nil, -defs.EFAULT
	}
	maxsl := 256
	if sl >= maxsl {
		return nil, -defs.ENOTSOCK
	}
	ub := p.Vm.Mkuserbuf(san, sl)
	sabuf := make([]uint8, sl)
	_, err := ub.Uioread(sabuf)
	//proc.Ubpool.Put(ub)
	if err != 0 {
		return nil, err
	}
	return sabuf, 0
}

func sys_sendto(p *proc.Proc_t, fdn, bufn, flaglen, sockaddrn, socklen int) int {
	fd, err := _fd_write(p, fdn)
	if err != 0 {
		return int(err)
	}
	flags := int(uint(uint32(flaglen)))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	if buflen < 0 {
		return int(-defs.EFAULT)
	}

	// copy sockaddr to kernel space to avoid races
	sabuf, err := copysockaddr(p, sockaddrn, socklen)
	if err != 0 {
		return int(err)
	}

	buf := p.Vm.Mkuserbuf(bufn, buflen)
	ret, err := fd.Fops.Sendmsg(buf, sabuf, nil, flags)
	//proc.Ubpool.Put(buf)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_recvfrom(p *proc.Proc_t, fdn, bufn, flaglen, sockaddrn,
	socklenn int) int {
	fd, err := _fd_read(p, fdn)
	if err != 0 {
		return int(err)
	}
	flags := uint(uint32(flaglen))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	buf := p.Vm.Mkuserbuf(bufn, buflen)

	// is the from address requested?
	var salen int
	if socklenn != 0 {
		l, err := p.Vm.Userreadn(socklenn, 8)
		if err != 0 {
			return int(err)
		}
		salen = l
		if salen < 0 {
			return int(-defs.EFAULT)
		}
	}
	fromsa := p.Vm.Mkuserbuf(sockaddrn, salen)
	ret, addrlen, _, _, err := fd.Fops.Recvmsg(buf, fromsa, zeroubuf, 0)
	//proc.Ubpool.Put(buf)
	//proc.Ubpool.Put(fromsa)
	if err != 0 {
		return int(err)
	}
	// write new socket size to user space
	if addrlen > 0 {
		if err := p.Vm.Userwriten(socklenn, 8, addrlen); err != 0 {
			return int(err)
		}
	}
	return ret
}

func sys_recvmsg(p *proc.Proc_t, fdn, _msgn, _flags int) int {
	if _flags != 0 {
		panic("no imp")
	}
	fd, err := _fd_read(p, fdn)
	if err != 0 {
		return int(err)
	}
	// maybe copy the msghdr to kernel space?
	msgn := uint(_msgn)
	iovn, err1 := p.Vm.Userreadn(int(msgn+2*8), 8)
	niov, err2 := p.Vm.Userreadn(int(msgn+3*8), 4)
	cmsgl, err3 := p.Vm.Userreadn(int(msgn+5*8), 8)
	salen, err4 := p.Vm.Userreadn(int(msgn+1*8), 8)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	if err3 != 0 {
		return int(err3)
	}
	if err4 != 0 {
		return int(err4)
	}

	var saddr fdops.Userio_i
	saddr = zeroubuf
	if salen > 0 {
		saddrn, err := p.Vm.Userreadn(int(msgn+0*8), 8)
		if err != 0 {
			return int(err)
		}
		ub := p.Vm.Mkuserbuf(saddrn, salen)
		saddr = ub
	}
	var cmsg fdops.Userio_i
	cmsg = zeroubuf
	if cmsgl > 0 {
		cmsgn, err := p.Vm.Userreadn(int(msgn+4*8), 8)
		if err != 0 {
			return int(err)
		}
		ub := p.Vm.Mkuserbuf(cmsgn, cmsgl)
		cmsg = ub
	}

	iov := &vm.Useriovec_t{}
	err = iov.Iov_init(&p.Vm, uint(iovn), niov)
	if err != 0 {
		return int(err)
	}

	ret, sawr, cmwr, msgfl, err := fd.Fops.Recvmsg(iov, saddr,
		cmsg, 0)
	if err != 0 {
		return int(err)
	}
	// write size of socket address, ancillary data, and msg flags back to
	// user space
	if err := p.Vm.Userwriten(int(msgn+28), 4, int(msgfl)); err != 0 {
		return int(err)
	}
	if saddr.Totalsz() != 0 {
		if err := p.Vm.Userwriten(int(msgn+1*8), 8, sawr); err != 0 {
			return int(err)
		}
	}
	if cmsg.Totalsz() != 0 {
		if err := p.Vm.Userwriten(int(msgn+5*8), 8, cmwr); err != 0 {
			return int(err)
		}
	}
	return ret
}

func sys_sendmsg(p *proc.Proc_t, fdn, _msgn, _flags int) int {
	if _flags != 0 {
		panic("no imp")
	}
	fd, err := _fd_write(p, fdn)
	if err != 0 {
		return int(err)
	}
	// maybe copy the msghdr to kernel space?
	msgn := uint(_msgn)
	iovn, err1 := p.Vm.Userreadn(int(msgn+2*8), 8)
	niov, err2 := p.Vm.Userreadn(int(msgn+3*8), 8)
	cmsgl, err3 := p.Vm.Userreadn(int(msgn+5*8), 8)
	salen, err4 := p.Vm.Userreadn(int(msgn+1*8), 8)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	if err3 != 0 {
		return int(err3)
	}
	if err4 != 0 {
		return int(err4)
	}

	// copy to address and ancillary data to kernel space
	var saddr []uint8
	if salen > 0 {
		if salen > 64 {
			return int(-defs.EINVAL)
		}
		saddrva, err := p.Vm.Userreadn(int(msgn+0*8), 8)
		if err != 0 {
			return int(err)
		}
		saddr = make([]uint8, salen)
		ub := p.Vm.Mkuserbuf(saddrva, salen)
		did, err := ub.Uioread(saddr)
		if err != 0 {
			return int(err)
		}
		if did != salen {
			panic("how")
		}
	}
	var cmsg []uint8
	if cmsgl > 0 {
		if cmsgl > 256 {
			return int(-defs.EINVAL)
		}
		cmsgva, err := p.Vm.Userreadn(int(msgn+4*8), 8)
		if err != 0 {
			return int(err)
		}
		cmsg = make([]uint8, cmsgl)
		ub := p.Vm.Mkuserbuf(cmsgva, cmsgl)
		did, err := ub.Uioread(cmsg)
		if err != 0 {
			return int(err)
		}
		if did != cmsgl {
			panic("how")
		}
	}
	iov := &vm.Useriovec_t{}
	err = iov.Iov_init(&p.Vm, uint(iovn), niov)
	if err != 0 {
		return int(err)
	}
	ret, err := fd.Fops.Sendmsg(iov, saddr, cmsg, 0)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_socketpair(p *proc.Proc_t, domain, typ, proto int, sockn int) int {
	var opts defs.Fdopt_t
	if typ&defs.SOCK_NONBLOCK != 0 {
		opts |= defs.O_NONBLOCK
	}
	var clop int
	if typ&defs.SOCK_CLOEXEC != 0 {
		clop = fd.FD_CLOEXEC
	}

	mask := defs.SOCK_STREAM | defs.SOCK_DGRAM
	if typ&mask == 0 || typ&mask == mask {
		return int(-defs.EINVAL)
	}

	if !limits.Syslimit.Socks.Take() {
		return int(-defs.ENOMEM)
	}

	var sfops1, sfops2 fdops.Fdops_i
	var err defs.Err_t
	switch {
	case domain == defs.AF_UNIX && typ&defs.SOCK_STREAM != 0:
		sfops1, sfops2, err = _suspair(opts)
	default:
		panic("no imp")
	}

	if err != 0 {
		limits.Syslimit.Socks.Give()
		return int(err)
	}

	fd1 := &fd.Fd_t{}
	fd1.Fops = sfops1
	fd2 := &fd.Fd_t{}
	fd2.Fops = sfops2
	perms := fd.FD_READ | fd.FD_WRITE | clop
	fdn1, fdn2, ok := p.Fd_insert2(fd1, perms, fd2, perms)
	if !ok {
		fd.Close_panic(fd1)
		fd.Close_panic(fd2)
		return int(-defs.EMFILE)
	}
	if err1, err2 := p.Vm.Userwriten(sockn, 4, fdn1), p.Vm.Userwriten(sockn+4, 4, fdn2); err1 != 0 || err2 != 0 {
		if sys.Sys_close(p, fdn1) != 0 || sys.Sys_close(p, fdn2) != 0 {
			panic("must succeed")
		}
		if err1 == 0 {
			err1 = err2
		}
		return int(err1)
	}
	return 0
}

func _suspair(opts defs.Fdopt_t) (fdops.Fdops_i, fdops.Fdops_i, defs.Err_t) {
	pipe1 := &pipe_t{}
	pipe2 := &pipe_t{}
	pipe1.pipe_start()
	pipe2.pipe_start()

	p1r := &pipefops_t{pipe: pipe1, options: opts}
	p1w := &pipefops_t{pipe: pipe2, writer: true, options: opts}

	p2r := &pipefops_t{pipe: pipe2, options: opts}
	p2w := &pipefops_t{pipe: pipe1, writer: true, options: opts}

	sfops1 := &susfops_t{pipein: p1r, pipeout: p1w, options: opts}
	sfops2 := &susfops_t{pipein: p2r, pipeout: p2w, options: opts}
	sfops1.conn, sfops2.conn = true, true
	return sfops1, sfops2, 0
}

func sys_shutdown(p *proc.Proc_t, fdn, how int) int {
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	var rdone, wdone bool
	if how&defs.SHUT_WR != 0 {
		wdone = true
	}
	if how&defs.SHUT_RD != 0 {
		rdone = true
	}
	return int(fd.Fops.Shutdown(rdone, wdone))
}

func sys_bind(p *proc.Proc_t, fdn, sockaddrn, socklen int) int {
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}

	sabuf, err := copysockaddr(p, sockaddrn, socklen)
	if err != 0 {
		return int(err)
	}
	r := fd.Fops.Bind(sabuf)
	return int(r)
}

type sudfops_t struct {
	// this lock protects open and bound; bud has its own lock
	sync.Mutex
	bud   *bud_t
	open  int
	bound bool
}

func (sf *sudfops_t) Close() defs.Err_t {
	// XXX use new method
	sf.Lock()
	sf.open--
	if sf.open < 0 {
		panic("negative ref count")
	}
	term := sf.open == 0
	if term {
		if sf.bound {
			sf.bud.bud_close()
			sf.bound = false
			sf.bud = nil
		}
		limits.Syslimit.Socks.Give()
	}
	sf.Unlock()
	return 0
}

func (sf *sudfops_t) Fstat(s *stat.Stat_t) defs.Err_t {
	panic("no imp")
}

func (sf *sudfops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	return nil, -defs.EINVAL
}

func (sf *sudfops_t) Pathi() defs.Inum_t {
	panic("cwd socket?")
}

func (sf *sudfops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	return 0, -defs.EBADF
}

func (sf *sudfops_t) Reopen() defs.Err_t {
	sf.Lock()
	sf.open++
	sf.Unlock()
	return 0
}

func (sf *sudfops_t) Write(fdops.Userio_i) (int, defs.Err_t) {
	return 0, -defs.EBADF
}

func (sf *sudfops_t) Truncate(newlen uint) defs.Err_t {
	return -defs.EINVAL
}

func (sf *sudfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sf *sudfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sf *sudfops_t) Lseek(int, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

// trims trailing nulls from slice
func slicetostr(buf []uint8) string {
	end := 0
	for i := range buf {
		end = i
		if buf[i] == 0 {
			break
		}
	}
	return string(buf[:end])
}

func (sf *sudfops_t) Accept(fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	return nil, 0, -defs.EINVAL
}

func (sf *sudfops_t) Bind(sa []uint8) defs.Err_t {
	sf.Lock()
	defer sf.Unlock()

	if sf.bound {
		return -defs.EINVAL
	}

	poff := 2
	path := ustr.MkUstrSlice(sa[poff:])
	// try to create the specified file as a special device
	bid := allbuds.bud_id_new()
	fsf, err := thefs.Fs_open_inner(path, defs.O_CREAT|defs.O_EXCL, 0, proc.CurrentProc().Cwd, defs.D_SUD, int(bid))
	if err != 0 {
		return err
	}
	inum := fsf.Inum
	bud := allbuds.bud_new(bid, path, inum)
	if thefs.Fs_close(fsf.Inum) != 0 {
		panic("must succeed")
	}
	sf.bud = bud
	sf.bound = true
	return 0
}

func (sf *sudfops_t) Connect(sabuf []uint8) defs.Err_t {
	return -defs.EINVAL
}

func (sf *sudfops_t) Listen(backlog int) (fdops.Fdops_i, defs.Err_t) {
	return nil, -defs.EINVAL
}

func (sf *sudfops_t) Sendmsg(src fdops.Userio_i, sa []uint8,
	cmsg []uint8, flags int) (int, defs.Err_t) {
	if len(cmsg) != 0 || flags != 0 {
		panic("no imp")
	}
	poff := 2
	if len(sa) <= poff {
		return 0, -defs.EINVAL
	}
	st := &stat.Stat_t{}
	path := ustr.MkUstrSlice(sa[poff:])

	err := thefs.Fs_stat(path, st, proc.CurrentProc().Cwd)
	if err != 0 {
		return 0, err
	}
	maj, min := defs.Unmkdev(st.Rdev())
	if maj != defs.D_SUD {
		return 0, -defs.ECONNREFUSED
	}
	ino := st.Rino()

	bid := budid_t(min)
	bud, ok := allbuds.bud_lookup(bid, defs.Inum_t(ino))
	if !ok {
		return 0, -defs.ECONNREFUSED
	}

	var bp ustr.Ustr
	sf.Lock()
	if sf.bound {
		bp = sf.bud.bpath
	}
	sf.Unlock()

	did, err := bud.bud_in(src, bp, cmsg)
	if err != 0 {
		return 0, err
	}
	return did, 0
}

func (sf *sudfops_t) Recvmsg(dst fdops.Userio_i,
	fromsa fdops.Userio_i, cmsg fdops.Userio_i, flags int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	if cmsg.Totalsz() != 0 || flags != 0 {
		panic("no imp")
	}

	sf.Lock()
	defer sf.Unlock()

	// XXX what is recv'ing on an unbound unix datagram socket supposed to
	// do? openbsd and linux seem to block forever.
	if !sf.bound {
		return 0, 0, 0, 0, -defs.ECONNREFUSED
	}
	bud := sf.bud

	datadid, addrdid, ancdid, msgfl, err := bud.bud_out(dst, fromsa, cmsg)
	if err != 0 {
		return 0, 0, 0, 0, err
	}
	return datadid, addrdid, ancdid, msgfl, 0
}

func (sf *sudfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	sf.Lock()
	defer sf.Unlock()

	if !sf.bound {
		return pm.Events & fdops.R_ERROR, 0
	}
	r, err := sf.bud.bud_poll(pm)
	return r, err
}

func (sf *sudfops_t) Fcntl(cmd, opt int) int {
	return int(-defs.ENOSYS)
}

func (sf *sudfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	return 0, -defs.EOPNOTSUPP
}

func (sf *sudfops_t) Setsockopt(int, int, fdops.Userio_i, int) defs.Err_t {
	return -defs.EOPNOTSUPP
}

func (sf *sudfops_t) Shutdown(read, write bool) defs.Err_t {
	return -defs.ENOTSOCK
}

type budid_t int

var allbuds = allbud_t{m: make(map[budkey_t]*bud_t)}

// buds are indexed by bid and inode number in order to detect stale socket
// files that happen to have the same bid.
type budkey_t struct {
	bid  budid_t
	priv defs.Inum_t
}

type allbud_t struct {
	// leaf lock
	sync.Mutex
	m       map[budkey_t]*bud_t
	nextbid budid_t
}

func (ab *allbud_t) bud_lookup(bid budid_t, fpriv defs.Inum_t) (*bud_t, bool) {
	key := budkey_t{bid, fpriv}

	ab.Lock()
	bud, ok := ab.m[key]
	ab.Unlock()

	return bud, ok
}

func (ab *allbud_t) bud_id_new() budid_t {
	ab.Lock()
	ret := ab.nextbid
	ab.nextbid++
	ab.Unlock()
	return ret
}

func (ab *allbud_t) bud_new(bid budid_t, budpath ustr.Ustr, fpriv defs.Inum_t) *bud_t {
	ret := &bud_t{}
	ret.bud_init(bid, budpath, fpriv)

	key := budkey_t{bid, fpriv}
	ab.Lock()
	if _, ok := ab.m[key]; ok {
		panic("bud exists")
	}
	ab.m[key] = ret
	ab.Unlock()
	return ret
}

func (ab *allbud_t) bud_del(bid budid_t, fpriv defs.Inum_t) {
	key := budkey_t{bid, fpriv}
	ab.Lock()
	if _, ok := ab.m[key]; !ok {
		panic("no such bud")
	}
	delete(ab.m, key)
	ab.Unlock()
}

type dgram_t struct {
	from ustr.Ustr
	sz   int
}

// a circular buffer for datagrams and their source addresses
type dgrambuf_t struct {
	cbuf   circbuf.Circbuf_t
	dgrams []dgram_t
	// add dgrams at head, remove from tail
	head uint
	tail uint
}

func (db *dgrambuf_t) dg_init(sz int) {
	db.cbuf.Cb_init(sz, mem.Physmem)
	// assume that messages are at least 10 bytes
	db.dgrams = make([]dgram_t, sz/10)
	db.head, db.tail = 0, 0
}

// returns true if there is enough buffers to hold a datagram of size sz
func (db *dgrambuf_t) _canhold(sz int) bool {
	if (db.head-db.tail) == uint(len(db.dgrams)) ||
		db.cbuf.Left() < sz {
		return false
	}
	return true
}

func (db *dgrambuf_t) _havedgram() bool {
	return db.head != db.tail
}

func (db *dgrambuf_t) copyin(src fdops.Userio_i, from ustr.Ustr) (int, defs.Err_t) {
	// is there a free source address slot and buffer space?
	if !db._canhold(src.Totalsz()) {
		panic("should have blocked")
	}
	did, err := db.cbuf.Copyin(src)
	if err != 0 {
		return 0, err
	}
	slot := &db.dgrams[db.head%uint(len(db.dgrams))]
	db.head++
	slot.from = from
	slot.sz = did
	return did, 0
}

func (db *dgrambuf_t) copyout(dst, fromsa, cmsg fdops.Userio_i) (int, int, defs.Err_t) {
	if cmsg.Totalsz() != 0 {
		panic("no imp")
	}
	if db.head == db.tail {
		panic("should have blocked")
	}
	slot := &db.dgrams[db.tail%uint(len(db.dgrams))]
	sz := slot.sz
	if sz == 0 {
		panic("huh?")
	}
	var fdid int
	if fromsa.Totalsz() != 0 {
		fsaddr := _sockaddr_un(slot.from)
		var err defs.Err_t
		fdid, err = fromsa.Uiowrite(fsaddr)
		if err != 0 {
			return 0, 0, err
		}
	}
	did, err := db.cbuf.Copyout_n(dst, sz)
	if err != 0 {
		return 0, 0, err
	}
	// commit tail
	db.tail++
	return did, fdid, 0
}

func (db *dgrambuf_t) dg_release() {
	db.cbuf.Cb_release()
}

// convert bound socket path to struct sockaddr_un
func _sockaddr_un(budpath ustr.Ustr) []uint8 {
	ret := make([]uint8, 2, 16)
	// len
	writen(ret, 1, 0, len(budpath))
	// family
	writen(ret, 1, 1, defs.AF_UNIX)
	// path
	ret = append(ret, budpath...)
	ret = append(ret, 0)
	return ret
}

// a type for bound UNIX datagram sockets
type bud_t struct {
	sync.Mutex
	bid     budid_t
	fpriv   defs.Inum_t
	dbuf    dgrambuf_t
	pollers fdops.Pollers_t
	cond    *sync.Cond
	closed  bool
	bpath   ustr.Ustr
}

func (bud *bud_t) bud_init(bid budid_t, bpath ustr.Ustr, priv defs.Inum_t) {
	bud.bid = bid
	bud.fpriv = priv
	bud.bpath = bpath
	bud.dbuf.dg_init(512)
	bud.cond = sync.NewCond(bud)
}

func (bud *bud_t) _rready() {
	bud.cond.Broadcast()
	bud.pollers.Wakeready(fdops.R_READ)
}

func (bud *bud_t) _wready() {
	bud.cond.Broadcast()
	bud.pollers.Wakeready(fdops.R_WRITE)
}

// returns number of bytes written and error
func (bud *bud_t) bud_in(src fdops.Userio_i, from ustr.Ustr, cmsg []uint8) (int, defs.Err_t) {
	if len(cmsg) != 0 {
		panic("no imp")
	}
	need := src.Totalsz()
	bud.Lock()
	for {
		if bud.closed {
			bud.Unlock()
			return 0, -defs.EBADF
		}
		if bud.dbuf._canhold(need) || bud.closed {
			break
		}
		if err := proc.KillableWait(bud.cond); err != 0 {
			bud.Unlock()
			return 0, err
		}
	}
	did, err := bud.dbuf.copyin(src, from)
	bud._rready()
	bud.Unlock()
	return did, err
}

// returns number of bytes written of data, socket address, ancillary data, and
// ancillary message flags...
func (bud *bud_t) bud_out(dst, fromsa, cmsg fdops.Userio_i) (int, int, int,
	defs.Msgfl_t, defs.Err_t) {
	if cmsg.Totalsz() != 0 {
		panic("no imp")
	}
	bud.Lock()
	for {
		if bud.closed {
			bud.Unlock()
			return 0, 0, 0, 0, -defs.EBADF
		}
		if bud.dbuf._havedgram() {
			break
		}
		if err := proc.KillableWait(bud.cond); err != 0 {
			bud.Unlock()
			return 0, 0, 0, 0, err
		}
	}
	ddid, fdid, err := bud.dbuf.copyout(dst, fromsa, cmsg)
	bud._wready()
	bud.Unlock()
	return ddid, fdid, 0, 0, err
}

func (bud *bud_t) bud_poll(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	var ret fdops.Ready_t
	var err defs.Err_t
	bud.Lock()
	if bud.closed {
		goto out
	}
	if pm.Events&fdops.R_READ != 0 && bud.dbuf._havedgram() {
		ret |= fdops.R_READ
	}
	if pm.Events&fdops.R_WRITE != 0 && bud.dbuf._canhold(32) {
		ret |= fdops.R_WRITE
	}
	if ret == 0 && pm.Dowait {
		err = bud.pollers.Addpoller(&pm)
	}
out:
	bud.Unlock()
	return ret, err
}

// the bud is closed; wake up any waiting threads
func (bud *bud_t) bud_close() {
	bud.Lock()
	bud.closed = true
	bud.cond.Broadcast()
	bud.pollers.Wakeready(fdops.R_READ | fdops.R_WRITE | fdops.R_ERROR)
	bid := bud.bid
	fpriv := bud.fpriv
	bud.dbuf.dg_release()
	bud.Unlock()

	allbuds.bud_del(bid, fpriv)
}

type susfops_t struct {
	pipein  *pipefops_t
	pipeout *pipefops_t
	bl      sync.Mutex
	conn    bool
	bound   bool
	lstn    bool
	myaddr  ustr.Ustr
	mysid   int
	options defs.Fdopt_t
}

func (sus *susfops_t) Close() defs.Err_t {
	if !sus.conn {
		return 0
	}
	err1 := sus.pipein.Close()
	err2 := sus.pipeout.Close()
	if err1 != 0 {
		return err1
	}
	// XXX
	sus.pipein.pipe.Lock()
	term := sus.pipein.pipe.closed
	sus.pipein.pipe.Unlock()
	if term {
		limits.Syslimit.Socks.Give()
	}
	return err2
}

func (sus *susfops_t) Fstat(*stat.Stat_t) defs.Err_t {
	panic("no imp")
}

func (sus *susfops_t) Lseek(int, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sus *susfops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	return nil, -defs.ENODEV
}

func (sus *susfops_t) Pathi() defs.Inum_t {
	panic("unix stream cwd?")
}

func (sus *susfops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	read, _, _, _, err := sus.Recvmsg(dst, zeroubuf, zeroubuf, 0)
	return read, err
}

func (sus *susfops_t) Reopen() defs.Err_t {
	if !sus.conn {
		return 0
	}
	err1 := sus.pipein.Reopen()
	err2 := sus.pipeout.Reopen()
	if err1 != 0 {
		return err1
	}
	return err2
}

func (sus *susfops_t) Write(src fdops.Userio_i) (int, defs.Err_t) {
	wrote, err := sus.Sendmsg(src, nil, nil, 0)
	if err == -defs.EPIPE {
		err = -defs.ECONNRESET
	}
	return wrote, err
}

func (sus *susfops_t) Truncate(newlen uint) defs.Err_t {
	return -defs.EINVAL
}

func (sus *susfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sus *susfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sus *susfops_t) Accept(fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	return nil, 0, -defs.EINVAL
}

func (sus *susfops_t) Bind(saddr []uint8) defs.Err_t {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.bound {
		return -defs.EINVAL
	}
	poff := 2
	path := ustr.MkUstrSlice(saddr[poff:])
	sid := susid_new()

	// create special file
	fsf, err := thefs.Fs_open_inner(path, defs.O_CREAT|defs.O_EXCL, 0, proc.CurrentProc().Cwd, defs.D_SUS, sid)
	if err != 0 {
		return err
	}
	if thefs.Fs_close(fsf.Inum) != 0 {
		panic("must succeed")
	}
	sus.myaddr = path
	sus.mysid = sid
	sus.bound = true
	return 0
}

func (sus *susfops_t) Connect(saddr []uint8) defs.Err_t {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.conn {
		return -defs.EISCONN
	}
	poff := 2
	path := ustr.MkUstrSlice(saddr[poff:])

	// lookup sid
	st := &stat.Stat_t{}
	err := thefs.Fs_stat(path, st, proc.CurrentProc().Cwd)
	if err != 0 {
		return err
	}
	maj, min := defs.Unmkdev(st.Rdev())
	if maj != defs.D_SUS {
		return -defs.ECONNREFUSED
	}
	sid := min

	allsusl.Lock()
	susl, ok := allsusl.m[sid]
	allsusl.Unlock()
	if !ok {
		return -defs.ECONNREFUSED
	}

	pipein := &pipe_t{}
	pipein.pipe_start()

	pipeout, err := susl.connectwait(pipein)
	if err != 0 {
		return err
	}

	sus.pipein = &pipefops_t{pipe: pipein, options: sus.options}
	sus.pipeout = &pipefops_t{pipe: pipeout, writer: true, options: sus.options}
	sus.conn = true
	return 0
}

func (sus *susfops_t) Listen(backlog int) (fdops.Fdops_i, defs.Err_t) {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.conn {
		return nil, -defs.EISCONN
	}
	if !sus.bound {
		return nil, -defs.EINVAL
	}
	if sus.lstn {
		return nil, -defs.EINVAL
	}
	sus.lstn = true

	// create a listening socket
	susl := &susl_t{}
	susl.susl_start(sus.mysid, backlog)
	newsock := &suslfops_t{susl: susl, myaddr: sus.myaddr,
		options: sus.options}
	allsusl.Lock()
	// XXXPANIC
	if _, ok := allsusl.m[sus.mysid]; ok {
		panic("susl exists")
	}
	allsusl.m[sus.mysid] = susl
	allsusl.Unlock()

	return newsock, 0
}

func (sus *susfops_t) Sendmsg(src fdops.Userio_i, toaddr []uint8,
	cmsg []uint8, flags int) (int, defs.Err_t) {
	if !sus.conn {
		return 0, -defs.ENOTCONN
	}
	if toaddr != nil {
		return 0, -defs.EISCONN
	}

	if len(cmsg) > 0 {
		scmsz := 16 + 8
		if len(cmsg) < scmsz {
			return 0, -defs.EINVAL
		}
		// allow fd sending
		cmsg_len := readn(cmsg, 8, 0)
		cmsg_level := readn(cmsg, 4, 8)
		cmsg_type := readn(cmsg, 4, 12)
		scm_rights := 1
		if cmsg_len != scmsz || cmsg_level != scm_rights ||
			cmsg_type != defs.SOL_SOCKET {
			return 0, -defs.EINVAL
		}
		chdrsz := 16
		fdn := readn(cmsg, 4, chdrsz)
		ofd, ok := proc.CurrentProc().Fd_get(fdn)
		if !ok {
			return 0, -defs.EBADF
		}
		nfd, err := fd.Copyfd(ofd)
		if err != 0 {
			return 0, err
		}
		err = sus.pipeout.pipe.op_fdadd(nfd)
		if err != 0 {
			return 0, err
		}
	}

	return sus.pipeout.Write(src)
}

func (sus *susfops_t) _fdrecv(cmsg fdops.Userio_i,
	fl defs.Msgfl_t) (int, defs.Msgfl_t, defs.Err_t) {
	scmsz := 16 + 8
	if cmsg.Totalsz() < scmsz {
		return 0, fl, 0
	}
	nfd, ok := sus.pipein.pipe.op_fdtake()
	if !ok {
		return 0, fl, 0
	}
	nfdn, ok := proc.CurrentProc().Fd_insert(nfd, nfd.Perms)
	if !ok {
		fd.Close_panic(nfd)
		return 0, fl, -defs.EMFILE
	}
	buf := make([]uint8, scmsz)
	writen(buf, 8, 0, scmsz)
	writen(buf, 4, 8, defs.SOL_SOCKET)
	scm_rights := 1
	writen(buf, 4, 12, scm_rights)
	writen(buf, 4, 16, nfdn)
	l, err := cmsg.Uiowrite(buf)
	if err != 0 {
		return 0, fl, err
	}
	if l != scmsz {
		panic("how")
	}
	return scmsz, fl, 0
}

func (sus *susfops_t) Recvmsg(dst fdops.Userio_i, fromsa fdops.Userio_i,
	cmsg fdops.Userio_i, flags int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	if !sus.conn {
		return 0, 0, 0, 0, -defs.ENOTCONN
	}

	ret, err := sus.pipein.Read(dst)
	if err != 0 {
		return 0, 0, 0, 0, err
	}
	cmsglen, msgfl, err := sus._fdrecv(cmsg, 0)
	return ret, 0, cmsglen, msgfl, err
}

func (sus *susfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	if !sus.conn {
		return pm.Events & fdops.R_ERROR, 0
	}

	// pipefops_t.pollone() doesn't allow polling for reading on write-end
	// of pipe and vice versa
	var readyin fdops.Ready_t
	var readyout fdops.Ready_t
	both := pm.Events&(fdops.R_READ|fdops.R_WRITE) == 0
	var err defs.Err_t
	if both || pm.Events&fdops.R_READ != 0 {
		readyin, err = sus.pipein.Pollone(pm)
	}
	if err != 0 {
		return 0, err
	}
	if readyin != 0 {
		return readyin, 0
	}
	if both || pm.Events&fdops.R_WRITE != 0 {
		readyout, err = sus.pipeout.Pollone(pm)
	}
	return readyin | readyout, err
}

func (sus *susfops_t) Fcntl(cmd, opt int) int {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	switch cmd {
	case defs.F_GETFL:
		return int(sus.options)
	case defs.F_SETFL:
		sus.options = defs.Fdopt_t(opt)
		if sus.conn {
			sus.pipein.options = defs.Fdopt_t(opt)
			sus.pipeout.options = defs.Fdopt_t(opt)
		}
		return 0
	default:
		panic("weird cmd")
	}
}

func (sus *susfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	switch opt {
	case defs.SO_ERROR:
		dur := [4]uint8{}
		writen(dur[:], 4, 0, 0)
		did, err := bufarg.Uiowrite(dur[:])
		return did, err
	default:
		return 0, -defs.EOPNOTSUPP
	}
}

func (sus *susfops_t) Setsockopt(int, int, fdops.Userio_i, int) defs.Err_t {
	return -defs.EOPNOTSUPP
}

func (sus *susfops_t) Shutdown(read, write bool) defs.Err_t {
	panic("no imp")
}

var _susid uint64

func susid_new() int {
	newid := atomic.AddUint64(&_susid, 1)
	return int(newid)
}

type allsusl_t struct {
	m map[int]*susl_t
	sync.Mutex
}

var allsusl = allsusl_t{m: map[int]*susl_t{}}

// listening unix stream socket
type susl_t struct {
	sync.Mutex
	waiters         []_suslblog_t
	pollers         fdops.Pollers_t
	opencount       int
	mysid           int
	readyconnectors int
}

type _suslblog_t struct {
	conn *pipe_t
	acc  *pipe_t
	cond *sync.Cond
	err  defs.Err_t
}

func (susl *susl_t) susl_start(mysid, backlog int) {
	blm := 64
	if backlog < 0 || backlog > blm {
		backlog = blm
	}
	susl.waiters = make([]_suslblog_t, backlog)
	for i := range susl.waiters {
		susl.waiters[i].cond = sync.NewCond(susl)
	}
	susl.opencount = 1
	susl.mysid = mysid
}

func (susl *susl_t) _findbed(amconnector bool) (*_suslblog_t, bool) {
	for i := range susl.waiters {
		var chk *pipe_t
		if amconnector {
			chk = susl.waiters[i].conn
		} else {
			chk = susl.waiters[i].acc
		}
		if chk == nil {
			return &susl.waiters[i], true
		}
	}
	return nil, false
}

func (susl *susl_t) _findwaiter(getacceptor bool) (*_suslblog_t, bool) {
	for i := range susl.waiters {
		var chk *pipe_t
		var oth *pipe_t
		if getacceptor {
			chk = susl.waiters[i].acc
			oth = susl.waiters[i].conn
		} else {
			chk = susl.waiters[i].conn
			oth = susl.waiters[i].acc
		}
		if chk != nil && oth == nil {
			return &susl.waiters[i], true
		}
	}
	return nil, false
}

func (susl *susl_t) _slotreset(slot *_suslblog_t) {
	slot.acc = nil
	slot.conn = nil
}

func (susl *susl_t) _getpartner(mypipe *pipe_t, getacceptor,
	noblk bool) (*pipe_t, defs.Err_t) {
	susl.Lock()
	if susl.opencount == 0 {
		susl.Unlock()
		return nil, -defs.EBADF
	}

	var theirs *pipe_t
	// fastpath: is there a peer already waiting?
	s, found := susl._findwaiter(getacceptor)
	if found {
		if getacceptor {
			theirs = s.acc
			s.conn = mypipe
		} else {
			theirs = s.conn
			s.acc = mypipe
		}
		susl.Unlock()
		s.cond.Signal()
		return theirs, 0
	}
	if noblk {
		susl.Unlock()
		return nil, -defs.EWOULDBLOCK
	}
	// darn. wait for a peer.
	b, found := susl._findbed(getacceptor)
	if !found {
		// backlog is full
		susl.Unlock()
		if !getacceptor {
			panic("fixme: allow more accepts than backlog")
		}
		return nil, -defs.ECONNREFUSED
	}
	if getacceptor {
		b.conn = mypipe
		susl.pollers.Wakeready(fdops.R_READ)
	} else {
		b.acc = mypipe
	}
	if getacceptor {
		susl.readyconnectors++
	}
	err := proc.KillableWait(b.cond)
	if err == 0 {
		err = b.err
	}
	if getacceptor {
		theirs = b.acc
	} else {
		theirs = b.conn
	}
	susl._slotreset(b)
	if getacceptor {
		susl.readyconnectors--
	}
	susl.Unlock()
	return theirs, err
}

func (susl *susl_t) connectwait(mypipe *pipe_t) (*pipe_t, defs.Err_t) {
	noblk := false
	return susl._getpartner(mypipe, true, noblk)
}

func (susl *susl_t) acceptwait(mypipe *pipe_t) (*pipe_t, defs.Err_t) {
	noblk := false
	return susl._getpartner(mypipe, false, noblk)
}

func (susl *susl_t) acceptnowait(mypipe *pipe_t) (*pipe_t, defs.Err_t) {
	noblk := true
	return susl._getpartner(mypipe, false, noblk)
}

func (susl *susl_t) susl_reopen(delta int) defs.Err_t {
	ret := defs.Err_t(0)
	dorem := false
	susl.Lock()
	if susl.opencount != 0 {
		susl.opencount += delta
		if susl.opencount == 0 {
			dorem = true
		}
	} else {
		ret = -defs.EBADF
	}

	if dorem {
		limits.Syslimit.Socks.Give()
		// wake up all blocked connectors/acceptors/pollers
		for i := range susl.waiters {
			s := &susl.waiters[i]
			a := s.acc
			b := s.conn
			if a == nil && b == nil {
				continue
			}
			s.err = -defs.ECONNRESET
			s.cond.Signal()
		}
		susl.pollers.Wakeready(fdops.R_READ | fdops.R_HUP | fdops.R_ERROR)
	}

	susl.Unlock()
	if dorem {
		allsusl.Lock()
		delete(allsusl.m, susl.mysid)
		allsusl.Unlock()
	}
	return ret
}

func (susl *susl_t) susl_poll(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	susl.Lock()
	if susl.opencount == 0 {
		susl.Unlock()
		return 0, 0
	}
	if pm.Events&fdops.R_READ != 0 {
		if susl.readyconnectors > 0 {
			susl.Unlock()
			return fdops.R_READ, 0
		}
	}
	var err defs.Err_t
	if pm.Dowait {
		err = susl.pollers.Addpoller(&pm)
	}
	susl.Unlock()
	return 0, err
}

type suslfops_t struct {
	susl    *susl_t
	myaddr  ustr.Ustr
	options defs.Fdopt_t
}

func (sf *suslfops_t) Close() defs.Err_t {
	return sf.susl.susl_reopen(-1)
}

func (sf *suslfops_t) Fstat(*stat.Stat_t) defs.Err_t {
	panic("no imp")
}

func (sf *suslfops_t) Lseek(int, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sf *suslfops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	return nil, -defs.ENODEV
}

func (sf *suslfops_t) Pathi() defs.Inum_t {
	panic("unix stream listener cwd?")
}

func (sf *suslfops_t) Read(fdops.Userio_i) (int, defs.Err_t) {
	return 0, -defs.ENOTCONN
}

func (sf *suslfops_t) Reopen() defs.Err_t {
	return sf.susl.susl_reopen(1)
}

func (sf *suslfops_t) Write(fdops.Userio_i) (int, defs.Err_t) {
	return 0, -defs.EPIPE
}

func (sf *suslfops_t) Truncate(newlen uint) defs.Err_t {
	return -defs.EINVAL
}

func (sf *suslfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sf *suslfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (sf *suslfops_t) Accept(fromsa fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	// the connector has already taken syslimit.Socks (1 sock reservation
	// counts for a connected pair of UNIX stream sockets).
	noblk := sf.options&defs.O_NONBLOCK != 0
	pipein := &pipe_t{}
	pipein.pipe_start()
	var pipeout *pipe_t
	var err defs.Err_t
	if noblk {
		pipeout, err = sf.susl.acceptnowait(pipein)
	} else {
		pipeout, err = sf.susl.acceptwait(pipein)
	}
	if err != 0 {
		return nil, 0, err
	}
	pfin := &pipefops_t{pipe: pipein, options: sf.options}
	pfout := &pipefops_t{pipe: pipeout, writer: true, options: sf.options}
	ret := &susfops_t{pipein: pfin, pipeout: pfout, conn: true,
		options: sf.options}
	return ret, 0, 0
}

func (sf *suslfops_t) Bind([]uint8) defs.Err_t {
	return -defs.EINVAL
}

func (sf *suslfops_t) Connect(sabuf []uint8) defs.Err_t {
	return -defs.EINVAL
}

func (sf *suslfops_t) Listen(backlog int) (fdops.Fdops_i, defs.Err_t) {
	return nil, -defs.EINVAL
}

func (sf *suslfops_t) Sendmsg(fdops.Userio_i, []uint8, []uint8,
	int) (int, defs.Err_t) {
	return 0, -defs.ENOTCONN
}

func (sf *suslfops_t) Recvmsg(fdops.Userio_i, fdops.Userio_i,
	fdops.Userio_i, int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	return 0, 0, 0, 0, -defs.ENOTCONN
}

func (sf *suslfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	return sf.susl.susl_poll(pm)
}

func (sf *suslfops_t) Fcntl(cmd, opt int) int {
	switch cmd {
	case defs.F_GETFL:
		return int(sf.options)
	case defs.F_SETFL:
		sf.options = defs.Fdopt_t(opt)
		return 0
	default:
		panic("weird cmd")
	}
}

func (sf *suslfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	return 0, -defs.EOPNOTSUPP
}

func (sf *suslfops_t) Setsockopt(int, int, fdops.Userio_i, int) defs.Err_t {
	return -defs.EOPNOTSUPP
}

func (sf *suslfops_t) Shutdown(read, write bool) defs.Err_t {
	return -defs.ENOTCONN
}

func sys_listen(p *proc.Proc_t, fdn, backlog int) int {
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	if backlog < 0 {
		backlog = 0
	}
	newfops, err := fd.Fops.Listen(backlog)
	if err != 0 {
		return int(err)
	}
	// replace old fops
	p.Fdl.Lock()
	fd.Fops = newfops
	p.Fdl.Unlock()
	return 0
}

func sys_getsockopt(p *proc.Proc_t, fdn, level, opt, optvaln, optlenn int) int {
	if level != defs.SOL_SOCKET {
		panic("no imp")
	}
	var olen int
	if optlenn != 0 {
		l, err := p.Vm.Userreadn(optlenn, 8)
		if err != 0 {
			return int(err)
		}
		if l < 0 {
			return int(-defs.EFAULT)
		}
		olen = l
	}
	bufarg := p.Vm.Mkuserbuf(optvaln, olen)
	// XXX why intarg??
	intarg := optvaln
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	optwrote, err := fd.Fops.Getsockopt(opt, bufarg, intarg)
	if err != 0 {
		return int(err)
	}
	if optlenn != 0 {
		if err := p.Vm.Userwriten(optlenn, 8, optwrote); err != 0 {
			return int(err)
		}
	}
	return 0
}

func sys_setsockopt(p *proc.Proc_t, fdn, level, opt, optvaln, optlenn int) int {
	if optlenn < 0 {
		return int(-defs.EFAULT)
	}
	var intarg int
	if optlenn >= 4 {
		var err defs.Err_t
		intarg, err = p.Vm.Userreadn(optvaln, 4)
		if err != 0 {
			return int(err)
		}
	}
	bufarg := p.Vm.Mkuserbuf(optvaln, optlenn)
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	err := fd.Fops.Setsockopt(level, opt, bufarg, intarg)
	return int(err)
}

func _closefds(fds []*fd.Fd_t) {
	for _, f := range fds {
		if f != nil {
			fd.Close_panic(f)
		}
	}
}

func sys_fork(parent *proc.Proc_t, ptf *[defs.TFSIZE]uintptr, tforkp int, flags int) int {
	tmp := flags & (defs.FORK_THREAD | defs.FORK_PROCESS)
	if tmp != defs.FORK_THREAD && tmp != defs.FORK_PROCESS {
		return int(-defs.EINVAL)
	}

	mkproc := flags&defs.FORK_PROCESS != 0
	var child *proc.Proc_t
	var childtid defs.Tid_t
	var ret int

	// copy parents trap frame
	chtf := &[defs.TFSIZE]uintptr{}
	*chtf = *ptf

	if mkproc {
		var ok bool
		// lock fd table for copying
		parent.Fdl.Lock()
		cwd := *parent.Cwd
		child, ok = proc.Proc_new(parent.Name, &cwd, parent.Fds, sys)
		parent.Fdl.Unlock()
		if !ok {
			lhits++
			return int(-defs.ENOMEM)
		}

		child.Vm.Pmap, child.Vm.P_pmap, ok = physmem.Pmap_new()
		if !ok {
			goto outproc
		}
		physmem.Refup(child.Vm.P_pmap)

		child.Pwait = &parent.Mywait
		ok = parent.Start_proc(child.Pid)
		if !ok {
			lhits++
			goto outmem
		}

		// fork parent address space
		parent.Vm.Lock_pmap()
		rsp := chtf[defs.TF_RSP]
		doflush, ok := parent.Vm_fork(child, rsp)
		if ok && !doflush {
			panic("no writable segs?")
		}
		// flush all ptes now marked COW
		if doflush {
			parent.Tlbflush()
		}
		parent.Vm.Unlock_pmap()

		if !ok {
			// child page table allocation failed. call
			// proc.Proc_t.terminate which will clean everything up. the
			// parent will get th error code directly.
			child.Thread_dead(child.Tid0(), 0, false)
			return int(-defs.ENOMEM)
		}

		childtid = child.Tid0()
		ret = child.Pid
	} else {
		// validate tfork struct
		tcb, err1 := parent.Vm.Userreadn(tforkp+0, 8)
		tidaddrn, err2 := parent.Vm.Userreadn(tforkp+8, 8)
		stack, err3 := parent.Vm.Userreadn(tforkp+16, 8)
		if err1 != 0 {
			return int(err1)
		}
		if err2 != 0 {
			return int(err2)
		}
		if err3 != 0 {
			return int(err3)
		}
		writetid := tidaddrn != 0
		if tcb != 0 {
			chtf[defs.TF_FSBASE] = uintptr(tcb)
		}

		child = parent
		var ok bool
		childtid, ok = parent.Thread_new()
		if !ok {
			lhits++
			return int(-defs.ENOMEM)
		}
		ok = parent.Start_thread(childtid)
		if !ok {
			lhits++
			parent.Thread_undo(childtid)
			return int(-defs.ENOMEM)
		}

		v := int(childtid)
		chtf[defs.TF_RSP] = uintptr(stack)
		ret = v
		if writetid {
			// it is not a fatal error if some thread unmapped the
			// memory that was supposed to hold the new thread's
			// tid out from under us.
			parent.Vm.Userwriten(tidaddrn, 8, v)
		}
	}

	chtf[defs.TF_RAX] = 0
	child.Sched_add(chtf, childtid)
	return ret
outmem:
	physmem.Refdown(child.Vm.P_pmap)
outproc:
	proc.Tid_del()
	proc.Proc_del(child.Pid)
	_closefds(child.Fds)
	return int(-defs.ENOMEM)
}

func sys_execv(p *proc.Proc_t, tf *[defs.TFSIZE]uintptr, pathn int, argn int) int {
	args, err := p.Userargs(argn)
	if err != 0 {
		return int(err)
	}
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	err = badpath(path)
	if err != 0 {
		return int(err)
	}
	return sys_execv1(p, tf, path, args)
}

var _zvmregion vm.Vmregion_t

func sys_execv1(p *proc.Proc_t, tf *[defs.TFSIZE]uintptr, paths ustr.Ustr,
	args []ustr.Ustr) int {
	// XXX a multithreaded process that execs is broken; POSIX2008 says
	// that all threads should terminate before exec.
	if p.Thread_count() > 1 {
		panic("fix exec with many threads")
	}

	p.Vm.Lock_pmap()
	defer p.Vm.Unlock_pmap()

	// save page trackers in case the exec fails
	ovmreg := p.Vm.Vmregion
	p.Vm.Vmregion = _zvmregion

	// create kernel page table
	opmap := p.Vm.Pmap
	op_pmap := p.Vm.P_pmap
	var ok bool
	p.Vm.Pmap, p.Vm.P_pmap, ok = physmem.Pmap_new()
	if !ok {
		p.Vm.Pmap, p.Vm.P_pmap = opmap, op_pmap
		return int(-defs.ENOMEM)
	}
	physmem.Refup(p.Vm.P_pmap)
	for _, e := range mem.Kents {
		p.Vm.Pmap[e.Pml4slot] = e.Entry
	}

	restore := func() {
		vm.Uvmfree_inner(p.Vm.Pmap, p.Vm.P_pmap, &p.Vm.Vmregion)
		physmem.Refdown(p.Vm.P_pmap)
		p.Vm.Vmregion.Clear()
		p.Vm.Pmap = opmap
		p.Vm.P_pmap = op_pmap
		p.Vm.Vmregion = ovmreg
	}

	// load binary image -- get first block of file
	file, err := thefs.Fs_open(paths, defs.O_RDONLY, 0, p.Cwd, 0, 0)
	if err != 0 {
		restore()
		return int(err)
	}
	defer fd.Close_panic(file)

	hdata := make([]uint8, 512)
	ub := &vm.Fakeubuf_t{}
	ub.Fake_init(hdata)
	ret, err := file.Fops.Read(ub)
	if err != 0 {
		restore()
		return int(err)
	}
	if ret < len(hdata) {
		hdata = hdata[0:ret]
	}

	// assume its always an elf, for now
	elfhdr := &elf_t{hdata}
	ok = elfhdr.sanity()
	if !ok {
		restore()
		return int(-defs.EPERM)
	}

	// elf_load() will create two copies of TLS section: one for the fresh
	// copy and one for thread 0
	freshtls, t0tls, tlssz, err := elfhdr.elf_load(p, file)
	if err != 0 {
		restore()
		return int(err)
	}

	// map new stack
	numstkpages := 6
	// +1 for the guard page
	stksz := (numstkpages + 1) * mem.PGSIZE
	stackva := p.Vm.Unusedva_inner(0x0ff<<39, stksz)
	p.Vm.Vmadd_anon(stackva, mem.PGSIZE, 0)
	p.Vm.Vmadd_anon(stackva+mem.PGSIZE, stksz-mem.PGSIZE, vm.PTE_U|vm.PTE_W)
	stackva += stksz
	// eagerly map first two pages for stack
	stkeagermap := 2
	for i := 0; i < stkeagermap; i++ {
		ptr := uintptr(stackva - (i+1)*mem.PGSIZE)
		_, p_pg, ok := physmem.Refpg_new()
		if !ok {
			restore()
			return int(-defs.ENOMEM)
		}
		_, ok = p.Vm.Page_insert(int(ptr), p_pg, vm.PTE_W|vm.PTE_U, true, nil)
		if !ok {
			restore()
			return int(-defs.ENOMEM)
		}
	}

	// XXX make insertargs not fail by using more than a page...
	argc, argv, err := insertargs(p, args)
	if err != 0 {
		restore()
		return int(err)
	}

	// put special struct on stack: fresh tls start, tls len, and tls0
	// pointer
	words := 4
	buf := make([]uint8, words*8)
	writen(buf, 8, 0, freshtls)
	writen(buf, 8, 8, tlssz)
	writen(buf, 8, 16, t0tls)
	writen(buf, 8, 24, int(runtime.Pspercycle))
	bufdest := stackva - words*8
	tls0addr := bufdest + 2*8

	if err := p.Vm.K2user_inner(buf, bufdest); err != 0 {
		restore()
		return int(err)
	}

	// the exec must succeed now; free old pmap/mapped files
	if op_pmap != 0 {
		vm.Uvmfree_inner(opmap, op_pmap, &ovmreg)
		physmem.Dec_pmap(op_pmap)
	}
	ovmreg.Clear()

	// close fds marked with CLOEXEC
	for fdn, f := range p.Fds {
		if f == nil {
			continue
		}
		if f.Perms&fd.FD_CLOEXEC != 0 {
			if sys.Sys_close(p, fdn) != 0 {
				panic("close")
			}
		}
	}

	// commit new image state
	tf[defs.TF_RSP] = uintptr(bufdest)
	tf[defs.TF_RIP] = uintptr(elfhdr.entry())
	tf[defs.TF_RFLAGS] = uintptr(defs.TF_FL_IF)
	ucseg := uintptr(5)
	udseg := uintptr(6)
	tf[defs.TF_CS] = (ucseg << 3) | 3
	tf[defs.TF_SS] = (udseg << 3) | 3
	tf[defs.TF_RDI] = uintptr(argc)
	tf[defs.TF_RSI] = uintptr(argv)
	tf[defs.TF_RDX] = uintptr(bufdest)
	tf[defs.TF_FSBASE] = uintptr(tls0addr)
	p.Mmapi = mem.USERMIN
	p.Name = paths

	return 0
}

func insertargs(p *proc.Proc_t, sargs []ustr.Ustr) (int, int, defs.Err_t) {
	// find free page
	uva := p.Vm.Unusedva_inner(0, mem.PGSIZE)
	p.Vm.Vmadd_anon(uva, mem.PGSIZE, vm.PTE_U)
	_, p_pg, ok := physmem.Refpg_new()
	if !ok {
		return 0, 0, -defs.ENOMEM
	}
	_, ok = p.Vm.Page_insert(uva, p_pg, vm.PTE_U, true, nil)
	if !ok {
		physmem.Refdown(p_pg)
		return 0, 0, -defs.ENOMEM
	}
	//var args [][]uint8
	args := make([][]uint8, 0, 12)
	for _, str := range sargs {
		args = append(args, []uint8(str))
	}
	argptrs := make([]int, len(args)+1)
	// copy strings to arg page
	cnt := 0
	for i, arg := range args {
		argptrs[i] = uva + cnt
		// add null terminators
		arg = append(arg, 0)
		if err := p.Vm.K2user_inner(arg, uva+cnt); err != 0 {
			// args take up more than a page? the user is on their
			// own.
			return 0, 0, err
		}
		cnt += len(arg)
	}
	argptrs[len(argptrs)-1] = 0
	// now put the array of strings
	argstart := uva + cnt
	vdata, err := p.Vm.Userdmap8_inner(argstart, true)
	if err != 0 || len(vdata) < len(argptrs)*8 {
		fmt.Printf("no room for args")
		// XXX
		return 0, 0, -defs.ENOSPC
	}
	for i, ptr := range argptrs {
		writen(vdata, 8, i*8, ptr)
	}
	return len(args), argstart, 0
}

func (s *syscall_t) Sys_exit(p *proc.Proc_t, tid defs.Tid_t, status int) {
	// set doomed so all other threads die
	p.Doomall()
	p.Thread_dead(tid, status, true)
}

func sys_threxit(p *proc.Proc_t, tid defs.Tid_t, status int) {
	p.Thread_dead(tid, status, false)
}

func sys_wait4(p *proc.Proc_t, tid defs.Tid_t, wpid, statusp, options, rusagep,
	_isthread int) int {
	if wpid == defs.WAIT_MYPGRP || options == defs.WCONTINUED ||
		options == defs.WUNTRACED {
		panic("no imp")
	}

	// no waiting for yourself!
	if tid == defs.Tid_t(wpid) {
		return int(-defs.ECHILD)
	}
	isthread := _isthread != 0
	if isthread && wpid == defs.WAIT_ANY {
		return int(-defs.EINVAL)
	}

	noblk := options&defs.WNOHANG != 0
	var resp proc.Waitst_t
	var err defs.Err_t
	if isthread {
		resp, err = p.Mywait.Reaptid(wpid, noblk)
	} else {
		resp, err = p.Mywait.Reappid(wpid, noblk)
	}

	if err != 0 {
		return int(err)
	}
	if isthread {
		if statusp != 0 {
			err := p.Vm.Userwriten(statusp, 8, resp.Status)
			if err != 0 {
				return int(err)
			}
		}
	} else {
		if statusp != 0 {
			err := p.Vm.Userwriten(statusp, 4, resp.Status)
			if err != 0 {
				return int(err)
			}
		}
		// update total child rusage
		p.Catime.Add(&resp.Atime)
		if rusagep != 0 {
			ru := resp.Atime.To_rusage()
			err = p.Vm.K2user(ru, rusagep)
		}
		if err != 0 {
			return int(err)
		}
	}
	return resp.Pid
}

func sys_kill(p *proc.Proc_t, pid, sig int) int {
	if sig != defs.SIGKILL {
		panic("no imp")
	}
	p, ok := proc.Proc_check(pid)
	if !ok {
		return int(-defs.ESRCH)
	}
	p.Doomall()
	return 0
}

func sys_pread(p *proc.Proc_t, fdn, bufn, lenn, offset int) int {
	fd, err := _fd_read(p, fdn)
	if err != 0 {
		return int(err)
	}
	dst := p.Vm.Mkuserbuf(bufn, lenn)
	ret, err := fd.Fops.Pread(dst, offset)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_pwrite(p *proc.Proc_t, fdn, bufn, lenn, offset int) int {
	fd, err := _fd_write(p, fdn)
	if err != 0 {
		return int(err)
	}
	src := p.Vm.Mkuserbuf(bufn, lenn)
	ret, err := fd.Fops.Pwrite(src, offset)
	if err != 0 {
		return int(err)
	}
	return ret
}

type futexmsg_t struct {
	op      uint
	aux     uint32
	ack     chan int
	othmut  futex_t
	cndtake []chan int
	totake  []_futto_t
	fumem   futumem_t
	timeout time.Time
	useto   bool
}

func (fm *futexmsg_t) fmsg_init(op uint, aux uint32, ack chan int) {
	fm.op = op
	fm.aux = aux
	fm.ack = ack
}

// futex timeout metadata
type _futto_t struct {
	when   time.Time
	tochan <-chan time.Time
	who    chan int
}

type futex_t struct {
	reopen chan int
	cmd    chan futexmsg_t
	_cnds  []chan int
	cnds   []chan int
	_tos   []_futto_t
	tos    []_futto_t
}

func (f *futex_t) cndsleep(c chan int) {
	f.cnds = append(f.cnds, c)
}

func (f *futex_t) cndwake(v int) {
	if len(f.cnds) == 0 {
		return
	}
	c := f.cnds[0]
	f.cnds = f.cnds[1:]
	if len(f.cnds) == 0 {
		f.cnds = f._cnds
	}
	f._torm(c)
	c <- v
}

func (f *futex_t) toadd(who chan int, when time.Time) {
	fto := _futto_t{when, time.After(when.Sub(time.Now())), who}
	f.tos = append(f.tos, fto)
}

func (f *futex_t) tonext() (<-chan time.Time, chan int) {
	if len(f.tos) == 0 {
		return nil, nil
	}
	small := f.tos[0].when
	next := f.tos[0]
	for _, nto := range f.tos {
		if nto.when.Before(small) {
			small = nto.when
			next = nto
		}
	}
	return next.tochan, next.who
}

func (f *futex_t) _torm(who chan int) {
	idx := -1
	for i, nto := range f.tos {
		if nto.who == who {
			idx = i
			break
		}
	}
	if idx == -1 {
		return
	}
	copy(f.tos[idx:], f.tos[idx+1:])
	l := len(f.tos)
	f.tos = f.tos[:l-1]
	if len(f.tos) == 0 {
		f.tos = f._tos
	}
}

func (f *futex_t) towake(who chan int, v int) {
	// remove from tos and cnds
	f._torm(who)
	idx := -1
	for i := range f.cnds {
		if f.cnds[i] == who {
			idx = i
			break
		}
	}
	copy(f.cnds[idx:], f.cnds[idx+1:])
	l := len(f.cnds)
	f.cnds = f.cnds[:l-1]
	if len(f.cnds) == 0 {
		f.cnds = f._cnds
	}
	who <- v
}

const (
	_FUTEX_LAST = defs.FUTEX_CNDGIVE
	// futex internal op
	_FUTEX_CNDTAKE = 4
)

func (f *futex_t) _resume(ack chan int, err defs.Err_t) {
	select {
	case ack <- int(err):
	default:
	}
}

func (f *futex_t) futex_start() {
	res.Kresdebug(res.Onek, "futex daemon")
	maxwait := 10
	f._cnds = make([]chan int, 0, maxwait)
	f.cnds = f._cnds
	f._tos = make([]_futto_t, 0, maxwait)
	f.tos = f._tos

	pack := make(chan int, 1)
	opencount := 1
	for opencount > 0 {
		res.Kunresdebug()
		res.Kresdebug(res.Onek, "futex daemon")
		tochan, towho := f.tonext()
		select {
		case <-tochan:
			f.towake(towho, 0)
		case d := <-f.reopen:
			opencount += d
		case fm := <-f.cmd:
			switch fm.op {
			case defs.FUTEX_SLEEP:
				val, err := fm.fumem.futload()
				if err != 0 {
					f._resume(fm.ack, err)
					break
				}
				if val != fm.aux {
					// owner just unlocked and it's this
					// thread's turn; don't sleep
					f._resume(fm.ack, 0)
				} else {
					if (fm.useto && len(f.tos) >= maxwait) ||
						len(f.cnds) >= maxwait {
						f._resume(fm.ack, -defs.ENOMEM)
						break
					}
					if fm.useto {
						f.toadd(fm.ack, fm.timeout)
					}
					f.cndsleep(fm.ack)
				}
			case defs.FUTEX_WAKE:
				var v int
				if fm.aux == 1 {
					v = 0
				} else if fm.aux == ^uint32(0) {
					v = 1
				} else {
					panic("weird wake n")
				}
				f.cndwake(v)
				f._resume(fm.ack, 0)
			case defs.FUTEX_CNDGIVE:
				// as an optimization to avoid thundering herd
				// after pthread_cond_broadcast(3), move
				// conditional variable's queue of sleepers to
				// the mutex of the thread we wakeup here.
				l := len(f.cnds)
				if l == 0 {
					f._resume(fm.ack, 0)
					break
				}
				here := make([]chan int, l)
				copy(here, f.cnds)
				tohere := make([]_futto_t, len(f.tos))
				copy(tohere, f.tos)

				var nfm futexmsg_t
				nfm.fmsg_init(_FUTEX_CNDTAKE, 0, pack)
				nfm.cndtake = here
				nfm.totake = tohere

				fm.othmut.cmd <- nfm
				err := <-nfm.ack
				if err == 0 {
					f.cnds = f._cnds
					f.tos = f._tos
				}
				f._resume(fm.ack, defs.Err_t(err))
			case _FUTEX_CNDTAKE:
				// add new waiters to our queue; get them
				// tickets
				here := fm.cndtake
				tohere := fm.totake
				if len(f.cnds)+len(here) >= maxwait ||
					len(f.tos)+len(tohere) >= maxwait {
					f._resume(fm.ack, -defs.ENOMEM)
					break
				}
				f.cnds = append(f.cnds, here...)
				f.tos = append(f.tos, tohere...)
				f._resume(fm.ack, 0)
			default:
				panic("bad futex op")
			}
		}
	}
	res.Kunresdebug()
}

type allfutex_t struct {
	sync.Mutex
	m map[uintptr]futex_t
}

var _allfutex = allfutex_t{m: map[uintptr]futex_t{}}

func futex_ensure(uniq uintptr) (futex_t, defs.Err_t) {
	_allfutex.Lock()
	if len(_allfutex.m) > limits.Syslimit.Futexes {
		_allfutex.Unlock()
		var zf futex_t
		return zf, -defs.ENOMEM
	}
	r, ok := _allfutex.m[uniq]
	if !ok {
		r.reopen = make(chan int)
		r.cmd = make(chan futexmsg_t)
		_allfutex.m[uniq] = r
		go r.futex_start()
	}
	_allfutex.Unlock()
	return r, 0
}

// pmap must be locked. maps user va to kernel va. returns kva as uintptr and
// *uint32
func _uva2kva(p *proc.Proc_t, va uintptr) (uintptr, *uint32, defs.Err_t) {
	p.Vm.Lockassert_pmap()

	pte := vm.Pmap_lookup(p.Vm.Pmap, int(va))
	if pte == nil || *pte&vm.PTE_P == 0 || *pte&vm.PTE_U == 0 {
		return 0, nil, -defs.EFAULT
	}
	pgva := physmem.Dmap(*pte & vm.PTE_ADDR)
	pgoff := uintptr(va) & uintptr(vm.PGOFFSET)
	uniq := uintptr(unsafe.Pointer(pgva)) + pgoff
	return uniq, (*uint32)(unsafe.Pointer(uniq)), 0
}

func va2fut(p *proc.Proc_t, va uintptr) (futex_t, defs.Err_t) {
	p.Vm.Lock_pmap()
	defer p.Vm.Unlock_pmap()

	var zf futex_t
	uniq, _, err := _uva2kva(p, va)
	if err != 0 {
		return zf, err
	}
	return futex_ensure(uniq)
}

// an object for atomically looking-up and incrementing/loading from a user
// address
type futumem_t struct {
	p    *proc.Proc_t
	umem uintptr
}

func (fu *futumem_t) futload() (uint32, defs.Err_t) {
	fu.p.Vm.Lock_pmap()
	defer fu.p.Vm.Unlock_pmap()

	_, ptr, err := _uva2kva(fu.p, fu.umem)
	if err != 0 {
		return 0, err
	}
	var ret uint32
	ret = atomic.LoadUint32(ptr)
	return ret, 0
}

func sys_futex(p *proc.Proc_t, _op, _futn, _fut2n, aux, timespecn int) int {
	op := uint(_op)
	if op > _FUTEX_LAST {
		return int(-defs.EINVAL)
	}
	futn := uintptr(_futn)
	fut2n := uintptr(_fut2n)
	// futn must be 4 byte aligned
	if (futn|fut2n)&0x3 != 0 {
		return int(-defs.EINVAL)
	}
	fut, err := va2fut(p, futn)
	if err != 0 {
		return int(err)
	}

	var fm futexmsg_t
	// could lazily allocate one futex channel per thread
	fm.fmsg_init(op, uint32(aux), make(chan int, 1))
	fm.fumem = futumem_t{p, futn}

	if timespecn != 0 {
		_, when, err := p.Vm.Usertimespec(timespecn)
		if err != 0 {
			return int(err)
		}
		n := time.Now()
		if when.Before(n) {
			return int(-defs.EINVAL)
		}
		fm.timeout = when
		fm.useto = true
	}

	if op == defs.FUTEX_CNDGIVE {
		fm.othmut, err = va2fut(p, fut2n)
		if err != 0 {
			return int(err)
		}
	}

	kn := &tinfo.Current().Killnaps
	fut.cmd <- fm
	select {
	case ret := <-fm.ack:
		return ret
	case <-kn.Killch:
		if kn.Kerr == 0 {
			panic("no")
		}
		return int(kn.Kerr)
	}
}

func sys_gettid(p *proc.Proc_t, tid defs.Tid_t) int {
	return int(tid)
}

func sys_fcntl(p *proc.Proc_t, fdn, cmd, opt int) int {
	f, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	switch cmd {
	// general fcntl(2) ops
	case defs.F_GETFD:
		return f.Perms & fd.FD_CLOEXEC
	case defs.F_SETFD:
		if opt&fd.FD_CLOEXEC == 0 {
			f.Perms &^= fd.FD_CLOEXEC
		} else {
			f.Perms |= fd.FD_CLOEXEC
		}
		return 0
	// fd specific fcntl(2) ops
	case defs.F_GETFL, defs.F_SETFL:
		return f.Fops.Fcntl(cmd, opt)
	default:
		return int(-defs.EINVAL)
	}
}

func sys_truncate(p *proc.Proc_t, pathn int, newlen uint) int {
	path, err := p.Vm.Userstr(pathn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	if err := badpath(path); err != 0 {
		return int(err)
	}
	f, err := thefs.Fs_open(path, defs.O_WRONLY, 0, p.Cwd, 0, 0)
	if err != 0 {
		return int(err)
	}
	err = f.Fops.Truncate(newlen)
	fd.Close_panic(f)
	return int(err)
}

func sys_ftruncate(p *proc.Proc_t, fdn int, newlen uint) int {
	fd, ok := p.Fd_get(fdn)
	if !ok {
		return int(-defs.EBADF)
	}
	return int(fd.Fops.Truncate(newlen))
}

func sys_getcwd(p *proc.Proc_t, bufn, sz int) int {
	dst := p.Vm.Mkuserbuf(bufn, sz)
	_, err := dst.Uiowrite([]uint8(p.Cwd.Path))
	if err != 0 {
		return int(err)
	}
	if _, err := dst.Uiowrite([]uint8{0}); err != 0 {
		return int(err)
	}
	return 0
}

func sys_chdir(p *proc.Proc_t, dirn int) int {
	path, err := p.Vm.Userstr(dirn, fs.NAME_MAX)
	if err != 0 {
		return int(err)
	}
	err = badpath(path)
	if err != 0 {
		return int(err)
	}

	p.Cwd.Lock()
	defer p.Cwd.Unlock()

	newfd, err := thefs.Fs_open(path, defs.O_RDONLY|defs.O_DIRECTORY, 0, p.Cwd, 0, 0)
	if err != 0 {
		return int(err)
	}
	fd.Close_panic(p.Cwd.Fd)
	p.Cwd.Fd = newfd
	if path.IsAbsolute() {
		p.Cwd.Path = bpath.Canonicalize(path)
	} else {
		p.Cwd.Path = p.Cwd.Canonicalpath(path)
	}
	return 0
}

func badpath(path ustr.Ustr) defs.Err_t {
	if len(path) == 0 {
		return -defs.ENOENT
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

func _prof_go(en bool) {
	if en {
		prof.init()
		err := pprof.StartCPUProfile(&prof)
		if err != nil {
			fmt.Printf("%v\n", err)
			return
		}
		//runtime.SetBlockProfileRate(1)
	} else {
		pprof.StopCPUProfile()
		prof.dump()

		//pprof.WriteHeapProfile(&prof)
		//prof.dump()

		//p := pprof.Lookup("block")
		//err := p.WriteTo(&prof, 0)
		//if err != nil {
		//	fmt.Printf("%v\n", err)
		//	return
		//}
		//prof.dump()
	}
}

func _prof_nmi(en bool, pmev pmev_t, intperiod int) {
	if en {
		min := uint(intperiod)
		// default unhalted cycles sampling rate
		defperiod := intperiod == 0
		if defperiod && pmev.evid == EV_UNHALTED_CORE_CYCLES {
			cyc := runtime.Cpumhz * 1000000
			samples := uint(1000)
			min = cyc / samples
		}
		max := uint(float64(min) * 1.2)
		if !profhw.startnmi(pmev.evid, pmev.pflags, min, max) {
			fmt.Printf("Failed to start NMI profiling\n")
		}
	} else {
		// stop profiling
		rips, isbt := profhw.stopnmi()
		if len(rips) == 0 {
			fmt.Printf("No samples!\n")
			return
		}
		fmt.Printf("%v samples\n", len(rips))

		if isbt {
			pd := &fs.Profdev
			pd.Lock()
			pd.Prips.Reset()
			pd.Bts = rips
			pd.Unlock()
		} else {
			m := make(map[uintptr]int)
			for _, v := range rips {
				m[v] = m[v] + 1
			}
			prips := fs.Perfrips_t{}
			prips.Init(m)
			sort.Sort(sort.Reverse(&prips))

			pd := &fs.Profdev
			pd.Lock()
			pd.Prips = prips
			pd.Bts = nil
			pd.Unlock()
		}
	}
}

var hacklock sync.Mutex
var hackctrs []int

func _prof_pmc(en bool, events []pmev_t) {
	hacklock.Lock()
	defer hacklock.Unlock()

	if en {
		if hackctrs != nil {
			fmt.Printf("counters in use\n")
			return
		}
		cs, ok := profhw.startpmc(events)
		if ok {
			hackctrs = cs
		} else {
			fmt.Printf("failed to start counters\n")
		}
	} else {
		if hackctrs == nil {
			return
		}
		r := profhw.stoppmc(hackctrs)
		hackctrs = nil
		for i, ev := range events {
			t := ""
			if ev.pflags&EVF_USR != 0 {
				t = "(usr"
			}
			if ev.pflags&EVF_OS != 0 {
				if t != "" {
					t += "+os"
				} else {
					t = "(os"
				}
			}
			if t != "" {
				t += ")"
			}
			n := pmevid_names[ev.evid] + " " + t
			fmt.Printf("%-30s: %15v\n", n, r[i])
		}
	}
}

var fakeptr *proc.Proc_t

//var fakedur = make([][]uint8, 256)
//var duri int

func sys_prof(p *proc.Proc_t, ptype, _events, _pmflags, intperiod int) int {
	en := true
	if ptype&defs.PROF_DISABLE != 0 {
		en = false
	}
	pmflags := pmflag_t(_pmflags)
	switch {
	case ptype&defs.PROF_GOLANG != 0:
		_prof_go(en)
	case ptype&defs.PROF_SAMPLE != 0:
		ev := pmev_t{evid: pmevid_t(_events),
			pflags: pmflags}
		_prof_nmi(en, ev, intperiod)
	case ptype&defs.PROF_COUNT != 0:
		if pmflags&EVF_BACKTRACE != 0 {
			return int(-defs.EINVAL)
		}
		evs := make([]pmev_t, 0, 4)
		for i := uint(0); i < 64; i++ {
			b := 1 << i
			if _events&b != 0 {
				n := pmev_t{}
				n.evid = pmevid_t(b)
				n.pflags = pmflags
				evs = append(evs, n)
			}
		}
		_prof_pmc(en, evs)
	case ptype&defs.PROF_HACK != 0:
		runtime.Setheap(_events << 20)
	case ptype&defs.PROF_HACK2 != 0:
		if _events < 0 {
			return int(-defs.EINVAL)
		}
		fmt.Printf("GOGC = %v\n", _events)
		debug.SetGCPercent(_events)
	case ptype&defs.PROF_HACK3 != 0:
		if _events < 0 {
			return int(-defs.EINVAL)
		}
		buf := make([]uint8, _events)
		if buf == nil {
		}
		//fakedur[duri] = buf
		//duri = (duri + 1) % len(fakedur)
		//for i := 0; i < _events/8; i++ {
		//fakeptr = proc
		//}
	case ptype&defs.PROF_HACK4 != 0:
		makefake(p)
	case ptype&defs.PROF_HACK5 != 0:
		n := _events
		if n < 0 {
			return int(-defs.EINVAL)
		}
		runtime.SetMaxheap(n)
		fmt.Printf("remaining mem: %v\n",
			res.Human(runtime.Remain()))
	case ptype&defs.PROF_HACK6 != 0:
		anum := float64(_events)
		adenom := float64(_pmflags)
		if adenom <= 0 || anum <= 0 {
			return int(-defs.EINVAL)
		}
		frac := anum / adenom
		runtime.Assistfactor = frac
		fmt.Printf("assist factor = %v\n", frac)
	default:
		return int(-defs.EINVAL)
	}
	return 0
}

func makefake(p *proc.Proc_t) defs.Err_t {
	p.Fdl.Lock()
	defer p.Fdl.Unlock()

	made := 0
	const want = 1e6
	newfds := make([]*fd.Fd_t, want)

	for times := 0; times < 4; times++ {
		fmt.Printf("close half...\n")
		for i := 0; i < len(newfds)/2; i++ {
			newfds[i] = nil
		}
		// sattolos
		for i := len(newfds) - 1; i >= 0; i-- {
			si := rand.Intn(i + 1)
			t := newfds[i]
			newfds[i] = newfds[si]
			newfds[si] = t
		}
		for i := range newfds {
			if newfds[i] == nil {
				newfds[i] = thefs.Makefake()
			}
		}
	}

	for i := range newfds {
		if i < len(p.Fds) && p.Fds[i] != nil {
			newfds[i] = p.Fds[i]
		} else {
			made++
		}
	}
	p.Fds = newfds
	fmt.Printf("bloat finished %v\n", made)
	return 0
}

func sys_info(p *proc.Proc_t, n int) int {
	ms := &runtime.MemStats{}
	runtime.ReadMemStats(ms)

	ret := int(-defs.EINVAL)
	switch n {
	case defs.SINFO_GCCOUNT:
		ret = int(ms.NumGC)
	case defs.SINFO_GCPAUSENS:
		ret = int(ms.PauseTotalNs)
	case defs.SINFO_GCHEAPSZ:
		ret = int(ms.Alloc)
		fmt.Printf("Total heap size: %v MB (%v MB)\n",
			runtime.Heapsz()/(1<<20), ms.Alloc>>20)
	case defs.SINFO_GCMS:
		tot := runtime.GCmarktime() + runtime.GCbgsweeptime()
		ret = tot / 1000000
	case defs.SINFO_GCTOTALLOC:
		ret = int(ms.TotalAlloc)
	case defs.SINFO_GCMARKT:
		ret = runtime.GCmarktime() / 1000000
	case defs.SINFO_GCSWEEPT:
		ret = runtime.GCbgsweeptime() / 1000000
	case defs.SINFO_GCWBARRT:
		ret = runtime.GCwbenabledtime() / 1000000
	case defs.SINFO_GCOBJS:
		ret = int(ms.HeapObjects)
	case defs.SINFO_DOGC:
		runtime.GC()
		ret = 0
		p1, p2, pcpg, pcpm := physmem.Pgcount()
		fmt.Printf("global pgcount: %v, %v\n", p1, p2)
		fmt.Printf("per-cpu free lists:\n")
		for i := range pcpg {
			fmt.Printf("   %4v, %4v\n", pcpg[i], pcpm[i])
		}
	case defs.SINFO_PROCLIST:
		//p.Vm.Vmregion.dump()
		fmt.Printf("proc dump:\n")
		proc.Ptable.Iter(func(_ int32, p *proc.Proc_t) bool {
			fmt.Printf("   %3v %v\n", p.Pid, p.Name)
			return false
		})
		ret = 0
	}

	return ret
}

func readn(a []uint8, n int, off int) int {
	p := unsafe.Pointer(&a[off])
	var ret int
	switch n {
	case 8:
		ret = *(*int)(p)
	case 4:
		ret = int(*(*uint32)(p))
	case 2:
		ret = int(*(*uint16)(p))
	case 1:
		ret = int(*(*uint8)(p))
	default:
		panic("no")
	}
	return ret
}

func writen(a []uint8, sz int, off int, val int) {
	p := unsafe.Pointer(&a[off])
	switch sz {
	case 8:
		*(*int)(p) = val
	case 4:
		*(*uint32)(p) = uint32(val)
	case 2:
		*(*uint16)(p) = uint16(val)
	case 1:
		*(*uint8)(p) = uint8(val)
	default:
		panic("no")
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

type elf_t struct {
	data []uint8
}

type elf_phdr struct {
	etype   int
	flags   int
	vaddr   int
	filesz  int
	fileoff int
	memsz   int
}

const (
	ELF_QUARTER = 2
	ELF_HALF    = 4
	ELF_OFF     = 8
	ELF_ADDR    = 8
	ELF_XWORD   = 8
)

func (e *elf_t) sanity() bool {
	// make sure its an elf
	e_ident := 0
	elfmag := 0x464c457f
	t := readn(e.data, ELF_HALF, e_ident)
	if t != elfmag {
		return false
	}

	// and that we read the entire elf header and program headers
	dlen := len(e.data)

	e_ehsize := 0x34
	ehlen := readn(e.data, ELF_QUARTER, e_ehsize)
	if dlen < ehlen {
		fmt.Printf("read too few elf bytes (elf header)\n")
		return false
	}

	e_phoff := 0x20
	e_phentsize := 0x36
	e_phnum := 0x38

	poff := readn(e.data, ELF_OFF, e_phoff)
	phsz := readn(e.data, ELF_QUARTER, e_phentsize)
	phnum := readn(e.data, ELF_QUARTER, e_phnum)
	phend := poff + phsz*phnum
	if dlen < phend {
		fmt.Printf("read too few elf bytes (program headers)\n")
		return false
	}

	return true
}

func (e *elf_t) npheaders() int {
	e_phnum := 0x38
	return readn(e.data, ELF_QUARTER, e_phnum)
}

func (e *elf_t) header(c int) elf_phdr {
	ret := elf_phdr{}

	nph := e.npheaders()
	if c >= nph {
		panic("header idx too large")
	}
	d := e.data
	e_phoff := 0x20
	e_phentsize := 0x36
	hoff := readn(d, ELF_OFF, e_phoff)
	hsz := readn(d, ELF_QUARTER, e_phentsize)

	p_type := 0x0
	p_flags := 0x4
	p_offset := 0x8
	p_vaddr := 0x10
	p_filesz := 0x20
	p_memsz := 0x28
	f := func(w int, sz int) int {
		return readn(d, sz, hoff+c*hsz+w)
	}
	ret.etype = f(p_type, ELF_HALF)
	ret.flags = f(p_flags, ELF_HALF)
	ret.fileoff = f(p_offset, ELF_OFF)
	ret.vaddr = f(p_vaddr, ELF_ADDR)
	ret.filesz = f(p_filesz, ELF_XWORD)
	ret.memsz = f(p_memsz, ELF_XWORD)
	return ret
}

func (e *elf_t) headers() []elf_phdr {
	pnum := e.npheaders()
	ret := make([]elf_phdr, pnum)
	for i := 0; i < pnum; i++ {
		ret[i] = e.header(i)
	}
	return ret
}

func (e *elf_t) entry() int {
	e_entry := 0x18
	return readn(e.data, ELF_ADDR, e_entry)
}

func segload(p *proc.Proc_t, entry int, hdr *elf_phdr, fops fdops.Fdops_i) defs.Err_t {
	if hdr.vaddr%mem.PGSIZE != hdr.fileoff%mem.PGSIZE {
		panic("requires copying")
	}
	perms := vm.PTE_U
	//PF_X := 1
	PF_W := 2
	if hdr.flags&PF_W != 0 {
		perms |= vm.PTE_W
	}

	var did int
	// the bss segment's virtual address may start on the same page as the
	// previous segment. if that is the case, we may not be able to avoid
	// copying.
	// XXX this doesn't seem to happen anymore; why was it ever the case?
	if _, ok := p.Vm.Vmregion.Lookup(uintptr(hdr.vaddr)); ok {
		panic("htf")
		va := hdr.vaddr
		pg, err := p.Vm.Userdmap8_inner(va, true)
		if err != 0 {
			return err
		}
		mmapi, err := fops.Mmapi(hdr.fileoff, 1, false)
		if err != 0 {
			return err
		}
		bsrc := mem.Pg2bytes(mmapi[0].Pg)[:]
		bsrc = bsrc[va&int(vm.PGOFFSET):]
		if len(pg) > hdr.filesz {
			pg = pg[0:hdr.filesz]
		}
		copy(pg, bsrc)
		did = len(pg)
	}
	filesz := util.Roundup(hdr.vaddr+hdr.filesz-did, mem.PGSIZE)
	filesz -= util.Rounddown(hdr.vaddr, mem.PGSIZE)
	p.Vm.Vmadd_file(hdr.vaddr+did, filesz, perms, fops, hdr.fileoff+did)
	// eagerly map the page at the entry address
	if entry >= hdr.vaddr && entry < hdr.vaddr+hdr.memsz {
		ent := uintptr(entry)
		vmi, ok := p.Vm.Vmregion.Lookup(ent)
		if !ok {
			panic("just mapped?")
		}
		err := vm.Sys_pgfault(&p.Vm, vmi, ent, uintptr(vm.PTE_U))
		if err != 0 {
			return err
		}
	}
	if hdr.filesz == hdr.memsz {
		return 0
	}
	// the bss must be zero, but the first bss address may lie on a page
	// which is mapped into the page cache. thus we must create a
	// per-process copy and zero the bss bytes in the copy.
	bssva := hdr.vaddr + hdr.filesz
	bsslen := hdr.memsz - hdr.filesz
	if bssva&int(vm.PGOFFSET) != 0 {
		bpg, err := p.Vm.Userdmap8_inner(bssva, true)
		if err != 0 {
			return err
		}
		if bsslen < len(bpg) {
			bpg = bpg[:bsslen]
		}
		copy(bpg, mem.Zerobpg[:])
		bssva += len(bpg)
		bsslen = util.Roundup(bsslen-len(bpg), mem.PGSIZE)
	}
	// bss may have been completely contained in the copied page.
	if bsslen > 0 {
		p.Vm.Vmadd_anon(bssva, util.Roundup(bsslen, mem.PGSIZE), perms)
	}
	return 0
}

// returns user address of read-only TLS, thread 0's TLS image, TLS size, and
// success. caller must hold proc's pagemap lock.
func (e *elf_t) elf_load(p *proc.Proc_t, f *fd.Fd_t) (int, int, int, defs.Err_t) {
	PT_LOAD := 1
	PT_TLS := 7
	istls := false
	tlssize := 0
	var tlsaddr int
	var tlscopylen int

	gimme := bounds.Bounds(bounds.B_ELF_T_ELF_LOAD)
	entry := e.entry()
	// load each elf segment directly into process memory
	for _, hdr := range e.headers() {
		// XXX get rid of worthless user program segments
		if !res.Resadd_noblock(gimme) {
			return 0, 0, 0, -defs.ENOHEAP
		}
		if hdr.etype == PT_TLS {
			istls = true
			tlsaddr = hdr.vaddr
			tlssize = util.Roundup(hdr.memsz, 8)
			tlscopylen = hdr.filesz
		} else if hdr.etype == PT_LOAD && hdr.vaddr >= mem.USERMIN {
			err := segload(p, entry, &hdr, f.Fops)
			if err != 0 {
				return 0, 0, 0, err
			}
		}
	}

	freshtls := 0
	t0tls := 0
	if istls {
		// create fresh TLS image and map it COW for thread 0
		l := util.Roundup(tlsaddr+tlssize, mem.PGSIZE)
		l -= util.Rounddown(tlsaddr, mem.PGSIZE)

		freshtls = p.Vm.Unusedva_inner(0, 2*l)
		t0tls = freshtls + l
		p.Vm.Vmadd_anon(freshtls, l, vm.PTE_U)
		p.Vm.Vmadd_anon(t0tls, l, vm.PTE_U|vm.PTE_W)
		perms := vm.PTE_U

		for i := 0; i < l; i += mem.PGSIZE {
			// allocator zeros objects, so tbss is already
			// initialized.
			_, p_pg, ok := physmem.Refpg_new()
			if !ok {
				return 0, 0, 0, -defs.ENOMEM
			}
			_, ok = p.Vm.Page_insert(freshtls+i, p_pg, perms,
				true, nil)
			if !ok {
				physmem.Refdown(p_pg)
				return 0, 0, 0, -defs.ENOMEM
			}
			// map fresh TLS for thread 0
			nperms := perms | vm.PTE_COW
			_, ok = p.Vm.Page_insert(t0tls+i, p_pg, nperms, true, nil)
			if !ok {
				physmem.Refdown(p_pg)
				return 0, 0, 0, -defs.ENOMEM
			}
		}
		// copy TLS data to freshtls
		tlsvmi, ok := p.Vm.Vmregion.Lookup(uintptr(tlsaddr))
		if !ok {
			panic("must succeed")
		}
		for i := 0; i < tlscopylen; {
			if !res.Resadd_noblock(gimme) {
				return 0, 0, 0, -defs.ENOHEAP
			}

			_src, p_pg, err := tlsvmi.Filepage(uintptr(tlsaddr + i))
			if err != 0 {
				return 0, 0, 0, err
			}
			off := (tlsaddr + i) & int(vm.PGOFFSET)
			src := mem.Pg2bytes(_src)[off:]
			bpg, err := p.Vm.Userdmap8_inner(freshtls+i, true)
			if err != 0 {
				physmem.Refdown(p_pg)
				return 0, 0, 0, err
			}
			left := tlscopylen - i
			if len(src) > left {
				src = src[0:left]
			}
			copy(bpg, src)
			i += len(src)
			physmem.Refdown(p_pg)
		}

		// amd64 sys 5 abi specifies that the tls pointer references to
		// the first invalid word past the end of the tls
		t0tls += tlssize
	}
	return freshtls, t0tls, tlssize, 0
}
