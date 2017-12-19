package main

import "fmt"
import "sync"
import "unsafe"
import "sort"

const LOGINODEBLK     = 5 // 2
const INODEMASK       = (1 << LOGINODEBLK)-1
const INDADDR        = (BSIZE/8)-1

// inode format:
// bytes, meaning
// 0-7,    inode type
// 8-15,   link count
// 16-23,  size in bytes
// 24-31,  major
// 32-39,  minor
// 40-47,  indirect block
// 48-80,  block addresses

// ...the above comment may be out of date
type inode_t struct {
	iblk	*bdev_block_t
	ioff	int
}

// inode file types
const(
	I_FIRST = 0
	I_INVALID = I_FIRST
	I_FILE  = 1
	I_DIR   = 2
	I_DEV   = 3
	I_VALID = I_DEV
	// ready to be reclaimed
	I_DEAD  = 4
	I_LAST = I_DEAD

	// direct block addresses
	NIADDRS = 10
	// number of words in an inode
	NIWORDS = 6 + NIADDRS
)

func ifield(iidx int, fieldn int) int {
	return iidx*NIWORDS + fieldn
}

// iidx is the inode index; necessary since there are four inodes in one block
func (ind *inode_t) itype() int {
	it := fieldr(ind.iblk.data, ifield(ind.ioff, 0))
	if it < I_FIRST || it > I_LAST {
		panic(fmt.Sprintf("weird inode type %d", it))
	}
	return it
}

func (ind *inode_t) linkcount() int {
	return fieldr(ind.iblk.data, ifield(ind.ioff, 1))
}

func (ind *inode_t) size() int {
	return fieldr(ind.iblk.data, ifield(ind.ioff, 2))
}

func (ind *inode_t) major() int {
	return fieldr(ind.iblk.data, ifield(ind.ioff, 3))
}

func (ind *inode_t) minor() int {
	return fieldr(ind.iblk.data, ifield(ind.ioff, 4))
}

func (ind *inode_t) indirect() int {
	return fieldr(ind.iblk.data, ifield(ind.ioff, 5))
}

func (ind *inode_t) addr(i int) int {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	return fieldr(ind.iblk.data, ifield(ind.ioff, addroff + i))
}

func (ind *inode_t) w_itype(n int) {
	if n < I_FIRST || n > I_LAST {
		panic("weird inode type")
	}
	fieldw(ind.iblk.data, ifield(ind.ioff, 0), n)
}

func (ind *inode_t) w_linkcount(n int) {
	fieldw(ind.iblk.data, ifield(ind.ioff, 1), n)
}

func (ind *inode_t) w_size(n int) {
	fieldw(ind.iblk.data, ifield(ind.ioff, 2), n)
}

func (ind *inode_t) w_major(n int) {
	fieldw(ind.iblk.data, ifield(ind.ioff, 3), n)
}

func (ind *inode_t) w_minor(n int) {
	fieldw(ind.iblk.data, ifield(ind.ioff, 4), n)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *inode_t) w_indirect(blk int) {
	fieldw(ind.iblk.data, ifield(ind.ioff, 5), blk)
}

func (ind *inode_t) w_addr(i int, blk int) {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	fieldw(ind.iblk.data, ifield(ind.ioff, addroff + i), blk)
}

func print_inodes(b *bdev_block_t) {
	blkwords := BSIZE/8
	n := blkwords/NIWORDS
	for i := 0; i < n; i++ {
		inode := &inode_t{b, i}
		fmt.Printf("inode: %v %v %v %v\n", i, b.block, inode.itype(), biencode(b.block,i))
	}
}

// In-memory representation of an inode.
type imemnode_t struct {
	sync.Mutex
	priv		inum
	blkno		int
	ioff		int
	// cache of inode metadata
	icache		icache_t
	// XXXPANIC for sanity
	_amlocked	bool
}

func (idm *imemnode_t) key() int {
	return int(idm.priv)
}

