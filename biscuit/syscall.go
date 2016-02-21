package main

import "fmt"
import "runtime"
import "runtime/pprof"
import "sort"
import "sync"
import "sync/atomic"
import "time"
import "unsafe"

const(
	TFSIZE		= 24
	TFREGS		= 17
	TF_SYSRSP	= 0
	TF_FSBASE	= 1
	TF_R13		= 4
	TF_R12		= 5
	TF_R8		= 9
	TF_RBP		= 10
	TF_RSI		= 11
	TF_RDI		= 12
	TF_RDX		= 13
	TF_RCX		= 14
	TF_RBX		= 15
	TF_RAX		= 16
	TF_TRAP		= TFREGS
	TF_RIP		= TFREGS + 2
	TF_CS		= TFREGS + 3
	TF_RSP		= TFREGS + 5
	TF_SS		= TFREGS + 6
	TF_RFLAGS	= TFREGS + 4
	TF_FL_IF	= 1 << 9
)

const(
	EPERM		= 1
	ENOENT		= 2
	ESRCH		= 3
	EINTR		= 4
	EIO		= 5
	E2BIG		= 7
	EBADF		= 9
	ECHILD		= 10
	EAGAIN		= 11
	EWOULDBLOCK	= EAGAIN
	EFAULT		= 14
	EBUSY		= 16
	EEXIST		= 17
	ENODEV		= 19
	ENOTDIR		= 20
	EISDIR		= 21
	EINVAL		= 22
	ENOSPC		= 28
	ESPIPE		= 29
	EPIPE		= 32
	ERANGE		= 34
	ENAMETOOLONG	= 36
	ENOSYS		= 38
	ENOTEMPTY	= 39
	ENOTSOCK	= 88
	EMSGSIZE	= 90
	EOPNOTSUPP	= 95
	ECONNRESET	= 104
	EISCONN		= 106
	ENOTCONN	= 107
	ETIMEDOUT	= 110
	ECONNREFUSED	= 111
	EINPROGRESS	= 115
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
  SYS_POLL     = 7
    POLLRDNORM    = 0x1
    POLLRDBAND    = 0x2
    POLLIN        = (POLLRDNORM | POLLRDBAND)
    POLLPRI       = 0x4
    POLLWRNORM    = 0x8
    POLLOUT       = POLLWRNORM
    POLLWRBAND    = 0x10
    POLLERR       = 0x20
    POLLHUP       = 0x40
    POLLNVAL      = 0x80
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
  SYS_SIGACT   = 13
  SYS_ACCESS   = 21
  SYS_DUP2     = 33
  SYS_PAUSE    = 34
  SYS_GETPID   = 39
  SYS_SOCKET   = 41
    // domains
    AF_UNIX       = 1
    // types
    SOCK_STREAM   = 1 << 0
    SOCK_DGRAM    = 1 << 1
    SOCK_RAW      = 1 << 2
    SOCK_SEQPACKET= 1 << 3
    SOCK_CLOEXEC  = 1 << 4
    SOCK_NONBLOCK = 1 << 5
  SYS_CONNECT  = 42
  SYS_ACCEPT   = 43
  SYS_SENDTO   = 44
  SYS_RECVFROM = 45
  SYS_BIND     = 49
  SYS_LISTEN   = 50
  SYS_GETSOCKOPT = 55
    // socket levels
    SOL_SOCKET     = 1
    // socket options
    SO_SNDBUF      = 1
    SO_SNDTIMEO    = 2
    SO_ERROR       = 3
  SYS_FORK     = 57
    FORK_PROCESS = 0x1
    FORK_THREAD  = 0x2
  SYS_EXECV    = 59
  SYS_EXIT     = 60
    CONTINUED    = 1 << 9
    EXITED       = 1 << 10
    SIGNALED     = 1 << 11
    SIGSHIFT     = 27
  SYS_WAIT4    = 61
    WAIT_ANY     = -1
    WAIT_MYPGRP  = 0
    WCONTINUED   = 1
    WNOHANG      = 2
    WUNTRACED    = 4
  SYS_KILL     = 62
  SYS_FCNTL    = 72
    F_GETFL      = 1
    F_SETFL      = 2
    F_GETFD      = 3
    F_SETFD      = 4
  SYS_TRUNC    = 76
  SYS_FTRUNC   = 77
  SYS_GETCWD   = 79
  SYS_CHDIR    = 80
  SYS_RENAME   = 82
  SYS_MKDIR    = 83
  SYS_LINK     = 86
  SYS_UNLINK   = 87
  SYS_GETTOD   = 96
  SYS_GETRLMT  = 97
    RLIMIT_NOFILE = 1
    RLIM_INFINITY = ^uint(0)
  SYS_GETRUSG  = 98
    RUSAGE_SELF      = 1
    RUSAGE_CHILDREN  = 2
  SYS_MKNOD    = 133
  SYS_SETRLMT  = 160
  SYS_SYNC     = 162
  SYS_REBOOT   = 169
  SYS_NANOSLEEP= 230
  SYS_PIPE2    = 293
  SYS_FAKE     = 31337
  SYS_THREXIT  = 31338
  SYS_FAKE2    = 31339
  SYS_PREAD    = 31340
  SYS_PWRITE   = 31341
  SYS_FUTEX    = 31342
    FUTEX_SLEEP    = 1
    FUTEX_WAKE     = 2
    FUTEX_CNDGIVE  = 3
    _FUTEX_LAST = FUTEX_CNDGIVE
    // futex internal op
    _FUTEX_CNDTAKE  = 4
)

const(
  SIGKILL = 9
)

// lowest userspace address
const USERMIN	int = VUSER << 39

func syscall(p *proc_t, tid tid_t, tf *[TFSIZE]int) int {

	p.threadi.Lock()
	talive, ok := p.threadi.alive[tid]
	if !ok {
		panic("bad thread")
	}
	if !talive {
		panic("thread not alive")
	}
	p.threadi.Unlock()

	if p.doomed {
		// this process has been killed
		reap_doomed(p, tid)
		return 0
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
	case SYS_POLL:
		ret = sys_poll(p, a1, a2, a3)
	case SYS_LSEEK:
		ret = sys_lseek(p, a1, a2, a3)
	case SYS_MMAP:
		ret = sys_mmap(p, a1, a2, a3, a4, a5)
	case SYS_MUNMAP:
		ret = sys_munmap(p, a1, a2)
	case SYS_SIGACT:
		ret = sys_sigaction(p, a1, a2, a3)
	case SYS_ACCESS:
		ret = sys_access(p, a1, a2)
	case SYS_DUP2:
		ret = sys_dup2(p, a1, a2)
	case SYS_PAUSE:
		ret = sys_pause(p)
	case SYS_GETPID:
		ret = sys_getpid(p, tid)
	case SYS_SOCKET:
		ret = sys_socket(p, a1, a2, a3)
	case SYS_CONNECT:
		ret = sys_connect(p, a1, a2, a3)
	case SYS_ACCEPT:
		ret = sys_accept(p, a1, a2, a3)
	case SYS_SENDTO:
		ret = sys_sendto(p, a1, a2, a3, a4, a5)
	case SYS_RECVFROM:
		ret = sys_recvfrom(p, a1, a2, a3, a4, a5)
	case SYS_BIND:
		ret = sys_bind(p, a1, a2, a3)
	case SYS_LISTEN:
		ret = sys_listen(p, a1, a2)
	case SYS_GETSOCKOPT:
		ret = sys_getsockopt(p, a1, a2, a3, a4, a5)
	case SYS_FORK:
		ret = sys_fork(p, tf, a1, a2)
	case SYS_EXECV:
		ret = sys_execv(p, tf, a1, a2)
	case SYS_EXIT:
		status := a1 & 0xff
		status |= EXITED
		sys_exit(p, tid, status)
	case SYS_WAIT4:
		ret = sys_wait4(p, tid, a1, a2, a3, a4, a5)
	case SYS_KILL:
		ret = sys_kill(p, a1, a2)
	case SYS_FCNTL:
		ret = sys_fcntl(p, a1, a2, a3)
	case SYS_TRUNC:
		ret = sys_truncate(p, a1, uint(a2))
	case SYS_FTRUNC:
		ret = sys_ftruncate(p, a1, uint(a2))
	case SYS_GETCWD:
		ret = sys_getcwd(p, a1, a2)
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
	case SYS_GETRLMT:
		ret = sys_getrlimit(p, a1, a2)
	case SYS_GETRUSG:
		ret = sys_getrusage(p, a1, a2)
	case SYS_MKNOD:
		ret = sys_mknod(p, a1, a2, a3)
	case SYS_SETRLMT:
		ret = sys_setrlimit(p, a1, a2)
	case SYS_SYNC:
		ret = sys_sync(p)
	case SYS_REBOOT:
		ret = sys_reboot(p)
	case SYS_NANOSLEEP:
		ret = sys_nanosleep(p, a1, a2)
	case SYS_PIPE2:
		ret = sys_pipe2(p, a1, a2)
	case SYS_FAKE:
		ret = sys_fake(p, a1)
	case SYS_FAKE2:
		ret = sys_fake2(p, a1)
	case SYS_THREXIT:
		sys_threxit(p, tid, a1)
	case SYS_PREAD:
		ret = sys_pread(p, a1, a2, a3, a4)
	case SYS_PWRITE:
		ret = sys_pwrite(p, a1, a2, a3, a4)
	case SYS_FUTEX:
		ret = sys_futex(p, a1, a2, a3, a4, a5)
	default:
		fmt.Printf("unexpected syscall %v\n", sysno)
		sys_exit(p, tid, SIGNALED | mkexitsig(31))
	}
	return ret
}

func reap_doomed(p *proc_t, tid tid_t) {
	if !p.doomed {
		panic("p not doomed")
	}
	p.thread_dead(tid, 0, false)
}

func cons_read(ub *userbuf_t, offset int) (int, int) {
	sz := ub.len
	kdata := kbd_get(sz)
	ret, err := ub.write(kdata)
	if err != 0 || ret != len(kdata) {
		panic("dropped keys")
	}
	return ret, 0
}

func cons_write(src *userbuf_t, off int) (int, int) {
		// merge into one buffer to avoid taking the console
		// lock many times.
		utext := int8(0x17)
		big := make([]uint8, src.len)
		read, err := src.read(big)
		if err != 0 {
			return 0, err
		}
		if read != src.len {
			panic("short read")
		}
		runtime.Pmsga(&big[0], len(big), utext)
		return len(big), 0
}

