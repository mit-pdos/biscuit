package main

import "fmt"
//import "math/rand"
import "runtime"
import "runtime/debug"
import "sync/atomic"
import "sync"
import "strings"
import "time"
import "unsafe"

var	numcpus	int = 1
var	bsp_apic_id int

// these functions can only be used when interrupts are cleared
//go:nosplit
func lap_id() int {
	lapaddr := (*[1024]uint32)(unsafe.Pointer(uintptr(0xfee00000)))
	return int(lapaddr[0x20/4] >> 24)
}

const(
	DIVZERO		= 0
	UD		= 6
	GPFAULT		= 13
	PGFAULT		= 14
	TIMER		= 32
	SYSCALL		= 64
	TLBSHOOT	= 70
	PERFMASK 	= 72

	IRQ_BASE	= 32
	IRQ_KBD		= 1
	IRQ_COM1	= 4

	INT_KBD		= IRQ_BASE + IRQ_KBD
	INT_COM1	= IRQ_BASE + IRQ_COM1

	INT_MSI0	= 56
	INT_MSI1	= 57
	INT_MSI2	= 58
	INT_MSI3	= 59
	INT_MSI4	= 60
	INT_MSI5	= 61
	INT_MSI6	= 62
	INT_MSI7	= 63

	IRQ_LAST	= INT_MSI7
)

// initialized by disk attach functions
var IRQ_DISK	int = -1
var INT_DISK	int = -1

var irqs int

// trapstub() cannot do anything that may have side-effects on the runtime
// (like allocate, fmt.Print, or use panic) since trapstub() runs in interrupt
// context and thus may run concurrently with code manipulating the same state.
// since trapstub() runs on the per-cpu interrupt stack, it must be nosplit.
//go:nosplit
func trapstub(tf *[TFSIZE]uintptr) {

	trapno := tf[TF_TRAP]

	// only IRQs come through here now
	if trapno <= TIMER || trapno > IRQ_LAST {
		runtime.Pnum(0x1001)
		for {}
	}

	irqs++
	switch trapno {
	case INT_KBD, INT_COM1:
		runtime.IRQwake(uint(trapno))
		// we need to mask the interrupt on the IOAPIC since my
		// hardware's LAPIC automatically send EOIs to IOAPICS when the
		// LAPIC receives an EOI and does not support disabling these
		// automatic EOI broadcasts (newer LAPICs do). its probably
		// better to disable automatic EOI broadcast and send EOIs to
		// the IOAPICs in the driver (as the code used to when using
		// 8259s).

		// masking the IRQ on the IO APIC must happen before writing
		// EOI to the LAPIC (otherwise the CPU will probably receive
		// another interrupt from the same IRQ). the LAPIC EOI happens
		// in the runtime...
		irqno := int(trapno - IRQ_BASE)
		apic.irq_mask(irqno)
	case uintptr(INT_DISK), INT_MSI0, INT_MSI1, INT_MSI2, INT_MSI3, INT_MSI4, INT_MSI5,
		INT_MSI6, INT_MSI7:
		// MSI dispatch doesn't use the IO APIC, thus no need for
		// irq_mask
		runtime.IRQwake(uint(trapno))
	default:
		// unexpected IRQ
		runtime.Pnum(int(trapno))
		runtime.Pnum(int(tf[TF_RIP]))
		runtime.Pnum(0xbadbabe)
		for {}
	}
}

var ide_int_done       = make(chan bool)

func trap_disk(intn uint) {
	for {
		runtime.IRQsched(intn)

		// is this a disk int?
		if !disk.intr() {
			fmt.Printf("spurious disk int\n")
			return
		}
		ide_int_done <- true
	}
}

func trap_cons(intn uint, ch chan bool) {
	for {
		runtime.IRQsched(intn)
		ch <- true
	}
}

func tfdump(tf *[TFSIZE]int) {
	fmt.Printf("RIP: %#x\n", tf[TF_RIP])
	fmt.Printf("RAX: %#x\n", tf[TF_RAX])
	fmt.Printf("RDI: %#x\n", tf[TF_RDI])
	fmt.Printf("RSI: %#x\n", tf[TF_RSI])
	fmt.Printf("RBX: %#x\n", tf[TF_RBX])
	fmt.Printf("RCX: %#x\n", tf[TF_RCX])
	fmt.Printf("RDX: %#x\n", tf[TF_RDX])
	fmt.Printf("RSP: %#x\n", tf[TF_RSP])
}

type dev_t struct {
	major	int
	minor	int
}

// allocated device major numbers
// internally, biscuit uses device numbers for all special, on-disk files.
const(
	D_CONSOLE int	= 1
	// UNIX domain sockets
	D_SUD 		= 2
	D_SUS 		= 3
	D_DEVNULL	= 4
	D_RAWDISK	= 5
	D_STAT          = 6
	D_FIRST		= D_CONSOLE
	D_LAST		= D_SUS
)

// threads/processes can concurrently call a single fd's methods
type fdops_i interface {
	// fd ops
	close() err_t
	fstat(*stat_t) err_t
	lseek(int, int) (int, err_t)
	mmapi(int, int, bool) ([]mmapinfo_t, err_t)
	pathi() inum_t
	read(*proc_t, userio_i) (int, err_t)
	// reopen() is called with proc_t.fdl is held
	reopen() err_t
	write(*proc_t, userio_i) (int, err_t)
	fullpath() (string, err_t)
	truncate(uint) err_t

	pread(userio_i, int) (int, err_t)
	pwrite(userio_i, int) (int, err_t)

	// socket ops
	// returns fops of new fd, size of connector's address written to user
	// space, and error
	accept(*proc_t, userio_i) (fdops_i, int, err_t)
	bind(*proc_t, []uint8) err_t
	connect(*proc_t, []uint8) err_t
	// listen changes the underlying socket type; thus is returns the new
	// fops.
	listen(*proc_t, int) (fdops_i, err_t)
	sendmsg(p *proc_t, data userio_i, saddr []uint8, cmsg []uint8,
	    flags int) (int, err_t)
	// returns number of bytes read, size of from sock address written,
	// size of ancillary data written, msghdr flags, and error. if no from
	// address or ancillary data is requested, the userio objects have
	// length 0.
	recvmsg(p *proc_t, data userio_i, saddr userio_i,
	    cmsg userio_i, flags int) (int, int, int, msgfl_t, err_t)

	// for poll/select
	// returns the current ready flags. pollone() will only cause the
	// device to send a notification if none of the states being polled are
	// currently true.
	pollone(pollmsg_t) (ready_t, err_t)

	fcntl(*proc_t, int, int) int
	getsockopt(*proc_t, int, userio_i, int) (int, err_t)
	setsockopt(*proc_t, int, int, userio_i, int) err_t
	shutdown(rdone, wdone bool) err_t
}

// this is the new fd_t
type fd_t struct {
	// fops is an interface implemented via a "pointer receiver", thus fops
	// is a reference, not a value
	fops	fdops_i
	perms	int
}

const(
	FD_READ		= 0x1
	FD_WRITE	= 0x2
	FD_CLOEXEC	= 0x4
)

var dummyfops	= &devfops_t{maj: D_CONSOLE, min: 0}

// special fds
var fd_stdin 	= fd_t{fops: dummyfops, perms: FD_READ}
var fd_stdout 	= fd_t{fops: dummyfops, perms: FD_WRITE}
var fd_stderr 	= fd_t{fops: dummyfops, perms: FD_WRITE}

// system-wide limits. the limits of type int are read-only while the
// sysatomic_t limits are incremented/decremented on object
// allocation/deallocation.
type syslimit_t struct {
	// protected by proclock
	sysprocs	int
	// proctected by idmonl lock
	vnodes		int
	// proctected by _allfutex lock
	futexes		int
	// proctected by arptbl lock
	arpents		int
	// proctected by routetbl lock
	routes		int
	// per TCP socket tx/rx segments to remember
	tcpsegs		int
	// socks includes pipes and all TCP connections in TIMEWAIT.
	socks		sysatomic_t
	// total cached dirents
	dirents		sysatomic_t
	// total pipes
	pipes		sysatomic_t
	// additional memory filesystem per-page objects; each file gets one
	// freebie.
	mfspgs		sysatomic_t
	// shared buffer space
	//shared		sysatomic_t
	// bdev blocks
	blocks          int
}

var syslimit = syslimit_t {
	sysprocs:	1e4,
	futexes:	1024,
	arpents:	1024,
	routes:		32,
	tcpsegs:	16,
	socks:		1e5,
	vnodes:		10000, // 1e6,
	dirents:	1 << 20,
	pipes:		1e4,
	// 8GB of block pages
        blocks:         1000, // 100000, // 1 << 21,
}

// a type for system limits that aren't protected by a lock.
type sysatomic_t int64

func (s *sysatomic_t) _aptr() *int64 {
	return (*int64)(unsafe.Pointer(s))
}

// returns false if the limit has been reached.
func (s *sysatomic_t) take() bool {
	return s.taken(1)
}

func (s *sysatomic_t) taken(_n uint) bool {
	n := int64(_n)
	if n < 0 {
		panic("too mighty")
	}
	g := atomic.AddInt64(s._aptr(), -n)
	if g >= 0 {
		return true
	}
	atomic.AddInt64(s._aptr(), n)
	return false
}

func (s *sysatomic_t) give() {
	s.given(1)
}

func (s *sysatomic_t) given(_n uint) {
	n := int64(_n)
	if n < 0 {
		panic("too mighty")
	}
	atomic.AddInt64(s._aptr(), n)
}

// per-process limits
type ulimit_t struct {
	pages	int
	nofile	uint
	novma	uint
	noproc	uint
}

// accnt_t is thread-safe
type accnt_t struct {
	// nanoseconds
	userns		int64
	sysns		int64
	// for getting consistent snapshot of both times; not always needed
	sync.Mutex
}

func (a *accnt_t) utadd(delta int) {
	atomic.AddInt64(&a.userns, int64(delta))
}

func (a *accnt_t) systadd(delta int) {
	atomic.AddInt64(&a.sysns, int64(delta))
}

func (a *accnt_t) now() int {
	return int(time.Now().UnixNano())
}

func (a *accnt_t) io_time(since int) {
	d := a.now() - since
	a.systadd(-d)
}

func (a *accnt_t) sleep_time(since int) {
	d := a.now() - since
	a.systadd(-d)
}

func (a *accnt_t) finish(inttime int) {
	a.systadd(a.now() - inttime)
}

func (a *accnt_t) add(n *accnt_t) {
	a.Lock()
	a.userns += n.userns
	a.sysns += n.sysns
	a.Unlock()
}

func (a *accnt_t) fetch() []uint8 {
	a.Lock()
	ru := a.to_rusage()
	a.Unlock()
	return ru
}

func (a *accnt_t) to_rusage() []uint8 {
	words := 4
	ret := make([]uint8, words*8)
	totv := func(nano int64) (int, int) {
		secs := int(nano/1e9)
		usecs := int((nano%1e9)/1000)
		return secs, usecs
	}
	off := 0
	// user timeval
	s, us := totv(a.userns)
	writen(ret, 8, off, s)
	off += 8
	writen(ret, 8, off, us)
	off += 8
	// sys timeval
	s, us = totv(a.sysns)
	writen(ret, 8, off, s)
	off += 8
	writen(ret, 8, off, us)
	off += 8
	return ret
}

// requirements for wait* syscalls (used for processes and threads):
// - wait for a pid that is not my child must fail
// - only one wait for a specific pid may succeed; others must fail
// - wait when there are no children must fail
// - wait for a process should not return thread info and vice versa
type waitst_t struct {
	pid		int
	status		int
	atime		accnt_t
	// true iff the exit status is valid
	valid		bool
}

type wait_t struct {
	sync.Mutex
	pwait		whead_t
	twait		whead_t
	cond		*sync.Cond
	pid		int
}

func (w *wait_t) wait_init(mypid int) {
	w.cond = sync.NewCond(w)
	w.pid = mypid
}

type wlist_t struct {
	next	*wlist_t
	wst	waitst_t
}

type whead_t struct {
	head	*wlist_t
	count	int
}

func (wh *whead_t) wpush(id int) {
	n := &wlist_t{}
	n.wst.pid = id
	n.next = wh.head
	wh.head = n
	wh.count++
}

func (wh *whead_t) wpopvalid() (waitst_t, bool) {
	var prev *wlist_t
	n := wh.head
	for n != nil {
		if n.wst.valid {
			wh.wremove(prev, n)
			return n.wst, true
		}
		prev = n
		n = n.next
	}
	var zw waitst_t
	return zw, false
}

// returns the previous element in the wait status singly-linked list (in order
// to remove the requested element), the requested element, and whether the
// requested element was found.
func (wh *whead_t) wfind(id int) (*wlist_t, *wlist_t, bool) {
	var prev *wlist_t
	ret := wh.head
	for ret != nil {
		if ret.wst.pid == id {
			return prev, ret, true
		}
		prev = ret
		ret = ret.next
	}
	return nil, nil, false
}

