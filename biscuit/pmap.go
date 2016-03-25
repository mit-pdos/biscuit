package main

import "fmt"
import "runtime"
import "sync"
import "unsafe"

const PTE_P      int = 1 << 0
const PTE_W      int = 1 << 1
const PTE_U      int = 1 << 2
const PTE_PCD    int = 1 << 4
const PTE_PS     int = 1 << 7
// our flags; bits 9-11 are ignored for all page map entries in long mode
const PTE_COW    int = 1 << 9
const PTE_WASCOW int = 1 << 10
const PGSIZE     int = 1 << 12
const PGSIZEW    uintptr = uintptr(PGSIZE)
const PGSHIFT    uint = 12
const PGOFFSET   int = 0xfff
const PGMASK     int = ^(PGOFFSET)
const PTE_ADDR   int = PGMASK
const PTE_FLAGS  int = (PTE_P | PTE_W | PTE_U | PTE_PCD | PTE_PS | PTE_COW |
    PTE_WASCOW)

const VREC      int = 0x42
const VDIRECT   int = 0x44
const VEND      int = 0x50
const VUSER     int = 0x59

// these types holds references to pages so the GC doesn't reclaim them -- the
// GC cannot find them without our help because it doesn't know how to trace a
// page map since references are physical addresses

// tracks pmap pages; pmap pages are never removed once added (until the
// process dies and its pmap is reclaimed)
type pmtracker_t struct {
	pms	[]*[512]int
}

func (pmt *pmtracker_t) pminit() {
	pmt.pms = make([]*[512]int, 0, 30)
}

func (pmt *pmtracker_t) pmadd(pm *[512]int) {
	pmt.pms = append(pmt.pms, pm)
}

// XXX right now, the caller has to use a vmseg to keep track of the seg and
// manually insert it into the page map. it would be easier to only use vmseg:
// populate vmseg with page information then install a vmseg into a taret page
// map.
type vmseg_t struct {
	start	int
	end	int
	startn	int
	next	*vmseg_t
	pages	[]*[512]int
}

func (vs *vmseg_t) seg_init(startva, len int) {
	vs.start = rounddown(startva, PGSIZE)
	vs.end = roundup(startva + len, PGSIZE)
	vs.next = nil
	vs.startn = vs.start >> PGSHIFT
	l := vs.end - vs.start
	l /= PGSIZE
	vs.pages = make([]*[512]int, l)
}

func (vs *vmseg_t) track(va int, pg *[512]int) bool {
	slot := (va >> PGSHIFT) - vs.startn
	old := vs.pages[slot]
	vs.pages[slot] = pg
	return old != nil
}

func (vs *vmseg_t) gettrack(va int) (*[512]int, bool) {
	if va < vs.start || va >= vs.end {
		panic("va outside seg")
	}
	slot := (va >> PGSHIFT) - vs.startn
	ret := vs.pages[slot]
	if ret == nil {
		return nil, false
	}
	return ret, true
}

type vmregion_t struct {
	head	*vmseg_t
}

func (vr *vmregion_t) pglen() int {
	ret := 0
	for h := vr.head; h != nil; h = h.next {
		ret += len(h.pages)
	}
	return ret
}

func (vr *vmregion_t) dump() {
	fmt.Printf("SEGS: ")
	for h := vr.head; h != nil; h = h.next {
		//fmt.Printf("(%x, %x) - %v\n", h.start, h.end, h.pages)
		fmt.Printf("(%x, %x)\n", h.start, h.end)
	}
}

func (vr *vmregion_t) clear() *vmseg_t {
	old := vr.head
	vr.head = nil
	return old
}

func (vr *vmregion_t) insert(ns *vmseg_t) *vmseg_t {
	if vr.head == nil {
		vr.head = ns
		return vr.head
	}
	var prev *vmseg_t
	var h *vmseg_t
	found := false
	for h = vr.head; h != nil; h = h.next {
		if h.start == ns.end {
			return vr._merge(prev, ns, h)
		} else if ns.start == h.end {
			return vr._merge(h, ns, h.next)
		} else if ns.end <= h.start {
			found = true
			break
		}
		prev = h
	}
	if !found {
		// end of list
		h = prev
		h.next = ns
		return ns
	}
	if prev != nil {
		prev.next = ns
		ns.next = h
	} else {
		// head of list
		ns.next = h
		vr.head = ns
	}
	return ns
}

func (vr *vmregion_t) remove(startva, len int) bool {
	start := rounddown(startva, PGSIZE)
	end := roundup(startva + len, PGSIZE)
	seg, prev, ok := vr.contain(start)
	if !ok || end > seg.end {
		return false
	}
	if start == seg.start && end == seg.end {
		if prev != nil {
			prev.next = seg.next
		} else {
			vr.head = seg.next
		}
		return true
	}
	vr._split(seg, start, end)
	return true
}

