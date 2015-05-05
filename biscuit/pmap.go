package main

import "fmt"
import "runtime"
import "sync"
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
const VUSER     int = 0x59

// tracks all pages allocated by go internally by the kernel such as pmap pages
// allocated by go (not the bootloader/runtime)
var allpages = map[int]*[512]int{}
var allplock = sync.Mutex{}

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
	return rounddown(v + b - 1, b)
}

func caddr(l4 int, ppd int, pd int, pt int, off int) *int {
	ret := mkpg(l4, ppd, pd, pt)
	ret += off*8

	return (*int)(unsafe.Pointer(uintptr(ret)))
}

// cannot use pg_new until after dmap_init has been called (pmap_walk depends
// on direct map)
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

	if _, ok := ptracker[physaddr]; ok {
		panic("page already tracked")
	}
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
	// the default cpu qemu uses for x86_64 supports 1GB pages, but
	// doesn't report it in cpuid 0x80000001... i wonder why.
	_, _, _, edx  := runtime.Cpuid(0x80000001, 0)
	gbpages := edx & (1 << 26) != 0

	dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)
	if *dpte & PTE_P != 0 {
		panic("dmap slot taken")
	}

	apadd := func(kva *[512]int, phys int) {
		if _, ok := allpages[phys]; ok {
			panic("page already in allpages")
		}
		allpages[phys] = kva
	}

	pdpt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pdpt)))
	if ptn & ((1 << 12) - 1) != 0 {
		panic("page table not aligned")
	}
	p_pdpt := runtime.Vtop(pdpt)
	apadd(pdpt, p_pdpt)

	*dpte = p_pdpt | PTE_P | PTE_W

	size := (1 << 30)

	// make qemu use 2MB pages, like my hardware, to help expose bugs that
	// the hardware may encounter.
	//if gbpages || qemu {
	if gbpages {
		fmt.Printf("dmap via 1GB pages\n")
		for i := range pdpt {
			pdpt[i] = i*size | PTE_P | PTE_W | PTE_PS
		}
		return
	}
	fmt.Printf("1GB pages not supported\n")

	size = 1 << 21
	pdptsz := 1 << 30
	for i := range pdpt {
		pd := new([512]int)
		p_pd := runtime.Vtop(pd)
		apadd(pd, p_pd)
		for j := range pd {
			pd[j] = i*pdptsz + j*size | PTE_P | PTE_W | PTE_PS
		}
		pdpt[i] = p_pd | PTE_P | PTE_W
	}
}

// returns a page-aligned virtual address for the given physical address using
// the direct mapping
func dmap(p int) *[512]int {
	pa := uint(p)
	if pa >= 1 << 39 {
		panic("physical address too large")
	}

	v := int(uintptr(unsafe.Pointer(caddr(VDIRECT, 0, 0, 0, 0))))
	v += rounddown(int(pa), PGSIZE)
	return (*[512]int)(unsafe.Pointer(uintptr(v)))
}

// returns a byte aligned virtual address for the physical address as slice of
// uint8s
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
		// copy mappings of PS pages and the recursive mapping
		if c & PTE_PS != 0 {
			dst[i] = c
			continue
		}
		if depth == 4 && i == VREC {
			dst[i] = 0
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
	npm[VREC] = p_npm | PTE_P | PTE_W
	return npm, p_npm, doinval
}

func pmap_copy_par1(src *[512]int, dst *[512]int, depth int,
    mywg *sync.WaitGroup, pch chan *map[int]*[512]int,
    convch chan *map[int]bool) {
	pt := make(map[int]*[512]int)
	convs := make(map[int]bool)
	nwg := sync.WaitGroup{}
	for i, pte := range src {
		if pte & PTE_P == 0 {
			continue
		}
		if depth == 0 {
			if pte & PTE_U != 0 {
				dst[i] = pte
				if pte & PTE_W != 0 {
					v := pte &^ PTE_W
					v |= PTE_COW
					dst[i] = v
					src[i] = v
				}
				p_pg := pte & PTE_ADDR
				convs[p_pg] = true
			} else {
				dst[i] = pte
			}
			continue
		}
		if pte & PTE_PS != 0 {
			dst[i] = pte
			continue
		}
		if depth == 3 && i == VREC {
			dst[i] = 0
			continue
		}
		np, p_np := pg_new(pt)
		perms := pte & PTE_FLAGS
		dst[i] = p_np | perms
		nsrc := pe2pg(pte)
		nwg.Add(1)
		go pmap_copy_par1(nsrc, np, depth - 1, &nwg, pch, convch)
	}
	pch <- &pt
	convch <- &convs
	nwg.Wait()

	mywg.Done()
}

