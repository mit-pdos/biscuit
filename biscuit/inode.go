package main

import "fmt"
import "sync"
import "unsafe"
import "sort"

const LOGINODEBLK     = 5 // 2
const INODEMASK       = (1 << LOGINODEBLK)-1
const INDADDR        = (BSIZE/8)-1


type pgn_t uint

func off2pgn(off int) pgn_t {
	return pgn_t(off) >> PGSHIFT
}

// XXX don't need to fill the destination page if the write covers the whole
// page
type pgcache_t struct {
	pgs	frbh_t
	// the unit size of underlying device
	blocksz		int
	pglim		int
	idm             *imemnode_t
}

type pginfo_t struct {
	pgn		pgn_t
	pg		*bytepg_t
	dirtyblocks	[]bool
	buf             *bdev_block_t
}

func (pc *pgcache_t) pgc_init(bsz int, idm *imemnode_t) {
	if idm == nil {
		panic("invalid idm")
	}
	if PGSIZE % bsz != 0 {
		panic("block size does not divide pgsize")
	}
	pc.blocksz = bsz
	pc.pglim = 0
	pc.idm = idm
}

// takes an offset, returns the page number and starting block
func (pc *pgcache_t) _pgblock(offset int) (pgn_t, int) {
	pgn := off2pgn(offset)
	block := (offset % PGSIZE) / pc.blocksz
	return pgn, block
}

// mark the range [offset, end) as dirty
func (pc *pgcache_t) pgdirty(offset, end int) {
	for i := offset; i < end; i += pc.blocksz {
		pgn, chn := pc._pgblock(i)
		pgi, ok := pc.pgs.lookup(pgn)
		if !ok {
			panic("dirtying absent page?")
		}
		pgi.dirtyblocks[chn] = true
	}
}

// returns whether the page for pgn needs to be filled from disk and success.
func (pc *pgcache_t) _ensureslot(pgn pgn_t, fill bool) bool {
	if pc.pgs.nodes >= pc.pglim {
		if !syslimit.mfspgs.take() {
			return false
		}
		pc.pglim++
	}
	if _, ok := pc.pgs.lookup(pgn); !ok {
		offset := (int(pgn) << PGSHIFT)
		blkno, err := pc.idm.offsetblk(offset, true)
		if err != 0 {
			return false
		}
		var b *bdev_block_t
		if fill {
			b = bcache_get_fill(blkno, "_ensureslot1", false)
		} else {
			b = bcache_get_zero(blkno, "_ensureslot2", false)
		}
		pgi := &pginfo_t{pgn: pgn, pg: b.data, buf: b}
		pgi.dirtyblocks = make([]bool, PGSIZE / pc.blocksz)
		pc.pgs.insert(pgi)
	}
	return true
}

func (pc *pgcache_t) _ensurefill(pgn pgn_t,fill bool) err_t {
	ok := pc._ensureslot(pgn, fill)
	if !ok {
		return -ENOMEM
	}
	return 0
}

// returns the raw page and physical address of requested page. fills the page
// if necessary.
func (pc *pgcache_t) pgraw(offset int, fill bool) (*pg_t, pa_t, err_t) {
	pgn, _ := pc._pgblock(offset)
	err := pc._ensurefill(pgn, fill)
	if err != 0 {
		return nil, 0, err
	}
	pgi, ok := pc.pgs.lookup(pgn)
	if !ok {
		panic("eh")
	}
	wpg := (*pg_t)(unsafe.Pointer(pgi.pg))
	return wpg, pgi.buf.pa, 0
}

// offset <= end. if offset lies on the same page as end, the returned slice is
// trimmed (bytes >= end are removed).
func (pc *pgcache_t) pgfor(offset, end int, fill bool) ([]uint8, err_t) {
	if offset > end {
		panic("offset must be less than end")
	}
	pgva, _, err := pc.pgraw(offset, fill)
	if err != 0 {
		return nil, err
	}
	pg := pg2bytes(pgva)[:]

	pgoff := offset % PGSIZE
	pg = pg[pgoff:]
	if offset + len(pg) > end {
		left := end - offset
		pg = pg[:left]
	}
	return pg, 0
}

// write all dirty pages back to device
func (pc *pgcache_t) flush() {
	if memtime {
		return
	}
	pc.pgs.iter(func(pgi *pginfo_t) {
		for j, dirty := range pgi.dirtyblocks {
			if !dirty {
				continue
			}
			pgi.buf.log_write()
			pgi.dirtyblocks[j] = false
		}
	})
}

// Returning buffer to bdev cache
func (pc *pgcache_t) evict() int {
	pc.pgs.iter(func(pgi *pginfo_t) {
		bcache_relse(pgi.buf, "evict")
		for _, d := range pgi.dirtyblocks {
			if d {
				panic("cannot be dirty")
			}
		}
	})
	pc.pgs.clear()
	return 0
}

// pglru_t's lock is a leaf lock
type pglru_t struct {
	sync.Mutex
	head	*imemnode_t
	tail	*imemnode_t
}

func (pl *pglru_t) mkhead(im *imemnode_t) {
	if memtime {
		return
	}
	pl.Lock()
	pl._mkhead(im)
	pl.Unlock()
}

