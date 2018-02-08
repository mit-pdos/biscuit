package main

import "fmt"
import "runtime"
import "sync"
import "unsafe"

type pa_t	uintptr
type pmap_t	[512]pa_t
type pg_t	[512]int
type bytepg_t	[PGSIZE]uint8

const PTE_P      pa_t = 1 << 0
const PTE_W      pa_t = 1 << 1
const PTE_U      pa_t = 1 << 2
const PTE_G      pa_t = 1 << 8
const PTE_PCD    pa_t = 1 << 4
const PTE_PS     pa_t = 1 << 7
// our flags; bits 9-11 are ignored for all page map entries in long mode
const PTE_COW    pa_t = 1 << 9
const PTE_WASCOW pa_t = 1 << 10
const PGSIZE     int = 1 << 12
const PGSIZEW    uintptr = uintptr(PGSIZE)
const PGSHIFT    uint = 12
const PGOFFSET   pa_t = 0xfff
const PGMASK     pa_t = ^(PGOFFSET)
const IPGMASK    int  = ^(int(PGOFFSET))
const PTE_ADDR   pa_t = PGMASK
const PTE_FLAGS  pa_t = (PTE_P | PTE_W | PTE_U | PTE_PCD | PTE_PS | PTE_COW |
    PTE_WASCOW)

const VREC      int = 0x42
const VDIRECT   int = 0x44
const VEND      int = 0x50
const VUSER     int = 0x59

func pg2bytes(pg *pg_t) *bytepg_t {
	return (*bytepg_t)(unsafe.Pointer(pg))
}

func pg2pmap(pg *pg_t) *pmap_t {
	return (*pmap_t)(unsafe.Pointer(pg))
}

/*
 * red-black tree based on niels provos' red-black tree macros
 */

type rbh_t struct {
	root	*rbn_t
}

type rbc_t int

const (
	RED	rbc_t = iota
	BLACK	rbc_t = iota
)

type rbn_t struct {
	p	*rbn_t
	r	*rbn_t
	l	*rbn_t
	c	rbc_t
	vmi	vminfo_t
}

type vminfo_t struct {
	mtype	mtype_t
	pgn	uintptr
	pglen	int
	perms	uint
	file struct {
		foff	int
		mfile	*mfile_t
		shared	bool
	}
	pch	[]pa_t
}

func (vmi *vminfo_t) filepage(va uintptr) (*pg_t, pa_t, err_t) {
	if vmi.mtype != VFILE {
		panic("must be file mapping")
	}
	voff := int(va - (vmi.pgn << PGSHIFT))
	foff := vmi.file.foff + voff
	mmapi, err := vmi.file.mfile.mfops.mmapi(foff, 1, true)
	if err != 0 {
		return nil, 0, err
	}
	return mmapi[0].pg, pa_t(mmapi[0].phys), 0
}