func (vr *vmregion_t) contain(va int) (*vmseg_t, *vmseg_t, bool) {
	var prev *vmseg_t
	var h *vmseg_t
	for h = vr.head; h != nil; h = h.next {
		if va >= h.start && va < h.end {
			return h, prev, true
		}
		prev = h
	}
	return nil, nil, false
}

func (vr *vmregion_t) _split(old *vmseg_t, rstart, rend int) {
	if old.start == rstart {
		l := (rend - old.start) / PGSIZE
		old.start = rend
		old.startn = old.start >> PGSHIFT
		old.pages = old.pages[l:]
	} else if old.end == rend {
		l := (rstart - old.start) / PGSIZE
		old.end = rstart
		old.pages = old.pages[:l]
	} else {
		ns := &vmseg_t{}
		ns.start = rend
		ns.startn = ns.start >> PGSHIFT
		ns.end = old.end
		ns.next = old.next
		nl := (rend - old.start) / PGSIZE
		ns.pages = old.pages[nl:]

		old.end = rstart
		old.next = ns
		ol := (rstart - old.start) / PGSIZE
		old.pages = old.pages[:ol]
	}
}

// left or right may be nil. left.next == right
func (vr *vmregion_t) _merge(left, mid, right *vmseg_t) *vmseg_t {
	if left != nil {
		vr._noisect(mid, left)
		if left.end == mid.start {
			left.end = mid.end
			left.pages = append(left.pages, mid.pages...)
			mid = left
		} else {
			left.next = mid
		}
	}
	if right != nil {
		vr._noisect(mid, right)
		if mid.end == right.start {
			mid.end = right.end
			mid.pages = append(mid.pages, right.pages...)
			mid.next = right.next
		} else {
			mid.next = right
		}
	}
	return mid
}

func (vr *vmregion_t) _noisect(ns, h *vmseg_t) {
	if (ns.start < ns.end && ns.end <= h.start && ns.start < ns.end) ||
	   (h.start < h.end && h.end <= ns.start && h.start < h.end) {
		return
	}
	vr.dump()
	fmt.Printf("trying to merge [%x, %x) and" +
	    " [%x, %x)\n", ns.start, ns.end, h.start, h.end)
	panic("segs intersect")
}

// finds an unreserved region of size sz, at startva or higher
func (vr *vmregion_t) empty(startva, sz int) int {
	if vr.head == nil {
		return startva
	}
	prev := vr.head
	for h := vr.head.next; h != nil; h = h.next {
		if startva > h.end {
			prev = h
			continue
		}
		if h.start - prev.end >= sz {
			return prev.end
		}
		prev = h
	}
	last := prev
	if startva > last.end {
		return startva
	}
	return last.end
}

// tracks memory pages
type pgtracker_t map[int]*[512]int

// tracks all pages allocated by go internally by the kernel such as pmap pages
// allocated by the kernel (not the bootloader/runtime)
var kplock = sync.Mutex{}
var kpages = pgtracker_t{}
var kpmpages = &pmtracker_t{}

func kpgadd(pg *[512]int) {
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
// on direct map). the caller should almost always call proc_t.page_insert()
// after pg_new().
func pg_new() (*[512]int, int) {
	pt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pt)))
	if ptn & PGOFFSET != 0 {
		panic("page not aligned")
	}
	// pmap walk for every allocation -- a cost of allocating pages with
	// the garbage collector.
	pte := pmap_lookup(kpmap(), int(uintptr(unsafe.Pointer(pt))))
	if pte == nil {
		panic("must be mapped")
	}
	physaddr := *pte & PTE_ADDR
	return pt, physaddr
}

var kpmapp      *[512]int

func kpmap() *[512]int {
	if kpmapp == nil {
		dur := caddr(VREC, VREC, VREC, VREC, 0)
		kpmapp = (*[512]int)(unsafe.Pointer(dur))
	}
	return kpmapp
}

var zeropg = new([512]int)
var p_zeropg int