func sys_read(proc *proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	if fd.perms & FD_READ == 0 {
		return -EPERM
	}

	userbuf := proc.mkuserbuf(bufp, sz)

	ret, err := fd.fops.read(userbuf)
	if err != 0 {
		return err
	}
	return ret
}

func sys_write(proc *proc_t, fdn int, bufp int, sz int) int {
	if sz == 0 {
		return 0
	}
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	if fd.perms & FD_WRITE == 0 {
		return -EPERM
	}

	userbuf := proc.mkuserbuf(bufp, sz)

	ret, err := fd.fops.write(userbuf)
	if err != 0 {
		return err
	}
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
	pi := proc.cwd.fops.pathi()
	file, err := fs_open(path, flags, mode, pi, 0, 0)
	if err != 0 {
		return err
	}
	if flags & O_CLOEXEC != 0 {
		fdperms |= FD_CLOEXEC
	}
	fdn := proc.fd_insert(file, fdperms)
	return fdn
}

func sys_pause(proc *proc_t) int {
	// no signals yet!
	var c chan bool
	<- c
	return -1
}

func sys_close(proc *proc_t, fdn int) int {
	fd, ok := proc.fd_del(fdn)
	if !ok {
		return -EBADF
	}
	ret := fd.fops.close()
	return ret
}

// a type to hold the virtual/physical addresses of memory mapped files
type mmapinfo_t struct {
	pg	*[512]int
	phys	int
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

	proc.Lock_pmap()

	perms := PTE_U
	if prot & PROT_WRITE != 0 {
		perms |= PTE_W
	}
	lenn = roundup(lenn, PGSIZE)
	if lenn/PGSIZE + proc.vmregion.pglen() > proc.ulim.pages {
		proc.Unlock_pmap()
		return MAP_FAILED
	}
	addr := proc.unusedva_inner(proc.mmapi, lenn)
	seg := proc.mkvmseg(addr, lenn)
	proc.mmapi = addr + lenn
	for i := 0; i < lenn; i += PGSIZE {
		pg, p_pg := pg_new()
		proc.page_insert(addr + i, seg, pg, p_pg, perms, true)
	}
	// no tlbshoot because mmap never replaces pages for now
	proc.Unlock_pmap()
	return addr
}

func sys_munmap(proc *proc_t, addrn, len int) int {
	if addrn & PGOFFSET != 0 || addrn < USERMIN {
		return -EINVAL
	}
	proc.Lock_pmap()
	defer proc.Unlock_pmap()
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
	pgs := len/PGSIZE
	proc.vmregion.remove(addrn, len)
	proc.tlbshoot(addrn, pgs)
	return 0
}

func sys_sigaction(proc *proc_t, sig, actn, oactn int) int {
	panic("no imp")
}

func sys_access(proc *proc_t, pathn, mode int) int {
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	if mode == 0 {
		return -EINVAL
	}

	pi := proc.cwd.fops.pathi()
	fsf, err := _fs_open(path, O_RDONLY, 0, pi, 0, 0)
	if err != 0 {
		return err
	}

	// XXX no permissions yet
	//R_OK := 1 << 0
	//W_OK := 1 << 1
	//X_OK := 1 << 2
	ret := 0

	if fs_close(fsf.priv) != 0 {
		panic("must succeed")
	}
	return ret
}

func copyfd(fd *fd_t) (*fd_t, int) {
	nfd := &fd_t{}
	*nfd = *fd
	err := nfd.fops.reopen()
	if err != 0 {
		return nil, err
	}
	return nfd, 0
}

func close_panic(f *fd_t) {
	if f.fops.close() != 0 {
		panic("must succeed")
	}
}

func sys_dup2(proc *proc_t, oldn, newn int) int{
	if oldn == newn {
		return newn
	}

	ofd, ok := proc.fd_get(oldn)
	if !ok {
		return -EBADF
	}
	nfd, err := copyfd(ofd)
	if err != 0 {
		return err
	}
	nfd.perms &^= FD_CLOEXEC

	// lock fd table to prevent racing on the same fd number
	proc.fdl.Lock()
	cfd := proc.fds[newn]
	needclose := cfd != nil
	proc.fds[newn] = nfd
	proc.fdl.Unlock()

	if needclose {
		close_panic(cfd)
	}
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
	err := fs_stat(path, buf, proc.cwd.fops.pathi())
	if err != 0 {
		return err
	}
	ok = proc.k2user(buf.data, statn)
	if !ok {
		return -EFAULT
	}
	return 0
}

func sys_fstat(proc *proc_t, fdn int, statn int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	buf := &stat_t{}
	buf.init()
	err := fd.fops.fstat(buf)
	if err != 0 {
		return err
	}

	ok = proc.k2user(buf.data, statn)
	if !ok {
		return -EFAULT
	}
	return 0
}

// converts internal states to poll states
// pokes poll status bits into user memory. since we only use one priority
// internally, mask away any POLL bits the user didn't not request.
func _ready2rev(orig int, r ready_t) int {
	inmask  := POLLIN | POLLPRI
	outmask := POLLOUT | POLLWRBAND
	pbits := 0
	if r & R_READ != 0 {
		pbits |= inmask
	}
	if r & R_WRITE != 0 {
		pbits |= outmask
	}
	if r & R_HUP != 0 {
		pbits |= POLLHUP
	}
	if r & R_ERROR != 0 {
		pbits |= POLLERR
	}
	wantevents := ((orig >> 32) & 0xffff) | POLLNVAL | POLLERR | POLLHUP
	revents := wantevents & pbits
	return orig | (revents << 48)
}

func _checkfds(proc *proc_t, pm *pollmsg_t, wait bool, buf []uint8, nfds int) (int, bool) {
	inmask  := POLLIN | POLLPRI
	outmask := POLLOUT | POLLWRBAND
	readyfds := 0
	writeback := false
	//proc.fdl.Lock()
	for i := 0; i < nfds; i++ {
		off := i*8
		uw := readn(buf, 8, off)
		fdn := int(uint32(uw))
		// fds < 0 are to be ignored
		if fdn < 0 {
			continue
		}
		//fd, ok := proc.fd_get_inner(fdn)
		fd, ok := proc.fd_get(fdn)
		if !ok {
			uw |= POLLNVAL
			writen(buf, 8, off, uw)
			writeback = true
			continue
		}
		var pev ready_t
		events := int((uint(uw) >> 32) & 0xffff)
		// one priority
		if events & inmask != 0 {
			pev |= R_READ
		}
		if events & outmask != 0 {
			pev |= R_WRITE
		}
		if events & POLLHUP != 0 {
			pev |= R_HUP
		}
		// poll unconditionally reports ERR, HUP, and NVAL
		pev |= R_ERROR | R_HUP
		pm.pm_set(pev, wait)
		devstatus := fd.fops.pollone(*pm)
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
	//proc.fdl.Unlock()
	return readyfds, writeback
}

func sys_poll(proc *proc_t, fdsn, nfds, timeout int) int {
	if nfds < 0  || timeout < -1 {
		return -EINVAL
	}

	// copy pollfds from userspace to avoid reading/writing overhead
	// (locking pmap and looking up uva mapping).
	pollfdsz := 8
	sz := pollfdsz*nfds
	buf := make([]uint8, sz)
	if !proc.user2k(buf, fdsn) {
		return -EFAULT
	}

	// first we tell the underlying device to notify us if their fd is
	// ready. if a device is immediately ready, we don't both to register
	// notifiers with the rest of the devices -- we just ask their status
	// too.
	devwait := timeout != 0
	pm := pollmsg_t{}
	readyfds, writeback := _checkfds(proc, &pm, devwait, buf, nfds)

	if writeback && !proc.k2user(buf, fdsn) {
		return -EFAULT
	}

	// if we found a ready fd, we are done
	if readyfds != 0 || !devwait {
		return readyfds
	}

	// otherwise, wait for a notification
	timedout, err := pm.pm_wait(timeout)
	if err != 0 {
		panic("must succeed")
	}
	if timedout {
		pm.pnote.prune = true
		return 0
	}
	// check the fds one more time, update ready status
	readyfds, writeback = _checkfds(proc, &pm, false, buf, nfds)
	if writeback && !proc.k2user(buf, fdsn) {
		return -EFAULT
	}
	if readyfds < 1 {
		panic("wokeup without ready fd?")
	}
	return readyfds
}

func sys_lseek(proc *proc_t, fdn, off, whence int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}

	ret := fd.fops.lseek(off, whence)
	return ret
}

func sys_pipe2(proc *proc_t, pipen, flags int) int {
	rfp := FD_READ
	wfp := FD_WRITE

	var opts int
	if flags & O_NONBLOCK != 0 {
		opts |= O_NONBLOCK
	}

	if flags & O_CLOEXEC != 0 {
		rfp |= FD_CLOEXEC
		wfp |= FD_CLOEXEC
	}

	p := &pipe_t{}
	p.pipe_start()
	rops := &pipefops_t{pipe: p, writer: false, options: opts}
	wops := &pipefops_t{pipe: p, writer: true, options: opts}
	rpipe := &fd_t{fops: rops}
	wpipe := &fd_t{fops: wops}
	rfd := proc.fd_insert(rpipe, rfp)
	wfd := proc.fd_insert(wpipe, wfp)

	ok1 := proc.userwriten(pipen, 4, rfd)
	ok2 := proc.userwriten(pipen + 4, 4, wfd)
	if !ok1 || !ok2 {
		err1 := sys_close(proc, rfd)
		err2 := sys_close(proc, wfd)
		if err1 != 0 || err2 != 0 {
			panic("must succeed")
		}
		return -EFAULT
	}
	return 0
}

type ready_t uint
const(
	R_READ 	ready_t	= 1 << iota
	R_WRITE	ready_t	= 1 << iota
	R_ERROR	ready_t	= 1 << iota
	R_HUP	ready_t	= 1 << iota
)

type pollnote_t struct {
	notif	chan bool
	prune	bool
}

// used by thread executing poll(2).
type pollmsg_t struct {
	pnote	*pollnote_t
	events	ready_t
	dowait	bool
}

func (pm *pollmsg_t) pm_set(events ready_t, dowait bool) {
	if pm.pnote == nil {
		pm.pnote = &pollnote_t{}
		// 1-element buffered channel; that way devices can send
		// notifies on the channel asynchronously without blocking.
		pm.pnote.notif = make(chan bool, 1)
	}
	pm.events = events
	pm.dowait = dowait
}