func pmap_copy_par(src *[512]int, pt map[int]*[512]int,
    ppt map[int]*[512]int) (*[512]int, int) {
	pch := make(chan *map[int]*[512]int, 512)
	convch := make(chan *map[int]bool, 512)
	done := make(chan bool)
	dst, p_dst := pg_new(pt)

	go func() {
		d1 := false
		d2 := false
		loopy: for {
			select {
			case nptt, ok := <- pch:
				var npt map[int]*[512]int
				if !ok {
					d1 = true
					pch = nil
					if d1 && d2 {
						break loopy
					}
				} else {
					npt = *nptt
				}
				for k, v := range npt {
					pt[k] = v
				}
			case nptt, ok := <- convch:
				var npt map[int]bool
				if !ok {
					d2 = true
					convch = nil
					if d1 && d2 {
						break loopy
					}
				} else {
					npt = *nptt
				}
				for phys, _ := range npt {
					va, ok := ppt[phys]
					if !ok {
						panic("wtf")
					}
					pt[phys] = va
				}
			}
		}
		done <- true
	}()

	wg := sync.WaitGroup{}
	wg.Add(1)
	pmap_copy_par1(src, dst, 3, &wg, pch, convch)
	close(pch)
	close(convch)
	<- done

	return dst, p_dst
}

func pmap_iter1(ptef func(int, *int), pm *[512]int, depth int, va int,
    startva bool) {
	starti := 0
	if startva {
		starti = va >> uint(12 + 9*depth)
		starti &= 0x1ff
	}
	mkva := func(ova int, d int, idx int) int {
		// canonicalize
		if d == 3 && idx >= 256 {
			ova = -1 &^ ((1 << 48) - 1)
		}
		return ova | idx << uint(9*d + 12)
	}
	for i := starti; i < len(pm); i++ {
		c := pm[i]
		cva := mkva(va, depth, i)
		if c & PTE_P  == 0 {
			continue
		}
		// skip recursive mapping
		if depth == 3 && i == VREC {
			continue
		}
		// run ptef on the PTE
		if depth == 0 || c & PTE_PS != 0 {
			ptef(cva, &pm[i])
			continue
		}
		// otherwise, recurse
		useva := i == starti
		nextpm := pe2pg(c)
		pmap_iter1(ptef, nextpm, depth - 1, cva, useva)
	}
}

func pmap_iter(ptef func(int, *int), pm *[512]int, va int) {
	// XXX pmap iter depth is between [0, 3], copy_pmap is [1, 4]...
	pmap_iter1(ptef, pm, 3, va, true)
}
// allocates a page tracked by allpages and maps it at va
func kmalloc(va int, perms int) {
	allplock.Lock()
	defer allplock.Unlock()
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

func physmapped1(pmap *[512]int, phys int, depth int, acc int,
    thresh int, tsz int) (bool, int) {
	for i, c := range pmap {
		if c & PTE_P == 0 {
			continue
		}
		if depth == 1 || c & PTE_PS != 0 {
			if c & PTE_ADDR == phys & PGMASK {
				ret := acc << 9 | i
				ret <<= 12
				if thresh == 0 {
					return true, ret
				}
				if  ret >= thresh && ret < thresh + tsz {
					return true, ret
				}
			}
			continue
		}
		// skip direct and recursive maps
		if depth == 4 && (i == VDIRECT || i == VREC) {
			continue
		}
		nextp := pe2pg(c)
		nexta := acc << 9 | i
		mapped, va := physmapped1(nextp, phys, depth - 1, nexta,
		    thresh, tsz)
		if mapped {
			return true, va
		}
	}
	return false, 0
}

func physmapped(pmap *[512]int, phys int) (bool, int) {
	return physmapped1(pmap, phys, 4, 0, 0, 0)
}

func physmapped_above(pmap *[512]int, phys int, thresh int, size int) (bool, int) {
	return physmapped1(pmap, phys, 4, 0, thresh, size)
}

func assert_no_phys(pmap *[512]int, phys int) {
	mapped, va := physmapped(pmap, phys)
	if mapped {
		panic(fmt.Sprintf("%v is mapped at page %#x", phys, va))
	}
}

func assert_no_va_map(pmap *[512]int, va int) {
	pte := pmap_walk(pmap, va, false, 0, nil)
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("va %#x is mapped", va))
	}
}
