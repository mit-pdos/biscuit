package vm

import "sync"

//import "sync/atomic"
import "unsafe"
import "runtime"
import "fmt"

import "mem"

// lowest userspace address
const USERMIN int = VUSER << 39
const DMAPLEN int = 1 << 39

var dmapinit bool

type Page_i interface {
	Refpg_new() (*mem.Pg_t, mem.Pa_t, bool)
	Refcnt(mem.Pa_t) int
	Dmap(mem.Pa_t) *mem.Pg_t
	Refup(mem.Pa_t)
	Refdown(mem.Pa_t) bool
}

// l is length of mapping in bytes
func Dmaplen(p mem.Pa_t, l int) []uint8 {
	_dmap := (*[DMAPLEN]uint8)(unsafe.Pointer(_vdirect))
	return _dmap[p : p+mem.Pa_t(l)]
}

// l is length of mapping in bytes. both p and l must be multiples of 4
func Dmaplen32(p uintptr, l int) []uint32 {
	if p%4 != 0 || l%4 != 0 {
		panic("not 32bit aligned")
	}
	_dmap := (*[DMAPLEN / 4]uint32)(unsafe.Pointer(_vdirect))
	p /= 4
	l /= 4
	return _dmap[p : p+uintptr(l)]
}

func Phys_init() *mem.Physmem_t {
	// reserve 128MB of pages
	//respgs := 1 << 15
	respgs := 1 << 16
	//respgs := 1 << (31 - 12)
	// 7.5 GB
	//respgs := 1835008
	//respgs := 1 << 18 + (1 <<17)
	phys := Physmem
	phys.pgs = make([]Physpg_t, respgs)
	for i := range phys.pgs {
		phys.pgs[i].refcnt = -10
	}
	first := mem.Pa_t(runtime.Get_phys())
	fpgn := _pg2pgn(first)
	phys.startn = fpgn
	phys.freei = 0
	phys.pmaps = ^uint32(0)
	phys.pgs[0].refcnt = 0
	phys.pgs[0].nexti = ^uint32(0)
	last := phys.freei
	for i := 0; i < respgs-1; i++ {
		p_pg := mem.Pa_t(runtime.Get_phys())
		pgn := _pg2pgn(p_pg)
		idx := pgn - phys.startn
		// Get_phys() may skip regions.
		if int(idx) >= len(phys.pgs) {
			if respgs-i > int(float64(respgs)*0.01) {
				panic("got many bad pages")
			}
			break
		}
		phys.pgs[idx].refcnt = 0
		phys.pgs[last].nexti = idx
		phys.pgs[idx].nexti = ^uint32(0)
		last = idx
	}
	fmt.Printf("Reserved %v pages (%vMB)\n", respgs, respgs>>8)
	return phys
}

func shl(c uint) uint {
	return 12 + 9*c
}

func pgbits(v uint) (uint, uint, uint, uint) {
	lb := func(c uint) uint {
		return (v >> shl(c)) & 0x1ff
	}
	return lb(3), lb(2), lb(1), lb(0)
}

func mkpg(l4 int, l3 int, l2 int, l1 int) int {
	lb := func(c uint) uint {
		var ret uint
		switch c {
		case 3:
			ret = uint(l4) & 0x1ff
		case 2:
			ret = uint(l3) & 0x1ff
		case 1:
			ret = uint(l2) & 0x1ff
		case 0:
			ret = uint(l1) & 0x1ff
		}
		return ret << shl(c)
	}

	return int(lb(3) | lb(2) | lb(1) | lb(0))
}

func caddr(l4 int, ppd int, pd int, pt int, off int) *int {
	ret := mkpg(l4, ppd, pd, pt)
	ret += off * 8

	return (*int)(unsafe.Pointer(uintptr(ret)))
}

var Zerobpg *Bytepg_t
var Kents = make([]Kent_t, 0, 5)

