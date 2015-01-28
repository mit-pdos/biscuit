package main

import "fmt"
import "math/rand"
import "runtime"
import "unsafe"

type trapstore_t struct {
	trapno    int
	pid       int
	faultaddr int
	rip       int
}
const ntrapst   int = 64
var trapstore [ntrapst]trapstore_t
var tshead      int
var tstail      int

func tsnext(c int) int {
	return (c + 1) % ntrapst
}

var     SYSCALL   int = 64
var     TIMER     int = 32
var     GPFAULT   int = 13
var     PGFAULT   int = 14

// trap cannot do anything that may have side-effects on the runtime (like
// fmt.Print, or use pancake!). the reason is that, by design, goroutines are
// scheduled cooperatively in the runtime. trap interrupts the runtime though,
// and then tries to execute more gocode, thus doing things the runtime did not
// expect.
//go:nosplit
func trapstub(tf *[23]int, pid int) {

	tfregs    := 16
	tf_trapno := tfregs
	//tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	//tf_rflags   := tfregs + 4
	//fl_intf     := 1 << 9

	trapno := tf[tf_trapno]

	// kernel faults are fatal errors for now, but they could be handled by
	// trap & c.
	if pid == 0 {
		runtime.Pnum(trapno)
		if trapno == PGFAULT {
			runtime.Pnum(runtime.Rcr2())
		}
		if trapno == GPFAULT {
			tf_rdx := 12
			runtime.Pnum(tf[tf_rdx])
		}
		rip := tf[tf_rip]
		runtime.Pnum(rip)
		runtime.Pnum(0x42)
		for {
		}
	}

	// add to trap circular buffer for actual trap handler
	if tsnext(tshead) == tstail {
		runtime.Pnum(0xbad)
		for {
		}
	}
	trapstore[tshead].trapno = trapno
	trapstore[tshead].pid = pid
	tcur := tshead
	tshead = tsnext(tshead)

	switch trapno {
	case SYSCALL, PGFAULT:
		if trapno == PGFAULT {
			trapstore[tcur].faultaddr = runtime.Rcr2()
			trapstore[tcur].rip = tf[tf_rip]
		}
		// yield until the syscall/fault is handled
		runtime.Procyield()
	case TIMER:
		// timer interrupts are not passed yet
		runtime.Pnum(0x41)
		for {
		}
	default:
		runtime.Pnum(trapno)
		runtime.Pnum(tf[tf_rip])
		runtime.Pnum(0xbadbabe)
		for {
		}
	}
}

func trap(handlers map[int]func(...interface{})) {
	for {
		for tstail == tshead {
			// no work
			runtime.Gosched()
		}

		tcur := &trapstore[tstail]
		trapno := tcur.trapno
		uc := tcur.pid
		tstail = tsnext(tstail)

		if h, ok := handlers[trapno]; ok {
			args := []interface{}{uc}
			if trapno == PGFAULT {
				args = append(args, tcur.faultaddr)
				args = append(args, tcur.rip)
			}
			go h(args...)
			continue
		}
		fmt.Printf("no handler for trap %v, pid %x ", trapno, uc)
	}
}

func trap_timer(p ...interface{}) {
	fmt.Printf("Timer!")
}

func trap_syscall(p ...interface{}) {
	pid, ok   := p[0].(int)
	if !ok {
		pancake("weird pid")
	}
	proc, ok := allprocs[pid]
	if !ok {
		pancake("no such pid", pid)
	}
	fmt.Printf("syscall from %v. rescheduling... ", proc.Name())
	runtime.Procrunnable(pid)
}

func trap_pgfault(p ...interface{}) {
	pid, ok := p[0].(int)
	if !ok {
		pancake("weird pid")
	}
	fa, ok := p[1].(int)
	if !ok {
		pancake("bad fault address")
	}
	rip, ok := p[2].(int)
	if !ok {
		pancake("bad rip")
	}
	proc, ok := allprocs[pid]
	if !ok {
		pancake("no such pid", pid)
	}
	fmt.Printf("*** fault *** %v: addr %x, rip %x. killing... ",
	    proc.Name(), fa, rip)
	proc_kill(pid)
}