func (wh *whead_t) wremove(prev, h *wlist_t) {
	if prev != nil {
		prev.next = h.next
	} else {
		wh.head = h.next
	}
	h.next = nil
	wh.count--
}

// if there are more unreaped child statuses (procs or threads) than noproc,
// _start() returns false and id is not added to the status map.
func (w *wait_t) _start(id int, isproc bool, noproc uint) bool {
	w.Lock()
	defer w.Unlock()
	if uint(w.pwait.count + w.twait.count) > noproc {
		return false
	}
	var wh *whead_t
	if isproc {
		wh = &w.pwait
	} else {
		wh = &w.twait
	}
	wh.wpush(id)
	return true
}

func (w *wait_t) putpid(pid, status int, atime *accnt_t) {
	w._put(pid, status, true, atime)
}

func (w *wait_t) puttid(tid, status int, atime *accnt_t) {
	w._put(tid, status, false, atime)
}

func (w *wait_t) _put(id, status int, isproc bool, atime *accnt_t) {
	w.Lock()
	defer w.Unlock()
	var wh *whead_t
	if isproc {
		wh = &w.pwait
	} else {
		wh = &w.twait
	}
	_, wn, ok := wh.wfind(id)
	if !ok {
		panic("id must exist")
	}
	wn.wst.valid = true
	wn.wst.status = status
	if atime != nil {
		wn.wst.atime.userns += atime.userns
		wn.wst.atime.sysns += atime.sysns
	}
	w.cond.Broadcast()
}

func (w *wait_t) reappid(pid int, noblk bool) (waitst_t, err_t) {
	return w._reap(pid, true, noblk)
}

func (w *wait_t) reaptid(tid int, noblk bool) (waitst_t, err_t) {
	return w._reap(tid, false, noblk)
}

func (w *wait_t) _reap(id int, isproc bool, noblk bool) (waitst_t, err_t) {
	if id == WAIT_MYPGRP {
		panic("no imp")
	}
	var wh *whead_t
	if isproc {
		wh = &w.pwait
	} else {
		wh = &w.twait
	}

	w.Lock()
	defer w.Unlock()
	var zw waitst_t
	for {
		if id == WAIT_ANY {
			// XXXPANIC
			if wh.count < 0 {
				panic("neg childs")
			}
			if wh.count == 0 {
				return zw, -ECHILD
			}
			if ret, ok := wh.wpopvalid(); ok {
				return ret, 0
			}
		} else {
			wp, wn, ok := wh.wfind(id)
			if !ok {
				return zw, -ECHILD
			}
			if wn.wst.valid {
				wh.wremove(wp, wn)
				return wn.wst, 0
			}
		}
		if noblk {
			return zw, 0
		}
		// wait for someone to exit
		w.cond.Wait()
	}
}

type tid_t int

type tnote_t struct {
	alive	bool
}

type threadinfo_t struct {
	notes	map[tid_t]*tnote_t
	sync.Mutex
}

func (t *threadinfo_t) init() {
	t.notes = make(map[tid_t]*tnote_t)
}

type proc_t struct {
	pid		int
	// first thread id
	tid0		tid_t
	name		string

	// waitinfo for my child processes
	mywait		wait_t
	// waitinfo of my parent
	pwait		*wait_t

	// thread tids of this process
	threadi		threadinfo_t

	// lock for vmregion, pmpages, pmap, and p_pmap
	pgfl		sync.Mutex

	vmregion	vmregion_t

	// pmap pages
	pmap		*pmap_t
	p_pmap		pa_t

	// mmap next virtual address hint
	mmapi		int

	// a process is marked doomed when it has been killed but may have
	// threads currently running on another processor
	pgfltaken	bool
	doomed		bool
	exitstatus	int

	fds		[]*fd_t
	// where to start scanning for free fds
	fdstart		int
	// fds, fdstart, nfds protected by fdl
	fdl		sync.Mutex
	// number of valid file descriptors
	nfds		int

	cwd		*fd_t
	// to serialize chdirs
	cwdl		sync.Mutex
	ulim		ulimit_t

	// this proc's rusage
	atime		accnt_t
	// total child rusage
	catime		accnt_t
}

var proclock = sync.Mutex{}
var allprocs = make(map[int]*proc_t, syslimit.sysprocs)
// total number of all threads
var nthreads int64

var pid_cur  int

// returns false if system-wide limit is hit.
func tid_new() (tid_t, bool) {
	proclock.Lock()
	defer proclock.Unlock()
	if nthreads > int64(syslimit.sysprocs) {
		return 0, false
	}
	nthreads++
	pid_cur++
	ret := pid_cur

	return tid_t(ret), true
}

func tid_del() {
	proclock.Lock()
	if nthreads == 0 {
		panic("oh shite")
	}
	nthreads--
	proclock.Unlock()
}

var _deflimits = ulimit_t {
	// mem limit = 128 MB
	pages: (1 << 27) / (1 << 12),
	//nofile: 512,
	nofile: RLIM_INFINITY,
	novma: (1 << 8),
	noproc: (1 << 10),
}

// returns the new proc and success; can fail if the system-wide limit of
// procs/threads has been reached. the parent's fdtable must be locked.
func proc_new(name string, cwd *fd_t, fds []*fd_t) (*proc_t, bool) {
	proclock.Lock()

	if nthreads >= int64(syslimit.sysprocs) {
		proclock.Unlock()
		return nil, false
	}

	nthreads++

	pid_cur++
	np := pid_cur
	pid_cur++
	tid0 := tid_t(pid_cur)
	if _, ok := allprocs[np]; ok {
		panic("pid exists")
	}
	ret := &proc_t{}
	allprocs[np] = ret
	proclock.Unlock()

	ret.name = name
	ret.pid = np
	ret.fds = make([]*fd_t, len(fds))
	ret.fdstart = 3
	for i := range fds {
		if fds[i] == nil {
			continue
		}
		tfd, err := copyfd(fds[i])
		// copying an fd may fail if another thread closes the fd out
		// from under us
		if err == 0 {
			ret.fds[i] = tfd
		}
		ret.nfds++
	}
	ret.cwd = cwd
	if ret.cwd.fops.reopen() != 0 {
		panic("must succeed")
	}
	ret.mmapi = USERMIN
	ret.ulim = _deflimits

	ret.threadi.init()
	ret.tid0 = tid0
	ret._thread_new(tid0)

	ret.mywait.wait_init(ret.pid)
	if !ret.start_thread(ret.tid0) {
		panic("silly noproc")
	}

	return ret, true
}

func proc_check(pid int) (*proc_t, bool) {
	proclock.Lock()
	p, ok := allprocs[pid]
	proclock.Unlock()
	return p, ok
}

func proc_del(pid int) {
	proclock.Lock()
	_, ok := allprocs[pid]
	if !ok {
		panic("bad pid")
	}
	delete(allprocs, pid)
	proclock.Unlock()
}

// an fd table invariant: every fd must have its file field set. thus the
// caller cannot set an fd's file field without holding fdl. otherwise you will
// race with a forking thread when it copies the fd table.
func (p *proc_t) fd_insert(f *fd_t, perms int) (int, bool) {
	p.fdl.Lock()
	a, b := p.fd_insert_inner(f, perms)
	p.fdl.Unlock()
	return a, b
}

func (p *proc_t) fd_insert_inner(f *fd_t, perms int) (int, bool) {

	if uint(p.nfds) >= p.ulim.nofile {
		return -1, false
	}
	// find free fd
	newfd := p.fdstart
	found := false
	for newfd < len(p.fds) {
		if p.fds[newfd] == nil {
			p.fdstart = newfd + 1
			found = true
			break
		}
		newfd++
	}
	if !found {
		// double size of fd table
		ol := len(p.fds)
		nl := 2*ol
		if p.ulim.nofile != RLIM_INFINITY && nl > int(p.ulim.nofile) {
			nl = int(p.ulim.nofile)
			if nl < ol {
				panic("how")
			}
		}
		nfdt := make([]*fd_t, nl, nl)
		copy(nfdt, p.fds)
		p.fds = nfdt
	}
	fdn := newfd
	fd := f
	fd.perms = perms
	if p.fds[fdn] != nil {
		panic(fmt.Sprintf("new fd exists %d", fdn))
	}
	p.fds[fdn] = fd
	if fd.fops == nil {
		panic("wtf!")
	}
	p.nfds++
	return fdn, true
}

// returns the fd numbers and success
func (p *proc_t) fd_insert2(f1 *fd_t, perms1 int,
   f2 *fd_t, perms2 int) (int, int, bool) {
	p.fdl.Lock()
	defer p.fdl.Unlock()
	var fd2 int
	var ok2 bool
	fd1, ok1 := p.fd_insert_inner(f1, perms1)
	if !ok1 {
		goto out
	}
	fd2, ok2 = p.fd_insert_inner(f2, perms2)
	if !ok2 {
		p.fd_del_inner(fd1)
		goto out
	}
	return fd1, fd2, true
out:
	return 0, 0, false
}

// fdn is not guaranteed to be a sane fd
func (p *proc_t) fd_get_inner(fdn int) (*fd_t, bool) {
	if fdn < 0 || fdn >= len(p.fds) {
		return nil, false
	}
	ret := p.fds[fdn]
	ok := ret != nil
	return ret, ok
}

func (p *proc_t) fd_get(fdn int) (*fd_t, bool) {
	p.fdl.Lock()
	ret, ok := p.fd_get_inner(fdn)
	p.fdl.Unlock()
	return ret, ok
}

// fdn is not guaranteed to be a sane fd
func (p *proc_t) fd_del(fdn int) (*fd_t, bool) {
	p.fdl.Lock()
	a, b := p.fd_del_inner(fdn)
	p.fdl.Unlock()
	return a, b
}

func (p *proc_t) fd_del_inner(fdn int) (*fd_t, bool) {
	if fdn < 0 || fdn >= len(p.fds) {
		return nil, false
	}
	ret := p.fds[fdn]
	p.fds[fdn] = nil
	ok := ret != nil
	if ok {
		p.nfds--
		if p.nfds < 0 {
			panic("neg nfds")
		}
		if fdn < p.fdstart {
			p.fdstart = fdn
		}
	}
	return ret, ok
}

// fdn is not guaranteed to be a sane fd. returns the the fd replaced by ofdn
// and whether it exists and needs to be closed, and success.
func (p *proc_t) fd_dup(ofdn, nfdn int) (*fd_t, bool, err_t) {
	if ofdn == nfdn {
		return nil, false, 0
	}

	p.fdl.Lock()
	defer p.fdl.Unlock()

	ofd, ok := p.fd_get_inner(ofdn)
	if !ok {
		return nil, false, -EBADF
	}
	cpy, err := copyfd(ofd)
	if err != 0 {
		return nil, false, err
	}
	cpy.perms &^= FD_CLOEXEC
	rfd, needclose := p.fd_get_inner(nfdn)
	p.fds[nfdn] = cpy

	return rfd, needclose, 0
}

// returns whether the parent's TLB should be flushed and whether the we
// successfully copied the parent's address space.
func (parent *proc_t) vm_fork(child *proc_t, rsp uintptr) (bool, bool) {
	parent.lockassert_pmap()
	// first add kernel pml4 entries
	for _, e := range kents {
		child.pmap[e.pml4slot] = e.entry
	}
	// recursive mapping
	child.pmap[VREC] = child.p_pmap | PTE_P | PTE_W

	failed := false
	doflush := false
	child.vmregion = parent.vmregion.copy()
	parent.vmregion.iter(func(vmi *vminfo_t) {
		start := int(vmi.pgn << PGSHIFT)
		end := start + int(vmi.pglen << PGSHIFT)
		ashared := vmi.mtype == VSANON
		fl, ok := ptefork(child.pmap, parent.pmap, start, end, ashared)
		failed = failed || !ok
		doflush = doflush || fl
	})

	if failed {
		return doflush, false
	}

	// don't mark stack COW since the parent/child will fault their stacks
	// immediately
	vmi, ok := child.vmregion.lookup(rsp)
	// give up if we can't find the stack
	if !ok {
		return doflush, true
	}
	pte, ok := vmi.ptefor(child.pmap, rsp)
	if !ok || *pte & PTE_P == 0 || *pte & PTE_U == 0 {
		return doflush, true
	}
	// sys_pgfault expects pmap to be locked
	child.Lock_pmap()
	perms := uintptr(PTE_U | PTE_W)
	if !sys_pgfault(child, vmi, rsp, perms) {
		return doflush, false
	}
	child.Unlock_pmap()
	vmi, ok = parent.vmregion.lookup(rsp)
	if !ok ||*pte & PTE_P == 0 || *pte & PTE_U == 0 {
		panic("child has stack but not parent")
	}
	pte, ok = vmi.ptefor(parent.pmap, rsp)
	if !ok {
		panic("must exist")
	}
	*pte &^= PTE_COW
	*pte |= PTE_W | PTE_WASCOW

	return true, true
}

