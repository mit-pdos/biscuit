package common

import "fmt"
import "sync/atomic"

type Kent_t struct {
	Pml4slot int
	Entry    Pa_t
}

const VREC int = 0x42
const VDIRECT int = 0x44
const VEND int = 0x50
const VUSER int = 0x59

const PTE_P Pa_t = 1 << 0
const PTE_W Pa_t = 1 << 1
const PTE_U Pa_t = 1 << 2
const PTE_G Pa_t = 1 << 8
const PTE_PCD Pa_t = 1 << 4
const PTE_PS Pa_t = 1 << 7

// our flags; bits 9-11 are ignored for all page map entries in long mode
const PTE_COW Pa_t = 1 << 9
const PTE_WASCOW Pa_t = 1 << 10

const PGSIZEW uintptr = uintptr(PGSIZE)
const PGSHIFT uint = 12
const PGOFFSET Pa_t = 0xfff
const PGMASK Pa_t = ^(PGOFFSET)
const IPGMASK int = ^(int(PGOFFSET))
const PTE_ADDR Pa_t = PGMASK
const PTE_FLAGS Pa_t = (PTE_P | PTE_W | PTE_U | PTE_PCD | PTE_PS | PTE_COW |
	PTE_WASCOW)

var P_zeropg Pa_t

type Mmapinfo_t struct {
	Pg   *Pg_t
	Phys Pa_t
}

type mtype_t uint

// types of mappings
const (
	VANON mtype_t = 1 << iota
	// shared or private file
	VFILE mtype_t = 1 << iota
	// shared anonymous
	VSANON mtype_t = 1 << iota
)

type Mfile_t struct {
	mfops Fdops_i
	unpin Unpin_i
	// once mapcount is 0, close mfops
	mapcount int
}

type Vminfo_t struct {
	mtype mtype_t
	pgn   uintptr
	pglen int
	perms uint
	file  struct {
		foff   int
		mfile  *Mfile_t
		shared bool
	}
	pch []Pa_t
}

func (vmi *Vminfo_t) Pgn() uintptr {
	return vmi.pgn
}

type Vmregion_t struct {
	rb     Rbh_t
	_pglen int
	Novma  uint
	hole   struct {
		startn uintptr
		pglen  uintptr
	}
}

// if err == 0, the FS increased the reference count on the page.
func (vmi *Vminfo_t) Filepage(va uintptr) (*Pg_t, Pa_t, Err_t) {
	if vmi.mtype != VFILE {
		panic("must be file mapping")
	}
	voff := int(va - (vmi.pgn << PGSHIFT))
	foff := vmi.file.foff + voff
	mmapi, err := vmi.file.mfile.mfops.Mmapi(foff, 1, vmi.file.shared)
	if err != 0 {
		return nil, 0, err
	}
	return mmapi[0].Pg, mmapi[0].Phys, 0
}

func (vmi *Vminfo_t) ptefor(pmap *Pmap_t, va uintptr) (*Pa_t, bool) {
	if vmi.pch == nil {
		bva := int(vmi.pgn) << PGSHIFT
		ptbl, slot := pmap_pgtbl(pmap, bva, true, PTE_U|PTE_W)
		if ptbl == nil {
			return nil, false
		}
		vmi.pch = ptbl[slot:]
	}
	vn := (va >> PGSHIFT) - vmi.pgn
	if vn >= uintptr(vmi.pglen) {
		panic("uh oh")
	}
	if vn < uintptr(len(vmi.pch)) {
		return &vmi.pch[vn], true
	} else {
		ptbl, slot := pmap_pgtbl(pmap, int(va), true, PTE_U|PTE_W)
		if ptbl == nil {
			return nil, false
		}
		return &ptbl[slot], true
	}
}

func (m *Vmregion_t) _canmerge(a, b *Vminfo_t) bool {
	aend := a.pgn + uintptr(a.pglen)
	bend := b.pgn + uintptr(b.pglen)
	if a.pgn != bend && b.pgn != aend {
		return false
	}
	if a.mtype != b.mtype {
		return false
	}
	if a.perms != b.perms {
		return false
	}
	if a.mtype == VFILE {
		if a.file.shared != b.file.shared {
			return false
		}
		if a.file.mfile.mfops.Pathi() != b.file.mfile.mfops.Pathi() {
			return false
		}
		afend := a.file.foff + (a.pglen << PGSHIFT)
		bfend := b.file.foff + (b.pglen << PGSHIFT)
		if a.file.foff != bfend && b.file.foff != afend {
			return false
		}
	}
	return true
}

