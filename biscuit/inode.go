package main

import "fmt"
import "sync"
import "unsafe"
import "sort"
import "strconv"

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
	ioff    int
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
	NIADDRS = 9
	// number of words in an inode
	NIWORDS = 7 + NIADDRS
	// number of address in indirect block
	INDADDR        = (BSIZE/8)
	ISIZE          = 128
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

func (ind *inode_t) dindirect() int {
	return fieldr(ind.iblk.data, ifield(ind.ioff, 6))
}

func (ind *inode_t) addr(i int) int {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 7
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

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *inode_t) w_dindirect(blk int) {
	fieldw(ind.iblk.data, ifield(ind.ioff, 6), blk)
}

func (ind *inode_t) w_addr(i int, blk int) {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 7
	fieldw(ind.iblk.data, ifield(ind.ioff, addroff + i), blk)
}

// In-memory representation of an inode.
type imemnode_t struct {
	sync.Mutex
	inum		inum_t
	// cache of inode metadata
	icache		icache_t
	// XXXPANIC for sanity
	_amlocked	bool
}

func (idm *imemnode_t) key() int {
	return int(idm.inum)
}

func (idm *imemnode_t) evict() {
	idm._derelease()
	if fs_debug {
		fmt.Printf("evict: %v\n", idm.inum)
	}
}

func (idm *imemnode_t) ilock(s string) {
	// if idm._amlocked {
	//	fmt.Printf("ilocked: warning %v %v\n", idm.inum, s)
	// }
	idm.Lock()
	if idm.inum < 0 {
		panic("negative inum")
	}
	idm._amlocked = true
	// fmt.Printf("ilock: acquired %v %s\n", idm.inum, s)
}

func (idm *imemnode_t) iunlock(s string) {
	// fmt.Printf("iunlock: release %v %v\n", idm.inum, s)
	if !idm._amlocked {
		panic("iunlock")
	}
	idm._amlocked = false
	idm.Unlock()
}

// inode refcache
var irefcache	= make_refcache(syslimit.vnodes)

func inode_stat() string {
	s := "icache: size "
	s += strconv.Itoa(len(irefcache.refs))
	s += " #evictions "
	s += strconv.Itoa(irefcache.nevict)
	s += " #live "
	s += strconv.Itoa(irefcache.nlive())
	s += "\n"
	return s
}

// obtain the reference for an inode
func iref(inum inum_t, s string) (*imemnode_t, err_t) {
	ref, err := irefcache.lookup(int(inum), s)
	if err != 0 {
		return nil, err
	}
	defer ref.Unlock()

	if !ref.valid {
		if fs_debug {
			fmt.Printf("iref load %v %v\n", inum, s)
		}

		imem := &imemnode_t{}
		imem.inum = inum
		ref.obj = imem
		err := imem.idm_init(inum)
		if err != 0 {
			panic("idm_init")
			// delete(refcache.refs, inum)
			return nil, err
		}
		ref.valid = true
	}
	
	return ref.obj.(*imemnode_t), err
}