func (pl *pglru_t) _mkhead(im *imemnode_t) {
	if pl.head == im {
		return
	}
	pl._remove(im)
	if pl.head != nil {
		pl.head.pgprev = im
	}
	im.pgnext = pl.head
	pl.head = im
	if pl.tail == nil {
		pl.tail = im
	}
}

func (pl *pglru_t) _remove(im *imemnode_t) {
	if pl.tail == im {
		pl.tail = im.pgprev
	}
	if pl.head == im {
		pl.head = im.pgnext
	}
	if im.pgprev != nil {
		im.pgprev.pgnext = im.pgnext
	}
	if im.pgnext != nil {
		im.pgnext.pgprev = im.pgprev
	}
	im.pgprev, im.pgnext = nil, nil
}

func (pl *pglru_t) remove(im *imemnode_t) {
	if memtime {
		return
	}
	pl.Lock()
	pl._remove(im)
	pl.Unlock()
}

var pglru pglru_t


// In-memory representation of an inode.
type imemnode_t struct {
	sync.Mutex
	priv		inum
	blkno		int
	ioff		int
	// cache of file data
	pgcache		pgcache_t
	// pgnext and pgprev can only be read/modified while holding the lru
	// list lock
	pgnext		*imemnode_t
	pgprev		*imemnode_t
	// cache of inode metadata
	icache		icache_t
	//l		sync.Mutex
	//l		trymutex_t
	// XXXPANIC for sanity
	_amlocked	bool
}


type iref_t struct {
	imem           *imemnode_t
	refcnt          int
}


// Cache of in-mmory inodes. Main invariant: an inode is in memory once so that
// threads see each other updates.
type irefcache_t struct {
	sync.Mutex
	irefs          map[inum]*iref_t
}


func make_irefcache() *irefcache_t {
	ic := &irefcache_t{}
	ic.irefs = make(map[inum]*iref_t, 1e6)
	return ic
}

var irefcache	= make_irefcache()

func print_live_inodes() {
	fmt.Printf("icache %v\n", len(irefcache.irefs))
	for _, v := range irefcache.irefs {
		fmt.Printf("inode %v refcnt %v opencnt %v\n", v.imem.priv, v.refcnt)
	}
}

func (irc *irefcache_t) iref(priv inum, s string) (*imemnode_t, err_t) {
	irc.Lock()
	b, i :=  bidecode(priv)
	if priv <= 0 {
		panic("non-positive priv")
	}
	iref, ok := irc.irefs[priv]
	if ok {
		iref.refcnt++
		if fs_debug {
			fmt.Printf("iref hit %v (%v/%v) cnt %v %s\n", priv, b, i, iref.refcnt, s)
		}
		irc.Unlock()
		return iref.imem, 0
	}
	
	for {
		if len(irc.irefs) < syslimit.vnodes {
			break
		}
		panic("too many inodes")
	}

	imem := &imemnode_t{}
	imem.priv = priv

	iref = &iref_t{}
	iref.refcnt = 1
	iref.imem = imem
	irc.irefs[priv] = iref
	
	if fs_debug {
		fmt.Printf("iref miss %v (%v/%v) cnt %v %s\n", priv, b, i, iref.refcnt, s)
	}

	imem.ilock("iref")
	defer imem.iunlock("iref")
	irc.Unlock()

	// holding inode lock while reading it from disk
	err := imem.idm_init(priv)
	if err != 0 {
		panic("idm_init")
		delete(irefcache.irefs, priv)
		return nil, err
	}
	pglru.mkhead(imem)

	if fs_debug {
		fmt.Printf("iref loaded %v (%v/%v) cnt %v %s %v\n", priv, b, i, iref.refcnt, s, imem)
	}

	return imem, 0
}

func (irc *irefcache_t) refup(idm *imemnode_t) {
	irc.Lock()
	defer irc.Unlock()
	
	iref, ok := irc.irefs[idm.priv]
	if !ok {
		panic("refup")
	}
	iref.refcnt++
}

func (irc *irefcache_t) refdown(idm *imemnode_t, s string) {
	irc.Lock()
	
	iref, ok := irc.irefs[idm.priv]
	if !ok {
		panic("refdown")
	}
	if idm != iref.imem {
		panic("refdown")
	}
	iref.refcnt--
	if iref.refcnt < 0 {
		panic("refdown")
	}
	if iref.refcnt == 0 {
		priv := iref.imem.priv
		b, i :=  bidecode(priv)
		if fs_debug {
			fmt.Printf("decrefcnt: delete inode %v (%v,%v) %v %v %v\n", priv, b, i, s)
		}
		iref.imem.priv = 0
		delete(irefcache.irefs, priv)
		defer irc.Unlock()
		// no need to hold lock because no other thread has access to idm
		idm.evict()
	} else {
		defer irc.Unlock()
	}
}


func (irc *irefcache_t) iref_locked(priv inum, s string) (*imemnode_t, err_t) {
	idm, err := irc.iref(priv, s)
	if err != 0 {
		return nil, err
	}
	idm.ilock(s)
	return idm, err
}

func (irc *irefcache_t) lockall(imems []*imemnode_t) []*imemnode_t {
	var locked []*imemnode_t
	sort.Slice(imems, func(i, j int) bool { return imems[i].priv < imems[j].priv })
	for _, imem := range imems {
		dup := false
		for _, l := range locked {
			if imem.priv == l.priv {
				dup = true
			}
		}
		if !dup {
			locked = append(locked, imem)
			imem.ilock("lockall")
		}
	}
	return locked
}