var allpages = map[int]*[512]int{}

func pg_test() {
	fmt.Print("page table test ")

	physaddr := 0x7c9e

	taddr := dmap(physaddr)
	fmt.Printf("boot code %x ", uint(taddr[396]))

	for p, v := range allpages {
		fmt.Printf(" [%p -> %x] ", v, p)
	}

	kpgdir := runtime.Kpmap()
	pte := pmap_walk(kpgdir, unsafe.Pointer(uintptr(0x7c00)), false, 0, allpages)
	fmt.Printf("boot pte %x ", *pte)

	paddr := 0x2200000000
	pte = pmap_walk(kpgdir, unsafe.Pointer(uintptr(paddr)), false, 0, allpages)
	if pte != nil {
		pancake("nyet")
	}
	pte = pmap_walk(kpgdir, unsafe.Pointer(uintptr(paddr)), true, PTE_W, allpages)
	fmt.Printf("null pte %x ", *pte)
	_, p_np := pg_new(allpages)
	*pte = p_np | PTE_P | PTE_W
	maddr := (*int)(unsafe.Pointer(uintptr(paddr)))
	fmt.Printf("new addr contents %x ", *maddr)
}

type proc_t struct {
	pid     int
	name    string
	pages   map[int]*[512]int
	pmap    *[512]int
	p_pmap  int
}

func (p *proc_t) Name() string {
	return "\"" + p.name + "\""
}

var allprocs = map[int]*proc_t{}

var pid_cur  int
func proc_new(name string) *proc_t {
	pid_cur++
	ret := &proc_t{}
	allprocs[pid_cur] = ret

	ret.name = name
	ret.pid = pid_cur
	ret.pages = make(map[int]*[512]int)

	return ret
}

func (p *proc_t) page_insert(va int, pg *[512]int, p_pg int,
    perms int, vempty bool) {
	p.pages[p_pg] = pg

	if p.pmap == nil {
		panic("null pmap")
	}

	dur := unsafe.Pointer(uintptr(va))
	pte := pmap_walk(p.pmap, dur, true, perms, p.pages)
	ninval := false
	if *pte & PTE_P != 0 {
		if vempty {
			panic("pte not empty")
		}
		ninval = true
	}
	*pte = p_pg | perms | PTE_P
	if ninval {
		runtime.Invlpg(dur)
	}
}

func proc_kill(pid int) {
	_, ok := allprocs[pid]
	if !ok {
		pancake("no pid", pid)
	}
	runtime.Prockill(pid)

	ms := runtime.MemStats{}
	runtime.ReadMemStats(&ms)
	before := ms.Alloc

	delete(allprocs, pid)
	runtime.GC()
	runtime.ReadMemStats(&ms)
	after := ms.Alloc
	fmt.Printf("reclaimed %vK ", (before-after)/1024)
}


func main() {
	// magic loop
	//if rand.Int() != 0 {
	//	for {
	//	}
	//}

	dmap_init()
	runtime.Install_traphandler(trapstub)

	trap_diex := func(c int) func(...interface{}) {
		return func(...interface{}) {
			fmt.Printf("[death on trap %v] ", c)
			pancake("perished")
		}
	}

	handlers := map[int]func(...interface{}) {
	     GPFAULT: trap_diex(13),
	     PGFAULT: trap_pgfault,
	     TIMER: trap_timer,
	     SYSCALL: trap_syscall}
	go trap(handlers)

	sys_test()
	//pg_test()

	fake_work()
}

func fake_work() {
	fmt.Printf("'network' test ")
	ch := make(chan packet)
	go genpackets(ch)

	process_packets(ch)
}

func pancake(msg ...interface{}) {
	runtime.Cli()
	fmt.Print(msg)
	for {
	}
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