// obtained the reference for an inode with the inode locked
func iref_locked(inum inum_t, s string) (*imemnode_t, err_t) {
	idm, err := iref(inum, s)
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
			if imem.inum == l.inum {
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
func (idm *imemnode_t) idm_init(inum inum_t) err_t {
	idm.inum = inum
	if fs_debug {
		fmt.Printf("idm_init: read inode %v\n", inum)
	}
	blk, err := idm.idibread()
	if err != 0 {
		return err
	}
	idm.icache.fill(blk, inum)
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
	if idm.icache.flushto(iblk, idm.inum) {
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
	st.wino(uint(idm.inum))
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

func (idm *imemnode_t) do_insert(fn string, n inum_t) err_t {
	// create new dir ent with given inode number
	err := idm.iinsert(fn, n)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_createnod(fn string, maj, min int) (inum_t, err_t) {
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DEV
	cnext, err := idm.icreate(fn, itype, maj, min)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createfile(fn string) (inum_t, err_t) {
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_FILE
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createdir(fn string) (inum_t, err_t) {
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DIR
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func _fullpath(inum inum_t) (string, err_t) {
	c, err := iref_locked(inum, "_fullpath")
	if err != 0 {
		return "", err
	}
	if c.icache.itype != I_DIR {
		panic("fullpath on non-dir")
	}

	// starting at the target file, walk up to the root prepending the name
	// (which is stored in the parent's directory entry)
	last := c.inum
	acc := ""
	for c.inum != iroot {
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
		last = par.inum
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
	dindir          int
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

func (ic *icache_t) fill(blk *bdev_block_t, inum inum_t) {
	inode := inode_t{blk, ioffset(inum)}
	ic.itype = inode.itype()
	if ic.itype <= I_FIRST || ic.itype > I_VALID {
		fmt.Printf("itype: %v for %v\n", ic.itype, inum)
		panic("bad itype in fill")
	}
	ic.links = inode.linkcount()
	ic.size  = inode.size()
	ic.major = inode.major()
	ic.minor = inode.minor()
	ic.indir = inode.indirect()
	ic.dindir = inode.dindirect()
	for i := 0; i < NIADDRS; i++ {
		ic.addrs[i] = inode.addr(i)
	}
}

// returns true if the inode data changed, and thus needs to be flushed to disk
func (ic *icache_t) flushto(blk *bdev_block_t, inum inum_t) bool {
	inode := inode_t{blk, ioffset(inum)}
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
	inode.w_dindirect(ic.dindir)
	for i := 0; i < NIADDRS; i++ {
		inode.w_addr(i, ic.addrs[i])
	}
	return ret
}

// ensure block exists
func (idm *imemnode_t) ensureb(blkno int, writing bool) (int, bool, err_t) {
	if !writing || blkno != 0 {
		return blkno, false, 0
	}
	nblkno, err := balloc()
	return nblkno, true, err
}

// ensure entry in indirect block exists
func (idm *imemnode_t) ensureind(blk *bdev_block_t, slot int, writing bool) (int, err_t) {
	off := slot * 8
	s := blk.data[:]
	blkn := readn(s, 8, off)
	blkn, isnew, err := idm.ensureb(blkn, writing)
	if err != 0 {
		return 0, err
	}
	if isnew {
		writen(s, 8, off, blkn)
		blk.log_write()
	}
	return blkn, 0
}

// Assumes that every block until b exits
func (idm *imemnode_t) fbn2block(fbn int, writing bool) (int, err_t) {
	if fbn < NIADDRS {
		if idm.icache.addrs[fbn] != 0 {
			return idm.icache.addrs[fbn], 0
		}
		blkn, err := balloc()
		if err != 0 {
			return 0, err
		}
		// imemnode_t.iupdate() will make sure the
		// icache is updated on disk
		idm.icache.addrs[fbn] = blkn
		return blkn, 0
	} else {
		fbn -= NIADDRS
		if fbn < INDADDR {
			indno := idm.icache.indir
			indno, isnew, err := idm.ensureb(indno, writing)
			if err != 0 {
				return 0, err
			}
			if isnew {
				// new indirect block will be written to log by iupdate()
				idm.icache.indir = indno
			}
			indblk, err := idm.icache.mbread(indno)
			if err != 0 {
				return 0, err
			}
			blkn, err := idm.ensureind(indblk, fbn, writing)
			bcache_relse(indblk, "indblk")
			return blkn, err
		} else if fbn < INDADDR * INDADDR {
			fbn -= INDADDR
			dindno := idm.icache.dindir
			dindno, isnew, err := idm.ensureb(dindno, writing)
			if err != 0 {
				return 0, err
			}
			if isnew {
				// new dindirect block will be written to log by iupdate()
				idm.icache.dindir = dindno
			}
			dindblk, err := idm.icache.mbread(dindno)
			if err != 0 {
				return 0, err
			}
			
			indno, err := idm.ensureind(dindblk, fbn/INDADDR, writing)
			bcache_relse(dindblk, "dindblk")

			indblk, err := idm.icache.mbread(indno)
			if err != 0 {
				return 0, err
			}
			blkn, err := idm.ensureind(indblk, fbn%INDADDR, writing)
			bcache_relse(indblk, "indblk2")
			return blkn, err
		} else {
			panic("too big fbn")
			return 0, 0

		}
	}
}

// make sure there are blocks from startblock through endblock. return blkn
// endblock.
func (idm *imemnode_t) bmapfill(lastblk int, whichblk int, writing bool) (int, err_t) {
	blkn := 0
	var err err_t

	if whichblk >= lastblk {   // a hole?
		// allocate the missing blocks
		for b := lastblk; b <= whichblk; b++ {
			// XXX we could remember where the last slot was
			blkn, err = idm.fbn2block(b, writing)
			if err != 0 {
				return blkn, err
			}
		}
	} else {
		blkn, err = idm.fbn2block(whichblk, writing)
		if err != 0 {
			return blkn, err
		}
	}
	return blkn, 0
}

// takes as input the file offset and whether the operation is a write and
// returns the block number of the block responsible for that offset.
func (idm *imemnode_t) offsetblk(offset int, writing bool) (int, err_t) {
	whichblk := offset/BSIZE
	lastblk := idm.icache.size/BSIZE
	blkn, err := idm.bmapfill(lastblk, whichblk, writing)
	if err != 0 {
		return blkn, err
	}
	if blkn <= 0 || blkn >= superb.lastblock() {
		panic("offsetblk: bad data blocks")
	}
	return blkn, 0
}

// Return locked buffer for offset
func (idm *imemnode_t) off2buf(offset int, len int, fillhole bool, fill bool, s string) (*bdev_block_t, err_t) {
	if offset%PGSIZE  + len > PGSIZE {
		panic("off2buf")
	}
	blkno, err := idm.offsetblk(offset, fillhole)
	if err != 0 {
		return nil, err
	}
	var b *bdev_block_t
	if fill {
		b, err = bcache_get_fill(blkno, s, true)
	} else {
		b, err = bcache_get_nofill(blkno, s, true)
        }
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
		b, err := idm.off2buf(offset, m, false, true, "iread")
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
			return c, err
		}
	}
	return c, 0
}

func (idm *imemnode_t) iwrite(src userio_i, offset int) (int, err_t) {
	sz := src.totalsz()
	newsz := offset + sz
	c := 0
	for c < sz {
		m := min(BSIZE-offset%BSIZE, sz -c)
		fill := m != BSIZE
		b, err := idm.off2buf(offset, m, true, fill, "iwrite")
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
func (idm *imemnode_t) create_undo(childi inum_t, childn string) err_t {
	ci, err := idm.iunlink(childn)
	if err != 0 {
		panic("but insert just succeeded")
	}
	if ci != childi {
		panic("inconsistent")
	}
	ib, err := bcache_get_fill(iblock(childi), "create_undo", true)
	if err != 0 {
		return err
	}
	ni := &inode_t{ib, ioffset(childi)}
	_iallocundo(iblock(childi), ni, ib)
	ib.Unlock()
	bcache_relse(ib, "create_undo")
	return 0
}

func (idm *imemnode_t) icreate(name string, nitype, major, minor int) (inum_t, err_t) {
	
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
		return de.inum, -EEXIST
	}

	// allocate new inode
	newinum, err := ialloc()
	newbn := iblock(newinum)
	newioff := ioffset(newinum)
	if err != 0 {
		return 0, err
	}
	newiblk, err := bcache_get_fill(newbn, "icreate", true)
	if err != 0 {
		return 0, err
	}

	if fs_debug {
		fmt.Printf("ialloc: %v %v %v\n", newbn, newioff, newinum)
	}
	
	newinode := &inode_t{newiblk, newioff}
	newinode.w_itype(nitype)
	newinode.w_linkcount(1)
	newinode.w_size(0)
	newinode.w_major(major)
	newinode.w_minor(minor)
	newinode.w_indirect(0)
	newinode.w_dindirect(0)
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
	err = idm._deinsert(name, newinum)
	if err != 0 {
		_iallocundo(newbn, newinode, newiblk)
	}
	newiblk.log_write()
	bcache_relse(newiblk, "icreate")

	return newinum, err
}


func (idm *imemnode_t) immapinfo(offset, len int, inc bool) ([]mmapinfo_t, err_t) {
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
		buf, err := idm.off2buf(o+i, PGSIZE, false, true, "immapinfo")
		if err != 0 {
			return nil, err
		}
		buf.Unlock()
		if inc {    // the VM system is going to use the page
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
	return bcache_get_fill(iblock(idm.inum), "idibread", true)
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
	idm.icache.flushto(iblk, idm.inum)
	if _alldead(iblk) {
		add(iblock(idm.inum))
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


// Inode allocator

type iallocater_t struct {
	alloc *allocater_t
	first int
}
var iallocater  *iallocater_t

func ialloc_init(start, len, first int) {
	iallocater = &iallocater_t{}
	iallocater.alloc = make_allocater(start, len)
	iallocater.first = first
}

func ialloc() (inum_t, err_t) {
	n, err := iallocater.alloc.alloc()
	inum := inum_t(n)
	if err != 0 {
		return 0, err
	}
	if fs_debug {
		fmt.Printf("ialloc %d\n", inum)
	}
	return inum, 0
}

func ifree(inum inum_t) err_t {
	return iallocater.alloc.free(int(inum))
}

func iblock(inum inum_t) int {
	b := int(inum) / (BSIZE / ISIZE)
	b += iallocater.first
        return b
}

func ioffset(inum inum_t) int {
	o := int(inum) % (BSIZE / ISIZE)
        return o
}
