package vm

import "fmt"

import "defs"
import "fdops"
import "mem"
import "util"

//import "fd"

const PTE_P mem.Pa_t = 1 << 0
const PTE_W mem.Pa_t = 1 << 1
const PTE_U mem.Pa_t = 1 << 2
const PTE_A mem.Pa_t = 1 << 5
const PTE_D mem.Pa_t = 1 << 6
const PTE_G mem.Pa_t = 1 << 8
const PTE_PCD mem.Pa_t = 1 << 4
const PTE_PS mem.Pa_t = 1 << 7

// our flags; bits 9-11 are ignored for all page map entries in long mode
const PTE_COW mem.Pa_t = 1 << 9
const PTE_WASCOW mem.Pa_t = 1 << 10

const PGSIZEW uintptr = uintptr(mem.PGSIZE)
const PGSHIFT uint = 12
const PGOFFSET mem.Pa_t = 0xfff
const PGMASK mem.Pa_t = ^(PGOFFSET)
const IPGMASK int = ^(int(PGOFFSET))
const PTE_ADDR mem.Pa_t = PGMASK
const PTE_FLAGS mem.Pa_t = (PTE_P | PTE_W | PTE_U | PTE_PCD | PTE_PS | PTE_COW |
	PTE_WASCOW)

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
	mfops fdops.Fdops_i
	unpin mem.Unpin_i
	// once mapcount is 0, close mfops
	mapcount int
}