// does not increase opencount on fops (vmregion_t.insert does). perms should
// only use PTE_U/PTE_W; the page fault handler will install the correct COW
// flags. perms == 0 means that no mapping can go here (like for guard pages).
func (p *proc_t) _mkvmi(mt mtype_t, start, len int, perms pa_t, foff int,
    fops fdops_i, shared bool) *vminfo_t {
	if len <= 0 {
		panic("bad vmi len")
	}
	if pa_t(start | len) & PGOFFSET != 0 {
		panic("start and len must be aligned")
	}
	// don't specify cow, present etc. -- page fault will handle all that
	pm := PTE_W | PTE_COW | PTE_WASCOW | PTE_PS | PTE_PCD | PTE_P | PTE_U
	if r := perms & pm; r != 0 && r != PTE_U && r != (PTE_W | PTE_U) {
		panic("bad perms")
	}
	ret := &vminfo_t{}
	pgn := uintptr(start) >> PGSHIFT
	pglen := roundup(len, PGSIZE) >> PGSHIFT
	ret.mtype = mt
	ret.pgn = pgn
	ret.pglen = pglen
	ret.perms = uint(perms)
	if mt == VFILE {
		ret.file.foff = foff
		ret.file.mfile = &mfile_t{}
		ret.file.mfile.mfops = fops
		ret.file.mfile.mapcount = pglen
		ret.file.shared = shared
	}
	return ret
}

func (p *proc_t) vmadd_anon(start, len int, perms pa_t) {
	vmi := p._mkvmi(VANON, start, len, perms, 0, nil, false)
	p.vmregion.insert(vmi)
}

func (p *proc_t) vmadd_file(start, len int, perms pa_t, fops fdops_i,
   foff int) {
	vmi := p._mkvmi(VFILE, start, len, perms, foff, fops, false)
	p.vmregion.insert(vmi)
}

func (p *proc_t) vmadd_shareanon(start, len int, perms pa_t) {
	vmi := p._mkvmi(VSANON, start, len, perms, 0, nil, false)
	p.vmregion.insert(vmi)
}

func (p *proc_t) vmadd_sharefile(start, len int, perms pa_t, fops fdops_i,
   foff int) {
	vmi := p._mkvmi(VFILE, start, len, perms, foff, fops, true)
	p.vmregion.insert(vmi)
}

func (p *proc_t) mkuserbuf(userva, len int) *userbuf_t {
	ret := &userbuf_t{}
	ret.ub_init(p, userva, len)
	return ret
}

var ubpool = sync.Pool{New: func() interface{} { return new(userbuf_t) }}

func (p *proc_t) mkuserbuf_pool(userva, len int) *userbuf_t {
	ret := ubpool.Get().(*userbuf_t)
	ret.ub_init(p, userva, len)
	return ret
}

func (p *proc_t) mkfxbuf() *[64]uintptr {
	ret := new([64]uintptr)
	n := uintptr(unsafe.Pointer(ret))
	if n & ((1 << 4) - 1) != 0 {
		panic("not 16 byte aligned")
	}
	*ret = runtime.Fxinit
	return ret
}

// the first return value is true if a present mapping was modified (i.e. need
// to flush TLB). the second return value is false if the page insertion failed
// due to lack of user pages. p_pg's ref count is increased so the caller can
// simply refdown()
func (p *proc_t) page_insert(va int, p_pg pa_t, perms pa_t,
   vempty bool) (bool, bool) {
	p.lockassert_pmap()
	refup(p_pg)
	pte, err := pmap_walk(p.pmap, va, PTE_U | PTE_W)
	if err != 0 {
		return false, false
	}
	ninval := false
	var p_old pa_t
	if *pte & PTE_P != 0 {
		if vempty {
			panic("pte not empty")
		}
		if *pte & PTE_U == 0 {
			panic("replacing kernel page")
		}
		ninval = true
		p_old = pa_t(*pte & PTE_ADDR)
	}
	*pte = p_pg | perms | PTE_P
	if ninval {
		refdown(p_old)
	}
	return ninval, true
}

func (p *proc_t) page_remove(va int) bool {
	p.lockassert_pmap()
	remmed := false
	pte := pmap_lookup(p.pmap, va)
	if pte != nil && *pte & PTE_P != 0 {
		if *pte & PTE_U == 0 {
			panic("removing kernel page")
		}
		p_old := pa_t(*pte & PTE_ADDR)
		refdown(p_old)
		*pte = 0
		remmed = true
	}
	return remmed
}

func (p *proc_t) pgfault(tid tid_t, fa, ecode uintptr) bool {
	p.Lock_pmap()
	defer p.Unlock_pmap()
	vmi, ok := p.vmregion.lookup(fa)
	if !ok {
		return false
	}
	ret := sys_pgfault(p, vmi, fa, ecode)
	return ret
}

// flush TLB on all CPUs that may have this processes' pmap loaded
func (p *proc_t) tlbflush() {
	// this flushes the TLB for now
	p.tlbshoot(0, 2)
}

func (p *proc_t) tlbshoot(startva uintptr, pgcount int) {
	if pgcount == 0 {
		return
	}
	p.lockassert_pmap()
	// fast path: the pmap is loaded in exactly one CPU's cr3, and it
	// happens to be this CPU. we detect that one CPU has the pmap loaded
	// by a pmap ref count == 2 (1 for proc_t ref, 1 for CPU).
	p_pmap := p.p_pmap
	refp, _ := _refaddr(p_pmap)
	if runtime.Condflush(refp, uintptr(p_pmap), startva, pgcount) {
		return
	}
	// slow path, must send TLB shootdowns
	tlb_shootdown(uintptr(p.p_pmap), startva, pgcount)
}

func (p *proc_t) resched(tid tid_t, n *tnote_t) bool {
	talive := n.alive
	if talive && p.doomed {
		// although this thread is still alive, the process should
		// terminate
		reap_doomed(p, tid)
		return false
	}
	return talive
}

func (p *proc_t) run(tf *[TFSIZE]uintptr, tid tid_t) {
	p.threadi.Lock()
	mynote, ok := p.threadi.notes[tid]
	p.threadi.Unlock()
	// each thread removes itself from threadi.notes; thus mynote must
	// exist
	if !ok {
		panic("note must exist")
	}

	fastret := false
	// could allocate fxbuf lazily
	fxbuf := p.mkfxbuf()
	for p.resched(tid, mynote) {
		// for fast syscalls, we restore little state. thus we must
		// distinguish between returning to the user program after it
		// was interrupted by a timer interrupt/CPU exception vs a
		// syscall.
		refp, _ := _refaddr(p.p_pmap)
		intno, aux, op_pmap, odec := runtime.Userrun(tf, fxbuf,
		    uintptr(p.p_pmap), fastret, refp)
		fastret = false
		switch intno {
		case SYSCALL:
			// fast return doesn't restore the registers used to
			// specify the arguments for libc _entry(), so do a
			// slow return when returning from sys_execv().
			sysno := tf[TF_RAX]
			if sysno != SYS_EXECV {
				fastret = true
			}
			tf[TF_RAX] = uintptr(syscall(p, tid, tf))
		case TIMER:
			//fmt.Printf(".")
			runtime.Gosched()
		case PGFAULT:
			faultaddr := uintptr(aux)
			if !p.pgfault(tid, faultaddr, tf[TF_ERROR]) {
				fmt.Printf("*** fault *** %v: addr %x, " +
				    "rip %x. killing...\n", p.name, faultaddr,
				    tf[TF_RIP])
				sys_exit(p, tid, SIGNALED | mkexitsig(11))
			}
		case DIVZERO, GPFAULT, UD:
			fmt.Printf("%s -- TRAP: %v, RIP: %x\n", p.name, intno,
			    tf[TF_RIP])
			sys_exit(p, tid, SIGNALED | mkexitsig(4))
		case TLBSHOOT, PERFMASK, INT_KBD, INT_COM1, INT_DISK, INT_MSI0,
		    INT_MSI1, INT_MSI2, INT_MSI3, INT_MSI4, INT_MSI5, INT_MSI6,
		    INT_MSI7:
			// XXX: shouldn't interrupt user program execution...
		default:
			panic(fmt.Sprintf("weird trap: %d", intno))
		}
		// did we switch pmaps? if so, the old pmap may need to be
		// freed.
		if odec {
			dec_pmap(pa_t(op_pmap))
		}
	}
	tid_del()
}

func (p *proc_t) sched_add(tf *[TFSIZE]uintptr, tid tid_t) {
	go p.run(tf, tid)
}

func (p *proc_t) _thread_new(t tid_t) {
	p.threadi.Lock()
	p.threadi.notes[t] = &tnote_t{alive: true}
	p.threadi.Unlock()
}

func (p *proc_t) thread_new() (tid_t, bool) {
	ret, ok := tid_new()
	if !ok {
		return 0, false
	}
	p._thread_new(ret)
	return ret, true
}

// undo thread_new(); sched_add() must not have been called on t.
func (p *proc_t) thread_undo(t tid_t) {
	tid_del()

	p.threadi.Lock()
	delete(p.threadi.notes, t)
	p.threadi.Unlock()
}

func (p *proc_t) thread_count() int {
	p.threadi.Lock()
	ret := len(p.threadi.notes)
	p.threadi.Unlock()
	return ret
}

// terminate a single thread
func (p *proc_t) thread_dead(tid tid_t, status int, usestatus bool) {
	// XXX exit process if thread is thread0, even if other threads exist
	p.threadi.Lock()
	ti := &p.threadi
	mynote, ok := ti.notes[tid]
	if !ok {
		panic("note must exist")
	}
	mynote.alive = false
	delete(ti.notes, tid)
	destroy := len(ti.notes) == 0

	if usestatus {
		p.exitstatus = status
	}
	p.threadi.Unlock()

	// update rusage user time
	// XXX
	utime := 42
	p.atime.utadd(utime)

	// put thread status in this process's wait info; threads don't have
	// rusage for now.
	p.mywait.puttid(int(tid), status, nil)

	if destroy {
		p.terminate()
	}
	//tid_del()
}

func (p *proc_t) doomall() {
	p.doomed = true
}

func pmfree(pml4 *pmap_t, start, end uintptr) {
	for i := start; i < end; {
		pg, slot := pmap_pgtbl(pml4, int(i), false, 0)
		if pg == nil {
			// this level is not mapped; skip to the next va that
			// may have a mapping at this level
			i += (1 << 21)
			i &^= (1 << 21) - 1
			continue
		}
		tofree := pg[slot:]
		left := (end - i) >> PGSHIFT
		if left < uintptr(len(tofree)) {
			tofree = tofree[:left]
		}
		for idx, p_pg := range tofree {
			if p_pg & PTE_P != 0 {
				if p_pg & PTE_U == 0 {
					panic("kernel pages in vminfo?")
				}
				refdown(p_pg & PTE_ADDR)
				tofree[idx] = 0
			}
		}
		i += uintptr(len(tofree)) << PGSHIFT
	}
}

// don't forget: there are two places where pmaps/memory are free'd:
// proc_t.terminate() and exec.
func _uvmfree(pmg *pmap_t, p_pmap pa_t, vmr *vmregion_t) {
	vmr.iter(func(vmi *vminfo_t) {
		start := uintptr(vmi.pgn << PGSHIFT)
		end := start + uintptr(vmi.pglen << PGSHIFT)
		pmfree(pmg, start, end)
	})
}

func (p *proc_t) uvmfree() {
	_uvmfree(p.pmap, p.p_pmap, &p.vmregion)
	// dec_pmap could free the pmap itself. thus it must come after
	// _uvmfree.
	dec_pmap(p.p_pmap)
	// close all open mmap'ed files
	p.vmregion.clear()
}

// decrease ref count of pml4, freeing it if no CPUs have it loaded into cr3.
func dec_pmap(p_pmap pa_t) {
	_phys_put(&physmem.pmaps, p_pmap)
}

// terminate a process. must only be called when the process has no more
// running threads.
func (p *proc_t) terminate() {
	if p.pid == 1 {
		panic("killed init")
	}

	p.threadi.Lock()
	ti := &p.threadi
	if len(ti.notes) != 0 {
		panic("terminate, but threads alive")
	}
	p.threadi.Unlock()

	// close open fds
	p.fdl.Lock()
	for i := range p.fds {
		if p.fds[i] == nil {
			continue
		}
		close_panic(p.fds[i])
	}
	p.fdl.Unlock()
	close_panic(p.cwd)

	p.mywait.pid = 1
	proc_del(p.pid)

	// free all user pages in the pmap. the last CPU to call dec_pmap on
	// the proc's pmap will free the pmap itself. freeing the user pages is
	// safe since we know that all user threads are dead and thus no CPU
	// will try to access user mappings. however, any CPU may access kernel
	// mappings via this pmap.
	p.uvmfree()

	// send status to parent
	if p.pwait == nil {
		panic("nil pwait")
	}

	// combine total child rusage with ours, send to parent
	na := accnt_t{userns: p.atime.userns, sysns: p.atime.sysns}
	// calling na.add() makes the compiler allocate na in the heap! escape
	// analysis' fault?
	//na.add(&p.catime)
	na.userns += p.catime.userns
	na.sysns += p.catime.sysns

	// put process exit status to parent's wait info
	p.pwait.putpid(p.pid, p.exitstatus, &na)
	// remove pointer to parent to prevent deep fork trees from consuming
	// unbounded memory.
	p.pwait = nil
}

