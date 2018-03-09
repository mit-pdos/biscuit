package main

import "fmt"
import "runtime"
import "runtime/debug"
import "runtime/pprof"
import "sort"
import "sync"
import "sync/atomic"
import "time"
import "unsafe"

import "common"
import "fs"

var _sysbounds = map[int]int{
	common.SYS_READ:       common.Bounds(common.B_SYS_READ),
	common.SYS_WRITE:      common.Bounds(common.B_SYS_WRITE),
	common.SYS_OPEN:       common.Bounds(common.B_SYS_OPEN),
	common.SYS_CLOSE:      common.Bounds(common.B_SYSCALL_T_SYS_CLOSE),
	common.SYS_STAT:       common.Bounds(common.B_SYS_STAT),
	common.SYS_FSTAT:      common.Bounds(common.B_SYS_FSTAT),
	common.SYS_POLL:       common.Bounds(common.B_SYS_POLL),
	common.SYS_LSEEK:      common.Bounds(common.B_SYS_LSEEK),
	common.SYS_MMAP:       common.Bounds(common.B_SYS_MMAP),
	common.SYS_MUNMAP:     common.Bounds(common.B_SYS_MUNMAP),
	common.SYS_SIGACT:     common.Bounds(common.B_SYS_SIGACTION),
	common.SYS_READV:      common.Bounds(common.B_SYS_READV),
	common.SYS_WRITEV:     common.Bounds(common.B_SYS_WRITEV),
	common.SYS_ACCESS:     common.Bounds(common.B_SYS_ACCESS),
	common.SYS_DUP2:       common.Bounds(common.B_SYS_DUP2),
	common.SYS_PAUSE:      common.Bounds(common.B_SYS_PAUSE),
	common.SYS_GETPID:     common.Bounds(common.B_SYS_GETPID),
	common.SYS_GETPPID:    common.Bounds(common.B_SYS_GETPPID),
	common.SYS_SOCKET:     common.Bounds(common.B_SYS_SOCKET),
	common.SYS_CONNECT:    common.Bounds(common.B_SYS_CONNECT),
	common.SYS_ACCEPT:     common.Bounds(common.B_SYS_ACCEPT),
	common.SYS_SENDTO:     common.Bounds(common.B_SYS_SENDTO),
	common.SYS_RECVFROM:   common.Bounds(common.B_SYS_RECVFROM),
	common.SYS_SOCKPAIR:   common.Bounds(common.B_SYS_SOCKETPAIR),
	common.SYS_SHUTDOWN:   common.Bounds(common.B_SYS_SHUTDOWN),
	common.SYS_BIND:       common.Bounds(common.B_SYS_BIND),
	common.SYS_LISTEN:     common.Bounds(common.B_SYS_LISTEN),
	common.SYS_RECVMSG:    common.Bounds(common.B_SYS_RECVMSG),
	common.SYS_SENDMSG:    common.Bounds(common.B_SYS_SENDMSG),
	common.SYS_GETSOCKOPT: common.Bounds(common.B_SYS_GETSOCKOPT),
	common.SYS_SETSOCKOPT: common.Bounds(common.B_SYS_SETSOCKOPT),
	common.SYS_FORK:       common.Bounds(common.B_SYS_FORK),
	common.SYS_EXECV:      common.Bounds(common.B_SYS_EXECV),
	common.SYS_EXIT:       common.Bounds(common.B_SYSCALL_T_SYS_EXIT),
	common.SYS_WAIT4:      common.Bounds(common.B_SYS_WAIT4),
	common.SYS_KILL:       common.Bounds(common.B_SYS_KILL),
	common.SYS_FCNTL:      common.Bounds(common.B_SYS_FCNTL),
	common.SYS_TRUNC:      common.Bounds(common.B_SYS_TRUNCATE),
	common.SYS_FTRUNC:     common.Bounds(common.B_SYS_FTRUNCATE),
	common.SYS_GETCWD:     common.Bounds(common.B_SYS_GETCWD),
	common.SYS_CHDIR:      common.Bounds(common.B_SYS_CHDIR),
	common.SYS_RENAME:     common.Bounds(common.B_SYS_RENAME),
	common.SYS_MKDIR:      common.Bounds(common.B_SYS_MKDIR),
	common.SYS_LINK:       common.Bounds(common.B_SYS_LINK),
	common.SYS_UNLINK:     common.Bounds(common.B_SYS_UNLINK),
	common.SYS_GETTOD:     common.Bounds(common.B_SYS_GETTIMEOFDAY),
	common.SYS_GETRLMT:    common.Bounds(common.B_SYS_GETRLIMIT),
	common.SYS_GETRUSG:    common.Bounds(common.B_SYS_GETRUSAGE),
	common.SYS_MKNOD:      common.Bounds(common.B_SYS_MKNOD),
	common.SYS_SETRLMT:    common.Bounds(common.B_SYS_SETRLIMIT),
	common.SYS_SYNC:       common.Bounds(common.B_SYS_SYNC),
	common.SYS_REBOOT:     common.Bounds(common.B_SYS_REBOOT),
	common.SYS_NANOSLEEP:  common.Bounds(common.B_SYS_NANOSLEEP),
	common.SYS_PIPE2:      common.Bounds(common.B_SYS_PIPE2),
	common.SYS_PROF:       common.Bounds(common.B_SYS_PROF),
	common.SYS_THREXIT:    common.Bounds(common.B_SYS_THREXIT),
	common.SYS_INFO:       common.Bounds(common.B_SYS_INFO),
	common.SYS_PREAD:      common.Bounds(common.B_SYS_PREAD),
	common.SYS_PWRITE:     common.Bounds(common.B_SYS_PWRITE),
	common.SYS_FUTEX:      common.Bounds(common.B_SYS_FUTEX),
	common.SYS_GETTID:     common.Bounds(common.B_SYS_GETTID),
}

// Implements Syscall_i
type syscall_t struct {
}

var sys = &syscall_t{}

func (s *syscall_t) Syscall(p *common.Proc_t, tid common.Tid_t, tf *[common.TFSIZE]uintptr) int {

	if p.Doomed() {
		// this process has been killed
		p.Reap_doomed(tid)
		return 0
	}

	sysno := int(tf[common.TF_RAX])

	lim, ok := _sysbounds[sysno]
	if !ok {
		panic("bad limit")
	}
	if !common.Resadd(lim) {
		fmt.Printf("X")
		return int(-common.ENOHEAP)
	}

	a1 := int(tf[common.TF_RDI])
	a2 := int(tf[common.TF_RSI])
	a3 := int(tf[common.TF_RDX])
	a4 := int(tf[common.TF_RCX])
	a5 := int(tf[common.TF_R8])

	var ret int
	switch sysno {
	case common.SYS_READ:
		ret = sys_read(p, a1, a2, a3)
	case common.SYS_WRITE:
		ret = sys_write(p, a1, a2, a3)
	case common.SYS_OPEN:
		ret = sys_open(p, a1, a2, a3)
	case common.SYS_CLOSE:
		ret = s.Sys_close(p, a1)
	case common.SYS_STAT:
		ret = sys_stat(p, a1, a2)
	case common.SYS_FSTAT:
		ret = sys_fstat(p, a1, a2)
	case common.SYS_POLL:
		ret = sys_poll(p, tid, a1, a2, a3)
	case common.SYS_LSEEK:
		ret = sys_lseek(p, a1, a2, a3)
	case common.SYS_MMAP:
		ret = sys_mmap(p, a1, a2, a3, a4, a5)
	case common.SYS_MUNMAP:
		ret = sys_munmap(p, a1, a2)
	case common.SYS_READV:
		ret = sys_readv(p, a1, a2, a3)
	case common.SYS_WRITEV:
		ret = sys_writev(p, a1, a2, a3)
	case common.SYS_SIGACT:
		ret = sys_sigaction(p, a1, a2, a3)
	case common.SYS_ACCESS:
		ret = sys_access(p, a1, a2)
	case common.SYS_DUP2:
		ret = sys_dup2(p, a1, a2)
	case common.SYS_PAUSE:
		ret = sys_pause(p)
	case common.SYS_GETPID:
		ret = sys_getpid(p, tid)
	case common.SYS_GETPPID:
		ret = sys_getppid(p, tid)
	case common.SYS_SOCKET:
		ret = sys_socket(p, a1, a2, a3)
	case common.SYS_CONNECT:
		ret = sys_connect(p, a1, a2, a3)
	case common.SYS_ACCEPT:
		ret = sys_accept(p, a1, a2, a3)
	case common.SYS_SENDTO:
		ret = sys_sendto(p, a1, a2, a3, a4, a5)
	case common.SYS_RECVFROM:
		ret = sys_recvfrom(p, a1, a2, a3, a4, a5)
	case common.SYS_SOCKPAIR:
		ret = sys_socketpair(p, a1, a2, a3, a4)
	case common.SYS_SHUTDOWN:
		ret = sys_shutdown(p, a1, a2)
	case common.SYS_BIND:
		ret = sys_bind(p, a1, a2, a3)
	case common.SYS_LISTEN:
		ret = sys_listen(p, a1, a2)
	case common.SYS_RECVMSG:
		ret = sys_recvmsg(p, a1, a2, a3)
	case common.SYS_SENDMSG:
		ret = sys_sendmsg(p, a1, a2, a3)
	case common.SYS_GETSOCKOPT:
		ret = sys_getsockopt(p, a1, a2, a3, a4, a5)
	case common.SYS_SETSOCKOPT:
		ret = sys_setsockopt(p, a1, a2, a3, a4, a5)
	case common.SYS_FORK:
		ret = sys_fork(p, tf, a1, a2)
	case common.SYS_EXECV:
		ret = sys_execv(p, tf, a1, a2)
	case common.SYS_EXIT:
		status := a1 & 0xff
		status |= common.EXITED
		s.Sys_exit(p, tid, status)
	case common.SYS_WAIT4:
		ret = sys_wait4(p, tid, a1, a2, a3, a4, a5)
	case common.SYS_KILL:
		ret = sys_kill(p, a1, a2)
	case common.SYS_FCNTL:
		ret = sys_fcntl(p, a1, a2, a3)
	case common.SYS_TRUNC:
		ret = sys_truncate(p, a1, uint(a2))
	case common.SYS_FTRUNC:
		ret = sys_ftruncate(p, a1, uint(a2))
	case common.SYS_GETCWD:
		ret = sys_getcwd(p, a1, a2)
	case common.SYS_CHDIR:
		ret = sys_chdir(p, a1)
	case common.SYS_RENAME:
		ret = sys_rename(p, a1, a2)
	case common.SYS_MKDIR:
		ret = sys_mkdir(p, a1, a2)
	case common.SYS_LINK:
		ret = sys_link(p, a1, a2)
	case common.SYS_UNLINK:
		ret = sys_unlink(p, a1, a2)
	case common.SYS_GETTOD:
		ret = sys_gettimeofday(p, a1)
	case common.SYS_GETRLMT:
		ret = sys_getrlimit(p, a1, a2)
	case common.SYS_GETRUSG:
		ret = sys_getrusage(p, a1, a2)
	case common.SYS_MKNOD:
		ret = sys_mknod(p, a1, a2, a3)
	case common.SYS_SETRLMT:
		ret = sys_setrlimit(p, a1, a2)
	case common.SYS_SYNC:
		ret = sys_sync(p)
	case common.SYS_REBOOT:
		ret = sys_reboot(p)
	case common.SYS_NANOSLEEP:
		ret = sys_nanosleep(p, a1, a2)
	case common.SYS_PIPE2:
		ret = sys_pipe2(p, a1, a2)
	case common.SYS_PROF:
		ret = sys_prof(p, a1, a2, a3, a4)
	case common.SYS_INFO:
		ret = sys_info(p, a1)
	case common.SYS_THREXIT:
		sys_threxit(p, tid, a1)
	case common.SYS_PREAD:
		ret = sys_pread(p, a1, a2, a3, a4)
	case common.SYS_PWRITE:
		ret = sys_pwrite(p, a1, a2, a3, a4)
	case common.SYS_FUTEX:
		ret = sys_futex(p, a1, a2, a3, a4, a5)
	case common.SYS_GETTID:
		ret = sys_gettid(p, tid)
	default:
		fmt.Printf("unexpected syscall %v\n", sysno)
		s.Sys_exit(p, tid, common.SIGNALED|common.Mkexitsig(31))
	}
	return ret
}

// Implements Console_i
type console_t struct {
}

var console = &console_t{}

func (c *console_t) Cons_read(ub common.Userio_i, offset int) (int, common.Err_t) {
	sz := ub.Remain()
	kdata := kbd_get(sz)
	ret, err := ub.Uiowrite(kdata)
	if err != 0 || ret != len(kdata) {
		panic("dropped keys")
	}
	return ret, 0
}

func (c *console_t) Cons_write(src common.Userio_i, off int) (int, common.Err_t) {
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

func _fd_read(proc *common.Proc_t, fdn int) (*common.Fd_t, common.Err_t) {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return nil, -common.EBADF
	}
	if fd.Perms&common.FD_READ == 0 {
		return nil, -common.EPERM
	}
	return fd, 0
}

func _fd_write(proc *common.Proc_t, fdn int) (*common.Fd_t, common.Err_t) {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return nil, -common.EBADF
	}
	if fd.Perms&common.FD_WRITE == 0 {
		return nil, -common.EPERM
	}
	return fd, 0
}

func sys_read(proc *common.Proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	fd, err := _fd_read(proc, fdn)
	if err != 0 {
		return int(err)
	}
	userbuf := proc.Mkuserbuf_pool(bufp, sz)

	ret, err := fd.Fops.Read(proc, userbuf)
	if err != 0 {
		return int(err)
	}
	common.Ubpool.Put(userbuf)
	return ret
}

func sys_write(proc *common.Proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	fd, err := _fd_write(proc, fdn)
	if err != 0 {
		return int(err)
	}
	userbuf := proc.Mkuserbuf_pool(bufp, sz)

	ret, err := fd.Fops.Write(proc, userbuf)
	if err != 0 {
		return int(err)
	}
	common.Ubpool.Put(userbuf)
	return ret
}

