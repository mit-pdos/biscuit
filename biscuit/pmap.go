package main

import "fmt"
import "runtime"
import "unsafe"

const PTE_P     int = 1 << 0
const PTE_W     int = 1 << 1
const PTE_U     int = 1 << 2
const PTE_PCD   int = 1 << 4
const PTE_PS    int = 1 << 7
const PTE_COW   int = 1 << 9	// our flags
const PGSIZE    int = 1 << 12
const PGOFFSET  int = 0xfff
const PGMASK    int = ^(PGOFFSET)
const PTE_ADDR  int = PGMASK
const PTE_FLAGS int = 0x1f	// only masks P, W, U, PWT, and PCD


const VREC      int = 0x42
const VDIRECT   int = 0x44
const VEND      int = 0x50

// tracks all pages allocated by go internally by the kernel such as pmap pages
// allocated by go (not the bootloader/runtime)
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

func rounddown(v int, b int) int {
	return v - (v % b)
}

func roundup(v int, b int) int {
	return v + (b - (v % b))
}

func caddr(l4 int, ppd int, pd int, pt int, off int) *int {
	ret := mkpg(l4, ppd, pd, pt)
	ret += off*8

	return (*int)(unsafe.Pointer(uintptr(ret)))
}

func pg_new(ptracker map[int]*[512]int) (*[512]int, int) {

	pt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pt)))
	if ptn & (PGSIZE - 1) != 0 {
		panic("page not aligned")
	}
	// pmap walk for every allocation -- a cost of allocating pages with
	// the garbage collector.
	pte := pmap_walk(kpmap(), int(uintptr(unsafe.Pointer(pt))),
	    false, 0, nil)
	if pte == nil {
		panic("must be mapped")
	}
	physaddr := *pte & PTE_ADDR

	ptracker[physaddr] = pt

	return pt, physaddr
}

var kpmapp      *[512]int

func kpmap() *[512]int {
	if kpmapp == nil {
		kpmapp = runtime.Kpmap()
	}
	return kpmapp
}

// installs a direct map for 512G of physical memory via the recursive mapping
func dmap_init() {
	dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)

	pdpt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pdpt)))
	if ptn & ((1 << 12) - 1) != 0 {
		panic("page table not aligned")
	}
	p_pdpt := runtime.Vtop(pdpt)
	allpages[p_pdpt] = pdpt

	for i := range pdpt {
		pdpt[i] = i*(1 << 30) | PTE_P | PTE_W | PTE_PS
	}

	if *dpte & PTE_P != 0 {
		panic("dmap slot taken")
	}
	*dpte = p_pdpt | PTE_P | PTE_W
}

// returns a virtual address for the given physical address using the direct
// mapping
func dmap(p int) *[512]int {
	pa := uint(p)
	if pa >= 1 << 39 {
		panic("physical address too large")
	}

	v := int(uintptr(unsafe.Pointer(caddr(VDIRECT, 0, 0, 0, 0))))
	v += rounddown(int(pa), PGSIZE)
	return (*[512]int)(unsafe.Pointer(uintptr(v)))
}

func dmap8(p int) []uint8 {
	pg := dmap(p)
	off := p & PGOFFSET
	bpg := (*[PGSIZE]uint8)(unsafe.Pointer(pg))
	return bpg[off:]
}

func pe2pg(pe int) *[512]int {
	addr := pe & PTE_ADDR
	return dmap(addr)
}

// requires direct mapping
func pmap_walk(pml4 *[512]int, v int, create bool, perms int,
    ptracker map[int]*[512]int) *int {
	vn := uint(uintptr(v))
	l4b, pdpb, pdb, ptb := pgbits(vn)
	if l4b >= uint(VREC) && l4b <= uint(VEND) {
		panic(fmt.Sprintf("map in special slots: %#x", l4b))
	}

	if v & PGMASK == 0 && create {
		panic("mapping page 0");
	}

	instpg := func(pg *[512]int, idx uint) int {
		_, p_np := pg_new(ptracker)
		npte :=  p_np | perms | PTE_P
		pg[idx] = npte
		return npte
	}

	cpe := func(pe int) *[512]int {
		if pe & PTE_PS != 0 {
			panic("insert mapping into PS page")
		}
		return pe2pg(pe)
	}

	pe := pml4[l4b]
	if pe & PTE_P == 0 {
		if !create {
			return nil
		}
		pe = instpg(pml4, l4b)
	}
	next := cpe(pe)
	pe = next[pdpb]
	if pe & PTE_P == 0 {
		if !create {
			return nil
		}
		pe = instpg(next, pdpb)
	}
	next = cpe(pe)
	pe = next[pdb]
	if pe & PTE_P == 0 {
		if !create {
			return nil
		}
		pe = instpg(next, pdb)
	}
	next = cpe(pe)
	return &next[ptb]
}

func copy_pmap1(ptemod func(int) (int, int), dst *[512]int, src *[512]int,
    depth int, ptracker map[int]*[512]int) bool {

	doinval := false
	for i, c := range src {
		if c & PTE_P  == 0 {
			continue
		}
		if depth == 1 {
			// copy ptes
			val := c
			srcval := val
			dstval := val
			if ptemod != nil {
				srcval, dstval = ptemod(val)
			}
			if srcval != val {
				src[i] = srcval
				doinval = true
			}
			dst[i] = dstval
			continue
		}
		// copy mappings of pages > PGSIZE
		if c & PTE_PS != 0 {
			dst[i] = c
			continue
		}
		// otherwise, recursively copy
		np, p_np := pg_new(ptracker)
		perms := c & PTE_FLAGS
		dst[i] = p_np | perms
		nsrc := pe2pg(c)
		if copy_pmap1(ptemod, np, nsrc, depth - 1, ptracker) {
			doinval = true
		}
	}

	return doinval
}

// deep copies the pmap. ptemod is an optional function that takes the
// original PTE as an argument and returns two values: new PTE for the pmap
// being copied and PTE for the new pmap.
func copy_pmap(ptemod func(int) (int, int), pm *[512]int,
    ptracker map[int]*[512]int) (*[512]int, int, bool) {
	npm, p_npm := pg_new(ptracker)
	doinval := copy_pmap1(ptemod, npm, pm, 4, ptracker)
	return npm, p_npm, doinval
}

func pmap_cperms(pm *[512]int, va int, nperms int) {
	b1, b2, b3, b4 := pgbits(uint(va))
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

// allocates a page tracked by allpages and maps it at va
func kmalloc(va int, perms int) {
	_, p_pg := pg_new(allpages)
	pte := pmap_walk(kpmap(), va, true, perms, allpages)
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("page already mapped %#x", va))
	}
	*pte = p_pg | PTE_P | perms
}

func is_mapped(pmap *[512]int, va int, size int) bool {
	p := rounddown(va, PGSIZE)
	end := roundup(va + size, PGSIZE)
	for ; p < end; p += PGSIZE {
		pte := pmap_walk(pmap, p, false, 0, nil)
		if pte == nil || *pte & PTE_P == 0 {
			return false
		}
	}
	return true
}

func invlpg(va int) {
	dur := unsafe.Pointer(uintptr(va))
	runtime.Invlpg(dur)
}
