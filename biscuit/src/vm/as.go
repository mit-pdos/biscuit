package vm

import "runtime"
import "sync"
import "sync/atomic"
import "time"

import "bounds"
import "defs"
import "fdops"
import "mem"
import "res"
import "ustr"
import "util"

type Vm_t struct {
	// lock for vmregion, pmpages, pmap, and p_pmap
	sync.Mutex

	Vmregion Vmregion_t

	// pmap pages
	Pmap   *mem.Pmap_t
	P_pmap mem.Pa_t

	pgfltaken bool
}

func (as *Vm_t) Lock_pmap() {
	// useful for finding deadlock bugs with one cpu
	//if p.pgfltaken {
	//	panic("double lock")
	//}
	as.Lock()
	as.pgfltaken = true
}

func (as *Vm_t) Unlock_pmap() {
	as.pgfltaken = false
	as.Unlock()
}

func (as *Vm_t) Lockassert_pmap() {
	if !as.pgfltaken {
		panic("pgfl lock must be held")
	}
}

func (as *Vm_t) Userdmap8_inner(va int, k2u bool) ([]uint8, defs.Err_t) {
	as.Lockassert_pmap()

	voff := va & int(PGOFFSET)
	uva := uintptr(va)
	vmi, ok := as.Vmregion.Lookup(uva)
	if !ok {
		return nil, -defs.EFAULT
	}
	pte, ok := vmi.Ptefor(as.Pmap, uva)
	if !ok {
		return nil, -defs.ENOMEM
	}
	ecode := uintptr(PTE_U)
	needfault := true
	isp := *pte&PTE_P != 0
	if k2u {
		ecode |= uintptr(PTE_W)
		// XXX how to distinguish between user asking kernel to write
		// to read-only page and kernel writing a page mapped read-only
		// to user? (exec args)

		//isw := *pte & PTE_W != 0
		//if isp && isw {
		iscow := *pte&PTE_COW != 0
		if isp && !iscow {
			needfault = false
		}
	} else {
		if isp {
			needfault = false
		}
	}

	if needfault {
		if err := Sys_pgfault(as, vmi, uva, ecode); err != 0 {
			return nil, err
		}
	}

	pg := mem.Physmem.Dmap(*pte & PTE_ADDR)
	bpg := mem.Pg2bytes(pg)
	return bpg[voff:], 0
}

// _userdmap8 and userdmap8r functions must only be used if concurrent
// modifications to the address space is impossible.
func (as *Vm_t) _userdmap8(va int, k2u bool) ([]uint8, defs.Err_t) {
	as.Lock_pmap()
	ret, err := as.Userdmap8_inner(va, k2u)
	as.Unlock_pmap()
	return ret, err
}

func (as *Vm_t) Userdmap8r(va int) ([]uint8, defs.Err_t) {
	return as._userdmap8(va, false)
}

func (as *Vm_t) usermapped(va, n int) bool {
	as.Lock_pmap()
	defer as.Unlock_pmap()

	_, ok := as.Vmregion.Lookup(uintptr(va))
	return ok
}

func (as *Vm_t) Userreadn(va, n int) (int, defs.Err_t) {
	as.Lock_pmap()
	a, b := as.userreadn_inner(va, n)
	as.Unlock_pmap()
	return a, b
}

func (as *Vm_t) userreadn_inner(va, n int) (int, defs.Err_t) {
	as.Lockassert_pmap()
	if n > 8 {
		panic("large n")
	}
	var ret int
	var src []uint8
	var err defs.Err_t
	for i := 0; i < n; i += len(src) {
		src, err = as.Userdmap8_inner(va+i, false)
		if err != 0 {
			return 0, err
		}
		l := n - i
		if len(src) < l {
			l = len(src)
		}
		v := util.Readn(src, l, 0)
		ret |= v << (8 * uint(i))
	}
	return ret, 0
}