func (p *proc_t) Lock_pmap() {
	// useful for finding deadlock bugs with one cpu
	//if p.pgfltaken {
	//	panic("double lock")
	//}
	p.pgfl.Lock()
	p.pgfltaken = true
}

func (p *proc_t) Unlock_pmap() {
	p.pgfltaken = false
	p.pgfl.Unlock()
}

func (p *proc_t) lockassert_pmap() {
	if !p.pgfltaken {
		panic("pgfl lock must be held")
	}
}

func (p *proc_t) userdmap8_inner(va int, k2u bool) ([]uint8, bool) {
	p.lockassert_pmap()

	voff := va & int(PGOFFSET)
	uva := uintptr(va)
	vmi, ok := p.vmregion.lookup(uva)
	if !ok {
		return nil, false
	}
	pte, ok := vmi.ptefor(p.pmap, uva)
	if !ok {
		return nil, false
	}
	ecode := uintptr(PTE_U)
	needfault := true
	isp := *pte & PTE_P != 0
	if k2u {
		ecode |= uintptr(PTE_W)
		// XXX how to distinguish between user asking kernel to write
		// to read-only page and kernel writing a page mapped read-only
		// to user? (exec args)

		//isw := *pte & PTE_W != 0
		//if isp && isw {
		iscow := *pte & PTE_COW != 0
		if isp && !iscow {
			needfault = false
		}
	} else {
		if isp {
			needfault = false
		}
	}

	if needfault {
		if !sys_pgfault(p, vmi, uva, ecode) {
			return nil, false
		}
	}

	pg := dmap(*pte & PTE_ADDR)
	bpg := pg2bytes(pg)
	return bpg[voff:], true
}

// _userdmap8 and userdmap8r functions must only be used if concurrent
// modifications to the proc_t's address space is impossible.
func (p *proc_t) _userdmap8(va int, k2u bool) ([]uint8, bool) {
	p.Lock_pmap()
	ret, ok := p.userdmap8_inner(va, k2u)
	p.Unlock_pmap()
	return ret, ok
}

func (p *proc_t) userdmap8r(va int) ([]uint8, bool) {
	return p._userdmap8(va, false)
}

func (p *proc_t) usermapped(va, n int) bool {
	p.Lock_pmap()
	defer p.Unlock_pmap()

	_, ok := p.vmregion.lookup(uintptr(va))
	return ok
}

func (p *proc_t) userreadn(va, n int) (int, bool) {
	p.Lock_pmap()
	a, b := p.userreadn_inner(va, n)
	p.Unlock_pmap()
	return a, b
}

func (p *proc_t) userreadn_inner(va, n int) (int, bool) {
	p.lockassert_pmap()
	if n > 8 {
		panic("large n")
	}
	var ret int
	var src []uint8
	var ok bool
	for i := 0; i < n; i += len(src) {
		src, ok = p.userdmap8_inner(va + i, false)
		if !ok {
			return 0, false
		}
		l := n - i
		if len(src) < l {
			l = len(src)
		}
		v := readn(src, l, 0)
		ret |= v << (8*uint(i))
	}
	return ret, true
}

func (p *proc_t) userwriten(va, n, val int) bool {
	if n > 8 {
		panic("large n")
	}
	p.Lock_pmap()
	defer p.Unlock_pmap()
	var dst []uint8
	for i := 0; i < n; i += len(dst) {
		v := val >> (8*uint(i))
		t, ok := p.userdmap8_inner(va + i, true)
		dst = t
		if !ok {
			return false
		}
		writen(dst, n - i, 0, v)
	}
	return true
}

// first ret value is the string from user space
// second ret value is whether or not the string is mapped
// third ret value is whether the string length is less than lenmax
func (p *proc_t) userstr(uva int, lenmax int) (string, bool, bool) {
	if lenmax < 0 {
		return "", false, false
	}
	p.Lock_pmap()
	defer p.Unlock_pmap()
	i := 0
	var s string
	for {
		str, ok := p.userdmap8_inner(uva + i, false)
		if !ok {
			return "", false, false
		}
		for j, c := range str {
			if c == 0 {
				s = s + string(str[:j])
				return s, true, false
			}
		}
		s = s + string(str)
		i += len(str)
		if len(s) >= lenmax {
			return "", true, true
		}
	}
}

func (p *proc_t) usertimespec(va int) (time.Duration, time.Time, err_t) {
	secs, ok1 := p.userreadn(va, 8)
	nsecs, ok2 := p.userreadn(va + 8, 8)
	var zt time.Time
	if !ok1 || !ok2 {
		return 0, zt, -EFAULT
	}
	if secs < 0 || nsecs < 0 {
		return 0, zt, -EINVAL
	}
	tot := time.Duration(secs) * time.Second
	tot += time.Duration(nsecs) * time.Nanosecond
	t := time.Unix(int64(secs), int64(nsecs))
	return tot, t, 0
}

func (p *proc_t) userargs(uva int) ([]string, bool) {
	if uva == 0 {
		return nil, true
	}
	isnull := func(cptr []uint8) bool {
		for _, b := range cptr {
			if b != 0 {
				return false
			}
		}
		return true
	}
	ret := make([]string, 0)
	argmax := 64
	addarg := func(cptr []uint8) bool {
		if len(ret) > argmax {
			return false
		}
		var uva int
		// cptr is little-endian
		for i, b := range cptr {
			uva = uva | int(uint(b)) << uint(i*8)
		}
		lenmax := 128
		str, ok, long := p.userstr(uva, lenmax)
		if !ok || long {
			return false
		}
		ret = append(ret, str)
		return true
	}
	uoff := 0
	psz := 8
	done := false
	curaddr := make([]uint8, 0, 8)
	for !done {
		ptrs, ok := p.userdmap8r(uva + uoff)
		if !ok {
			return nil, false
		}
		for _, ab := range ptrs {
			curaddr = append(curaddr, ab)
			if len(curaddr) == psz {
				if isnull(curaddr) {
					done = true
					break
				}
				if !addarg(curaddr) {
					return nil, false
				}
				curaddr = curaddr[0:0]
			}
		}
		uoff += len(ptrs)
	}
	return ret, true
}

// copies src to the user virtual address uva. may copy part of src if uva +
// len(src) is not mapped
func (p *proc_t) k2user(src []uint8, uva int) bool {
	p.Lock_pmap()
	ret := p.k2user_inner(src, uva)
	p.Unlock_pmap()
	return ret
}

func (p *proc_t) k2user_inner(src []uint8, uva int) bool {
	p.lockassert_pmap()
	cnt := 0
	l := len(src)
	for cnt != l {
		dst, ok := p.userdmap8_inner(uva + cnt, true)
		if !ok {
			return false
		}
		ub := len(src)
		if ub > len(dst) {
			ub = len(dst)
		}
		copy(dst, src)
		src = src[ub:]
		cnt += ub
	}
	return true
}

// copies len(dst) bytes from userspace address uva to dst
func (p *proc_t) user2k(dst []uint8, uva int) bool {
	p.Lock_pmap()
	ret := p.user2k_inner(dst, uva)
	p.Unlock_pmap()
	return ret
}

func (p *proc_t) user2k_inner(dst []uint8, uva int) bool {
	p.lockassert_pmap()
	cnt := 0
	for len(dst) != 0 {
		src, ok := p.userdmap8_inner(uva + cnt, false)
		if !ok {
			return false
		}
		did := copy(dst, src)
		dst = dst[did:]
		cnt += did
	}
	return true
}

func (p *proc_t) unusedva_inner(startva, len int) int {
	p.lockassert_pmap()
	if len < 0 || len > 1 << 48 {
		panic("weird len")
	}
	startva = rounddown(startva, PGSIZE)
	if startva < USERMIN {
		startva = USERMIN
	}
	_ret, _l := p.vmregion.empty(uintptr(startva), uintptr(len))
	ret := int(_ret)
	l := int(_l)
	if startva > ret && startva < ret + l {
		ret = startva
	}
	return ret
}

// returns false if the number of running threads or unreaped child statuses is
// larger than noproc.
func (p *proc_t) start_proc(pid int) bool {
	return p.mywait._start(pid, true, p.ulim.noproc)
}

// returns false if the number of running threads or unreaped child statuses is
// larger than noproc.
func (p *proc_t) start_thread(t tid_t) bool {
	return p.mywait._start(int(t), false, p.ulim.noproc)
}

// interface for reading/writing from user space memory either via a pointer
// and length or an array of pointers and lengths (iovec)
type userio_i interface {
	// copy src to user memory
	uiowrite(src []uint8) (int, err_t)
	// copy user memory to dst
	uioread(dst []uint8) (int, err_t)
	// returns the number of unwritten/unread bytes remaining
	remain() int
	// the total buffer size
	totalsz() int
}

// a userio_i type that copies nothing. useful as an argument to {send,recv}msg
// when no from/to address or ancillary data is requested.
type _nilbuf_t struct {
}


func (nb *_nilbuf_t) uiowrite(src []uint8) (int, err_t) {
	return 0, 0
}

func (nb *_nilbuf_t) uioread(dst []uint8) (int, err_t) {
	return 0, 0
}

func (nb *_nilbuf_t) remain() int {
	return 0
}

func (nb *_nilbuf_t) totalsz() int {
	return 0
}

var zeroubuf = &_nilbuf_t{}

// helper type which kernel code can use as userio_i, but is actually a kernel
// buffer (i.e. reading an ELF header from the file system for exec(2)).
type fakeubuf_t struct {
	fbuf	[]uint8
	off	int
	len	int
}

func (fb *fakeubuf_t) fake_init(buf []uint8) {
	fb.fbuf = buf
	fb.len = len(fb.fbuf)
}

func (fb *fakeubuf_t) remain() int {
	return len(fb.fbuf)
}

func (fb *fakeubuf_t) totalsz() int {
	return fb.len
}

func (fb *fakeubuf_t) _tx(buf []uint8, tofbuf bool) (int, err_t) {
	var c int
	if tofbuf {
		c = copy(fb.fbuf, buf)
	} else {
		c = copy(buf, fb.fbuf)
	}
	fb.fbuf = fb.fbuf[c:]
	return c, 0
}

func (fb *fakeubuf_t) uioread(dst []uint8) (int, err_t) {
	return fb._tx(dst, false)
}

func (fb *fakeubuf_t) uiowrite(src []uint8) (int, err_t) {
	return fb._tx(src, true)
}

// a helper object for read/writing from userspace memory. virtual address
// lookups and reads/writes to those addresses must be atomic with respect to
// page faults.
type userbuf_t struct {
	userva	int
	len	int
	// 0 <= off <= len
	off	int
	proc	*proc_t
}

func (ub *userbuf_t) ub_init(p *proc_t, uva, len int) {
	// XXX fix signedness
	if len < 0 {
		panic("negative length")
	}
	if len >= 1 << 39 {
		fmt.Printf("suspiciously large user buffer (%v)\n", len)
	}
	ub.userva = uva
	ub.len = len
	ub.off = 0
	ub.proc = p
}

func (ub *userbuf_t) remain() int {
	return ub.len - ub.off
}

func (ub *userbuf_t) totalsz() int {
	return ub.len
}
func (ub *userbuf_t) uioread(dst []uint8) (int, err_t) {
	ub.proc.Lock_pmap()
	a, b := ub._tx(dst, false)
	ub.proc.Unlock_pmap()
	return a, b
}

func (ub *userbuf_t) uiowrite(src []uint8) (int, err_t) {
	ub.proc.Lock_pmap()
	a, b := ub._tx(src, true)
	ub.proc.Unlock_pmap()
	return a, b
}

// copies the min of either the provided buffer or ub.len. returns number of
// bytes copied and error.
func (ub *userbuf_t) _tx(buf []uint8, write bool) (int, err_t) {
	ret := 0
	for len(buf) != 0 && ub.off != ub.len {
		va := ub.userva + ub.off
		ubuf, ok := ub.proc.userdmap8_inner(va, write)
		if !ok {
			return ret, -EFAULT
		}
		end := ub.off + len(ubuf)
		if end > ub.len {
			left := ub.len - ub.off
			ubuf = ubuf[:left]
		}
		var c int
		if write {
			c = copy(ubuf, buf)
		} else {
			c = copy(buf, ubuf)
		}
		buf = buf[c:]
		ub.off += c
		ret += c
	}
	return ret, 0
}

type _iove_t struct {
	uva	uint
	sz	int
}

type useriovec_t struct {
	iovs	[]_iove_t
	tsz	int
	proc	*proc_t
}

