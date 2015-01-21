package main

import "fmt"
import "math/rand"
import "runtime"
import "unsafe"

type trapstore_t struct {
	trapno	int
	ucookie int
}
const ntrapst	int = 64
var trapstore [ntrapst]trapstore_t
var tshead	int
var tstail	int

func tsnext(c int) int {
	return (c + 1) % ntrapst
}

// trap cannot do anything that may have side-effects on the runtime (like
// fmt.Print, or use pancake!). the reason is that, by design, goroutines are
// scheduled cooperatively in the runtime. trap interrupts the runtime though,
// and then tries to execute more gocode, thus doing things the runtime did not
// expect.
//go:nosplit
func trapstub(tf *[23]int, ucookie int) {

	tfregs    := 16
	tf_trapno := tfregs
	//tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	//tf_rflags   := tfregs + 4
	//fl_intf     := 1 << 9

	// add to trap circular buffer for actual trap handler
	if tsnext(tshead) == tstail {
		runtime.Pnum(4)
		for {
		}
	}
	trapno := tf[tf_trapno]
	trapstore[tshead].trapno = int(trapno)
	trapstore[tshead].ucookie = ucookie
	tshead = tsnext(tshead)

	SYSCALL   := 64
	TIMER     := 32
	//GPFAULT   := 13
	PGFAULT   := 14

	if trapno == SYSCALL {
		// yield until the syscall is handled
		runtime.Useryield()
	} else if trapno == TIMER {
		// timer interrupts are not exposed to this traphandler yet
		runtime.Pnum(0x41)
		for {
		}
	} else {
		runtime.Pnum(trapno)
		if trapno == PGFAULT {
			runtime.Pnum(runtime.Rcr2())
			rip := tf[tf_rip]
			runtime.Pnum(rip)
		}
		runtime.Pnum(0x42)
		for {
		}
	}
}

func trap(handlers map[int]func()) {
	for {
		for tstail == tshead {
			// no work
			runtime.Gosched()
		}

		curtrap := trapstore[tstail].trapno
		uc := trapstore[tstail].ucookie
		tstail = tsnext(tstail)

		if h, ok := handlers[curtrap]; ok {
			fmt.Printf("[trap %v] ", curtrap)
			go h()
			continue
		}
		fmt.Printf("no handler for trap %v, ucookie %x ", curtrap, uc)
	}
}

func trap_timer() {
	fmt.Printf("Timer!")
}

var PTE_P     int = 1 << 0
var PTE_W     int = 1 << 1
var PTE_U     int = 1 << 2
var PTE_PS    int = 1 << 7
var PGSIZE    int = 1 << 12
var PTE_ADDR  int = ^(0xfff)

var VREC      int = 0x42
var VDIRECT   int = 0x44

func shl(c uint) uint {
	return 12 + 9 * c
}

func pgbits(v uint) (uint, uint, uint, uint) {
	lb := func (c uint) uint {
		return (v >> shl(c)) & 0x1ff
	}
	return lb(3), lb(2), lb(1), lb(0)
}

func rounddown(v int, b int) int {
	return int(v & ^(b - 1))
}

func caddr(l4 int, ppd int, pd int, pt int, off int) *int {
	ret := l4 << shl(3) | ppd << shl(2) | pd << shl(1) | pt << shl(0)
	ret += off*8

	return (*int)(unsafe.Pointer(uintptr(ret)))
}

func new_pgt(ptracker map[int]*[512]int) (*[512]int, int) {
	pt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pt)))
	if ptn & ((1 << 12) - 1) != 0 {
		pancake("page table not aligned", ptn)
	}
	pte := pgdir_walk(runtime.Kpgdir(), unsafe.Pointer(pt),
	    0, 0, ptracker)
	if pte == nil {
		pancake("must be mapped")
	}
	physaddr := *pte & PTE_ADDR

	if ptracker != nil {
		ptracker[physaddr] = pt
	}

	return pt, physaddr
}

// installs a direct map for 512G of physical memory via the recursive mapping
func dmap_init() {
	dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)

	pdpt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pdpt)))
	if ptn & ((1 << 12) - 1) != 0 {
		pancake("page table not aligned", ptn)
	}
	p_pdpt := runtime.Vtop(pdpt)
	allpages[p_pdpt] = pdpt

	for i := range pdpt {
		pdpt[i] = i*PGSIZE | PTE_P | PTE_W | PTE_PS
	}

	*dpte = p_pdpt | PTE_P | PTE_W
}

