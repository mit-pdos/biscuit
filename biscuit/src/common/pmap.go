package common

import "unsafe"
import "fmt"
import "sync"
import "sync/atomic"
import "runtime"

const PGSIZE     int = 1 << 12

type Pa_t	uintptr
type Bytepg_t	[PGSIZE]uint8
type Pg_t	[512]int
type Pmap_t	[512]Pa_t

func Pg2bytes(pg *Pg_t) *Bytepg_t {
	return (*Bytepg_t)(unsafe.Pointer(pg))
}

func pg2pmap(pg *Pg_t) *Pmap_t {
	return (*Pmap_t)(unsafe.Pointer(pg))
}

func _pg2pgn(p_pg Pa_t) uint32 {
	return uint32(p_pg >> PGSHIFT)
}

func _refaddr(p_pg Pa_t) (*int32, uint32) {
	idx := _pg2pgn(p_pg) - Physmem.startn
	return &Physmem.pgs[idx].refcnt, idx
}

var _vdirect	= uintptr(VDIRECT << 39)

func _instpg(pg *Pmap_t, idx uint, perms Pa_t) (Pa_t, bool) {
	_, p_np, ok := Physmem.Refpg_new()
	if !ok {
		return 0, false
	}
	Physmem.Refup(p_np)
	npte :=  p_np | perms | PTE_P
	pg[idx] = npte
	return npte, true
}

// returns nil if either 1) create was false and the mapping doesn't exist or
// 2) create was true but we failed to allocate a page to create the mapping.
func pmap_pgtbl(pml4 *Pmap_t, v int, create bool, perms Pa_t) (*Pmap_t, int) {
	vn := uint(uintptr(v))
	l4b  := (vn >> (12 + 9*3)) & 0x1ff
	pdpb := (vn >> (12 + 9*2)) & 0x1ff
	pdb  := (vn >> (12 + 9*1)) & 0x1ff
	ptb  := (vn >> (12 + 9*0)) & 0x1ff
	if l4b >= uint(VREC) && l4b <= uint(VEND) {
		panic(fmt.Sprintf("map in special slots: %#x", l4b))
	}

	if v & IPGMASK == 0 && create {
		panic("mapping page 0");
	}

	cpe := func(pe Pa_t) *Pmap_t {
		if pe & PTE_PS != 0 {
			panic("insert mapping into PS page")
		}
		phys := uintptr(pe & PTE_ADDR)
		return (*Pmap_t)(unsafe.Pointer(_vdirect + phys))
	}

	var ok bool
	pe := pml4[l4b]
	if pe & PTE_P == 0 {
		if !create {
			return nil, 0
		}
		pe, ok = _instpg(pml4, l4b, perms)
		if !ok {
			return nil, 0
		}
	}
	next := cpe(pe)
	pe = next[pdpb]
	if pe & PTE_P == 0 {
		if !create {
			return nil, 0
		}
		pe, ok = _instpg(next, pdpb, perms)
		if !ok {
			return nil, 0
		}
	}
	next = cpe(pe)
	pe = next[pdb]
	if pe & PTE_P == 0 {
		if !create {
			return nil, 0
		}
		pe, ok = _instpg(next, pdb, perms)
		if !ok {
			return nil, 0
		}
	}
	next = cpe(pe)
	return next, int(ptb)
}

// requires direct mapping
func _pmap_walk(pml4 *Pmap_t, v int, create bool, perms Pa_t) *Pa_t {
	pgtbl, slot := pmap_pgtbl(pml4, v, create, perms)
	if pgtbl == nil {
		return nil
	}
	return &pgtbl[slot]
}

func pmap_walk(pml4 *Pmap_t, v int, perms Pa_t) (*Pa_t, Err_t) {
	ret := _pmap_walk(pml4, v, true, perms)
	if ret == nil {
		// create was set; failed to allocate a page
		return nil, -ENOMEM
	}
	return ret, 0
}

func Pmap_lookup(pml4 *Pmap_t, v int) *Pa_t {
	return _pmap_walk(pml4, v, false, 0)
}

func pmfree(pml4 *Pmap_t, start, end uintptr) {
	for i := start; i < end; {
		pg, slot := pmap_pgtbl(pml4, int(i), false, 0)
		if pg == nil {
			// this level is not mapped; skip to the next va that
			// may have a mapping at this level
			i += (1 << 21)
			i &^= (1 << 21) - 1
			continue
		}
		tofree := pg[slot:]
		left := (end - i) >> PGSHIFT
		if left < uintptr(len(tofree)) {
			tofree = tofree[:left]
		}
		for idx, p_pg := range tofree {
			if p_pg & PTE_P != 0 {
				if p_pg & PTE_U == 0 {
					panic("kernel pages in vminfo?")
				}
				Physmem.Refdown(p_pg & PTE_ADDR)
				tofree[idx] = 0
			}
		}
		i += uintptr(len(tofree)) << PGSHIFT
	}
}

// forks the ptes only for the virtual address range specified. returns true if
// the parent's TLB should be flushed because we added COW bits to PTEs and
// whether the fork failed due to allocation failure
func ptefork(cpmap, ppmap *Pmap_t, start, end int,
   shared bool) (bool, bool) {
	doflush := false
	mkcow := !shared
	i := start
	for i < end {
		pptb, slot := pmap_pgtbl(ppmap, i, false, 0)
		if pptb == nil {
			// skip to next page directory
			i += 1 << 21
			i &^= ((1 << 21) - 1)
			continue
		}
		cptb, _ := pmap_pgtbl(cpmap, i, true, PTE_U | PTE_W)
		if cptb == nil {
			// failed to allocate user page table
			return doflush, false
		}
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
			if flags & PTE_W != 0 && mkcow {
				flags &^= (PTE_W | PTE_WASCOW)
				flags |= PTE_COW
				doflush = true
				ps[j] = phys | flags
			}
			cs[j] = phys | flags
			// XXXPANIC
			if pte & PTE_U == 0 {
				panic("huh?")
			}
			Physmem.Refup(phys)
		}
		i += len(ps)*PGSIZE

	}
	return doflush, true
}

var Numcpus	int = 1
var _tlblock	= sync.Mutex{}

func tlb_shootdown(p_pmap, va uintptr, pgcount int) {
	if Numcpus == 1 {
		runtime.TLBflush()
		return
	}
	_tlblock.Lock()
	defer _tlblock.Unlock()

	runtime.Tlbshoot.Waitfor = int64(Numcpus)
	runtime.Tlbshoot.P_pmap = p_pmap

	lapaddr := 0xfee00000
	lap := (*[PGSIZE/4]uint32)(unsafe.Pointer(uintptr(lapaddr)))
	ipilow := func(ds int, deliv int, vec int) uint32 {
		return uint32(ds << 18 | 1 << 14 | deliv << 8 | vec)
	}

	icrw := func(hi uint32, low uint32) {
		icrh := 0x310/4
		icrl := 0x300/4
		// use sync to guarantee order
		atomic.StoreUint32(&lap[icrh], hi)
		atomic.StoreUint32(&lap[icrl], low)
		ipisent := uint32(1 << 12)
		for atomic.LoadUint32(&lap[icrl]) & ipisent != 0 {
		}
	}

	tlbshootvec := 70
	// broadcast shootdown
	allandself := 2
	low := ipilow(allandself, 0, tlbshootvec)
	icrw(0, low)

	// if we make it here, the IPI must have been sent and thus we
	// shouldn't spin in this loop for very long. this loop does not
	// contain a stack check and thus cannot be preempted by the runtime.
	for atomic.LoadInt64(&runtime.Tlbshoot.Waitfor) != 0 {
	}
}