func sys_open(proc *common.Proc_t, pathn int, _flags int, mode int) int {
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	flags := common.Fdopt_t(_flags)
	temp := flags & (common.O_RDONLY | common.O_WRONLY | common.O_RDWR)
	if temp != common.O_RDONLY && temp != common.O_WRONLY && temp != common.O_RDWR {
		return int(-common.EINVAL)
	}
	if temp == common.O_RDONLY && flags&common.O_TRUNC != 0 {
		return int(-common.EINVAL)
	}
	fdperms := 0
	switch temp {
	case common.O_RDONLY:
		fdperms = common.FD_READ
	case common.O_WRONLY:
		fdperms = common.FD_WRITE
	case common.O_RDWR:
		fdperms = common.FD_READ | common.FD_WRITE
	default:
		fdperms = common.FD_READ
	}
	err := badpath(path)
	if err != 0 {
		return int(err)
	}
	pi := proc.Cwd().Fops.Pathi()
	file, err := thefs.Fs_open(path, flags, mode, pi, 0, 0)
	if err != 0 {
		return int(err)
	}
	if flags&common.O_CLOEXEC != 0 {
		fdperms |= common.FD_CLOEXEC
	}
	fdn, ok := proc.Fd_insert(file, fdperms)
	if !ok {
		lhits++
		common.Close_panic(file)
		return int(-common.EMFILE)
	}
	return fdn
}

func sys_pause(proc *common.Proc_t) int {
	// no signals yet!
	var c chan bool
	<-c
	return -1
}