// creates a new idaemon struct and fills its inode
func (idm *imemnode_t) idm_init(priv inum) err_t {
	blkno, ioff := bidecode(priv)
	idm.priv = priv
	idm.blkno = blkno
	idm.ioff = ioff

	idm.pgcache.pgc_init(BSIZE, idm)

	if fs_debug {
		fmt.Printf("idm_init: readinode block %v(%v,%v)\n", priv, blkno, ioff)
	}
	blk := idm.idibread()
	// print_inodes(blk)
	idm.icache.fill(blk, ioff)
	blk.Unlock()
	bcache_relse(blk, "idm_init")
	return 0
}

func (idm *imemnode_t) evict() {
	idm._derelease()
	idm.pgcache.evict()
	b, i :=  bidecode(idm.priv)
	if fs_debug {
		fmt.Printf("evict: %v (%v,%v)\n", idm.priv, b, i)
	}
	pglru.remove(idm)
}

func (idm *imemnode_t) ilock(s string) {
	// if idm._amlocked {
	//	fmt.Printf("ilocked: warning %v %v\n", idm.priv, s)
	// }
	idm.Lock()
	if idm.priv <= 0 {
		panic("non-positive priv")
	}
	idm._amlocked = true
	// fmt.Printf("ilock: acquired %v %s\n", idm.priv, s)
}


func (idm *imemnode_t) iunlock(s string) {
	// fmt.Printf("iunlock: release %v %v\n", idm.priv, s)
	if !idm._amlocked {
		panic("iunlock")
	}
	idm._amlocked = false
	idm.Unlock()
}


func (idm *imemnode_t) iunlock_refdown(s string) {
	idm.iunlock(s)
	irefcache.refdown(idm, s)
}

func (idm *imemnode_t) _iupdate() {
	iblk := idm.idibread()
	if idm.icache.flushto(iblk, idm.ioff) {
		iblk.Unlock()
		iblk.log_write()
	} else {
		iblk.Unlock()
	}
	bcache_relse(iblk, "_iupdate")
}

