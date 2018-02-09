package common

import "sync"
import "sync/atomic"
import "unsafe"
import "runtime"
import "fmt"

// lowest userspace address
const USERMIN	int = VUSER << 39
const DMAPLEN int = 1 << 39
var dmapinit bool

type Page_i interface {
	Refpg_new() (*Pg_t, Pa_t, bool)
	Refcnt(Pa_t) int
	Dmap(Pa_t) *Pg_t
	Refup(Pa_t)
	Refdown(Pa_t) bool
}

// can account for up to 16TB of mem
type Physpg_t struct {
	refcnt		int32
	// index into pgs of next page on free list
	nexti		uint32
}

type Physmem_t struct {
	pgs		[]Physpg_t
	startn		uint32
	// index into pgs of first free pg
	freei		uint32
	pmaps		uint32
	sync.Mutex
}

var Physmem = &Physmem_t{}

func (phys *Physmem_t) _refpg_new() (*Pg_t, Pa_t, bool) {
	return phys._phys_new(&phys.freei)
}

func (phys *Physmem_t) Refcnt(p_pg Pa_t) int {
	ref, _ := _refaddr(p_pg)
	c := atomic.AddInt32(ref, 1)
	return int(c)
}

func (phys *Physmem_t) Refup(p_pg Pa_t) {
	ref, _ := _refaddr(p_pg)
	c := atomic.AddInt32(ref, 1)
	// XXXPANIC
	if c <= 0 {
		panic("wut")
	}
}

// returns true if p_pg should be added to the free list and the index of the
// page in the pgs array
func (phys *Physmem_t) _refdec(p_pg Pa_t) (bool, uint32) {
	ref, idx := _refaddr(p_pg)
	c := atomic.AddInt32(ref, -1)
	// XXXPANIC
	if c < 0 {
		panic("wut")
	}
	return c == 0, idx
}

func (phys *Physmem_t) _reffree(idx uint32) {
	phys.Lock()
	onext := phys.freei
	phys.pgs[idx].nexti = onext
	phys.freei = idx
	phys.Unlock()
}

func (phys *Physmem_t) Refdown(p_pg Pa_t) bool {
	// add to freelist?
	if add, idx := phys._refdec(p_pg); add {
		phys._reffree(idx)
		return true
	}
	return false
}


var Zeropg *Pg_t

// refcnt of returned page is not incremented (it is usually incremented via
// Proc_t.page_insert). requires direct mapping.
func (phys *Physmem_t) Refpg_new() (*Pg_t, Pa_t, bool) {
	pg, p_pg, ok := phys._refpg_new()
	if !ok {
		return nil, 0, false
	}
	*pg = *Zeropg
	return pg, p_pg, true
}

func (phys *Physmem_t) Refpg_new_nozero() (*Pg_t, Pa_t, bool) {
	pg, p_pg, ok := phys._refpg_new()
	if !ok {
		return nil, 0, false
	}
	return pg, p_pg, true
}

func (phys *Physmem_t) Pmap_new() (*Pmap_t, Pa_t, bool) {
	a, b, ok := phys._phys_new(&phys.pmaps)
	if ok {
		return pg2pmap(a), b, true
	}
	a, b, ok = phys.Refpg_new()
	return pg2pmap(a), b, ok
}

func (phys *Physmem_t) _phys_new(fl *uint32) (*Pg_t, Pa_t, bool) {
	if !dmapinit {
		panic("dmap not initted")
	}

	var p_pg Pa_t
	var ok bool
	phys.Lock()
	ff := *fl
	if ff != ^uint32(0) {
		p_pg = Pa_t(ff + phys.startn) << PGSHIFT
		*fl = phys.pgs[ff].nexti
		ok = true
		if phys.pgs[ff].refcnt < 0 {
			panic("negative ref count")
		}
	}
	phys.Unlock()
	if ok {
		return phys.Dmap(p_pg), p_pg, true
	}
	return nil, 0, false
}