func (as *Vm_t) Userwriten(va, n, val int) defs.Err_t {
	if n > 8 {
		panic("large n")
	}
	as.Lock_pmap()
	defer as.Unlock_pmap()
	var dst []uint8
	for i := 0; i < n; i += len(dst) {
		v := val >> (8 * uint(i))
		t, err := as.Userdmap8_inner(va+i, true)
		dst = t
		if err != 0 {
			return err
		}
		util.Writen(dst, n-i, 0, v)
	}
	return 0
}

// first ret value is the string from user space second is error
func (as *Vm_t) Userstr(uva int, lenmax int) (ustr.Ustr, defs.Err_t) {
	if lenmax < 0 {
		return nil, 0
	}
	as.Lock_pmap()
	//defer p.Vm.Unlock_pmap()
	i := 0
	s := ustr.MkUstr()
	for {
		str, err := as.Userdmap8_inner(uva+i, false)
		if err != 0 {
			as.Unlock_pmap()
			return s, err
		}
		for j, c := range str {
			if c == 0 {
				s = append(s, str[:j]...)
				// s = s + string(str[:j])
				as.Unlock_pmap()
				return s, 0
			}
		}
		s = append(s, str...)
		// s = s + string(str)
		i += len(str)
		if len(s) >= lenmax {
			as.Unlock_pmap()
			return nil, -defs.ENAMETOOLONG
		}
	}
}

func (as *Vm_t) Usertimespec(va int) (time.Duration, time.Time, defs.Err_t) {
	var zt time.Time
	secs, err := as.Userreadn(va, 8)
	if err != 0 {
		return 0, zt, err
	}
	nsecs, err := as.Userreadn(va+8, 8)
	if err != 0 {
		return 0, zt, err
	}
	if secs < 0 || nsecs < 0 {
		return 0, zt, -defs.EINVAL
	}
	tot := time.Duration(secs) * time.Second
	tot += time.Duration(nsecs) * time.Nanosecond
	t := time.Unix(int64(secs), int64(nsecs))
	return tot, t, 0
}

// copies src to the user virtual address uva. may copy part of src if uva +
// len(src) is not mapped
func (as *Vm_t) K2user(src []uint8, uva int) defs.Err_t {
	as.Lock_pmap()
	ret := as.K2user_inner(src, uva)
	as.Unlock_pmap()
	return ret
}

func (as *Vm_t) K2user_inner(src []uint8, uva int) defs.Err_t {
	as.Lockassert_pmap()
	cnt := 0
	l := len(src)
	for cnt != l {
		gimme := bounds.Bounds(bounds.B_ASPACE_T_K2USER_INNER)
		if !res.Resadd_noblock(gimme) {
			return -defs.ENOHEAP
		}
		dst, err := as.Userdmap8_inner(uva+cnt, true)
		if err != 0 {
			return err
		}
		ub := len(src)
		if ub > len(dst) {
			ub = len(dst)
		}
		copy(dst, src)
		src = src[ub:]
		cnt += ub
	}
	return 0
}

// copies len(dst) bytes from userspace address uva to dst
func (as *Vm_t) User2k(dst []uint8, uva int) defs.Err_t {
	as.Lock_pmap()
	ret := as.User2k_inner(dst, uva)
	as.Unlock_pmap()
	return ret
}

func (as *Vm_t) User2k_inner(dst []uint8, uva int) defs.Err_t {
	as.Lockassert_pmap()
	cnt := 0
	for len(dst) != 0 {
		gimme := bounds.Bounds(bounds.B_ASPACE_T_USER2K_INNER)
		if !res.Resadd_noblock(gimme) {
			return -defs.ENOHEAP
		}
		src, err := as.Userdmap8_inner(uva+cnt, false)
		if err != 0 {
			return err
		}
		did := copy(dst, src)
		dst = dst[did:]
		cnt += did
	}
	return 0
}