func (m *Vmregion_t) _merge(dst, src *Vminfo_t) {
	// XXXPANIC
	if !m._canmerge(dst, src) {
		panic("shat upon")
	}
	if src.pgn < dst.pgn {
		dst.pgn = src.pgn
		dst.pch = src.pch
	}
	if src.mtype == VFILE {
		if src.file.foff < dst.file.foff {
			dst.file.foff = src.file.foff
		}
		dst.file.mfile.mapcount += src.file.mfile.mapcount
	}
	dst.pglen += src.pglen
}

// looks for an adjacent mapping of the same type which can be merged into nn.
func (m *Vmregion_t) _trymerge(nn *Rbn_t, larger bool) {
	var n *Rbn_t
	if larger {
		n = nn.r
	} else {
		n = nn.l
	}
	for n != nil {
		if m._canmerge(&nn.vmi, &n.vmi) {
			m._merge(&nn.vmi, &n.vmi)
			m.rb.remove(n)
			return
		}
		if larger {
			n = n.l
		} else {
			n = n.r
		}
	}
}

// insert a new mapping, merging into the mapping both adjacent mappings, if
// they exist. there are three cases of pre-existing mappings when we do a new
// insert:
// 1) two adjacent mappings of the same type exist
// 2) one adjacent mapping of the same type exists
// 3) no adjacent mappings exist.
// my strategy is to check for an adjacent mapping while looking up the place
// to insert the new node. if we are in case 1 or 2, we must find one adjacent
// mapping during the traversal. case 3 is the only scenario where we must
// insert a new node.
func (m *Vmregion_t) insert(vmi *Vminfo_t) {
	// increase opencount for the file, if any
	if vmi.mtype == VFILE {
		// XXXPANIC
		if vmi.file.mfile.mapcount != vmi.pglen {
			panic("bad mapcount")
		}
		vmi.file.mfile.mfops.Reopen()
	}
	// adjust the cached hole
	if vmi.pgn == m.hole.startn {
		m.hole.startn += uintptr(vmi.pglen)
		m.hole.pglen -= uintptr(vmi.pglen)
	} else if vmi.pgn >= m.hole.startn &&
		vmi.pgn < m.hole.startn+m.hole.pglen {
		m.hole.pglen = vmi.pgn - m.hole.startn
	}
	m._pglen += vmi.pglen
	var par *Rbn_t
	for n := m.rb.root; n != nil; {
		// XXXPANIC
		if n.vmi.pgn == vmi.pgn {
			panic("addr exists")
		}
		// is this an adjacent, merge-able mapping?
		if m._canmerge(&n.vmi, vmi) {
			m._merge(&n.vmi, vmi)
			if n.vmi.pgn < vmi.pgn {
				// found the lower piece, check for higher
				m._trymerge(n, true)
			} else {
				// found the higher piece, check for the lower
				m._trymerge(n, false)
			}
			return
		}
		par = n
		if vmi.pgn > n.vmi.pgn {
			n = n.r
		} else {
			n = n.l
		}
	}
	// there are no mergable mappings, otherwise we would have encountered
	// one during traversal.
	m.Novma++
	nn := &Rbn_t{p: par, c: RED, vmi: *vmi}
	if par == nil {
		m.rb.root = nn
	} else {
		if par.vmi.pgn > vmi.pgn {
			par.l = nn
		} else {
			par.r = nn
		}
	}
	m.rb._balance(nn)
}

func (m *Vmregion_t) _clear(vmi *Vminfo_t, pglen int) {
	// decrement mapcounts, close file if necessary
	if vmi.mtype != VFILE {
		return
	}
	//oc := vmi.file.mfile.mapcount
	vmi.file.mfile.mapcount -= pglen
	// XXXPANIC
	if vmi.file.mfile.mapcount < 0 {
		//fmt.Printf("%v %v (%v)\n", oc, pglen, vmi.pglen)
		panic("negative ref count")
	}
	if vmi.file.mfile.mapcount == 0 {
		vmi.file.mfile.mfops.Close()
	}
}

func (m *Vmregion_t) Clear() {
	m.iter(func(vmi *Vminfo_t) {
		m._clear(vmi, vmi.pglen)
	})
}

func (m *Vmregion_t) Lookup(va uintptr) (*Vminfo_t, bool) {
	pgn := va >> PGSHIFT
	n := m.rb.lookup(pgn)
	if n == nil {
		return nil, false
	}
	return &n.vmi, true
}

func (m *Vmregion_t) _copy1(par, src *Rbn_t) *Rbn_t {
	if src == nil {
		return nil
	}
	ret := &Rbn_t{}
	*ret = *src
	ret.vmi.pch = nil
	// create per-process mfile objects and increase opencount for file
	// mappings
	if ret.vmi.mtype == VFILE {
		nmf := &Mfile_t{}
		*nmf = *src.vmi.file.mfile
		ret.vmi.file.mfile = nmf
		nmf.mfops.Reopen()
	}
	ret.p = par
	ret.l = m._copy1(ret, src.l)
	ret.r = m._copy1(ret, src.r)
	return ret
}

