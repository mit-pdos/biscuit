package main

import "fmt"
import "math/rand"
import "runtime"
import "sync/atomic"
import "sync"
import "time"
import "unsafe"

type trapstore_t struct {
	trapno    int
	pid       int
	tid       tid_t
	faultaddr int
	tf        [TFSIZE]int
	notify    bool
	inttime   int
}
const maxtstore int = 64
const maxcpus   int = 32

//go:nosplit
func tsnext(c int) int {
	return (c + 1) % maxtstore
}

var	numcpus	int = 1

type cpu_t struct {
	// logical number, not lapic id
	num		int
	// per-cpus interrupt queues. the cpu interrupt handler is the
	// producer, the go routine running trap() below is the consumer. each
	// cpus interrupt handler increments head while the go routine consuer
	// increments tail
	trapstore	[maxtstore]trapstore_t
	tshead		int
	tstail		int
}

var cpus	[maxcpus]cpu_t

// these functions can only be used when interrupts are cleared
//go:nosplit
func lap_id() int {
	lapaddr := (*[1024]int32)(unsafe.Pointer(uintptr(0xfee00000)))
	return int(lapaddr[0x20/4] >> 24)
}

func p8259_eoi() {
	pic1 := 0x20
	pic2 := 0xa0
	// non-specific eoi
	eoi := 0x20

	outb := runtime.Outb
	outb(pic1, eoi)
	outb(pic2, eoi)
}

const(
	DIVZERO		= 0
	GPFAULT		= 13
	PGFAULT		= 14
	TIMER		= 32
	SYSCALL		= 64

	// low 3 bits must be zero
	IRQ_BASE	= 32
	IRQ_KBD		= 1
	IRQ_COM1	= 4
	IRQ_LAST	= IRQ_BASE + 16

	INT_KBD		= IRQ_BASE + IRQ_KBD
	INT_COM1	= IRQ_BASE + IRQ_COM1
)

// initialized by disk attach functions
var IRQ_DISK	int = -1
var INT_DISK	int = -1

// trap cannot do anything that may have side-effects on the runtime (like
// fmt.Print, or use panic!). the reason is that goroutines are scheduled
// cooperatively in the runtime. trap interrupts the runtime though, and then
// tries to execute more gocode on the same M, thus doing things the runtime
// did not expect.
//go:nosplit
func trapstub(tf *[TFSIZE]int, ptid runtime.Ptid_t, notify int) {

	lid := cpus[lap_id()].num
	head := cpus[lid].tshead
	tail := cpus[lid].tstail
	ts := &cpus[lid].trapstore[head]

	// make sure circular buffer has room
	if tsnext(head) == tail {
		for i := tail; i != head; i = tsnext(i) {
			runtime.Pnum(cpus[lid].trapstore[i].trapno)
		}
		runtime.Pnum(0xbad)
		for {}
	}

	// extract process and thread id
	pid := int(ptid >> 32)
	tid := tid_t(uint32(ptid))
	ts.pid = pid
	ts.tid = tid
	ts.inttime = runtime.Nanotime()

	// if notify is non-zero, a trap has not been received but a thread has
	// stopped running (due to a timer interrupt) and thus it's pmap can be
	// freed.
	if notify != 0 {
		if pid == 0 {
			runtime.Pnum(0xbad2)
			for {}
		}
		ts.notify = true
		// commit "interrupt"
		head = tsnext(head)
		cpus[lid].tshead = head
		runtime.Trapwake()
		return
	}

	trapno := tf[TF_TRAP]

	// kernel faults are fatal errors for now, but they could be handled by
	// trap & c.
	if pid == 0 && trapno < IRQ_BASE {
		runtime.Pnum(trapno)
		if trapno == PGFAULT {
			runtime.Pnum(runtime.Rcr2())
		}
		rip := tf[TF_RIP]
		runtime.Pnum(rip)
		runtime.Pnum(0x42)
		runtime.Pnum(lap_id())
		runtime.Tfdump(tf)
		//runtime.Stackdump(tf[TF_RSP])
		for {}
	}

	// add to trap circular buffer for actual trap handler
	ts.trapno = trapno
	ts.tf = *tf
	ts.notify = false
	if trapno == PGFAULT {
		ts.faultaddr = runtime.Rcr2()
	}

	// commit interrupt
	head = tsnext(head)
	cpus[lid].tshead = head

	runtime.Trapwake()

	switch trapno {
	case SYSCALL, PGFAULT, DIVZERO:
		// yield until the syscall/fault is handled
		runtime.Procyield()
	case INT_DISK:
		// unclear whether automatic eoi mode works on the slave 8259a.
		// from page 15 of intel's 8259a doc: "The AEOI mode can only
		// be used in a master 8259A and not a slave. 8259As with a
		// copyright date of 1985 or later will operate in the AEOI
		// mode as a master or a slave." linux seems to observe that
		// automatic eoi also doesn't work on the slave.
		//p8259_eoi()
		runtime.Proccontinue()
	case INT_KBD, INT_COM1:
		runtime.Proccontinue()
	default:
		runtime.Pnum(trapno)
		runtime.Pnum(tf[TF_RIP])
		runtime.Pnum(0xbadbabe)
		for {}
	}
}