func (as *Vm_t) Unusedva_inner(startva, len int) int {
	as.Lockassert_pmap()
	if len < 0 || len > 1<<48 {
		panic("weird len")
	}
	startva = util.Rounddown(startva, mem.PGSIZE)
	if startva < mem.USERMIN {
		startva = mem.USERMIN
	}
	_ret, _l := as.Vmregion.empty(uintptr(startva), uintptr(len))
	ret := int(_ret)
	l := int(_l)
	if startva > ret && startva < ret+l {
		ret = startva
	}
	return ret
}

var _numtoapicid func(int) uint32

func Cpumap(f func(int) uint32) {
	_numtoapicid = f
}

func (as *Vm_t) Tlbshoot(startva uintptr, pgcount int) {
	if pgcount == 0 {
		return
	}
	as.Lockassert_pmap()
	if _numtoapicid == nil {
		panic("cpumap not initted")
	}

	// fast path: the pmap is loaded in exactly one CPU's cr3, and it
	// happens to be this CPU. we detect that one CPU has the pmap loaded
	// by a pmap ref count == 2 (1 for Proc_t ref, 1 for CPU).
	p_pmap := as.P_pmap
	refp, _ := mem.Physmem.Refaddr(p_pmap)
	// XXX XXX XXX use Tlbaddr to implement Condflush more simply
	if runtime.Condflush(refp, uintptr(p_pmap), startva, pgcount) {
		return
	}
	tlbp := mem.Physmem.Tlbaddr(p_pmap)
	// slow path, must send TLB shootdowns
	tlb_shootdown(as.P_pmap, tlbp, startva, pgcount)
}

// returns true if the fault was handled successfully
func Sys_pgfault(as *Vm_t, vmi *Vminfo_t, faultaddr, ecode uintptr) defs.Err_t {
	isguard := vmi.Perms == 0
	iswrite := ecode&uintptr(PTE_W) != 0
	writeok := vmi.Perms&uint(PTE_W) != 0
	if isguard || (iswrite && !writeok) {
		return -defs.EFAULT
	}
	// pmap is Lock'ed in Proc_t.pgfault...
	if ecode&uintptr(PTE_U) == 0 {
		// kernel page faults should be noticed and crashed upon in
		// runtime.trap(), but just in case
		panic("kernel page fault")
	}
	if vmi.Mtype == VSANON {
		panic("shared anon pages should always be mapped")
	}

	pte, ok := vmi.Ptefor(as.Pmap, faultaddr)
	if !ok {
		return -defs.ENOMEM
	}
	if (iswrite && *pte&PTE_WASCOW != 0) ||
		(!iswrite && *pte&PTE_P != 0) {
		// two threads simultaneously faulted on same page
		return 0
	}

	var p_pg mem.Pa_t
	isblockpage := false
	perms := PTE_U | PTE_P
	isempty := true

	// shared file mappings are handled the same way regardless of whether
	// the fault is read or write
	if vmi.Mtype == VFILE && vmi.file.shared {
		var err defs.Err_t
		_, p_pg, err = vmi.Filepage(faultaddr)
		if err != 0 {
			return err
		}
		isblockpage = true
		if vmi.Perms&uint(PTE_W) != 0 {
			perms |= PTE_W
		}
	} else if iswrite {
		// XXXPANIC
		if *pte&PTE_W != 0 {
			panic("bad state")
		}
		var pgsrc *mem.Pg_t
		var p_bpg mem.Pa_t
		// the copy-on-write page may be specified in the pte or it may
		// not have been mapped at all yet.
		cow := *pte&PTE_COW != 0
		if cow {
			// if this anonymous COW page is mapped exactly once
			// (i.e.  only this mapping maps the page), we can
			// claim the page, skip the copy, and mark it writable.
			phys := *pte & PTE_ADDR
			ref, _ := mem.Physmem.Refaddr(phys)
			if vmi.Mtype == VANON && atomic.LoadInt32(ref) == 1 &&
				phys != mem.P_zeropg {
				tmp := *pte &^ PTE_COW
				tmp |= PTE_W | PTE_WASCOW
				*pte = tmp
				as.Tlbshoot(faultaddr, 1)
				return 0
			}
			pgsrc = mem.Physmem.Dmap(phys)
			isempty = false
		} else {
			// XXXPANIC
			if *pte != 0 {
				panic("no")
			}
			switch vmi.Mtype {
			case VANON:
				pgsrc = mem.Zeropg
			case VFILE:
				var err defs.Err_t
				pgsrc, p_bpg, err = vmi.Filepage(faultaddr)
				if err != 0 {
					return err
				}
				defer mem.Physmem.Refdown(p_bpg)
			default:
				panic("wut")
			}
		}
		var pg *mem.Pg_t
		var ok bool
		// don't zero new page
		pg, p_pg, ok = mem.Physmem.Refpg_new_nozero()
		if !ok {
			return -defs.ENOMEM
		}
		*pg = *pgsrc
		perms |= PTE_WASCOW
		perms |= PTE_W
	} else {
		if *pte != 0 {
			panic("must be 0")
		}
		switch vmi.Mtype {
		case VANON:
			p_pg = mem.P_zeropg
		case VFILE:
			var err defs.Err_t
			_, p_pg, err = vmi.Filepage(faultaddr)
			if err != 0 {
				return err
			}
			isblockpage = true
		default:
			panic("wut")
		}
		if vmi.Perms&uint(PTE_W) != 0 {
			perms |= PTE_COW
		}
	}
	if perms & PTE_W != 0 {
		perms |= PTE_D
	}
	perms |= PTE_A

	var tshoot bool
	if isblockpage {
		tshoot, ok = as.Blockpage_insert(int(faultaddr), p_pg, perms, isempty, pte)
	} else {
		tshoot, ok = as.Page_insert(int(faultaddr), p_pg, perms, isempty, pte)
	}
	if !ok {
		mem.Physmem.Refdown(p_pg)
		return -defs.ENOMEM
	}
	if tshoot {
		as.Tlbshoot(faultaddr, 1)
	}
	return 0
}