// returns a virtual address for the given physical address using the direct
// mapping
func dmap(pa int) *[512]int {
	if pa >= 1 << 39 {
		pancake("physical address too large")
	}

	v := int(uintptr(unsafe.Pointer(caddr(VDIRECT, 0, 0, 0, 0))))
	v += rounddown(pa, PGSIZE)
	return (*[512]int)(unsafe.Pointer(uintptr(v)))
}

func pe2pg(pe int) *[512]int {
	addr := pe & PTE_ADDR
	return dmap(addr)
}

// requires direct mapping
func pgdir_walk(pml4 *[512]int, v unsafe.Pointer, create int,
    perms int, ptracker map[int]*[512]int) *int {

	vn := uint(uintptr(v))
	l4b, pdpb, pdb, ptb := pgbits(vn)

	instpg := func(pg *[512]int, idx uint) int {
		_, p_np := new_pgt(ptracker)
		npte :=  p_np | perms | PTE_P
		pg[idx] = npte
		return npte
	}

	pe := pml4[l4b]
	if pe & PTE_P == 0 {
		if create == 0 {
			return nil
		}
		pe = instpg(pml4, l4b)
	}
	next := pe2pg(pe)
	pe = next[pdpb]
	if pe & PTE_P == 0 {
		if create == 0 {
			return nil
		}
		pe = instpg(next, pdpb)
	}
	next = pe2pg(pe)
	pe = next[pdb]
	if pe & PTE_P == 0 {
		if create == 0 {
			return nil
		}
		pe = instpg(next, pdb)
	}
	next = pe2pg(pe)
	return &next[ptb]
}

func pg_test() {
	fmt.Print("page table test ")

	physaddr := 0x7c9e

	taddr := dmap(physaddr)
	fmt.Printf("boot code %x ", uint(taddr[396]))

	for p, v := range allpages {
		fmt.Printf(" [%p -> %x] ", v, p)
	}

	kpgdir := runtime.Kpgdir()
	pte := pgdir_walk(kpgdir, unsafe.Pointer(uintptr(0x7c00)), 0, 0, allpages)
	fmt.Printf("boot pte %x ", *pte)

	paddr := 0x2200000000
	pte = pgdir_walk(kpgdir, unsafe.Pointer(uintptr(paddr)), 0, 0, allpages)
	if pte != nil {
		pancake("nyet")
	}
	pte = pgdir_walk(kpgdir, unsafe.Pointer(uintptr(paddr)), 1, PTE_W, allpages)
	fmt.Printf("null pte %x ", *pte)
	_, p_np := new_pgt(allpages)
	*pte = p_np | PTE_P | PTE_W
	maddr := (*int)(unsafe.Pointer(uintptr(paddr)))
	fmt.Printf("new addr contents %x ", *maddr)
}

func user_test() {
	fmt.Printf("add 'user' prog ")

	var tf [23]int
	tfregs    := 16
	tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	tf_rflags := tfregs + 4
	fl_intf   := 1 << 9
	tf_ss     := tfregs + 6
	tf_cs     := tfregs + 3

	tf[tf_rip] = runtime.Fnaddr(runtime.Turdyprog)
	tf[tf_rsp] = runtime.Newstack()
	tf[tf_rflags] = fl_intf
	tf[tf_cs] = 6 << 3 | 3
	tf[tf_ss] = 7 << 3 | 3

	pgtbl := runtime.Copy_pgt(runtime.Kpgdir())
	runtime.Useradd(&tf, 0x31337, pgtbl)
}

var allpages = map[int]*[512]int{}

func main() {
	// magic loop
	//if rand.Int() != 0 {
	//	for {
	//	}
	//}

	dmap_init()
	runtime.Install_traphandler(trapstub)

	trap_diex := func(c int) func() {
		return func() {
			fmt.Printf("[death on trap %v] ", c)
			pancake("perished")
		}
	}

	handlers := map[int]func() { 32: trap_timer, 14:trap_diex(14), 13:trap_diex(13)}
	go trap(handlers)

	//user_test()
	pg_test()

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

