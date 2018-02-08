package common

import "sync"
import "sync/atomic"
import "unsafe"

// lowest userspace address
const USERMIN	int = VUSER << 39
const DMAPLEN int = 1 << 39
var _dmapinit bool

// can account for up to 16TB of mem
type Physpg_t struct {
	refcnt		int32
	// index into pgs of next page on free list
	nexti		uint32
}

var Physmem struct {
	pgs		[]Physpg_t
	startn		uint32
	// index into pgs of first free pg
	freei		uint32
	pmaps		uint32
	sync.Mutex
}

func _refpg_new() (*Pg_t, Pa_t, bool) {
	return _phys_new(&Physmem.freei)
}

func refcnt(p_pg Pa_t) int {
	ref, _ := _refaddr(p_pg)
	c := atomic.AddInt32(ref, 1)
	return int(c)
}

func refup(p_pg Pa_t) {
	ref, _ := _refaddr(p_pg)
	c := atomic.AddInt32(ref, 1)
	// XXXPANIC
	if c <= 0 {
		panic("wut")
	}
}

// returns true if p_pg should be added to the free list and the index of the
// page in the pgs array
func _refdec(p_pg Pa_t) (bool, uint32) {
	ref, idx := _refaddr(p_pg)
	c := atomic.AddInt32(ref, -1)
	// XXXPANIC
	if c < 0 {
		panic("wut")
	}
	return c == 0, idx
}

func _reffree(idx uint32) {
	Physmem.Lock()
	onext := Physmem.freei
	Physmem.pgs[idx].nexti = onext
	Physmem.freei = idx
	Physmem.Unlock()
}

func refdown(p_pg Pa_t) bool {
	// add to freelist?
	if add, idx := _refdec(p_pg); add {
		_reffree(idx)
		return true
	}
	return false
}


var zeropg *Pg_t

// refcnt of returned page is not incremented (it is usually incremented via
// common.Proc_t.page_insert). requires direct mapping.
func refpg_new() (*Pg_t, Pa_t, bool) {
	pg, p_pg, ok := _refpg_new()
	if !ok {
		return nil, 0, false
	}
	*pg = *zeropg
	return pg, p_pg, true
}

func refpg_new_nozero() (*Pg_t, Pa_t, bool) {
	pg, p_pg, ok := _refpg_new()
	if !ok {
		return nil, 0, false
	}
	return pg, p_pg, true
}

func pmap_new() (*pmap_t, Pa_t, bool) {
	a, b, ok := _phys_new(&Physmem.pmaps)
	if ok {
		return pg2pmap(a), b, true
	}
	a, b, ok = refpg_new()
	return pg2pmap(a), b, ok
}

func _phys_new(fl *uint32) (*Pg_t, Pa_t, bool) {
	if !_dmapinit {
		panic("dmap not initted")
	}

	var p_pg Pa_t
	var ok bool
	Physmem.Lock()
	ff := *fl
	if ff != ^uint32(0) {
		p_pg = Pa_t(ff + Physmem.startn) << PGSHIFT
		*fl = Physmem.pgs[ff].nexti
		ok = true
		if Physmem.pgs[ff].refcnt < 0 {
			panic("negative ref count")
		}
	}
	Physmem.Unlock()
	if ok {
		return dmap(p_pg), p_pg, true
	}
	return nil, 0, false
}

func _phys_put(fl *uint32, p_pg Pa_t) {
	if add, idx := _refdec(p_pg); add {
		Physmem.Lock()
		Physmem.pgs[idx].nexti = *fl
		*fl = idx
		Physmem.Unlock()
	}
}

// decrease ref count of pml4, freeing it if no CPUs have it loaded into cr3.
func dec_pmap(p_pmap Pa_t) {
	_phys_put(&Physmem.pmaps, p_pmap)
}

// returns a page-aligned virtual address for the given physical address using
// the direct mapping
func dmap(p Pa_t) *Pg_t {
	pa := uintptr(p)
	if pa >= 1 << 39 {
		panic("direct map not large enough")
	}

	v := _vdirect
	v += uintptr(Rounddown(int(pa), PGSIZE))
	return (*Pg_t)(unsafe.Pointer(v))
}

func dmap_v2p(v *Pg_t) Pa_t {
	va := (uintptr)(unsafe.Pointer(v))
	if va <= 1 << 39 {
		panic("address isn't in the direct map")
	}

	pa := va - _vdirect
	return Pa_t(pa)
}

// returns a byte aligned virtual address for the physical address as slice of
// uint8s
func dmap8(p Pa_t) []uint8 {
	pg := dmap(p)
	off := p & PGOFFSET
	bpg := pg2bytes(pg)
	return bpg[off:]
}

// l is length of mapping in bytes
func dmaplen(p Pa_t, l int) []uint8 {
	_dmap := (*[DMAPLEN]uint8)(unsafe.Pointer(_vdirect))
	return _dmap[p:p+Pa_t(l)]
}

// l is length of mapping in bytes. both p and l must be multiples of 4
func dmaplen32(p uintptr, l int) []uint32 {
	if p % 4 != 0 || l % 4 != 0 {
		panic("not 32bit aligned")
	}
	_dmap := (*[DMAPLEN/4]uint32)(unsafe.Pointer(_vdirect))
	p /= 4
	l /= 4
	return _dmap[p:p+uintptr(l)]
}