func (iov *useriovec_t) iov_init(proc *proc_t, iovarn uint, niovs int) err_t {
	if niovs > 10 {
		fmt.Printf("many iovecs\n")
		return -EINVAL
	}
	iov.tsz = 0
	iov.iovs = make([]_iove_t, niovs)
	iov.proc = proc

	proc.Lock_pmap()
	defer proc.Unlock_pmap()
	for i := range iov.iovs {
		elmsz := uint(16)
		va := iovarn + uint(i)*elmsz
		dstva, ok1 := proc.userreadn_inner(int(va), 8)
		sz, ok2    := proc.userreadn_inner(int(va) + 8, 8)
		if !ok1 || !ok2 {
			return -EFAULT
		}
		iov.iovs[i].uva = uint(dstva)
		iov.iovs[i].sz = sz
		iov.tsz += sz
	}
	return 0
}

func (iov *useriovec_t) remain() int {
	ret := 0
	for i := range iov.iovs {
		ret += iov.iovs[i].sz
	}
	return ret
}

func (iov *useriovec_t) totalsz() int {
	return iov.tsz
}

func (iov *useriovec_t) _tx(buf []uint8, touser bool) (int, err_t) {
	ub := &userbuf_t{}
	did := 0
	for len(buf) > 0 && len(iov.iovs) > 0 {
		ciov := &iov.iovs[0]
		ub.ub_init(iov.proc, int(ciov.uva), ciov.sz)
		var c int
		var err err_t
		if touser {
			c, err = ub._tx(buf, true)
		} else {
			c, err = ub._tx(buf, false)
		}
		ciov.uva += uint(c)
		ciov.sz -= c
		if ciov.sz == 0 {
			iov.iovs = iov.iovs[1:]
		}
		buf = buf[c:]
		did += c
		if err != 0 {
			return did, err
		}
	}
	return did, 0
}

func (iov *useriovec_t) uioread(dst []uint8) (int, err_t) {
	iov.proc.Lock_pmap()
	a, b := iov._tx(dst, false)
	iov.proc.Unlock_pmap()
	return a, b
}

func (iov *useriovec_t) uiowrite(src []uint8) (int, err_t) {
	iov.proc.Lock_pmap()
	a, b := iov._tx(src, true)
	iov.proc.Unlock_pmap()
	return a, b
}

// a circular buffer that is read/written by userspace. not thread-safe -- it
// is intended to be used by one daemon.
type circbuf_t struct {
	buf	[]uint8
	bufsz	int
	// XXX uint
	head	int
	tail	int
	p_pg	pa_t
}

// may fail to allocate a page for the buffer. when cb's life is over, someone
// must free the buffer page by calling cb_release().
func (cb *circbuf_t) cb_init(sz int) err_t {
	bufmax := int(PGSIZE)
	if sz <= 0 || sz > bufmax {
		panic("bad circbuf size")
	}
	cb.bufsz = sz
	cb.head, cb.tail = 0, 0
	// lazily allocated the buffers. it is easier to handle an error at the
	// time of read or write instead of during the initialization of the
	// object using a circbuf.
	return 0
}

// provide the page for the buffer explicitly; useful for guaranteeing that
// read/writes won't fail to allocate memory.
func (cb *circbuf_t) cb_init_phys(v []uint8, p_pg pa_t) {
	refup(p_pg)
	cb.p_pg = p_pg
	cb.buf = v
	cb.bufsz = len(cb.buf)
	cb.head, cb.tail = 0, 0
}

func (cb *circbuf_t) cb_release() {
	if cb.buf == nil {
		return
	}
	refdown(cb.p_pg)
	cb.p_pg = 0
	cb.buf = nil
	cb.head, cb.tail = 0, 0
}

func (cb *circbuf_t) cb_ensure() err_t {
	if cb.buf != nil {
		return 0
	}
	if cb.bufsz == 0 {
		panic("not initted")
	}
	pg, p_pg, ok := refpg_new_nozero()
	if !ok {
		return -ENOMEM
	}
	bpg := pg2bytes(pg)[:]
	bpg = bpg[:cb.bufsz]
	cb.cb_init_phys(bpg, p_pg)
	return 0
}

func (cb *circbuf_t) full() bool {
	return cb.head - cb.tail == cb.bufsz
}

func (cb *circbuf_t) empty() bool {
	return cb.head == cb.tail
}

func (cb *circbuf_t) left() int {
	used := cb.head - cb.tail
	rem := cb.bufsz - used
	return rem
}

func (cb *circbuf_t) used() int {
	used := cb.head - cb.tail
	return used
}

func (cb *circbuf_t) copyin(src userio_i) (int, err_t) {
	if err := cb.cb_ensure(); err != 0 {
		return 0, err
	}
	if cb.full() {
		return 0, 0
	}
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	c := 0
	// wraparound?
	if ti <= hi {
		dst := cb.buf[hi:]
		wrote, err := src.uioread(dst)
		if err != 0 {
			return 0, err
		}
		if wrote != len(dst) {
			cb.head += wrote
			return wrote, 0
		}
		c += wrote
		hi = (cb.head + wrote) % cb.bufsz
	}
	// XXXPANIC
	if hi > ti {
		panic("wut?")
	}
	dst := cb.buf[hi:ti]
	wrote, err := src.uioread(dst)
	c += wrote
	if err != 0 {
		return c, err
	}
	cb.head += c
	return c, 0
}

func (cb *circbuf_t) copyout(dst userio_i) (int, err_t) {
	return cb.copyout_n(dst, 0)
}

func (cb *circbuf_t) copyout_n(dst userio_i, max int) (int, err_t) {
	if err := cb.cb_ensure(); err != 0 {
		return 0, err
	}
	if cb.empty() {
		return 0, 0
	}
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	c := 0
	// wraparound?
	if hi <= ti {
		src := cb.buf[ti:]
		if max != 0 && max < len(src) {
			src = src[:max]
		}
		wrote, err := dst.uiowrite(src)
		if err != 0 {
			return 0, err
		}
		if wrote != len(src) || wrote == max {
			cb.tail += wrote
			return wrote, 0
		}
		c += wrote
		if max != 0 {
			max -= c
		}
		ti = (cb.tail + wrote) % cb.bufsz
	}
	// XXXPANIC
	if ti > hi {
		panic("wut?")
	}
	src := cb.buf[ti:hi]
	if max != 0 && max < len(src) {
		src = src[:max]
	}
	wrote, err := dst.uiowrite(src)
	if err != 0 {
		return 0, err
	}
	c += wrote
	cb.tail += c
	return c, 0
}

// returns slices referencing the internal circular buffer [head+offset,
// head+offset+sz) which must be outside [tail, head). returns two slices when
// the returned buffer wraps.
// XXX XXX XXX XXX XXX remove arg
func (cb *circbuf_t) _rawwrite(offset, sz int) ([]uint8, []uint8) {
	if cb.buf == nil {
		panic("no lazy allocation for tcp")
	}
	if cb.left() < sz {
		panic("bad size")
	}
	if sz == 0 {
		return nil, nil
	}
	oi := (cb.head + offset) % cb.bufsz
	oe := (cb.head + offset + sz) % cb.bufsz
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	var r1 []uint8
	var r2 []uint8
	if ti <= hi {
		if (oi >= ti && oi < hi) || (oe > ti && oe <= hi) {
			panic("intersects with user data")
		}
		r1 = cb.buf[oi:]
		if len(r1) > sz {
			r1 = r1[:sz]
		} else {
			r2 = cb.buf[:oe]
		}
	} else {
		// user data wraps
		if !(oi >= hi && oi < ti && oe > hi && oe <= ti) {
			panic("intersects with user data")
		}
		r1 = cb.buf[oi:oe]
	}
	return r1, r2
}

// advances head index sz bytes (allowing the bytes to be copied out)
func (cb *circbuf_t) _advhead(sz int) {
	if cb.full() || cb.left() < sz {
		panic("advancing full cb")
	}
	cb.head += sz
}

// returns slices referencing the circular buffer [tail+offset, tail+offset+sz)
// which must be inside [tail, head). returns two slices when the returned
// buffer wraps.
func (cb *circbuf_t) _rawread(offset int) ([]uint8, []uint8) {
	if cb.buf == nil {
		panic("no lazy allocation for tcp")
	}
	oi := (cb.tail + offset) % cb.bufsz
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	var r1 []uint8
	var r2 []uint8
	if ti < hi {
		if oi >= hi || oi < ti {
			panic("outside user data")
		}
		r1 = cb.buf[oi:hi]
	} else {
		if oi >= hi && oi < ti {
			panic("outside user data")
		}
		tlen := len(cb.buf[ti:])
		if tlen > offset {
			r1 = cb.buf[oi:]
			r2 = cb.buf[:hi]
		} else {
			roff := offset - tlen
			r1 = cb.buf[roff:hi]
		}
	}
	return r1, r2
}

// advances head index sz bytes (allowing the bytes to be copied out)
func (cb *circbuf_t) _advtail(sz int) {
	if sz != 0 && (cb.empty() || cb.used() < sz) {
		panic("advancing empty cb")
	}
	cb.tail += sz
}

type passfd_t struct {
	cb	[]*fd_t
	inum	uint
	cnum	uint
}

func (pf *passfd_t) add(nfd *fd_t) bool {
	if pf.cb == nil {
		pf.cb = make([]*fd_t, 10)
	}
	l := uint(len(pf.cb))
	if pf.inum - pf.cnum == l {
		return false
	}
	pf.cb[pf.inum % l] = nfd
	pf.inum++
	return true
}

func (pf *passfd_t) take() (*fd_t, bool) {
	l := uint(len(pf.cb))
	if pf.inum == pf.cnum {
		return nil, false
	}
	ret := pf.cb[pf.cnum % l]
	pf.cnum++
	return ret, true
}

func (pf *passfd_t) closeall() {
	for {
		fd, ok := pf.take()
		if !ok {
			break
		}
		fd.fops.close()
	}
}

func cpus_stack_init(apcnt int, stackstart uintptr) {
	for i := 0; i < apcnt; i++ {
		// allocate/map interrupt stack
		kmalloc(stackstart, PTE_W)
		stackstart += PGSIZEW
		assert_no_va_map(kpmap(), stackstart)
		stackstart += PGSIZEW
		// allocate/map NMI stack
		kmalloc(stackstart, PTE_W)
		stackstart += PGSIZEW
		assert_no_va_map(kpmap(), stackstart)
		stackstart += PGSIZEW
	}
}