// returns whether we timed out, and error
func (pm *pollmsg_t) pm_wait(to int) (bool, int) {
	var tochan <-chan time.Time
	if to != -1 {
		tochan = time.After(time.Duration(to)*time.Millisecond)
	}
	var timeout bool
	select {
	case <- pm.pnote.notif:
	case <- tochan:
		timeout = true
	}
	return timeout, 0
}

// keeps track of all outstanding pollers. used by devices supporting poll(2)
type pollers_t struct {
	allmask		ready_t
	_waiters	[]pollmsg_t
	waiters		[]pollmsg_t
}

func (p *pollers_t) _rm(idx int) {
	if len(p.waiters) == 1 {
		p.waiters = p._waiters
		return
	}
	copy(p.waiters[idx:], p.waiters[idx+1:])
	l := len(p.waiters)
	p.waiters = p.waiters[:l-1]
}

func (p *pollers_t) addpoller(pm *pollmsg_t) {
	if p.waiters == nil {
		p._waiters = make([]pollmsg_t, 0, 10)
		p.waiters = p._waiters
	}
	p.waiters = append(p.waiters, *pm)
	p.allmask |= pm.events
}

func (p *pollers_t) wakeready(r ready_t) {
	if p.allmask & r == 0 {
		return
	}
	var newallmask ready_t
	for i := 0; i < len(p.waiters); i++ {
		rmed := false
		pm := p.waiters[i]
		// prune useless waiters
		if pm.pnote.prune {
			p._rm(i)
			i--
			rmed = true
		} else if pm.events & r != 0 {
			// found a waiter
			pm.events &= r
			// non-blocking send on a 1-element buffered channel
			select {
			case pm.pnote.notif <- true:
				pm.pnote.prune = true
			default:
			}
			p._rm(i)
			i--
			rmed = true
		}
		if !rmed {
			newallmask |= pm.events
		}
	}
	p.allmask = newallmask
}

type pipe_t struct {
	sync.Mutex
	cbuf	circbuf_t
	rcond	*sync.Cond
	wcond	*sync.Cond
	readers	int
	writers int
	closed	bool
	pollers	pollers_t
}

func (o *pipe_t) pipe_start() {
	pipesz := 512
	o.cbuf.cb_init(pipesz)
	o.readers, o.writers = 1, 1
	o.rcond = sync.NewCond(o)
	o.wcond = sync.NewCond(o)
}

func (o *pipe_t) op_write(src *userbuf_t, noblock bool) (int, int) {
	o.Lock()
	for {
		if o.closed {
			o.Unlock()
			return 0, -EBADF
		}
		if o.readers == 0 {
			o.Unlock()
			return 0, -EPIPE
		}
		if !o.cbuf.full() {
			break
		}
		if noblock {
			o.Unlock()
			return 0, -EWOULDBLOCK
		}
		o.wcond.Wait()
	}
	ret, err := o.cbuf.copyin(src)
	if err != 0 {
		o.Unlock()
		return 0, err
	}
	o.rcond.Signal()
	o.pollers.wakeready(R_READ)
	o.Unlock()

	return ret, 0
}

func (o *pipe_t) op_read(dst *userbuf_t, noblock bool) (int, int) {
	o.Lock()
	for {
		if o.closed {
			o.Unlock()
			return 0, -EBADF
		}
		if o.writers == 0 || !o.cbuf.empty() {
			break
		}
		if noblock {
			o.Unlock()
			return 0, -EWOULDBLOCK
		}
		o.rcond.Wait()
	}
	ret, err := o.cbuf.copyout(dst)
	if err != 0 {
		o.Unlock()
		return 0, err
	}
	o.wcond.Signal()
	o.pollers.wakeready(R_WRITE)
	o.Unlock()

	return ret, 0
}

func (o *pipe_t) op_poll(pm pollmsg_t) ready_t {
	o.Lock()

	if o.closed {
		o.Unlock()
		return 0
	}

	var r ready_t
	readable := false
	if !o.cbuf.empty() || o.writers == 0 {
		readable = true
	}
	writeable := false
	if !o.cbuf.full() || o.readers == 0 {
		writeable = true
	}
	if pm.events & R_READ != 0 && readable {
		r |= R_READ
	}
	if pm.events & R_HUP != 0 && o.writers == 0 {
		r |= R_HUP
	} else if pm.events & R_WRITE != 0 && writeable {
		r |= R_WRITE
	}
	if r != 0 {
		o.Unlock()
		return r
	}
	o.pollers.addpoller(&pm)
	o.Unlock()
	return 0
}

func (o *pipe_t) op_reopen(rd, wd int) int {
	o.Lock()
	if o.closed {
		o.Unlock()
		return -EBADF
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
	}
	o.Unlock()
	return 0
}

type pipefops_t struct {
	pipe	*pipe_t
	options	int
	writer	bool
}

func (of *pipefops_t) close() int {
	var ret int
	if of.writer {
		ret = of.pipe.op_reopen(0, -1)
	} else {
		ret = of.pipe.op_reopen(-1, 0)
	}
	return ret
}

func (of *pipefops_t) fstat(*stat_t) int {
	panic("fstat on pipe")
}

func (of *pipefops_t) lseek(int, int) int {
	return -ESPIPE
}

func (of *pipefops_t) mmapi(int) ([]mmapinfo_t, int) {
	return nil, -EINVAL
}

func (of *pipefops_t) pathi() inum {
	panic("pipe cwd")
}

func (of *pipefops_t) read(dst *userbuf_t) (int, int) {
	noblk := of.options & O_NONBLOCK != 0
	return of.pipe.op_read(dst, noblk)
}

func (of *pipefops_t) reopen() int {
	var ret int
	if of.writer {
		ret = of.pipe.op_reopen(0, 1)
	} else {
		ret = of.pipe.op_reopen(1, 0)
	}
	return ret
}

func (of *pipefops_t) write(src *userbuf_t) (int, int) {
	noblk := of.options & O_NONBLOCK != 0
	c := 0
	for c != src.len {
		ret, err := of.pipe.op_write(src, noblk)
		if noblk || err != 0 {
			return ret, err
		}
		c += ret
	}
	return c, 0
}

func (of *pipefops_t) fullpath() (string, int) {
	panic("weird cwd")
}

func (of *pipefops_t) truncate(uint) int {
	return -EINVAL
}

func (of *pipefops_t) pread(*userbuf_t, int) (int, int) {
	return 0, -ESPIPE
}

func (of *pipefops_t) pwrite(*userbuf_t, int) (int, int) {
	return 0, -ESPIPE
}

func (of *pipefops_t) accept(*proc_t, *userbuf_t) (fdops_i, int, int) {
	return nil, 0, -ENOTSOCK
}

func (of *pipefops_t) bind(*proc_t, []uint8) int {
	return -ENOTSOCK
}

func (of *pipefops_t) connect(*proc_t, []uint8) int {
	return -ENOTSOCK
}

func (of *pipefops_t) listen(*proc_t, int) (fdops_i, int) {
	return nil, -ENOTSOCK
}

func (of *pipefops_t) sendto(*proc_t, *userbuf_t, []uint8, int) (int, int) {
	return 0, -ENOTSOCK
}

func (of *pipefops_t) recvfrom(*proc_t, *userbuf_t, *userbuf_t) (int, int, int) {
	return 0, 0, -ENOTSOCK
}

func (of *pipefops_t) pollone(pm pollmsg_t) ready_t {
	if of.writer {
		pm.events &^= R_READ
	} else {
		pm.events &^= R_WRITE
	}
	return of.pipe.op_poll(pm)
}

func (of *pipefops_t) fcntl(proc *proc_t, cmd, opt int) int {
	switch cmd {
	case F_GETFL:
		return of.options
	case F_SETFL:
		of.options = opt
		return 0
	default:
		panic("weird cmd")
	}
}

func (of *pipefops_t) getsockopt(*proc_t, int, *userbuf_t, int) (int, int) {
	return 0, -ENOTSOCK
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
	ret := fs_rename(old, new, proc.cwd.fops.pathi())
	return ret
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
	ret := fs_mkdir(path, mode, proc.cwd.fops.pathi())
	return ret
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
	ret := fs_link(old, new, proc.cwd.fops.pathi())
	return ret
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
	ret := fs_unlink(path, proc.cwd.fops.pathi())
	return ret
}

func sys_gettimeofday(proc *proc_t, timevaln int) int {
	tvalsz := 16
	now := time.Now()
	buf := make([]uint8, tvalsz)
	us := int(now.UnixNano() / 1000)
	writen(buf, 8, 0, us/1e6)
	writen(buf, 8, 8, us%1e6)
	if !proc.k2user(buf, timevaln) {
		return -EFAULT
	}
	return 0
}

var _rlimits = map[int]uint{RLIMIT_NOFILE: RLIM_INFINITY}

func sys_getrlimit(proc *proc_t, resn, rlpn int) int {
	var cur uint
	switch resn {
	case RLIMIT_NOFILE:
		cur = proc.ulim.nofile
	default:
		return -EINVAL
	}
	max := _rlimits[resn]
	ok1 := proc.userwriten(rlpn, 8, int(cur))
	ok2 := proc.userwriten(rlpn + 8, 8, int(max))
	if !ok1 || !ok2 {
		return -EFAULT
	}
	return 0
}

func sys_setrlimit(proc *proc_t, resn, rlpn int) int {
	// XXX root can raise max
	_ncur, ok := proc.userreadn(rlpn, 8)
	if !ok {
		return -EFAULT
	}
	ncur := uint(_ncur)
	if ncur > _rlimits[resn] {
		return -EINVAL
	}
	switch resn {
	case RLIMIT_NOFILE:
		proc.ulim.nofile = ncur
	default:
		return -EINVAL
	}
	return 0
}

