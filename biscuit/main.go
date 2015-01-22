package main

import "fmt"
import "math/rand"
import "runtime"
import "unsafe"

type trapstore_t struct {
	trapno    int
	pid       int
	faultaddr int
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
		}
		// yield until the syscall/fault is handled
		runtime.Useryield()
	case TIMER:
		// timer interrupts are not passed yet
		runtime.Pnum(0x41)
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
	uc, ok := p[0].(int)
	if !ok {
		pancake("weird pid")
	}
	fmt.Printf("syscall from %x. rescheduling... ", uc)
	runtime.Userrunnable(uc)
}

func trap_pgfault(p ...interface{}) {
	uc, ok := p[0].(int)
	if !ok {
		pancake("weird pid")
	}
	fa, ok := p[1].(int)
	if !ok {
		pancake("bad fault address")
	}
	fmt.Printf("fault from %x at %x. terminating... ", uc, fa)
	runtime.Userkill(uc)
}

var PTE_P     int = 1 << 0
var PTE_W     int = 1 << 1
var PTE_U     int = 1 << 2
var PTE_PS    int = 1 << 7
var PGSIZE    int = 1 << 12
var PTE_ADDR  int = ^(0xfff)
var PTE_FLAGS int = 0x1f

var VREC      int = 0x42
var VDIRECT   int = 0x44

var allpages = map[int]*[512]int{}

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

func new_mpg(ptracker map[int]*[512]int) (*[512]int, int) {
	pt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pt)))
	if ptn & (PGSIZE - 1) != 0 {
		pancake("page table not aligned", ptn)
	}
	pte := pmap_walk(runtime.Kpmap(), unsafe.Pointer(pt),
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
func dmap(p int) *[512]int {
	pa := uint(p)
	if pa >= 1 << 39 {
		pancake("physical address too large", pa)
	}

	v := int(uintptr(unsafe.Pointer(caddr(VDIRECT, 0, 0, 0, 0))))
	v += rounddown(int(pa), PGSIZE)
	return (*[512]int)(unsafe.Pointer(uintptr(v)))
}

func pe2pg(pe int) *[512]int {
	addr := pe & PTE_ADDR
	return dmap(addr)
}

// requires direct mapping
func pmap_walk(pml4 *[512]int, v unsafe.Pointer, create int,
    perms int, ptracker map[int]*[512]int) *int {

	vn := uint(uintptr(v))
	l4b, pdpb, pdb, ptb := pgbits(vn)

	instpg := func(pg *[512]int, idx uint) int {
		_, p_np := new_mpg(ptracker)
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

	kpgdir := runtime.Kpmap()
	pte := pmap_walk(kpgdir, unsafe.Pointer(uintptr(0x7c00)), 0, 0, allpages)
	fmt.Printf("boot pte %x ", *pte)

	paddr := 0x2200000000
	pte = pmap_walk(kpgdir, unsafe.Pointer(uintptr(paddr)), 0, 0, allpages)
	if pte != nil {
		pancake("nyet")
	}
	pte = pmap_walk(kpgdir, unsafe.Pointer(uintptr(paddr)), 1, PTE_W, allpages)
	fmt.Printf("null pte %x ", *pte)
	_, p_np := new_mpg(allpages)
	*pte = p_np | PTE_P | PTE_W
	maddr := (*int)(unsafe.Pointer(uintptr(paddr)))
	fmt.Printf("new addr contents %x ", *maddr)
}

func copy_pmap1(dst *[512]int, src *[512]int, depth int,
    ptracker map[int]*[512]int) {

	for i, c := range src {
		if c & PTE_P  == 0 {
			continue
		}
		if depth == 1 {
			// copy ptes
			dst[i] = c
			continue
		}
		// copy mappings of pages > PGSIZE
		if c & PTE_PS != 0 {
			dst[i] = c
			continue
		}
		// otherwise, recursively copy
		np, p_np := new_mpg(ptracker)
		perms := c & PTE_FLAGS
		dst[i] = p_np | perms
		nsrc := pe2pg(c)
		copy_pmap1(np, nsrc, depth - 1, ptracker)
	}
}

func copy_pmap(pm *[512]int, ptracker map[int]*[512]int) (*[512]int, int) {
	npm, p_npm := new_mpg(ptracker)
	copy_pmap1(npm, pm, 4, ptracker)
	return npm, p_npm
}

func pmap_cperms(pm *[512]int, va unsafe.Pointer, nperms int) {
	b1, b2, b3, b4 := pgbits(uint(uintptr(va)))
	if pm[b1] & PTE_P == 0 {
		return
	}
	pm[b1] |= nperms
	next := pe2pg(pm[b1])
	if next[b2] & PTE_P == 0 {
		return
	}
	next[b2] |= nperms
	next = pe2pg(next[b2])
	if next[b3] & PTE_P == 0 {
		return
	}
	next[b3] |= nperms
	next = pe2pg(next[b3])
	if next[b4] & PTE_P == 0 {
		return
	}
	next[b4] |= nperms
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

	fnaddr := runtime.Fnaddr(runtime.Turdyprog)
	stack, p_stack := new_mpg(allpages)
	tf[tf_rip] = fnaddr
	tf[tf_rsp] = int(uintptr(unsafe.Pointer(stack))) + PGSIZE - 8
	tf[tf_rflags] = fl_intf
	tf[tf_cs] = 6 << 3 | 3
	tf[tf_ss] = 7 << 3 | 3

	// copy kernel page table, map new stack
	kpmap := runtime.Kpmap()
	upmap, p_upmap := copy_pmap(kpmap, allpages)
	spte := pmap_walk(upmap, unsafe.Pointer(stack), 1, PTE_U | PTE_W, allpages)
	*spte = p_stack | PTE_P | PTE_W | PTE_U
	// set user accessible for page containing Turdyprog. it sometimes
	// spans two pages.
	fnaddrp := unsafe.Pointer(uintptr(fnaddr))
	fnaddrp2 := unsafe.Pointer(uintptr(fnaddr + PGSIZE))
	pmap_cperms(upmap, fnaddrp, PTE_U)
	pmap_cperms(upmap, fnaddrp2, PTE_U)
	// stack
	pmap_cperms(upmap, unsafe.Pointer(stack), PTE_U)
	// VGA too
	pmap_cperms(upmap, unsafe.Pointer(uintptr(0xb8000)), PTE_U)
	pmap_cperms(upmap, unsafe.Pointer(uintptr(0xb8000)), PTE_U)
	// "system call"
	daddr := runtime.Fnaddr(runtime.Death)
	pmap_cperms(upmap, unsafe.Pointer(uintptr(daddr)), PTE_U)
	runtime.Useradd(&tf, 0x31337, p_upmap)
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

	user_test()
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