func trap(handlers map[int]func(*trapstore_t)) {
	runtime.Trapinit()
	for {
		for cpu := 0; cpu < numcpus; {
			head := cpus[cpu].tshead
			tail := cpus[cpu].tstail

			if tail == head {
				// no work for this cpu
				cpu++
				continue
			}

			tcur := trapstore_t{}
			tcur = cpus[cpu].trapstore[tail]

			trapno := tcur.trapno
			notify := tcur.notify

			tail = tsnext(tail)
			cpus[cpu].tstail = tail

			if notify {
				pid := tcur.pid
				tid := tcur.tid
				p := proc_get(pid)
				reap_doomed(p, tid)
				continue
			}

			if h, ok := handlers[trapno]; ok {
				go h(&tcur)
				continue
			}
			pid := tcur.pid
			panic(fmt.Sprintf("no handler for trap %v, pid %x\n",
			    trapno,pid))
		}
		runtime.Trapsched()
	}
}

func trap_divzero(ts *trapstore_t) {
	fmt.Printf("pid %v divide by zero; killing...\n", ts.pid)
	p := proc_get(ts.pid)
	sys_exit(p, ts.tid, -1)
}

func trap_disk(ts *trapstore_t) {
	// is this a disk int?
	if !disk.intr() {
		fmt.Printf("spurious disk int\n")
		return
	}
	ide_int_done <- true
}

func trap_cons(ts *trapstore_t) {
	var ch chan bool
	if ts.trapno == INT_KBD {
		ch = cons.kbd_int
	} else if ts.trapno == INT_COM1 {
		ch = cons.com_int
	} else {
		panic("bad int")
	}
	ch <- true
}

func trap_syscall(ts *trapstore_t) {
	pid  := ts.pid
	tid := ts.tid
	p := proc_get(pid)

	ret := syscall(p, tid, &ts.tf)
	ts.tf[TF_RAX] = ret
	if p.resched(tid) {
		p.sched_runnable(&ts.tf, tid)
		p.atime.finish(ts.inttime)
	}
}