// the first return value is true if a present mapping was modified (i.e. need
// to flush TLB). the second return value is false if the page insertion failed
// due to lack of user pages. p_pg's ref count is increased so the caller can
// simply Physmem.Refdown()
func (as *Vm_t) Page_insert(va int, p_pg mem.Pa_t, perms mem.Pa_t,
	vempty bool, pte *mem.Pa_t) (bool, bool) {
	return as._page_insert(va, p_pg, perms, vempty, true, pte)
}

// the first return value is true if a present mapping was modified (i.e. need
// to flush TLB). the second return value is false if the page insertion failed
// due to lack of user pages. p_pg's ref count is increased so the caller can
// simply Physmem.Refdown()
func (as *Vm_t) Blockpage_insert(va int, p_pg mem.Pa_t, perms mem.Pa_t,
	vempty bool, pte *mem.Pa_t) (bool, bool) {
	return as._page_insert(va, p_pg, perms, vempty, false, pte)
}

func (as *Vm_t) _page_insert(va int, p_pg mem.Pa_t, perms mem.Pa_t,
	vempty, refup bool, pte *mem.Pa_t) (bool, bool) {
	as.Lockassert_pmap()
	if refup {
		mem.Physmem.Refup(p_pg)
	}
	if pte == nil {
		var err defs.Err_t
		pte, err = pmap_walk(as.Pmap, va, PTE_U|PTE_W)
		if err != 0 {
			return false, false
		}
	}
	ninval := false
	var p_old mem.Pa_t
	if *pte&PTE_P != 0 {
		if vempty {
			panic("pte not empty")
		}
		if *pte&PTE_U == 0 {
			panic("replacing kernel page")
		}
		ninval = true
		p_old = mem.Pa_t(*pte & PTE_ADDR)
	}
	*pte = p_pg | perms | PTE_P
	if ninval {
		mem.Physmem.Refdown(p_old)
	}
	return ninval, true
}

