package proc

import "sync"
import "sync/atomic"

import "fmt"
import "runtime"

import "accnt"
import "bounds"
import "defs"
import "fd"
import "limits"
import "mem"
import "res"
import "tinfo"
import "ustr"
import "vm"
import "hashtable"

// per-process limits
type Ulimit_t struct {
	Pages  int
	Nofile uint
	Novma  uint
	Noproc uint
}

type Proc_t struct {
	Pid int
	// first thread id
	tid0 defs.Tid_t
	Name ustr.Ustr

	// waitinfo for my child processes
	Mywait Wait_t
	// waitinfo of my parent
	Pwait *Wait_t

	// thread tids of this process
	Threadi tinfo.Threadinfo_t

	// Address space
	Vm vm.Vm_t

	// mmap next virtual address hint
	Mmapi int

	// a process is marked doomed when it has been killed but may have
	// threads currently running on another processor
	doomed     bool
	exitstatus int

	Fds []*fd.Fd_t
	// where to start scanning for free fds
	fdstart int
	// fds, fdstart, nfds protected by fdl
	Fdl sync.Mutex
	// number of valid file descriptors
	nfds int

	Cwd *fd.Cwd_t

	Ulim Ulimit_t

	// this proc's rusage
	Atime accnt.Accnt_t
	// total child rusage
	Catime accnt.Accnt_t

	syscall Syscall_i
	// no thread can read/write Oomlink except the OOM killer
	Oomlink *Proc_t
}

type ptable_t struct {
	ht	*hashtable.Hashtable_t
}

func (pt *ptable_t) Get(pid int32) (*Proc_t, bool) {
	ret, ok := pt.ht.Get(pid)
	if ok {
		return ret.(*Proc_t), true
	}
	return nil, false
}

func (pt *ptable_t) Set(pid int32, p *Proc_t) {
	pt.ht.Set(pid, p)
}

func (pt *ptable_t) Del(pid int32) {
	pt.ht.Del(pid)
}

// Iter may execute concurrently with other lookups, inserts, and deletes
func (pt *ptable_t) Iter(f func (int32, *Proc_t) bool) {
	pt.ht.Iter(func (key, value interface{}) bool {
		pid := key.(int32)
		p := value.(*Proc_t)
		return f(pid, p)
	})
}

var Ptable = ptable_t {
	ht: hashtable.MkHash(limits.Syslimit.Sysprocs),
}

func (p *Proc_t) Tid0() defs.Tid_t {
	return p.tid0
}

func (p *Proc_t) Doomed() bool {
	return p.doomed
}

// an fd table invariant: every fd must have its file field set. thus the
// caller cannot set an fd's file field without holding fdl. otherwise you will
// race with a forking thread when it copies the fd table.
func (p *Proc_t) Fd_insert(f *fd.Fd_t, perms int) (int, bool) {
	p.Fdl.Lock()
	a, b := p.fd_insert_inner(f, perms)
	p.Fdl.Unlock()
	return a, b
}