func cpus_start(ncpu, aplim int) {
	runtime.GOMAXPROCS(1 + aplim)
	apcnt := ncpu - 1

	fmt.Printf("found %v CPUs\n", ncpu)

	if apcnt == 0 {
		fmt.Printf("uniprocessor\n")
		return
	}

	// AP code must be between 0-1MB because the APs are in real mode. load
	// code to 0x8000 (overwriting bootloader)
	mpaddr := pa_t(0x8000)
	mpcode := allbins["mpentry.bin"].data
	c := pa_t(0)
	mpl := pa_t(len(mpcode))
	for c < mpl {
		mppg := dmap8(mpaddr + c)
		did := copy(mppg, mpcode)
		mpcode = mpcode[did:]
		c += pa_t(did)
	}

	// skip mucking with CMOS reset code/warm reset vector (as per the the
	// "universal startup algoirthm") and instead use the STARTUP IPI which
	// is supported by lapics of version >= 1.x. (the runtime panicks if a
	// lapic whose version is < 1.x is found, thus assume their absence).
	// however, only one STARTUP IPI is accepted after a CPUs RESET or INIT
	// pin is asserted, thus we need to send an INIT IPI assert first (it
	// appears someone already used a STARTUP IPI; probably the BIOS).

	lapaddr := 0xfee00000
	pte := pmap_lookup(kpmap(), lapaddr)
	if pte == nil || *pte & PTE_P == 0 || *pte & PTE_PCD == 0 {
		panic("lapaddr unmapped")
	}
	lap := (*[PGSIZE/4]uint32)(unsafe.Pointer(uintptr(lapaddr)))
	icrh := 0x310/4
	icrl := 0x300/4

	ipilow := func(ds int, t int, l int, deliv int, vec int) uint32 {
		return uint32(ds << 18 | t << 15 | l << 14 |
		    deliv << 8 | vec)
	}

	icrw := func(hi uint32, low uint32) {
		// use sync to guarantee order
		atomic.StoreUint32(&lap[icrh], hi)
		atomic.StoreUint32(&lap[icrl], low)
		ipisent := uint32(1 << 12)
		for atomic.LoadUint32(&lap[icrl]) & ipisent != 0 {
		}
	}

	// destination shorthands:
	// 1: self
	// 2: all
	// 3: all but me

	initipi := func(assert bool) {
		vec := 0
		delivmode := 0x5
		level := 1
		trig  := 0
		dshort:= 3
		if !assert {
			trig = 1
			level = 0
			dshort = 2
		}
		hi  := uint32(0)
		low := ipilow(dshort, trig, level, delivmode, vec)
		icrw(hi, low)
	}

	startupipi := func() {
		vec       := int(mpaddr >> 12)
		delivmode := 0x6
		level     := 0x1
		trig      := 0x0
		dshort    := 0x3

		hi := uint32(0)
		low := ipilow(dshort, trig, level, delivmode, vec)
		icrw(hi, low)
	}

	// pass arguments to the ap startup code via secret storage (the old
	// boot loader page at 0x7c00)

	// secret storage layout
	// 0 - e820map
	// 1 - pmap
	// 2 - firstfree
	// 3 - ap entry
	// 4 - gdt
	// 5 - gdt
	// 6 - idt
	// 7 - idt
	// 8 - ap count
	// 9 - stack start
	// 10- proceed

	ss := (*[11]uintptr)(unsafe.Pointer(uintptr(0x7c00)))
	sap_entry := 3
	sgdt      := 4
	sidt      := 6
	sapcnt    := 8
	sstacks   := 9
	sproceed  := 10
	var _dur func(uint)
	_dur = ap_entry
	ss[sap_entry] = **(**uintptr)(unsafe.Pointer(&_dur))
	// sgdt and sidt save 10 bytes
	runtime.Sgdt(&ss[sgdt])
	runtime.Sidt(&ss[sidt])
	atomic.StoreUintptr(&ss[sapcnt], 0)
	// for BSP:
	// 	int stack	[0xa100000000, 0xa100001000)
	// 	guard page	[0xa100001000, 0xa100002000)
	// 	NMI stack	[0xa100002000, 0xa100003000)
	// 	guard page	[0xa100003000, 0xa100004000)
	// for each AP:
	// 	int stack	BSP's + apnum*4*PGSIZE + 0*PGSIZE
	// 	NMI stack	BSP's + apnum*4*PGSIZE + 2*PGSIZE
	stackstart := uintptr(0xa100004000)
	// each ap grabs a unique stack
	atomic.StoreUintptr(&ss[sstacks], stackstart)
	atomic.StoreUintptr(&ss[sproceed], 0)

	dummy := int64(0)
	atomic.CompareAndSwapInt64(&dummy, 0, 10)

	initipi(true)
	// not necessary since we assume lapic version >= 1.x (ie not 82489DX)
	//initipi(false)
	time.Sleep(10*time.Millisecond)

	startupipi()
	time.Sleep(10*time.Millisecond)
	startupipi()

	// wait a while for hopefully all APs to join.
	time.Sleep(500*time.Millisecond)
	apcnt = int(atomic.LoadUintptr(&ss[sapcnt]))
	if apcnt > aplim {
		apcnt = aplim
	}
	set_cpucount(apcnt + 1)

	// actually map the stacks for the CPUs that joined
	cpus_stack_init(apcnt, stackstart)

	// tell the cpus to carry on
	atomic.StoreUintptr(&ss[sproceed], uintptr(apcnt))

	fmt.Printf("done! %v APs found (%v joined)\n", ss[sapcnt], apcnt)
}

// myid is a logical id, not lapic id
//go:nosplit
func ap_entry(myid uint) {

	// myid starts from 1
	runtime.Ap_setup(myid)

	// ints are still cleared. wait for timer int to enter scheduler
	fl := runtime.Pushcli()
	fl |= TF_FL_IF
	runtime.Popcli(fl)
	for {}
}

func set_cpucount(n int) {
	numcpus = n
	runtime.Setncpu(int32(n))
}

func irq_unmask(irq int) {
	apic.irq_unmask(irq)
}

func irq_eoi(irq int) {
	//apic.eoi(irq)
	apic.irq_unmask(irq)
}

func kbd_init() {
	km := make(map[int]byte)
	NO := byte(0)
	tm := []byte{
	    // ty xv6
	    NO,   0x1B, '1',  '2',  '3',  '4',  '5',  '6',  // 0x00
	    '7',  '8',  '9',  '0',  '-',  '=',  '\b', '\t',
	    'q',  'w',  'e',  'r',  't',  'y',  'u',  'i',  // 0x10
	    'o',  'p',  '[',  ']',  '\n', NO,   'a',  's',
	    'd',  'f',  'g',  'h',  'j',  'k',  'l',  ';',  // 0x20
	    '\'', '`',  NO,   '\\', 'z',  'x',  'c',  'v',
	    'b',  'n',  'm',  ',',  '.',  '/',  NO,   '*',  // 0x30
	    NO,   ' ',  NO,   NO,   NO,   NO,   NO,   NO,
	    NO,   NO,   NO,   NO,   NO,   NO,   NO,   '7',  // 0x40
	    '8',  '9',  '-',  '4',  '5',  '6',  '+',  '1',
	    '2',  '3',  '0',  '.',  NO,   NO,   NO,   NO,   // 0x50
	    }

	for i, c := range tm {
		if c != NO {
			km[i] = c
		}
	}
	cons.kbd_int = make(chan bool)
	cons.com_int = make(chan bool)
	cons.reader = make(chan []byte)
	cons.reqc = make(chan int)
	cons.pollc = make(chan pollmsg_t)
	cons.pollret = make(chan ready_t)
	go kbd_daemon(&cons, km)
	irq_unmask(IRQ_KBD)
	irq_unmask(IRQ_COM1)

	// make sure kbd int and com1 int are clear
	for _kready() {
		runtime.Inb(0x60)
	}
	for _comready() {
		runtime.Inb(0x3f8)
	}

	go trap_cons(INT_KBD, cons.kbd_int)
	go trap_cons(INT_COM1, cons.com_int)
}

type cons_t struct {
	kbd_int		chan bool
	com_int		chan bool
	reader		chan []byte
	reqc		chan int
	pollc		chan pollmsg_t
	pollret		chan ready_t
}

var cons	= cons_t{}

func _comready() bool {
	com1ctl := uint16(0x3f8 + 5)
	b := runtime.Inb(com1ctl)
	if b & 0x01 == 0 {
		return false
	}
	return true
}

func _kready() bool {
	ibf := uint(1 << 0)
	st := runtime.Inb(0x64)
	if st & ibf == 0 {
		//panic("no kbd data?")
		return false
	}
	return true
}

func netdump() {
	fmt.Printf("net dump\n")
	tcpcons.l.Lock()
	fmt.Printf("tcp table len: %v\n", len(tcpcons.econns))
	//for _, tcb := range tcpcons.econns {
	//	tcb.l.Lock()
	//	//if tcb.state == TIMEWAIT {
	//	//	tcb.l.Unlock()
	//	//	continue
	//	//}
	//	fmt.Printf("%v:%v -> %v:%v: %s\n",
	//	    ip2str(tcb.lip), tcb.lport,
	//	    ip2str(tcb.rip), tcb.rport,
	//	    statestr[tcb.state])
	//	tcb.l.Unlock()
	//}
	tcpcons.l.Unlock()
}

func loping() {
	fmt.Printf("POING\n")
	sip, dip, err := routetbl.lookup(ip4_t(0x7f000001))
	if err != 0 {
		panic("error")
	}
	dmac, err := arp_resolve(sip, dip)
	if err != 0 {
		panic("error")
	}
	nic, ok := nic_lookup(sip)
	if !ok {
		panic("not ok")
	}
	pkt := &icmppkt_t{}
	data := make([]uint8, 8)
	writen(data, 8, 0, int(time.Now().UnixNano()))
	pkt.init(nic.lmac(), dmac, sip, dip, 8, data)
	pkt.ident = 0
	pkt.seq = 0
	pkt.crc()
	sgbuf := [][]uint8{pkt.hdrbytes(), data}
	nic.tx_ipv4(sgbuf)
}

func sizedump() {
	is := unsafe.Sizeof(int(0))
	condsz := unsafe.Sizeof(sync.Cond{})

	//pollersz := unsafe.Sizeof(pollers_t{}) + 10*unsafe.Sizeof(pollmsg_t{})
	var tf [TFSIZE]uintptr
	var fx [64]uintptr
	tfsz := unsafe.Sizeof(tf)
	fxsz := unsafe.Sizeof(fx)
	waitsz := uintptr(1e9)
	tnotesz := is
	timer := uintptr(2*8 + 8*8)
	polls := unsafe.Sizeof(pollers_t{}) + 10*(unsafe.Sizeof(pollmsg_t{}) + timer)
	fdsz := unsafe.Sizeof(fd_t{})
	mfile := unsafe.Sizeof(mfile_t{})
	// add fops, pollers_t, Conds

	fmt.Printf("in bytes\n")
	fmt.Printf("ARP rec: %v + 1map\n", unsafe.Sizeof(arprec_t{}))
	fmt.Printf("dentry : %v\n", unsafe.Sizeof(dc_rbn_t{}))
	fmt.Printf("futex  : %v + stack\n", unsafe.Sizeof(futex_t{}))
	fmt.Printf("route  : %v + 1map\n", unsafe.Sizeof(rtentry_t{}) + is)

	// XXX account for block and inode cache

	// dirtyarray := uintptr(8)
	//fmt.Printf("mfs    : %v\n", uintptr(2*8 + dirtyarray) +
	//	unsafe.Sizeof(frbn_t{}) + unsafe.Sizeof(pginfo_t{}))

	fmt.Printf("vnode  : %v + 1map\n", unsafe.Sizeof(imemnode_t{}) +
		unsafe.Sizeof(bdev_block_t{}) + 512 + condsz +
		unsafe.Sizeof(fsfops_t{}))
	fmt.Printf("pipe   : %v\n", unsafe.Sizeof(pipe_t{}) +
		unsafe.Sizeof(pipefops_t{}) + 2*condsz)
	fmt.Printf("process: %v + stack + wait\n", unsafe.Sizeof(proc_t{}) +
		tfsz + fxsz + waitsz + tnotesz + timer)
	fmt.Printf("\tvma    : %v\n", unsafe.Sizeof(rbn_t{}) + mfile)
	fmt.Printf("\t1 RBfd : %v\n", unsafe.Sizeof(frbn_t{}))
	fmt.Printf("\t1 fd   : %v\n", fdsz)
	fmt.Printf("\tper-dev poll md: %v\n", polls)
	fmt.Printf("TCB    : %v + 1map\n", unsafe.Sizeof(tcptcb_t{}) +
		unsafe.Sizeof(tcpkey_t{}) + unsafe.Sizeof(tcpfops_t{}) +
		2*condsz + timer)
	fmt.Printf("LTCB   : %v + 1map\n", unsafe.Sizeof(tcplisten_t{}) +
		unsafe.Sizeof(tcplkey_t{}) + is + unsafe.Sizeof(tcplfops_t{}) +
		timer)
	fmt.Printf("US sock: %v + 1map\n", unsafe.Sizeof(susfops_t{}) +
		2*unsafe.Sizeof(pipefops_t{}) + unsafe.Sizeof(pipe_t{}) + 2*condsz)
	fmt.Printf("UD sock: %v + 1map\n", unsafe.Sizeof(sudfops_t{}) +
		unsafe.Sizeof(bud_t{}) + condsz + uintptr(PGSIZE)/10)
}

var _nflip int

func kbd_daemon(cons *cons_t, km map[int]byte) {
	inb := runtime.Inb
	start := make([]byte, 0, 10)
	data := start
	addprint := func(c byte) {
		fmt.Printf("%c", c)
		if len(data) > 1024 {
			fmt.Printf("key dropped!\n")
			return
		}
		data = append(data, c)
		if c == '\\' {
			debug.SetTraceback("all")
			panic("yahoo")
		} else if c == '@' {

		} else if c == '%' {
			//loping()
			//netdump()

			//bp := &bprof_t{}
			//err := pprof.WriteHeapProfile(bp)
			//if err != nil {
			//	fmt.Printf("shat on: %v\n", err)
			//} else {
			//	bp.dump()
			//	fmt.Printf("success?\n")
			//}

		}
	}
	var reqc chan int
	pollers := &pollers_t{}
	for {
		select {
		case <- cons.kbd_int:
			for _kready() {
				sc := int(inb(0x60))
				c, ok := km[sc]
				if ok {
					addprint(c)
				}
			}
			irq_eoi(IRQ_KBD)
		case <- cons.com_int:
			for _comready() {
				com1data := uint16(0x3f8 + 0)
				sc := inb(com1data)
				c := byte(sc)
				if c == '\r' {
					c = '\n'
				} else if c == 127 {
					// delete -> backspace
					c = '\b'
				}
				addprint(c)
			}
			irq_eoi(IRQ_COM1)
		case l := <- reqc:
			if l > len(data) {
				l = len(data)
			}
			s := data[0:l]
			cons.reader <- s
			data = data[l:]
		case pm := <- cons.pollc:
			if pm.events & R_READ == 0 {
				cons.pollret <- 0
				continue
			}
			var ret ready_t
			if len(data) > 0 {
				ret |= R_READ
			} else if pm.dowait {
				pollers.addpoller(&pm)
			}
			cons.pollret <- ret
		}
		if len(data) == 0 {
			reqc = nil
			data = start
		} else {
			reqc = cons.reqc
			pollers.wakeready(R_READ)
		}
	}
}