func (m *Vmregion_t) copy() Vmregion_t {
	var ret Vmregion_t
	ret._pglen, ret.Novma = m._pglen, m.Novma
	ret.rb.root = m._copy1(nil, m.rb.root)
	return ret
}

func (m *Vmregion_t) dump() {
	fmt.Printf("novma: %v\n", m.Novma)
	m.iter(func(vmi *Vminfo_t) {
		end := (vmi.pgn + uintptr(vmi.pglen)) << PGSHIFT
		var perms string
		switch vmi.mtype {
		case VANON:
			perms = "A-"
		case VFILE:
			if vmi.file.shared {
				perms = "SF-"
			} else {
				perms = "F-"
			}
		case VSANON:
			perms = "SA-"
		}
		if vmi.perms&uint(PTE_U) != 0 {
			perms += "R"
		}
		if vmi.perms&uint(PTE_W) != 0 {
			perms += ",W"
		}
		if vmi.perms&uint(PTE_U) != 0 {
			perms += ",U"
		}
		fmt.Printf("[%x - %x) (%v)\n", vmi.pgn<<PGSHIFT, end,
			perms)
	})
}

func (m *Vmregion_t) _iter1(n *Rbn_t, f func(*Vminfo_t)) {
	if n == nil {
		return
	}
	m._iter1(n.l, f)
	f(&n.vmi)
	m._iter1(n.r, f)
}

func (m *Vmregion_t) iter(f func(*Vminfo_t)) {
	m._iter1(m.rb.root, f)
}

func (m *Vmregion_t) Pglen() int {
	return m._pglen
}

func (m *Vmregion_t) _findhole(minpgn, minlen uintptr) (uintptr, uintptr) {
	var startn uintptr
	var pglen uintptr
	var done bool
	m.iter(func(vmi *Vminfo_t) {
		if done {
			return
		}
		if startn == 0 {
			t := vmi.pgn + uintptr(vmi.pglen)
			if t >= minpgn {
				startn = t
			}
		} else {
			if vmi.pgn-startn >= minlen {
				pglen = vmi.pgn - startn
				done = true
			} else {
				startn = vmi.pgn + uintptr(vmi.pglen)
			}
		}
	})
	if startn == 0 {
		startn = minpgn
	}
	if pglen == 0 {
		pglen = (0x100 << (39 - PGSHIFT)) - startn
	}
	return startn, pglen
}

func (m *Vmregion_t) empty(minva, len uintptr) (uintptr, uintptr) {
	minn := minva >> PGSHIFT
	pglen := uintptr(Roundup(int(len), PGSIZE) >> PGSHIFT)
	if minn >= m.hole.startn && pglen <= m.hole.pglen {
		return m.hole.startn << PGSHIFT, m.hole.pglen << PGSHIFT
	}
	nhs, nhl := m._findhole(minn, pglen)
	m.hole.startn, m.hole.pglen = nhs, nhl
	if !(minn+pglen <= m.hole.startn+m.hole.pglen) {
		panic("wut")
	}
	return m.hole.startn << PGSHIFT, m.hole.pglen << PGSHIFT
}

func (m *Vmregion_t) end() uintptr {
	last := uintptr(0)
	n := m.rb.root
	for n != nil {
		last = n.vmi.pgn + uintptr(n.vmi.pglen)
		n = n.r
	}
	return last << PGSHIFT
}

func (m *Vmregion_t) Remove(start, len int, novma uint) Err_t {
	pgn := uintptr(start) >> PGSHIFT
	pglen := Roundup(len, PGSIZE) >> PGSHIFT
	m._pglen -= pglen
	n := m.rb.lookup(pgn)
	if n == nil {
		//m.dump()
		panic("addr not mapped")
	}
	m._clear(&n.vmi, pglen)
	n.vmi.pch = nil
	// remove the whole node?
	if n.vmi.pgn == pgn && n.vmi.pglen == pglen {
		m.rb.remove(n)
		m.Novma--
		if m.Novma < 0 {
			panic("shaish!")
		}
		return 0
	}
	// if we are removing the beginning or end of the mapping, we can
	// simply adjust the node.
	pgend := n.vmi.pgn + uintptr(n.vmi.pglen)
	if pgn == n.vmi.pgn || pgn+uintptr(pglen) == pgend {
		if pgn == n.vmi.pgn {
			n.vmi.pgn += uintptr(pglen)
			n.vmi.pglen -= pglen
			if n.vmi.mtype == VFILE {
				n.vmi.file.foff += pglen << PGSHIFT
			}
		} else {
			n.vmi.pglen -= pglen
		}
		return 0
	}
	// too many vma objects
	if m.Novma >= novma {
		return -ENOMEM
	}
	// removing middle of a mapping; must add a new node
	avmi := &Vminfo_t{}
	*avmi = n.vmi

	n.vmi.pglen = int(pgn - n.vmi.pgn)
	avmi.pgn = pgn + uintptr(pglen)
	avmi.pglen = int(pgend - avmi.pgn)
	if avmi.mtype == VFILE {
		avmi.file.foff += int((avmi.pgn - n.vmi.pgn) << PGSHIFT)
	}
	m.rb._insert(avmi)
	m.Novma++
	return 0
}