type Vminfo_t struct {
	Mtype mtype_t
	Pgn   uintptr
	Pglen int
	Perms uint
	file  struct {
		foff   int
		mfile  *Mfile_t
		shared bool
	}
	pch []mem.Pa_t
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
func (vmi *Vminfo_t) Filepage(va uintptr) (*mem.Pg_t, mem.Pa_t, defs.Err_t) {
	if vmi.Mtype != VFILE {
		panic("must be file mapping")
	}
	voff := int(va - (vmi.Pgn << PGSHIFT))
	foff := vmi.file.foff + voff
	mmapi, err := vmi.file.mfile.mfops.Mmapi(foff, 1, vmi.file.shared)
	if err != 0 {
		return nil, 0, err
	}
	return mmapi[0].Pg, mmapi[0].Phys, 0
}

func (vmi *Vminfo_t) Ptefor(pmap *mem.Pmap_t, va uintptr) (*mem.Pa_t, bool) {
	if vmi.pch == nil {
		bva := int(vmi.Pgn) << PGSHIFT
		ptbl, slot := pmap_pgtbl(pmap, bva, true, PTE_U|PTE_W)
		if ptbl == nil {
			return nil, false
		}
		vmi.pch = ptbl[slot:]
	}
	vn := (va >> PGSHIFT) - vmi.Pgn
	if vn >= uintptr(vmi.Pglen) {
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
	aend := a.Pgn + uintptr(a.Pglen)
	bend := b.Pgn + uintptr(b.Pglen)
	if a.Pgn != bend && b.Pgn != aend {
		return false
	}
	if a.Mtype != b.Mtype {
		return false
	}
	if a.Perms != b.Perms {
		return false
	}
	if a.Mtype == VFILE {
		if a.file.shared != b.file.shared {
			return false
		}
		if a.file.mfile.mfops.Pathi() != b.file.mfile.mfops.Pathi() {
			return false
		}
		afend := a.file.foff + (a.Pglen << PGSHIFT)
		bfend := b.file.foff + (b.Pglen << PGSHIFT)
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
	if src.Pgn < dst.Pgn {
		dst.Pgn = src.Pgn
		dst.pch = src.pch
	}
	if src.Mtype == VFILE {
		if src.file.foff < dst.file.foff {
			dst.file.foff = src.file.foff
		}
		dst.file.mfile.mapcount += src.file.mfile.mapcount
	}
	dst.Pglen += src.Pglen
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
	if vmi.Mtype == VFILE {
		// XXXPANIC
		if vmi.file.mfile.mapcount != vmi.Pglen {
			panic("bad mapcount")
		}
		vmi.file.mfile.mfops.Reopen()
	}
	// adjust the cached hole
	if vmi.Pgn == m.hole.startn {
		m.hole.startn += uintptr(vmi.Pglen)
		m.hole.pglen -= uintptr(vmi.Pglen)
	} else if vmi.Pgn >= m.hole.startn &&
		vmi.Pgn < m.hole.startn+m.hole.pglen {
		m.hole.pglen = vmi.Pgn - m.hole.startn
	}
	m._pglen += vmi.Pglen
	var par *Rbn_t
	for n := m.rb.root; n != nil; {
		// XXXPANIC
		if n.vmi.Pgn == vmi.Pgn {
			panic("addr exists")
		}
		// is this an adjacent, merge-able mapping?
		if m._canmerge(&n.vmi, vmi) {
			m._merge(&n.vmi, vmi)
			if n.vmi.Pgn < vmi.Pgn {
				// found the lower piece, check for higher
				m._trymerge(n, true)
			} else {
				// found the higher piece, check for the lower
				m._trymerge(n, false)
			}
			return
		}
		par = n
		if vmi.Pgn > n.vmi.Pgn {
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
		if par.vmi.Pgn > vmi.Pgn {
			par.l = nn
		} else {
			par.r = nn
		}
	}
	m.rb._balance(nn)
}

func (m *Vmregion_t) _clear(vmi *Vminfo_t, pglen int) {
	// decrement mapcounts, close file if necessary
	if vmi.Mtype != VFILE {
		return
	}
	//oc := vmi.file.mfile.mapcount
	vmi.file.mfile.mapcount -= pglen
	// XXXPANIC
	if vmi.file.mfile.mapcount < 0 {
		//fmt.Printf("%v %v (%v)\n", oc, pglen, vmi.Pglen)
		panic("negative ref count")
	}
	if vmi.file.mfile.mapcount == 0 {
		vmi.file.mfile.mfops.Close()
	}
}

func (m *Vmregion_t) Clear() {
	m.Iter(func(vmi *Vminfo_t) {
		m._clear(vmi, vmi.Pglen)
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
	if ret.vmi.Mtype == VFILE {
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

func (m *Vmregion_t) Copy() Vmregion_t {
	var ret Vmregion_t
	ret._pglen, ret.Novma = m._pglen, m.Novma
	ret.rb.root = m._copy1(nil, m.rb.root)
	return ret
}

func (m *Vmregion_t) dump() {
	fmt.Printf("novma: %v\n", m.Novma)
	m.Iter(func(vmi *Vminfo_t) {
		end := (vmi.Pgn + uintptr(vmi.Pglen)) << PGSHIFT
		var perms string
		switch vmi.Mtype {
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
		if vmi.Perms&uint(PTE_U) != 0 {
			perms += "R"
		}
		if vmi.Perms&uint(PTE_W) != 0 {
			perms += ",W"
		}
		if vmi.Perms&uint(PTE_U) != 0 {
			perms += ",U"
		}
		fmt.Printf("[%x - %x) (%v)\n", vmi.Pgn<<PGSHIFT, end,
			perms)
	})
}

func (m *Vmregion_t) _iterX(n *Rbn_t, f func(*Vminfo_t)) {
	if n == nil {
		return
	}
	m._iterX(n.l, f)
	m._iterX(n.r, f)
	f(&n.vmi)
	n.p, n.r, n.l = nil, nil, nil
	n.vmi = Vminfo_t{}
}

func (m *Vmregion_t) iterX(f func(*Vminfo_t)) {
	m._iterX(m.rb.root, f)
}

func (m *Vmregion_t) _iter1(n *Rbn_t, f func(*Vminfo_t)) {
	if n == nil {
		return
	}
	m._iter1(n.l, f)
	f(&n.vmi)
	m._iter1(n.r, f)
}

func (m *Vmregion_t) Iter(f func(*Vminfo_t)) {
	m._iter1(m.rb.root, f)
}

func (m *Vmregion_t) Pglen() int {
	return m._pglen
}

func (m *Vmregion_t) _findhole(minpgn, minlen uintptr) (uintptr, uintptr) {
	var startn uintptr
	var pglen uintptr
	var done bool
	m.Iter(func(vmi *Vminfo_t) {
		if done {
			return
		}
		if startn == 0 {
			t := vmi.Pgn + uintptr(vmi.Pglen)
			if t >= minpgn {
				startn = t
			}
		} else {
			if vmi.Pgn-startn >= minlen {
				pglen = vmi.Pgn - startn
				done = true
			} else {
				startn = vmi.Pgn + uintptr(vmi.Pglen)
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
	pglen := uintptr(util.Roundup(int(len), mem.PGSIZE) >> PGSHIFT)
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
		last = n.vmi.Pgn + uintptr(n.vmi.Pglen)
		n = n.r
	}
	return last << PGSHIFT
}

func (m *Vmregion_t) Remove(start, len int, novma uint) defs.Err_t {
	pgn := uintptr(start) >> PGSHIFT
	pglen := util.Roundup(len, mem.PGSIZE) >> PGSHIFT
	m._pglen -= pglen
	n := m.rb.lookup(pgn)
	if n == nil {
		//m.dump()
		panic("addr not mapped")
	}
	m._clear(&n.vmi, pglen)
	n.vmi.pch = nil
	// remove the whole node?
	if n.vmi.Pgn == pgn && n.vmi.Pglen == pglen {
		m.rb.remove(n)
		m.Novma--
		if m.Novma < 0 {
			panic("shaish!")
		}
		return 0
	}
	// if we are removing the beginning or end of the mapping, we can
	// simply adjust the node.
	pgend := n.vmi.Pgn + uintptr(n.vmi.Pglen)
	if pgn == n.vmi.Pgn || pgn+uintptr(pglen) == pgend {
		if pgn == n.vmi.Pgn {
			n.vmi.Pgn += uintptr(pglen)
			n.vmi.Pglen -= pglen
			if n.vmi.Mtype == VFILE {
				n.vmi.file.foff += pglen << PGSHIFT
			}
		} else {
			n.vmi.Pglen -= pglen
		}
		return 0
	}
	// too many vma objects
	if m.Novma >= novma {
		return -defs.ENOMEM
	}
	// removing middle of a mapping; must add a new node
	avmi := &Vminfo_t{}
	*avmi = n.vmi

	n.vmi.Pglen = int(pgn - n.vmi.Pgn)
	avmi.Pgn = pgn + uintptr(pglen)
	avmi.Pglen = int(pgend - avmi.Pgn)
	if avmi.Mtype == VFILE {
		avmi.file.foff += int((avmi.Pgn - n.vmi.Pgn) << PGSHIFT)
	}
	m.rb._insert(avmi)
	m.Novma++
	return 0
}