func (phys *Physmem_t) _phys_put(fl *uint32, p_pg Pa_t) {
	if add, idx := phys._refdec(p_pg); add {
		phys.Lock()
		phys.pgs[idx].nexti = *fl
		*fl = idx
		phys.Unlock()
	}
}

// decrease ref count of pml4, freeing it if no CPUs have it loaded into cr3.
func (phys *Physmem_t) Dec_pmap(p_pmap Pa_t) {
	phys._phys_put(&phys.pmaps, p_pmap)
}

// returns a page-aligned virtual address for the given physical address using
// the direct mapping
func (phys *Physmem_t) Dmap(p Pa_t) *Pg_t {
	pa := uintptr(p)
	if pa >= 1 << 39 {
		panic("direct map not large enough")
	}

	v := _vdirect
	v += uintptr(Rounddown(int(pa), PGSIZE))
	return (*Pg_t)(unsafe.Pointer(v))
}

func Dmap_v2p(v *Pg_t) Pa_t {
	va := (uintptr)(unsafe.Pointer(v))
	if va <= 1 << 39 {
		panic("address isn't in the direct map")
	}

	pa := va - _vdirect
	return Pa_t(pa)
}

// returns a byte aligned virtual address for the physical address as slice of
// uint8s
func (phys *Physmem_t) Dmap8(p Pa_t) []uint8 {
	pg := phys.Dmap(p)
	off := p & PGOFFSET
	bpg := Pg2bytes(pg)
	return bpg[off:]
}

// l is length of mapping in bytes
func Dmaplen(p Pa_t, l int) []uint8 {
	_dmap := (*[DMAPLEN]uint8)(unsafe.Pointer(_vdirect))
	return _dmap[p:p+Pa_t(l)]
}

// l is length of mapping in bytes. both p and l must be multiples of 4
func Dmaplen32(p uintptr, l int) []uint32 {
	if p % 4 != 0 || l % 4 != 0 {
		panic("not 32bit aligned")
	}
	_dmap := (*[DMAPLEN/4]uint32)(unsafe.Pointer(_vdirect))
	p /= 4
	l /= 4
	return _dmap[p:p+uintptr(l)]
}

func Phys_init() (*Physmem_t) {
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
	first := Pa_t(runtime.Get_phys())
	fpgn := _pg2pgn(first)
	phys.startn = fpgn
	phys.freei = 0
	phys.pmaps = ^uint32(0)
	phys.pgs[0].refcnt = 0
	phys.pgs[0].nexti = ^uint32(0)
	last := phys.freei
	for i := 0; i < respgs - 1; i++ {
		p_pg := Pa_t(runtime.Get_phys())
		pgn := _pg2pgn(p_pg)
		idx := pgn - phys.startn
		// Get_phys() may skip regions.
		if int(idx) >= len(phys.pgs) {
			if respgs - i > int(float64(respgs)*0.01) {
				panic("got many bad pages")
			}
			break
		}
		phys.pgs[idx].refcnt = 0
		phys.pgs[last].nexti = idx;
		phys.pgs[idx].nexti =  ^uint32(0)
		last = idx
	}
	fmt.Printf("Reserved %v pages (%vMB)\n", respgs, respgs >> 8)
	return phys
}

func (phys *Physmem_t) Pgcount() (int, int) {
	phys.Lock()
	r1 := 0
	for i := phys.freei; i != ^uint32(0); i = phys.pgs[i].nexti {
		r1++
	}
	r2 := phys.pmapcount()
	phys.Unlock()
	return r1, r2
}

func (phys *Physmem_t) _pmcount(pml4 Pa_t, lev int) int {
	pg := pg2pmap(phys.Dmap(pml4))
	ret := 0
	for _, pte := range pg {
		if pte & PTE_U != 0 && pte & PTE_P != 0 {
			ret += 1 + phys._pmcount(Pa_t(pte & PTE_ADDR), lev - 1)
		}
	}
	return ret
}