// reads keyboard data, blocking for at least 1 byte. returns at most cnt
// bytes.
func kbd_get(cnt int) []byte {
	if cnt < 0 {
		panic("negative cnt")
	}
	cons.reqc <- cnt
	return <- cons.reader
}

func attach_devs() int {
	ncpu := acpi_attach()
	pcibus_attach()
	return ncpu
}

var _tlblock	= sync.Mutex{}

func tlb_shootdown(p_pmap, va uintptr, pgcount int) {
	if numcpus == 1 {
		return
	}
	_tlblock.Lock()
	defer _tlblock.Unlock()

	runtime.Tlbshoot.Waitfor = int64(numcpus)
	runtime.Tlbshoot.P_pmap = p_pmap

	lapaddr := 0xfee00000
	lap := (*[PGSIZE/4]uint32)(unsafe.Pointer(uintptr(lapaddr)))
	ipilow := func(ds int, deliv int, vec int) uint32 {
		return uint32(ds << 18 | 1 << 14 | deliv << 8 | vec)
	}

	icrw := func(hi uint32, low uint32) {
		icrh := 0x310/4
		icrl := 0x300/4
		// use sync to guarantee order
		atomic.StoreUint32(&lap[icrh], hi)
		atomic.StoreUint32(&lap[icrl], low)
		ipisent := uint32(1 << 12)
		for atomic.LoadUint32(&lap[icrl]) & ipisent != 0 {
		}
	}

	tlbshootvec := 70
	// broadcast shootdown
	allandself := 2
	low := ipilow(allandself, 0, tlbshootvec)
	icrw(0, low)

	// if we make it here, the IPI must have been sent and thus we
	// shouldn't spin in this loop for very long. this loop does not
	// contain a stack check and thus cannot be preempted by the runtime.
	for atomic.LoadInt64(&runtime.Tlbshoot.Waitfor) != 0 {
	}
}

type bprof_t struct {
	data	[]byte
}

func (b *bprof_t) init() {
	b.data = make([]byte, 0, 4096)
}

func (b *bprof_t) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func (b *bprof_t) len() int {
	return len(b.data)
}

// dumps profile to serial console/vga for xxd -r
func (b *bprof_t) dump() {
	hexdump(b.data)
}

func hexdump(buf []uint8) {
	l := len(buf)
	for i := 0; i < l; i += 16 {
		cur := buf[i:]
		if len(cur) > 16 {
			cur = cur[:16]
		}
		fmt.Printf("%07x: ", i)
		prc := 0
		for _, b := range cur {
			fmt.Printf("%02x", b)
			prc++
			if prc % 2 == 0 {
				fmt.Printf(" ")
			}
		}
		fmt.Printf("\n")
	}
}

var prof = bprof_t{}

func cpuidfamily() (uint, uint) {
	ax, _, _, _ := runtime.Cpuid(1, 0)
	model :=  (ax >> 4) & 0xf
	family := (ax >> 8) & 0xf
	emodel := (ax >> 16) & 0xf
	efamily := (ax >> 20) & 0xff

	dispmodel := emodel << 4 + model
	dispfamily := efamily + family
	return uint(dispmodel), uint(dispfamily)
}

func cpuchk() {
	_, _, _, dx := runtime.Cpuid(0x80000001, 0)
	arch64 := uint32(1 << 29)
	if dx & arch64 == 0 {
		panic("not intel 64 arch?")
	}

	rmodel, rfamily := cpuidfamily()
	fmt.Printf("CPUID: family: %x, model: %x\n", rfamily, rmodel)

	ax, _, _, dx := runtime.Cpuid(1, 0)
	stepping := ax & 0xf
	oldp := rfamily == 6 && rmodel < 3 && stepping < 3
	sep := uint32(1 << 11)
	if dx & sep == 0 || oldp {
		panic("sysenter not supported")
	}

	_, _, _, dx = runtime.Cpuid(0x80000007, 0)
	invartsc := uint32(1 << 8)
	if dx & invartsc == 0 {
		// no qemu CPUs support invariant tsc, but my hardware does...
		//panic("invariant tsc not supported")
		fmt.Printf("invariant TSC not supported\n")
	}
}

func perfsetup() {
	ax, bx, _, _ := runtime.Cpuid(0xa, 0)
	perfv := ax & 0xff
	npmc := (ax >> 8) & 0xff
	pmcbits := (ax >> 16) & 0xff
	pmebits := (ax >> 24) & 0xff
	cyccnt := bx & 1 == 0
	_, _, cx, _ := runtime.Cpuid(0x1, 0)
	pdc := cx & (1 << 15) != 0
	if pdc && perfv >= 2 && perfv <= 3 && npmc >= 1 && pmebits >= 1 &&
	    cyccnt && pmcbits >= 32 {
		fmt.Printf("Hardware Performance monitoring enabled: " +
		    "%v counters\n", npmc)
		profhw = &intelprof_t{}
		profhw.prof_init(uint(npmc))
	} else {
		fmt.Printf("No hardware performance monitoring\n")
		profhw = &nilprof_t{}
	}
}

// performance monitoring event id
type pmevid_t uint

const(
	// if you modify the order of these flags, you must update them in libc
	// too.
	// architectural
	EV_UNHALTED_CORE_CYCLES		pmevid_t = 1 << iota
	EV_LLC_MISSES			pmevid_t = 1 << iota
	EV_LLC_REFS			pmevid_t = 1 << iota
	EV_BRANCH_INSTR_RETIRED		pmevid_t = 1 << iota
	EV_BRANCH_MISS_RETIRED		pmevid_t = 1 << iota
	EV_INSTR_RETIRED		pmevid_t = 1 << iota
	// non-architectural
	// "all TLB misses that cause a page walk"
	EV_DTLB_LOAD_MISS_ANY		pmevid_t = 1 << iota
	// "number of completed walks due to miss in sTLB"
	EV_DTLB_LOAD_MISS_STLB		pmevid_t = 1 << iota
	// "retired stores that missed in the dTLB"
	EV_STORE_DTLB_MISS		pmevid_t = 1 << iota
	EV_L2_LD_HITS			pmevid_t = 1 << iota
	// "Counts the number of misses in all levels of the ITLB which causes
	// a page walk."
	EV_ITLB_LOAD_MISS_ANY		pmevid_t = 1 << iota
)

type pmflag_t uint

const(
	EVF_OS				pmflag_t = 1 << iota
	EVF_USR				pmflag_t = 1 << iota
)

type pmev_t struct {
	evid	pmevid_t
	pflags	pmflag_t
}

var pmevid_names = map[pmevid_t]string{
	EV_UNHALTED_CORE_CYCLES: "Unhalted core cycles",
	EV_LLC_MISSES: "LLC misses",
	EV_LLC_REFS: "LLC references",
	EV_BRANCH_INSTR_RETIRED: "Branch instructions retired",
	EV_BRANCH_MISS_RETIRED: "Branch misses retired",
	EV_INSTR_RETIRED: "Instructions retired",
	EV_DTLB_LOAD_MISS_ANY: "dTLB load misses",
	EV_ITLB_LOAD_MISS_ANY: "iTLB load misses",
	EV_DTLB_LOAD_MISS_STLB: "sTLB misses",
	EV_STORE_DTLB_MISS: "Store dTLB misses",
	//EV_WTF1: "dummy 1",
	//EV_WTF2: "dummy 2",
	EV_L2_LD_HITS: "L2 load hits",
}

// a device driver for hardware profiling
type profhw_i interface {
	prof_init(uint)
	startpmc([]pmev_t) ([]int, bool)
	stoppmc([]int) []uint
	startnmi(pmevid_t, pmflag_t, uint, uint) bool
	stopnmi() []uintptr
}

var profhw profhw_i

type nilprof_t struct {
}

func (n *nilprof_t) prof_init(uint) {
}

func (n *nilprof_t) startpmc([]pmev_t) ([]int, bool) {
	return nil, false
}

func (n *nilprof_t) stoppmc([]int) []uint {
	return nil
}

func (n *nilprof_t) startnmi(pmevid_t, pmflag_t, uint, uint) bool {
	return false
}

func (n *nilprof_t) stopnmi() []uintptr {
	return nil
}

type intelprof_t struct {
	l		sync.Mutex
	pmcs		[]intelpmc_t
	events		map[pmevid_t]pmevent_t
}

type intelpmc_t struct {
	alloced		bool
	eventid		pmevid_t
}

type pmevent_t struct {
	event	int
	umask	int
}

func (ip *intelprof_t) _disableall() {
	ip._perfmaskipi()
}

func (ip *intelprof_t) _enableall() {
	ip._perfmaskipi()
}

func (ip *intelprof_t) _perfmaskipi() {
	lapaddr := 0xfee00000
	lap := (*[PGSIZE/4]uint32)(unsafe.Pointer(uintptr(lapaddr)))

	allandself := 2
	trap_perfmask := 72
	level := 1 << 14
	low := uint32(allandself << 18 | level | trap_perfmask)
	icrl := 0x300/4
	atomic.StoreUint32(&lap[icrl], low)
	ipisent := uint32(1 << 12)
	for atomic.LoadUint32(&lap[icrl]) & ipisent != 0 {
	}
}

func (ip *intelprof_t) _ev2msr(eid pmevid_t, pf pmflag_t) int {
	ev, ok := ip.events[eid]
	if !ok {
		panic("no such event")
	}
	usr := 1 << 16
	os  := 1 << 17
	en  := 1 << 22
	event := ev.event
	umask := ev.umask << 8
	v := umask | event | en
	if pf & EVF_OS != 0 {
		v |= os
	}
	if pf & EVF_USR != 0 {
		v |= usr
	}
	if pf == 0 {
		v |= os | usr
	}
	return v
}

// XXX counting PMCs only works with one CPU; move counter start/stop to perf
// IPI.
func (ip *intelprof_t) _pmc_start(cid int, eid pmevid_t, pf pmflag_t) {
	if cid < 0 || cid >= len(ip.pmcs) {
		panic("wtf")
	}
	wrmsr := func(a, b int) {
		runtime.Wrmsr(a, b)
	}
	ia32_pmc0 := 0xc1
	ia32_perfevtsel0 := 0x186
	pmc := ia32_pmc0 + cid
	evtsel := ia32_perfevtsel0 + cid
	// disable perf counter before clearing
	wrmsr(evtsel, 0)
	wrmsr(pmc, 0)

	v := ip._ev2msr(eid, pf)
	wrmsr(evtsel, v)
}

func (ip *intelprof_t) _pmc_stop(cid int) uint {
	if cid < 0 || cid >= len(ip.pmcs) {
		panic("wtf")
	}
	ia32_pmc0 := 0xc1
	ia32_perfevtsel0 := 0x186
	pmc := ia32_pmc0 + cid
	evtsel := ia32_perfevtsel0 + cid
	ret := runtime.Rdmsr(pmc)
	runtime.Wrmsr(evtsel, 0)
	return uint(ret)
}

func (ip *intelprof_t) prof_init(npmc uint) {
	ip.pmcs = make([]intelpmc_t, npmc)
	// architectural events
	ip.events = map[pmevid_t]pmevent_t{
	    EV_UNHALTED_CORE_CYCLES:
		{0x3c, 0},
	    EV_LLC_MISSES:
		{0x2e, 0x41},
	    EV_LLC_REFS:
		{0x2e, 0x4f},
	    EV_BRANCH_INSTR_RETIRED:
		{0xc4, 0x0},
	    EV_BRANCH_MISS_RETIRED:
		{0xc5, 0x0},
	    EV_INSTR_RETIRED:
		{0xc0, 0x0},
	}

	_xeon5000 := map[pmevid_t]pmevent_t{
	    EV_DTLB_LOAD_MISS_ANY:
		{0x08, 0x1},
	    EV_DTLB_LOAD_MISS_STLB:
		{0x08, 0x2},
	    EV_STORE_DTLB_MISS:
		{0x0c, 0x1},
	    EV_ITLB_LOAD_MISS_ANY:
		{0x85, 0x1},
	    //EV_WTF1:
	    //    {0x49, 0x1},
	    //EV_WTF2:
	    //    {0x14, 0x2},
	    EV_L2_LD_HITS:
		{0x24, 0x1},
	}

	dispmodel, dispfamily := cpuidfamily()

	if dispfamily == 0x6 && dispmodel == 0x1e {
		for k, v := range _xeon5000 {
			ip.events[k] = v
		}
	}
}

// starts a performance counter for each event in evs. if all the counters
// cannot be allocated, no performance counter is started.
func (ip *intelprof_t) startpmc(evs []pmev_t) ([]int, bool) {
	ip.l.Lock()
	defer ip.l.Unlock()

	// are the event ids supported?
	for _, ev := range evs {
		if _, ok := ip.events[ev.evid]; !ok {
			return nil, false
		}
	}
	// make sure we have enough counters
	cnt := 0
	for i := range ip.pmcs {
		if !ip.pmcs[i].alloced {
			cnt++
		}
	}
	if cnt < len(evs) {
		return nil, false
	}

	ret := make([]int, len(evs))
	ri := 0
	// find available counter
	outer:
	for _, ev := range evs {
		eid := ev.evid
		for i := range ip.pmcs {
			if !ip.pmcs[i].alloced {
				ip.pmcs[i].alloced = true
				ip.pmcs[i].eventid = eid
				ip._pmc_start(i, eid, ev.pflags)
				ret[ri] = i
				ri++
				continue outer
			}
		}
	}
	return ret, true
}

