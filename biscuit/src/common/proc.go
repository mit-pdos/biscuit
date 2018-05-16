package common

//import "sync/atomic"
import "sync"
//import "strings"
import "fmt"
import "time"
import "unsafe"
import "runtime"

type Tnote_t struct {
	// XXX "alive" should be "terminated"
	proc	*Proc_t
	alive bool
	killed bool
	// protects killed, Killnaps.Cond and Kerr, and is a leaf lock
	sync.Mutex
	Killnaps struct {
		Killch	chan bool
		Cond	*sync.Cond
		Kerr	Err_t
	}
}

type Threadinfo_t struct {
	Notes map[Tid_t]*Tnote_t
	sync.Mutex
}

func (t *Threadinfo_t) init() {
	t.Notes = make(map[Tid_t]*Tnote_t)
}

// per-process limits
type Ulimit_t struct {
	Pages  int
	Nofile uint
	Novma  uint
	Noproc uint
}

type Tid_t int

type Proc_t struct {
	Pid int
	// first thread id
	tid0 Tid_t
	Name string

	// waitinfo for my child processes
	Mywait Wait_t
	// waitinfo of my parent
	Pwait *Wait_t

	// thread tids of this process
	Threadi Threadinfo_t

	// lock for vmregion, pmpages, pmap, and p_pmap
	pgfl sync.Mutex

	Vmregion Vmregion_t

	// pmap pages
	Pmap   *Pmap_t
	P_pmap Pa_t

	// mmap next virtual address hint
	Mmapi int

	// a process is marked doomed when it has been killed but may have
	// threads currently running on another processor
	pgfltaken  bool
	doomed     bool
	exitstatus int

	Fds []*Fd_t
	// where to start scanning for free fds
	fdstart int
	// fds, fdstart, nfds protected by fdl
	Fdl sync.Mutex
	// number of valid file descriptors
	nfds int

	cwd *Fd_t
	// to serialize chdirs
	Cwdl sync.Mutex
	Ulim Ulimit_t

	// this proc's rusage
	Atime Accnt_t
	// total child rusage
	Catime Accnt_t

	syscall Syscall_i
	// no thread can read/write Oomlink except the OOM killer
	Oomlink	*Proc_t
}

var Allprocs = make(map[int]*Proc_t, Syslimit.Sysprocs)

func (p *Proc_t) Tid0() Tid_t {
	return p.tid0
}

func (p *Proc_t) Doomed() bool {
	return p.doomed
}

func (p *Proc_t) Cwd() *Fd_t {
	return p.cwd
}

func (p *Proc_t) Set_cwd(cwd *Fd_t) {
	p.cwd = cwd
}

// an fd table invariant: every fd must have its file field set. thus the
// caller cannot set an fd's file field without holding fdl. otherwise you will
// race with a forking thread when it copies the fd table.
func (p *Proc_t) Fd_insert(f *Fd_t, perms int) (int, bool) {
	p.Fdl.Lock()
	a, b := p.fd_insert_inner(f, perms)
	p.Fdl.Unlock()
	return a, b
}

func (p *Proc_t) fd_insert_inner(f *Fd_t, perms int) (int, bool) {

	if uint(p.nfds) >= p.Ulim.Nofile {
		return -1, false
	}
	// find free fd
	newfd := p.fdstart
	found := false
	for newfd < len(p.Fds) {
		if p.Fds[newfd] == nil {
			p.fdstart = newfd + 1
			found = true
			break
		}
		newfd++
	}
	if !found {
		// double size of fd table
		ol := len(p.Fds)
		nl := 2 * ol
		if p.Ulim.Nofile != RLIM_INFINITY && nl > int(p.Ulim.Nofile) {
			nl = int(p.Ulim.Nofile)
			if nl < ol {
				panic("how")
			}
		}
		nfdt := make([]*Fd_t, nl, nl)
		copy(nfdt, p.Fds)
		p.Fds = nfdt
	}
	fdn := newfd
	fd := f
	fd.Perms = perms
	if p.Fds[fdn] != nil {
		panic(fmt.Sprintf("new fd exists %d", fdn))
	}
	p.Fds[fdn] = fd
	if fd.Fops == nil {
		panic("wtf!")
	}
	p.nfds++
	return fdn, true
}