// installs a direct map for 512G of physical memory via the recursive mapping
func Dmap_init() {
	//kpmpages.pminit()

	// the default cpu qemu uses for x86_64 supports 1GB pages, but
	// doesn't report it in cpuid 0x80000001... i wonder why.
	_, _, _, edx := runtime.Cpuid(0x80000001, 0)
	gbpages := edx&(1<<26) != 0

	_, _, _, edx = runtime.Cpuid(0x1, 0)
	gse := edx&(1<<13) != 0
	if !gse {
		panic("no global pages")
	}
	if runtime.Rcr4()&(1<<7) == 0 {
		panic("global disabled")
	}

	_dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)
	dpte := (*mem.Pa_t)(unsafe.Pointer(_dpte))
	if *dpte&PTE_P != 0 {
		panic("dmap slot taken")
	}

	pdpt := new(Pmap_t)
	ptn := mem.Pa_t(unsafe.Pointer(pdpt))
	if ptn&PGOFFSET != 0 {
		panic("page table not aligned")
	}
	p_pdpt, ok := runtime.Vtop(unsafe.Pointer(pdpt))
	if !ok {
		panic("must succeed")
	}
	kpgadd(pdpt)

	*dpte = mem.Pa_t(p_pdpt) | PTE_P | PTE_W

	size := mem.Pa_t(1 << 30)

	// make qemu use 2MB pages, like my hardware, to help expose bugs that
	// the hardware may encounter.
	if gbpages {
		fmt.Printf("dmap via 1GB pages\n")
		for i := range pdpt {
			pdpt[i] = mem.Pa_t(i)*size | PTE_P | PTE_W | PTE_PS
		}
		return
	}
	fmt.Printf("1GB pages not supported\n")

	size = 1 << 21
	pdptsz := mem.Pa_t(1 << 30)
	for i := range pdpt {
		pd := new(Pmap_t)
		p_pd, ok := runtime.Vtop(unsafe.Pointer(pd))
		if !ok {
			panic("must succeed")
		}
		kpgadd(pd)
		for j := range pd {
			pd[j] = mem.Pa_t(i)*pdptsz +
				mem.Pa_t(j)*size | PTE_P | PTE_W | PTE_PS
		}
		pdpt[i] = mem.Pa_t(p_pd) | PTE_P | PTE_W
	}

	// fill in kent, the list of kernel pml4 entries. make sure we will
	// panic if the runtime adds new mappings that we don't record here.
	runtime.Pml4freeze()
	for i, e := range Kpmap() {
		if e&PTE_U == 0 && e&PTE_P != 0 {
			ent := Kent_t{i, e}
			Kents = append(Kents, ent)
		}
	}
	dmapinit = true

	// Physmem.Refpg_new() uses the Zeropg to zero the page
	Zeropg, P_zeropg, ok = Physmem._refpg_new()
	if !ok {
		panic("oom in dmap init")
	}
	for i := range Zeropg {
		Zeropg[i] = 0
	}
	Physmem.Refup(P_zeropg)
	Zerobpg = Pg2bytes(Zeropg)
}

var Kpmapp *mem.Pmap_t

func Kpmap() *mem.Pmap_t {
	if Kpmapp == nil {
		dur := caddr(VREC, VREC, VREC, VREC, 0)
		Kpmapp = (*Pmap_t)(unsafe.Pointer(dur))
	}
	return Kpmapp
}

// tracks all pages allocated by go internally by the kernel such as pmap pages
// allocated by the kernel (not the bootloader/runtime)
var kplock = sync.Mutex{}
var kpages = pgtracker_t{}

func kpgadd(pg *mem.Pmap_t) {
	va := uintptr(unsafe.Pointer(pg))
	pgn := int(va >> 12)
	if _, ok := kpages[pgn]; ok {
		panic("page already in kpages")
	}
	kpages[pgn] = pg
}

// tracks pages
type pgtracker_t map[int]*mem.Pmap_t

// allocates a page tracked by kpages and maps it at va. only used during AP
// bootup.
// XXX remove this crap
func Kmalloc(va uintptr, perms mem.Pa_t) {
	kplock.Lock()
	defer kplock.Unlock()
	_, p_pg, ok := Physmem.Refpg_new()
	if !ok {
		panic("oom in init?")
	}
	Physmem.Refup(p_pg)
	pte, err := pmap_walk(Kpmap(), int(va), perms)
	if err != 0 {
		panic("oom during AP init")
	}
	if pte != nil && *pte&PTE_P != 0 {
		panic(fmt.Sprintf("page already mapped %#x", va))
	}
	*pte = p_pg | PTE_P | perms
}

func Assert_no_va_map(pmap *mem.Pmap_t, va uintptr) {
	pte := Pmap_lookup(pmap, int(va))
	if pte != nil && *pte&PTE_P != 0 {
		panic(fmt.Sprintf("va %#x is mapped", va))
	}
}