func (ip *intelprof_t) stoppmc(idxs []int) []uint {
	ip.l.Lock()
	defer ip.l.Unlock()

	ret := make([]uint, len(idxs))
	ri := 0
	for _, idx := range idxs {
		if !ip.pmcs[idx].alloced {
			ret[ri] = 0
			ri++
			continue
		}
		ip.pmcs[idx].alloced = false
		c := ip._pmc_stop(idx)
		ret[ri] = c
		ri++
	}
	return ret
}

func (ip *intelprof_t) startnmi(evid pmevid_t, pf pmflag_t, min,
    max uint) bool {
	ip.l.Lock()
	defer ip.l.Unlock()
	if ip.pmcs[0].alloced {
		return false
	}
	if _, ok := ip.events[evid]; !ok {
		return false
	}
	// NMI profiling currently only uses pmc0 (but could use any other
	// counter)
	ip.pmcs[0].alloced = true

	v := ip._ev2msr(evid, pf)
	// enable LVT interrupt on PMC overflow
	inte := 1 << 20
	v |= inte

	mask := false
	runtime.SetNMI(mask, v, min, max)
	ip._enableall()
	return true
}

func (ip *intelprof_t) stopnmi() []uintptr {
	ip.l.Lock()
	defer ip.l.Unlock()

	mask := true
	runtime.SetNMI(mask, 0, 0, 0)
	ip._disableall()
	buf, full := runtime.TakeNMIBuf()
	if full {
		fmt.Printf("*** NMI buffer is full!\n")
	}

	ip.pmcs[0].alloced = false

	return buf
}

// can account for up to 16TB of mem
type physpg_t struct {
	refcnt		int32
	// index into pgs of next page on free list
	nexti		uint32
}

var physmem struct {
	pgs		[]physpg_t
	startn		uint32
	// index into pgs of first free pg
	freei		uint32
	pmaps		uint32
	sync.Mutex
}

func _refaddr(p_pg pa_t) (*int32, uint32) {
	idx := _pg2pgn(p_pg) - physmem.startn
	return &physmem.pgs[idx].refcnt, idx
}

func refcnt(p_pg pa_t) int {
	ref, _ := _refaddr(p_pg)
	c := atomic.AddInt32(ref, 1)
	return int(c)
}

func refup(p_pg pa_t) {
	ref, _ := _refaddr(p_pg)
	c := atomic.AddInt32(ref, 1)
	// XXXPANIC
	if c <= 0 {
		panic("wut")
	}
}

// returns true if p_pg should be added to the free list and the index of the
// page in the pgs array
func _refdec(p_pg pa_t) (bool, uint32) {
	ref, idx := _refaddr(p_pg)
	c := atomic.AddInt32(ref, -1)
	// XXXPANIC
	if c < 0 {
		panic("wut")
	}
	return c == 0, idx
}

func _reffree(idx uint32) {
	physmem.Lock()
	onext := physmem.freei
	physmem.pgs[idx].nexti = onext
	physmem.freei = idx
	physmem.Unlock()
}

func refdown(p_pg pa_t) bool {
	// add to freelist?
	if add, idx := _refdec(p_pg); add {
		_reffree(idx)
		return true
	}
	return false
}

var failalloc bool = false

var failsites = make(map[uintptr]bool)
// white-listed functions; don't fail these allocations. terminate() is for
// init resurrection.
var oksites = map[string]bool{"main.main":true, "main.(*proc_t).terminate":true,}

func _pchash(pcs []uintptr) uintptr {
	if len(pcs) == 0 {
		panic("d'oh")
	}
	var ret uintptr
	for _, pc := range pcs {
		pc = pc * 1103515245 + 12345
		ret ^= pc
	}
	return ret
}

// returns true if the allocation should fail
func _fakefail() bool {
	if !failalloc {
		return false
	}
	//return rand.Intn(10000) < 10
	var pcs []uintptr
	for sz, got := 30, 30; got >= sz; sz *= 2 {
		if sz != 30 {
			fmt.Printf("!")
		}
		pcs = make([]uintptr, 30)
		// get caller of _refpg_new
		got = runtime.Callers(4, pcs)
		if got == 0 {
			panic("no")
		}
	}
	h := _pchash(pcs)
	if ok := failsites[h]; !ok {
		failsites[h] = true
		frames := runtime.CallersFrames(pcs)
		fs := ""
		// check for white-listed caller
		for {
			fr, more := frames.Next()
			if ok := oksites[fr.Function]; ok {
				return false
			}
			if fs == "" {
				fs = fmt.Sprintf("%v (%v:%v)->", fr.Function,
				   fr.File, fr.Line)
			} else {
				fs += fr.Function + "->"
			}
			if !more || fr.Function == "runtime.goexit" {
				break
			}
		}
		fmt.Printf("failing: %v\n", fs)
		return true
	}
	return false
}

func callerdump() {
	i := 3
	s := ""
	for {
		_, f, l, ok := runtime.Caller(i)
		if !ok {
			break
		}
		i++
		li := strings.LastIndex(f, "/")
		if li != -1 {
			f = f[li + 1:]
		}
		if s == "" {
			s = fmt.Sprintf("%s:%d", f, l)
		} else {
			s += fmt.Sprintf("<-%s:%d", f, l)
		}
	}
	fmt.Printf("%s\n", s)
}

func _refpg_new() (*pg_t, pa_t, bool) {
	return _phys_new(&physmem.freei)
}

// refcnt of returned page is not incremented (it is usually incremented via
// proc_t.page_insert). requires direct mapping.
func refpg_new() (*pg_t, pa_t, bool) {
	pg, p_pg, ok := _refpg_new()
	if !ok {
		return nil, 0, false
	}
	*pg = *zeropg
	return pg, p_pg, true
}

func refpg_new_nozero() (*pg_t, pa_t, bool) {
	pg, p_pg, ok := _refpg_new()
	if !ok {
		return nil, 0, false
	}
	return pg, p_pg, true
}

func pmap_new() (*pmap_t, pa_t, bool) {
	a, b, ok := _phys_new(&physmem.pmaps)
	if ok {
		return pg2pmap(a), b, true
	}
	a, b, ok = refpg_new()
	return pg2pmap(a), b, ok
}

func _phys_new(fl *uint32) (*pg_t, pa_t, bool) {
	if !_dmapinit {
		panic("dmap not initted")
	}

	var p_pg pa_t
	var ok bool
	physmem.Lock()
	ff := *fl
	if ff != ^uint32(0) {
		p_pg = pa_t(ff + physmem.startn) << PGSHIFT
		*fl = physmem.pgs[ff].nexti
		ok = true
		if physmem.pgs[ff].refcnt < 0 {
			panic("negative ref count")
		}
	}
	physmem.Unlock()
	if ok {
		return dmap(p_pg), p_pg, true
	}
	return nil, 0, false
}

func _phys_put(fl *uint32, p_pg pa_t) {
	if add, idx := _refdec(p_pg); add {
		physmem.Lock()
		physmem.pgs[idx].nexti = *fl
		*fl = idx
		physmem.Unlock()
	}
}

func _pg2pgn(p_pg pa_t) uint32 {
	return uint32(p_pg >> PGSHIFT)
}

func phys_init() {
	// reserve 128MB of pages
	//respgs := 1 << 15
	respgs := 1 << 16
	//respgs := 1 << (31 - 12)
	// 7.5 GB
	//respgs := 1835008
	//respgs := 1 << 18 + (1 <<17)
	physmem.pgs = make([]physpg_t, respgs)
	for i := range physmem.pgs {
		physmem.pgs[i].refcnt = -10
	}
	first := pa_t(runtime.Get_phys())
	fpgn := _pg2pgn(first)
	physmem.startn = fpgn
	physmem.freei = 0
	physmem.pmaps = ^uint32(0)
	physmem.pgs[0].refcnt = 0
	physmem.pgs[0].nexti = ^uint32(0)
	last := physmem.freei
	for i := 0; i < respgs - 1; i++ {
		p_pg := pa_t(runtime.Get_phys())
		pgn := _pg2pgn(p_pg)
		idx := pgn - physmem.startn
		// Get_phys() may skip regions.
		if int(idx) >= len(physmem.pgs) {
			if respgs - i > int(float64(respgs)*0.01) {
				panic("got many bad pages")
			}
			break
		}
		physmem.pgs[idx].refcnt = 0
		physmem.pgs[last].nexti = idx;
		physmem.pgs[idx].nexti =  ^uint32(0)
		last = idx
	}
	fmt.Printf("Reserved %v pages (%vMB)\n", respgs, respgs >> 8)
}

func pgcount() (int, int) {
	physmem.Lock()
	r1 := 0
	for i := physmem.freei; i != ^uint32(0); i = physmem.pgs[i].nexti {
		r1++
	}
	r2 := pmapcount()
	physmem.Unlock()
	return r1, r2
}

func _pmcount(pml4 pa_t, lev int) int {
	pg := pg2pmap(dmap(pml4))
	ret := 0
	for _, pte := range pg {
		if pte & PTE_U != 0 && pte & PTE_P != 0 {
			ret += 1 + _pmcount(pa_t(pte & PTE_ADDR), lev - 1)
		}
	}
	return ret
}

func pmapcount() int {
	c := 0
	for ni := physmem.pmaps; ni != ^uint32(0); ni = physmem.pgs[ni].nexti {
		v := _pmcount(pa_t(ni+ physmem.startn) << PGSHIFT, 4)
		c += v
	}
	return c
}

func structchk() {
	if unsafe.Sizeof(stat_t{}) != 9*8 {
		panic("bad stat_t size")
	}
}

var lhits int

func main() {
	// magic loop
	//if rand.Int() != 0 {
	//	for {
	//	}
	//}
	bsp_apic_id = lap_id()
	phys_init()

	go func() {
		<- time.After(10*time.Second)
		fmt.Printf("[It is now safe to benchmark...]\n")
	}()

	go func() {
		for {
			<- time.After(1*time.Second)
			got := lhits
			lhits = 0
			if got != 0 {
				fmt.Printf("*** limit hits: %v\n", got)
			}
		}
	}()

	fmt.Printf("              BiscuitOS\n")
	fmt.Printf("          go version: %v\n", runtime.Version())
	pmem := runtime.Totalphysmem()
	fmt.Printf("  %v MB of physical memory\n", pmem >> 20)

	structchk()
	cpuchk()
	net_init()

	dmap_init()
	perfsetup()

	// must come before any irq_unmask()s
	runtime.Install_traphandler(trapstub)

	//pci_dump()
	ncpu := attach_devs()

	kbd_init()

	// control CPUs
	aplim := 7
	cpus_start(ncpu, aplim)
	//runtime.SCenable = false

	rf := fs_init()

	exec := func(cmd string, args []string) {
		fmt.Printf("start [%v %v]\n", cmd, args)
		nargs := []string{cmd}
		nargs = append(nargs, args...)
		defaultfds := []*fd_t{&fd_stdin, &fd_stdout, &fd_stderr}
		p, ok := proc_new(cmd, rf, defaultfds)
		if !ok {
			panic("silly sysprocs")
		}
		var tf [TFSIZE]uintptr
		ret := sys_execv1(p, &tf, cmd, nargs)
		if ret != 0 {
			panic(fmt.Sprintf("exec failed %v", ret))
		}
		p.sched_add(&tf, p.tid0)
	}

	//exec("bin/lsh", nil)
	exec("bin/init", nil)
	//exec("bin/rs", []string{"/redis.conf"})

	//go func() {
	//	d := time.Second
	//	for {
	//		<- time.After(d)
	//		ms := &runtime.MemStats{}
	//		runtime.ReadMemStats(ms)
	//		fmt.Printf("%v MiB\n", ms.Alloc/ (1 << 20))
	//	}
	//}()

	// sleep forever
	var dur chan bool
	<- dur
}

func findbm() {
	dmap_init()
	//n := incn()
	//var aplim int
	//if n == 0 {
	//	aplim = 1
	//} else {
	//	aplim = 7
	//}
	al := 7
	cpus_start(al, al)

	ch := make(chan bool)
	times := uint64(0)
	sum := uint64(0)
	for {
		st0 := runtime.Rdtsc()
		go func(st uint64) {
			tot := runtime.Rdtsc() - st
			sum += tot
			times++
			if times % 1000000 == 0 {
				fmt.Printf("%9v cycles to find (avg)\n",
				    sum/times)
				sum = 0
				times = 0
			}
			ch <- true
		}(st0)
		//<- ch
		loopy: for {
			select {
			case <- ch:
				break loopy
			default:
			}
		}
	}
}