// returns the fd numbers and success
func (p *Proc_t) Fd_insert2(f1 *Fd_t, perms1 int,
	f2 *Fd_t, perms2 int) (int, int, bool) {
	p.Fdl.Lock()
	defer p.Fdl.Unlock()
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
func (p *Proc_t) Fd_get_inner(fdn int) (*Fd_t, bool) {
	if fdn < 0 || fdn >= len(p.Fds) {
		return nil, false
	}
	ret := p.Fds[fdn]
	ok := ret != nil
	return ret, ok
}

func (p *Proc_t) Fd_get(fdn int) (*Fd_t, bool) {
	p.Fdl.Lock()
	ret, ok := p.Fd_get_inner(fdn)
	p.Fdl.Unlock()
	return ret, ok
}

// fdn is not guaranteed to be a sane fd
func (p *Proc_t) Fd_del(fdn int) (*Fd_t, bool) {
	p.Fdl.Lock()
	a, b := p.fd_del_inner(fdn)
	p.Fdl.Unlock()
	return a, b
}

func (p *Proc_t) fd_del_inner(fdn int) (*Fd_t, bool) {
	if fdn < 0 || fdn >= len(p.Fds) {
		return nil, false
	}
	ret := p.Fds[fdn]
	p.Fds[fdn] = nil
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
func (p *Proc_t) Fd_dup(ofdn, nfdn int) (*Fd_t, bool, Err_t) {
	if ofdn == nfdn {
		return nil, false, 0
	}

	p.Fdl.Lock()
	defer p.Fdl.Unlock()

	ofd, ok := p.Fd_get_inner(ofdn)
	if !ok {
		return nil, false, -EBADF
	}
	cpy, err := Copyfd(ofd)
	if err != 0 {
		return nil, false, err
	}
	cpy.Perms &^= FD_CLOEXEC
	rfd, needclose := p.Fd_get_inner(nfdn)
	p.Fds[nfdn] = cpy

	return rfd, needclose, 0
}

// returns whether the parent's TLB should be flushed and whether the we
// successfully copied the parent's address space.
func (parent *Proc_t) Vm_fork(child *Proc_t, rsp uintptr) (bool, bool) {
	parent.Lockassert_pmap()
	// first add kernel pml4 entries
	for _, e := range Kents {
		child.Pmap[e.Pml4slot] = e.Entry
	}
	// recursive mapping
	child.Pmap[VREC] = child.P_pmap | PTE_P | PTE_W

	failed := false
	doflush := false
	child.Vmregion = parent.Vmregion.copy()
	parent.Vmregion.iter(func(vmi *Vminfo_t) {
		start := int(vmi.pgn << PGSHIFT)
		end := start + int(vmi.pglen<<PGSHIFT)
		ashared := vmi.mtype == VSANON
		fl, ok := ptefork(child.Pmap, parent.Pmap, start, end, ashared)
		failed = failed || !ok
		doflush = doflush || fl
	})

	if failed {
		return doflush, false
	}

	// don't mark stack COW since the parent/child will fault their stacks
	// immediately
	vmi, ok := child.Vmregion.Lookup(rsp)
	// give up if we can't find the stack
	if !ok {
		return doflush, true
	}
	pte, ok := vmi.ptefor(child.Pmap, rsp)
	if !ok || *pte&PTE_P == 0 || *pte&PTE_U == 0 {
		return doflush, true
	}
	// sys_pgfault expects pmap to be locked
	child.Lock_pmap()
	perms := uintptr(PTE_U | PTE_W)
	if Sys_pgfault(child, vmi, rsp, perms) != 0 {
		return doflush, false
	}
	child.Unlock_pmap()
	vmi, ok = parent.Vmregion.Lookup(rsp)
	if !ok || *pte&PTE_P == 0 || *pte&PTE_U == 0 {
		panic("child has stack but not parent")
	}
	pte, ok = vmi.ptefor(parent.Pmap, rsp)
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
func (p *Proc_t) _mkvmi(mt mtype_t, start, len int, perms Pa_t, foff int,
	fops Fdops_i, unpin Unpin_i) *Vminfo_t {
	if len <= 0 {
		panic("bad vmi len")
	}
	if Pa_t(start|len)&PGOFFSET != 0 {
		panic("start and len must be aligned")
	}
	// don't specify cow, present etc. -- page fault will handle all that
	pm := PTE_W | PTE_COW | PTE_WASCOW | PTE_PS | PTE_PCD | PTE_P | PTE_U
	if r := perms & pm; r != 0 && r != PTE_U && r != (PTE_W|PTE_U) {
		panic("bad perms")
	}
	ret := &Vminfo_t{}
	pgn := uintptr(start) >> PGSHIFT
	pglen := Roundup(len, PGSIZE) >> PGSHIFT
	ret.mtype = mt
	ret.pgn = pgn
	ret.pglen = pglen
	ret.perms = uint(perms)
	if mt == VFILE {
		ret.file.foff = foff
		ret.file.mfile = &Mfile_t{}
		ret.file.mfile.mfops = fops
		ret.file.mfile.unpin = unpin
		ret.file.mfile.mapcount = pglen
		ret.file.shared = unpin != nil
	}
	return ret
}

func (p *Proc_t) Vmadd_anon(start, len int, perms Pa_t) {
	vmi := p._mkvmi(VANON, start, len, perms, 0, nil, nil)
	p.Vmregion.insert(vmi)
}

func (p *Proc_t) Vmadd_file(start, len int, perms Pa_t, fops Fdops_i,
	foff int) {
	vmi := p._mkvmi(VFILE, start, len, perms, foff, fops, nil)
	p.Vmregion.insert(vmi)
}

func (p *Proc_t) Vmadd_shareanon(start, len int, perms Pa_t) {
	vmi := p._mkvmi(VSANON, start, len, perms, 0, nil, nil)
	p.Vmregion.insert(vmi)
}

func (p *Proc_t) Vmadd_sharefile(start, len int, perms Pa_t, fops Fdops_i,
	foff int, unpin Unpin_i) {
	vmi := p._mkvmi(VFILE, start, len, perms, foff, fops, unpin)
	p.Vmregion.insert(vmi)
}

func (p *Proc_t) Mkuserbuf(userva, len int) *Userbuf_t {
	ret := &Userbuf_t{}
	ret.ub_init(p, userva, len)
	return ret
}

var Ubpool = sync.Pool{New: func() interface{} { return new(Userbuf_t) }}

func (p *Proc_t) mkfxbuf() *[64]uintptr {
	ret := new([64]uintptr)
	n := uintptr(unsafe.Pointer(ret))
	if n&((1<<4)-1) != 0 {
		panic("not 16 byte aligned")
	}
	*ret = runtime.Fxinit
	return ret
}

// the first return value is true if a present mapping was modified (i.e. need
// to flush TLB). the second return value is false if the page insertion failed
// due to lack of user pages. p_pg's ref count is increased so the caller can
// simply Physmem.Refdown()
func (p *Proc_t) Page_insert(va int, p_pg Pa_t, perms Pa_t,
	vempty bool) (bool, bool) {
	return p._page_insert(va, p_pg, perms, vempty, true)
}

// the first return value is true if a present mapping was modified (i.e. need
// to flush TLB). the second return value is false if the page insertion failed
// due to lack of user pages. p_pg's ref count is increased so the caller can
// simply Physmem.Refdown()
func (p *Proc_t) Blockpage_insert(va int, p_pg Pa_t, perms Pa_t,
	vempty bool) (bool, bool) {
	return p._page_insert(va, p_pg, perms, vempty, false)
}

func (p *Proc_t) _page_insert(va int, p_pg Pa_t, perms Pa_t,
	vempty, refup bool) (bool, bool) {
	p.Lockassert_pmap()
	if refup {
		Physmem.Refup(p_pg)
	}
	pte, err := pmap_walk(p.Pmap, va, PTE_U|PTE_W)
	if err != 0 {
		return false, false
	}
	ninval := false
	var p_old Pa_t
	if *pte&PTE_P != 0 {
		if vempty {
			panic("pte not empty")
		}
		if *pte&PTE_U == 0 {
			panic("replacing kernel page")
		}
		ninval = true
		p_old = Pa_t(*pte & PTE_ADDR)
	}
	*pte = p_pg | perms | PTE_P
	if ninval {
		Physmem.Refdown(p_old)
	}
	return ninval, true
}

func (p *Proc_t) Page_remove(va int) bool {
	p.Lockassert_pmap()
	remmed := false
	pte := Pmap_lookup(p.Pmap, va)
	if pte != nil && *pte&PTE_P != 0 {
		if *pte&PTE_U == 0 {
			panic("removing kernel page")
		}
		p_old := Pa_t(*pte & PTE_ADDR)
		Physmem.Refdown(p_old)
		*pte = 0
		remmed = true
	}
	return remmed
}

// returns true if the pagefault was handled successfully
func (p *Proc_t) pgfault(tid Tid_t, fa, ecode uintptr) Err_t {
	p.Lock_pmap()
	vmi, ok := p.Vmregion.Lookup(fa)
	if !ok {
		p.Unlock_pmap()
		return -EFAULT
	}
	ret := Sys_pgfault(p, vmi, fa, ecode)
	p.Unlock_pmap()
	return ret
}

// flush TLB on all CPUs that may have this processes' pmap loaded
func (p *Proc_t) Tlbflush() {
	// this flushes the TLB for now
	p.Tlbshoot(0, 2)
}

func (p *Proc_t) Tlbshoot(startva uintptr, pgcount int) {
	if pgcount == 0 {
		return
	}
	p.Lockassert_pmap()
	// fast path: the pmap is loaded in exactly one CPU's cr3, and it
	// happens to be this CPU. we detect that one CPU has the pmap loaded
	// by a pmap ref count == 2 (1 for Proc_t ref, 1 for CPU).
	p_pmap := p.P_pmap
	refp, _ := _refaddr(p_pmap)
	if runtime.Condflush(refp, uintptr(p_pmap), startva, pgcount) {
		return
	}
	// slow path, must send TLB shootdowns
	tlb_shootdown(uintptr(p.P_pmap), startva, pgcount)
}

func (p *Proc_t) resched(tid Tid_t, n *Tnote_t) bool {
	talive := n.alive
	if talive && p.doomed {
		// although this thread is still alive, the process should
		// terminate
		p.Reap_doomed(tid)
		return false
	}
	return talive
}

// blocks until memory is available or returns false if this process has been
// killed and should terminate.
func Resbegin(c int) bool {
	if !Kernel {
		return true
	}
	r := _reswait(c, false, true)
	//if !r {
	//	fmt.Printf("Slain!\n")
	//}
	return r
}

var Kernel bool

var Resfail = Distinct_caller_t{
	Enabled: true,
	Whitel: map[string]bool{
		// XXX these need to be fixed to handle ENOHEAP
		"fs.(*Fs_t).Fs_rename": true,
		"main.(*susfops_t)._fdrecv": true,
		},
}

const resfail = false

func Resremain() int {
	ret := runtime.Memremain()
	if ret < 0 {
		// not an error; outstanding res may increase pass max if our
		// read raced with a relase, but live should never surpass max
		ret = 0
	}
	return ret
}

// blocks until memory is available or returns false if this process has been
// killed and should terminate.
func Resadd(c int) bool {
	if !Kernel {
		return true
	}
	if resfail {
		if ok, path := Resfail.Distinct(); ok {
			fmt.Printf("failing: %s\n", path)
			return false
		}
	}
	r := _reswait(c, true, true)
	//if !r {
	//	fmt.Printf("Slain!\n")
	//}
	return r
}

// for reservations when locks may be held; the caller should abort and retry.
func Resadd_noblock(c int) bool {
	if !Kernel {
		return true
	}
	if resfail {
		if ok, path := Resfail.Distinct(); ok {
			fmt.Printf("failing: %s\n", path)
			return false
		}
	}
	return _reswait(c, true, false)
}

func Resend() {
	if !Kernel {
		return
	}
	//if !Lims {
	//	return
	//}
	runtime.Memunres()
}

func Human(_bytes int) string {
	bytes := float64(_bytes)
	div := float64(1)
	order := 0
	for bytes / div > 1024 {
		div *= 1024
		order++
	}
	sufs := map[int]string{0: "B", 1: "kB", 2: "MB", 3: "GB", 4: "TB",
	    5: "PB"}
	return fmt.Sprintf("%.2f%s", float64(bytes) / div, sufs[order])
}

//var lastp time.Time

func _reswait(c int, incremental, block bool) bool {
	//if !Lims {
	//	return true
	//}
	f := runtime.Memreserve
	if incremental {
		f = runtime.Memresadd
	}
	for !f(c) {
		//if time.Since(lastp) > time.Second {
		//	fmt.Printf("RES failed %v\n", c)
		//	Callerdump(2)
		//}
		p := Current().proc
		if p.Doomed() {
			return false
		}
		if !block {
			return false
		}
		//fmt.Printf("%v: Wait for memory hog to die...\n", p.Name)
		var omsg oommsg_t
		omsg.need = 2 << 20
		omsg.resume = make(chan bool, 1)
		select {
		case Oom.halp <- omsg:
		case <-Current().Killnaps.Killch:
			return false
		}
		select {
		case <-omsg.resume:
		case <-Current().Killnaps.Killch:
			return false
		}
	}
	return true
}

// returns non-zero if this calling process has been killed and the caller
// should finish the system call.
func KillableWait(cond *sync.Cond) Err_t {
	mynote := Current()

	// ensure the sleep is atomic w.r.t. killed flag and kn.Cond writes
	// from killer
	mynote.Lock()
	if mynote.killed {
		ret := mynote.Killnaps.Kerr
		mynote.Unlock()
		if ret == 0 {
			panic("must be non-zero")
		}
		return ret
	}

	mynote.Killnaps.Cond = cond
	// WaitWith() unlocks mynote after adding us to sleep queue. neat huh?
	cond.WaitWith(mynote)
	return mynote.Killnaps.Kerr
}

// returns true if the kernel may safely use a "fast" resume and whether the
// system call should be restarted.
func (p *Proc_t) trap_proc(tf *[TFSIZE]uintptr, tid Tid_t, intno, aux int) (bool, bool) {
	fastret := false
	restart := false
	switch intno {
	case SYSCALL:
		// fast return doesn't restore the registers used to
		// specify the arguments for libc _entry(), so do a
		// slow return when returning from sys_execv().
		sysno := tf[TF_RAX]
		if sysno != SYS_EXECV {
			fastret = true
		}
		ret := p.syscall.Syscall(p, tid, tf)
		restart = ret == int(-ENOHEAP)
		if !restart {
			tf[TF_RAX] = uintptr(ret)
		}

	case TIMER:
		//fmt.Printf(".")
		runtime.Gosched()
	case PGFAULT:
		faultaddr := uintptr(aux)
		err := p.pgfault(tid, faultaddr, tf[TF_ERROR])
		restart = err == -ENOHEAP
		if err != 0 && !restart {
			fmt.Printf("*** fault *** %v: addr %x, "+
				"rip %x, err %v. killing...\n", p.Name, faultaddr,
				tf[TF_RIP], err)
			p.syscall.Sys_exit(p, tid, SIGNALED|Mkexitsig(11))
		}
	case DIVZERO, GPFAULT, UD:
		fmt.Printf("%s -- TRAP: %v, RIP: %x\n", p.Name, intno,
			tf[TF_RIP])
		p.syscall.Sys_exit(p, tid, SIGNALED|Mkexitsig(4))
	case TLBSHOOT, PERFMASK, INT_KBD, INT_COM1, INT_MSI0,
		INT_MSI1, INT_MSI2, INT_MSI3, INT_MSI4, INT_MSI5, INT_MSI6,
		INT_MSI7:
		// XXX: shouldn't interrupt user program execution...
	default:
		panic(fmt.Sprintf("weird trap: %d", intno))
	}
	return fastret, restart
}

func (p *Proc_t) run(tf *[TFSIZE]uintptr, tid Tid_t) {

	p.Threadi.Lock()
	mynote, ok := p.Threadi.Notes[tid]
	p.Threadi.Unlock()
	// each thread removes itself from threadi.Notes; thus mynote must
	// exist
	if !ok {
		panic("note must exist")
	}
	SetCurrent(mynote)

	var fxbuf *[64]uintptr
	const runonly = 14 << 10
	if Resbegin(runonly) {
		// could allocate fxbuf lazily
		fxbuf = p.mkfxbuf()
	}

	fastret := false
	for p.resched(tid, mynote) {
		// for fast syscalls, we restore little state. thus we must
		// distinguish between returning to the user program after it
		// was interrupted by a timer interrupt/CPU exception vs a
		// syscall.
		refp, _ := _refaddr(p.P_pmap)
		Resend()

		intno, aux, op_pmap, odec := runtime.Userrun(tf, fxbuf,
			uintptr(p.P_pmap), fastret, refp)

		// XXX debug
		if Current() != mynote {
			panic("oh wtf")
		}

again:
		var restart bool
		if Resbegin(runonly) {
			fastret, restart = p.trap_proc(tf, tid, intno, aux)
		}
		if restart && !p.doomed {
			//fmt.Printf("restart! ")
			Resend()
			goto again
		}

		// did we switch pmaps? if so, the old pmap may need to be
		// freed.
		if odec {
			Physmem.Dec_pmap(Pa_t(op_pmap))
		}
	}
	Resend()
	Tid_del()
}

func (p *Proc_t) Sched_add(tf *[TFSIZE]uintptr, tid Tid_t) {
	go p.run(tf, tid)
}

func (p *Proc_t) _thread_new(t Tid_t) {
	p.Threadi.Lock()
	tnote := &Tnote_t{alive: true, proc: p}
	tnote.Killnaps.Killch = make(chan bool, 1)
	p.Threadi.Notes[t] = tnote
	p.Threadi.Unlock()
}

func (p *Proc_t) Thread_new() (Tid_t, bool) {
	ret, ok := tid_new()
	if !ok {
		return 0, false
	}
	p._thread_new(ret)
	return ret, true
}

// undo thread_new(); sched_add() must not have been called on t.
func (p *Proc_t) Thread_undo(t Tid_t) {
	Tid_del()

	p.Threadi.Lock()
	delete(p.Threadi.Notes, t)
	p.Threadi.Unlock()
}

func (p *Proc_t) Thread_count() int {
	p.Threadi.Lock()
	ret := len(p.Threadi.Notes)
	p.Threadi.Unlock()
	return ret
}

// terminate a single thread
func (p *Proc_t) Thread_dead(tid Tid_t, status int, usestatus bool) {
	ClearCurrent()
	// XXX exit process if thread is thread0, even if other threads exist
	p.Threadi.Lock()
	ti := &p.Threadi
	mynote, ok := ti.Notes[tid]
	if !ok {
		panic("note must exist")
	}
	mynote.alive = false
	delete(ti.Notes, tid)
	destroy := len(ti.Notes) == 0

	if usestatus {
		p.exitstatus = status
	}
	p.Threadi.Unlock()

	// update rusage user time
	// XXX
	utime := 42
	p.Atime.Utadd(utime)

	// put thread status in this process's wait info; threads don't have
	// rusage for now.
	p.Mywait.puttid(int(tid), status, nil)

	if destroy {
		p.terminate()
	}
	//tid_del()
}

func (p *Proc_t) Doomall() {

	p.doomed = true

	// XXX skip if this process has one thread
	p.Threadi.Lock()
	for _, tnote := range p.Threadi.Notes {
		tnote.Lock()

		tnote.killed = true
		kn := &tnote.Killnaps
		if kn.Kerr == 0 {
			kn.Kerr = -EINTR
		}
		select {
		case kn.Killch <- false:
		default:
		}
		if tmp := kn.Cond; tmp != nil {
			tmp.Broadcast()
		}

		tnote.Unlock()
	}
	p.Threadi.Unlock()
}

func (p *Proc_t) Lock_pmap() {
	// useful for finding deadlock bugs with one cpu
	//if p.pgfltaken {
	//	panic("double lock")
	//}
	p.pgfl.Lock()
	p.pgfltaken = true
}

func (p *Proc_t) Unlock_pmap() {
	p.pgfltaken = false
	p.pgfl.Unlock()
}

func (p *Proc_t) Lockassert_pmap() {
	if !p.pgfltaken {
		panic("pgfl lock must be held")
	}
}

func (p *Proc_t) Userdmap8_inner(va int, k2u bool) ([]uint8, Err_t) {
	p.Lockassert_pmap()

	voff := va & int(PGOFFSET)
	uva := uintptr(va)
	vmi, ok := p.Vmregion.Lookup(uva)
	if !ok {
		return nil, -EFAULT
	}
	pte, ok := vmi.ptefor(p.Pmap, uva)
	if !ok {
		return nil, -ENOMEM
	}
	ecode := uintptr(PTE_U)
	needfault := true
	isp := *pte&PTE_P != 0
	if k2u {
		ecode |= uintptr(PTE_W)
		// XXX how to distinguish between user asking kernel to write
		// to read-only page and kernel writing a page mapped read-only
		// to user? (exec args)

		//isw := *pte & PTE_W != 0
		//if isp && isw {
		iscow := *pte&PTE_COW != 0
		if isp && !iscow {
			needfault = false
		}
	} else {
		if isp {
			needfault = false
		}
	}

	if needfault {
		if err := Sys_pgfault(p, vmi, uva, ecode); err != 0 {
			return nil, err
		}
	}

	pg := Physmem.Dmap(*pte & PTE_ADDR)
	bpg := Pg2bytes(pg)
	return bpg[voff:], 0
}

// _userdmap8 and userdmap8r functions must only be used if concurrent
// modifications to the Proc_t's address space is impossible.
func (p *Proc_t) _userdmap8(va int, k2u bool) ([]uint8, Err_t) {
	p.Lock_pmap()
	ret, err := p.Userdmap8_inner(va, k2u)
	p.Unlock_pmap()
	return ret, err
}

func (p *Proc_t) Userdmap8r(va int) ([]uint8, Err_t) {
	return p._userdmap8(va, false)
}

func (p *Proc_t) usermapped(va, n int) bool {
	p.Lock_pmap()
	defer p.Unlock_pmap()

	_, ok := p.Vmregion.Lookup(uintptr(va))
	return ok
}

func (p *Proc_t) Userreadn(va, n int) (int, Err_t) {
	p.Lock_pmap()
	a, b := p.userreadn_inner(va, n)
	p.Unlock_pmap()
	return a, b
}

func (p *Proc_t) userreadn_inner(va, n int) (int, Err_t) {
	p.Lockassert_pmap()
	if n > 8 {
		panic("large n")
	}
	var ret int
	var src []uint8
	var err Err_t
	for i := 0; i < n; i += len(src) {
		src, err = p.Userdmap8_inner(va+i, false)
		if err != 0 {
			return 0, err
		}
		l := n - i
		if len(src) < l {
			l = len(src)
		}
		v := Readn(src, l, 0)
		ret |= v << (8 * uint(i))
	}
	return ret, 0
}

func (p *Proc_t) Userwriten(va, n, val int) Err_t {
	if n > 8 {
		panic("large n")
	}
	p.Lock_pmap()
	defer p.Unlock_pmap()
	var dst []uint8
	for i := 0; i < n; i += len(dst) {
		v := val >> (8 * uint(i))
		t, err := p.Userdmap8_inner(va+i, true)
		dst = t
		if err != 0 {
			return err
		}
		Writen(dst, n-i, 0, v)
	}
	return 0
}

// first ret value is the string from user space second is error
func (p *Proc_t) Userstr(uva int, lenmax int) (string, Err_t) {
	if lenmax < 0 {
		return "", 0
	}
	p.Lock_pmap()
	//defer p.Unlock_pmap()
	i := 0
	var s string
	for {
		str, err := p.Userdmap8_inner(uva+i, false)
		if err != 0 {
			p.Unlock_pmap()
			return "", err
		}
		for j, c := range str {
			if c == 0 {
				s = s + string(str[:j])
				p.Unlock_pmap()
				return s, 0
			}
		}
		s = s + string(str)
		i += len(str)
		if len(s) >= lenmax {
			p.Unlock_pmap()
			return "", -ENAMETOOLONG
		}
	}
}

func (p *Proc_t) Usertimespec(va int) (time.Duration, time.Time, Err_t) {
	var zt time.Time
	secs, err := p.Userreadn(va, 8)
	if err != 0 {
		return 0, zt, err
	}
	nsecs, err := p.Userreadn(va+8, 8)
	if err != 0 {
		return 0, zt, err
	}
	if secs < 0 || nsecs < 0 {
		return 0, zt, -EINVAL
	}
	tot := time.Duration(secs) * time.Second
	tot += time.Duration(nsecs) * time.Nanosecond
	t := time.Unix(int64(secs), int64(nsecs))
	return tot, t, 0
}

func (p *Proc_t) Userargs(uva int) ([]string, Err_t) {
	if uva == 0 {
		return nil, 0
	}
	isnull := func(cptr []uint8) bool {
		for _, b := range cptr {
			if b != 0 {
				return false
			}
		}
		return true
	}
	ret := make([]string, 0, 12)
	argmax := 64
	addarg := func(cptr []uint8) Err_t {
		if len(ret) > argmax {
			return -ENAMETOOLONG
		}
		var uva int
		// cptr is little-endian
		for i, b := range cptr {
			uva = uva | int(uint(b))<<uint(i*8)
		}
		lenmax := 128
		str, err := p.Userstr(uva, lenmax)
		if err != 0 {
			return err
		}
		ret = append(ret, str)
		return 0
	}
	uoff := 0
	psz := 8
	done := false
	curaddr := make([]uint8, 0, 8)
	for !done {
		if !Resadd(Bounds(B_PROC_T_USERARGS)) {
			return nil, -ENOHEAP
		}
		ptrs, err := p.Userdmap8r(uva + uoff)
		if err != 0 {
			return nil, err
		}
		for _, ab := range ptrs {
			curaddr = append(curaddr, ab)
			if len(curaddr) == psz {
				if isnull(curaddr) {
					done = true
					break
				}
				if err := addarg(curaddr); err != 0 {
					return nil, err
				}
				curaddr = curaddr[0:0]
			}
		}
		uoff += len(ptrs)
	}
	return ret, 0
}

// copies src to the user virtual address uva. may copy part of src if uva +
// len(src) is not mapped
func (p *Proc_t) K2user(src []uint8, uva int) Err_t {
	p.Lock_pmap()
	ret := p.K2user_inner(src, uva)
	p.Unlock_pmap()
	return ret
}

func (p *Proc_t) K2user_inner(src []uint8, uva int) Err_t {
	p.Lockassert_pmap()
	cnt := 0
	l := len(src)
	for cnt != l {
		gimme := Bounds(B_PROC_T_K2USER_INNER)
		if !Resadd_noblock(gimme) {
			return -ENOHEAP
		}
		dst, err := p.Userdmap8_inner(uva+cnt, true)
		if err != 0 {
			return err
		}
		ub := len(src)
		if ub > len(dst) {
			ub = len(dst)
		}
		copy(dst, src)
		src = src[ub:]
		cnt += ub
	}
	return 0
}

// copies len(dst) bytes from userspace address uva to dst
func (p *Proc_t) User2k(dst []uint8, uva int) Err_t {
	p.Lock_pmap()
	ret := p.User2k_inner(dst, uva)
	p.Unlock_pmap()
	return ret
}

func (p *Proc_t) User2k_inner(dst []uint8, uva int) Err_t {
	p.Lockassert_pmap()
	cnt := 0
	for len(dst) != 0 {
		gimme := Bounds(B_PROC_T_USER2K_INNER)
		if !Resadd_noblock(gimme) {
			return -ENOHEAP
		}
		src, err := p.Userdmap8_inner(uva+cnt, false)
		if err != 0 {
			return err
		}
		did := copy(dst, src)
		dst = dst[did:]
		cnt += did
	}
	return 0
}

func (p *Proc_t) Unusedva_inner(startva, len int) int {
	p.Lockassert_pmap()
	if len < 0 || len > 1<<48 {
		panic("weird len")
	}
	startva = Rounddown(startva, PGSIZE)
	if startva < USERMIN {
		startva = USERMIN
	}
	_ret, _l := p.Vmregion.empty(uintptr(startva), uintptr(len))
	ret := int(_ret)
	l := int(_l)
	if startva > ret && startva < ret+l {
		ret = startva
	}
	return ret
}

// don't forget: there are two places where pmaps/memory are free'd:
// Proc_t.terminate() and exec.
func Uvmfree_inner(pmg *Pmap_t, p_pmap Pa_t, vmr *Vmregion_t) {
	vmr.iter(func(vmi *Vminfo_t) {
		start := uintptr(vmi.pgn << PGSHIFT)
		end := start + uintptr(vmi.pglen<<PGSHIFT)
		var unpin Unpin_i
		if vmi.mtype == VFILE {
			unpin = vmi.file.mfile.unpin
		}
		pmfree(pmg, start, end, unpin)
	})
}

func (p *Proc_t) Uvmfree() {
	Uvmfree_inner(p.Pmap, p.P_pmap, &p.Vmregion)
	// Dec_pmap could free the pmap itself. thus it must come after
	// Uvmfree.
	Physmem.Dec_pmap(p.P_pmap)
	// close all open mmap'ed files
	p.Vmregion.Clear()
}

// terminate a process. must only be called when the process has no more
// running threads.
func (p *Proc_t) terminate() {
	if p.Pid == 1 {
		panic("killed init")
	}

	p.Threadi.Lock()
	ti := &p.Threadi
	if len(ti.Notes) != 0 {
		panic("terminate, but threads alive")
	}
	p.Threadi.Unlock()

	// close open fds
	p.Fdl.Lock()
	for i := range p.Fds {
		if p.Fds[i] == nil {
			continue
		}
		Close_panic(p.Fds[i])
	}
	p.Fdl.Unlock()
	Close_panic(p.cwd)

	p.Mywait.Pid = 1

	// free all user pages in the pmap. the last CPU to call Dec_pmap on
	// the proc's pmap will free the pmap itself. freeing the user pages is
	// safe since we know that all user threads are dead and thus no CPU
	// will try to access user mappings. however, any CPU may access kernel
	// mappings via this pmap.
	p.Uvmfree()

	// send status to parent
	if p.Pwait == nil {
		panic("nil pwait")
	}

	// combine total child rusage with ours, send to parent
	na := Accnt_t{Userns: p.Atime.Userns, Sysns: p.Atime.Sysns}
	// calling na.add() makes the compiler allocate na in the heap! escape
	// analysis' fault?
	//na.add(&p.Catime)
	na.Userns += p.Catime.Userns
	na.Sysns += p.Catime.Sysns

	// put process exit status to parent's wait info
	p.Pwait.putpid(p.Pid, p.exitstatus, &na)
	// remove pointer to parent to prevent deep fork trees from consuming
	// unbounded memory.
	p.Pwait = nil
	// OOM killer assumes a process has terminated once its pid is no
	// longer in the pid table.
	Proc_del(p.Pid)
}

// returns false if the number of running threads or unreaped child statuses is
// larger than noproc.
func (p *Proc_t) Start_proc(pid int) bool {
	return p.Mywait._start(pid, true, p.Ulim.Noproc)
}

// returns false if the number of running threads or unreaped child statuses is
// larger than noproc.
func (p *Proc_t) Start_thread(t Tid_t) bool {
	return p.Mywait._start(int(t), false, p.Ulim.Noproc)
}

var Proclock = sync.Mutex{}

func Proc_check(pid int) (*Proc_t, bool) {
	Proclock.Lock()
	p, ok := Allprocs[pid]
	Proclock.Unlock()
	return p, ok
}

func Proc_del(pid int) {
	Proclock.Lock()
	_, ok := Allprocs[pid]
	if !ok {
		panic("bad pid")
	}
	delete(Allprocs, pid)
	Proclock.Unlock()
}

var _deflimits = Ulimit_t{
	// mem limit = 128 MB
	Pages: (1 << 27) / (1 << 12),
	//nofile: 512,
	Nofile: RLIM_INFINITY,
	Novma:  (1 << 8),
	Noproc: (1 << 10),
}

// returns the new proc and success; can fail if the system-wide limit of
// procs/threads has been reached. the parent's fdtable must be locked.
func Proc_new(name string, cwd *Fd_t, fds []*Fd_t, sys Syscall_i) (*Proc_t, bool) {
	Proclock.Lock()

	if nthreads >= int64(Syslimit.Sysprocs) {
		Proclock.Unlock()
		return nil, false
	}

	nthreads++

	pid_cur++
	np := pid_cur
	pid_cur++
	tid0 := Tid_t(pid_cur)
	if _, ok := Allprocs[np]; ok {
		panic("pid exists")
	}
	ret := &Proc_t{}
	Allprocs[np] = ret
	Proclock.Unlock()

	ret.Name = name
	ret.Pid = np
	ret.Fds = make([]*Fd_t, len(fds))
	ret.fdstart = 3
	for i := range fds {
		if fds[i] == nil {
			continue
		}
		tfd, err := Copyfd(fds[i])
		// copying an fd may fail if another thread closes the fd out
		// from under us
		if err == 0 {
			ret.Fds[i] = tfd
		}
		ret.nfds++
	}
	ret.cwd = cwd
	if ret.cwd.Fops.Reopen() != 0 {
		panic("must succeed")
	}
	ret.Mmapi = USERMIN
	ret.Ulim = _deflimits

	ret.Threadi.init()
	ret.tid0 = tid0
	ret._thread_new(tid0)

	ret.Mywait.Wait_init(ret.Pid)
	if !ret.Start_thread(ret.tid0) {
		panic("silly noproc")
	}

	ret.syscall = sys
	return ret, true
}

func (p *Proc_t) Reap_doomed(tid Tid_t) {
	if !p.doomed {
		panic("p not doomed")
	}
	p.Thread_dead(tid, 0, false)
}

// total number of all threads
var nthreads int64
var pid_cur int

// returns false if system-wide limit is hit.
func tid_new() (Tid_t, bool) {
	Proclock.Lock()
	defer Proclock.Unlock()
	if nthreads > int64(Syslimit.Sysprocs) {
		return 0, false
	}
	nthreads++
	pid_cur++
	ret := pid_cur

	return Tid_t(ret), true
}

func Tid_del() {
	Proclock.Lock()
	if nthreads == 0 {
		panic("oh shite")
	}
	nthreads--
	Proclock.Unlock()
}

// a type to make it easier for code that allocates cached objects to determine
// when we must try to evict them.
type Cacheallocs_t struct {
	initted	bool
}

// returns true if the caller must try to evict their recent cache allocations.
func (ca *Cacheallocs_t) Shouldevict(res int) bool {
	if !Kernel {
		return false
	}
	//if !Lims {
	//	return false
	//}
	init := !ca.initted
	ca.initted = true
	return !runtime.Cacheres(res, init)
}

var Kwaits int

//var Lims = true

func Kreswait(c int, name string) {
	if !Kernel {
		return
	}
	//if !Lims {
	//	return
	//}
	for !runtime.Memreserve(c) {
		//fmt.Printf("kernel thread \"%v\" waiting for hog to die...\n", name)

		Kwaits++
		var omsg oommsg_t
		omsg.need = 100 << 20
		omsg.resume = make(chan bool, 1)
		Oom.halp <- omsg
		<-omsg.resume
	}
}

func Kunres() int {
	if !Kernel {
		return 0
	}
	//if !Lims {
	//	return 0
	//}
	return runtime.Memunres()
}

func Kresdebug(c int, name string) {
	//Kreswait(c, name)
}

func Kunresdebug() int {
	//return Kunres()
	return 0
}

func Current() *Tnote_t {
	_p := runtime.Gptr()
	if _p == nil {
		panic("nuts")
	}
	ret := (*Tnote_t)(_p)
	return ret
}

func SetCurrent(p *Tnote_t) {
	if p == nil {
		panic("nuts")
	}
	if runtime.Gptr() != nil {
		panic("nuts")
	}
	_p := (unsafe.Pointer)(p)
	runtime.Setgptr(_p)
}

func ClearCurrent() {
	if runtime.Gptr() == nil {
		panic("nuts")
	}
	runtime.Setgptr(nil)
}

func Callerdump(start int) {
	i := start
	s := ""
	for {
		_, f, l, ok := runtime.Caller(i)
		if !ok {
			break
		}
		i++
		//li := strings.LastIndex(f, "/")
		//if li != -1 {
		//	f = f[li+1:]
		//}
		if s == "" {
			s = fmt.Sprintf("%s:%d\n", f, l)
		} else {
			s += fmt.Sprintf("\t<-%s:%d\n", f, l)
		}
	}
	fmt.Printf("%s", s)
}

// a type for detecting the first call from each distinct path of ancestor
// callers.
type Distinct_caller_t struct {
	sync.Mutex
	Enabled	bool
	did	map[uintptr]bool
	Whitel	map[string]bool
}

// returns a poor-man's hash of the given RIP values, which is probably unique.
func (dc *Distinct_caller_t) _pchash(pcs []uintptr) uintptr {
	if len(pcs) == 0 {
		panic("d'oh")
	}
	var ret uintptr
	for _, pc := range pcs {
		pc = pc*1103515245 + 12345
		ret ^= pc
	}
	return ret
}

func (dc *Distinct_caller_t) Len() int {
	dc.Lock()
	ret := len(dc.did)
	dc.Unlock()
	return ret
}

// returns true if the caller path is unique and is not white-listed and the
// caller path as a string.
func (dc *Distinct_caller_t) Distinct() (bool, string) {
	dc.Lock()
	defer dc.Unlock()
	if !dc.Enabled {
		return false, ""
	}

	if dc.did == nil {
		dc.did = make(map[uintptr]bool)
	}

	var pcs []uintptr
	for sz, got := 30, 30; got >= sz; sz *= 2 {
		pcs = make([]uintptr, 30)
		got = runtime.Callers(3, pcs)
		if got == 0 {
			panic("no")
		}
	}
	h := dc._pchash(pcs)
	if ok := dc.did[h]; !ok {
		dc.did[h] = true
		frames := runtime.CallersFrames(pcs)
		fs := ""
		// check for white-listed caller
		for {
			fr, more := frames.Next()
			if ok := dc.Whitel[fr.Function]; ok {
				return false, ""
			}
			if fs == "" {
				fs = fmt.Sprintf("%v (%v:%v)\n", fr.Function,
					fr.File, fr.Line)
			} else {
				fs += fmt.Sprintf("\t%v (%v:%v)\n", fr.Function,
				    fr.File, fr.Line)
			}
			if !more || fr.Function == "runtime.goexit" {
				break
			}
		}
		return true, fs
	}
	return false, ""
}

type oommsg_t struct {
	need	int
	resume	chan bool
}

type oom_t struct {
	halp	chan oommsg_t
	evict	func() (int, int)
	lastpr	time.Time
}

var Oom *oom_t

func Oom_init(evict func() (int, int)) {
	Oom = &oom_t{}
	Oom.halp = make(chan oommsg_t)
	Oom.evict = evict
	go Oom.reign()
}

func (o *oom_t) gc() {
	now := time.Now()
	if now.Sub(o.lastpr) > time.Second {
		o.lastpr = now.Add(time.Second)
	}
	runtime.GCX()
}

func (o *oom_t) reign() {
outter:
	for msg := range o.halp {
		//fmt.Printf("A need %v, rem %v\n", msg.need, runtime.Memremain())
		o.gc()
		//fmt.Printf("B need %v, rem %v\n", msg.need, runtime.Memremain())
		//panic("OOM KILL\n")
		if msg.need < runtime.Memremain() {
			// there is apparently enough reservation available for
			// them now
			msg.resume <- true
			continue
		}

		// XXX make sure msg.need is a satisfiable reservation size

		// XXX expand kernel heap with free pages; page if none
		// available

		last := 0
		for {
			a, b := o.evict()
			if a + b == last || a + b < 1000 {
				break
			}
			last = a + b
		}
		o.gc()
		if msg.need < runtime.Memremain() {
			msg.resume <- true
			continue outter
		}
		for {
			// someone must die
			o.dispatch_peasant(msg.need)
			o.gc()
			if msg.need < runtime.Memremain() {
				msg.resume <- true
				continue outter
			}
		}
	}
}

func (o *oom_t) dispatch_peasant(need int) {
	// the oom killer's memory use should have a small bound
	var head *Proc_t
	Proclock.Lock()
	for _, p := range Allprocs {
		p.Oomlink = head
		head = p
	}
	Proclock.Unlock()

	var memmax int
	var vic *Proc_t
	for p := head; p != nil; p = p.Oomlink {
		mem := o.judge_peasant(p)
		if mem > memmax {
			memmax = mem
			vic = p
		}
	}

	// destroy the list so the Proc_ts and reachable objects become dead
	var next *Proc_t
	for p := head; p != nil; p = next {
		next = p.Oomlink
		p.Oomlink = nil
	}

	if vic == nil {
		panic("nothing to kill?")
	}

	fmt.Printf("Killing PID %d \"%v\" for (%v %v)...\n", vic.Pid, vic.Name,
	    Human(need), vic.Vmregion.Novma)
	vic.Doomall()
	st := time.Now()
	dl := st.Add(time.Second)
	// wait for the victim to die
	sleept := 10*time.Microsecond
	for {
		if _, ok := Proc_check(vic.Pid); !ok {
			break
		}
		now := time.Now()
		if now.After(dl) {
			dl = dl.Add(time.Second)
			fmt.Printf("oom killer: waiting for hog for %v...\n",
			    now.Sub(st))
			o.gc()
		}
		time.Sleep(sleept)
		sleept *= 2
		const maxs = 10*time.Millisecond
		if sleept > maxs {
			sleept = maxs
		}
	}
}

// acquires p's pmap and fd locks (separately)
func (o *oom_t) judge_peasant(p *Proc_t) int {
	// init(1) must never perish
	if p.Pid == 1 {
		return 0
	}

	p.Lock_pmap()
	novma := int(p.Vmregion.Novma)
	p.Unlock_pmap()

	var nofd int
	p.Fdl.Lock()
	for _, fd := range p.Fds {
		if fd != nil {
			nofd++
		}
	}
	p.Fdl.Unlock()

	// count per-thread and per-child process wait objects
	chalds := p.Mywait.Len()

	return novma + nofd + chalds
}
