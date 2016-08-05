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
	}
}

func (vmi *vminfo_t) filepage(va uintptr) (*[512]int, uintptr) {
	if vmi.mtype != VFILE {
		panic("must be file mapping")
	}
	voff := int(va - (vmi.pgn << PGSHIFT))
	foff := vmi.file.foff + voff
	mmapi, err := vmi.file.mfile.mfops.mmapi(foff, 1)
	if err != 0 {
		panic("must succeed")
	}
	return mmapi[0].pg, uintptr(mmapi[0].phys)
}

type mtype_t uint

// types of mappings
const (
	VANON	mtype_t = 1 << iota
	VFILE	mtype_t = 1 << iota
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
// XXX make arg value to avoid allocations?
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
		m.hole.pglen = m.hole.startn - vmi.pgn
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
	ret.rb.root = m._copy1(nil, m.rb.root)
	return ret
}

func (m *vmregion_t) dump() {
	m.iter(func(vmi *vminfo_t) {
		end := (vmi.pgn + uintptr(vmi.pglen)) << PGSHIFT
		var perms string
		if vmi.mtype == VANON {
			perms = "A-"
		} else {
			perms = "F-"
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
				pglen = startn - vmi.pgn
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
		pglen = (0x100 << 39) - startn
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

func (m *vmregion_t) remove(start, len int) {
	pgn := uintptr(start) >> PGSHIFT
	pglen := roundup(len, PGSIZE) >> PGSHIFT
	m._pglen -= pglen
	n := m.rb.lookup(pgn)
	if n == nil {
		//m.dump()
		panic("addr not mapped")
	}
	m._clear(&n.vmi, pglen)
	// remove the whole node?
	if n.vmi.pgn == pgn && n.vmi.pglen == pglen {
		m.rb.remove(n)
		return
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
		return
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
}

// tracks memory pages
type pgtracker_t map[int]*[512]int

// tracks all pages allocated by go internally by the kernel such as pmap pages
// allocated by the kernel (not the bootloader/runtime)
var kplock = sync.Mutex{}
var kpages = pgtracker_t{}

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
	panic("no")
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

//var zeropg = new([512]int)
var zerobpg *[PGSIZE]uint8
var zeropg *[512]int
var p_zeropg int
var _dmapinit bool

// installs a direct map for 512G of physical memory via the recursive mapping
func dmap_init() {
	//kpmpages.pminit()

	// the default cpu qemu uses for x86_64 supports 1GB pages, but
	// doesn't report it in cpuid 0x80000001... i wonder why.
	_, _, _, edx  := runtime.Cpuid(0x80000001, 0)
	gbpages := edx & (1 << 26) != 0

	dpte := caddr(VREC, VREC, VREC, VREC, VDIRECT)
	if *dpte & PTE_P != 0 {
		panic("dmap slot taken")
	}

	pdpt  := new([512]int)
	//pdpt, p_pdpt := refpg_new()
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
		//pd, p_pd := refpg_new()
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
	_dmapinit = true

	//pte := pmap_lookup(kpmap(), int(uintptr(unsafe.Pointer(zeropg))))
	//if pte == nil || *pte & PTE_P == 0 {
	//	panic("wut")
	//}
	//p_zeropg = *pte & PTE_ADDR
	zeropg, p_zeropg = _refpg_new()
	refup(uintptr(p_zeropg))
	zerobpg = (*[PGSIZE]uint8)(unsafe.Pointer(zeropg))
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

func pmap_pgtbl(pml4 *[512]int, v int, create bool,
    perms int) (*[512]int, int) {
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
		//np, p_np := pg_new()
		_, p_np := refpg_new()
		refup(uintptr(p_np))
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
func _pmap_walk(pml4 *[512]int, v int, create bool, perms int) *int {
	pgtbl, slot := pmap_pgtbl(pml4, v, create, perms)
	if pgtbl == nil {
		return nil
	}
	return &pgtbl[slot]
}

func pmap_walk(pml4 *[512]int, v int, perms int) *int {
	return _pmap_walk(pml4, v, true, perms)
}

func pmap_lookup(pml4 *[512]int, v int) *int {
	return _pmap_walk(pml4, v, false, 0)
}

func copy_pmap1(ptemod func(int) (int, int), dst *[512]int, src *[512]int,
    depth int) bool {

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
		perms := c & PTE_FLAGS
		dst[i] = p_np | perms
		nsrc := pe2pg(c)
		if copy_pmap1(ptemod, np, nsrc, depth - 1) {
			doinval = true
		}
	}

	return doinval
}

// deep copies the pmap. ptemod is an optional function that takes the
// original PTE as an argument and returns two values: new PTE for the pmap
// being copied and PTE for the new pmap.
func copy_pmap(ptemod func(int) (int, int),
    pm *[512]int) (*[512]int, int, bool) {
	npm, p_npm := pg_new()
	doinval := copy_pmap1(ptemod, npm, pm, 4)
	npm[VREC] = p_npm | PTE_P | PTE_W
	return npm, p_npm, doinval
}

func fork_pmap1(dst *[512]int, src *[512]int, depth int) bool {

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
		perms := c & PTE_FLAGS
		dst[i] = p_np | perms
		nsrc := pe2pg(c)
		if fork_pmap1(np, nsrc, depth - 1) {
			doinval = true
		}
	}

	return doinval
}

func fork_pmap(pm *[512]int) (*[512]int, int, bool) {
	npm, p_npm := pg_new()
	doinval := fork_pmap1(npm, pm, 4)
	npm[VREC] = p_npm | PTE_P | PTE_W
	return npm, p_npm, doinval
}

// forks the ptes only for the virtual address range specified
func ptefork(cpmap, ppmap *[512]int, start, end int) bool {
	doflush := false
	i := start
	for i < end {
		pptb, slot := pmap_pgtbl(ppmap, i, false, 0)
		cptb, _ := pmap_pgtbl(cpmap, i, true, PTE_U | PTE_W)
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
			// XXXPANIC
			if pte & PTE_U == 0 {
				panic("huh?")
			}
			refup(uintptr(phys))
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
	_, p_pg := refpg_new()
	refup(uintptr(p_pg))
	pte := pmap_walk(kpmap(), int(va), perms)
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