// installs a direct map for 512G of physical memory via the recursive mapping
func dmap_init() {
	kpmpages.pminit()

	// the default cpu qemu uses for x86_64 supports 1GB pages, but
	// doesn't report it in cpuid 0x80000001... i wonder why.
	_, _, _, edx  := runtime.Cpuid(0x80000001, 0)
	gbpages := edx & (1 << 26) != 0

	dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)
	if *dpte & PTE_P != 0 {
		panic("dmap slot taken")
	}

	pdpt  := new([512]int)
	ptn := int(uintptr(unsafe.Pointer(pdpt)))
	if ptn & PGOFFSET != 0 {
		panic("page table not aligned")
	}
	p_pdpt, ok := runtime.Vtop(unsafe.Pointer(pdpt))
	if !ok {
		panic("must succeed")
	}
	kpgadd(pdpt)

	*dpte = int(p_pdpt) | PTE_P | PTE_W

	size := (1 << 30)

	// make qemu use 2MB pages, like my hardware, to help expose bugs that
	// the hardware may encounter.
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
		p_pd, ok := runtime.Vtop(unsafe.Pointer(pd))
		if !ok {
			panic("must succeed")
		}
		kpgadd(pd)
		for j := range pd {
			pd[j] = i*pdptsz + j*size | PTE_P | PTE_W | PTE_PS
		}
		pdpt[i] = int(p_pd) | PTE_P | PTE_W
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

	pte := pmap_lookup(kpmap(), int(uintptr(unsafe.Pointer(zeropg))))
	if pte == nil || *pte & PTE_P == 0 {
		panic("wut")
	}
	p_zeropg = *pte & PTE_ADDR
}

type kent_t struct {
	pml4slot	int
	entry		int
}

var kents = make([]kent_t, 0, 5)

var _vdirect	= VDIRECT << 39

// returns a page-aligned virtual address for the given physical address using
// the direct mapping
func dmap(p int) *[512]int {
	pa := uint(p)
	if pa >= 1 << 39 {
		panic("physical address too large")
	}

	v := _vdirect
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

func pmap_pgtbl(pml4 *[512]int, v int, create bool, perms int,
    pmt *pmtracker_t) (*[512]int, int) {
	vn := uint(uintptr(v))
	l4b  := (vn >> (12 + 9*3)) & 0x1ff
	pdpb := (vn >> (12 + 9*2)) & 0x1ff
	pdb  := (vn >> (12 + 9*1)) & 0x1ff
	ptb  := (vn >> (12 + 9*0)) & 0x1ff
	if l4b >= uint(VREC) && l4b <= uint(VEND) {
		panic(fmt.Sprintf("map in special slots: %#x", l4b))
	}

	if v & PGMASK == 0 && create {
		panic("mapping page 0");
	}

	instpg := func(pg *[512]int, idx uint) int {
		np, p_np := pg_new()
		pmt.pmadd(np)
		npte :=  p_np | perms | PTE_P
		pg[idx] = npte
		return npte
	}

	cpe := func(pe int) *[512]int {
		if pe & PTE_PS != 0 {
			panic("insert mapping into PS page")
		}
		phys := pe & PTE_ADDR
		return (*[512]int)(unsafe.Pointer(uintptr(_vdirect + phys)))
	}

	pe := pml4[l4b]
	if pe & PTE_P == 0 {
		if !create {
			return nil, 0
		}
		pe = instpg(pml4, l4b)
	}
	next := cpe(pe)
	pe = next[pdpb]
	if pe & PTE_P == 0 {
		if !create {
			return nil, 0
		}
		pe = instpg(next, pdpb)
	}
	next = cpe(pe)
	pe = next[pdb]
	if pe & PTE_P == 0 {
		if !create {
			return nil, 0
		}
		pe = instpg(next, pdb)
	}
	next = cpe(pe)
	return next, int(ptb)
}

// requires direct mapping
func _pmap_walk(pml4 *[512]int, v int, create bool, perms int,
    pmt *pmtracker_t) *int {
	pgtbl, slot := pmap_pgtbl(pml4, v, create, perms, pmt)
	if pgtbl == nil {
		return nil
	}
	return &pgtbl[slot]
}

func pmap_walk(pml4 *[512]int, v int, perms int, pmt *pmtracker_t) *int {
	return _pmap_walk(pml4, v, true, perms, pmt)
}

var _nilpmt	= &pmtracker_t{}

func pmap_lookup(pml4 *[512]int, v int) *int {
	return _pmap_walk(pml4, v, false, 0, _nilpmt)
}

func copy_pmap1(ptemod func(int) (int, int), dst *[512]int, src *[512]int,
    depth int, pmt *pmtracker_t) bool {

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
		if depth == 4 && i == VREC {
			dst[i] = 0
			continue
		}
		// reference kernel mappings pmap pages, PS pages, and create
		// nil recursive mapping
		if c & PTE_U == 0 || c & PTE_PS != 0 {
			dst[i] = c
			continue
		}
		// otherwise, recursively copy
		np, p_np := pg_new()
		pmt.pmadd(np)
		perms := c & PTE_FLAGS
		dst[i] = p_np | perms
		nsrc := pe2pg(c)
		if copy_pmap1(ptemod, np, nsrc, depth - 1, pmt) {
			doinval = true
		}
	}

	return doinval
}