// returns true if the fault was handled successfully
func Sys_pgfault(proc *Proc_t, vmi *Vminfo_t, faultaddr, ecode uintptr) bool {
	isguard := vmi.perms == 0
	iswrite := ecode&uintptr(PTE_W) != 0
	writeok := vmi.perms&uint(PTE_W) != 0
	if isguard || (iswrite && !writeok) {
		return false
	}
	// pmap is Lock'ed in Proc_t.pgfault...
	if ecode&uintptr(PTE_U) == 0 {
		// kernel page faults should be noticed and crashed upon in
		// runtime.trap(), but just in case
		panic("kernel page fault")
	}
	if vmi.mtype == VSANON {
		panic("shared anon pages should always be mapped")
	}

	pte, ok := vmi.ptefor(proc.Pmap, faultaddr)
	if !ok {
		return false
	}
	if (iswrite && *pte&PTE_WASCOW != 0) ||
		(!iswrite && *pte&PTE_P != 0) {
		// two threads simultaneously faulted on same page
		return true
	}

	var p_pg Pa_t
	isblockpage := false
	perms := PTE_U | PTE_P
	isempty := true

	// shared file mappings are handled the same way regardless of whether
	// the fault is read or write
	if vmi.mtype == VFILE && vmi.file.shared {
		var err Err_t
		_, p_pg, err = vmi.Filepage(faultaddr)
		if err != 0 {
			return false
		}
		isblockpage = true
		if vmi.perms&uint(PTE_W) != 0 {
			perms |= PTE_W
		}
	} else if iswrite {
		// XXXPANIC
		if *pte&PTE_W != 0 {
			panic("bad state")
		}
		var pgsrc *Pg_t
		var p_bpg Pa_t
		// the copy-on-write page may be specified in the pte or it may
		// not have been mapped at all yet.
		cow := *pte&PTE_COW != 0
		if cow {
			// if this anonymous COW page is mapped exactly once
			// (i.e.  only this mapping maps the page), we can
			// claim the page, skip the copy, and mark it writable.
			phys := *pte & PTE_ADDR
			ref, _ := _refaddr(phys)
			if vmi.mtype == VANON && atomic.LoadInt32(ref) == 1 &&
				phys != P_zeropg {
				tmp := *pte &^ PTE_COW
				tmp |= PTE_W | PTE_WASCOW
				*pte = tmp
				proc.Tlbshoot(faultaddr, 1)
				return true
			}
			pgsrc = Physmem.Dmap(phys)
			isempty = false
		} else {
			// XXXPANIC
			if *pte != 0 {
				panic("no")
			}
			switch vmi.mtype {
			case VANON:
				pgsrc = Zeropg
			case VFILE:
				var err Err_t
				pgsrc, p_bpg, err = vmi.Filepage(faultaddr)
				if err != 0 {
					return false
				}
				defer Physmem.Refdown(p_bpg)
			default:
				panic("wut")
			}
		}
		var pg *Pg_t
		var ok bool
		// don't zero new page
		pg, p_pg, ok = Physmem.Refpg_new_nozero()
		if !ok {
			return false
		}
		*pg = *pgsrc
		perms |= PTE_WASCOW
		perms |= PTE_W
	} else {
		if *pte != 0 {
			panic("must be 0")
		}
		switch vmi.mtype {
		case VANON:
			p_pg = P_zeropg
		case VFILE:
			var err Err_t
			_, p_pg, err = vmi.Filepage(faultaddr)
			if err != 0 {
				return false
			}
			isblockpage = true
		default:
			panic("wut")
		}
		if vmi.perms&uint(PTE_W) != 0 {
			perms |= PTE_COW
		}
	}

	var tshoot bool
	if isblockpage {
		tshoot, ok = proc.Blockpage_insert(int(faultaddr), p_pg, perms,
			isempty)
	} else {
		tshoot, ok = proc.Page_insert(int(faultaddr), p_pg, perms, isempty)
	}
	if !ok {
		Physmem.Refdown(p_pg)
		return false
	}
	if tshoot {
		proc.Tlbshoot(faultaddr, 1)
	}
	return true
}