func (idm *imemnode_t) evict() {
	idm._derelease()
	b, i :=  bidecode(idm.priv)
	if fs_debug {
		fmt.Printf("evict: %v (%v,%v)\n", idm.priv, b, i)
	}
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

// inode refcache
var irefcache	= make_refcache(syslimit.vnodes)


// obtain the reference for an inode
func iref(priv inum, s string) (*imemnode_t, err_t) {
	ref, err := irefcache.lookup(int(priv), s)
	defer ref.Unlock()

	if !ref.valid {
		if fs_debug {
			fmt.Printf("iref load %v %v\n", priv, s)
		}

		imem := &imemnode_t{}
		imem.priv = priv
		ref.obj = imem
		err := imem.idm_init(priv)
		if err != 0 {
			panic("idm_init")
			// delete(refcache.refs, priv)
			return nil, err
		}
		ref.valid = true
	}
	
	return ref.obj.(*imemnode_t), err
}

// obtained the reference for an inode with the inode locked
func iref_locked(priv inum, s string) (*imemnode_t, err_t) {
	idm, err := iref(priv, s)
	if err != 0 {
		return nil, err
	}
	idm.ilock(s)
	return idm, err
}


func iref_lockall(imems []*imemnode_t) []*imemnode_t {
	var locked []*imemnode_t
	sort.Slice(imems, func(i, j int) bool { return imems[i].key() < imems[j].key() })
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


// Fill in inode
func (idm *imemnode_t) idm_init(priv inum) err_t {
	blkno, ioff := bidecode(priv)
	idm.priv = priv
	idm.blkno = blkno
	idm.ioff = ioff

	// idm.pgcache.pgc_init(BSIZE, idm)

	if fs_debug {
		fmt.Printf("idm_init: readinode block %v(%v,%v)\n", priv, blkno, ioff)
	}
	blk, err := idm.idibread()
	if err != 0 {
		return err
	}
	// print_inodes(blk)
	idm.icache.fill(blk, ioff)
	blk.Unlock()
	bcache_relse(blk, "idm_init")
	return 0
}

func (idm *imemnode_t) iunlock_refdown(s string) {
	idm.iunlock(s)
	irefcache.refdown(idm, s)
}

func (idm *imemnode_t) _iupdate() err_t {
	iblk, err := idm.idibread()
	if err != 0 {
		return err
	}
	if idm.icache.flushto(iblk, idm.ioff) {
		iblk.Unlock()
		iblk.log_write()
	} else {
		iblk.Unlock()
	}
	bcache_relse(iblk, "_iupdate")
	return 0
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

func (idm *imemnode_t) do_mmapi(off, len int, inc bool) ([]mmapinfo_t, err_t) {
	if idm.icache.itype != I_FILE && idm.icache.itype != I_DIR {
		panic("bad mmapinfo")
	}
	return idm.immapinfo(off, len, inc)
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
	c, err := iref_locked(priv, "_fullpath")
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
		par, err := iref(pari, "do_fullpath_par")
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

// metadata block interface; only one inode touches these blocks at a time,
// thus no concurrency control
func (ic *icache_t) mbread(blockn int) (*bdev_block_t, err_t) {
	mb, err := bcache_get_fill(blockn, "mbread", false)
	return mb, err
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
	ensureb := func(blkno int) (int, bool, err_t) {
		if !writing || blkno != 0 {
			return blkno, false, 0
		}
		nblkno, err := balloc()
		return nblkno, true, err
	}
	// this function makes sure that every slot up to and including idx of
	// the given indirect block has a block allocated. it is necessary to
	// allocate a bunch of zero'd blocks for a file when someone lseek()s
	// past the end of the file and then writes.
	ensureind := func(blk *bdev_block_t, idx int) err_t {
		if !writing {
			return 0
		}
		added := false
		for i := 0; i <= idx; i++ {
			slot := i * 8
			s := blk.data[:]
			blkn := readn(s, 8, slot)
			blkn, isnew, err := ensureb(blkn)
			if err != 0 {
				return err
			}
			if isnew {
				writen(s, 8, slot, blkn)
				added = true
				// make sure to zero indirect pointer block if
				// allocated.  XXX balloc zeros.
				if idx == INDADDR {
					fmt.Printf("indirect indx\n")
				}
			}
		}
		if added {
			blk.log_write()
		}
		return 0
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
			tblkn, err := balloc()
			if err != 0 {
				return tblkn, err
			}
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
		indno, isnew, err := ensureb(indno)
		if err != 0 {
			return indno, err
		}
		if isnew {
			// new indirect block will be written to log below
			idm.icache.indir = indno
		}
		// follow indirect block chain
		indblk, err := idm.icache.mbread(indno)
		if err != 0 {
			return 0, err
		}
		for i := 0; i < indslot/slotpb; i++ {
			// make sure the indirect block has no empty spaces if
			// we are writing past the end of the file. if
			// necessary, ensureind also allocates the next
			// indirect pointer block.
			ensureind(indblk, slotpb)

			indno = readn(indblk.data[:], 8, nextindb)
			bcache_relse(indblk, "offsetblk1")
			indblk, err = idm.icache.mbread(indno)
			if err != 0 {
				return 0, err
			}
		}
		// finally get data block from indirect block
		slotoff := (indslot % slotpb)
		ensureind(indblk, slotoff)
		noff := (slotoff)*8
		s := indblk.data[:]
		blkn = readn(s, 8, noff)
		blkn, isnew, err = ensureb(blkn)
		if err != 0 {
			return blkn, err
		}
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

// Return locked buffer for offset
func (idm *imemnode_t) off2buf(offset int, len int, fillhole bool, s string) (*bdev_block_t, err_t) {
	if offset%PGSIZE  + len > PGSIZE {
		panic("off2buf")
	}
	blkno, err := idm.offsetblk(offset, fillhole)
	if err != 0 {
		return nil, err
	}
	b, err  := bcache_get_fill(blkno, s, true)
	if err != 0 {
		return nil, err
	}
	return b, 0
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}

func (idm *imemnode_t) iread(dst userio_i, offset int) (int, err_t) {
	isz := idm.icache.size
	c := 0
	for offset < isz && dst.remain() != 0 {
		m := min(BSIZE-offset%BSIZE, dst.remain())
		m = min(isz - offset, m)
		b, err := idm.off2buf(offset, m, false, "iread")
		if err != 0 {
			return c, err
		}
		s := offset%BSIZE
		src := b.data[s:s+m]

		if fs_debug {
			fmt.Printf("_iread c %v isz %v remain %v offset %v m %v s %v s+m %v\n",
				c, isz, dst.remain(), offset, m, s, s+m)
		}
		
		wrote, err := dst.uiowrite(src)
		c += wrote
		offset += wrote
		b.Unlock()
		bcache_relse(b, "_iread")
		if err != 0 {
			fmt.Printf("iread %v %v\n", idm.priv, err)
			return c, err
		}
	}
	fmt.Printf("iread1 %v\n", idm.priv)
	return c, 0
}

func (idm *imemnode_t) iwrite(src userio_i, offset int) (int, err_t) {
	sz := src.totalsz()
	newsz := offset + sz
	c := 0
	for c < sz {
		m := min(BSIZE-offset%BSIZE, sz -c)
		b, err := idm.off2buf(offset, m, true, "iwrite")
		if err != 0 {
			return c, err
		}
		s := offset%BSIZE

		if fs_debug {
			fmt.Printf("_iwrite c %v sz %v off %v m %v s %v s+m %v\n",
				c, sz, offset, m, s, s+m)
		}

		dst := b.data[s:s+m]
		read, err := src.uioread(dst)
		b.Unlock()
		b.log_write()
		bcache_relse(b, "_iwrite")
		if err != 0 {
			return c, err
		}
		c += read
		offset += read
	}
	wrote := c
	if newsz > idm.icache.size {
		idm.icache.size = newsz
	}
	return wrote, 0
}

func (idm *imemnode_t) itrunc(newlen uint) err_t {
	if newlen > uint(idm.icache.size) {
		// this will cause the hole to filled in with zero blocks which
		// are logged to disk
		_, err := idm.offsetblk(int(newlen), true)
		if err != 0 {
			return err
		}

	}
	// inode is flushed by do_itrunc
	idm.icache.size = int(newlen)
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
func (idm *imemnode_t) create_undo(childi inum, childn string) err_t {
	ci, err := idm.iunlink(childn)
	if err != 0 {
		panic("but insert just succeeded")
	}
	if ci != childi {
		panic("inconsistent")
	}
	bn, ioff := bidecode(childi)
	ib, err := bcache_get_fill(bn, "create_undo", true)
	if err != 0 {
		return err
	}
	ni := &inode_t{ib, ioff}
	_iallocundo(bn, ni, ib)
	ib.Unlock()
	bcache_relse(ib, "create_undo")
	return 0
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
	newbn, newioff, err := ialloc()
	if err != 0 {
		return 0, err
	}
	newiblk, err := bcache_get_fill(newbn, "icreate", true)
	if err != 0 {
		return 0, err
	}

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


func (idm *imemnode_t) immapinfo(offset, len int, inc bool) ([]mmapinfo_t, err_t) {

	fmt.Printf("immapinfo %v %v %v %v\n", idm.priv, offset, len, inc)
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
	o := rounddown(offset, PGSIZE)
	len = roundup(offset + len, PGSIZE) - o
	pgc := len / PGSIZE
	ret := make([]mmapinfo_t, pgc)
	for i := 0; i < len; i += PGSIZE {
		buf, err := idm.off2buf(o+i, PGSIZE, false, "immapinfo")
		if err != 0 {
			return nil, err
		}
		buf.Unlock()
		if inc {    // the VM system is going to use the page
			fmt.Printf("refup %#x\n", buf.pa)
			refup(buf.pa)
		}
		pgn := i / PGSIZE
		wpg := (*pg_t)(unsafe.Pointer(buf.data))
		ret[pgn].pg = wpg
		ret[pgn].phys = buf.pa
		// release buffer. the VM system will increase the page count.
		// XXX race? vm system may not do this for a while. maybe we
		// should increase page count here.
		bcache_relse(buf, "immapinfo")
	}
	return ret, 0
}

func (idm *imemnode_t) idibread() (*bdev_block_t, err_t) {
	return bcache_get_fill(idm.blkno, "idibread", true)
}

// frees all blocks occupied by idm
func (idm *imemnode_t) ifree() err_t {
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
		blk, err := idm.icache.mbread(indno)
		if err != 0 {
			return err
		}
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

	iblk, err := idm.idibread()
	if err != 0 {
		return err
	}
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
	return 0
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