func (p *Proc_t) fd_insert_inner(f *fd.Fd_t, perms int) (int, bool) {

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
		if p.Ulim.Nofile != defs.RLIM_INFINITY && nl > int(p.Ulim.Nofile) {
			nl = int(p.Ulim.Nofile)
			if nl < ol {
				panic("how")
			}
		}
		nfdt := make([]*fd.Fd_t, nl, nl)
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
func (p *Proc_t) Fd_insert2(f1 *fd.Fd_t, perms1 int,
	f2 *fd.Fd_t, perms2 int) (int, int, bool) {
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
func (p *Proc_t) Fd_get_inner(fdn int) (*fd.Fd_t, bool) {
	if fdn < 0 || fdn >= len(p.Fds) {
		return nil, false
	}
	ret := p.Fds[fdn]
	ok := ret != nil
	return ret, ok
}

func (p *Proc_t) Fd_get(fdn int) (*fd.Fd_t, bool) {
	p.Fdl.Lock()
	ret, ok := p.Fd_get_inner(fdn)
	p.Fdl.Unlock()
	return ret, ok
}

// fdn is not guaranteed to be a sane fd
func (p *Proc_t) Fd_del(fdn int) (*fd.Fd_t, bool) {
	p.Fdl.Lock()
	a, b := p.fd_del_inner(fdn)
	p.Fdl.Unlock()
	return a, b
}

func (p *Proc_t) fd_del_inner(fdn int) (*fd.Fd_t, bool) {
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
func (p *Proc_t) Fd_dup(ofdn, nfdn int) (*fd.Fd_t, bool, defs.Err_t) {
	if ofdn == nfdn {
		return nil, false, 0
	}

	p.Fdl.Lock()
	defer p.Fdl.Unlock()

	ofd, ok := p.Fd_get_inner(ofdn)
	if !ok {
		return nil, false, -defs.EBADF
	}
	cpy, err := fd.Copyfd(ofd)
	if err != 0 {
		return nil, false, err
	}
	cpy.Perms &^= fd.FD_CLOEXEC
	rfd, needclose := p.Fd_get_inner(nfdn)
	p.Fds[nfdn] = cpy

	return rfd, needclose, 0
}

// returns whether the parent's TLB should be flushed and whether the we
// successfully copied the parent's address space.
func (parent *Proc_t) Vm_fork(child *Proc_t, rsp uintptr) (bool, bool) {
	parent.Vm.Lockassert_pmap()
	// first add kernel pml4 entries
	for _, e := range mem.Kents {
		child.Vm.Pmap[e.Pml4slot] = e.Entry
	}
	// recursive mapping
	child.Vm.Pmap[mem.VREC] = child.Vm.P_pmap | vm.PTE_P | vm.PTE_W

	failed := false
	doflush := false
	child.Vm.Vmregion = parent.Vm.Vmregion.Copy()
	parent.Vm.Vmregion.Iter(func(vmi *vm.Vminfo_t) {
		start := int(vmi.Pgn << vm.PGSHIFT)
		end := start + int(vmi.Pglen<<vm.PGSHIFT)
		ashared := vmi.Mtype == vm.VSANON
		fl, ok := vm.Ptefork(child.Vm.Pmap, parent.Vm.Pmap, start, end, ashared)
		failed = failed || !ok
		doflush = doflush || fl
	})

	if failed {
		return doflush, false
	}

	// don't mark stack COW since the parent/child will fault their stacks
	// immediately
	vmi, ok := child.Vm.Vmregion.Lookup(rsp)
	// give up if we can't find the stack
	if !ok {
		return doflush, true
	}
	pte, ok := vmi.Ptefor(child.Vm.Pmap, rsp)
	if !ok || *pte&vm.PTE_P == 0 || *pte&vm.PTE_U == 0 {
		return doflush, true
	}
	// sys_pgfault expects pmap to be locked
	child.Vm.Lock_pmap()
	perms := uintptr(vm.PTE_U | vm.PTE_W)
	if vm.Sys_pgfault(&child.Vm, vmi, rsp, perms) != 0 {
		return doflush, false
	}
	child.Vm.Unlock_pmap()
	vmi, ok = parent.Vm.Vmregion.Lookup(rsp)
	if !ok || *pte&vm.PTE_P == 0 || *pte&vm.PTE_U == 0 {
		panic("child has stack but not parent")
	}
	pte, ok = vmi.Ptefor(parent.Vm.Pmap, rsp)
	if !ok {
		panic("must exist")
	}
	*pte &^= vm.PTE_COW
	*pte |= vm.PTE_W | vm.PTE_WASCOW

	return true, true
}

// flush TLB on all CPUs that may have this processes' pmap loaded
func (p *Proc_t) Tlbflush() {
	// this flushes the TLB for now
	p.Vm.Tlbshoot(0, 2)
}

func (p *Proc_t) resched(tid defs.Tid_t, n *tinfo.Tnote_t) bool {
	talive := n.Alive
	if talive && p.doomed {
		// although this thread is still alive, the process should
		// terminate
		p.Reap_doomed(tid)
		return false
	}
	return talive
}

// returns non-zero if this calling process has been killed and the caller
// should finish the system call.
func KillableWait(cond *sync.Cond) defs.Err_t {
	if !res.Kernel {
		cond.Wait()
		return 0
	} else {
		mynote := tinfo.Current()

		// ensure the sleep is atomic w.r.t. killed flag and kn.Cond writes
		// from killer
		mynote.Lock()
		if mynote.Killed {
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
}

// returns true if the kernel may safely use a "fast" resume and whether the
// system call should be restarted.
func (p *Proc_t) trap_proc(tf *[defs.TFSIZE]uintptr, tid defs.Tid_t, intno, aux int) (bool, bool) {
	fastret := false
	restart := false
	switch intno {
	case defs.SYSCALL:
		// fast return doesn't restore the registers used to
		// specify the arguments for libc _entry(), so do a
		// slow return when returning from sys_execv().
		sysno := tf[defs.TF_RAX]
		if sysno != defs.SYS_EXECV {
			fastret = true
		}
		ret := p.syscall.Syscall(p, tid, tf)
		restart = ret == int(-defs.ENOHEAP)
		if !restart {
			tf[defs.TF_RAX] = uintptr(ret)
		}

	case defs.TIMER:
		//fmt.Printf(".")
		runtime.Gosched()
	case defs.PGFAULT:
		faultaddr := uintptr(aux)
		err := p.Vm.Pgfault(tid, faultaddr, tf[defs.TF_ERROR])
		restart = err == -defs.ENOHEAP
		if err != 0 && !restart {
			fmt.Printf("*** fault *** %v: addr %x, "+
				"rip %x, err %v. killing...\n", p.Name, faultaddr,
				tf[defs.TF_RIP], err)
			p.syscall.Sys_exit(p, tid, defs.SIGNALED|defs.Mkexitsig(11))
		}
	case defs.DIVZERO, defs.GPFAULT, defs.UD:
		fmt.Printf("%s -- TRAP: %v, RIP: %x\n", p.Name, intno,
			tf[defs.TF_RIP])
		p.syscall.Sys_exit(p, tid, defs.SIGNALED|defs.Mkexitsig(4))
	case defs.TLBSHOOT, defs.PERFMASK, defs.INT_KBD, defs.INT_COM1, defs.INT_MSI0,
		defs.INT_MSI1, defs.INT_MSI2, defs.INT_MSI3, defs.INT_MSI4, defs.INT_MSI5, defs.INT_MSI6,
		defs.INT_MSI7:
		// XXX: shouldn't interrupt user program execution...
	default:
		panic(fmt.Sprintf("weird trap: %d", intno))
	}
	return fastret, restart
}

func (p *Proc_t) run(tf *[defs.TFSIZE]uintptr, tid defs.Tid_t) {

	p.Threadi.Lock()
	mynote, ok := p.Threadi.Notes[tid]
	p.Threadi.Unlock()
	// each thread removes itself from threadi.Notes; thus mynote must
	// exist
	if !ok {
		panic("note must exist")
	}
	tinfo.SetCurrent(mynote)

	var fxbuf *[64]uintptr
	if res.Resbegin(res.Onek) {
		// could allocate fxbuf lazily
		fxbuf = vm.Mkfxbuf()
	}

	gimme := bounds.Bounds(bounds.B_PROC_T_RUN1)
	fastret := false
	for p.resched(tid, mynote) {
		// for fast syscalls, we restore little state. thus we must
		// distinguish between returning to the user program after it
		// was interrupted by a timer interrupt/CPU exception vs a
		// syscall.
		refp, _ := mem.Physmem.Refaddr(p.Vm.P_pmap)
		tlbp := mem.Physmem.Tlbaddr(p.Vm.P_pmap)
		res.Resend()

		intno, aux, op_pmap, odec, cpunum := runtime.Userrun(tf, fxbuf,
			uintptr(p.Vm.P_pmap), fastret, refp, tlbp)

		// XXX debug
		if tinfo.Current() != mynote {
			panic("oh wtf")
		}

	again:
		var restart bool
		if res.Resbegin(gimme) {
			fastret, restart = p.trap_proc(tf, tid, intno, aux)
		}
		if restart && !p.doomed {
			//fmt.Printf("restart! ")
			res.Resend()
			goto again
		}

		// did we switch pmaps? if so, the old pmap may need to be
		// freed.
		if odec {
			opmap := mem.Pa_t(op_pmap)
			otlbp := mem.Physmem.Tlbaddr(opmap)
			runtime.And64(otlbp, ^uint64(1 << cpunum))
			mem.Physmem.Dec_pmap(opmap)
		}
	}
	res.Resend()
	Tid_del()
}

func (p *Proc_t) Sched_add(tf *[defs.TFSIZE]uintptr, tid defs.Tid_t) {
	go p.run(tf, tid)
}

func (p *Proc_t) _thread_new(t defs.Tid_t) {
	p.Threadi.Lock()
	tnote := &tinfo.Tnote_t{Alive: true, State: p}
	tnote.Killnaps.Killch = make(chan bool, 1)
	p.Threadi.Notes[t] = tnote
	p.Threadi.Unlock()
}

func (p *Proc_t) Thread_new() (defs.Tid_t, bool) {
	ret, ok := tid_new()
	if !ok {
		return 0, false
	}
	p._thread_new(ret)
	return ret, true
}

// undo thread_new(); sched_add() must not have been called on t.
func (p *Proc_t) Thread_undo(t defs.Tid_t) {
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
func (p *Proc_t) Thread_dead(tid defs.Tid_t, status int, usestatus bool) {
	tinfo.ClearCurrent()
	// XXX exit process if thread is thread0, even if other threads exist
	p.Threadi.Lock()
	ti := &p.Threadi
	mynote, ok := ti.Notes[tid]
	if !ok {
		panic("note must exist")
	}
	mynote.Alive = false
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

		tnote.Killed = true
		tnote.Isdoomed = true
		kn := &tnote.Killnaps
		if kn.Kerr == 0 {
			kn.Kerr = -defs.EINTR
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

func (p *Proc_t) Userargs(uva int) ([]ustr.Ustr, defs.Err_t) {
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
	ret := make([]ustr.Ustr, 0, 12)
	argmax := 64
	addarg := func(cptr []uint8) defs.Err_t {
		if len(ret) > argmax {
			fmt.Printf("too long\n")
			return -defs.ENAMETOOLONG
		}
		var uva int
		// cptr is little-endian
		for i, b := range cptr {
			uva = uva | int(uint(b))<<uint(i*8)
		}
		lenmax := 128
		str, err := p.Vm.Userstr(uva, lenmax)
		if err != 0 {
			return err
		}
		ret = append(ret, str)
		return 0
	}
	uoff := 0
	const psz = 8
	curaddr := make([]uint8, 0, 8)
	for {
		if !res.Resadd(bounds.Bounds(bounds.B_PROC_T_USERARGS)) {
			return nil, -defs.ENOHEAP
		}
		ptrs, err := p.Vm.Userdmap8r(uva + uoff)
		if err != 0 {
			return nil, err
		}
		for _, ab := range ptrs {
			uoff++
			curaddr = append(curaddr, ab)
			if len(curaddr) == psz {
				break
			}
		}
		if len(curaddr) == psz {
			if isnull(curaddr) {
				break
			}
			if err := addarg(curaddr); err != 0 {
				return nil, err
			}
			curaddr = curaddr[0:0]
		}
	}
	return ret, 0
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
		fd.Close_panic(p.Fds[i])
	}
	p.Fdl.Unlock()
	fd.Close_panic(p.Cwd.Fd)

	p.Mywait.Pid = 1

	// free all user pages in the pmap. the last CPU to call Dec_pmap on
	// the proc's pmap will free the pmap itself. freeing the user pages is
	// safe since we know that all user threads are dead and thus no CPU
	// will try to access user mappings. however, any CPU may access kernel
	// mappings via this pmap.
	p.Vm.Uvmfree()

	// send status to parent
	if p.Pwait == nil {
		panic("nil pwait")
	}

	// combine total child rusage with ours, send to parent
	na := accnt.Accnt_t{Userns: p.Atime.Userns, Sysns: p.Atime.Sysns}
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
func (p *Proc_t) Start_thread(t defs.Tid_t) bool {
	return p.Mywait._start(int(t), false, p.Ulim.Noproc)
}

func Proc_check(pid int) (*Proc_t, bool) {
	p, ok := Ptable.Get(int32(pid))
	return p, ok
}

func Proc_del(pid int) {
	Ptable.Del(int32(pid))
}

var _deflimits = Ulimit_t{
	// mem limit = 128 MB
	//Pages: (1 << 27) / (1 << 12),
	Pages: 0x7fffffffffffffff,
	//nofile: 512,
	Nofile: defs.RLIM_INFINITY,
	//Novma:  (1 << 8),
	Novma:  defs.RLIM_INFINITY,
	Noproc: (1 << 10),
}

// returns the new proc and success; can fail if the system-wide limit of
// procs/threads has been reached. the parent's fdtable must be locked.
func Proc_new(name ustr.Ustr, cwd *fd.Cwd_t, fds []*fd.Fd_t, sys Syscall_i) (*Proc_t, bool) {
	if atomic.AddInt64(&nthreads, 1) >= int64(limits.Syslimit.Sysprocs) {
		atomic.AddInt64(&nthreads, -1)
		return nil, false
	}

	t0 := atomic.AddInt32(&atomic_pid, 2)
	np := t0 - 1
	tid0 := defs.Tid_t(t0)
	if _, ok := Ptable.Get(np); ok {
		panic("pid exists")
	}
	ret := &Proc_t{}
	Ptable.Set(np, ret)

	ret.Name = name
	ret.Pid = int(np)
	ret.Fds = make([]*fd.Fd_t, len(fds))
	ret.fdstart = 3
	for i := range fds {
		if fds[i] == nil {
			continue
		}
		tfd, err := fd.Copyfd(fds[i])
		// copying an fd may fail if another thread closes the fd out
		// from under us
		if err == 0 {
			ret.Fds[i] = tfd
		}
		ret.nfds++
	}
	ret.Cwd = cwd
	if ret.Cwd.Fd.Fops.Reopen() != 0 {
		panic("must succeed")
	}
	ret.Mmapi = mem.USERMIN
	ret.Ulim = _deflimits

	ret.Threadi.Init()
	ret.tid0 = tid0
	ret._thread_new(tid0)

	ret.Mywait.Wait_init(ret.Pid)
	if !ret.Start_thread(ret.tid0) {
		panic("silly noproc")
	}

	ret.syscall = sys
	return ret, true
}

func (p *Proc_t) Reap_doomed(tid defs.Tid_t) {
	if !p.doomed {
		panic("p not doomed")
	}
	p.Thread_dead(tid, 0, false)
}

// total number of all threads
var nthreads int64
var atomic_pid int32

// returns false if system-wide limit is hit.
func tid_new() (defs.Tid_t, bool) {
	if atomic.AddInt64(&nthreads, 1) > int64(limits.Syslimit.Sysprocs) {
		atomic.AddInt64(&nthreads, -1)
		return 0, false
	}
	ret := atomic.AddInt32(&atomic_pid, 1)
	return defs.Tid_t(ret), true
}

func Tid_del() {
	if atomic.AddInt64(&nthreads, -1) < 0 {
		panic("oh shite")
	}
}

func CurrentProc() *Proc_t {
	st := tinfo.Current().State
	proc := st.(*Proc_t)
	return proc
}
