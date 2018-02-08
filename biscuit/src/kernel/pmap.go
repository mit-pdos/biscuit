package main

import "fmt"
import "runtime"
import "sync"
import "unsafe"


// tracks pages
type pgtracker_t map[int]*common.Pmap_t

// tracks all pages allocated by go internally by the kernel such as pmap pages
// allocated by the kernel (not the bootloader/runtime)
var kplock = sync.Mutex{}
var kpages = pgtracker_t{}

func kpgadd(pg *pmap_t) {
	va := uintptr(unsafe.Pointer(pg))
	pgn := int(va >> 12)
	if _, ok := kpages[pgn]; ok {
		panic("page already in kpages")
	}
	kpages[pgn] = pg
}

func shl(c uint) uint {
	return 12 + 9 * c
}

func pgbits(v uint) (uint, uint, uint, uint) {
	lb := func (c uint) uint {
		return (v >> shl(c)) & 0x1ff
	}
	return lb(3), lb(2), lb(1), lb(0)
}

func mkpg(l4 int, l3 int, l2 int, l1 int) int {
	lb := func (c uint) uint {
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
	ret += off*8

	return (*int)(unsafe.Pointer(uintptr(ret)))
}

var kpmapp      *pmap_t

func kpmap() *pmap_t {
	if kpmapp == nil {
		dur := caddr(VREC, VREC, VREC, VREC, 0)
		kpmapp = (*pmap_t)(unsafe.Pointer(dur))
	}
	return kpmapp
}

var zerobpg *bytepg_t



// installs a direct map for 512G of physical memory via the recursive mapping
func dmap_init() {
	//kpmpages.pminit()

	// the default cpu qemu uses for x86_64 supports 1GB pages, but
	// doesn't report it in cpuid 0x80000001... i wonder why.
	_, _, _, edx  := runtime.Cpuid(0x80000001, 0)
	gbpages := edx & (1 << 26) != 0

	_dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)
	dpte := (*pa_t)(unsafe.Pointer(_dpte))
	if *dpte & PTE_P != 0 {
		panic("dmap slot taken")
	}

	pdpt  := new(pmap_t)
	ptn := pa_t(unsafe.Pointer(pdpt))
	if ptn & PGOFFSET != 0 {
		panic("page table not aligned")
	}
	p_pdpt, ok := runtime.Vtop(unsafe.Pointer(pdpt))
	if !ok {
		panic("must succeed")
	}
	kpgadd(pdpt)

	*dpte = pa_t(p_pdpt) | PTE_P | PTE_W

	size := pa_t(1 << 30)

	// make qemu use 2MB pages, like my hardware, to help expose bugs that
	// the hardware may encounter.
	if gbpages {
		fmt.Printf("dmap via 1GB pages\n")
		for i := range pdpt {
			pdpt[i] = pa_t(i)*size | PTE_P | PTE_W | PTE_PS
		}
		return
	}
	fmt.Printf("1GB pages not supported\n")

	size = 1 << 21
	pdptsz := pa_t(1 << 30)
	for i := range pdpt {
		pd := new(pmap_t)
		p_pd, ok := runtime.Vtop(unsafe.Pointer(pd))
		if !ok {
			panic("must succeed")
		}
		kpgadd(pd)
		for j := range pd {
			pd[j] = pa_t(i)*pdptsz +
			   pa_t(j)*size | PTE_P | PTE_W | PTE_PS
		}
		pdpt[i] = pa_t(p_pd) | PTE_P | PTE_W
	}

	// fill in kent, the list of kernel pml4 entries. make sure we will
	// panic if the runtime adds new mappings that we don't record here.
	runtime.Pml4freeze()
	for i, e := range kpmap() {
		if e & PTE_U == 0 && e & PTE_P != 0 {
			ent := kent_t{i, e}
			kents = append(kents, ent)
		}
	}
	_dmapinit = true

	// refpg_new() uses the zeropg to zero the page
	zeropg, p_zeropg, ok = _refpg_new()
	if !ok {
		panic("oom in dmap init")
	}
	for i := range zeropg {
		zeropg[i] = 0
	}
	refup(p_zeropg)
	zerobpg = pg2bytes(zeropg)
}


var kents = make([]kent_t, 0, 5)


// allocates a page tracked by kpages and maps it at va. only used during AP
// bootup.
// XXX remove this crap
func kmalloc(va uintptr, perms pa_t) {
	kplock.Lock()
	defer kplock.Unlock()
	_, p_pg, ok := refpg_new()
	if !ok {
		panic("oom in init?")
	}
	refup(p_pg)
	pte, err := pmap_walk(kpmap(), int(va), perms)
	if err != 0 {
		panic("oom during AP init")
	}
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("page already mapped %#x", va))
	}
	*pte = p_pg | PTE_P | perms
}

func assert_no_va_map(pmap *pmap_t, va uintptr) {
	pte := pmap_lookup(pmap, int(va))
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("va %#x is mapped", va))
	}
}
