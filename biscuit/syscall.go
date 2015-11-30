package main

import "fmt"
import "runtime"
import "runtime/pprof"
import "sync"
import "time"
import "unsafe"

const(
  TFSIZE       = 24
  TFREGS       = 17
  TF_SYSRSP    = 0
  TF_FSBASE    = 1
  TF_R13       = 4
  TF_R12       = 5
  TF_R8        = 9
  TF_RBP       = 10
  TF_RSI       = 11
  TF_RDI       = 12
  TF_RDX       = 13
  TF_RCX       = 14
  TF_RBX       = 15
  TF_RAX       = 16
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
  EINTR	       = 4
  EIO	       = 5
  E2BIG	       = 7
  EBADF        = 9
  ECHILD       = 10
  EFAULT       = 14
  EEXIST       = 17
  ENODEV       = 19
  ENOTDIR      = 20
  EISDIR       = 21
  EINVAL       = 22
  ENOSPC       = 28
  ESPIPE       = 29
  EPIPE        = 32
  ERANGE       = 34
  ENAMETOOLONG = 36
  ENOSYS       = 38
  ENOTEMPTY    = 39
  ENOTSOCK     = 88
  EMSGSIZE     = 90
  ETIMEDOUT    = 110
  ECONNREFUSED = 111
  EINPROGRESS  = 115
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
    SIGSHIFT     = 27
  SYS_WAIT4    = 61
    WAIT_ANY     = -1
    WAIT_MYPGRP  = 0
    WCONTINUED   = 1
    WNOHANG      = 2
    WUNTRACED    = 4
  SYS_KILL     = 62
  SYS_CHDIR    = 80
  SYS_RENAME   = 82
  SYS_MKDIR    = 83
  SYS_LINK     = 86
  SYS_UNLINK   = 87
  SYS_GETTOD   = 96
  SYS_GETRUSG  = 98
    RUSAGE_SELF      = 1
    RUSAGE_CHILDREN  = 2
  SYS_MKNOD    = 133
  SYS_SYNC     = 162
  SYS_REBOOT   = 169
  SYS_NANOSLEEP= 230
  SYS_PIPE2    = 293
  SYS_FAKE     = 31337
  SYS_THREXIT  = 31338
  SYS_FAKE2    = 31339
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
	case SYS_DUP2:
		ret = sys_dup2(p, a1, a2)
	case SYS_PAUSE:
		ret = sys_pause(p)
	case SYS_GETPID:
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
		ret = sys_wait4(p, tid, a1, a2, a3, a4, a5)
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
	case SYS_GETRUSG:
		ret = sys_getrusage(p, a1, a2)
	case SYS_MKNOD:
		ret = sys_mknod(p, a1, a2, a3)
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

func cons_write(src *userbuf_t, off int, ap bool) (int, int) {
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

	n := proc.atime.now()
	ret, err := fd.fops.read(userbuf)
	proc.atime.io_time(n)
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

	apnd := fd.perms & FD_APPEND != 0

	userbuf := proc.mkuserbuf(bufp, sz)

	n := proc.atime.now()
	ret, err := fd.fops.write(userbuf, apnd)
	proc.atime.io_time(n)
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
	n := proc.atime.now()
	pi := proc.cwd.fops.pathi()
	file, err := fs_open(path, flags, mode, pi, 0, 0)
	proc.atime.io_time(n)
	if err != 0 {
		return err
	}
	if flags & O_APPEND != 0 {
		fdperms |= FD_APPEND
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
	n := proc.atime.now()
	<- c
	proc.atime.sleep_time(n)
	return -1
}

func sys_close(proc *proc_t, fdn int) int {
	fd, ok := proc.fd_del(fdn)
	if !ok {
		return -EBADF
	}
	ret := file_close(fd)
	return ret
}

// XXX remove
func file_close(f *fd_t) int {
	return f.fops.close()
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
	defer proc.Unlock_pmap()

	perms := PTE_U
	if prot & PROT_WRITE != 0 {
		perms |= PTE_W
	}
	lenn = roundup(lenn, PGSIZE)
	if lenn/PGSIZE + proc.vmregion.pglen() > proc.ulim.pages {
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

func copyfd(fd *fd_t) (*fd_t, int) {
	nfd := &fd_t{}
	*nfd = *fd
	err := nfd.fops.reopen()
	if err != 0 {
		return nil, err
	}
	return nfd, 0
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
		n := proc.atime.now()
		if file_close(cfd) != 0 {
			panic("must succeed")
		}
		proc.atime.io_time(n)
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
	n := proc.atime.now()
	err := fs_stat(path, buf, proc.cwd.fops.pathi())
	proc.atime.io_time(n)
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
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}
	buf := &stat_t{}
	buf.init()
	n := proc.atime.now()
	err := fd.fops.fstat(buf)
	proc.atime.io_time(n)
	if err != 0 {
		return err
	}

	ok = proc.usercopy(buf.data, statn)
	if !ok {
		return -EFAULT
	}
	return 0
}

func sys_poll(proc *proc_t, fdsn, nfds, timeout int) int {
	if nfds < 0  || timeout < -1 {
		return -EINVAL
	}

	// converts internal states to poll states
	inmask  := POLLIN | POLLPRI
	outmask := POLLOUT | POLLWRBAND
	ready2rev := func(r ready_t) int {
		ret := 0
		if r & R_READ != 0 {
			ret |= inmask
		}
		if r & R_WRITE != 0 {
			ret |= outmask
		}
		if r & R_HUP != 0 {
			ret |= POLLHUP
		}
		if r & R_ERROR != 0 {
			ret |= POLLERR
		}
		return ret
	}

	// pokes poll status bits into user memory. since we only use one
	// priority internally, mask away any POLL bits the user didn't not
	// request.
	ppoke := func(orig, bits int) int {
		origevents := ((orig >> 32) & 0xffff) | POLLNVAL | POLLERR | POLLHUP
		return orig & 0x0000ffffffffffff | ((origevents & bits) << 48)
	}

	// first we tell the underlying device to notify us if their fd is
	// ready. if a device is immediately ready, we don't both to register
	// notifiers with the rest of the devices -- we just ask their status
	// too.
	pollfdsz := 8
	devwait := timeout != 0
	pm := pollmsg_t{}
	readyfds := 0
	for i := 0; i < nfds; i++ {
		va := fdsn + pollfdsz*i
		uw, ok := proc.userreadn(va, 8)
		if !ok {
			return -EFAULT
		}
		fdn := int(uint32(uw))
		// fds < 0 are to be ignored
		if fdn < 0 {
			continue
		}
		fd, ok := proc.fd_get(fdn)
		if !ok {
			uw |= POLLNVAL
			if !proc.userwriten(va, 8, uw) {
				return -EFAULT
			}
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
		pm.pm_set(pev, fdn, devwait, i)
		devstatus := fd.fops.pollone(pm)
		if devstatus != 0 {
			// found at least one ready fd; don't bother having the
			// other fds send notifications. update user revents
			devwait = false
			nuw := ppoke(uw, ready2rev(devstatus))
			if !proc.userwriten(va, 8, nuw) {
				return -EFAULT
			}
			readyfds++
		}
	}

	// if we found a ready fd, we are done
	if !devwait {
		return readyfds
	}

	// otherwise, wait for one notification
	rpm, timedout, err := pm.pm_wait(timeout)
	if err != 0 {
		panic("must succeed")
	}
	if timedout {
		return 0
	}
	// update pollfd revent field
	idx := rpm.phint
	va := fdsn + idx*pollfdsz
	uw, ok := proc.userreadn(va, 8)
	if !ok {
		return -EFAULT
	}
	nuw := ppoke(uw, ready2rev(rpm.events))
	if !proc.userwriten(va, 8, nuw) {
		return -EFAULT
	}
	return 1
}

func sys_lseek(proc *proc_t, fdn, off, whence int) int {
	fd, ok := proc.fd_get(fdn)
	if !ok {
		return -EBADF
	}

	n := proc.atime.now()
	ret := fd.fops.lseek(off, whence)
	proc.atime.io_time(n)
	return ret
}

func sys_pipe2(proc *proc_t, pipen, flags int) int {
	rfp := FD_READ
	wfp := FD_WRITE

	if flags & O_NONBLOCK != 0 {
		panic("no imp yet")
	}

	if flags & O_CLOEXEC != 0 {
		rfp |= FD_CLOEXEC
		wfp |= FD_CLOEXEC
	}

	p := &pipe_t{}
	p.pipe_start()
	rops := &pipefops_t{pipe: p, writer: false}
	wops := &pipefops_t{pipe: p, writer: true}
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

// used by thread executing poll(2).
type pollmsg_t struct {
	notif	chan pollmsg_t
	events	ready_t
	fdn	int
	dowait	bool
	// phint is this fd's index into the user pollfd array
	phint	int
}

func (pm *pollmsg_t) pm_set(events ready_t, fdn int, dowait bool, phint int) {
	if pm.notif == nil {
		// 1-element buffered channel; that way devices can send
		// notifies on the channel asynchronously without blocking.
		pm.notif = make(chan pollmsg_t, 1)
	}
	pm.events = events
	pm.fdn = fdn
	pm.dowait = dowait
	pm.phint = phint
}

// returns the pollmsg_t of ready fd, whether we timed out, and error
func (pm *pollmsg_t) pm_wait(tous int) (pollmsg_t, bool, int) {
	// XXXPANIC
	if tous == 0 {
		panic("wait for non-blocking poll?")
	}
	var tochan <-chan time.Time
	if tous > 0 {
		tochan = time.After(time.Duration(tous) * time.Microsecond)
	}
	var readypm pollmsg_t
	var timeout bool
	select {
	case readypm = <- pm.notif:
	case <- tochan:
		timeout = true
	}
	return readypm, timeout, 0
}

// keeps track of all outstanding pollers. used by devices supporting poll(2)
type pollers_t struct {
	allmask	ready_t
	// just need an unordered set; use array if too slow
	waiters	map[chan pollmsg_t]pollmsg_t
}

func (p *pollers_t) addpoller(pm *pollmsg_t) {
	if p.waiters == nil {
		p.waiters = make(map[chan pollmsg_t]pollmsg_t, 4)
	}
	// XXXPANIC
	if _, ok := p.waiters[pm.notif]; ok {
		panic("poller exists")
	}
	p.waiters[pm.notif] = *pm
	p.allmask |= pm.events
}

func (p *pollers_t) wakeready(r ready_t) {
	if p.allmask & r == 0 {
		return
	}
	var newallmask ready_t
	for k, pm := range p.waiters {
		// found a waiter
		if pm.events & r != 0 {
			pm.events &= r
			// non-blocking send on a 1-element buffered channel
			select {
			case pm.notif <- pm:
			default:
			}
			delete(p.waiters, k)
		} else {
			newallmask |= pm.events
		}
	}
	p.allmask = newallmask
}

type pipe_t struct {
	pollers		pollers_t
	inret		chan int
	in		chan []uint8
	outsz		chan int
	out		chan []uint8
	readers		chan int
	writers		chan int
	admit		chan bool
	poll_in		chan pollmsg_t
	poll_out	chan ready_t
}

func (p *pipe_t) pipe_start() {
	inret := make(chan int)
	in := make(chan []uint8)
	out := make(chan []uint8)
	outsz := make(chan int)
	writers := make(chan int)
	readers := make(chan int)
	p.admit = make(chan bool)
	p.poll_in = make(chan pollmsg_t)
	p.poll_out = make(chan ready_t)
	p.inret = inret
	p.in = in
	p.outsz = outsz
	p.out = out
	p.readers = readers
	p.writers = writers

	go func() {
		pipebufsz := 512
		writec := 1
		readc := 1
		var buf []uint8
		var toutsz chan int
		tin := in
		admit := 0
		for writec > 0 || readc > 0 {
			select {
			case p.admit <- true:
				admit++
				continue
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
			// poll
			case pm := <- p.poll_in:
				ev := pm.events
				var ret ready_t
				// check if reading, writing, or errors are
				// available
				if ev & R_READ != 0 && toutsz != nil {
					ret |= R_READ
				}
				if ev & R_WRITE != 0 && tin != nil {
					ret |= R_WRITE
				}
				if ev & R_HUP != 0 && readc == 0 {
					ret |= R_HUP
				}
				// ignore R_ERROR
				if ret == 0 && pm.dowait {
					p.pollers.addpoller(&pm)
				}
				p.poll_out <- ret
			}

			admit--
			if admit < 0 {
				panic("wtf")
			}

			// allow more reads if there is data in the pipe or if
			// there are no writers and the pipe is empty.
			if len(buf) > 0 || writec == 0 {
				toutsz = outsz
				p.pollers.wakeready(R_READ)
			} else {
				toutsz = nil
			}
			if len(buf) < pipebufsz || readc == 0 {
				tin = in
				p.pollers.wakeready(R_WRITE)
			} else {
				tin = nil
			}
		}

		// fail requests from threads that raced with last closer.
		close(p.admit)
		for ; admit > 0; admit-- {
			select {
			case <- in:
				inret <- -EBADF
			case <- outsz:
				var e []uint8
				out <- e
			case <- writers:
			case <- readers:
			}
		}
	}()
}

type pipefops_t struct {
	pipe	*pipe_t
	writer	bool
}

func (pf *pipefops_t) read(ub *userbuf_t) (int, int) {
	if _, ok := <- pf.pipe.admit; !ok {
		return 0, -EBADF
	}
	sz := ub.len
	p := pf.pipe
	p.outsz <- sz
	buf := <- p.out
	ret, err := ub.write(buf)
	return ret, err
}

func (pf *pipefops_t) write(src *userbuf_t, appnd bool) (int, int) {
	buf := make([]uint8, src.len)
	read, err := src.read(buf)
	if err != 0 {
		return 0, err
	}
	if read != src.len {
		panic("short user read")
	}
	p := pf.pipe
	ret := 0
	for len(buf) > 0 {
		if _, ok := <- p.admit; !ok {
			return 0, -EBADF
		}
		p.in <- buf
		c := <- p.inret
		if c < 0 {
			return 0, c
		}
		buf = buf[c:]
		ret += c
	}
	return ret, 0
}

func (pf *pipefops_t) close() int {
	if _, ok := <- pf.pipe.admit; !ok {
		return -EBADF
	}
	p := pf.pipe
	var ch chan int
	if pf.writer {
		ch = p.writers
	} else {
		ch = p.readers
	}
	ch <- -1
	return 0
}

func (pf *pipefops_t) fstat(st *stat_t) int {
	panic("fstat on pipe")
}

func (pf *pipefops_t) mmapi(offset int) ([]mmapinfo_t, int) {
	panic("mmap on pipe")
}

func (pf *pipefops_t) pathi() inum {
	panic("pipe cwd")
}

func (pf *pipefops_t) reopen() int {
	if _, ok := <- pf.pipe.admit; !ok {
		return -EBADF
	}
	var ch chan int
	if pf.writer {
		ch = pf.pipe.writers
	} else {
		ch = pf.pipe.readers
	}
	ch <- 1
	return 0
}

func (pf *pipefops_t) lseek(int, int) int {
	return -ENODEV
}

func (pf *pipefops_t) bind(*proc_t, []uint8) int {
	return -ENOTSOCK
}

func (pf *pipefops_t) sendto(*proc_t, *userbuf_t, []uint8, int) (int, int) {
	return 0, -ENOTSOCK
}

func (pf *pipefops_t) recvfrom(*proc_t, *userbuf_t,
    *userbuf_t) (int, int, int) {
	return 0, 0, -ENOTSOCK
}

func (pf *pipefops_t) pollone(pm pollmsg_t) ready_t {
	if _, ok := <- pf.pipe.admit; !ok {
		return 0
	}
	// only a pipe writer can poll for writing, and reader for reading
	if pf.writer {
		pm.events &^= R_READ
	} else {
		pm.events &^= R_WRITE
	}
	pf.pipe.poll_in <- pm
	status := <- pf.pipe.poll_out
	return status
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
	n := proc.atime.now()
	ret := fs_rename(old, new, proc.cwd.fops.pathi())
	proc.atime.io_time(n)
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
	n := proc.atime.now()
	ret := fs_mkdir(path, mode, proc.cwd.fops.pathi())
	proc.atime.io_time(n)
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
	n := proc.atime.now()
	ret := fs_link(old, new, proc.cwd.fops.pathi())
	proc.atime.io_time(n)
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
	n := proc.atime.now()
	ret := fs_unlink(path, proc.cwd.fops.pathi())
	proc.atime.io_time(n)
	return ret
}

func sys_gettimeofday(proc *proc_t, timevaln int) int {
	tvalsz := 16
	now := time.Now()
	buf := make([]uint8, tvalsz)
	us := int(now.UnixNano() / 1000)
	writen(buf, 8, 0, us/1e6)
	writen(buf, 8, 8, us%1e6)
	if !proc.usercopy(buf, timevaln) {
		return -EFAULT
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
	if !proc.usercopy(ru, rusagep) {
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
	n := proc.atime.now()
	fsf, err := _fs_open(path, O_CREAT, 0, proc.cwd.fops.pathi(), maj, min)
	proc.atime.io_time(n)
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

	n := proc.atime.now()
	<- time.After(tot)
	proc.atime.sleep_time(n)

	return 0
}

func sys_getpid(proc *proc_t) int {
	return proc.pid
}

func sys_socket(proc *proc_t, domain, typ, proto int) int {
	var sfops fdops_i
	switch {
	case domain == AF_UNIX && typ == SOCK_DGRAM:
		sfops = &sudfops_t{open: 1}
	default:
		return -EINVAL
	}
	file := &fd_t{}
	file.fops = sfops
	fdn := proc.fd_insert(file, FD_READ | FD_WRITE)
	return fdn
}

func copysockaddr(proc *proc_t, san, sl int) ([]uint8, int) {
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
	// to protect the "open" field from racing close()s
	sync.Mutex
}

func (sf *sudfops_t) close() int {
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
	return -ENODEV
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

func (sf *sudfops_t) write(*userbuf_t, bool) (int, int) {
	return 0, -ENODEV
}

func (sf *sudfops_t) lseek(int, int) int {
	return -ENODEV
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

func (sf *sudfops_t) bind(proc *proc_t, sa []uint8) int {
	poff := 2
	path := slicetostr(sa[poff:])
	// try to create the specified file as a special device
	sid := sun_new()
	n := proc.atime.now()
	pi := proc.cwd.fops.pathi()
	fsf, err := _fs_open(path, O_CREAT | O_EXCL, 0, pi, D_SUD, int(sid))
	proc.atime.io_time(n)
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

func (sf *sudfops_t) sendto(proc *proc_t, src *userbuf_t, sa []uint8,
    flags int) (int, int) {
	poff := 2
	if len(sa) <= poff {
		return 0, -EINVAL
	}
	st := &stat_t{}
	st.init()
	path := slicetostr(sa[poff:])
	n := proc.atime.now()
	err := fs_stat(path, st, proc.cwd.fops.pathi())
	proc.atime.io_time(n)
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
	n := proc.atime.now()
	file, err := fs_open(paths, O_RDONLY, 0, proc.cwd.fops.pathi(), 0, 0)
	proc.atime.io_time(n)
	if err != 0 {
		restore()
		return err
	}
	defer func() {
		if file.fops.close() != 0 {
			panic("must succeed")
		}
	}()

	hdata := make([]uint8, 512)
	ub := &userbuf_t{}
	ub.fake_init(hdata)
	n = proc.atime.now()
	ret, err := file.fops.read(ub)
	proc.atime.io_time(n)
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
	numstkpages := 2
	// +1 for the guard page
	stksz := (numstkpages + 1) * PGSIZE
	stackva := proc.unusedva_inner(0, stksz)
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
	words := 3
	buf := make([]uint8, words*8)
	writen(buf, 8, 0, freshtls)
	writen(buf, 8, 8, tlssz)
	writen(buf, 8, 16, t0tls)
	bufdest := stackva - words*8
	tls0addr := bufdest + 2*8
	if !proc.usercopy_inner(buf, bufdest) {
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
		if !proc.usercopy_inner(arg, uva + cnt) {
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

	n := proc.atime.now()
	resp := proc.mywait.reap(wpid)
	proc.atime.sleep_time(n)

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
			if !proc.usercopy(ru, rusagep) {
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

	n := proc.atime.now()
	pi := proc.cwd.fops.pathi()
	newcwd, err := fs_open(path, O_RDONLY | O_DIRECTORY, 0, pi, 0, 0)
	proc.atime.io_time(n)
	if err != 0 {
		return err
	}
	n = proc.atime.now()
	if file_close(proc.cwd) != 0 {
		panic("must succeed")
	}
	proc.atime.io_time(n)
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

func sys_fake(proc *proc_t, n int) int {
	if n != 0 {
		//runtime.Kreset()
		prof.init()
		err := pprof.StartCPUProfile(&prof)
		if err != nil {
			fmt.Printf("%v\n", err)
			return 1
		}
	} else {
		//kns := runtime.Ktime()
		pprof.StopCPUProfile()
		//pprof.WriteHeapProfile(&prof)
		prof.dump()
		//fmt.Printf("K    ns: %v\n", kns)
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