func sys_getrusage(proc *proc_t, who, rusagep int) int {
	var ru []uint8
	if who == RUSAGE_SELF {
		// user time is gathered at thread termination... report user
		// time as best as we can
		tmp := proc.atime

		proc.threadi.Lock()
		for tid := range proc.threadi.alive {
			//val := runtime.Proctime(proc.mkptid(tid))
			if tid == 0 {
			}
			val := 42
			// tid may not exist if the query for the time races
			// with a thread exiting.
			if val > 0 {
				tmp.userns += int64(val)
			}
		}
		proc.threadi.Unlock()

		ru = tmp.to_rusage()
	} else if who == RUSAGE_CHILDREN {
		ru = proc.catime.fetch()
	} else {
		return -EINVAL
	}
	if !proc.k2user(ru, rusagep) {
		return -EFAULT
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
	fsf, err := _fs_open(path, O_CREAT, 0, proc.cwd.fops.pathi(), maj, min)
	if err != 0 {
		return err
	}
	if fs_close(fsf.priv) != 0 {
		panic("must succeed")
	}
	return 0
}

func sys_sync(proc *proc_t) int {
	return fs_sync()
}

func sys_reboot(proc *proc_t) int {
	// who needs ACPI?
	runtime.Lcr3(p_zeropg)
	// poof
	fmt.Printf("what?\n")
	return 0
}

func sys_nanosleep(proc *proc_t, sleeptsn, remaintsn int) int {
	tot, _, err := proc.usertimespec(sleeptsn)
	if err != 0 {
		return err
	}
	<- time.After(tot)

	return 0
}

func sys_getpid(proc *proc_t, tid tid_t) int {
	return proc.pid
}

func sys_socket(proc *proc_t, domain, typ, proto int) int {
	var opts int
	if typ & SOCK_NONBLOCK != 0 {
		opts |= O_NONBLOCK
	}
	var clop int
	if typ & SOCK_CLOEXEC != 0 {
		clop = FD_CLOEXEC
	}

	var sfops fdops_i
	switch {
	case domain == AF_UNIX && typ & SOCK_DGRAM != 0:
		sfops = &sudfops_t{open: 1}
	case domain == AF_UNIX && typ & SOCK_STREAM != 0:
		sfops = &susfops_t{options: opts}
	default:
		return -EINVAL
	}
	file := &fd_t{}
	file.fops = sfops
	fdn := proc.fd_insert(file, FD_READ | FD_WRITE | clop)
	return fdn
}

func sys_connect(proc *proc_t, fdn, sockaddrn, socklen int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}

	// copy sockaddr to kernel space to avoid races
	sabuf, err := copysockaddr(proc, sockaddrn, socklen)
	if err != 0 {
		return err
	}
	err = fd.fops.connect(proc, sabuf)
	return err
}

func sys_accept(proc *proc_t, fdn, sockaddrn, socklenn int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	var sl int
	if socklenn != 0 {
		l, ok := proc.userreadn(socklenn, 8)
		if !ok {
			return -EFAULT
		}
		if l < 0 {
			return -EFAULT
		}
		sl = l
	}
	fromsa := proc.mkuserbuf(sockaddrn, sl)
	newfops, fromlen, err := fd.fops.accept(proc, fromsa)
	if err != 0 {
		return err
	}
	if fromlen != 0 {
		if !proc.userwriten(socklenn, 8, fromlen) {
			return -EFAULT
		}
	}
	newfd := &fd_t{fops: newfops}
	ret := proc.fd_insert(newfd, FD_READ | FD_WRITE)
	return ret
}

func copysockaddr(proc *proc_t, san, sl int) ([]uint8, int) {
	if sl == 0 {
		return nil, 0
	}
	if sl < 0 {
		return nil, -EFAULT
	}
	maxsl := 256
	if sl >= maxsl {
		return nil, -ENOTSOCK
	}
	ub := proc.mkuserbuf(san, sl)
	sabuf := make([]uint8, sl)
	_, err := ub.read(sabuf)
	if err != 0 {
		return nil, err
	}
	return sabuf, 0
}

func sys_sendto(proc *proc_t, fdn, bufn, flaglen, sockaddrn, socklen int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	flags := int(uint(uint32(flaglen)))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	if buflen < 0 {
		return -EFAULT
	}

	// copy sockaddr to kernel space to avoid races
	sabuf, err := copysockaddr(proc, sockaddrn, socklen)
	if err != 0 {
		return err
	}

	buf := proc.mkuserbuf(bufn, buflen)
	ret, err := fd.fops.sendto(proc, buf, sabuf, flags)
	if err != 0 {
		return err
	}
	return ret
}

func sys_recvfrom(proc *proc_t, fdn, bufn, flaglen, sockaddrn,
    socklenn int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	flags := uint(uint32(flaglen))
	if flags != 0 {
		panic("no imp")
	}
	buflen := int(uint(flaglen) >> 32)
	buf := proc.mkuserbuf(bufn, buflen)

	// is the from address requested?
	var salen int
	if socklenn != 0 {
		l, ok := proc.userreadn(socklenn, 8)
		if !ok {
			return -EFAULT
		}
		salen = l
		if salen < 0 {
			return -EFAULT
		}
	}
	fromsa := proc.mkuserbuf(sockaddrn, salen)
	ret, addrlen, err := fd.fops.recvfrom(proc, buf, fromsa)
	if err != 0 {
		return err
	}
	// write new socket size to user space
	if addrlen > 0 {
		if !proc.userwriten(socklenn, 8, addrlen) {
			return -EFAULT
		}
	}
	return ret
}

func sys_bind(proc *proc_t, fdn, sockaddrn, socklen int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}

	sabuf, err := copysockaddr(proc, sockaddrn, socklen)
	if err != 0 {
		return err
	}

	return fd.fops.bind(proc, sabuf)
}

type sudfops_t struct {
	sunaddr	sunaddr_t
	open	int
	bound	bool
	// XXX use new method
	// to protect the "open" field from racing close()s
	sync.Mutex
}

func (sf *sudfops_t) close() int {
	// XXX use new method
	sf.Lock()
	sf.open--
	if sf.open < 0 {
		panic("negative ref count")
	}
	termsund := sf.open == 0
	sf.Unlock()

	if termsund && sf.bound {
		allsunds.Lock()
		delete(allsunds.m, sf.sunaddr.id)
		sf.sunaddr.sund.finish <- true
		allsunds.Unlock()
	}
	return 0
}

func (sf *sudfops_t) fstat(s *stat_t) int {
	panic("no imp")
}

func (sf *sudfops_t) mmapi(offset int) ([]mmapinfo_t, int) {
	return nil, -ENODEV
}

func (sf *sudfops_t) pathi() inum {
	panic("cwd socket?")
}

func (sf *sudfops_t) read(dst *userbuf_t) (int, int) {
	return 0, -ENODEV
}

func (sf *sudfops_t) reopen() int {
	sf.Lock()
	sf.open++
	sf.Unlock()
	return 0
}

func (sf *sudfops_t) write(*userbuf_t) (int, int) {
	return 0, -ENODEV
}

func (sf *sudfops_t) fullpath() (string, int) {
	panic("weird cwd")
}

func (sf *sudfops_t) truncate(newlen uint) int {
	return -EINVAL
}

func (sf *sudfops_t) pread(dst *userbuf_t, offset int) (int, int) {
	return 0, -ESPIPE
}

func (sf *sudfops_t) pwrite(src *userbuf_t, offset int) (int, int) {
	return 0, -ESPIPE
}