func (s *syscall_t) Sys_close(proc *common.Proc_t, fdn int) int {
	fd, ok := proc.Fd_del(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	ret := fd.Fops.Close()
	return int(ret)
}

// a type to hold the virtual/physical addresses of memory mapped files
type mmapinfo_t struct {
	pg   *common.Pg_t
	phys common.Pa_t
}

func sys_mmap(proc *common.Proc_t, addrn, lenn, protflags, fdn, offset int) int {
	if lenn == 0 {
		return int(-common.EINVAL)
	}
	prot := uint(protflags) >> 32
	flags := uint(uint32(protflags))

	mask := common.MAP_SHARED | common.MAP_PRIVATE
	if flags&mask == 0 || flags&mask == mask {
		return int(-common.EINVAL)
	}
	shared := flags&common.MAP_SHARED != 0
	anon := flags&common.MAP_ANON != 0
	fdmap := !anon
	if (fdmap && fdn < 0) || (fdmap && offset < 0) || (anon && fdn >= 0) {
		return int(-common.EINVAL)
	}
	if flags&common.MAP_FIXED != 0 {
		return int(-common.EINVAL)
	}
	// OpenBSD allows mappings of only PROT_WRITE and read accesses that
	// fault-in the page cause a segfault while writes do not. Reads
	// following a write do not cause segfault (of course). POSIX
	// apparently requires an implementation to support only common.PROT_WRITE,
	// but it seems better to disallow permission schemes that the CPU
	// cannot enforce.
	if prot&common.PROT_READ == 0 {
		return int(-common.EINVAL)
	}
	if prot == common.PROT_NONE {
		panic("no imp")
		return proc.Mmapi
	}

	var fd *common.Fd_t
	if fdmap {
		var ok bool
		fd, ok = proc.Fd_get(fdn)
		if !ok {
			return int(-common.EBADF)
		}
		if fd.Perms&common.FD_READ == 0 ||
			(shared && prot&common.PROT_WRITE != 0 &&
				fd.Perms&common.FD_WRITE == 0) {
			return int(-common.EACCES)
		}
	}

	proc.Lock_pmap()

	perms := common.PTE_U
	if prot&common.PROT_WRITE != 0 {
		perms |= common.PTE_W
	}
	lenn = common.Roundup(lenn, common.PGSIZE)
	// limit checks
	if lenn/int(common.PGSIZE)+proc.Vmregion.Pglen() > proc.Ulim.Pages {
		proc.Unlock_pmap()
		lhits++
		return int(-common.ENOMEM)
	}
	if proc.Vmregion.Novma >= proc.Ulim.Novma {
		proc.Unlock_pmap()
		lhits++
		return int(-common.ENOMEM)
	}

	addr := proc.Unusedva_inner(proc.Mmapi, lenn)
	proc.Mmapi = addr + lenn
	switch {
	case anon && shared:
		proc.Vmadd_shareanon(addr, lenn, perms)
	case anon && !shared:
		proc.Vmadd_anon(addr, lenn, perms)
	case fdmap:
		fops := fd.Fops
		// vmadd_*file will increase the open count on the file
		if shared {
			proc.Vmadd_sharefile(addr, lenn, perms, fops, offset,
				thefs)
		} else {
			proc.Vmadd_file(addr, lenn, perms, fops, offset)
		}
	}
	tshoot := false
	// eagerly map anonymous pages, lazily-map file pages. our vm system
	// supports lazily-mapped private anonymous pages though.
	var ub int
	failed := false
	if anon {
		for i := 0; i < lenn; i += int(common.PGSIZE) {
			_, p_pg, ok := physmem.Refpg_new()
			if !ok {
				failed = true
				break
			}
			ns, ok := proc.Page_insert(addr+i, p_pg, perms, true)
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
		for i := 0; i < ub; i += common.PGSIZE {
			proc.Page_remove(addr + i)
		}
		// removing this region cannot create any more vm objects than
		// what this call to sys_mmap started with.
		if proc.Vmregion.Remove(addr, lenn, proc.Ulim.Novma) != 0 {
			panic("wut")
		}
		ret = int(-common.ENOMEM)
	}
	// sys_mmap won't replace pages since it always finds unused VA space,
	// so the following TLB shootdown is never used.
	if tshoot {
		proc.Tlbshoot(0, 1)
	}
	proc.Unlock_pmap()
	return ret
}

func sys_munmap(proc *common.Proc_t, addrn, len int) int {
	if addrn&int(common.PGOFFSET) != 0 || addrn < common.USERMIN {
		return int(-common.EINVAL)
	}
	proc.Lock_pmap()
	defer proc.Unlock_pmap()

	vmi1, ok1 := proc.Vmregion.Lookup(uintptr(addrn))
	vmi2, ok2 := proc.Vmregion.Lookup(uintptr(addrn+len) - 1)
	if !ok1 || !ok2 || vmi1.Pgn() != vmi2.Pgn() {
		return int(-common.EINVAL)
	}

	err := proc.Vmregion.Remove(addrn, len, proc.Ulim.Novma)
	if err != 0 {
		lhits++
		return int(err)
	}
	// addrn must be page-aligned
	len = common.Roundup(len, common.PGSIZE)
	for i := 0; i < len; i += common.PGSIZE {
		p := addrn + i
		if p < common.USERMIN {
			panic("how")
		}
		proc.Page_remove(p)
	}
	pgs := len >> common.PGSHIFT
	proc.Tlbshoot(uintptr(addrn), pgs)
	return 0
}

func sys_readv(proc *common.Proc_t, fdn, _iovn, iovcnt int) int {
	fd, err := _fd_read(proc, fdn)
	if err != 0 {
		return int(err)
	}
	iovn := uint(_iovn)
	iov := &common.Useriovec_t{}
	if err := iov.Iov_init(proc, iovn, iovcnt); err != 0 {
		return int(err)
	}
	ret, err := fd.Fops.Read(proc, iov)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_writev(proc *common.Proc_t, fdn, _iovn, iovcnt int) int {
	fd, err := _fd_write(proc, fdn)
	if err != 0 {
		return int(err)
	}
	iovn := uint(_iovn)
	iov := &common.Useriovec_t{}
	if err := iov.Iov_init(proc, iovn, iovcnt); err != 0 {
		return int(err)
	}
	ret, err := fd.Fops.Write(proc, iov)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_sigaction(proc *common.Proc_t, sig, actn, oactn int) int {
	panic("no imp")
}

func sys_access(proc *common.Proc_t, pathn, mode int) int {
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	if mode == 0 {
		return int(-common.EINVAL)
	}

	pi := proc.Cwd().Fops.Pathi()
	fsf, err := thefs.Fs_open_inner(path, common.O_RDONLY, 0, pi, 0, 0)
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

func sys_dup2(proc *common.Proc_t, oldn, newn int) int {
	if oldn == newn {
		return newn
	}
	ofd, needclose, err := proc.Fd_dup(oldn, newn)
	if err != 0 {
		return int(err)
	}
	if needclose {
		common.Close_panic(ofd)
	}
	return newn
}

func sys_stat(proc *common.Proc_t, pathn, statn int) int {
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	buf := &common.Stat_t{}
	err := thefs.Fs_stat(path, buf, proc.Cwd().Fops.Pathi())
	if err != 0 {
		return int(err)
	}
	return int(proc.K2user(buf.Bytes(), statn))
}

func sys_fstat(proc *common.Proc_t, fdn int, statn int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	buf := &common.Stat_t{}
	err := fd.Fops.Fstat(buf)
	if err != 0 {
		return int(err)
	}

	return int(proc.K2user(buf.Bytes(), statn))
}

// converts internal states to poll states
// pokes poll status bits into user memory. since we only use one priority
// internally, mask away any POLL bits the user didn't not request.
func _ready2rev(orig int, r common.Ready_t) int {
	inmask := common.POLLIN | common.POLLPRI
	outmask := common.POLLOUT | common.POLLWRBAND
	pbits := 0
	if r&common.R_READ != 0 {
		pbits |= inmask
	}
	if r&common.R_WRITE != 0 {
		pbits |= outmask
	}
	if r&common.R_HUP != 0 {
		pbits |= common.POLLHUP
	}
	if r&common.R_ERROR != 0 {
		pbits |= common.POLLERR
	}
	wantevents := ((orig >> 32) & 0xffff) | common.POLLNVAL | common.POLLERR | common.POLLHUP
	revents := wantevents & pbits
	return orig | (revents << 48)
}

func _checkfds(proc *common.Proc_t, tid common.Tid_t, pm *common.Pollmsg_t, wait bool, buf []uint8,
	nfds int) (int, bool, common.Err_t) {
	inmask := common.POLLIN | common.POLLPRI
	outmask := common.POLLOUT | common.POLLWRBAND
	readyfds := 0
	writeback := false
	proc.Fdl.Lock()
	for i := 0; i < nfds; i++ {
		off := i * 8
		uw := readn(buf, 8, off)
		fdn := int(uint32(uw))
		// fds < 0 are to be ignored
		if fdn < 0 {
			continue
		}
		fd, ok := proc.Fd_get_inner(fdn)
		if !ok {
			uw |= common.POLLNVAL
			writen(buf, 8, off, uw)
			writeback = true
			continue
		}
		var pev common.Ready_t
		events := int((uint(uw) >> 32) & 0xffff)
		// one priority
		if events&inmask != 0 {
			pev |= common.R_READ
		}
		if events&outmask != 0 {
			pev |= common.R_WRITE
		}
		if events&common.POLLHUP != 0 {
			pev |= common.R_HUP
		}
		// poll unconditionally reports ERR, HUP, and NVAL
		pev |= common.R_ERROR | common.R_HUP
		pm.Pm_set(tid, pev, wait)
		devstatus, err := fd.Fops.Pollone(*pm)
		if err != 0 {
			proc.Fdl.Unlock()
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
	proc.Fdl.Unlock()
	return readyfds, writeback, 0
}

func sys_poll(proc *common.Proc_t, tid common.Tid_t, fdsn, nfds, timeout int) int {
	if nfds < 0 || timeout < -1 {
		return int(-common.EINVAL)
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
		return int(-common.EINVAL)
	}
	buf := make([]uint8, sz)
	if err := proc.User2k(buf, fdsn); err != 0 {
		return int(err)
	}

	// first we tell the underlying device to notify us if their fd is
	// ready. if a device is immediately ready, we don't bother to register
	// notifiers with the rest of the devices -- we just ask their status
	// too.
	pm := common.Pollmsg_t{}
	for {
		wait := timeout != 0
		rfds, writeback, err := _checkfds(proc, tid, &pm, wait, buf,
			nfds)
		if err != 0 {
			return int(err)
		}
		if writeback {
			if err := proc.K2user(buf, fdsn); err != 0 {
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
			panic("must succeed")
		}
		if timedout {
			return 0
		}
	}
}

func sys_lseek(proc *common.Proc_t, fdn, off, whence int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}

	ret, err := fd.Fops.Lseek(off, whence)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_pipe2(proc *common.Proc_t, pipen, _flags int) int {
	rfp := common.FD_READ
	wfp := common.FD_WRITE

	flags := common.Fdopt_t(_flags)
	var opts common.Fdopt_t
	if flags&common.O_NONBLOCK != 0 {
		opts |= common.O_NONBLOCK
	}

	if flags&common.O_CLOEXEC != 0 {
		rfp |= common.FD_CLOEXEC
		wfp |= common.FD_CLOEXEC
	}

	// if there is an error, pipe_t.op_reopen() will release the pipe
	// reservation.
	if !common.Syslimit.Pipes.Take() {
		lhits++
		return int(-common.ENOMEM)
	}

	p := &pipe_t{lraise: true}
	p.pipe_start()
	rops := &pipefops_t{pipe: p, writer: false, options: opts}
	wops := &pipefops_t{pipe: p, writer: true, options: opts}
	rpipe := &common.Fd_t{Fops: rops}
	wpipe := &common.Fd_t{Fops: wops}
	rfd, wfd, ok := proc.Fd_insert2(rpipe, rfp, wpipe, wfp)
	if !ok {
		common.Close_panic(rpipe)
		common.Close_panic(wpipe)
		return int(-common.EMFILE)
	}

	ok1 := proc.Userwriten(pipen, 4, rfd)
	ok2 := proc.Userwriten(pipen+4, 4, wfd)
	if !ok1 || !ok2 {
		err1 := sys.Sys_close(proc, rfd)
		err2 := sys.Sys_close(proc, wfd)
		if err1 != 0 || err2 != 0 {
			panic("must succeed")
		}
		return int(-common.EFAULT)
	}
	return 0
}

type pipe_t struct {
	sync.Mutex
	cbuf    circbuf_t
	rcond   *sync.Cond
	wcond   *sync.Cond
	readers int
	writers int
	closed  bool
	pollers common.Pollers_t
	passfds passfd_t
	// if true, this pipe was allocated against the pipe limit; raise it on
	// termination.
	lraise bool
}

func (o *pipe_t) pipe_start() {
	pipesz := common.PGSIZE
	o.cbuf.cb_init(pipesz)
	o.readers, o.writers = 1, 1
	o.rcond = sync.NewCond(o)
	o.wcond = sync.NewCond(o)
}

func (o *pipe_t) op_write(src common.Userio_i, noblock bool) (int, common.Err_t) {
	o.Lock()
	for {
		if o.closed {
			o.Unlock()
			return 0, -common.EBADF
		}
		if o.readers == 0 {
			o.Unlock()
			return 0, -common.EPIPE
		}
		if !o.cbuf.full() {
			break
		}
		if noblock {
			o.Unlock()
			return 0, -common.EWOULDBLOCK
		}
		o.wcond.Wait()
	}
	ret, err := o.cbuf.copyin(src)
	if err != 0 {
		o.Unlock()
		return 0, err
	}
	o.rcond.Signal()
	o.pollers.Wakeready(common.R_READ)
	o.Unlock()

	return ret, 0
}

func (o *pipe_t) op_read(dst common.Userio_i, noblock bool) (int, common.Err_t) {
	o.Lock()
	for {
		if o.closed {
			o.Unlock()
			return 0, -common.EBADF
		}
		if o.writers == 0 || !o.cbuf.empty() {
			break
		}
		if noblock {
			o.Unlock()
			return 0, -common.EWOULDBLOCK
		}
		o.rcond.Wait()
	}
	ret, err := o.cbuf.copyout(dst)
	if err != 0 {
		o.Unlock()
		return 0, err
	}
	o.wcond.Signal()
	o.pollers.Wakeready(common.R_WRITE)
	o.Unlock()

	return ret, 0
}

func (o *pipe_t) op_poll(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	o.Lock()

	if o.closed {
		o.Unlock()
		return 0, 0
	}

	var r common.Ready_t
	readable := false
	if !o.cbuf.empty() || o.writers == 0 {
		readable = true
	}
	writeable := false
	if !o.cbuf.full() || o.readers == 0 {
		writeable = true
	}
	if pm.Events&common.R_READ != 0 && readable {
		r |= common.R_READ
	}
	if pm.Events&common.R_HUP != 0 && o.writers == 0 {
		r |= common.R_HUP
	} else if pm.Events&common.R_WRITE != 0 && writeable {
		r |= common.R_WRITE
	}
	if r != 0 || !pm.Dowait {
		o.Unlock()
		return r, 0
	}
	err := o.pollers.Addpoller(&pm)
	o.Unlock()
	return 0, err
}

func (o *pipe_t) op_reopen(rd, wd int) common.Err_t {
	o.Lock()
	if o.closed {
		o.Unlock()
		return -common.EBADF
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
		o.cbuf.cb_release()
		o.passfds.closeall()
		if o.lraise {
			common.Syslimit.Pipes.Give()
		}
	}
	o.Unlock()
	return 0
}

func (o *pipe_t) op_fdadd(nfd *common.Fd_t) common.Err_t {
	o.Lock()
	defer o.Unlock()

	// KILL HERE
	for !o.passfds.add(nfd) {
		o.wcond.Wait()
	}
	return 0
}

func (o *pipe_t) op_fdtake() (*common.Fd_t, bool) {
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
	options common.Fdopt_t
	writer  bool
}

func (of *pipefops_t) Close() common.Err_t {
	var ret common.Err_t
	if of.writer {
		ret = of.pipe.op_reopen(0, -1)
	} else {
		ret = of.pipe.op_reopen(-1, 0)
	}
	return ret
}

func (of *pipefops_t) Fstat(st *common.Stat_t) common.Err_t {
	// linux and openbsd give same mode for all pipes
	st.Wdev(0)
	pipemode := uint(3 << 16)
	st.Wmode(pipemode)
	return 0
}

func (of *pipefops_t) Lseek(int, int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (of *pipefops_t) Mmapi(int, int, bool) ([]common.Mmapinfo_t, common.Err_t) {
	return nil, -common.EINVAL
}

func (of *pipefops_t) Pathi() common.Inum_t {
	panic("pipe cwd")
}

func (of *pipefops_t) Read(p *common.Proc_t, dst common.Userio_i) (int, common.Err_t) {
	noblk := of.options&common.O_NONBLOCK != 0
	return of.pipe.op_read(dst, noblk)
}

func (of *pipefops_t) Reopen() common.Err_t {
	var ret common.Err_t
	if of.writer {
		ret = of.pipe.op_reopen(0, 1)
	} else {
		ret = of.pipe.op_reopen(1, 0)
	}
	return ret
}

func (of *pipefops_t) Write(p *common.Proc_t, src common.Userio_i) (int, common.Err_t) {
	noblk := of.options&common.O_NONBLOCK != 0
	c := 0
	for c != src.Totalsz() {
		if !common.Resadd(common.Bounds(common.B_PIPEFOPS_T_WRITE)) {
			return c, -common.ENOHEAP
		}
		ret, err := of.pipe.op_write(src, noblk)
		if noblk || err != 0 {
			return ret, err
		}
		c += ret
	}
	return c, 0
}

func (of *pipefops_t) Fullpath() (string, common.Err_t) {
	panic("weird cwd")
}

func (of *pipefops_t) Truncate(uint) common.Err_t {
	return -common.EINVAL
}

func (of *pipefops_t) Pread(common.Userio_i, int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (of *pipefops_t) Pwrite(common.Userio_i, int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (of *pipefops_t) Accept(*common.Proc_t, common.Userio_i) (common.Fdops_i, int, common.Err_t) {
	return nil, 0, -common.ENOTSOCK
}

func (of *pipefops_t) Bind(*common.Proc_t, []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (of *pipefops_t) Connect(*common.Proc_t, []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (of *pipefops_t) Listen(*common.Proc_t, int) (common.Fdops_i, common.Err_t) {
	return nil, -common.ENOTSOCK
}

func (of *pipefops_t) Sendmsg(*common.Proc_t, common.Userio_i, []uint8, []uint8,
	int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (of *pipefops_t) Recvmsg(*common.Proc_t, common.Userio_i, common.Userio_i,
	common.Userio_i, int) (int, int, int, common.Msgfl_t, common.Err_t) {
	return 0, 0, 0, 0, -common.ENOTSOCK
}

func (of *pipefops_t) Pollone(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	if of.writer {
		pm.Events &^= common.R_READ
	} else {
		pm.Events &^= common.R_WRITE
	}
	return of.pipe.op_poll(pm)
}

func (of *pipefops_t) Fcntl(proc *common.Proc_t, cmd, opt int) int {
	switch cmd {
	case common.F_GETFL:
		return int(of.options)
	case common.F_SETFL:
		of.options = common.Fdopt_t(opt)
		return 0
	default:
		panic("weird cmd")
	}
}

func (of *pipefops_t) Getsockopt(*common.Proc_t, int, common.Userio_i, int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (of *pipefops_t) Setsockopt(*common.Proc_t, int, int, common.Userio_i, int) common.Err_t {
	return -common.ENOTSOCK
}

func (of *pipefops_t) Shutdown(read, write bool) common.Err_t {
	return -common.ENOTCONN
}

func sys_rename(proc *common.Proc_t, oldn int, newn int) int {
	old, ok1, toolong1 := proc.Userstr(oldn, fs.NAME_MAX)
	new, ok2, toolong2 := proc.Userstr(newn, fs.NAME_MAX)
	if !ok1 || !ok2 {
		return int(-common.EFAULT)
	}
	if toolong1 || toolong2 {
		return int(-common.ENAMETOOLONG)
	}
	err1 := badpath(old)
	err2 := badpath(new)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	err := thefs.Fs_rename(old, new, proc.Cwd().Fops.Pathi())
	return int(err)
}

func sys_mkdir(proc *common.Proc_t, pathn int, mode int) int {
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	err := badpath(path)
	if err != 0 {
		return int(err)
	}
	err = thefs.Fs_mkdir(path, mode, proc.Cwd().Fops.Pathi())
	return int(err)
}

func sys_link(proc *common.Proc_t, oldn int, newn int) int {
	old, ok1, toolong1 := proc.Userstr(oldn, fs.NAME_MAX)
	new, ok2, toolong2 := proc.Userstr(newn, fs.NAME_MAX)
	if !ok1 || !ok2 {
		return int(-common.EFAULT)
	}
	if toolong1 || toolong2 {
		return int(-common.ENAMETOOLONG)
	}
	err1 := badpath(old)
	err2 := badpath(new)
	if err1 != 0 {
		return int(err1)
	}
	if err2 != 0 {
		return int(err2)
	}
	err := thefs.Fs_link(old, new, proc.Cwd().Fops.Pathi())
	return int(err)
}

func sys_unlink(proc *common.Proc_t, pathn, isdiri int) int {
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	err := badpath(path)
	if err != 0 {
		return int(err)
	}
	wantdir := isdiri != 0
	err = thefs.Fs_unlink(path, proc.Cwd().Fops.Pathi(), wantdir)
	return int(err)
}

func sys_gettimeofday(proc *common.Proc_t, timevaln int) int {
	tvalsz := 16
	now := time.Now()
	buf := make([]uint8, tvalsz)
	us := int(now.UnixNano() / 1000)
	writen(buf, 8, 0, us/1e6)
	writen(buf, 8, 8, us%1e6)
	if err := proc.K2user(buf, timevaln); err != 0 {
		return int(err)
	}
	return 0
}

var _rlimits = map[int]uint{common.RLIMIT_NOFILE: common.RLIM_INFINITY}

func sys_getrlimit(proc *common.Proc_t, resn, rlpn int) int {
	var cur uint
	switch resn {
	case common.RLIMIT_NOFILE:
		cur = proc.Ulim.Nofile
	default:
		return int(-common.EINVAL)
	}
	max := _rlimits[resn]
	ok1 := proc.Userwriten(rlpn, 8, int(cur))
	ok2 := proc.Userwriten(rlpn+8, 8, int(max))
	if !ok1 || !ok2 {
		return int(-common.EFAULT)
	}
	return 0
}

func sys_setrlimit(proc *common.Proc_t, resn, rlpn int) int {
	// XXX root can raise max
	_ncur, ok := proc.Userreadn(rlpn, 8)
	if !ok {
		return int(-common.EFAULT)
	}
	ncur := uint(_ncur)
	if ncur > _rlimits[resn] {
		return int(-common.EINVAL)
	}
	switch resn {
	case common.RLIMIT_NOFILE:
		proc.Ulim.Nofile = ncur
	default:
		return int(-common.EINVAL)
	}
	return 0
}

func sys_getrusage(proc *common.Proc_t, who, rusagep int) int {
	var ru []uint8
	if who == common.RUSAGE_SELF {
		// user time is gathered at thread termination... report user
		// time as best as we can
		tmp := proc.Atime

		proc.Threadi.Lock()
		for tid := range proc.Threadi.Notes {
			if tid == 0 {
			}
			val := 42
			// tid may not exist if the query for the time races
			// with a thread exiting.
			if val > 0 {
				tmp.Userns += int64(val)
			}
		}
		proc.Threadi.Unlock()

		ru = tmp.To_rusage()
	} else if who == common.RUSAGE_CHILDREN {
		ru = proc.Catime.Fetch()
	} else {
		return int(-common.EINVAL)
	}
	if err := proc.K2user(ru, rusagep); err != 0 {
		return int(err)
	}
	return int(-common.ENOSYS)
}

func mkdev(_maj, _min int) uint {
	maj := uint(_maj)
	min := uint(_min)
	if min > 0xff {
		panic("bad minor")
	}
	m := maj<<8 | min
	return uint(m << 32)
}

func unmkdev(d uint) (int, int) {
	return int(d >> 40), int(uint8(d >> 32))
}

func sys_mknod(proc *common.Proc_t, pathn, moden, devn int) int {
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}

	err := badpath(path)
	if err != 0 {
		return int(err)
	}
	maj, min := unmkdev(uint(devn))
	fsf, err := thefs.Fs_open_inner(path, common.O_CREAT, 0, proc.Cwd().Fops.Pathi(), maj, min)
	if err != 0 {
		return int(err)
	}
	if thefs.Fs_close(fsf.Inum) != 0 {
		panic("must succeed")
	}
	return 0
}

func sys_sync(proc *common.Proc_t) int {
	return int(thefs.Fs_sync())
}

func sys_reboot(proc *common.Proc_t) int {
	// who needs ACPI?
	runtime.Lcr3(uintptr(common.P_zeropg))
	// poof
	fmt.Printf("what?\n")
	return 0
}

func sys_nanosleep(proc *common.Proc_t, sleeptsn, remaintsn int) int {
	tot, _, err := proc.Usertimespec(sleeptsn)
	if err != 0 {
		return int(err)
	}
	<-time.After(tot)

	return 0
}

func sys_getpid(proc *common.Proc_t, tid common.Tid_t) int {
	return proc.Pid
}

func sys_getppid(proc *common.Proc_t, tid common.Tid_t) int {
	return proc.Pwait.Pid
}

func sys_socket(proc *common.Proc_t, domain, typ, proto int) int {
	var opts common.Fdopt_t
	if typ&common.SOCK_NONBLOCK != 0 {
		opts |= common.O_NONBLOCK
	}
	var clop int
	if typ&common.SOCK_CLOEXEC != 0 {
		clop = common.FD_CLOEXEC
	}

	var sfops common.Fdops_i
	switch {
	case domain == common.AF_UNIX && typ&common.SOCK_DGRAM != 0:
		if opts != 0 {
			panic("no imp")
		}
		sfops = &sudfops_t{open: 1}
	case domain == common.AF_UNIX && typ&common.SOCK_STREAM != 0:
		sfops = &susfops_t{options: opts}
	case domain == common.AF_INET && typ&common.SOCK_STREAM != 0:
		tfops := &tcpfops_t{tcb: &tcptcb_t{}, options: opts}
		tfops.tcb.openc = 1
		sfops = tfops
	default:
		return int(-common.EINVAL)
	}
	if !common.Syslimit.Socks.Take() {
		lhits++
		return int(-common.ENOMEM)
	}
	file := &common.Fd_t{}
	file.Fops = sfops
	fdn, ok := proc.Fd_insert(file, common.FD_READ|common.FD_WRITE|clop)
	if !ok {
		common.Close_panic(file)
		common.Syslimit.Socks.Give()
		return int(-common.EMFILE)
	}
	return fdn
}

func sys_connect(proc *common.Proc_t, fdn, sockaddrn, socklen int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}

	// copy sockaddr to kernel space to avoid races
	sabuf, err := copysockaddr(proc, sockaddrn, socklen)
	if err != 0 {
		return int(err)
	}
	err = fd.Fops.Connect(proc, sabuf)
	return int(err)
}

func sys_accept(proc *common.Proc_t, fdn, sockaddrn, socklenn int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	var sl int
	if socklenn != 0 {
		l, ok := proc.Userreadn(socklenn, 8)
		if !ok {
			return int(-common.EFAULT)
		}
		if l < 0 {
			return int(-common.EFAULT)
		}
		sl = l
	}
	fromsa := proc.Mkuserbuf_pool(sockaddrn, sl)
	newfops, fromlen, err := fd.Fops.Accept(proc, fromsa)
	common.Ubpool.Put(fromsa)
	if err != 0 {
		return int(err)
	}
	if fromlen != 0 {
		if !proc.Userwriten(socklenn, 8, fromlen) {
			return int(-common.EFAULT)
		}
	}
	newfd := &common.Fd_t{Fops: newfops}
	ret, ok := proc.Fd_insert(newfd, common.FD_READ|common.FD_WRITE)
	if !ok {
		common.Close_panic(newfd)
		return int(-common.EMFILE)
	}
	return ret
}

func copysockaddr(proc *common.Proc_t, san, sl int) ([]uint8, common.Err_t) {
	if sl == 0 {
		return nil, 0
	}
	if sl < 0 {
		return nil, -common.EFAULT
	}
	maxsl := 256
	if sl >= maxsl {
		return nil, -common.ENOTSOCK
	}
	ub := proc.Mkuserbuf_pool(san, sl)
	sabuf := make([]uint8, sl)
	_, err := ub.Uioread(sabuf)
	common.Ubpool.Put(ub)
	if err != 0 {
		return nil, err
	}
	return sabuf, 0
}

func sys_sendto(proc *common.Proc_t, fdn, bufn, flaglen, sockaddrn, socklen int) int {
	fd, err := _fd_write(proc, fdn)
	if err != 0 {
		return int(err)
	}
	flags := int(uint(uint32(flaglen)))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	if buflen < 0 {
		return int(-common.EFAULT)
	}

	// copy sockaddr to kernel space to avoid races
	sabuf, err := copysockaddr(proc, sockaddrn, socklen)
	if err != 0 {
		return int(err)
	}

	buf := proc.Mkuserbuf_pool(bufn, buflen)
	ret, err := fd.Fops.Sendmsg(proc, buf, sabuf, nil, flags)
	common.Ubpool.Put(buf)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_recvfrom(proc *common.Proc_t, fdn, bufn, flaglen, sockaddrn,
	socklenn int) int {
	fd, err := _fd_read(proc, fdn)
	if err != 0 {
		return int(err)
	}
	flags := uint(uint32(flaglen))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	buf := proc.Mkuserbuf_pool(bufn, buflen)

	// is the from address requested?
	var salen int
	if socklenn != 0 {
		l, ok := proc.Userreadn(socklenn, 8)
		if !ok {
			return int(-common.EFAULT)
		}
		salen = l
		if salen < 0 {
			return int(-common.EFAULT)
		}
	}
	fromsa := proc.Mkuserbuf_pool(sockaddrn, salen)
	ret, addrlen, _, _, err := fd.Fops.Recvmsg(proc, buf, fromsa, zeroubuf, 0)
	common.Ubpool.Put(buf)
	common.Ubpool.Put(fromsa)
	if err != 0 {
		return int(err)
	}
	// write new socket size to user space
	if addrlen > 0 {
		if !proc.Userwriten(socklenn, 8, addrlen) {
			return int(-common.EFAULT)
		}
	}
	return ret
}

func sys_recvmsg(proc *common.Proc_t, fdn, _msgn, _flags int) int {
	if _flags != 0 {
		panic("no imp")
	}
	fd, err := _fd_read(proc, fdn)
	if err != 0 {
		return int(err)
	}
	// maybe copy the msghdr to kernel space?
	msgn := uint(_msgn)
	iovn, ok1 := proc.Userreadn(int(msgn+2*8), 8)
	niov, ok2 := proc.Userreadn(int(msgn+3*8), 4)
	cmsgl, ok3 := proc.Userreadn(int(msgn+5*8), 8)
	salen, ok4 := proc.Userreadn(int(msgn+1*8), 8)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return int(-common.EFAULT)
	}

	var saddr common.Userio_i
	saddr = zeroubuf
	if salen > 0 {
		saddrn, ok := proc.Userreadn(int(msgn+0*8), 8)
		if !ok {
			return int(-common.EFAULT)
		}
		ub := proc.Mkuserbuf(saddrn, salen)
		saddr = ub
	}
	var cmsg common.Userio_i
	cmsg = zeroubuf
	if cmsgl > 0 {
		cmsgn, ok := proc.Userreadn(int(msgn+4*8), 8)
		if !ok {
			return int(-common.EFAULT)
		}
		ub := proc.Mkuserbuf(cmsgn, cmsgl)
		cmsg = ub
	}

	iov := &common.Useriovec_t{}
	err = iov.Iov_init(proc, uint(iovn), niov)
	if err != 0 {
		return int(err)
	}

	ret, sawr, cmwr, msgfl, err := fd.Fops.Recvmsg(proc, iov, saddr,
		cmsg, 0)
	if err != 0 {
		return int(err)
	}
	// write size of socket address, ancillary data, and msg flags back to
	// user space
	if !proc.Userwriten(int(msgn+28), 4, int(msgfl)) {
		return int(-common.EFAULT)
	}
	if saddr.Totalsz() != 0 {
		if !proc.Userwriten(int(msgn+1*8), 8, sawr) {
			return int(-common.EFAULT)
		}
	}
	if cmsg.Totalsz() != 0 {
		if !proc.Userwriten(int(msgn+5*8), 8, cmwr) {
			return int(-common.EFAULT)
		}
	}
	return ret
}

func sys_sendmsg(proc *common.Proc_t, fdn, _msgn, _flags int) int {
	if _flags != 0 {
		panic("no imp")
	}
	fd, err := _fd_write(proc, fdn)
	if err != 0 {
		return int(err)
	}
	// maybe copy the msghdr to kernel space?
	msgn := uint(_msgn)
	iovn, ok1 := proc.Userreadn(int(msgn+2*8), 8)
	niov, ok2 := proc.Userreadn(int(msgn+3*8), 8)
	cmsgl, ok3 := proc.Userreadn(int(msgn+5*8), 8)
	salen, ok4 := proc.Userreadn(int(msgn+1*8), 8)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return int(-common.EFAULT)
	}
	// copy to address and ancillary data to kernel space
	var saddr []uint8
	if salen > 0 {
		if salen > 64 {
			return int(-common.EINVAL)
		}
		saddrva, ok := proc.Userreadn(int(msgn+0*8), 8)
		if !ok {
			return int(-common.EFAULT)
		}
		saddr = make([]uint8, salen)
		ub := proc.Mkuserbuf(saddrva, salen)
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
			return int(-common.EINVAL)
		}
		cmsgva, ok := proc.Userreadn(int(msgn+4*8), 8)
		if !ok {
			return int(-common.EFAULT)
		}
		cmsg = make([]uint8, cmsgl)
		ub := proc.Mkuserbuf(cmsgva, cmsgl)
		did, err := ub.Uioread(cmsg)
		if err != 0 {
			return int(err)
		}
		if did != cmsgl {
			panic("how")
		}
	}
	iov := &common.Useriovec_t{}
	err = iov.Iov_init(proc, uint(iovn), niov)
	if err != 0 {
		return int(err)
	}
	ret, err := fd.Fops.Sendmsg(proc, iov, saddr, cmsg, 0)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_socketpair(proc *common.Proc_t, domain, typ, proto int, sockn int) int {
	var opts common.Fdopt_t
	if typ&common.SOCK_NONBLOCK != 0 {
		opts |= common.O_NONBLOCK
	}
	var clop int
	if typ&common.SOCK_CLOEXEC != 0 {
		clop = common.FD_CLOEXEC
	}

	mask := common.SOCK_STREAM | common.SOCK_DGRAM
	if typ&mask == 0 || typ&mask == mask {
		return int(-common.EINVAL)
	}

	if !common.Syslimit.Socks.Take() {
		return int(-common.ENOMEM)
	}

	var sfops1, sfops2 common.Fdops_i
	var err common.Err_t
	switch {
	case domain == common.AF_UNIX && typ&common.SOCK_STREAM != 0:
		sfops1, sfops2, err = _suspair(opts)
	default:
		panic("no imp")
	}

	if err != 0 {
		common.Syslimit.Socks.Give()
		return int(err)
	}

	fd1 := &common.Fd_t{}
	fd1.Fops = sfops1
	fd2 := &common.Fd_t{}
	fd2.Fops = sfops2
	perms := common.FD_READ | common.FD_WRITE | clop
	fdn1, fdn2, ok := proc.Fd_insert2(fd1, perms, fd2, perms)
	if !ok {
		common.Close_panic(fd1)
		common.Close_panic(fd2)
		return int(-common.EMFILE)
	}
	if !proc.Userwriten(sockn, 4, fdn1) ||
		!proc.Userwriten(sockn+4, 4, fdn2) {
		if sys.Sys_close(proc, fdn1) != 0 || sys.Sys_close(proc, fdn2) != 0 {
			panic("must succeed")
		}
		return int(-common.EFAULT)
	}
	return 0
}

func _suspair(opts common.Fdopt_t) (common.Fdops_i, common.Fdops_i, common.Err_t) {
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

func sys_shutdown(proc *common.Proc_t, fdn, how int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	var rdone, wdone bool
	if how&common.SHUT_WR != 0 {
		wdone = true
	}
	if how&common.SHUT_RD != 0 {
		rdone = true
	}
	return int(fd.Fops.Shutdown(rdone, wdone))
}

func sys_bind(proc *common.Proc_t, fdn, sockaddrn, socklen int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}

	sabuf, err := copysockaddr(proc, sockaddrn, socklen)
	if err != 0 {
		return int(err)
	}

	return int(fd.Fops.Bind(proc, sabuf))
}

type sudfops_t struct {
	// this lock protects open and bound; bud has its own lock
	sync.Mutex
	bud   *bud_t
	open  int
	bound bool
}

func (sf *sudfops_t) Close() common.Err_t {
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
		common.Syslimit.Socks.Give()
	}
	sf.Unlock()
	return 0
}

func (sf *sudfops_t) Fstat(s *common.Stat_t) common.Err_t {
	panic("no imp")
}

func (sf *sudfops_t) Mmapi(int, int, bool) ([]common.Mmapinfo_t, common.Err_t) {
	return nil, -common.EINVAL
}

func (sf *sudfops_t) Pathi() common.Inum_t {
	panic("cwd socket?")
}

func (sf *sudfops_t) Read(p *common.Proc_t, dst common.Userio_i) (int, common.Err_t) {
	return 0, -common.EBADF
}

func (sf *sudfops_t) Reopen() common.Err_t {
	sf.Lock()
	sf.open++
	sf.Unlock()
	return 0
}

func (sf *sudfops_t) Write(*common.Proc_t, common.Userio_i) (int, common.Err_t) {
	return 0, -common.EBADF
}

func (sf *sudfops_t) Fullpath() (string, common.Err_t) {
	panic("weird cwd")
}

func (sf *sudfops_t) Truncate(newlen uint) common.Err_t {
	return -common.EINVAL
}

func (sf *sudfops_t) Pread(dst common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sf *sudfops_t) Pwrite(src common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sf *sudfops_t) Lseek(int, int) (int, common.Err_t) {
	return 0, -common.ESPIPE
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

func (sf *sudfops_t) Accept(*common.Proc_t, common.Userio_i) (common.Fdops_i, int, common.Err_t) {
	return nil, 0, -common.EINVAL
}

func (sf *sudfops_t) Bind(proc *common.Proc_t, sa []uint8) common.Err_t {
	sf.Lock()
	defer sf.Unlock()

	if sf.bound {
		return -common.EINVAL
	}

	poff := 2
	path := slicetostr(sa[poff:])
	// try to create the specified file as a special device
	bid := allbuds.bud_id_new()
	pi := proc.Cwd().Fops.Pathi()
	fsf, err := thefs.Fs_open_inner(path, common.O_CREAT|common.O_EXCL, 0, pi, common.D_SUD, int(bid))
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

func (sf *sudfops_t) Connect(proc *common.Proc_t, sabuf []uint8) common.Err_t {
	return -common.EINVAL
}

func (sf *sudfops_t) Listen(proc *common.Proc_t, backlog int) (common.Fdops_i, common.Err_t) {
	return nil, -common.EINVAL
}

func (sf *sudfops_t) Sendmsg(proc *common.Proc_t, src common.Userio_i, sa []uint8,
	cmsg []uint8, flags int) (int, common.Err_t) {
	if len(cmsg) != 0 || flags != 0 {
		panic("no imp")
	}
	poff := 2
	if len(sa) <= poff {
		return 0, -common.EINVAL
	}
	st := &common.Stat_t{}
	path := slicetostr(sa[poff:])
	err := thefs.Fs_stat(path, st, proc.Cwd().Fops.Pathi())
	if err != 0 {
		return 0, err
	}
	maj, min := unmkdev(st.Rdev())
	if maj != common.D_SUD {
		return 0, -common.ECONNREFUSED
	}
	ino := st.Rino()

	bid := budid_t(min)
	bud, ok := allbuds.bud_lookup(bid, common.Inum_t(ino))
	if !ok {
		return 0, -common.ECONNREFUSED
	}

	var bp string
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

func (sf *sudfops_t) Recvmsg(proc *common.Proc_t, dst common.Userio_i,
	fromsa common.Userio_i, cmsg common.Userio_i, flags int) (int, int, int, common.Msgfl_t, common.Err_t) {
	if cmsg.Totalsz() != 0 || flags != 0 {
		panic("no imp")
	}

	sf.Lock()
	defer sf.Unlock()

	// XXX what is recv'ing on an unbound unix datagram socket supposed to
	// do? openbsd and linux seem to block forever.
	if !sf.bound {
		return 0, 0, 0, 0, -common.ECONNREFUSED
	}
	bud := sf.bud

	datadid, addrdid, ancdid, msgfl, err := bud.bud_out(dst, fromsa, cmsg)
	if err != 0 {
		return 0, 0, 0, 0, err
	}
	return datadid, addrdid, ancdid, msgfl, 0
}

func (sf *sudfops_t) Pollone(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	sf.Lock()
	defer sf.Unlock()

	if !sf.bound {
		return pm.Events & common.R_ERROR, 0
	}
	r, err := sf.bud.bud_poll(pm)
	return r, err
}

func (sf *sudfops_t) Fcntl(proc *common.Proc_t, cmd, opt int) int {
	return int(-common.ENOSYS)
}

func (sf *sudfops_t) Getsockopt(proc *common.Proc_t, opt int, bufarg common.Userio_i,
	intarg int) (int, common.Err_t) {
	return 0, -common.EOPNOTSUPP
}

func (sf *sudfops_t) Setsockopt(*common.Proc_t, int, int, common.Userio_i, int) common.Err_t {
	return -common.EOPNOTSUPP
}

func (sf *sudfops_t) Shutdown(read, write bool) common.Err_t {
	return -common.ENOTSOCK
}

type budid_t int

var allbuds = allbud_t{m: make(map[budkey_t]*bud_t)}

// buds are indexed by bid and inode number in order to detect stale socket
// files that happen to have the same bid.
type budkey_t struct {
	bid  budid_t
	priv common.Inum_t
}

type allbud_t struct {
	// leaf lock
	sync.Mutex
	m       map[budkey_t]*bud_t
	nextbid budid_t
}

func (ab *allbud_t) bud_lookup(bid budid_t, fpriv common.Inum_t) (*bud_t, bool) {
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

func (ab *allbud_t) bud_new(bid budid_t, budpath string, fpriv common.Inum_t) *bud_t {
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

func (ab *allbud_t) bud_del(bid budid_t, fpriv common.Inum_t) {
	key := budkey_t{bid, fpriv}
	ab.Lock()
	if _, ok := ab.m[key]; !ok {
		panic("no such bud")
	}
	delete(ab.m, key)
	ab.Unlock()
}

type dgram_t struct {
	from string
	sz   int
}

// a circular buffer for datagrams and their source addresses
type dgrambuf_t struct {
	cbuf   circbuf_t
	dgrams []dgram_t
	// add dgrams at head, remove from tail
	head uint
	tail uint
}

func (db *dgrambuf_t) dg_init(sz int) {
	db.cbuf.cb_init(sz)
	// assume that messages are at least 10 bytes
	db.dgrams = make([]dgram_t, sz/10)
	db.head, db.tail = 0, 0
}

// returns true if there is enough buffers to hold a datagram of size sz
func (db *dgrambuf_t) _canhold(sz int) bool {
	if (db.head-db.tail) == uint(len(db.dgrams)) ||
		db.cbuf.left() < sz {
		return false
	}
	return true
}

func (db *dgrambuf_t) _havedgram() bool {
	return db.head != db.tail
}

func (db *dgrambuf_t) copyin(src common.Userio_i, from string) (int, common.Err_t) {
	// is there a free source address slot and buffer space?
	if !db._canhold(src.Totalsz()) {
		panic("should have blocked")
	}
	did, err := db.cbuf.copyin(src)
	if err != 0 {
		return 0, err
	}
	slot := &db.dgrams[db.head%uint(len(db.dgrams))]
	db.head++
	slot.from = from
	slot.sz = did
	return did, 0
}

func (db *dgrambuf_t) copyout(dst, fromsa, cmsg common.Userio_i) (int, int, common.Err_t) {
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
	did, err := db.cbuf.copyout_n(dst, sz)
	if err != 0 {
		return 0, 0, err
	}
	var fdid int
	if fromsa.Totalsz() != 0 {
		fsaddr := _sockaddr_un(slot.from)
		var err common.Err_t
		fdid, err = fromsa.Uiowrite(fsaddr)
		if err != 0 {
			return 0, 0, err
		}
	}
	// commit tail
	db.tail++
	return did, fdid, 0
}

func (db *dgrambuf_t) dg_release() {
	db.cbuf.cb_release()
}

// convert bound socket path to struct sockaddr_un
func _sockaddr_un(budpath string) []uint8 {
	ret := make([]uint8, 2, 16)
	// len
	writen(ret, 1, 0, len(budpath))
	// family
	writen(ret, 1, 1, common.AF_UNIX)
	// path
	ret = append(ret, budpath...)
	ret = append(ret, 0)
	return ret
}

// a type for bound UNIX datagram sockets
type bud_t struct {
	sync.Mutex
	bid     budid_t
	fpriv   common.Inum_t
	dbuf    dgrambuf_t
	pollers common.Pollers_t
	cond    *sync.Cond
	closed  bool
	bpath   string
}

func (bud *bud_t) bud_init(bid budid_t, bpath string, priv common.Inum_t) {
	bud.bid = bid
	bud.fpriv = priv
	bud.bpath = bpath
	bud.dbuf.dg_init(512)
	bud.cond = sync.NewCond(bud)
}

func (bud *bud_t) _rready() {
	bud.cond.Broadcast()
	bud.pollers.Wakeready(common.R_READ)
}

func (bud *bud_t) _wready() {
	bud.cond.Broadcast()
	bud.pollers.Wakeready(common.R_WRITE)
}

// returns number of bytes written and error
func (bud *bud_t) bud_in(src common.Userio_i, from string, cmsg []uint8) (int, common.Err_t) {
	if len(cmsg) != 0 {
		panic("no imp")
	}
	need := src.Totalsz()
	bud.Lock()
	for {
		if bud.closed {
			bud.Unlock()
			return 0, -common.EBADF
		}
		if bud.dbuf._canhold(need) || bud.closed {
			break
		}
		bud.cond.Wait()
	}
	did, err := bud.dbuf.copyin(src, from)
	bud._rready()
	bud.Unlock()
	if err != 0 && did != need {
		panic("wut")
	}
	return did, err
}

// returns number of bytes written of data, socket address, ancillary data, and
// ancillary message flags...
func (bud *bud_t) bud_out(dst, fromsa, cmsg common.Userio_i) (int, int, int,
	common.Msgfl_t, common.Err_t) {
	if cmsg.Totalsz() != 0 {
		panic("no imp")
	}
	bud.Lock()
	for {
		if bud.closed {
			bud.Unlock()
			return 0, 0, 0, 0, -common.EBADF
		}
		if bud.dbuf._havedgram() {
			break
		}
		bud.cond.Wait()
	}
	ddid, fdid, err := bud.dbuf.copyout(dst, fromsa, cmsg)
	bud._wready()
	bud.Unlock()
	return ddid, fdid, 0, 0, err
}

func (bud *bud_t) bud_poll(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	var ret common.Ready_t
	var err common.Err_t
	bud.Lock()
	if bud.closed {
		goto out
	}
	if pm.Events&common.R_READ != 0 && bud.dbuf._havedgram() {
		ret |= common.R_READ
	}
	if pm.Events&common.R_WRITE != 0 && bud.dbuf._canhold(32) {
		ret |= common.R_WRITE
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
	bud.pollers.Wakeready(common.R_READ | common.R_WRITE | common.R_ERROR)
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
	myaddr  string
	mysid   int
	options common.Fdopt_t
}

func (sus *susfops_t) Close() common.Err_t {
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
		common.Syslimit.Socks.Give()
	}
	return err2
}

func (sus *susfops_t) Fstat(*common.Stat_t) common.Err_t {
	panic("no imp")
}

func (sus *susfops_t) Lseek(int, int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sus *susfops_t) Mmapi(int, int, bool) ([]common.Mmapinfo_t, common.Err_t) {
	return nil, -common.ENODEV
}

func (sus *susfops_t) Pathi() common.Inum_t {
	panic("unix stream cwd?")
}

func (sus *susfops_t) Read(p *common.Proc_t, dst common.Userio_i) (int, common.Err_t) {
	read, _, _, _, err := sus.Recvmsg(p, dst, zeroubuf, zeroubuf, 0)
	return read, err
}

func (sus *susfops_t) Reopen() common.Err_t {
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

func (sus *susfops_t) Write(p *common.Proc_t, src common.Userio_i) (int, common.Err_t) {
	wrote, err := sus.Sendmsg(p, src, nil, nil, 0)
	if err == -common.EPIPE {
		err = -common.ECONNRESET
	}
	return wrote, err
}

func (sus *susfops_t) Fullpath() (string, common.Err_t) {
	panic("weird cwd")
}

func (sus *susfops_t) Truncate(newlen uint) common.Err_t {
	return -common.EINVAL
}

func (sus *susfops_t) Pread(dst common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sus *susfops_t) Pwrite(src common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sus *susfops_t) Accept(*common.Proc_t, common.Userio_i) (common.Fdops_i, int, common.Err_t) {
	return nil, 0, -common.EINVAL
}

func (sus *susfops_t) Bind(proc *common.Proc_t, saddr []uint8) common.Err_t {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.bound {
		return -common.EINVAL
	}
	poff := 2
	path := slicetostr(saddr[poff:])
	sid := susid_new()

	// create special file
	pi := proc.Cwd().Fops.Pathi()
	fsf, err := thefs.Fs_open_inner(path, common.O_CREAT|common.O_EXCL, 0, pi, common.D_SUS, sid)
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

func (sus *susfops_t) Connect(proc *common.Proc_t, saddr []uint8) common.Err_t {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.conn {
		return -common.EISCONN
	}
	poff := 2
	path := slicetostr(saddr[poff:])

	// lookup sid
	st := &common.Stat_t{}
	err := thefs.Fs_stat(path, st, proc.Cwd().Fops.Pathi())
	if err != 0 {
		return err
	}
	maj, min := unmkdev(st.Rdev())
	if maj != common.D_SUS {
		return -common.ECONNREFUSED
	}
	sid := min

	allsusl.Lock()
	susl, ok := allsusl.m[sid]
	allsusl.Unlock()
	if !ok {
		return -common.ECONNREFUSED
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

func (sus *susfops_t) Listen(proc *common.Proc_t, backlog int) (common.Fdops_i, common.Err_t) {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.conn {
		return nil, -common.EISCONN
	}
	if !sus.bound {
		return nil, -common.EINVAL
	}
	if sus.lstn {
		return nil, -common.EINVAL
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

func (sus *susfops_t) Sendmsg(proc *common.Proc_t, src common.Userio_i, toaddr []uint8,
	cmsg []uint8, flags int) (int, common.Err_t) {
	if !sus.conn {
		return 0, -common.ENOTCONN
	}
	if toaddr != nil {
		return 0, -common.EISCONN
	}

	if len(cmsg) > 0 {
		scmsz := 16 + 8
		if len(cmsg) < scmsz {
			return 0, -common.EINVAL
		}
		// allow fd sending
		cmsg_len := readn(cmsg, 8, 0)
		cmsg_level := readn(cmsg, 4, 8)
		cmsg_type := readn(cmsg, 4, 12)
		scm_rights := 1
		if cmsg_len != scmsz || cmsg_level != scm_rights ||
			cmsg_type != common.SOL_SOCKET {
			return 0, -common.EINVAL
		}
		chdrsz := 16
		fdn := readn(cmsg, 4, chdrsz)
		ofd, ok := proc.Fd_get(fdn)
		if !ok {
			return 0, -common.EBADF
		}
		nfd, err := common.Copyfd(ofd)
		if err != 0 {
			return 0, err
		}
		err = sus.pipeout.pipe.op_fdadd(nfd)
		if err != 0 {
			return 0, err
		}
	}

	return sus.pipeout.Write(proc, src)
}

func (sus *susfops_t) _fdrecv(proc *common.Proc_t, cmsg common.Userio_i,
	fl common.Msgfl_t) (int, common.Msgfl_t, common.Err_t) {
	scmsz := 16 + 8
	if cmsg.Totalsz() < scmsz {
		return 0, fl, 0
	}
	nfd, ok := sus.pipein.pipe.op_fdtake()
	if !ok {
		return 0, fl, 0
	}
	nfdn, ok := proc.Fd_insert(nfd, nfd.Perms)
	if !ok {
		common.Close_panic(nfd)
		return 0, fl, -common.EMFILE
	}
	buf := make([]uint8, scmsz)
	writen(buf, 8, 0, scmsz)
	writen(buf, 4, 8, common.SOL_SOCKET)
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

func (sus *susfops_t) Recvmsg(proc *common.Proc_t, dst common.Userio_i, fromsa common.Userio_i,
	cmsg common.Userio_i, flags int) (int, int, int, common.Msgfl_t, common.Err_t) {
	if !sus.conn {
		return 0, 0, 0, 0, -common.ENOTCONN
	}

	ret, err := sus.pipein.Read(proc, dst)
	if err != 0 {
		return 0, 0, 0, 0, err
	}
	cmsglen, msgfl, err := sus._fdrecv(proc, cmsg, 0)
	return ret, 0, cmsglen, msgfl, err
}

func (sus *susfops_t) Pollone(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	if !sus.conn {
		return pm.Events & common.R_ERROR, 0
	}

	// pipefops_t.pollone() doesn't allow polling for reading on write-end
	// of pipe and vice versa
	var readyin common.Ready_t
	var readyout common.Ready_t
	both := pm.Events&(common.R_READ|common.R_WRITE) == 0
	var err common.Err_t
	if both || pm.Events&common.R_READ != 0 {
		readyin, err = sus.pipein.Pollone(pm)
	}
	if err != 0 {
		return 0, err
	}
	if readyin != 0 {
		return readyin, 0
	}
	if both || pm.Events&common.R_WRITE != 0 {
		readyout, err = sus.pipeout.Pollone(pm)
	}
	return readyin | readyout, err
}

func (sus *susfops_t) Fcntl(proc *common.Proc_t, cmd, opt int) int {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	switch cmd {
	case common.F_GETFL:
		return int(sus.options)
	case common.F_SETFL:
		sus.options = common.Fdopt_t(opt)
		if sus.conn {
			sus.pipein.options = common.Fdopt_t(opt)
			sus.pipeout.options = common.Fdopt_t(opt)
		}
		return 0
	default:
		panic("weird cmd")
	}
}

func (sus *susfops_t) Getsockopt(proc *common.Proc_t, opt int, bufarg common.Userio_i,
	intarg int) (int, common.Err_t) {
	switch opt {
	case common.SO_ERROR:
		dur := [4]uint8{}
		writen(dur[:], 4, 0, 0)
		did, err := bufarg.Uiowrite(dur[:])
		return did, err
	default:
		return 0, -common.EOPNOTSUPP
	}
}

func (sus *susfops_t) Setsockopt(*common.Proc_t, int, int, common.Userio_i, int) common.Err_t {
	return -common.EOPNOTSUPP
}

func (sus *susfops_t) Shutdown(read, write bool) common.Err_t {
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
	pollers         common.Pollers_t
	opencount       int
	mysid           int
	readyconnectors int
}

type _suslblog_t struct {
	conn *pipe_t
	acc  *pipe_t
	cond *sync.Cond
	err  common.Err_t
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
	noblk bool) (*pipe_t, common.Err_t) {
	susl.Lock()
	if susl.opencount == 0 {
		susl.Unlock()
		return nil, -common.EBADF
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
		return nil, -common.EWOULDBLOCK
	}
	// darn. wait for a peer.
	b, found := susl._findbed(getacceptor)
	if !found {
		// backlog is full
		susl.Unlock()
		if !getacceptor {
			panic("fixme: allow more accepts than backlog")
		}
		return nil, -common.ECONNREFUSED
	}
	if getacceptor {
		b.conn = mypipe
		susl.pollers.Wakeready(common.R_READ)
	} else {
		b.acc = mypipe
	}
	if getacceptor {
		susl.readyconnectors++
	}
	b.cond.Wait()
	err := b.err
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

func (susl *susl_t) connectwait(mypipe *pipe_t) (*pipe_t, common.Err_t) {
	noblk := false
	return susl._getpartner(mypipe, true, noblk)
}

func (susl *susl_t) acceptwait(mypipe *pipe_t) (*pipe_t, common.Err_t) {
	noblk := false
	return susl._getpartner(mypipe, false, noblk)
}

func (susl *susl_t) acceptnowait(mypipe *pipe_t) (*pipe_t, common.Err_t) {
	noblk := true
	return susl._getpartner(mypipe, false, noblk)
}

func (susl *susl_t) susl_reopen(delta int) common.Err_t {
	ret := common.Err_t(0)
	dorem := false
	susl.Lock()
	if susl.opencount != 0 {
		susl.opencount += delta
		if susl.opencount == 0 {
			dorem = true
		}
	} else {
		ret = -common.EBADF
	}

	if dorem {
		common.Syslimit.Socks.Give()
		// wake up all blocked connectors/acceptors/pollers
		for i := range susl.waiters {
			s := &susl.waiters[i]
			a := s.acc
			b := s.conn
			if a == nil && b == nil {
				continue
			}
			s.err = -common.ECONNRESET
			s.cond.Signal()
		}
		susl.pollers.Wakeready(common.R_READ | common.R_HUP | common.R_ERROR)
	}

	susl.Unlock()
	if dorem {
		allsusl.Lock()
		delete(allsusl.m, susl.mysid)
		allsusl.Unlock()
	}
	return ret
}

func (susl *susl_t) susl_poll(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	susl.Lock()
	if susl.opencount == 0 {
		susl.Unlock()
		return 0, 0
	}
	if pm.Events&common.R_READ != 0 {
		if susl.readyconnectors > 0 {
			susl.Unlock()
			return common.R_READ, 0
		}
	}
	var err common.Err_t
	if pm.Dowait {
		err = susl.pollers.Addpoller(&pm)
	}
	susl.Unlock()
	return 0, err
}

type suslfops_t struct {
	susl    *susl_t
	myaddr  string
	options common.Fdopt_t
}

func (sf *suslfops_t) Close() common.Err_t {
	return sf.susl.susl_reopen(-1)
}

func (sf *suslfops_t) Fstat(*common.Stat_t) common.Err_t {
	panic("no imp")
}

func (sf *suslfops_t) Lseek(int, int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sf *suslfops_t) Mmapi(int, int, bool) ([]common.Mmapinfo_t, common.Err_t) {
	return nil, -common.ENODEV
}

func (sf *suslfops_t) Pathi() common.Inum_t {
	panic("unix stream listener cwd?")
}

func (sf *suslfops_t) Read(*common.Proc_t, common.Userio_i) (int, common.Err_t) {
	return 0, -common.ENOTCONN
}

func (sf *suslfops_t) Reopen() common.Err_t {
	return sf.susl.susl_reopen(1)
}

func (sf *suslfops_t) Write(*common.Proc_t, common.Userio_i) (int, common.Err_t) {
	return 0, -common.EPIPE
}

func (sf *suslfops_t) Fullpath() (string, common.Err_t) {
	panic("weird cwd")
}

func (sf *suslfops_t) Truncate(newlen uint) common.Err_t {
	return -common.EINVAL
}

func (sf *suslfops_t) Pread(dst common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sf *suslfops_t) Pwrite(src common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (sf *suslfops_t) Accept(proc *common.Proc_t,
	fromsa common.Userio_i) (common.Fdops_i, int, common.Err_t) {
	// the connector has already taken syslimit.Socks (1 sock reservation
	// counts for a connected pair of UNIX stream sockets).
	noblk := sf.options&common.O_NONBLOCK != 0
	pipein := &pipe_t{}
	pipein.pipe_start()
	var pipeout *pipe_t
	var err common.Err_t
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

func (sf *suslfops_t) Bind(*common.Proc_t, []uint8) common.Err_t {
	return -common.EINVAL
}

func (sf *suslfops_t) Connect(proc *common.Proc_t, sabuf []uint8) common.Err_t {
	return -common.EINVAL
}

func (sf *suslfops_t) Listen(proc *common.Proc_t, backlog int) (common.Fdops_i, common.Err_t) {
	return nil, -common.EINVAL
}

func (sf *suslfops_t) Sendmsg(*common.Proc_t, common.Userio_i, []uint8, []uint8,
	int) (int, common.Err_t) {
	return 0, -common.ENOTCONN
}

func (sf *suslfops_t) Recvmsg(*common.Proc_t, common.Userio_i, common.Userio_i,
	common.Userio_i, int) (int, int, int, common.Msgfl_t, common.Err_t) {
	return 0, 0, 0, 0, -common.ENOTCONN
}

func (sf *suslfops_t) Pollone(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	return sf.susl.susl_poll(pm)
}

func (sf *suslfops_t) Fcntl(proc *common.Proc_t, cmd, opt int) int {
	switch cmd {
	case common.F_GETFL:
		return int(sf.options)
	case common.F_SETFL:
		sf.options = common.Fdopt_t(opt)
		return 0
	default:
		panic("weird cmd")
	}
}

func (sf *suslfops_t) Getsockopt(proc *common.Proc_t, opt int, bufarg common.Userio_i,
	intarg int) (int, common.Err_t) {
	return 0, -common.EOPNOTSUPP
}

func (sf *suslfops_t) Setsockopt(*common.Proc_t, int, int, common.Userio_i, int) common.Err_t {
	return -common.EOPNOTSUPP
}

func (sf *suslfops_t) Shutdown(read, write bool) common.Err_t {
	return -common.ENOTCONN
}

func sys_listen(proc *common.Proc_t, fdn, backlog int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	if backlog < 0 {
		backlog = 0
	}
	newfops, err := fd.Fops.Listen(proc, backlog)
	if err != 0 {
		return int(err)
	}
	// replace old fops
	proc.Fdl.Lock()
	fd.Fops = newfops
	proc.Fdl.Unlock()
	return 0
}

func sys_getsockopt(proc *common.Proc_t, fdn, level, opt, optvaln, optlenn int) int {
	if level != common.SOL_SOCKET {
		panic("no imp")
	}
	var olen int
	if optlenn != 0 {
		l, ok := proc.Userreadn(optlenn, 8)
		if !ok {
			return int(-common.EFAULT)
		}
		if l < 0 {
			return int(-common.EFAULT)
		}
		olen = l
	}
	bufarg := proc.Mkuserbuf(optvaln, olen)
	// XXX why intarg??
	intarg := optvaln
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	optwrote, err := fd.Fops.Getsockopt(proc, opt, bufarg, intarg)
	if err != 0 {
		return int(err)
	}
	if optlenn != 0 {
		if !proc.Userwriten(optlenn, 8, optwrote) {
			return int(-common.EFAULT)
		}
	}
	return 0
}

func sys_setsockopt(proc *common.Proc_t, fdn, level, opt, optvaln, optlenn int) int {
	if optlenn < 0 {
		return int(-common.EFAULT)
	}
	var intarg int
	if optlenn >= 4 {
		var ok bool
		intarg, ok = proc.Userreadn(optvaln, 4)
		if !ok {
			return int(-common.EFAULT)
		}
	}
	bufarg := proc.Mkuserbuf(optvaln, optlenn)
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	err := fd.Fops.Setsockopt(proc, level, opt, bufarg, intarg)
	return int(err)
}

func _closefds(fds []*common.Fd_t) {
	for _, fd := range fds {
		if fd != nil {
			common.Close_panic(fd)
		}
	}
}

func sys_fork(parent *common.Proc_t, ptf *[common.TFSIZE]uintptr, tforkp int, flags int) int {
	tmp := flags & (common.FORK_THREAD | common.FORK_PROCESS)
	if tmp != common.FORK_THREAD && tmp != common.FORK_PROCESS {
		return int(-common.EINVAL)
	}

	mkproc := flags&common.FORK_PROCESS != 0
	var child *common.Proc_t
	var childtid common.Tid_t
	var ret int

	// copy parents trap frame
	chtf := &[common.TFSIZE]uintptr{}
	*chtf = *ptf

	if mkproc {
		var ok bool
		// lock fd table for copying
		parent.Fdl.Lock()
		child, ok = common.Proc_new(parent.Name, parent.Cwd(), parent.Fds, sys)
		parent.Fdl.Unlock()
		if !ok {
			lhits++
			return int(-common.ENOMEM)
		}

		child.Pmap, child.P_pmap, ok = physmem.Pmap_new()
		if !ok {
			goto outproc
		}
		physmem.Refup(child.P_pmap)

		child.Pwait = &parent.Mywait
		ok = parent.Start_proc(child.Pid)
		if !ok {
			lhits++
			goto outmem
		}

		// fork parent address space
		parent.Lock_pmap()
		rsp := chtf[common.TF_RSP]
		doflush, ok := parent.Vm_fork(child, rsp)
		if ok && !doflush {
			panic("no writable segs?")
		}
		// flush all ptes now marked COW
		if doflush {
			parent.Tlbflush()
		}
		parent.Unlock_pmap()

		if !ok {
			// child page table allocation failed. call
			// common.Proc_t.terminate which will clean everything up. the
			// parent will get th error code directly.
			child.Thread_dead(child.Tid0(), 0, false)
			return int(-common.ENOMEM)
		}

		childtid = child.Tid0()
		ret = child.Pid
	} else {
		// validate tfork struct
		tcb, ok1 := parent.Userreadn(tforkp+0, 8)
		tidaddrn, ok2 := parent.Userreadn(tforkp+8, 8)
		stack, ok3 := parent.Userreadn(tforkp+16, 8)
		if !ok1 || !ok2 || !ok3 {
			return int(-common.EFAULT)
		}
		writetid := tidaddrn != 0
		if tcb != 0 {
			chtf[common.TF_FSBASE] = uintptr(tcb)
		}

		child = parent
		var ok bool
		childtid, ok = parent.Thread_new()
		if !ok {
			lhits++
			return int(-common.ENOMEM)
		}
		ok = parent.Start_thread(childtid)
		if !ok {
			lhits++
			parent.Thread_undo(childtid)
			return int(-common.ENOMEM)
		}

		v := int(childtid)
		chtf[common.TF_RSP] = uintptr(stack)
		ret = v
		if writetid {
			// it is not a fatal error if some thread unmapped the
			// memory that was supposed to hold the new thread's
			// tid out from under us.
			parent.Userwriten(tidaddrn, 8, v)
		}
	}

	chtf[common.TF_RAX] = 0
	child.Sched_add(chtf, childtid)
	return ret
outmem:
	physmem.Refdown(child.P_pmap)
outproc:
	common.Tid_del()
	common.Proc_del(child.Pid)
	_closefds(child.Fds)
	return int(-common.ENOMEM)
}

func sys_execv(proc *common.Proc_t, tf *[common.TFSIZE]uintptr, pathn int, argn int) int {
	args, ok := proc.Userargs(argn)
	if !ok {
		return int(-common.EFAULT)
	}
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	err := badpath(path)
	if err != 0 {
		return int(err)
	}
	return sys_execv1(proc, tf, path, args)
}

var _zvmregion common.Vmregion_t

func sys_execv1(proc *common.Proc_t, tf *[common.TFSIZE]uintptr, paths string,
	args []string) int {
	// XXX a multithreaded process that execs is broken; POSIX2008 says
	// that all threads should terminate before exec.
	if proc.Thread_count() > 1 {
		panic("fix exec with many threads")
	}

	proc.Lock_pmap()
	defer proc.Unlock_pmap()

	// save page trackers in case the exec fails
	ovmreg := proc.Vmregion
	proc.Vmregion = _zvmregion

	// create kernel page table
	opmap := proc.Pmap
	op_pmap := proc.P_pmap
	var ok bool
	proc.Pmap, proc.P_pmap, ok = physmem.Pmap_new()
	if !ok {
		proc.Pmap, proc.P_pmap = opmap, op_pmap
		return int(-common.ENOMEM)
	}
	physmem.Refup(proc.P_pmap)
	for _, e := range common.Kents {
		proc.Pmap[e.Pml4slot] = e.Entry
	}

	restore := func() {
		common.Uvmfree_inner(proc.Pmap, proc.P_pmap, &proc.Vmregion)
		physmem.Refdown(proc.P_pmap)
		proc.Vmregion.Clear()
		proc.Pmap = opmap
		proc.P_pmap = op_pmap
		proc.Vmregion = ovmreg
	}

	// load binary image -- get first block of file
	file, err := thefs.Fs_open(paths, common.O_RDONLY, 0, proc.Cwd().Fops.Pathi(), 0, 0)
	if err != 0 {
		restore()
		return int(err)
	}
	defer func() {
		common.Close_panic(file)
	}()

	hdata := make([]uint8, 512)
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)
	ret, err := file.Fops.Read(proc, ub)
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
		return int(-common.EPERM)
	}

	// elf_load() will create two copies of TLS section: one for the fresh
	// copy and one for thread 0
	freshtls, t0tls, tlssz, err := elfhdr.elf_load(proc, file)
	if err != 0 {
		restore()
		return int(err)
	}

	// map new stack
	numstkpages := 6
	// +1 for the guard page
	stksz := (numstkpages + 1) * common.PGSIZE
	stackva := proc.Unusedva_inner(0x0ff<<39, stksz)
	proc.Vmadd_anon(stackva, common.PGSIZE, 0)
	proc.Vmadd_anon(stackva+common.PGSIZE, stksz-common.PGSIZE, common.PTE_U|common.PTE_W)
	stackva += stksz
	// eagerly map first two pages for stack
	stkeagermap := 2
	for i := 0; i < stkeagermap; i++ {
		p := uintptr(stackva - (i+1)*common.PGSIZE)
		_, p_pg, ok := physmem.Refpg_new()
		if !ok {
			restore()
			return int(-common.ENOMEM)
		}
		_, ok = proc.Page_insert(int(p), p_pg, common.PTE_W|common.PTE_U, true)
		if !ok {
			restore()
			return int(-common.ENOMEM)
		}
	}

	// XXX make insertargs not fail by using more than a page...
	argc, argv, ok := insertargs(proc, args)
	if !ok {
		restore()
		return int(-common.EINVAL)
	}

	// the exec must succeed now; free old pmap/mapped files
	if op_pmap != 0 {
		common.Uvmfree_inner(opmap, op_pmap, &ovmreg)
		physmem.Dec_pmap(op_pmap)
	}
	ovmreg.Clear()

	// close fds marked with CLOEXEC
	for fdn, fd := range proc.Fds {
		if fd == nil {
			continue
		}
		if fd.Perms&common.FD_CLOEXEC != 0 {
			if sys.Sys_close(proc, fdn) != 0 {
				panic("close")
			}
		}
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
	if err := proc.K2user_inner(buf, bufdest); err != 0 {
		panic("must succeed")
	}

	// commit new image state
	tf[common.TF_RSP] = uintptr(bufdest)
	tf[common.TF_RIP] = uintptr(elfhdr.entry())
	tf[common.TF_RFLAGS] = uintptr(common.TF_FL_IF)
	ucseg := uintptr(5)
	udseg := uintptr(6)
	tf[common.TF_CS] = (ucseg << 3) | 3
	tf[common.TF_SS] = (udseg << 3) | 3
	tf[common.TF_RDI] = uintptr(argc)
	tf[common.TF_RSI] = uintptr(argv)
	tf[common.TF_RDX] = uintptr(bufdest)
	tf[common.TF_FSBASE] = uintptr(tls0addr)
	proc.Mmapi = common.USERMIN
	proc.Name = paths

	return 0
}

func insertargs(proc *common.Proc_t, sargs []string) (int, int, bool) {
	// find free page
	uva := proc.Unusedva_inner(0, common.PGSIZE)
	proc.Vmadd_anon(uva, common.PGSIZE, common.PTE_U)
	_, p_pg, ok := physmem.Refpg_new()
	if !ok {
		return 0, 0, false
	}
	_, ok = proc.Page_insert(uva, p_pg, common.PTE_U, true)
	if !ok {
		physmem.Refdown(p_pg)
		return 0, 0, false
	}
	var args [][]uint8
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
		if err := proc.K2user_inner(arg, uva+cnt); err != 0 {
			// args take up more than a page? the user is on their
			// own.
			return 0, 0, false
		}
		cnt += len(arg)
	}
	argptrs[len(argptrs)-1] = 0
	// now put the array of strings
	argstart := uva + cnt
	vdata, ok := proc.Userdmap8_inner(argstart, true)
	if !ok || len(vdata) < len(argptrs)*8 {
		fmt.Printf("no room for args")
		return 0, 0, false
	}
	for i, ptr := range argptrs {
		writen(vdata, 8, i*8, ptr)
	}
	return len(args), argstart, true
}

func (s *syscall_t) Sys_exit(proc *common.Proc_t, tid common.Tid_t, status int) {
	// set doomed to all other threads die
	proc.Doomall()
	proc.Thread_dead(tid, status, true)
}

func sys_threxit(proc *common.Proc_t, tid common.Tid_t, status int) {
	proc.Thread_dead(tid, status, false)
}

func sys_wait4(proc *common.Proc_t, tid common.Tid_t, wpid, statusp, options, rusagep,
	_isthread int) int {
	if wpid == common.WAIT_MYPGRP || options == common.WCONTINUED ||
		options == common.WUNTRACED {
		panic("no imp")
	}

	// no waiting for yourself!
	if tid == common.Tid_t(wpid) {
		return int(-common.ECHILD)
	}
	isthread := _isthread != 0
	if isthread && wpid == common.WAIT_ANY {
		return int(-common.EINVAL)
	}

	noblk := options&common.WNOHANG != 0
	var resp common.Waitst_t
	var err common.Err_t
	if isthread {
		resp, err = proc.Mywait.Reaptid(wpid, noblk)
	} else {
		resp, err = proc.Mywait.Reappid(wpid, noblk)
	}

	if err != 0 {
		return int(err)
	}
	if isthread {
		if statusp != 0 {
			if !proc.Userwriten(statusp, 8, resp.Status) {
				return int(-common.EFAULT)
			}
		}
	} else {
		if statusp != 0 {
			if !proc.Userwriten(statusp, 4, resp.Status) {
				err = -common.EFAULT
			}
		}
		// update total child rusage
		proc.Catime.Add(&resp.Atime)
		if rusagep != 0 {
			ru := resp.Atime.To_rusage()
			err = proc.K2user(ru, rusagep)
		}
		if err != 0 {
			return int(err)
		}
	}
	return resp.Pid
}

func sys_kill(proc *common.Proc_t, pid, sig int) int {
	if sig != common.SIGKILL {
		panic("no imp")
	}
	p, ok := common.Proc_check(pid)
	if !ok {
		return int(-common.ESRCH)
	}
	p.Doomall()
	return 0
}

func sys_pread(proc *common.Proc_t, fdn, bufn, lenn, offset int) int {
	fd, err := _fd_read(proc, fdn)
	if err != 0 {
		return int(err)
	}
	dst := proc.Mkuserbuf(bufn, lenn)
	ret, err := fd.Fops.Pread(dst, offset)
	if err != 0 {
		return int(err)
	}
	return ret
}

func sys_pwrite(proc *common.Proc_t, fdn, bufn, lenn, offset int) int {
	fd, err := _fd_write(proc, fdn)
	if err != 0 {
		return int(err)
	}
	src := proc.Mkuserbuf(bufn, lenn)
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
	_FUTEX_LAST = common.FUTEX_CNDGIVE
	// futex internal op
	_FUTEX_CNDTAKE = 4
)

func (f *futex_t) futex_start() {
	common.Kresdebug(1<<10, "futex daemon")
	maxwait := 10
	f._cnds = make([]chan int, 0, maxwait)
	f.cnds = f._cnds
	f._tos = make([]_futto_t, 0, maxwait)
	f.tos = f._tos

	pack := make(chan int)
	opencount := 1
	for opencount > 0 {
		common.Kunresdebug()
		common.Kresdebug(1<<10, "futex daemon")
		tochan, towho := f.tonext()
		select {
		case <-tochan:
			f.towake(towho, 0)
		case d := <-f.reopen:
			opencount += d
		case fm := <-f.cmd:
			switch fm.op {
			case common.FUTEX_SLEEP:
				val, err := fm.fumem.futload()
				if err != 0 {
					fm.ack <- int(err)
					break
				}
				if val != fm.aux {
					// owner just unlocked and it's this
					// thread's turn; don't sleep
					fm.ack <- 0
				} else {
					if (fm.useto && len(f.tos) >= maxwait) ||
						len(f.cnds) >= maxwait {
						fm.ack <- int(-common.ENOMEM)
						break
					}
					if fm.useto {
						f.toadd(fm.ack, fm.timeout)
					}
					f.cndsleep(fm.ack)
				}
			case common.FUTEX_WAKE:
				var v int
				if fm.aux == 1 {
					v = 0
				} else if fm.aux == ^uint32(0) {
					v = 1
				} else {
					panic("weird wake n")
				}
				f.cndwake(v)
				fm.ack <- 0
			case common.FUTEX_CNDGIVE:
				// as an optimization to avoid thundering herd
				// after pthread_cond_broadcast(3), move
				// conditional variable's queue of sleepers to
				// the mutex of the thread we wakeup here.
				l := len(f.cnds)
				if l == 0 {
					fm.ack <- 0
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
				fm.ack <- err
			case _FUTEX_CNDTAKE:
				// add new waiters to our queue; get them
				// tickets
				here := fm.cndtake
				tohere := fm.totake
				if len(f.cnds)+len(here) >= maxwait ||
					len(f.tos)+len(tohere) >= maxwait {
					fm.ack <- int(-common.ENOMEM)
					break
				}
				f.cnds = append(f.cnds, here...)
				f.tos = append(f.tos, tohere...)
				fm.ack <- 0
			default:
				panic("bad futex op")
			}
		}
	}
	common.Kunresdebug()
}

type allfutex_t struct {
	sync.Mutex
	m map[uintptr]futex_t
}

var _allfutex = allfutex_t{m: map[uintptr]futex_t{}}

func futex_ensure(uniq uintptr) (futex_t, common.Err_t) {
	_allfutex.Lock()
	if len(_allfutex.m) > common.Syslimit.Futexes {
		_allfutex.Unlock()
		var zf futex_t
		return zf, -common.ENOMEM
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
func _uva2kva(proc *common.Proc_t, va uintptr) (uintptr, *uint32, common.Err_t) {
	proc.Lockassert_pmap()

	pte := common.Pmap_lookup(proc.Pmap, int(va))
	if pte == nil || *pte&common.PTE_P == 0 || *pte&common.PTE_U == 0 {
		return 0, nil, -common.EFAULT
	}
	pgva := physmem.Dmap(*pte & common.PTE_ADDR)
	pgoff := uintptr(va) & uintptr(common.PGOFFSET)
	uniq := uintptr(unsafe.Pointer(pgva)) + pgoff
	return uniq, (*uint32)(unsafe.Pointer(uniq)), 0
}

func va2fut(proc *common.Proc_t, va uintptr) (futex_t, common.Err_t) {
	proc.Lock_pmap()
	defer proc.Unlock_pmap()

	var zf futex_t
	uniq, _, err := _uva2kva(proc, va)
	if err != 0 {
		return zf, err
	}
	return futex_ensure(uniq)
}

// an object for atomically looking-up and incrementing/loading from a user
// address
type futumem_t struct {
	proc *common.Proc_t
	umem uintptr
}

func (fu *futumem_t) futload() (uint32, common.Err_t) {
	fu.proc.Lock_pmap()
	defer fu.proc.Unlock_pmap()

	_, ptr, err := _uva2kva(fu.proc, fu.umem)
	if err != 0 {
		return 0, err
	}
	var ret uint32
	ret = atomic.LoadUint32(ptr)
	return ret, 0
}

func sys_futex(proc *common.Proc_t, _op, _futn, _fut2n, aux, timespecn int) int {
	op := uint(_op)
	if op > _FUTEX_LAST {
		return int(-common.EINVAL)
	}
	futn := uintptr(_futn)
	fut2n := uintptr(_fut2n)
	// futn must be 4 byte aligned
	if (futn|fut2n)&0x3 != 0 {
		return int(-common.EINVAL)
	}
	fut, err := va2fut(proc, futn)
	if err != 0 {
		return int(err)
	}

	var fm futexmsg_t
	// could lazily allocate one futex channel per thread
	fm.fmsg_init(op, uint32(aux), make(chan int))
	fm.fumem = futumem_t{proc, futn}

	if timespecn != 0 {
		_, when, err := proc.Usertimespec(timespecn)
		if err != 0 {
			return int(err)
		}
		n := time.Now()
		if when.Before(n) {
			return int(-common.EINVAL)
		}
		fm.timeout = when
		fm.useto = true
	}

	if op == common.FUTEX_CNDGIVE {
		fm.othmut, err = va2fut(proc, fut2n)
		if err != 0 {
			return int(err)
		}
	}

	fut.cmd <- fm
	ret := <-fm.ack
	return ret
}

func sys_gettid(proc *common.Proc_t, tid common.Tid_t) int {
	return int(tid)
}

func sys_fcntl(proc *common.Proc_t, fdn, cmd, opt int) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	switch cmd {
	// general fcntl(2) ops
	case common.F_GETFD:
		return fd.Perms & common.FD_CLOEXEC
	case common.F_SETFD:
		if opt&common.FD_CLOEXEC == 0 {
			fd.Perms &^= common.FD_CLOEXEC
		} else {
			fd.Perms |= common.FD_CLOEXEC
		}
		return 0
	// fd specific fcntl(2) ops
	case common.F_GETFL, common.F_SETFL:
		return fd.Fops.Fcntl(proc, cmd, opt)
	default:
		return int(-common.EINVAL)
	}
}

func sys_truncate(proc *common.Proc_t, pathn int, newlen uint) int {
	path, ok, toolong := proc.Userstr(pathn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	if err := badpath(path); err != 0 {
		return int(err)
	}
	pi := proc.Cwd().Fops.Pathi()
	f, err := thefs.Fs_open(path, common.O_WRONLY, 0, pi, 0, 0)
	if err != 0 {
		return int(err)
	}
	err = f.Fops.Truncate(newlen)
	common.Close_panic(f)
	return int(err)
}

func sys_ftruncate(proc *common.Proc_t, fdn int, newlen uint) int {
	fd, ok := proc.Fd_get(fdn)
	if !ok {
		return int(-common.EBADF)
	}
	return int(fd.Fops.Truncate(newlen))
}

func sys_getcwd(proc *common.Proc_t, bufn, sz int) int {
	dst := proc.Mkuserbuf(bufn, sz)
	pwd, err := proc.Cwd().Fops.Fullpath()
	if err != 0 {
		return int(err)
	}
	_, err = dst.Uiowrite([]uint8(pwd))
	if err != 0 {
		return int(err)
	}
	if _, err := dst.Uiowrite([]uint8{0}); err != 0 {
		return int(err)
	}
	return 0
}

func sys_chdir(proc *common.Proc_t, dirn int) int {
	path, ok, toolong := proc.Userstr(dirn, fs.NAME_MAX)
	if !ok {
		return int(-common.EFAULT)
	}
	if toolong {
		return int(-common.ENAMETOOLONG)
	}
	err := badpath(path)
	if err != 0 {
		return int(err)
	}

	proc.Cwdl.Lock()
	defer proc.Cwdl.Unlock()

	pi := proc.Cwd().Fops.Pathi()
	newcwd, err := thefs.Fs_open(path, common.O_RDONLY|common.O_DIRECTORY, 0, pi, 0, 0)
	if err != 0 {
		return int(err)
	}
	common.Close_panic(proc.Cwd())
	proc.Set_cwd(newcwd)
	return 0
}

func badpath(path string) common.Err_t {
	if len(path) == 0 {
		return -common.ENOENT
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

type perfrips_t struct {
	rips  []uintptr
	times []int
}

func (pr *perfrips_t) init(m map[uintptr]int) {
	l := len(m)
	pr.rips = make([]uintptr, l)
	pr.times = make([]int, l)
	idx := 0
	for k, v := range m {
		pr.rips[idx] = k
		pr.times[idx] = v
		idx++
	}
}

func (pr *perfrips_t) Len() int {
	return len(pr.rips)
}

func (pr *perfrips_t) Less(i, j int) bool {
	return pr.times[i] < pr.times[j]
}

func (pr *perfrips_t) Swap(i, j int) {
	pr.rips[i], pr.rips[j] = pr.rips[j], pr.rips[i]
	pr.times[i], pr.times[j] = pr.times[j], pr.times[i]
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
		rips := profhw.stopnmi()
		if len(rips) == 0 {
			fmt.Printf("No samples!\n")
			return
		}
		fmt.Printf("%v samples\n", len(rips))

		m := make(map[uintptr]int)
		for _, v := range rips {
			m[v] = m[v] + 1
		}
		prips := perfrips_t{}
		prips.init(m)
		sort.Sort(sort.Reverse(&prips))
		for i := 0; i < prips.Len(); i++ {
			r := prips.rips[i]
			t := prips.times[i]
			fmt.Printf("%0.16x -- %10v\n", r, t)
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

var fakeptr *common.Proc_t

//var fakedur = make([][]uint8, 256)
//var duri int

func sys_prof(proc *common.Proc_t, ptype, _events, _pmflags, intperiod int) int {
	en := true
	if ptype&common.PROF_DISABLE != 0 {
		en = false
	}
	switch {
	case ptype&common.PROF_GOLANG != 0:
		_prof_go(en)
	case ptype&common.PROF_SAMPLE != 0:
		ev := pmev_t{evid: pmevid_t(_events),
			pflags: pmflag_t(_pmflags)}
		_prof_nmi(en, ev, intperiod)
	case ptype&common.PROF_COUNT != 0:
		evs := make([]pmev_t, 0, 4)
		for i := uint(0); i < 64; i++ {
			b := 1 << i
			if _events&b != 0 {
				n := pmev_t{}
				n.evid = pmevid_t(b)
				n.pflags = pmflag_t(_pmflags)
				evs = append(evs, n)
			}
		}
		_prof_pmc(en, evs)
	case ptype&common.PROF_HACK != 0:
		runtime.Setheap(_events << 20)
	case ptype&common.PROF_HACK2 != 0:
		if _events < 0 {
			return int(-common.EINVAL)
		}
		fmt.Printf("GOGC = %v\n", _events)
		debug.SetGCPercent(_events)
	case ptype&common.PROF_HACK3 != 0:
		if _events < 0 {
			return int(-common.EINVAL)
		}
		buf := make([]uint8, _events)
		if buf == nil {
		}
		//fakedur[duri] = buf
		//duri = (duri + 1) % len(fakedur)
		//for i := 0; i < _events/8; i++ {
		//fakeptr = proc
		//}
	case ptype&common.PROF_HACK4 != 0:
		if _events == 0 {
			proc.Closehalf()
		} else {
			fmt.Printf("have %v fds\n", proc.Countino())
		}
	default:
		return int(-common.EINVAL)
	}
	return 0
}

func sys_info(proc *common.Proc_t, n int) int {
	ms := &runtime.MemStats{}
	runtime.ReadMemStats(ms)

	ret := int(-common.EINVAL)
	switch n {
	case common.SINFO_GCCOUNT:
		ret = int(ms.NumGC)
	case common.SINFO_GCPAUSENS:
		ret = int(ms.PauseTotalNs)
	case common.SINFO_GCHEAPSZ:
		ret = int(ms.Alloc)
		fmt.Printf("Total heap size: %v MB (%v MB)\n",
			runtime.Heapsz()/(1<<20), ms.Alloc>>20)
	case common.SINFO_GCMS:
		tot := runtime.GCmarktime() + runtime.GCbgsweeptime()
		ret = tot / 1000000
	case common.SINFO_GCTOTALLOC:
		ret = int(ms.TotalAlloc)
	case common.SINFO_GCMARKT:
		ret = runtime.GCmarktime() / 1000000
	case common.SINFO_GCSWEEPT:
		ret = runtime.GCbgsweeptime() / 1000000
	case common.SINFO_GCWBARRT:
		ret = runtime.GCwbenabledtime() / 1000000
	case common.SINFO_GCOBJS:
		ret = int(ms.HeapObjects)
	case 10:
		runtime.GC()
		ret = 0
		p1, p2 := physmem.Pgcount()
		fmt.Printf("pgcount: %v, %v\n", p1, p2)
	case 11:
		//proc.Vmregion.dump()
		fmt.Printf("proc dump:\n")
		common.Proclock.Lock()
		for i := range common.Allprocs {
			fmt.Printf("   %3v %v\n", common.Allprocs[i].Pid, common.Allprocs[i].Name)
		}
		common.Proclock.Unlock()
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

func segload(proc *common.Proc_t, entry int, hdr *elf_phdr, fops common.Fdops_i) common.Err_t {
	if hdr.vaddr%common.PGSIZE != hdr.fileoff%common.PGSIZE {
		panic("requires copying")
	}
	perms := common.PTE_U
	//PF_X := 1
	PF_W := 2
	if hdr.flags&PF_W != 0 {
		perms |= common.PTE_W
	}

	var did int
	// the bss segment's virtual address may start on the same page as the
	// previous segment. if that is the case, we may not be able to avoid
	// copying.
	// XXX this doesn't seem to happen anymore; why was it ever the case?
	if _, ok := proc.Vmregion.Lookup(uintptr(hdr.vaddr)); ok {
		panic("htf")
		va := hdr.vaddr
		pg, ok := proc.Userdmap8_inner(va, true)
		if !ok {
			panic("must be mapped")
		}
		mmapi, err := fops.Mmapi(hdr.fileoff, 1, false)
		if err != 0 {
			return err
		}
		bsrc := common.Pg2bytes(mmapi[0].Pg)[:]
		bsrc = bsrc[va&int(common.PGOFFSET):]
		if len(pg) > hdr.filesz {
			pg = pg[0:hdr.filesz]
		}
		copy(pg, bsrc)
		did = len(pg)
	}
	filesz := common.Roundup(hdr.vaddr+hdr.filesz-did, common.PGSIZE)
	filesz -= common.Rounddown(hdr.vaddr, common.PGSIZE)
	proc.Vmadd_file(hdr.vaddr+did, filesz, perms, fops, hdr.fileoff+did)
	// eagerly map the page at the entry address
	if entry >= hdr.vaddr && entry < hdr.vaddr+hdr.memsz {
		ent := uintptr(entry)
		vmi, ok := proc.Vmregion.Lookup(ent)
		if !ok {
			panic("just mapped?")
		}
		err := common.Sys_pgfault(proc, vmi, ent, uintptr(common.PTE_U))
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
	if bssva&int(common.PGOFFSET) != 0 {
		bpg, ok := proc.Userdmap8_inner(bssva, true)
		if !ok {
			return -common.ENOMEM
		}
		if bsslen < len(bpg) {
			bpg = bpg[:bsslen]
		}
		copy(bpg, common.Zerobpg[:])
		bssva += len(bpg)
		bsslen = common.Roundup(bsslen-len(bpg), common.PGSIZE)
	}
	// bss may have been completely contained in the copied page.
	if bsslen > 0 {
		proc.Vmadd_anon(bssva, common.Roundup(bsslen, common.PGSIZE), perms)
	}
	return 0
}

// returns user address of read-only TLS, thread 0's TLS image, TLS size, and
// success. caller must hold proc's pagemap lock.
func (e *elf_t) elf_load(proc *common.Proc_t, f *common.Fd_t) (int, int, int, common.Err_t) {
	PT_LOAD := 1
	PT_TLS := 7
	istls := false
	tlssize := 0
	var tlsaddr int
	var tlscopylen int

	gimme := common.Bounds(common.B_ELF_T_ELF_LOAD)
	entry := e.entry()
	// load each elf segment directly into process memory
	for _, hdr := range e.headers() {
		// XXX get rid of worthless user program segments
		if !common.Resadd_noblock(gimme) {
			return 0, 0, 0, -common.ENOHEAP
		}
		if hdr.etype == PT_TLS {
			istls = true
			tlsaddr = hdr.vaddr
			tlssize = common.Roundup(hdr.memsz, 8)
			tlscopylen = hdr.filesz
		} else if hdr.etype == PT_LOAD && hdr.vaddr >= common.USERMIN {
			err := segload(proc, entry, &hdr, f.Fops)
			if err != 0 {
				return 0, 0, 0, err
			}
		}
	}

	freshtls := 0
	t0tls := 0
	if istls {
		// create fresh TLS image and map it COW for thread 0
		l := common.Roundup(tlsaddr+tlssize, common.PGSIZE)
		l -= common.Rounddown(tlsaddr, common.PGSIZE)

		freshtls = proc.Unusedva_inner(0, 2*l)
		t0tls = freshtls + l
		proc.Vmadd_anon(freshtls, l, common.PTE_U)
		proc.Vmadd_anon(t0tls, l, common.PTE_U|common.PTE_W)
		perms := common.PTE_U

		for i := 0; i < l; i += common.PGSIZE {
			// allocator zeros objects, so tbss is already
			// initialized.
			_, p_pg, ok := physmem.Refpg_new()
			if !ok {
				return 0, 0, 0, -common.ENOMEM
			}
			_, ok = proc.Page_insert(freshtls+i, p_pg, perms,
				true)
			if !ok {
				physmem.Refdown(p_pg)
				return 0, 0, 0, -common.ENOMEM
			}
			// map fresh TLS for thread 0
			nperms := perms | common.PTE_COW
			_, ok = proc.Page_insert(t0tls+i, p_pg, nperms, true)
			if !ok {
				physmem.Refdown(p_pg)
				return 0, 0, 0, -common.ENOMEM
			}
		}
		// copy TLS data to freshtls
		tlsvmi, ok := proc.Vmregion.Lookup(uintptr(tlsaddr))
		if !ok {
			panic("must succeed")
		}
		for i := 0; i < tlscopylen; {
			if !common.Resadd_noblock(gimme) {
				return 0, 0, 0, -common.ENOHEAP
			}

			_src, p_pg, err := tlsvmi.Filepage(uintptr(tlsaddr + i))
			if err != 0 {
				return 0, 0, 0, err
			}
			off := (tlsaddr + i) & int(common.PGOFFSET)
			src := common.Pg2bytes(_src)[off:]
			bpg, ok := proc.Userdmap8_inner(freshtls+i, true)
			if !ok {
				physmem.Refdown(p_pg)
				return 0, 0, 0, -common.ENOMEM
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