func (idm *imemnode_t) do_trunc(truncto uint) err_t {
	if idm.icache.itype != I_FILE && idm.icache.itype != I_DEV {
		panic("bad truncate")
	}
	err := idm.itrunc(truncto)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_read(dst userio_i, offset int) (int, err_t) {
	return idm.iread(dst, offset)
}

func (idm *imemnode_t) do_write(src userio_i, _offset int, append bool) (int, err_t) {
	if idm.icache.itype == I_DIR {
		panic("write to dir")
	}
	offset := _offset
	if append {
		offset = idm.icache.size
	}
	wrote, err := idm.iwrite(src, offset)
	idm._iupdate()
	return wrote, err
}

func (idm *imemnode_t) do_stat(st *stat_t) err_t {
	st.wdev(0)
	st.wino(uint(idm.priv))
	st.wmode(idm.mkmode())
	st.wsize(uint(idm.icache.size))
	st.wrdev(mkdev(idm.icache.major, idm.icache.minor))
	return 0
}

func (idm *imemnode_t) do_mmapi(off, len int) ([]mmapinfo_t, err_t) {
	if idm.icache.itype != I_FILE && idm.icache.itype != I_DIR {
		panic("bad mmapinfo")
	}
	return idm.immapinfo(off, len)
}

func (idm *imemnode_t) do_dirchk(wantdir bool) err_t {
	amdir := idm.icache.itype == I_DIR
	if wantdir && !amdir {
		return -ENOTDIR
	} else if !wantdir && amdir {
		return -EISDIR
	} else if amdir {
		amempty, err := idm.idirempty()
		if err != 0 {
			return err
		}
		if !amempty {
			return -ENOTEMPTY
		}
	}
	return 0
}

// unlink cannot encounter OOM since directory page caches are eagerly loaded.
func (idm *imemnode_t) do_unlink(name string) err_t {
	_, err := idm.iunlink(name)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_insert(fn string, n inum) err_t {
	// create new dir ent with given inode number
	err := idm.iinsert(fn, n)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_createnod(fn string, maj, min int) (inum, err_t) {
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DEV
	cnext, err := idm.icreate(fn, itype, maj, min)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createfile(fn string) (inum, err_t) {
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_FILE
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createdir(fn string) (inum, err_t) {
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DIR
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func _fullpath(priv inum) (string, err_t) {
	c, err := irefcache.iref_locked(priv, "_fullpath")
	if err != 0 {
		return "", err
	}
	if c.icache.itype != I_DIR {
		panic("fullpath on non-dir")
	}

	// starting at the target file, walk up to the root prepending the name
	// (which is stored in the parent's directory entry)
	last := c.priv
	acc := ""
	for c.priv != iroot {
		pari, err := c.ilookup("..")
		if err != 0 {
			panic(".. must exist")
		}
		par, err := irefcache.iref(pari, "do_fullpath_par")
		c.iunlock("do_fullpath_c")
		if err != 0 {
			return "", err
		}
		par.ilock("do_fullpath_par")
		name, err := par._denamefor(last)
		if err != 0 {
			if err == -ENOENT {
				panic("child must exist")
			}
			par.iunlock_refdown("do_fullpath_par")
			return "", err
		}
		// POSIX: "no unnecessary slashes"
		if acc == "" {
			acc = name
		} else {
			acc = name + "/" + acc
		}
		last = par.priv
		c = par
	}
	c.iunlock_refdown("do_fullpath_c2")
	acc = "/" + acc
	return acc, 0
}

func (idm *imemnode_t) _linkdown() {
	idm.icache.links--
	idm._iupdate()
}

func (idm *imemnode_t) _linkup() {
	idm.icache.links++
	idm._iupdate()
}

type icache_t struct {
	itype		int
	links		int
	size		int
	major		int
	minor		int
	indir		int
	addrs		[NIADDRS]int
	// inode specific metadata blocks
	dentc struct {
		// true iff all directory entries are cached, thus there is no
		// need to check the disk
		haveall		bool
		// maximum number of cached dents this icache is allowed to
		// have
		max		int
		dents		dc_rbh_t
		freel		fdelist_t
	}
}

type fdent_t struct {
	offset	int
	next	*fdent_t
}

// linked list of free directory entries
type fdelist_t struct {
	head	*fdent_t
	n	int
}

func (il *fdelist_t) addhead(off int) {
	d := &fdent_t{offset: off}
	d.next = il.head
	il.head = d
	il.n++
}

func (il *fdelist_t) remhead() (*fdent_t, bool) {
	var ret *fdent_t
	if il.head != nil {
		ret = il.head
		il.head = ret.next
		il.n--
	}
	return ret, ret != nil
}

func (il *fdelist_t) count() int {
	return il.n
}

// struct to hold the offset/priv of directory entry slots
type icdent_t struct {
	offset	int
	priv	inum
}

// metadata block interface; only one inode touches these blocks at a time,
// thus no concurrency control
func (ic *icache_t) mbread(blockn int) *bdev_block_t {
	mb := bcache_get_fill(blockn, "_mbensure", false)
	return mb
}

func (ic *icache_t) fill(blk *bdev_block_t, ioff int) {
	inode := inode_t{blk, ioff}
	ic.itype = inode.itype()
	if ic.itype <= I_FIRST || ic.itype > I_VALID {
		fmt.Printf("itype: %v for %v (%v,%v)\n", ic.itype,
		    biencode(blk.block, ioff), blk.block, ioff)
		panic("bad itype in fill")
	}
	ic.links = inode.linkcount()
	ic.size  = inode.size()
	ic.major = inode.major()
	ic.minor = inode.minor()
	ic.indir = inode.indirect()
	for i := 0; i < NIADDRS; i++ {
		ic.addrs[i] = inode.addr(i)
	}
}

// returns true if the inode data changed, and thus needs to be flushed to disk
func (ic *icache_t) flushto(blk *bdev_block_t, ioff int) bool {
	inode := inode_t{blk, ioff}
	j := inode
	k := ic
	ret := false
	if j.itype() != k.itype || j.linkcount() != k.links ||
	    j.size() != k.size || j.major() != k.major ||
	    j.minor() != k.minor || j.indirect() != k.indir {
		ret = true
	}
	for i, v := range ic.addrs {
		if inode.addr(i) != v {
			ret = true
		}
	}
	inode.w_itype(ic.itype)
	inode.w_linkcount(ic.links)
	inode.w_size(ic.size)
	inode.w_major(ic.major)
	inode.w_minor(ic.minor)
	inode.w_indirect(ic.indir)
	for i := 0; i < NIADDRS; i++ {
		inode.w_addr(i, ic.addrs[i])
	}
	return ret
}

// takes as input the file offset and whether the operation is a write and
// returns the block number of the block responsible for that offset.
func (idm *imemnode_t) offsetblk(offset int, writing bool) (int, err_t) {
	// ensure block exists
	ensureb := func(blkno int) (int, bool) {
		if !writing || blkno != 0 {
			return blkno, false
		}
		nblkno := balloc()
		return nblkno, true
	}
	// this function makes sure that every slot up to and including idx of
	// the given indirect block has a block allocated. it is necessary to
	// allocate a bunch of zero'd blocks for a file when someone lseek()s
	// past the end of the file and then writes.
	ensureind := func(blk *bdev_block_t, idx int) {
		if !writing {
			return
		}
		added := false
		for i := 0; i <= idx; i++ {
			slot := i * 8
			s := blk.data[:]
			blkn := readn(s, 8, slot)
			blkn, isnew := ensureb(blkn)
			if isnew {
				writen(s, 8, slot, blkn)
				added = true
				// make sure to zero indirect pointer block if
				// allocated
				if idx == INDADDR {
					fmt.Printf("indirect indx\n")
				}
			}
		}
		if added {
			blk.log_write()
		}
	}

	whichblk := offset/BSIZE
	// make sure there is no empty space for writes past the end of the
	// file
	if writing {
		ub := whichblk + 1
		if ub > NIADDRS {
			ub = NIADDRS
		}
		for i := 0; i < ub; i++ {
			if idm.icache.addrs[i] != 0 {
				continue
			}
			tblkn := balloc()
			// imemnode_t.iupdate() will make sure the
			// icache is updated on disk
			idm.icache.addrs[i] = tblkn
		}
	}

	var blkn int
	if whichblk >= NIADDRS {
		indslot := whichblk - NIADDRS
		slotpb := INDADDR
		nextindb :=  INDADDR *8
		indno := idm.icache.indir
		// get first indirect block
		indno, isnew := ensureb(indno)
		if isnew {
			// new indirect block will be written to log below
			idm.icache.indir = indno
		}
		// follow indirect block chain
		indblk := idm.icache.mbread(indno)
		for i := 0; i < indslot/slotpb; i++ {
			// make sure the indirect block has no empty spaces if
			// we are writing past the end of the file. if
			// necessary, ensureind also allocates the next
			// indirect pointer block.
			ensureind(indblk, slotpb)

			indno = readn(indblk.data[:], 8, nextindb)
			bcache_relse(indblk, "offsetblk1")
			indblk = idm.icache.mbread(indno)
		}
		// finally get data block from indirect block
		slotoff := (indslot % slotpb)
		ensureind(indblk, slotoff)
		noff := (slotoff)*8
		s := indblk.data[:]
		blkn = readn(s, 8, noff)
		blkn, isnew = ensureb(blkn)
		if isnew {
			writen(s, 8, noff, blkn)
			indblk.log_write()
		}
		bcache_relse(indblk, "offsetblk2")
	} else {
		blkn = idm.icache.addrs[whichblk]
	}
	if blkn <= 0 || blkn >= superb.lastblock() {
		panic("bad data blocks")
	}
	return blkn, 0
}

func (idm *imemnode_t) iread(dst userio_i, offset int) (int, err_t) {
	pglru.mkhead(idm)
	isz := idm.icache.size
	c := 0
	for offset + c < isz && dst.remain() != 0 {
		src, err := idm.pgcache.pgfor(offset + c, isz, true)
		if err != 0 {
			return c, err
		}
		wrote, err := dst.uiowrite(src)
		c += wrote
		if err != 0 {
			return c, err
		}
	}
	return c, 0
}

func (idm *imemnode_t) iwrite(src userio_i, offset int) (int, err_t) {
	pglru.mkhead(idm)
	sz := src.totalsz()
	newsz := offset + sz
	err := idm._preventhole(idm.icache.size, uint(newsz))
	if err != 0 {
		return 0, err
	}
	c := 0
	for c < sz {
		noff := offset + c
		dst, err := idm.pgcache.pgfor(noff, newsz, false)
		if err != 0 {
			return c, err
		}
		read, err := src.uioread(dst)
		if err != 0 {
			return c, err
		}
		idm.pgcache.pgdirty(noff, noff + read)
		c += read
	}
	wrote := c
	if newsz > idm.icache.size {
		idm.icache.size = newsz
	}
	idm.pgcache.flush()
	return wrote, 0
}

// if a progam 1) lseeks past end of file and writes or 2) extends file size
// via ftruncate/truncate, make sure the page cache is filled with zeros for
// the new bytes.
func (idm *imemnode_t) _preventhole(_oldlen int, newlen uint) err_t {
	// XXX fix sign
	oldlen := uint(_oldlen)
	ret := err_t(0)
	if newlen > oldlen {
		// extending file; fill new content in page cache with zeros
		first := true
		c := oldlen
		for c < newlen {
			pg, err := idm.pgcache.pgfor(int(c), int(newlen), false)
			if err != 0 {
				ret = -ENOMEM
				break
			}
			if first {
				// XXX go doesn't seem to have a good way to
				// zero a slice
				for i := range pg {
					pg[i] = 0
				}
				first = false
			}
			c += uint(len(pg))
		}
		idm.pgcache.pgdirty(int(oldlen), int(c))
	}
	return ret
}

func (idm *imemnode_t) itrunc(newlen uint) err_t {
	err := idm._preventhole(idm.icache.size, newlen)
	if err != 0 {
		return err
	}
	// it's important that the icache.size is updated after filling the
	// page cache since fs_fill reads from disk any pages whose offset is
	// <= icache.size
	idm.icache.size = int(newlen)
	idm.pgcache.flush()
	return 0
}


// returns the offset of an empty directory entry. returns error if failed to
// allocate page for the new directory entry.
func (idm *imemnode_t) _denextempty() (int, err_t) {
	dc := &idm.icache.dentc
	if ret, ok := dc.freel.remhead(); ok {
		return ret.offset, 0
	}

	// see if we have an empty slot before expanding the directory
	if !idm.icache.dentc.haveall {
		var de icdent_t
		found, err := idm._descan(func(fn string, tde icdent_t) bool {
			if fn == "" {
				de = tde
				return true
			}
			return false
		})
		if err != 0 {
			return 0, err
		}
		if found {
			return de.offset, 0
		}
	}

	// current dir blocks are full -- allocate new dirdata block. make
	// sure its in the page cache but not fill'ed from disk
	newsz := idm.icache.size + BSIZE
	_, err := idm.pgcache.pgfor(idm.icache.size, newsz, false)
	if err != 0 {
		return 0, err
	}
	idm.pgcache.pgdirty(idm.icache.size, newsz)
	newoff := idm.icache.size
	// start from 1 since we return slot 0 directly
	for i := 1; i < NDIRENTS; i++ {
		noff := newoff + NDBYTES*i
		idm._deaddempty(noff)
	}
	idm.icache.size = newsz
	return newoff, 0
}

// if _deinsert fails to allocate a page, idm is left unchanged.
func (idm *imemnode_t) _deinsert(name string, nblkno int, ioff int) err_t {
	// XXXPANIC
	//if _, err := idm._delookup(name); err == 0 {
	//	panic("dirent already exists")
	//}
	var ddata dirdata_t
	noff, err := idm._denextempty()
	if err != 0 {
		return err
	}
        // dennextempty() made the slot so we won't fill
	pg, err := idm.pgcache.pgfor(noff, noff+NDBYTES, true) 
	if err != 0 {
		return err
	}
	ddata.data = pg

	// write dir entry
	//if ddata.filename(0) != "" {
	//	panic("dir entry slot is not free")
	//}
	ddata.w_filename(0, name)
	ddata.w_inodenext(0, nblkno, ioff)
	idm.pgcache.pgdirty(noff, noff+NDBYTES)
	idm.pgcache.flush()
	icd := icdent_t{noff, inum(biencode(nblkno, ioff))}
	ok := idm._dceadd(name, icd)
	dc := &idm.icache.dentc
	dc.haveall = dc.haveall && ok
	return 0
}

// calls f on each directory entry (including empty ones) until f returns true
// or it has been called on all directory entries. _descan returns true if f
// returned true.
func (idm *imemnode_t) _descan(f func(fn string, de icdent_t) bool) (bool, err_t) {
	for i := 0; i < idm.icache.size; i+= BSIZE {
		pg, err := idm.pgcache.pgfor(i, i+BSIZE, true)
		if err != 0 {
			return false, err
		}
		// XXXPANIC
		if len(pg) != BSIZE {
			panic("wut")
		}
		dd := dirdata_t{pg}
		for j := 0; j < NDIRENTS; j++ {
			tfn := dd.filename(j)
			tpriv := dd.inodenext(j)
			tde := icdent_t{i+j*NDBYTES, tpriv}
			if f(tfn, tde) {
				return true, 0
			}
		}
	}
	return false, 0
}

func (idm *imemnode_t) _delookup(fn string) (icdent_t, err_t) {
	if fn == "" {
		panic("bad lookup")
	}
	if de, ok := idm.icache.dentc.dents.lookup(fn); ok {
		return de, 0
	}
	var zi icdent_t
	if idm.icache.dentc.haveall {
		// cache negative entries?
		return zi, -ENOENT
	}

	// not in cached dirents
	found := false
	haveall := true
	var de icdent_t
	_, err := idm._descan(func(tfn string, tde icdent_t) bool {
		if tfn == "" {
			return false
		}
		if tfn == fn {
			de = tde
			found = true
		}
		if !idm._dceadd(tfn, tde) {
			haveall = false
		}
		return found && !haveall
	})
	if err != 0 {
		return zi, err
	}
	idm.icache.dentc.haveall = haveall
	if !found {
		return zi, -ENOENT
	}
	return de, 0
}

func (idm *imemnode_t) _deremove(fn string) (icdent_t, err_t) {
	var zi icdent_t
	de, err := idm._delookup(fn)
	if err != 0 {
		return zi, err
	}
	pg, err := idm.pgcache.pgfor(de.offset, de.offset + NDBYTES, true)
	if err != 0 {
		return zi, err
	}
	dirdata := dirdata_t{pg}
	dirdata.w_filename(0, "")
	dirdata.w_inodenext(0, 0, 0)
	idm.pgcache.pgdirty(de.offset, de.offset + NDBYTES)
	idm.pgcache.flush()
	// add back to free dents
	idm.icache.dentc.dents.remove(fn)
	idm._deaddempty(de.offset)
	return de, 0
}

// returns the filename mapping to tnum
func (idm *imemnode_t) _denamefor(tnum inum) (string, err_t) {
	// check cache first
	var fn string
	found := idm.icache.dentc.dents.iter(func(dn string, de icdent_t) bool {
		if de.priv == tnum {
			fn = dn
			return true
		}
		return false
	})
	if found {
		return fn, 0
	}

	// not in cache; shucks!
	var de icdent_t
	found, err := idm._descan(func(tfn string, tde icdent_t) bool {
		if tde.priv == tnum {
			fn = tfn
			de = tde
			return true
		}
		return false
	})
	if err != 0 {
		return "", err
	}
	if !found {
		return "", -ENOENT
	}
	idm._dceadd(fn, de)
	return fn, 0
}

// returns true if idm, a directory, is empty (excluding ".." and ".").
func (idm *imemnode_t) _deempty() (bool, err_t) {
	if idm.icache.dentc.haveall {
		dentc := &idm.icache.dentc
		hasfiles := dentc.dents.iter(func(dn string, de icdent_t) bool {
			if dn != "." && dn != ".." {
				return true
			}
			return false
		})
		return !hasfiles, 0
	}
	notempty, err := idm._descan(func(fn string, de icdent_t) bool {
		return fn != "" && fn != "." && fn != ".."
	})
	if err != 0 {
		return false, err
	}
	return !notempty, 0
}

// empties the dirent cache, returning the number of dents purged.
func (idm *imemnode_t) _derelease() int {
	dc := &idm.icache.dentc
	dc.haveall = false
	dc.dents.clear()
	dc.freel.head = nil
	ret := dc.max
	syslimit.dirents.given(uint(ret))
	dc.max = 0
	return ret
}

// ensure that an insert/unlink cannot fail i.e. fail to allocate a page. if fn
// == "", look for an empty dirent, otherwise make sure the page containing fn
// is in the page cache.
func (idm *imemnode_t) _deprobe(fn string) err_t {
	if fn != "" {
		de, err := idm._delookup(fn)
		if err != 0 {
			return err
		}
		noff := de.offset
		_, err = idm.pgcache.pgfor(noff, noff+NDBYTES, true)
		return err
	}
	noff, err := idm._denextempty()
	if err != 0 {
		return err
	}
	// dennextempty() made the slot so we won't fill
	_, err = idm.pgcache.pgfor(noff, noff+NDBYTES, true)
	if err != 0 {
		return err
	}
	idm._deaddempty(noff)
	return 0
}

// returns true if this idm has enough free cache space for a single dentry
func (idm *imemnode_t) _demayadd() bool {
	dc := &idm.icache.dentc
	//have := len(dc.dents) + len(dc.freem)
	have := dc.dents.nodes + dc.freel.count()
	if have + 1 < dc.max {
		return true
	}
	// reserve more directory entries
	take := 64
	if syslimit.dirents.taken(uint(take)) {
		dc.max += take
		return true
	}
	lhits++
	return false
}

// caching is best-effort. returns true if fn was added to the cache
func (idm *imemnode_t) _dceadd(fn string, de icdent_t) bool {
	dc := &idm.icache.dentc
	if !idm._demayadd() {
		return false
	}
	dc.dents.insert(fn, de)
	return true
}

func (idm *imemnode_t) _deaddempty(off int) {
	if !idm._demayadd() {
		return
	}
	dc := &idm.icache.dentc
	dc.freel.addhead(off)
}

// guarantee that there is enough memory to insert at least one directory
// entry.
func (idm *imemnode_t) probe_insert() err_t {
	// insert and remove a fake directory entry, forcing a page allocation
	// if necessary.
	if err := idm._deprobe(""); err != 0 {
		return err
	}
	return 0
}

// guarantee that there is enough memory to unlink a dirent (which may require
// allocating a page to load the dirents from disk).
func (idm *imemnode_t) probe_unlink(fn string) err_t {
	if err := idm._deprobe(fn); err != 0 {
		return err
	}
	return 0
}

// returns true if all the inodes on ib are also marked dead and thus this
// inode block should be freed.
func _alldead(ib *bdev_block_t) bool {
	bwords := BSIZE/8
	ret := true
	for i := 0; i < bwords/NIWORDS; i++ {
		icache := inode_t{ib, i}
		if icache.itype() != I_DEAD {
			ret = false
			break
		}
	}
	return ret
}

// caller must flush ib to disk
func _iallocundo(iblkn int, ni *inode_t, ib *bdev_block_t) {
	ni.w_itype(I_DEAD)
	if _alldead(ib) {
		bfree(iblkn)
	}
}

// reverts icreate(). called after failure to allocate that prevents an FS
// operation from continuing.
func (idm *imemnode_t) create_undo(childi inum, childn string) {
	ci, err := idm.iunlink(childn)
	if err != 0 {
		panic("but insert just succeeded")
	}
	if ci != childi {
		panic("inconsistent")
	}
	bn, ioff := bidecode(childi)
	ib := bcache_get_fill(bn, "create_undo", true)
	ni := &inode_t{ib, ioff}
	_iallocundo(bn, ni, ib)
	ib.Unlock()
	bcache_relse(ib, "create_undo")
}

func print_inodes(b *bdev_block_t) {
	blkwords := BSIZE/8
	n := blkwords/NIWORDS
	for i := 0; i < n; i++ {
		inode := &inode_t{b, i}
		fmt.Printf("inode: %v %v %v %v\n", i, b.block, inode.itype(), biencode(b.block,i))
	}
}

func (idm *imemnode_t) icreate(name string, nitype, major, minor int) (inum, err_t) {
	
	if nitype <= I_INVALID || nitype > I_VALID {
		panic("bad itype!")
	}
	if name == "" {
		panic("icreate with no name")
	}
	if nitype != I_DEV && (major != 0 || minor != 0) {
		panic("inconsistent args")
	}
	// make sure file does not already exist
	de, err := idm._delookup(name)
	if err == 0 {
		return de.priv, -EEXIST
	}

	// allocate new inode but read from disk, because block may have gotten
	// evicted from bcache.
	newbn, newioff := ialloc()
	newiblk := bcache_get_fill(newbn, "icreate", true)

	if fs_debug {
		fmt.Printf("ialloc: %v %v %v\n", newbn, newioff, biencode(newbn, newioff))
	}
	
	newinode := &inode_t{newiblk, newioff}
	newinode.w_itype(nitype)
	newinode.w_linkcount(1)
	newinode.w_size(0)
	newinode.w_major(major)
	newinode.w_minor(minor)
	newinode.w_indirect(0)
	for i := 0; i < NIADDRS; i++ {
		newinode.w_addr(i, 0)
	}

	// deinsert will call write_log and we don't want to hold newiblk's lock
	// while sending on the channel to the daemon.  The daemon might want to
	// lock this block (e.g., to pin) when adding it to the log, but it
	// would block and not read from the channel again.  it is safe to
	// unlock because we are done creating a legit inode in the block.
	newiblk.Unlock()

	// write new directory entry referencing newinode
	err = idm._deinsert(name, newbn, newioff)
	if err != 0 {
		_iallocundo(newbn, newinode, newiblk)
	}
	newiblk.log_write()
	bcache_relse(newiblk, "icreate")

	newinum := inum(biencode(newbn, newioff))

	if newinum <= 0 {
		panic("icreate")
	}
	return newinum, err
}

func (idm *imemnode_t) ilookup(name string) (inum, err_t) {
	pglru.mkhead(idm)
	// did someone confuse a file with a directory?
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}
	de, err := idm._delookup(name)
	if err != 0 {
		return 0, err
	}
	return de.priv, 0
}

// creates a new directory entry with name "name" and inode number priv
func (idm *imemnode_t) iinsert(name string, priv inum) err_t {
	if idm.icache.itype != I_DIR {
		return -ENOTDIR
	}
	if _, err := idm._delookup(name); err == 0 {
		return -EEXIST
	} else if err != -ENOENT {
		return err
	}
	if priv <= 0 {
		fmt.Printf("insert: non-positive inum %v %v\n", name, priv)
		panic("iinsert")
	}
	a, b := bidecode(priv)
	err := idm._deinsert(name, a, b)
	return err
}

// returns inode number of unliked inode so caller can decrement its ref count
func (idm *imemnode_t) iunlink(name string) (inum, err_t) {
	if idm.icache.itype != I_DIR {
		panic("unlink to non-dir")
	}
	de, err := idm._deremove(name)
	if err != 0 {
		return 0, err
	}
	return de.priv, 0
}

// returns true if the inode has no directory entries
func (idm *imemnode_t) idirempty() (bool, err_t) {
	if idm.icache.itype != I_DIR {
		panic("i am not a dir")
	}
	return idm._deempty()
}

func (idm *imemnode_t) immapinfo(offset, len int) ([]mmapinfo_t, err_t) {
	isz := idm.icache.size
	if (len != -1 && len < 0) || offset < 0 {
		panic("bad off/len")
	}
	if offset >= isz {
		return nil, -EINVAL
	}
	if len == -1 || offset + len > isz {
		len = isz - offset
	}
	len = roundup(offset + len, PGSIZE) - rounddown(offset, PGSIZE)
	pgc := len / PGSIZE
	ret := make([]mmapinfo_t, pgc)
	for i := 0; i < len; i += PGSIZE {
		pg, phys, err := idm.pgcache.pgraw(offset + i, true)
		if err != 0 {
			return nil, err
		}
		pgn := i / PGSIZE
		ret[pgn].pg = pg
		ret[pgn].phys = phys
	}
	return ret, 0
}

func (idm *imemnode_t) idibread() *bdev_block_t {
	return bcache_get_fill(idm.blkno, "idibread", true)
}

// frees all blocks occupied by idm
func (idm *imemnode_t) ifree() {
	allb := make([]int, 0, 10)
	add := func(blkno int) {
		if blkno != 0 {
			allb = append(allb, blkno)
		}
	}

	for i := 0; i < NIADDRS; i++ {
		add(idm.icache.addrs[i])
	}
	indno := idm.icache.indir
	for indno != 0 {
		add(indno)
		blk := idm.icache.mbread(indno)
		blks := blk.data[:]
		for i := 0; i < INDADDR; i++ {
			off := i*8
			nblkno := readn(blks, 8, off)
			add(nblkno)
		}
		nextoff := INDADDR*8
		indno = readn(blks, 8, nextoff)
		bcache_relse(blk, "ifree1")
	}

	iblk := idm.idibread()
	if iblk == nil {
		panic("ifree")
	}
	idm.icache.itype = I_DEAD
	idm.icache.flushto(iblk, idm.ioff)
	if _alldead(iblk) {
		add(idm.blkno)
		iblk.Unlock()    // XXX log_write first?
	} else {
		iblk.Unlock()
		iblk.log_write()
	}
	bcache_relse(iblk, "ifree2")

	// could batch free
	for _, blkno := range allb {
		bfree(blkno)
	}

	// idm.pgcache.release()
}

// used for {,f}stat
func (idm *imemnode_t) mkmode() uint {
	itype := idm.icache.itype
	switch itype {
	case I_DIR, I_FILE:
		return uint(itype << 16)
	case I_DEV:
		// this can happen by fs-internal stats
		return mkdev(idm.icache.major, idm.icache.minor)
	default:
		panic("weird itype")
	}
}

// XXX: remove
func fieldr(p *bytepg_t, field int) int {
	return readn(p[:], 8, field*8)
}

func fieldw(p *bytepg_t, field int, val int) {
	writen(p[:], 8, field*8, val)
}

func biencode(blk int, iidx int) int {
	logisperblk := uint(LOGINODEBLK)
	isperblk := 1 << logisperblk
	if iidx < 0 || iidx >= isperblk {
		panic("bad inode index")
	}
	return int(blk << logisperblk | iidx)
}

func bidecode(val inum) (int, int) {
	logisperblk := uint(LOGINODEBLK)
	blk := int(uint(val) >> logisperblk)
	iidx := int(val) & INODEMASK
	return blk, iidx
}