// deep copies the pmap. ptemod is an optional function that takes the
// original PTE as an argument and returns two values: new PTE for the pmap
// being copied and PTE for the new pmap.
func copy_pmap(ptemod func(int) (int, int), pm *[512]int,
    pmt *pmtracker_t) (*[512]int, int, bool) {
	npm, p_npm := pg_new()
	pmt.pmadd(npm)
	doinval := copy_pmap1(ptemod, npm, pm, 4, pmt)
	npm[VREC] = p_npm | PTE_P | PTE_W
	return npm, p_npm, doinval
}

func fork_pmap1(dst *[512]int, src *[512]int, depth int,
    pmt *pmtracker_t) bool {

	doinval := false
	for i, c := range src {
		if c & PTE_P  == 0 {
			continue
		}
		if depth == 1 {
			// copy ptes
			if c & PTE_U != 0 {
				// XXX: sets readable pages writable!
				// copies dirty bits (and others) too...
				v := c &^ (PTE_W | PTE_WASCOW)
				v |= PTE_COW
				src[i] = v
				dst[i] = v
				if v != c {
					doinval = true
				}
			} else {
				dst[i] = c
			}
			continue
		}
		if depth == 4 && i == VREC {
			dst[i] = 0
			continue
		}
		// reference kernel mappings pmap pages, PS pages, and create
		// nil recursive mapping
		if c & PTE_U == 0 || c & PTE_PS != 0 {
			dst[i] = c
			continue
		}
		// otherwise, recursively copy
		np, p_np := pg_new()
		pmt.pmadd(np)
		perms := c & PTE_FLAGS
		dst[i] = p_np | perms
		nsrc := pe2pg(c)
		if fork_pmap1(np, nsrc, depth - 1, pmt) {
			doinval = true
		}
	}

	return doinval
}

func fork_pmap(pm *[512]int, pmt *pmtracker_t) (*[512]int, int, bool) {
	npm, p_npm := pg_new()
	pmt.pmadd(npm)
	doinval := fork_pmap1(npm, pm, 4, pmt)
	npm[VREC] = p_npm | PTE_P | PTE_W
	return npm, p_npm, doinval
}

// forks the ptes only for the virtual address range specified
func ptefork(cpmap, ppmap *[512]int, cpmt *pmtracker_t, start, end int) bool {
	doflush := false
	i := start
	for i < end {
		pptb, slot := pmap_pgtbl(ppmap, i, false, 0, nil)
		cptb, _ := pmap_pgtbl(cpmap, i, true, PTE_U | PTE_W, cpmt)
		ps := pptb[slot:]
		cs := cptb[slot:]
		left := (end - i) / PGSIZE
		if left < len(ps) {
			ps = ps[:left]
			cs = cs[:left]
		}
		for j, pte := range ps {
			// may be guard pages
			if pte & PTE_P == 0 {
				continue
			}
			phys := pte & PTE_ADDR
			flags := pte & PTE_FLAGS
			if flags & PTE_W != 0 {
				flags &^= (PTE_W | PTE_WASCOW)
				flags |= PTE_COW
				doflush = true
				ps[j] = phys | flags
			}
			cs[j] = phys | flags
		}
		i += len(ps)*PGSIZE

	}
	return doflush
}

func pmap_copy_par1(src *[512]int, dst *[512]int, depth int,
    mywg *sync.WaitGroup, pch chan *map[int]*[512]int,
    convch chan *map[int]bool) {
	panic("update")
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
					v := pte &^ (PTE_W | PTE_WASCOW)
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
		if depth == 3 && i == VREC {
			dst[i] = 0
			continue
		}
		if pte & PTE_U == 0 || pte & PTE_PS != 0 {
			dst[i] = pte
			continue
		}
		np, p_np := pg_new()
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
	panic("update")
	pch := make(chan *map[int]*[512]int, 512)
	convch := make(chan *map[int]bool, 512)
	done := make(chan bool)
	dst, p_dst := pg_new()

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
	panic("update")
	pmap_iter1(ptef, pm, 3, va, true)
}

// allocates a page tracked by kpages and maps it at va. only used during AP
// bootup.
func kmalloc(va uintptr, perms int) {
	kplock.Lock()
	defer kplock.Unlock()
	pg, p_pg := pg_new()
	kpgadd(pg)
	pte := pmap_walk(kpmap(), int(va), perms, kpmpages)
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("page already mapped %#x", va))
	}
	*pte = p_pg | PTE_P | perms
}

func is_mapped(pmap *[512]int, va int, size int) bool {
	p := rounddown(va, PGSIZE)
	end := roundup(va + size, PGSIZE)
	for ; p < end; p += PGSIZE {
		pte := pmap_lookup(pmap, p)
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

func assert_no_va_map(pmap *[512]int, va uintptr) {
	pte := pmap_lookup(pmap, int(va))
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("va %#x is mapped", va))
	}
}