func (as *Vm_t) Page_remove(va int) bool {
	as.Lockassert_pmap()
	remmed := false
	pte := Pmap_lookup(as.Pmap, va)
	if pte != nil && *pte&PTE_P != 0 {
		if *pte&PTE_U == 0 {
			panic("removing kernel page")
		}
		p_old := mem.Pa_t(*pte & PTE_ADDR)
		mem.Physmem.Refdown(p_old)
		*pte = 0
		remmed = true
	}
	return remmed
}

// returns true if the pagefault was handled successfully
func (as *Vm_t) Pgfault(tid defs.Tid_t, fa, ecode uintptr) defs.Err_t {
	as.Lock_pmap()
	vmi, ok := as.Vmregion.Lookup(fa)
	if !ok {
		as.Unlock_pmap()
		return -defs.EFAULT
	}
	ret := Sys_pgfault(as, vmi, fa, ecode)
	as.Unlock_pmap()
	return ret
}

func (as *Vm_t) Uvmfree() {
	Uvmfree_inner(as.Pmap, as.P_pmap, &as.Vmregion)
	// Dec_pmap could free the pmap itself. thus it must come after
	// Uvmfree.
	mem.Physmem.Dec_pmap(as.P_pmap)
	// close all open mmap'ed files
	as.Vmregion.Clear()
}

func (as *Vm_t) Vmadd_anon(start, len int, perms mem.Pa_t) {
	vmi := as._mkvmi(VANON, start, len, perms, 0, nil, nil)
	as.Vmregion.insert(vmi)
}

func (as *Vm_t) Vmadd_file(start, len int, perms mem.Pa_t, fops fdops.Fdops_i,
	foff int) {
	vmi := as._mkvmi(VFILE, start, len, perms, foff, fops, nil)
	as.Vmregion.insert(vmi)
}

func (as *Vm_t) Vmadd_shareanon(start, len int, perms mem.Pa_t) {
	vmi := as._mkvmi(VSANON, start, len, perms, 0, nil, nil)
	as.Vmregion.insert(vmi)
}

func (as *Vm_t) Vmadd_sharefile(start, len int, perms mem.Pa_t, fops fdops.Fdops_i,
	foff int, unpin mem.Unpin_i) {
	vmi := as._mkvmi(VFILE, start, len, perms, foff, fops, unpin)
	as.Vmregion.insert(vmi)
}

// does not increase opencount on fops (vmregion_t.insert does). perms should
// only use PTE_U/PTE_W; the page fault handler will install the correct COW
// flags. perms == 0 means that no mapping can go here (like for guard pages).
func (as *Vm_t) _mkvmi(mt mtype_t, start, len int, perms mem.Pa_t, foff int,
	fops fdops.Fdops_i, unpin mem.Unpin_i) *Vminfo_t {
	if len <= 0 {
		panic("bad vmi len")
	}
	if mem.Pa_t(start|len)&PGOFFSET != 0 {
		panic("start and len must be aligned")
	}
	// don't specify cow, present etc. -- page fault will handle all that
	pm := PTE_W | PTE_COW | PTE_WASCOW | PTE_PS | PTE_PCD | PTE_P | PTE_U
	if r := perms & pm; r != 0 && r != PTE_U && r != (PTE_W|PTE_U) {
		panic("bad perms")
	}
	ret := &Vminfo_t{}
	pgn := uintptr(start) >> PGSHIFT
	pglen := util.Roundup(len, mem.PGSIZE) >> PGSHIFT
	ret.Mtype = mt
	ret.Pgn = pgn
	ret.Pglen = pglen
	ret.Perms = uint(perms)
	if mt == VFILE {
		ret.file.foff = foff
		ret.file.mfile = &Mfile_t{}
		ret.file.mfile.mfops = fops
		ret.file.mfile.unpin = unpin
		ret.file.mfile.mapcount = pglen
		ret.file.shared = unpin != nil
	}
	return ret
}

func (as *Vm_t) Mkuserbuf(userva, len int) *Userbuf_t {
	ret := &Userbuf_t{}
	ret.ub_init(as, userva, len)
	return ret
}