func (vmi *vminfo_t) ptefor(pmap *pmap_t, va uintptr) (*pa_t, bool) {
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

type mtype_t uint

// types of mappings
const (
	VANON	mtype_t = 1 << iota
	// shared or private file
	VFILE	mtype_t = 1 << iota
	// shared anonymous
	VSANON	mtype_t = 1 << iota
)

type mfile_t struct {
	mfops		fdops_i
	// once mapcount is 0, close mfops
	mapcount	int
}

func (h *rbh_t) _rol(nn *rbn_t) {
	tmp := nn.r
	nn.r = tmp.l
	if nn.r != nil {
		tmp.l.p = nn
	}
	tmp.p = nn.p
	if tmp.p != nil {
		if nn == nn.p.l {
			nn.p.l = tmp
		} else {
			nn.p.r = tmp
		}
	} else {
		h.root = tmp
	}
	tmp.l = nn
	nn.p = tmp
}

func (h *rbh_t) _ror(nn *rbn_t) {
	tmp := nn.l
	nn.l = tmp.r
	if nn.l != nil {
		tmp.r.p = nn
	}
	tmp.p = nn.p
	if tmp.p != nil {
		if nn == nn.p.l {
			nn.p.l = tmp
		} else {
			nn.p.r = tmp
		}
	} else {
		h.root = tmp
	}
	tmp.r = nn
	nn.p = tmp
}

func (h *rbh_t) _balance(nn *rbn_t) {
	for par := nn.p; par != nil && par.c == RED; par = nn.p {
		gp := par.p
		if par == gp.l {
			tmp := gp.r
			if tmp != nil && tmp.c == RED {
				tmp.c = BLACK
				par.c = BLACK
				gp.c = RED
				nn = gp
				continue
			}
			if par.r == nn {
				h._rol(par)
				tmp = par
				par = nn
				nn = tmp
			}
			par.c = BLACK
			gp.c = RED
			h._ror(gp)
		} else {
			tmp := gp.l
			if tmp != nil && tmp.c == RED {
				tmp.c = BLACK
				par.c = BLACK
				gp.c = RED
				nn = gp
				continue
			}
			if par.l == nn {
				h._ror(par)
				tmp = par
				par = nn
				nn = tmp
			}
			par.c = BLACK
			gp.c = RED
			h._rol(gp)
		}
	}
	h.root.c = BLACK
}

func (h *rbh_t) _insert(vmi *vminfo_t) *rbn_t {
	nn := &rbn_t{vmi: *vmi, c: RED}
	if h.root == nil {
		h.root = nn
		h._balance(nn)
		return nn
	}

	n := h.root
	for {
		if vmi.pgn > n.vmi.pgn {
			if n.r == nil {
				n.r = nn
				nn.p = n
				break
			}
			n = n.r
		} else if vmi.pgn < n.vmi.pgn {
			if n.l == nil {
				n.l = nn
				nn.p = n
				break
			}
			n = n.l
		} else if n.vmi.pgn == vmi.pgn {
			return n
		}
	}
	h._balance(nn)
	return nn
}

func (h *rbh_t) lookup(pgn uintptr) *rbn_t {
	n := h.root
	for n != nil {
		pgend := n.vmi.pgn + uintptr(n.vmi.pglen)
		if pgn >= n.vmi.pgn && pgn < pgend {
			break
		} else if n.vmi.pgn < pgn {
			n = n.r
		} else {
			n = n.l
		}
	}
	return n
}

func (h *rbh_t) _rembalance(par, nn *rbn_t) {
	for (nn == nil || nn.c == BLACK) && nn != h.root {
		if par.l == nn {
			tmp := par.r
			if tmp.c == RED {
				tmp.c = BLACK
				par.c = RED
				h._rol(par)
				tmp = par.r
			}
			if (tmp.l == nil || tmp.l.c == BLACK) &&
			    (tmp.r == nil || tmp.r.c == BLACK) {
				tmp.c = RED
				nn = par
				par = nn.p
			} else {
				if tmp.r == nil || tmp.r.c == BLACK {
					oleft := tmp.l
					if oleft != nil {
						oleft.c = BLACK
					}
					tmp.c = RED
					h._ror(tmp)
					tmp = par.r
				}
				tmp.c = par.c
				par.c = BLACK
				if tmp.r != nil {
					tmp.r.c = BLACK
				}
				h._rol(par)
				nn = h.root
				break
			}
		} else {
			tmp := par.l
			if tmp.c == RED {
				tmp.c = BLACK
				par.c = RED
				h._ror(par)
				tmp = par.l
			}
			if (tmp.l == nil || tmp.l.c == BLACK) &&
			    (tmp.r == nil || tmp.r.c == BLACK) {
				tmp.c = RED
				nn = par
				par = nn.p
			} else {
				if tmp.l == nil || tmp.l.c == BLACK {
					oright := tmp.r
					if oright != nil {
						oright.c = BLACK
					}
					tmp.c = RED
					h._rol(tmp)
					tmp = par.l
				}
				tmp.c = par.c
				par.c = BLACK
				if tmp.l != nil {
					tmp.l.c = BLACK
				}
				h._ror(par)
				nn = h.root
				break
			}
		}
	}
	if nn != nil {
		nn.c = BLACK
	}
}

func (h *rbh_t) remove(nn *rbn_t) *rbn_t {
	old := nn
	fast := true
	var child *rbn_t
	var par *rbn_t
	var col rbc_t
	if nn.l == nil {
		child = nn.r
	} else if nn.r == nil  {
		child = nn.l
	} else {
		nn = nn.r
		left := nn.l
		for left != nil {
			nn = left
			left = nn.l
		}
		child = nn.r
		par = nn.p
		col = nn.c
		if child != nil {
			child.p = par
		}
		if par != nil {
			if par.l == nn {
				par.l = child
			} else {
				par.r = child
			}
		} else {
			h.root = child
		}
		if nn.p == old {
			par = nn
		}
		nn.p = old.p
		nn.l = old.l
		nn.r = old.r
		nn.c = old.c
		if old.p != nil {
			if old.p.l == old {
				old.p.l = nn
			} else {
				old.p.r = nn
			}
		} else {
			h.root = nn
		}
		old.l.p = nn
		if old.r != nil {
			old.r.p = nn
		}
		fast = false
	}
	if fast {
		par = nn.p
		col = nn.c
		if child != nil {
			child.p = par
		}
		if par != nil {
			if par.l == nn {
				par.l = child
			} else {
				par.r = child
			}
		} else {
			h.root = child
		}
	}
	if col == BLACK {
		h._rembalance(par, child)
	}
	return old
}

type vmregion_t struct {
	rb	rbh_t
	_pglen	int
	novma	uint
	hole struct {
		startn	uintptr
		pglen	uintptr
	}
}

func (m *vmregion_t) _canmerge(a, b *vminfo_t) bool {
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
		if a.file.mfile.mfops.pathi() != b.file.mfile.mfops.pathi() {
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

func (m *vmregion_t) _merge(dst, src *vminfo_t) {
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
func (m *vmregion_t) _trymerge(nn *rbn_t, larger bool) {
	var n *rbn_t
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
func (m *vmregion_t) insert(vmi *vminfo_t) {
	// increase opencount for the file, if any
	if vmi.mtype == VFILE {
		// XXXPANIC
		if vmi.file.mfile.mapcount != vmi.pglen {
			panic("bad mapcount")
		}
		vmi.file.mfile.mfops.reopen()
	}
	// adjust the cached hole
	if vmi.pgn == m.hole.startn {
		m.hole.startn += uintptr(vmi.pglen)
		m.hole.pglen -= uintptr(vmi.pglen)
	} else if vmi.pgn >= m.hole.startn &&
	    vmi.pgn < m.hole.startn + m.hole.pglen {
		m.hole.pglen = vmi.pgn - m.hole.startn
	}
	m._pglen += vmi.pglen
	var par *rbn_t
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
	m.novma++
	nn := &rbn_t{p: par, c: RED, vmi: *vmi}
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

func (m *vmregion_t) _clear(vmi *vminfo_t, pglen int) {
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
		vmi.file.mfile.mfops.close()
	}
}

func (m *vmregion_t) clear() {
	m.iter(func (vmi *vminfo_t) {
		m._clear(vmi, vmi.pglen)
	})
}

func (m *vmregion_t) lookup(va uintptr) (*vminfo_t, bool) {
	pgn := va >> PGSHIFT
	n := m.rb.lookup(pgn)
	if n == nil {
		return nil, false
	}
	return &n.vmi, true
}

func (m *vmregion_t) _copy1(par, src *rbn_t) *rbn_t {
	if src == nil {
		return nil
	}
	ret := &rbn_t{}
	*ret = *src
	ret.vmi.pch = nil
	// create per-process mfile objects and increase opencount for file
	// mappings
	if ret.vmi.mtype == VFILE {
		nmf := &mfile_t{}
		*nmf = *src.vmi.file.mfile
		ret.vmi.file.mfile = nmf
		nmf.mfops.reopen()
	}
	ret.p = par
	ret.l = m._copy1(ret, src.l)
	ret.r = m._copy1(ret, src.r)
	return ret
}

func (m *vmregion_t) copy() vmregion_t {
	var ret vmregion_t
	ret._pglen, ret.novma = m._pglen, m.novma
	ret.rb.root = m._copy1(nil, m.rb.root)
	return ret
}

func (m *vmregion_t) dump() {
	fmt.Printf("novma: %v\n", m.novma)
	m.iter(func(vmi *vminfo_t) {
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
		if vmi.perms & uint(PTE_U) != 0 {
			perms += "R"
		}
		if vmi.perms & uint(PTE_W) != 0 {
			perms += ",W"
		}
		if vmi.perms & uint(PTE_U) != 0 {
			perms += ",U"
		}
		fmt.Printf("[%x - %x) (%v)\n", vmi.pgn << PGSHIFT, end,
		    perms)
	})
}

func (m *vmregion_t) _iter1(n *rbn_t, f func(*vminfo_t)) {
	if n == nil {
		return
	}
	m._iter1(n.l, f)
	f(&n.vmi)
	m._iter1(n.r, f)
}

func (m *vmregion_t) iter(f func(*vminfo_t)) {
	m._iter1(m.rb.root, f)
}

func (m *vmregion_t) pglen() int {
	return m._pglen
}

func (m *vmregion_t) _findhole(minpgn, minlen uintptr) (uintptr, uintptr) {
	var startn uintptr
	var pglen uintptr
	var done bool
	m.iter(func(vmi *vminfo_t) {
		if done {
			return
		}
		if startn == 0 {
			t := vmi.pgn + uintptr(vmi.pglen)
			if t >= minpgn {
				startn = t
			}
		} else {
			if vmi.pgn - startn >= minlen {
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

func (m *vmregion_t) empty(minva, len uintptr) (uintptr, uintptr) {
	minn := minva >> PGSHIFT
	pglen := uintptr(roundup(int(len), PGSIZE) >> PGSHIFT)
	if minn >= m.hole.startn && pglen <= m.hole.pglen {
		return m.hole.startn << PGSHIFT, m.hole.pglen << PGSHIFT
	}
	nhs, nhl := m._findhole(minn, pglen)
	m.hole.startn, m.hole.pglen = nhs, nhl
	if !(minn + pglen <= m.hole.startn + m.hole.pglen) {
		panic("wut")
	}
	return m.hole.startn << PGSHIFT, m.hole.pglen << PGSHIFT
}

func (m *vmregion_t) end() uintptr {
	last := uintptr(0)
	n := m.rb.root
	for n != nil {
		last = n.vmi.pgn + uintptr(n.vmi.pglen)
		n = n.r
	}
	return last << PGSHIFT
}

func (m *vmregion_t) remove(start, len int, novma uint) err_t {
	pgn := uintptr(start) >> PGSHIFT
	pglen := roundup(len, PGSIZE) >> PGSHIFT
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
		m.novma--
		if m.novma < 0 {
			panic("shaish!")
		}
		return 0
	}
	// if we are removing the beginning or end of the mapping, we can
	// simply adjust the node.
	pgend := n.vmi.pgn + uintptr(n.vmi.pglen)
	if pgn == n.vmi.pgn || pgn + uintptr(pglen) == pgend {
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
	if m.novma >= novma {
		return -ENOMEM
	}
	// removing middle of a mapping; must add a new node
	avmi := &vminfo_t{}
	*avmi = n.vmi

	n.vmi.pglen = int(pgn - n.vmi.pgn)
	avmi.pgn = pgn + uintptr(pglen)
	avmi.pglen = int(pgend - avmi.pgn)
	if avmi.mtype == VFILE {
		avmi.file.foff += int((avmi.pgn - n.vmi.pgn) << PGSHIFT)
	}
	m.rb._insert(avmi)
	m.novma++
	return 0
}

// tracks pages
type pgtracker_t map[int]*pmap_t

// tracks all pages allocated by go internally by the kernel such as pmap pages
// allocated by the kernel (not the bootloader/runtime)
var kplock = sync.Mutex{}
var kpages = pgtracker_t{}

func kpgadd(pg *pmap_t) {
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

var kpmapp      *pmap_t

func kpmap() *pmap_t {
	if kpmapp == nil {
		dur := caddr(VREC, VREC, VREC, VREC, 0)
		kpmapp = (*pmap_t)(unsafe.Pointer(dur))
	}
	return kpmapp
}

var zerobpg *bytepg_t
var p_zeropg pa_t
var _dmapinit bool
const DMAPLEN int = 1 << 39

// installs a direct map for 512G of physical memory via the recursive mapping
func dmap_init() {
	//kpmpages.pminit()

	// the default cpu qemu uses for x86_64 supports 1GB pages, but
	// doesn't report it in cpuid 0x80000001... i wonder why.
	_, _, _, edx  := runtime.Cpuid(0x80000001, 0)
	gbpages := edx & (1 << 26) != 0

	_dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)
	dpte := (*pa_t)(unsafe.Pointer(_dpte))
	if *dpte & PTE_P != 0 {
		panic("dmap slot taken")
	}

	pdpt  := new(pmap_t)
	ptn := pa_t(unsafe.Pointer(pdpt))
	if ptn & PGOFFSET != 0 {
		panic("page table not aligned")
	}
	p_pdpt, ok := runtime.Vtop(unsafe.Pointer(pdpt))
	if !ok {
		panic("must succeed")
	}
	kpgadd(pdpt)

	*dpte = pa_t(p_pdpt) | PTE_P | PTE_W

	size := pa_t(1 << 30)

	// make qemu use 2MB pages, like my hardware, to help expose bugs that
	// the hardware may encounter.
	if gbpages {
		fmt.Printf("dmap via 1GB pages\n")
		for i := range pdpt {
			pdpt[i] = pa_t(i)*size | PTE_P | PTE_W | PTE_PS
		}
		return
	}
	fmt.Printf("1GB pages not supported\n")

	size = 1 << 21
	pdptsz := pa_t(1 << 30)
	for i := range pdpt {
		pd := new(pmap_t)
		p_pd, ok := runtime.Vtop(unsafe.Pointer(pd))
		if !ok {
			panic("must succeed")
		}
		kpgadd(pd)
		for j := range pd {
			pd[j] = pa_t(i)*pdptsz +
			   pa_t(j)*size | PTE_P | PTE_W | PTE_PS
		}
		pdpt[i] = pa_t(p_pd) | PTE_P | PTE_W
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
	_dmapinit = true

	// refpg_new() uses the zeropg to zero the page
	zeropg, p_zeropg, ok = _refpg_new()
	if !ok {
		panic("oom in dmap init")
	}
	for i := range zeropg {
		zeropg[i] = 0
	}
	refup(p_zeropg)
	zerobpg = pg2bytes(zeropg)
}

type kent_t struct {
	pml4slot	int
	entry		pa_t
}

var kents = make([]kent_t, 0, 5)

var _vdirect	= uintptr(VDIRECT << 39)

// returns a page-aligned virtual address for the given physical address using
// the direct mapping
func dmap(p pa_t) *pg_t {
	pa := uintptr(p)
	if pa >= 1 << 39 {
		panic("direct map not large enough")
	}

	v := _vdirect
	v += uintptr(rounddown(int(pa), PGSIZE))
	return (*pg_t)(unsafe.Pointer(v))
}

func dmap_v2p(v *pg_t) pa_t {
	va := (uintptr)(unsafe.Pointer(v))
	if va <= 1 << 39 {
		panic("address isn't in the direct map")
	}

	pa := va - _vdirect
	return pa_t(pa)
}

// returns a byte aligned virtual address for the physical address as slice of
// uint8s
func dmap8(p pa_t) []uint8 {
	pg := dmap(p)
	off := p & PGOFFSET
	bpg := pg2bytes(pg)
	return bpg[off:]
}

// l is length of mapping in bytes
func dmaplen(p pa_t, l int) []uint8 {
	_dmap := (*[DMAPLEN]uint8)(unsafe.Pointer(_vdirect))
	return _dmap[p:p+pa_t(l)]
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

func _instpg(pg *pmap_t, idx uint, perms pa_t) (pa_t, bool) {
	_, p_np, ok := refpg_new()
	if !ok {
		return 0, false
	}
	refup(p_np)
	npte :=  p_np | perms | PTE_P
	pg[idx] = npte
	return npte, true
}

// returns nil if either 1) create was false and the mapping doesn't exist or
// 2) create was true but we failed to allocate a page to create the mapping.
func pmap_pgtbl(pml4 *pmap_t, v int, create bool, perms pa_t) (*pmap_t, int) {
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

	cpe := func(pe pa_t) *pmap_t {
		if pe & PTE_PS != 0 {
			panic("insert mapping into PS page")
		}
		phys := uintptr(pe & PTE_ADDR)
		return (*pmap_t)(unsafe.Pointer(_vdirect + phys))
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
func _pmap_walk(pml4 *pmap_t, v int, create bool, perms pa_t) *pa_t {
	pgtbl, slot := pmap_pgtbl(pml4, v, create, perms)
	if pgtbl == nil {
		return nil
	}
	return &pgtbl[slot]
}

func pmap_walk(pml4 *pmap_t, v int, perms pa_t) (*pa_t, err_t) {
	ret := _pmap_walk(pml4, v, true, perms)
	if ret == nil {
		// create was set; failed to allocate a page
		return nil, -ENOMEM
	}
	return ret, 0
}

func pmap_lookup(pml4 *pmap_t, v int) *pa_t {
	return _pmap_walk(pml4, v, false, 0)
}

// forks the ptes only for the virtual address range specified. returns true if
// the parent's TLB should be flushed because we added COW bits to PTEs and
// whether the fork failed due to allocation failure
func ptefork(cpmap, ppmap *pmap_t, start, end int,
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
			refup(phys)
		}
		i += len(ps)*PGSIZE

	}
	return doflush, true
}

// allocates a page tracked by kpages and maps it at va. only used during AP
// bootup.
// XXX remove this crap
func kmalloc(va uintptr, perms pa_t) {
	kplock.Lock()
	defer kplock.Unlock()
	_, p_pg, ok := refpg_new()
	if !ok {
		panic("oom in init?")
	}
	refup(p_pg)
	pte, err := pmap_walk(kpmap(), int(va), perms)
	if err != 0 {
		panic("oom during AP init")
	}
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("page already mapped %#x", va))
	}
	*pte = p_pg | PTE_P | perms
}

func assert_no_va_map(pmap *pmap_t, va uintptr) {
	pte := pmap_lookup(pmap, int(va))
	if pte != nil && *pte & PTE_P != 0 {
		panic(fmt.Sprintf("va %#x is mapped", va))
	}
}