func trap_pgfault(ts *trapstore_t) {
	pid := ts.pid
	tid := ts.tid
	fa  := ts.faultaddr
	proc := proc_get(pid)

	proc.Lock_pmap()
	defer proc.Unlock_pmap()

	pte := pmap_walk(proc.pmap, fa, false, 0, nil)
	if pte != nil {
		cow := *pte & PTE_COW != 0
		wascow := *pte & PTE_WASCOW != 0
		if cow || wascow {
			if fa < USERMIN {
				panic("kern addr marked cow")
			}
			sys_pgfault(proc, tid, pte, fa, &ts.tf)
			// set process as runnable again
			proc.sched_runnable(nil, tid)
			proc.atime.finish(ts.inttime)
			return
		}
	}

	rip := ts.tf[TF_RIP]
	fmt.Printf("*** fault *** %v: addr %x, rip %x. killing...\n",
	    proc.name, fa, rip)
	sys_exit(proc, tid, SIGNALED)
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

// XXX
func cdelay(n int) {
	for i := 0; i < n*1000000; i++ {
	}
}

type dev_t struct {
	major	int
	minor	int
}

// allocated device major numbers
const(
	D_CONSOLE int	= 1
	// UNIX domain sockets
	D_SUN 		= 2
	D_FIRST		= D_CONSOLE
	D_LAST		= D_SUN
)

type file_t struct {
	ftype	ftype_t
	sync.Mutex
	offset	int
	priv	inum
	pipe	pipe_t
	dev	dev_t
	sock	sock_t
}

type ftype_t int
const(
	INODE ftype_t = iota
	PIPE
	DEV	// only console for now
	SOCKET
)

const(
	FD_READ		= 0x1
	FD_WRITE	= 0x2
	FD_CLOEXEC	= 0x4
	FD_APPEND	= 0x8
)

type fd_t struct {
	file	*file_t
	perms	int
}

var dummyfile	= file_t{ftype: DEV, dev: dev_t{int(D_CONSOLE), 0}}

// special fds
var fd_stdin 	= fd_t{file: &dummyfile, perms: FD_READ}
var fd_stdout 	= fd_t{file: &dummyfile, perms: FD_WRITE}
var fd_stderr 	= fd_t{file: &dummyfile, perms: FD_WRITE}

type ulimit_t struct {
	pages	int
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

type tid_t int

type threadinfo_t struct {
	alive	map[tid_t]bool
	sync.Mutex
}

func (t *threadinfo_t) init() {
	t.alive = make(map[tid_t]bool)
}

type proc_t struct {
	pid		int
	// first thread id
	tid0		tid_t
	name		string

	// waitinfo for my child processes
	waiti		waitinfo_t
	// waitinfo of my parent
	pwaiti		*waitinfo_t

	// thread waitinfo
	threadi		threadinfo_t
	// waitinfo for threads
	twaiti		waitinfo_t

	// all pages
	pages		map[int]*[512]int
	// user va -> physical mapping
	upages		map[int]int

	mmapi		int
	pmap		*[512]int
	p_pmap		int
	// lock for pages, upages, pmap, and p_pmap
	pgfl		sync.Mutex
	pgfltaken	bool

	// a process is marked doomed when it has been killed but may have
	// threads currently running on another processor
	doomed		bool
	perished	bool
	exitstatus	int

	fds		map[int]*fd_t
	// where to start scanning for free fds
	fdstart		int
	// fds, fdstart protected by fdl
	fdl		sync.Mutex

	cwd		*file_t
	// to serialize chdirs
	cwdl		sync.Mutex
	ulim		ulimit_t

	// this proc's rusage
	atime		accnt_t
	// total child rusage
	catime		accnt_t
}

var proclock = sync.Mutex{}
var allprocs = map[int]*proc_t{}

var pid_cur  int

func newpid() int {
	proclock.Lock()
	pid_cur++
	ret := pid_cur
	proclock.Unlock()

	return ret
}

func proc_new(name string, cwd *file_t) *proc_t {
	ret := &proc_t{}

	proclock.Lock()
	pid_cur++
	np := pid_cur
	if _, ok := allprocs[np]; ok {
		panic("pid exists")
	}
	allprocs[np] = ret
	proclock.Unlock()

	ret.name = name
	ret.pid = np
	ret.pages = make(map[int]*[512]int)
	ret.upages = make(map[int]int)
	ret.fds = map[int]*fd_t{0: &fd_stdin, 1: &fd_stdout, 2: &fd_stderr}
	ret.fdstart = 3
	ret.cwd = cwd
	if cwd.ftype != INODE {
		panic("weird cwd")
	}
	fs_memref(ret.cwd, 0)
	ret.mmapi = USERMIN
	// mem limit = 128 MB
	ret.ulim.pages = (1 << 27) / (1 << 12)

	ret.waiti.init()
	ret.twaiti.init()
	ret.threadi.init()
	ret.tid0 = ret.tid_new()

	var pm parmsg_t
	pm.init(int(ret.tid0), true)
	ret.twaiti.getch <- pm
	resp := <- pm.ack
	if resp.err != 0 {
		panic("add child thread")
	}

	return ret
}

func proc_get(pid int) *proc_t {
	proclock.Lock()
	p, ok := allprocs[pid]
	proclock.Unlock()
	if !ok {
		panic(fmt.Sprintf("no such pid %d", pid))
	}
	return p
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

func (p *proc_t) fd_new(perms int) (int, *fd_t) {
	p.fdl.Lock()
	defer p.fdl.Unlock()

	// find free fd
	newfd := p.fdstart
	for {
		if _, ok := p.fds[newfd]; !ok {
			p.fdstart = newfd + 1
			break
		}
		newfd++
	}
	fdn := newfd
	fd := &fd_t{}
	fd.perms = perms
	if _, ok := p.fds[fdn]; ok {
		panic(fmt.Sprintf("new fd exists %d", fdn))
	}
	p.fds[fdn] = fd
	return fdn, fd
}

func (p *proc_t) fd_get(fdn int) (*fd_t, bool) {
	p.fdl.Lock()
	ret, ok := p.fds[fdn]
	p.fdl.Unlock()
	return ret, ok
}

func (p *proc_t) fd_del(fdn int) (*fd_t, bool) {
	p.fdl.Lock()
	ret, ok := p.fds[fdn]
	if ok {
		delete(p.fds, fdn)
	}
	if ok && fdn < p.fdstart {
		p.fdstart = fdn
	}
	p.fdl.Unlock()
	return ret, ok
}

func (p *proc_t) mkptid(tid tid_t) runtime.Ptid_t {
	// make pid/tid pair
	ptid := p.pid << 32
	ptid |= int(tid)
	return runtime.Ptid_t(ptid)
}

func (p *proc_t) page_insert(va int, pg *[512]int, p_pg int,
    perms int, vempty bool) {
	p.lockassert_pmap()
	uva := va & PGMASK
	pte := pmap_walk(p.pmap, va, true, PTE_U | PTE_W, p.pages)
	ninval := false
	if pte != nil && *pte & PTE_P != 0 {
		if vempty {
			panic("pte not empty")
		}
		ninval = true
		// remove from tracking maps
		p_rem := *pte & PTE_ADDR
		if _, ok := p.pages[p_rem]; !ok {
			panic("kern va not tracked")
		}
		if _, ok := p.upages[uva]; !ok {
			panic("user va not tracked")
		}
		delete(p.pages, p_rem)
		// upages entry updated below
	}
	*pte = p_pg | perms | PTE_P
	if ninval {
		invlpg(va)
	}

	// make sure page is tracked if shared (ie the page was not allocated
	// via pg_new with p's "pages")
	p.pages[p_pg] = pg
	p.upages[uva] = p_pg
}

func (p *proc_t) page_remove(va int) bool {
	p.lockassert_pmap()
	remmed := false
	pte := pmap_walk(p.pmap, va, false, 0, nil)
	if pte != nil && *pte & PTE_P != 0 {
		p_pa := *pte & PTE_ADDR
		*pte = 0
		invlpg(va)
		delete(p.pages, p_pa)
		uva := va & PGMASK
		delete(p.upages, uva)
		remmed = true
	}
	return remmed
}

func (p *proc_t) tlbshoot(startva, pgcount int) {
	p.lockassert_pmap()
	if p.thread_count() > 1 {
		tlb_shootdown(p.p_pmap, startva, pgcount)
	}
}

func (p *proc_t) resched(tid tid_t) bool {
	p.threadi.Lock()
	talive := p.threadi.alive[tid]
	p.threadi.Unlock()
	return talive
}

func (p *proc_t) sched_add(tf *[TFSIZE]int, tid tid_t) {
	ptid := p.mkptid(tid)
	runtime.Procadd(ptid, tf, p.p_pmap)
}

func (p *proc_t) sched_runnable(tf *[TFSIZE]int, tid tid_t) {
	ptid := p.mkptid(tid)
	runtime.Procrunnable(ptid, tf, p.p_pmap)
}

func (p *proc_t) tid_new() tid_t {
	ret := tid_t(newpid())

	p.threadi.Lock()
	p.threadi.alive[ret] = true
	p.threadi.Unlock()

	return ret
}

func (p *proc_t) thread_count() int {
	p.threadi.Lock()
	ret := len(p.threadi.alive)
	p.threadi.Unlock()
	return ret
}

// remove a particular thread that was never added to scheduler; like when fork
// fails.
func (p *proc_t) thread_del(tid tid_t) {
	p.threadi.Lock()
	ti := &p.threadi
	delete(ti.alive, tid)
	p.threadi.Unlock()
}

// terminate a single thread
func (p *proc_t) thread_dead(tid tid_t, status int, usestatus bool) {
	// XXX exit process if thread is thread0, even if other threads exist
	p.threadi.Lock()
	ti := &p.threadi
	delete(ti.alive, tid)
	destroy := len(ti.alive) == 0

	if usestatus {
		p.exitstatus = status
	}
	p.threadi.Unlock()

	// update rusage user time
	utime := runtime.Proctime(p.mkptid(tid))
	if utime < 0 {
		panic("tid must exist")
	}
	p.atime.utadd(utime)

	// send thread status to thread wait daemon; threads don't have rusage
	// for now.
	cm := childmsg_t{pid: int(tid), status: status}
	p.twaiti.addch <- cm

	runtime.Prockill(p.mkptid(tid))
	if destroy {
		p.terminate()
	}
}

func (p *proc_t) doomall() {
	p.doomed = true
	p.threadi.Lock()
	for t := range p.threadi.alive {
		if runtime.Procnotify(p.mkptid(t)) != 0 {
			panic("pid gone")
		}
	}
	p.threadi.Unlock()
}

// termiante a process. must only be called when the process has no more
// running threads.
func (p *proc_t) terminate() {
	if p.pid == 1 {
		panic("killed init")
	}

	p.threadi.Lock()
	ti := &p.threadi
	if len(ti.alive) != 0 {
		panic("terminate, but threads alive")
	}
	p.threadi.Unlock()

	// close open fds
	p.fdl.Lock()
	for _, fd := range p.fds {
		if file_close(fd.file, fd.perms) != 0 {
			panic("must succeed")
		}
	}
	p.fdl.Unlock()

	//fmt.Printf("%v exited with status %v\n", p.name, p.exitstatus)
	proc_del(p.pid)

	// send status to parent
	if p.pwaiti == nil {
		panic("pwaiti nil")
	}
	// combine total child rusage with ours, send to parent
	na := accnt_t{userns: p.atime.userns, sysns: p.atime.sysns}
	na.add(&p.catime)
	cm := childmsg_t{pid: p.pid, status: p.exitstatus, atime: na}
	p.pwaiti.addch <- cm

	// tell wait daemons to finish
	p.waiti.finish()
	p.twaiti.finish()
	p.perished = true
}

// XXX these can probably go away since user processes share kernel pmaps
// now... (don't switch to kernel pmap, just use the user pmap)

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

// returns a slice whose underlying buffer points to va, which can be
// page-unaligned. the length of the returned slice is (PGSIZE - (va % PGSIZE))
func (p *proc_t) userdmap8_inner(va int) ([]uint8, bool) {
	p.lockassert_pmap()
	uva := va & PGMASK
	voff := va & PGOFFSET
	phys, ok := p.upages[uva]
	if !ok {
		return nil, false
	}
	return dmap8(phys + voff), true
}

func (p *proc_t) userdmap8(va int) ([]uint8, bool) {
	p.Lock_pmap()
	defer p.Unlock_pmap()
	return p.userdmap8_inner(va)
}

// caller must have the vm lock
func (p *proc_t) userdmap_inner(va int) (*[512]int, bool) {
	p.lockassert_pmap()
	uva := va & PGMASK
	phys, ok := p.upages[uva]
	if !ok {
		return nil, false
	}
	return dmap(phys), true
}

// like userdmap8, but returns a pointer to the page
func (p *proc_t) userdmap(va int) (*[512]int, bool) {
	p.Lock_pmap()
	defer p.Unlock_pmap()
	return p.userdmap_inner(va)
}

func (p *proc_t) usermapped(va, n int) bool {
	p.Lock_pmap()
	defer p.Unlock_pmap()

	if n < 0 {
		panic("negative count")
	}
	end := roundup(va + n, PGSIZE)
	for i := rounddown(va, PGSIZE); i < end; i += PGSIZE {
		pn := i & PGMASK
		_, ok := p.upages[pn]
		if !ok {
			return false
		}
	}
	return true
}

func (p *proc_t) userreadn(va, n int) (int, bool) {
	if n > 8 {
		panic("large n")
	}
	var ret int
	var src []uint8
	var ok bool
	for i := 0; i < n; i += len(src) {
		src, ok = p.userdmap8(va + i)
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
	var dst []uint8
	for i := 0; i < n; i += len(dst) {
		v := val >> (8*uint(i))
		t, ok := p.userdmap8(va + i)
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
	i := 0
	var ret []byte
	for {
		str, ok := p.userdmap8(uva + i)
		if !ok {
			return "", false, false
		}
		for _, c := range str {
			if c == 0 {
				return string(ret), true, false
			}
			ret = append(ret, c)
		}
		i += len(str)
		if len(ret) >= lenmax {
			return "", true, true
		}
	}
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
	argmax := 16
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
	curaddr := make([]uint8, 0)
	for !done {
		ptrs, ok := p.userdmap8(uva + uoff)
		if !ok {
			fmt.Printf("dmap\n")
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
	}
	return ret, true
}

// copies src to the user virtual address uva. may copy part of src if uva +
// len(src) is not mapped
func (p *proc_t) usercopy(src []uint8, uva int) bool {
	p.Lock_pmap()
	defer p.Unlock_pmap()
	return p.usercopy_inner(src, uva)
}

func (p *proc_t) usercopy_inner(src []uint8, uva int) bool {
	p.lockassert_pmap()
	cnt := 0
	l := len(src)
	for cnt != l {
		dst, ok := p.userdmap8_inner(uva + cnt)
		if !ok {
			return false
		}
		ub := len(src)
		if ub > len(dst) {
			ub = len(dst)
		}
		for i := 0; i < ub; i++ {
			dst[i] = src[i]
		}
		src = src[ub:]
		cnt += ub
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
	for startva < 256 << 39 {
		found := true
		for i := 0; i < len; i += PGSIZE {
			pte := pmap_walk(p.pmap, startva + i, false, 0,
			    nil)
			if pte != nil && *pte & PTE_P != 0 {
				found = false
				startva += i + PGSIZE
				break
			}
		}
		if found {
			return startva
		}
	}
	panic("no addr space left")
	return 0
}

func mp_sum(d []uint8) int {
	ret := 0
	for _, c := range d {
		ret += int(c)
	}
	return ret & 0xff
}

func mp_tblget(f []uint8) ([]uint8, []uint8, []uint8) {
	feat := readn(f, 1, 11)
	if feat != 0 {
		panic("\"default\" configurations not supported")
	}
	ctbla := readn(f, 4, 4)
	c := dmap8(ctbla)
	sig := readn(c, 4, 0)
	// sig is "PCMP"
	if sig != 0x504d4350 {
		panic("bad conf table sig")
	}

	bsz := readn(c, 2, 4)
	base := c[:bsz]
	if mp_sum(base) != 0 {
		panic("bad conf table cksum")
	}

	esz := readn(c, 2, 0x28)
	eck := readn(c, 1, 0x2a)
	extended := c[bsz : bsz + esz]

	esum := (mp_sum(extended) + eck) & 0xff
	if esum != 0 {
		panic("bad extended table checksum")
	}

	return f, base, extended
}

func mp_scan() ([]uint8, []uint8, []uint8) {
	assert_no_phys(kpmap(), 0)

	// should use ACPI. don't bother to scan other areas as recommended by
	// MP spec.
	// try bios readonly memory, from 0xe0000-0xfffff
	bro := 0xe0000
	const brolen = 0x1ffff
	p := (*[brolen]uint8)(unsafe.Pointer(dmap(bro)))
	for i := 0; i < brolen - 4; i++ {
		if p[i] == '_' &&
		   p[i+1] == 'M' &&
		   p[i+2] == 'P' &&
		   p[i+3] == '_' && mp_sum(p[i:i+16]) == 0 {
			return mp_tblget(p[i:i+16])
		}
	}
	return nil, nil, nil
}

type mpcpu_t struct {
	lapid    int
	bsp      bool
}

func cpus_find() []mpcpu_t {

	fl, base, _ := mp_scan()
	if fl == nil {
		fmt.Println("uniprocessor")
		return nil
	}

	ret := make([]mpcpu_t, 0, 10)

	// runtime does the same check
	lapaddr := readn(base, 4, 0x24)
	if lapaddr != 0xfee00000 {
		panic(fmt.Sprintf("weird lapic addr %#x", lapaddr))
	}

	// switch to symmetric mode interrupts if we are in PIC mode
	mpfeat := readn(fl, 4, 12)
	imcrp := 1 << 7
	if mpfeat & imcrp != 0 {
		fmt.Printf("entering symmetric mode\n")
		// select imcr
		runtime.Outb(0x22, 0x70)
		// "writing a value of 01h forces the NMI and 8259 INTR signals
		// to pass through the APIC."
		runtime.Outb(0x23, 0x1)
	}

	ecount := readn(base, 2, 0x22)
	entries := base[44:]
	idx := 0
	for i := 0; i < ecount; i++ {
		t := readn(entries, 1, idx)
		switch t {
		case 0:		// processor
			idx += 20
			lapid  := readn(entries, 1, idx + 1)
			cpuf   := readn(entries, 1, idx + 3)
			cpu_en := 1
			cpu_bp := 2

			bsp := false
			if cpuf & cpu_bp != 0 {
				bsp = true
			}

			if cpuf & cpu_en == 0 {
				fmt.Printf("cpu %v disabled\n", lapid)
				continue
			}

			ret = append(ret, mpcpu_t{lapid, bsp})

		default:	// bus, IO apic, IO int. assignment, local int
				// assignment.
			idx += 8
		}
	}
	return ret
}

func cpus_stack_init(apcnt int, stackstart int) {
	for i := 0; i < apcnt; i++ {
		kmalloc(stackstart, PTE_W)
		stackstart += PGSIZE
		assert_no_va_map(kpmap(), stackstart)
		stackstart += PGSIZE
	}
}

func cpus_start(aplim int) {
	runtime.GOMAXPROCS(1 + aplim)
	cpus := cpus_find()
	apcnt := len(cpus) - 1

	// XXX how to determine logical cpus, not cores from mp table? my test
	// hardware has 8 logical cpus and 4 cores but there are only 4 entries
	// in the mp table...

	fmt.Printf("found %v CPUs\n", len(cpus))

	if apcnt == 0 {
		fmt.Printf("uniprocessor with mpconf\n")
		return
	}

	// AP code must be between 0-1MB because the APs are in real mode. load
	// code to 0x8000 (overwriting bootloader)
	mpaddr := 0x8000
	mppg := dmap(mpaddr)
	for i := range mppg {
		mppg[i] = 0
	}
	mpcode := allbins["mpentry.bin"].data
	runtime.Memmove(unsafe.Pointer(mppg), unsafe.Pointer(&mpcode[0]),
	    len(mpcode))

	// skip mucking with CMOS reset code/warm reset vector (as per the the
	// "universal startup algoirthm") and instead use the STARTUP IPI which
	// is supported by lapics of version >= 1.x. (the runtime panicks if a
	// lapic whose version is < 1.x is found, thus assume their absence).
	// however, only one STARTUP IPI is accepted after a CPUs RESET or INIT
	// pin is asserted, thus we need to send an INIT IPI assert first (it
	// appears someone already used a STARTUP IPI; probably the BIOS).

	lapaddr := 0xfee00000
	pte := pmap_walk(kpmap(), lapaddr, false, 0, nil)
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
		vec       := mpaddr >> 12
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

	ss := (*[11]int)(unsafe.Pointer(uintptr(0x7c00)))
	sap_entry := 3
	sgdt      := 4
	sidt      := 6
	sapcnt    := 8
	sstacks   := 9
	sproceed  := 10
	ss[sap_entry] = runtime.Fnaddri(ap_entry)
	// sgdt and sidt save 10 bytes
	runtime.Sgdt(&ss[sgdt])
	runtime.Sidt(&ss[sidt])
	ss[sapcnt] = 0
	// the top of bsp's interrupt stack is 0xa100001000. map an interrupt
	// stack for each ap. leave 0xa100001000 as a guard page.
	stackstart := 0xa100002000
	ss[sstacks] = stackstart   // each ap grabs a unique stack
	ss[sproceed] = 0

	// XXX make sure secret storage values are not in store buffer
	dummy := int64(0)
	atomic.CompareAndSwapInt64(&dummy, 0, 10)

	initipi(true)
	// not necessary since we assume lapic version >= 1.x (ie not 82489DX)
	//initipi(false)
	cdelay(1)

	startupipi()
	cdelay(1)
	startupipi()

	// wait a while for hopefully all APs to join. it'd be better to use
	// ACPI to determine the correct count of CPUs and then wait for them
	// all to join.
	cdelay(500)
	apcnt = ss[sapcnt]
	if apcnt > aplim {
		apcnt = aplim
	}
	numcpus = apcnt + 1

	// actually  map the stacks for the CPUs that joined
	cpus_stack_init(apcnt, stackstart)

	// tell the cpus to carry on
	ss[sproceed] = apcnt

	fmt.Printf("done! %v APs found (%v joined)\n", ss[sapcnt], apcnt)
}

// myid is a logical id, not lapic id
//go:nosplit
func ap_entry(myid int) {

	// myid starts from 1
	runtime.Ap_setup(myid)

	lid := lap_id()
	if lid > maxcpus || lid < 0 {
		runtime.Pnum(0xb1dd1e)
		for {
		}
	}
	cpus[lid].num = myid

	// ints are still cleared
	runtime.Procyield()
}

// since this function is used when interrupts are cleared, it must be marked
// nosplit. otherwise we may GC and then resume in some other goroutine with
// interrupts cleared, thus interrupts would never be cleared.
//go:nosplit
func poutb(reg int, val int) {
	runtime.Outb(reg, val)
	cdelay(1)
}

func p8259_init() {
	// the piix3 provides two 8259 compatible pics. the runtime masks all
	// irqs for us.
	pic1 := 0x20
	pic1d := pic1 + 1
	pic2 := 0xa0
	pic2d := pic2 + 1

	runtime.Cli()

	// master pic
	// start icw1: icw4 required
	poutb(pic1, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	poutb(pic1d, IRQ_BASE)
	// icw3, cascaded mode
	poutb(pic1d, 4)
	// icw4, auto eoi, intel arch mode.
	poutb(pic1d, 3)

	// slave pic
	// start icw1: icw4 required
	poutb(pic2, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	poutb(pic2d, IRQ_BASE + 8)
	// icw3, slave identification code
	poutb(pic2d, 2)
	// icw4, auto eoi, intel arch mode.
	poutb(pic2d, 3)

	// ocw3, enable "special mask mode" (??)
	poutb(pic1, 0x68)
	// ocw3, read irq register
	poutb(pic1, 0x0a)

	// ocw3, enable "special mask mode" (??)
	poutb(pic2, 0x68)
	// ocw3, read irq register
	poutb(pic2, 0x0a)

	// enable slave 8259
	irq_unmask(2)
	// all irqs go to CPU 0 until redirected

	runtime.Sti()
}

var intmask uint 	= 0xffff

func irq_unmask(irq int) {
	if irq < 0 || irq >= 16 {
		panic("weird irq")
	}
	pic1 := 0x20
	pic1d := pic1 + 1
	pic2 := 0xa0
	pic2d := pic2 + 1
	intmask = intmask & ^(1 << uint(irq))
	dur := int(intmask)
	runtime.Outb(pic1d, dur)
	runtime.Outb(pic2d, dur >> 8)
}

var amqemu	bool = false

func qemuconfig() {
	// XXX will think AMD machines are qemu...
	_, _, ecx, _  := runtime.Cpuid(0, 0)
	amqemu = ecx == 0x444d4163
	if !amqemu {
		return
	}
	fmt.Printf("I am Qemu\n")
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
	go kbd_daemon(&cons, km)
	irq_unmask(IRQ_KBD)
	irq_unmask(IRQ_COM1)

	// make sure kbd int is clear
	runtime.Inb(0x60)
}

type cons_t struct {
	kbd_int		chan bool
	com_int		chan bool
	reader		chan []byte
	reqc		chan int
}

var cons	= cons_t{}

func kbd_daemon(cons *cons_t, km map[int]byte) {
	inb := runtime.Inb
	kready := func() bool {
		ibf := 1 << 0
		st := inb(0x64)
		if st & ibf == 0 {
			//panic("no kbd data?")
			return false
		}
		return true
	}
	comready := func() bool {
		com1ctl := 0x3f8 + 5
		b := inb(com1ctl)
		if b & 0x01 == 0 {
			return false
		}
		return true
	}
	start := make([]byte, 0, 10)
	data := start
	addprint := func(c byte) {
		fmt.Printf("%c", c)
		data = append(data, c)
	}
	var reqc chan int
	for {
		select {
		case <- cons.kbd_int:
			if !kready() {
				continue
			}
			sc := inb(0x60)
			c, ok := km[sc]
			if ok {
				addprint(c)
			}
		case <- cons.com_int:
			if !comready() {
				continue
			}
			com1data := 0x3f8 + 0
			sc := inb(com1data)
			c := byte(sc)
			if c == '\r' {
				c = '\n'
			} else if c == 127 {
				// delete -> backspace
				c = '\b'
			}
			addprint(c)
		case l := <- reqc:
			if l > len(data) {
				l = len(data)
			}
			s := data[0:l]
			cons.reader <- s
			data = data[l:]
		}
		if len(data) == 0 {
			reqc = nil
			data = start
		} else {
			reqc = cons.reqc
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

func attach_devs() {
	pcibus_attach()
}

func tlb_shootdown(p_pmap, va, pgcount int) {
	if numcpus == 1 {
		return
	}
	othercpus := numcpus - 1
	runtime.Tlbadmit(p_pmap, othercpus, va, pgcount)

	lapaddr := 0xfee00000
	pte := pmap_walk(kpmap(), lapaddr, false, 0, nil)
	if pte == nil || *pte & PTE_P == 0 || *pte & PTE_PCD == 0 {
		panic("lapaddr unmapped")
	}
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
	low := ipilow(3, 0, tlbshootvec)
	icrw(0, low)

	// wait for other cpus to finish
	runtime.Tlbadmit(0, 0, 0, 0)
}

type bprof_t struct {
	data	[]byte
	c	int
}

func (b *bprof_t) init() {
	b.data = make([]byte, 0)
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
	l := len(b.data)
	for i := 0; i < l; i += 16 {
		cur := b.data[i:]
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

func main() {
	// magic loop
	//if rand.Int() != 0 {
	//	for {
	//	}
	//}

	//chanbm()

	//findbm()

	// control CPUs
	aplim := 7

	qemuconfig()

	dmap_init()
	p8259_init()

	//pci_dump()
	attach_devs()

	if disk == nil || INT_DISK < 0 {
		panic("no disk")
	}

	// XXX pass irqs from attach_devs to trapstub, not global state.
	// must come before init funcs below
	runtime.Install_traphandler(trapstub)

	trap_diex := func(c int) func(*trapstore_t) {
		return func(ts *trapstore_t) {
			fmt.Printf("[death on trap %v]\n", c)
			panic("perished")
		}
	}

	handlers := map[int]func(*trapstore_t) {
	     DIVZERO: trap_divzero,
	     GPFAULT: trap_diex(GPFAULT),
	     PGFAULT: trap_pgfault,
	     SYSCALL: trap_syscall,
	     INT_DISK: trap_disk,
	     INT_KBD: trap_cons,
	     INT_COM1: trap_cons,
	     }
	go trap(handlers)

	cpus_start(aplim)
	//runtime.SCenable = false

	rf := fs_init()
	//use_memfs()
	kbd_init()

	runtime.Resetgcticks()

	exec := func(cmd string, args []string) {
		fmt.Printf("start [%v %v]\n", cmd, args)
		nargs := []string{cmd}
		nargs = append(nargs, args...)
		p := proc_new(cmd, rf)
		n := p.atime.now()
		var tf [TFSIZE]int
		ret := sys_execv1(p, &tf, cmd, nargs)
		if ret != 0 {
			panic(fmt.Sprintf("exec failed %v", ret))
		}
		p.atime.finish(n)
		p.sched_add(&tf, p.tid0)
	}

	//exec("bin/bmgc2", []string{"100000000"})
	//exec("bin/bmgc2", []string{"10"})
	//exec("bin/mail-qman", []string{"/mail/spool", "/mail", "1"})
	exec("bin/lsh", []string{})
	//exec("bin/usertests", []string{})
	//exec("bin/pipetest", []string{})
	//exec("bin/ls", []string{})

	//ide_test()

	var dur chan bool
	<- dur
	//fake_work()
}

func incn() int {
	blkno := 31337
	b := bread(blkno)
	n := readn(b.buf.data[:], 8, 0)
	ret := n
	n++
	writen(b.buf.data[:], 8, 0, n)
	b.writeback()
	fmt.Printf("n is now %d\n", n)
	return ret
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
	aplim := 7
	cpus_start(aplim)

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

func ide_test() {
	req := idereq_new(0, false, nil)
	ide_request <- req
	<- req.ack
	fmt.Printf("read of block %v finished!\n", req.buf.block)
	for _, c := range req.buf.data {
		fmt.Printf("%x ", c)
	}
	fmt.Printf("\n")
	fmt.Printf("will overwrite block %v now...\n", req.buf.block)
	req.write = true
	for i := range req.buf.data {
		req.buf.data[i] = 0xcc
	}
	ide_request <- req
	<- req.ack
	fmt.Printf("done!\n")
}

func fake_work() {
	fmt.Printf("'network' test\n")
	ch := make(chan packet)
	go genpackets(ch)

	process_packets(ch)
}

type packet struct {
	ipaddr 		int
	payload		int
}

func genpackets(ch chan packet) {
	for {
		n := packet{rand.Intn(1000), 0}
		ch <- n
	}
}

func spawnsend(p packet, outbound map[int]chan packet, f func(chan packet)) {
	pri := p_priority(p)
	if ch, ok := outbound[pri]; ok {
		ch <- p
	} else {
		pch := make(chan packet)
		outbound[pri] = pch
		go f(pch)
		pch <- p
	}
}

func p_priority(p packet) int {
	return p.ipaddr / 100
}

func process_packets(in chan packet) {
	outbound := make(map[int]chan packet)

	for {
		p := <- in
		spawnsend(p, outbound, ip_process)
	}
}

func ip_process(ipchan chan packet) {
	for {
		p := <- ipchan
		if rand.Intn(1000) == 1 {
			fmt.Printf("%v ", p_priority(p))
		}
	}
}

func chanbm() {
	//dmap_init()
	//cpus_start(1)
	//runtime.GOMAXPROCS(2)

	for i := range workytown {
		workytown[i] = rand.Int()
	}

	bs := 1
	for {
		st := runtime.Rdtsc()
		chanit(bs)
		elapsed := runtime.Rdtsc() - st
		fmt.Printf("channel cycles: %20v (bs %v)\n", elapsed, bs)
		bs *= 10

		st = runtime.Rdtsc()
		lockit()
		elapsed = runtime.Rdtsc() - st
		fmt.Printf("lock cycles:    %20v\n", elapsed)
		workytown[0] = workyans
	}
}

type msg_t struct {
	a	int
	//b	int
}

var niters int = 100000

func chanit(bs int) {
	a := msg_t{10}
	if bs > niters {
		bs = niters
	}
	bgo := make(chan msg_t, bs)
	bdone := make(chan msg_t, bs)
	go chand(bgo, bdone)

	for i := 0; i < niters/bs; i++ {
		for i := 0; i < bs; i++ {
			bgo <- a
		}
		for i := 0; i < bs; i++ {
			a = <- bdone
			workytown[0] = a.a
		}
	}
	close(bgo)
	close(bdone)
}

func chand(bgo chan msg_t, bdone chan msg_t) {
	for {
		a, ok := <- bgo
		if !ok {
			break
		}
		workytown[0] = a.a
		work()
		bdone <- a
	}
}

var block = sync.Mutex{}

func lockit() {
	for i := 0; i < niters; i++ {
		block.Lock()
		work()
		block.Unlock()
	}
}

var workytown = make([]int, 512)
var workyans int

func work() {
	ret := 0
	for i, c := range workytown {
		ret += i ^ c
		workytown[i] = ret
	}
	workyans = ret
}
