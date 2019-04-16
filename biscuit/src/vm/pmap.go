package vm

import "unsafe"
import "fmt"
import "sync"
import "sync/atomic"
import "runtime"
import "math/bits"

import "defs"
import "mem"

func _instpg(pg *mem.Pmap_t, idx uint, perms mem.Pa_t) (mem.Pa_t, bool) {
	_, p_np, ok := mem.Physmem.Refpg_new()
	if !ok {
		return 0, false
	}
	mem.Physmem.Refup(p_np)
	npte := p_np | perms | PTE_P
	pg[idx] = npte
	return npte, true
}

// returns nil if either 1) create was false and the mapping doesn't exist or
// 2) create was true but we failed to allocate a page to create the mapping.
func pmap_pgtbl(pml4 *mem.Pmap_t, v int, create bool, perms mem.Pa_t) (*mem.Pmap_t, int) {
	vn := uint(uintptr(v))
	l4b := (vn >> (12 + 9*3)) & 0x1ff
	pdpb := (vn >> (12 + 9*2)) & 0x1ff
	pdb := (vn >> (12 + 9*1)) & 0x1ff
	ptb := (vn >> (12 + 9*0)) & 0x1ff
	if l4b >= uint(mem.VREC) && l4b <= uint(mem.VEND) {
		panic(fmt.Sprintf("map in special slots: %#x", l4b))
	}

	if v&IPGMASK == 0 && create {
		panic("mapping page 0")
	}

	cpe := func(pe mem.Pa_t) *mem.Pmap_t {
		if pe&PTE_PS != 0 {
			panic("insert mapping into PS page")
		}
		phys := uintptr(pe & PTE_ADDR)
		return (*mem.Pmap_t)(unsafe.Pointer(mem.Vdirect + phys))
	}

	var ok bool
	pe := pml4[l4b]
	if pe&PTE_P == 0 {
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
	if pe&PTE_P == 0 {
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
	if pe&PTE_P == 0 {
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
func _pmap_walk(pml4 *mem.Pmap_t, v int, create bool, perms mem.Pa_t) *mem.Pa_t {
	pgtbl, slot := pmap_pgtbl(pml4, v, create, perms)
	if pgtbl == nil {
		return nil
	}
	return &pgtbl[slot]
}

func pmap_walk(pml4 *mem.Pmap_t, v int, perms mem.Pa_t) (*mem.Pa_t, defs.Err_t) {
	ret := _pmap_walk(pml4, v, true, perms)
	if ret == nil {
		// create was set; failed to allocate a page
		return nil, -defs.ENOMEM
	}
	return ret, 0
}

func Pmap_lookup(pml4 *mem.Pmap_t, v int) *mem.Pa_t {
	return _pmap_walk(pml4, v, false, 0)
}

func pmfree(pml4 *mem.Pmap_t, start, end uintptr, fops mem.Unpin_i) {
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
			if p_pg&PTE_P != 0 {
				if p_pg&PTE_U == 0 {
					panic("kernel pages in vminfo?")
				}
				pa := p_pg & PTE_ADDR
				if fops != nil {
					fops.Unpin(pa)
				}
				mem.Physmem.Refdown(pa)
				tofree[idx] = 0
			}
		}
		i += uintptr(len(tofree)) << PGSHIFT
	}
}

// forks the ptes only for the virtual address range specified. returns true if
// the parent's TLB should be flushed because we added COW bits to PTEs and
// whether the fork failed due to allocation failure
func Ptefork(cpmap, ppmap *mem.Pmap_t, start, end int,
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
		cptb, _ := pmap_pgtbl(cpmap, i, true, PTE_U|PTE_W)
		if cptb == nil {
			// failed to allocate user page table
			return doflush, false
		}
		ps := pptb[slot:]
		cs := cptb[slot:]
		left := (end - i) / mem.PGSIZE
		if left < len(ps) {
			ps = ps[:left]
			cs = cs[:left]
		}
		for j, pte := range ps {
			// may be guard pages
			if pte&PTE_P == 0 {
				continue
			}
			phys := pte & PTE_ADDR
			flags := pte & PTE_FLAGS
			if flags&PTE_W != 0 && mkcow {
				flags &^= (PTE_W | PTE_WASCOW)
				flags |= PTE_COW
				doflush = true
				ps[j] = phys | flags
			}
			cs[j] = phys | flags
			// XXXPANIC
			if pte&PTE_U == 0 {
				panic("huh?")
			}
			mem.Physmem.Refup(phys)
		}
		i += len(ps) * mem.PGSIZE

	}
	return doflush, true
}

var Numtlbs int = 1

var _tlbslocks [runtime.MAXCPUS]struct {
	sync.Mutex
	_ [64-8]uint8
}

func tlb_shootdown(p_pmap mem.Pa_t, tlb_ref *uint64, va uintptr, pgcount int) {
	if Numtlbs == 1 {
		runtime.TLBflush()
		return
	}
	stalecpus := atomic.LoadUint64(tlb_ref)

	me := runtime.CPUHint()
	tlock := &_tlbslocks[me]
	tlock.Lock()
	defer tlock.Unlock()

	mytlbs := &runtime.Tlbshoots.States[me]
	mytlbs.Cpum = 0
	atomic.StoreUintptr(&mytlbs.P_pmap, uintptr(p_pmap))

	lapaddr := 0xfee00000
	lap := (*[mem.PGSIZE / 4]uint32)(unsafe.Pointer(uintptr(lapaddr)))
	ipilow := func(ds int, deliv int, vec int) uint32 {
		return uint32(ds<<18 | 1<<14 | deliv<<8 | vec)
	}

	icrw := func(hi uint32, low uint32) {
		icrh := 0x310 / 4
		icrl := 0x300 / 4
		// use sync to guarantee order
		fl := runtime.Pushcli()
		atomic.StoreUint32(&lap[icrh], hi)
		atomic.StoreUint32(&lap[icrl], low)
		ipisent := uint32(1 << 12)
		for atomic.LoadUint32(&lap[icrl])&ipisent != 0 {
		}
		runtime.Popcli(fl)
	}

	tlbshootvec := 70
	// this code broadcasts a TLB shootdown
	//allandself := 2
	//low := ipilow(allandself, 0, tlbshootvec)
	//icrw(0, low)

	// targeted TLB shootdowns
	low := ipilow(0, 0, tlbshootvec)
	setbits := bits.OnesCount64(stalecpus)
	did := 0
	for i := 0; did < setbits; i++ {
		if (1 << uint(i)) & stalecpus != 0 {
			apicid := _numtoapicid(i)
			icrw(apicid << 24, low)
			did++
		}
	}

	// if we make it here, the IPI must have been sent and thus we
	// shouldn't spin in this loop for very long. this loop does not
	// contain a stack check and thus cannot be preempted by the runtime.

	// due to a limitation of the current implementation, a CPU that
	// receives the TLB shootdown IPI may not know which pmap the IPI was
	// sent for. thus that CPU must acknowledge all outstanding TLB
	// shootdowns. for this reason, Cpum may have more bits set than
	// requested, so wait for at least those bits from the CPUs to which we
	// sent shootdowns.
	for atomic.LoadUint64(&mytlbs.Cpum) & stalecpus != stalecpus {
	}
	mytlbs.P_pmap = 0
}

var kplock = sync.Mutex{}

// allocates a page tracked by kpages and maps it at va. only used during AP
// bootup.
// XXX remove this crap
func Kmalloc(va uintptr, perms mem.Pa_t) {
	kplock.Lock()
	defer kplock.Unlock()
	_, p_pg, ok := mem.Physmem.Refpg_new()
	if !ok {
		panic("oom in init?")
	}
	mem.Physmem.Refup(p_pg)
	pte, err := pmap_walk(mem.Kpmap(), int(va), perms)
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

// don't forget: there are two places where pmaps/memory are free'd:
// Proc_t.terminate() and exec.
func Uvmfree_inner(pmg *mem.Pmap_t, p_pmap mem.Pa_t, vmr *Vmregion_t) {
	vmr.Iter(func(vmi *Vminfo_t) {
		start := uintptr(vmi.Pgn << PGSHIFT)
		end := start + uintptr(vmi.Pglen<<PGSHIFT)
		var unpin mem.Unpin_i
		if vmi.Mtype == VFILE {
			unpin = vmi.file.mfile.unpin
		}
		pmfree(pmg, start, end, unpin)
	})
}