func (phys *Physmem_t) pmapcount() int {
	c := 0
	for ni := phys.pmaps; ni != ^uint32(0); ni = phys.pgs[ni].nexti {
		v := phys._pmcount(Pa_t(ni+ phys.startn) << PGSHIFT, 4)
		c += v
	}
	return c
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

var Zerobpg *Bytepg_t
var Kents = make([]Kent_t, 0, 5)

// installs a direct map for 512G of physical memory via the recursive mapping
func Dmap_init() {
	//kpmpages.pminit()

	// the default cpu qemu uses for x86_64 supports 1GB pages, but
	// doesn't report it in cpuid 0x80000001... i wonder why.
	_, _, _, edx  := runtime.Cpuid(0x80000001, 0)
	gbpages := edx & (1 << 26) != 0

	_dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)
	dpte := (*Pa_t)(unsafe.Pointer(_dpte))
	if *dpte & PTE_P != 0 {
		panic("dmap slot taken")
	}

	pdpt  := new(Pmap_t)
	ptn := Pa_t(unsafe.Pointer(pdpt))
	if ptn & PGOFFSET != 0 {
		panic("page table not aligned")
	}
	p_pdpt, ok := runtime.Vtop(unsafe.Pointer(pdpt))
	if !ok {
		panic("must succeed")
	}
	kpgadd(pdpt)

	*dpte = Pa_t(p_pdpt) | PTE_P | PTE_W

	size := Pa_t(1 << 30)

	// make qemu use 2MB pages, like my hardware, to help expose bugs that
	// the hardware may encounter.
	if gbpages {
		fmt.Printf("dmap via 1GB pages\n")
		for i := range pdpt {
			pdpt[i] = Pa_t(i)*size | PTE_P | PTE_W | PTE_PS
		}
		return
	}
	fmt.Printf("1GB pages not supported\n")

	size = 1 << 21
	pdptsz := Pa_t(1 << 30)
	for i := range pdpt {
		pd := new(Pmap_t)
		p_pd, ok := runtime.Vtop(unsafe.Pointer(pd))
		if !ok {
			panic("must succeed")
		}
		kpgadd(pd)
		for j := range pd {
			pd[j] = Pa_t(i)*pdptsz +
			   Pa_t(j)*size | PTE_P | PTE_W | PTE_PS
		}
		pdpt[i] = Pa_t(p_pd) | PTE_P | PTE_W
	}

	// fill in kent, the list of kernel pml4 entries. make sure we will
	// panic if the runtime adds new mappings that we don't record here.
	runtime.Pml4freeze()
	for i, e := range Kpmap() {
		if e & PTE_U == 0 && e & PTE_P != 0 {
			ent := Kent_t{i, e}
			Kents = append(Kents, ent)
		}
	}
	dmapinit = true

	// Physmem.Refpg_new() uses the Zeropg to zero the page
	Zeropg, P_zeropg, ok = Physmem.Refpg_new()
	if !ok {
		panic("oom in dmap init")
	}
	for i := range Zeropg {
		Zeropg[i] = 0
	}
	Physmem.Refup(P_zeropg)
	Zerobpg = Pg2bytes(Zeropg)
}

var Kpmapp      *Pmap_t

func Kpmap() *Pmap_t {
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

func kpgadd(pg *Pmap_t) {
	va := uintptr(unsafe.Pointer(pg))
	pgn := int(va >> 12)
	if _, ok := kpages[pgn]; ok {
		panic("page already in kpages")
	}
	kpages[pgn] = pg
}





// tracks pages
type pgtracker_t map[int]*Pmap_t


// allocates a page tracked by kpages and maps it at va. only used during AP
// bootup.
// XXX remove this crap
func Kmalloc(va uintptr, perms Pa_t) {
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
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("page already mapped %#x", va))
	}
	*pte = p_pg | PTE_P | perms
}

func Assert_no_va_map(pmap *Pmap_t, va uintptr) {
	pte := Pmap_lookup(pmap, int(va))
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("va %#x is mapped", va))
	}
}