func (sf *sudfops_t) lseek(int, int) int {
	return -ESPIPE
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

func (sf *sudfops_t) accept(*proc_t, *userbuf_t) (fdops_i, int, int) {
	return nil, 0, -EINVAL
}

func (sf *sudfops_t) bind(proc *proc_t, sa []uint8) int {
	sf.Lock()
	defer sf.Unlock()

	if sf.bound {
		return -EINVAL
	}

	poff := 2
	path := slicetostr(sa[poff:])
	// try to create the specified file as a special device
	sid := sun_new()
	pi := proc.cwd.fops.pathi()
	fsf, err := _fs_open(path, O_CREAT | O_EXCL, 0, pi, D_SUD, int(sid))
	if err != 0 {
		return err
	}
	if fs_close(fsf.priv) != 0 {
		panic("must succeed")
	}
	sf.sunaddr.id = sid
	sf.sunaddr.path = path
	sf.sunaddr.sund = sun_start(sid)
	sf.bound = true
	return 0
}

func (sf *sudfops_t) connect(proc *proc_t, sabuf []uint8) int {
	return -EINVAL
}

func (sf *sudfops_t) listen(proc *proc_t, backlog int) (fdops_i, int) {
	return nil, -EINVAL
}

func (sf *sudfops_t) sendto(proc *proc_t, src *userbuf_t, sa []uint8,
    flags int) (int, int) {
	poff := 2
	if len(sa) <= poff {
		return 0, -EINVAL
	}
	st := &stat_t{}
	st.init()
	path := slicetostr(sa[poff:])
	err := fs_stat(path, st, proc.cwd.fops.pathi())
	if err != 0 {
		return 0, err
	}
	maj, min := unmkdev(st.rdev())
	if maj != D_SUD {
		return 0, -ECONNREFUSED
	}

	sunid := sunid_t(min)
	// XXX use new way
	// lookup sid, get admission to write. we must get admission in order
	// to ensure that after the socket daemon terminates 1) no writers are
	// left blocking indefinitely and 2) that no new writers attempt to
	// write to a channel that no one is listening on
	allsunds.Lock()
	sund, ok := allsunds.m[sunid]
	if ok {
		sund.adm <- true
	}
	allsunds.Unlock()
	if !ok {
		return 0, -ECONNREFUSED
	}

	// XXX pass userbuf directly to sund
	data := make([]uint8, src.len)
	_, err = src.read(data)
	if err != 0 {
		return 0, err
	}

	sbuf := sunbuf_t{from: sf.sunaddr, data: data}
	sund.in <- sbuf
	return <- sund.inret, 0
}

func (sf *sudfops_t) recvfrom(proc *proc_t, dst *userbuf_t,
    fromsa *userbuf_t) (int, int, int) {
	// XXX what does recv'ing on an unbound unix datagram socket supposed
	// to do? openbsd and linux seem to block forever.
	if !sf.bound {
		return 0, 0, -ECONNREFUSED
	}
	sund := sf.sunaddr.sund
	// XXX send userbuf to sund directly
	sund.outsz <- dst.remain()
	sbuf := <- sund.out

	ret, err := dst.write(sbuf.data)
	if err != 0 {
		return 0, 0, err
	}
	// fill in from address
	var addrlen int
	if fromsa.remain() > 0 {
		sa := sbuf.from.sockaddr_un()
		addrlen, err = fromsa.write(sa)
		if err != 0 {
			return 0, 0, err
		}
	}
	return ret, addrlen, 0
}

func (sf *sudfops_t) pollone(pm pollmsg_t) ready_t {
	if !sf.bound {
		return pm.events & R_ERROR
	}
	sf.sunaddr.sund.poll_in <- pm
	status := <- sf.sunaddr.sund.poll_out
	return status
}

func (sf *sudfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	return -ENOSYS
}

func (sf *sudfops_t) getsockopt(proc *proc_t, opt int, bufarg *userbuf_t,
    intarg int) (int, int) {
	return 0, -EOPNOTSUPP
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
func (sa *sunaddr_t) sockaddr_un() []uint8 {
	ret := make([]uint8, 16)
	// len
	writen(ret, 1, 0, len(sa.path))
	// family
	writen(ret, 1, 1, AF_UNIX)
	// path
	ret = append(ret, sa.path...)
	ret = append(ret, 0)
	return ret
}

type sunbuf_t struct {
	from	sunaddr_t
	data	[]uint8
}

type sund_t struct {
	adm		chan bool
	finish		chan bool
	outsz		chan int
	out		chan sunbuf_t
	inret		chan int
	in		chan sunbuf_t
	poll_in		chan pollmsg_t
	poll_out 	chan ready_t
	pollers		pollers_t
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
	poll_in := make(chan pollmsg_t)
	poll_out := make(chan ready_t)

	ns.adm = adm
	ns.finish = finish
	ns.outsz = outsz
	ns.out = out
	ns.inret = inret
	ns.in = in
	ns.poll_in = poll_in
	ns.poll_out = poll_out

	go func() {
		bstart := make([]sunbuf_t, 0, 10)
		buf := bstart
		buflen := 0
		bufadd := func(n sunbuf_t) {
			if len(n.data) == 0 {
				return
			}
			buf = append(buf, n)
			buflen += len(n.data)
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
			buflen -= len(ret.data)
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
				if buflen >= sunbufsz {
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
				if buflen == 0 {
					panic("no data")
				}
				d := bufdeq(sz)
				out <- d
			case pm := <- ns.poll_in:
				ev := pm.events
				var ret ready_t
				// check if reading, writing, or errors are
				// available. ignore R_ERROR
				if ev & R_READ != 0 && toutsz != nil {
					ret |= R_READ
				}
				if ev & R_WRITE != 0 && tin != nil {
					ret |= R_WRITE
				}
				if ret == 0 && pm.dowait {
					ns.pollers.addpoller(&pm)
				}
				ns.poll_out <- ret
			}

			// block writes if socket is full
			// block reads if socket is empty
			if buflen > 0 {
				toutsz = outsz
				ns.pollers.wakeready(R_READ)
			} else {
				toutsz = nil
			}
			if buflen < sunbufsz {
				tin = in
				ns.pollers.wakeready(R_WRITE)
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

// may need this later...
// unix stream socket with connection
//type susd_t struct {
//	// admittance/non-blocking
//	admit		chan bool
//
//	// sharing/closing fd
//	openc		chan int
//
//	// writing
//	in		chan *userbuf_t
//	innoblk		chan *userbuf_t
//	inret		chan int
//
//	// reading
//	out		chan *userbuf_t
//	outnoblk	chan *userbuf_t
//	outret		chan int
//
//	// polling
//	poll_in		chan pollmsg_t
//	poll_out	chan ready_t
//}
//
//func susd_start() *susd_t {
//	ret := &susd_t{}
//	admit := make(chan bool)
//	openc := make(chan int)
//	in := make(chan *userbuf_t)
//	inret := make(chan int)
//	_out := make(chan *userbuf_t)
//	outret := make(chan int)
//	poll_in := make(chan pollmsg_t)
//	poll_out := make(chan ready_t)
//	ret.admit = admit
//	ret.openc = openc
//	ret.in = in
//	ret.inret = inret
//	ret.out = _out
//	ret.outret = outret
//	ret.poll_in = poll_in
//	ret.poll_out = poll_out
//
//	go func() {
//		bufsz := 512
//		cbuf := circbuf_t{}
//		cbuf.cb_init(bufsz)
//
//		pollers := pollers_t{}
//
//		admits := 0
//		opencnt := 2
//		done := false
//
//		// socket initially empty
//		var out chan *userbuf_t
//		for !done {
//			select {
//			case admit <- true:
//				admits++
//				continue
//			case delta := <- openc:
//				opencnt += delta
//			case src := <- in:
//				if opencnt == 1 {
//					inret <- -ECONNRESET
//					break
//				}
//				read, err := cbuf.copyin(src)
//				if err != 0 {
//					inret <- err
//				}
//				inret <- read
//			case dst := <- out:
//				wrote, err := cbuf.copyout(dst)
//				if err != 0 {
//					outret <- err
//				}
//				outret <- wrote
//			case pm := <- poll_in:
//				ev := pm.events
//				var r ready_t
//				if ev & R_READ != 0 && out != nil {
//					r |= R_READ
//				}
//				if ev & R_WRITE != 0 && in != nil {
//					r |= R_WRITE
//				}
//				if r == 0 {
//					pollers.addpoller(&pm)
//				}
//			}
//			admits--
//			// XXXPANIC
//			if admits < 0 {
//				panic("neg admits")
//			}
//			// block readers/writers if buffer is empty/full or if
//			// one end of the connection is closed.
//			if !cbuf.full() || opencnt == 1 {
//				in = ret.in
//				pollers.wakeready(R_READ)
//			} else {
//				in = nil
//			}
//			if !cbuf.empty() || opencnt == 1 {
//				out = ret.out
//				pollers.wakeready(R_WRITE)
//			} else {
//				out = nil
//			}
//			if opencnt == 0 {
//				done = true
//			}
//		}
//
//		close(admit)
//		for i := 0; i < admits; i++ {
//			select {
//			case <- openc:
//			case <- ret.in:
//				inret <- -ECONNRESET
//			case <- ret.out:
//				outret <- -ENOTCONN
//			case pm := <- poll_in:
//				poll_out <- pm.events & R_HUP
//			}
//		}
//	}()
//	return ret
//}

type susfops_t struct {
	//susd	*susd_t
	pipein	*pipefops_t
	pipeout	*pipefops_t
	conn	bool
	// to prevent bind(2) and listen(2) races
	bl	sync.Mutex
	bound	bool
	lstn	bool
	myaddr	string
	mysid	int
	options	int
}

func (sus *susfops_t) close() int {
	if !sus.conn {
		return 0
	}
	err1 := sus.pipein.close()
	err2 := sus.pipeout.close()
	if err1 != 0 {
		return err1
	}
	return err2
}

func (sus *susfops_t) fstat(*stat_t) int {
	panic("no imp")
}

func (sus *susfops_t) lseek(int, int) int {
	return -ESPIPE
}

func (sus *susfops_t) mmapi(int) ([]mmapinfo_t, int) {
	return nil, -ENODEV
}

func (sus *susfops_t) pathi() inum {
	panic("unix stream cwd?")
}

func (sus *susfops_t) read(dst *userbuf_t) (int, int) {
	read, _, err := sus.recvfrom(nil, dst, nil)
	return read, err
}

func (sus *susfops_t) reopen() int {
	if !sus.conn {
		return 0
	}
	err1 := sus.pipein.reopen()
	err2 := sus.pipeout.reopen()
	if err1 != 0 {
		return err1
	}
	return err2
}

func (sus *susfops_t) write(src *userbuf_t) (int, int) {
	wrote, err := sus.sendto(nil, src, nil, 0)
	if err == -EPIPE {
		err = -ECONNRESET
	}
	return wrote, err
}

func (sus *susfops_t) fullpath() (string, int) {
	panic("weird cwd")
}

func (sus *susfops_t) truncate(newlen uint) int {
	return -EINVAL
}

func (sus *susfops_t) pread(dst *userbuf_t, offset int) (int, int) {
	return 0, -ESPIPE
}

func (sus *susfops_t) pwrite(src *userbuf_t, offset int) (int, int) {
	return 0, -ESPIPE
}

func (sus *susfops_t) accept(*proc_t, *userbuf_t) (fdops_i, int, int) {
	return nil, 0, -EINVAL
}

func (sus *susfops_t) bind(proc *proc_t, saddr []uint8) int {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.bound {
		return -EINVAL
	}
	poff := 2
	path := slicetostr(saddr[poff:])
	sid := susid_new()

	// create special file
	pi := proc.cwd.fops.pathi()
	fsf, err := _fs_open(path, O_CREAT | O_EXCL, 0, pi, D_SUS, sid)
	if err != 0 {
		return err
	}
	if fs_close(fsf.priv) != 0 {
		panic("must succeed")
	}
	sus.myaddr = path
	sus.mysid = sid
	sus.bound = true
	return 0
}

func (sus *susfops_t) connect(proc *proc_t, saddr []uint8) int {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.conn {
		return -EISCONN
	}
	poff := 2
	path := slicetostr(saddr[poff:])

	// lookup sid
	st := &stat_t{}
	st.init()
	err := fs_stat(path, st, proc.cwd.fops.pathi())
	if err != 0 {
		return err
	}
	maj, min := unmkdev(st.rdev())
	if maj != D_SUS {
		return -ECONNREFUSED
	}
	sid := min

	allsusl.Lock()
	susld, ok := allsusl.m[sid]
	allsusl.Unlock()
	if !ok {
		return -ECONNREFUSED
	}
	_, ok = <- susld.admit
	if !ok {
		return -ECONNREFUSED
	}
	ch := make(chan *pipe_t)
	susld.push <- ch
	pipea := <- ch
	if pipea == nil {
		return -ECONNREFUSED
	}
	pipeb := <- ch

	sus.pipein = &pipefops_t{pipe: pipea, options: sus.options}
	sus.pipeout = &pipefops_t{pipe: pipeb, writer: true, options: sus.options}
	sus.conn = true
	return 0
}

func (sus *susfops_t) listen(proc *proc_t, backlog int) (fdops_i, int) {
	sus.bl.Lock()
	defer sus.bl.Unlock()

	if sus.conn {
		return nil, -EISCONN
	}
	if !sus.bound {
		return nil, -EINVAL
	}
	if sus.lstn {
		return nil, -EINVAL
	}
	sus.lstn = true

	// create a listening socket
	susld := susld_start(sus.mysid, backlog)
	newsock := &suslfops_t{susld: susld, myaddr: sus.myaddr,
	    options: sus.options}
	allsusl.Lock()
	// XXXPANIC
	if _, ok := allsusl.m[sus.mysid]; ok {
		panic("susl exists")
	}
	allsusl.m[sus.mysid] = susld
	allsusl.Unlock()

	return newsock, 0
}

func (sus *susfops_t) sendto(proc *proc_t, src *userbuf_t,
    toaddr []uint8, flags int) (int, int) {
	if !sus.conn {
		return 0, -ENOTCONN
	}
	if toaddr != nil {
		return 0, -EISCONN
	}

	return sus.pipeout.write(src)
}

func (sus *susfops_t) recvfrom(proc *proc_t, dst *userbuf_t,
    fromsa *userbuf_t) (int, int, int) {
	if !sus.conn {
		return 0, 0, -ENOTCONN
	}

	ret, err := sus.pipein.read(dst)
	return ret, 0, err
}

func (sus *susfops_t) pollone(pm pollmsg_t) ready_t {
	if !sus.conn {
		return pm.events & R_ERROR
	}

	// pipefops_t.pollone() doesn't allow polling for reading on write-end
	// of pipe and vice versa
	var readyin ready_t
	var readyout ready_t
	both := pm.events & (R_READ|R_WRITE) == 0
	if both || pm.events & R_READ != 0 {
		readyin = sus.pipein.pollone(pm)
	}
	if readyin != 0 {
		return readyin
	}
	if both || pm.events & R_WRITE != 0 {
		readyout = sus.pipeout.pollone(pm)
	}
	return readyin | readyout
}

func (sus *susfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	switch cmd {
	case F_GETFL:
		return sus.options
	case F_SETFL:
		sus.options = opt
		if sus.conn {
			sus.pipein.options = opt
			sus.pipeout.options = opt
		}
		return 0
	default:
		panic("weird cmd")
	}
}

func (sus *susfops_t) getsockopt(proc *proc_t, opt int, bufarg *userbuf_t,
    intarg int) (int, int) {
	switch opt {
	case SO_ERROR:
		if !proc.userwriten(bufarg.userva, 4, 0) {
			return 0, -EFAULT
		}
		return 4, 0
	default:
		return 0, -EOPNOTSUPP
	}
}

var _susid uint64

func susid_new() int {
	newid := atomic.AddUint64(&_susid, 1)
	return int(newid)
}

type allsusl_t struct {
	m	map[int]*susld_t
	sync.Mutex
}

var allsusl = allsusl_t{m: map[int]*susld_t{}}

// listening unix stream socket
type susld_t struct {
	// admittance
	admit		chan bool

	// sharing/closing fd
	openc		chan int

	// new connections
	push		chan chan *pipe_t
	pop		chan chan *pipe_t
	popnoblk	chan chan *pipe_t

	// poll
	poll_in		chan pollmsg_t
	poll_out	chan ready_t
}

func susld_start(mysid, backlog int) *susld_t {
	ret := &susld_t{}
	admit := make(chan bool)
	openc := make(chan int)
	push := make(chan chan *pipe_t)
	_pop := make(chan chan *pipe_t)
	popnoblk := make(chan chan *pipe_t)
	poll_in := make(chan pollmsg_t)
	poll_out := make(chan ready_t)
	ret.admit = admit
	ret.openc = openc
	ret.push = push
	ret.pop = _pop
	ret.popnoblk = popnoblk
	ret.poll_in = poll_in
	ret.poll_out = poll_out

	go func() {
		// circular buffer
		_conns := make([]chan *pipe_t, 0, backlog)
		conns := _conns

		qpop := func() {
			conns = conns[1:]
			if len(conns) == 0 {
				conns = _conns
			}
		}

		admits := 0
		opencnt := 2
		pollers := pollers_t{}
		// queue is initially empty
		var pop chan chan *pipe_t
		done := false
		for !done {
			var first chan *pipe_t
			if len(conns) > 0 {
				first = conns[0]
			}
			select {
			case admit <- true:
				admits++
				continue
			case delta := <- openc:
				opencnt += delta
			case e := <- push:
				if len(conns) == backlog {
					e <- nil
				}
				conns = append(conns, e)
			case pop <- first:
				qpop()
			case popnoblk <- first:
				if len(conns) != 0 {
					qpop()
				}
			case pm := <- poll_in:
				ev := pm.events
				// are new connections available?
				if ev & R_READ != 0 {
					var st ready_t
					if pop == nil {
						st = 0
						if pm.dowait {
							pollers.addpoller(&pm)
						}
					} else {
						st = R_READ
					}
					poll_out <- st
				}
			}
			admits--
			// XXXPANIC
			if admits < 0 {
				panic("neg admits")
			}
			// block acceptors if queue is empty
			if len(conns) == 0 {
				pop = nil
			} else {
				pop = ret.pop
				pollers.wakeready(R_READ)
			}
			// terminate?
			if opencnt == 0 {
				done = true
			}
		}

		// fail requests that race with last close
		allsusl.Lock()
		delete(allsusl.m, mysid)
		allsusl.Unlock()
		close(admit)
		for i := 0; i < admits; i++ {
			select {
			case <- openc:
			case e := <- push:
				e <- nil
			case ret.pop <- nil:
			case pm := <- poll_in:
				poll_out <- pm.events & R_HUP
			}
		}
	}()
	return ret
}

type suslfops_t struct {
	susld	*susld_t
	myaddr	string
	options	int
}

func (sul *suslfops_t) _admit() bool {
	_, ok := <- sul.susld.admit
	return ok
}

func (sul *suslfops_t) close() int {
	if !sul._admit() {
		return -EBADF
	}
	sul.susld.openc <- -1
	return 0
}

func (sul *suslfops_t) fstat(*stat_t) int {
	panic("no imp")
}

func (sul *suslfops_t) lseek(int, int) int {
	return -ESPIPE
}

func (sul *suslfops_t) mmapi(int) ([]mmapinfo_t, int) {
	return nil, -ENODEV
}

func (sul *suslfops_t) pathi() inum {
	panic("unix stream listener cwd?")
}

func (sul *suslfops_t) read(*userbuf_t) (int, int) {
	return 0, -ENOTCONN
}

func (sul *suslfops_t) reopen() int {
	if !sul._admit() {
		return -EBADF
	}
	sul.susld.openc <- 1
	return 0
}

func (sul *suslfops_t) write(*userbuf_t) (int, int) {
	return 0, -ENOTCONN
}

func (sul *suslfops_t) fullpath() (string, int) {
	panic("weird cwd")
}

func (sul *suslfops_t) truncate(newlen uint) int {
	return -EINVAL
}

func (sul *suslfops_t) pread(dst *userbuf_t, offset int) (int, int) {
	return 0, -ESPIPE
}

func (sul *suslfops_t) pwrite(src *userbuf_t, offset int) (int, int) {
	return 0, -ESPIPE
}

func (sul *suslfops_t) accept(proc *proc_t,
    fromsa *userbuf_t) (fdops_i, int, int) {
	if !sul._admit() {
		return nil, 0, -EBADF
	}
	noblk := sul.options & O_NONBLOCK != 0
	popc := sul.susld.pop
	ferr := -EBADF
	if noblk {
		popc = sul.susld.popnoblk
		ferr = -EWOULDBLOCK
	}
	newconn := <- popc
	if newconn == nil {
		return nil, 0, ferr
	}
	pipea := &pipe_t{}
	pipeb := &pipe_t{}
	pipea.pipe_start()
	pipeb.pipe_start()
	newconn <- pipeb
	newconn <- pipea
	pipein := &pipefops_t{pipe: pipea, options: sul.options}
	pipeout := &pipefops_t{pipe: pipeb, writer: true, options: sul.options}
	ret := &susfops_t{pipein: pipein, pipeout: pipeout, conn: true,
	    options: sul.options}
	return ret, 0, 0
}

func (sul *suslfops_t) bind(*proc_t, []uint8) int {
	return -EINVAL
}

func (sul *suslfops_t) connect(proc *proc_t, sabuf []uint8) int {
	return -EINVAL
}

func (sul *suslfops_t) listen(proc *proc_t, backlog int) (fdops_i, int) {
	return nil, -EINVAL
}

func (sul *suslfops_t) sendto(*proc_t, *userbuf_t, []uint8, int) (int, int) {
	return 0, -ENOTCONN
}

func (sul *suslfops_t) recvfrom(*proc_t, *userbuf_t,
    *userbuf_t) (int, int, int) {
	return 0, 0, -ENOTCONN
}

func (sul *suslfops_t) pollone(pm pollmsg_t) ready_t {
	if !sul._admit() {
		return pm.events & (R_HUP|R_ERROR)
	}
	sul.susld.poll_in <- pm
	return <- sul.susld.poll_out
}

func (sul *suslfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	switch cmd {
	case F_GETFL:
		return sul.options
	case F_SETFL:
		sul.options = opt
		return 0
	default:
		panic("weird cmd")
	}
}

func (sul *suslfops_t) getsockopt(proc *proc_t, opt int, bufarg *userbuf_t,
    intarg int) (int, int) {
	return 0, -EOPNOTSUPP
}

func sys_listen(proc *proc_t, fdn, backlog int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	if backlog < 0 {
		backlog = 0
	}
	newfops, err := fd.fops.listen(proc, backlog)
	if err != 0 {
		return err
	}
	// replace old fops
	proc.fdl.Lock()
	fd.fops = newfops
	proc.fdl.Unlock()
	return 0
}

func sys_getsockopt(proc *proc_t, fdn, level, opt, optvaln, optlenn int) int {
	if level != SOL_SOCKET {
		panic("no imp")
	}
	var olen int
	if optlenn != 0 {
		l, ok := proc.userreadn(optlenn, 8)
		if !ok {
			return -EFAULT
		}
		if l < 0 {
			return -EFAULT
		}
		olen = l
	}
	bufarg := proc.mkuserbuf(optvaln, olen)
	intarg := optvaln
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	optwrote, err := fd.fops.getsockopt(proc, opt, bufarg, intarg)
	if err != 0 {
		return err
	}
	if optlenn != 0 {
		if !proc.userwriten(optlenn, 8, optwrote) {
			return -EFAULT
		}
	}
	return 0
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
	var ret int

	// copy parents trap frame
	chtf := [TFSIZE]int{}
	chtf = *ptf

	if mkproc {

		// copy fd table
		parent.fdl.Lock()
		cfds := make([]*fd_t, len(parent.fds))
		for i := range parent.fds {
			if parent.fds[i] != nil {
				tfd, err := copyfd(parent.fds[i])
				// copying an fd may fail if another thread
				// closes the fd out from under us
				if err == 0 {
					cfds[i] = tfd
				}
			}
		}
		parent.fdl.Unlock()

		child = proc_new(parent.name + " [child]",
		    parent.cwd, cfds)
		child.pwait = &parent.mywait
		parent.mywait.start_proc(child.pid)

		// fork parent address space
		parent.Lock_pmap()
		pmap, p_pmap := pg_new()
		child.pmap = pmap
		child.p_pmap = p_pmap
		rsp := chtf[TF_RSP]
		doflush := parent.vm_fork(child, rsp)

		// flush all ptes now marked COW
		if doflush {
			// this flushes the TLB for now
			parent.tlbshoot(0, 1)
		}
		parent.Unlock_pmap()

		childtid = child.tid0
		ret = child.pid
	} else {
		// XXX XXX XXX need to copy FPU state from parent thread to
		// child thread

		// validate tfork struct
		tcb, ok1      := parent.userreadn(tforkp + 0, 8)
		tidaddrn, ok2 := parent.userreadn(tforkp + 8, 8)
		stack, ok3    := parent.userreadn(tforkp + 16, 8)
		if !ok1 || !ok2 || !ok3 {
			return -EFAULT
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
		parent.mywait.start_thread(childtid)

		v := int(childtid)
		chtf[TF_RSP] = stack
		if writetid && !parent.userwriten(tidaddrn, 8, v) {
			panic("unexpected unmap")
		}
		ret = v
	}

	chtf[TF_RAX] = 0

	child.sched_add(&chtf, childtid)

	return ret
}

func sys_pgfault(proc *proc_t, pte *int, faultaddr int) {
	// pmap is Lock'ed in trap_pgfault...
	cow := *pte & PTE_COW != 0
	wascow := *pte & PTE_WASCOW != 0
	if cow && wascow {
		panic("invalid state")
	}

	if cow {
		// copy page
		dst, p_dst := pg_new()
		p_src := *pte & PTE_ADDR
		src := dmap(p_src)
		if p_src != p_zeropg {
			*dst = *src
		}

		seg, _, ok := proc.vmregion.contain(faultaddr)
		if !ok {
			panic("no seg")
		}

		// insert new page into pmap
		va := faultaddr & PGMASK
		perms := (*pte & PTE_FLAGS) & ^PTE_COW
		perms |= PTE_W | PTE_WASCOW
		proc.page_insert(va, seg, dst, p_dst, perms, false)

		proc.tlbshoot(faultaddr, 1)
	} else if wascow {
		// land here if two threads fault on same page
		if wascow && *pte & PTE_W == 0 {
			panic("handled but read-only")
		}
	} else {
		panic("fault on non-cow page")
	}
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
	// XXX a multithreaded process that execs is broken; POSIX2008 says
	// that all threads should terminate before exec.
	if proc.thread_count() > 1 {
		panic("fix exec with many threads")
	}

	proc.Lock_pmap()
	defer proc.Unlock_pmap()

	// save page trackers in case the exec fails
	opmpages := proc.pmpages
	proc.pmpages = &pmtracker_t{}
	proc.pmpages.pminit()
	ovmreg := proc.vmregion.clear()

	// create kernel page table
	opmap := proc.pmap
	op_pmap := proc.p_pmap
	proc.pmap, proc.p_pmap = pg_new()
	for _, e := range kents {
		proc.pmap[e.pml4slot] = e.entry
	}

	restore := func() {
		proc.pmpages = opmpages
		proc.pmap = opmap
		proc.p_pmap = op_pmap
		proc.vmregion.head = ovmreg
	}

	// load binary image -- get first block of file
	file, err := fs_open(paths, O_RDONLY, 0, proc.cwd.fops.pathi(), 0, 0)
	if err != 0 {
		restore()
		return err
	}
	defer func() {
		close_panic(file)
	}()

	hdata := make([]uint8, 512)
	ub := &userbuf_t{}
	ub.fake_init(hdata)
	ret, err := file.fops.read(ub)
	if err != 0 {
		restore()
		return err
	}
	if ret < len(hdata) {
		hdata = hdata[0:ret]
	}

	// assume its always an elf, for now
	elfhdr := &elf_t{hdata}
	ok := elfhdr.sanity()
	if !ok {
		restore()
		return -EPERM
	}

	// elf_load() will create two copies of TLS section: one for the fresh
	// copy and one for thread 0
	freshtls, t0tls, tlssz := elfhdr.elf_load(proc, file)

	// map new stack
	numstkpages := 6
	// +1 for the guard page
	stksz := (numstkpages + 1) * PGSIZE
	stackva := proc.unusedva_inner(0x0ff << 39, stksz)
	seg := proc.mkvmseg(stackva, stksz)
	stackva += stksz
	for i := 0; i < numstkpages; i++ {
		var stack *[512]int
		var p_stack int
		perms := PTE_U
		if i == 0 {
			stack, p_stack = pg_new()
			perms |= PTE_W
		} else {
			stack = zeropg
			p_stack = p_zeropg
			perms |= PTE_COW
		}
		va := stackva - PGSIZE*(i+1)
		proc.page_insert(va, seg, stack, p_stack, perms, true)
	}

	// XXX make insertargs not fail by using more than a page...
	argc, argv, ok := insertargs(proc, args)
	if !ok {
		// restore old process
		restore()
		return -EINVAL
	}

	// close fds marked with CLOEXEC
	for fdn, fd := range proc.fds {
		if fd == nil {
			continue
		}
		if fd.perms & FD_CLOEXEC != 0 {
			if sys_close(proc, fdn) != 0 {
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
	if !proc.k2user_inner(buf, bufdest) {
		panic("must succeed")
	}

	// commit new image state
	tf[TF_RSP] = bufdest
	tf[TF_RIP] = elfhdr.entry()
	tf[TF_RFLAGS] = TF_FL_IF
	ucseg := 5
	udseg := 6
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
	uva := proc.unusedva_inner(0, PGSIZE)
	seg := proc.mkvmseg(uva, PGSIZE)
	pg, p_pg := pg_new()
	proc.page_insert(uva, seg, pg, p_pg, PTE_U, true)
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
		if !proc.k2user_inner(arg, uva + cnt) {
			// args take up more than a page? the user is on their
			// own.
			return 0, 0, false
		}
		cnt += len(arg)
	}
	argptrs[len(argptrs) - 1] = 0
	// now put the array of strings
	argstart := uva + cnt
	vdata, ok := proc.userdmap8_inner(argstart)
	if !ok || len(vdata) < len(argptrs)*8 {
		fmt.Printf("no room for args")
		return 0, 0, false
	}
	for i, ptr := range argptrs {
		writen(vdata, 8, i*8, ptr)
	}
	return len(args), argstart, true
}

func mkexitsig(sig int) int {
	if sig < 0 || sig > 32 {
		panic("bad sig")
	}
	return sig << SIGSHIFT
}

func sys_exit(proc *proc_t, tid tid_t, status int) {
	// set doomed to all other threads die
	proc.doomall()
	proc.thread_dead(tid, status, true)
}

func sys_threxit(proc *proc_t, tid tid_t, status int) {
	proc.thread_dead(tid, status, false)
}

func sys_wait4(proc *proc_t, tid tid_t, wpid, statusp, options, rusagep,
    threadwait int) int {
	if wpid == WAIT_MYPGRP || options != 0 {
		panic("no imp")
	}

	// no waiting for yourself!
	if tid == tid_t(wpid) {
		return -ECHILD
	}

	resp := proc.mywait.reap(wpid)

	if resp.err != 0 {
		return resp.err
	}
	if threadwait == 0 {
		ok :=  true
		if statusp != 0 {
			ok = proc.userwriten(statusp, 4, resp.status)
		}
		// update total child rusage
		proc.catime.add(&resp.atime)
		if rusagep != 0 {
			ru := resp.atime.to_rusage()
			if !proc.k2user(ru, rusagep) {
				ok = false
			}
		}
		if !ok {
			return -EFAULT
		}
	} else {
		if statusp != 0 {
			if !proc.userwriten(statusp, 8, resp.status) {
				return -EFAULT
			}
		}
	}
	return resp.pid
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

func sys_pread(proc *proc_t, fdn, bufn, lenn, offset int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	dst := proc.mkuserbuf(bufn, lenn)
	ret, err := fd.fops.pread(dst, offset)
	if err != 0 {
		return err
	}
	return ret
}

func sys_pwrite(proc *proc_t, fdn, bufn, lenn, offset int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	src := proc.mkuserbuf(bufn, lenn)
	ret, err := fd.fops.pwrite(src, offset)
	if err != 0 {
		return err
	}
	return ret
}

type futexmsg_t struct {
	op	uint
	aux	uint32
	ack	chan int
	othmut	futex_t
	cndtake	[]chan int
	totake	[]_futto_t
	fumem	futumem_t
	timeout	time.Time
	useto	bool
}

func (fm *futexmsg_t) fmsg_init(op uint, aux uint32, ack chan int) {
	fm.op = op
	fm.aux = aux
	fm.ack = ack
}

// futex timeout metadata
type _futto_t struct {
	when	time.Time
	tochan	<-chan time.Time
	who	chan int
}

type futex_t struct {
	reopen	chan int
	cmd	chan futexmsg_t
	_cnds	[]chan int
	cnds	[]chan int
	_tos	[]_futto_t
	tos	[]_futto_t
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

func (f *futex_t) futex_start() {
	f._cnds = make([]chan int, 0, 10)
	f.cnds = f._cnds
	f._tos = make([]_futto_t, 0, 10)
	f.tos = f._tos

	pack := make(chan int)
	opencount := 1
	for opencount > 0 {
		tochan, towho := f.tonext()
		select {
		case <- tochan:
			f.towake(towho, 0)
		case d := <- f.reopen:
			opencount += d
		case fm := <- f.cmd:
			switch fm.op {
			case FUTEX_SLEEP:
				val, err := fm.fumem.futload()
				if err != 0 {
					fm.ack <- err
					break
				}
				if val != fm.aux {
					// owner just unlocked and it's this
					// thread's turn; don't sleep
					fm.ack <- 0
				} else {
					if fm.useto {
						f.toadd(fm.ack, fm.timeout)
					}
					f.cndsleep(fm.ack)
				}
			case FUTEX_WAKE:
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
			case FUTEX_CNDGIVE:
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
				err := <- nfm.ack
				if err == 0 {
					f.cnds = f._cnds
					f.tos = f._tos
				}
				fm.ack <- err
			case _FUTEX_CNDTAKE:
				// add new waiters to our queue; get them
				// tickets
				here := fm.cndtake
				f.cnds = append(f.cnds, here...)
				tohere := fm.totake
				f.tos = append(f.tos, tohere...)
				fm.ack <- 0
			default:
				panic("bad futex op")
			}
		}
	}
}

type allfutex_t struct {
	sync.Mutex
	m	map[uintptr]futex_t
}

var _allfutex = allfutex_t{m: map[uintptr]futex_t{}}

func futex_ensure(uniq uintptr) futex_t {
	_allfutex.Lock()
	r, ok := _allfutex.m[uniq]
	if !ok {
		r.reopen = make(chan int)
		r.cmd = make(chan futexmsg_t)
		_allfutex.m[uniq] = r
		go r.futex_start()
	}
	_allfutex.Unlock()
	return r
}

// pmap must be locked. maps user va to kernel va. returns kva as uintptr and
// *uint32
func _uva2kva(proc *proc_t, va uintptr) (uintptr, *uint32, int) {
	proc.lockassert_pmap()

	seg, _, ok := proc.vmregion.contain(int(va))
	if !ok {
		return 0, nil, -EFAULT
	}
	pgva, ok := seg.gettrack(int(va))
	if !ok {
		return 0, nil, -EFAULT
	}
	pgoff := uintptr(va) & uintptr(PGOFFSET)
	uniq := uintptr(unsafe.Pointer(pgva)) + pgoff
	return uniq, (*uint32)(unsafe.Pointer(uniq)), 0
}

func va2fut(proc *proc_t, va uintptr) (futex_t, int) {
	proc.Lock_pmap()
	defer proc.Unlock_pmap()

	var zf futex_t
	uniq, _, err := _uva2kva(proc, va)
	if err != 0 {
		return zf, err
	}
	return futex_ensure(uniq), 0
}

// an object for atomically looking-up and incrementing/loading from a user
// address
type futumem_t struct {
	proc	*proc_t
	umem	uintptr
}

func (fu *futumem_t) futload() (uint32, int) {
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

func sys_futex(proc *proc_t, _op, _futn, _fut2n, aux, timespecn int) int {
	op := uint(_op)
	if op > _FUTEX_LAST {
		return -EINVAL
	}
	futn := uintptr(_futn)
	fut2n := uintptr(_fut2n)
	fut, err := va2fut(proc, futn)
	if err != 0 {
		return err
	}

	var fm futexmsg_t
	// could lazily allocate one futex channel per thread
	fm.fmsg_init(op, uint32(aux), make(chan int))
	fm.fumem = futumem_t{proc, futn}

	if timespecn != 0 {
		_, when, err := proc.usertimespec(timespecn)
		if err != 0 {
			return err
		}
		n := time.Now()
		if when.Before(n) {
			return -EINVAL
		}
		fm.timeout = when
		fm.useto = true
	}

	if op == FUTEX_CNDGIVE {
		fm.othmut, err = va2fut(proc, fut2n)
		if err != 0 {
			return err
		}
	}

	fut.cmd <- fm
	ret := <- fm.ack
	return ret
}

func sys_fcntl(proc *proc_t, fdn, cmd, opt int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	switch cmd {
	// general fcntl(2) ops
	case F_GETFD:
		return fd.perms & FD_CLOEXEC
	case F_SETFD:
		if opt & FD_CLOEXEC == 0 {
			fd.perms &^= FD_CLOEXEC
		} else {
			fd.perms |= FD_CLOEXEC
		}
		return 0
	// fd specific fcntl(2) ops
	case F_GETFL, F_SETFL:
		return fd.fops.fcntl(proc, cmd, opt)
	default:
		return -EINVAL
	}
}

func sys_truncate(proc *proc_t, pathn int, newlen uint) int {
	path, ok, toolong := proc.userstr(pathn, NAME_MAX)
	if !ok {
		return -EFAULT
	}
	if toolong {
		return -ENAMETOOLONG
	}
	if err := badpath(path); err != 0 {
		return err
	}
	pi := proc.cwd.fops.pathi()
	f, err := fs_open(path, O_WRONLY, 0, pi, 0, 0)
	if err != 0 {
		return err
	}
	ret := f.fops.truncate(newlen)
	close_panic(f)
	return ret
}

func sys_ftruncate(proc *proc_t, fdn int, newlen uint) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	return fd.fops.truncate(newlen)
}

func sys_getcwd(proc *proc_t, bufn, sz int) int {
	dst := proc.mkuserbuf(bufn, sz)
	pwd, err := proc.cwd.fops.fullpath()
	if err != 0 {
		return err
	}
	_, err = dst.write([]uint8(pwd))
	if err != 0 {
		return err
	}
	if _, err := dst.write([]uint8{0}); err != 0 {
		return err
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

	proc.cwdl.Lock()
	defer proc.cwdl.Unlock()

	pi := proc.cwd.fops.pathi()
	newcwd, err := fs_open(path, O_RDONLY | O_DIRECTORY, 0, pi, 0, 0)
	if err != 0 {
		return err
	}
	close_panic(proc.cwd)
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

type perfrips_t struct {
	rips	[]uintptr
	times	[]int
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

func _prof_nmi(en bool) {
	if en {
		profhw.start()
	} else {
		// stop profiling
		rips := profhw.stop()
		if len(rips) == 0 {
			fmt.Printf("No samples!\n")
			return
		}

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
		//fmt.Printf("K    ns: %v\n", kns)

		//p := pprof.Lookup("block")
		//err := p.WriteTo(&prof, 0)
		//if err != nil {
		//	fmt.Printf("%v\n", err)
		//	return
		//}
		//prof.dump()
	}
}

func sys_fake(proc *proc_t, n int) int {
	en := false
	if n != 0 {
		en = true
	}

	nmiprof := false
	if nmiprof {
		_prof_nmi(en)
	} else {
		_prof_go(en)
	}

	return 0
}

func sys_fake2(proc *proc_t, n int) int {
	idmonl.Lock()
	ret := len(allidmons)
	idmonl.Unlock()
	runtime.GC()
	return ret
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

// XXX use "unsafe" structs instead of the awful "go" way
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
	fileoff	int
	memsz   int
}

const(
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
	phend := poff + phsz * phnum
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

func segload(proc *proc_t, hdr *elf_phdr, mmapi []mmapinfo_t) {
	if hdr.vaddr % PGSIZE != hdr.fileoff % PGSIZE {
		panic("requires copying")
	}
	perms := PTE_U
	//PF_X := 1
	PF_W := 2
	if hdr.flags & PF_W != 0 {
		perms |= PTE_COW
	}

	var vastart int
	var did int
	// this segment's virtual address may start on the same page as the
	// previous segment. if that is the case, we may not be able to avoid
	// copying.
	if _, _, ok := proc.vmregion.contain(hdr.vaddr); ok {
		va := hdr.vaddr
		proc.cowfault(va)
		pg, ok := proc.userdmap8_inner(va)
		if !ok {
			panic("must be mapped")
		}
		pgn := hdr.fileoff / PGSIZE
		bsrc := (*[PGSIZE]uint8)(unsafe.Pointer(mmapi[pgn].pg))[:]
		bsrc = bsrc[va & PGOFFSET:]
		if len(pg) > hdr.filesz {
			pg = pg[0:hdr.filesz]
		}
		copy(pg, bsrc)
		did = len(pg)
		vastart = PGSIZE
	}
	filesz := roundup(hdr.vaddr + hdr.filesz, PGSIZE)
	filesz -= rounddown(hdr.vaddr, PGSIZE)
	seg := proc.mkvmseg(hdr.vaddr + did, hdr.memsz - did)
	for i := vastart; i < filesz; i += PGSIZE {
		pgn := (hdr.fileoff + i) / PGSIZE
		pg := mmapi[pgn].pg
		p_pg := mmapi[pgn].phys
		proc.page_insert(hdr.vaddr + i, seg, pg, p_pg, perms, true)
	}
	// setup bss: zero first page, map the rest to the zero page
	for i := hdr.vaddr + hdr.filesz; i < hdr.vaddr + hdr.memsz; {
		if (i % PGSIZE) == 0 {
			// user zero pg
			proc.page_insert(i, seg, zeropg, p_zeropg, perms, true)
			i += PGSIZE
		} else {
			// make our own copy of the file's page
			proc.cowfault(i)
			pg, ok := proc.userdmap8_inner(i)
			if !ok {
				panic("must be mapped")
			}
			for j := range pg {
				pg[j] = 0
			}
			i += len(pg)
		}
	}
}

// returns user address of read-only TLS, thread 0's TLS image, and TLS size.
// caller must hold proc's pagemap lock.
func (e *elf_t) elf_load(proc *proc_t, f *fd_t) (int, int, int) {
	PT_LOAD := 1
	PT_TLS  := 7
	istls := false
	tlssize := 0
	var tlsaddr int
	var tlscopylen int

	// load each elf segment directly into process memory
	for _, hdr := range e.headers() {
		// XXX get rid of worthless user program segments
		if hdr.etype == PT_TLS {
			istls = true
			tlsaddr = hdr.vaddr
			tlssize = roundup(hdr.memsz, 8)
			tlscopylen = hdr.filesz
		} else if hdr.etype == PT_LOAD && hdr.vaddr >= USERMIN {
			mmapi, err := f.fops.mmapi(0)
			if err != 0 {
				panic("must succeed")
			}
			segload(proc, &hdr, mmapi)
		}
	}

	freshtls := 0
	t0tls := 0
	if istls {
		// create fresh TLS image and map it COW for thread 0
		l := roundup(tlsaddr + tlssize, PGSIZE)
		l -= rounddown(tlsaddr, PGSIZE)

		freshtls = proc.unusedva_inner(0, 2*l)
		t0tls = freshtls + l
		seg := proc.mkvmseg(freshtls, 2*l)
		perms := PTE_U

		for i := 0; i < l; i += PGSIZE {
			// allocator zeros objects, so tbss is already
			// initialized.
			pg, p_pg := pg_new()
			proc.page_insert(freshtls + i, seg, pg, p_pg, perms,
			    true)
			src, ok := proc.userdmap8_inner(tlsaddr + i)
			if !ok {
				panic("must succeed")
			}
			bpg := ((*[PGSIZE]uint8)(unsafe.Pointer(pg)))[:]
			left := tlscopylen - i
			if len(bpg) > left {
				bpg = bpg[0:left]
			}
			copy(bpg, src)
			// map fresh TLS for thread 0
			nperms := perms | PTE_COW
			proc.page_insert(t0tls + i, seg, pg, p_pg, nperms,
			    true)
		}
		// map thread 0's TLS

		// amd64 sys 5 abi specifies that the tls pointer references to
		// the first invalid word past the end of the tls
		t0tls += tlssize
	}
	return freshtls, t0tls, tlssize
}
