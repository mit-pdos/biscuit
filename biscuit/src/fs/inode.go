package fs

import "fmt"
import "sync"
import "unsafe"
import "sort"
import "strconv"
import "reflect"
import "sync/atomic"

import "common"

type counter_t int64

type inode_stats_t struct {
	Nopen     counter_t
	Nnamei    counter_t
	Nifill    counter_t
	Niupdate  counter_t
	Nistat    counter_t
	Niread    counter_t
	Niwrite   counter_t
	Ndo_write counter_t
	Nfillhole counter_t
	Ngrow     counter_t
	Nitrunc   counter_t
	Nimmap    counter_t
	Nifree    counter_t
	Nicreate  counter_t
	Nilink    counter_t
	Nunlink   counter_t
	Nrename   counter_t
	Nlseek    counter_t
	Nmkdir    counter_t
	Nclose    counter_t
	Nsync     counter_t
	Nreopen   counter_t
}

func (c *counter_t) inc() {
	n := (*int64)(unsafe.Pointer(c))
	atomic.AddInt64(n, 1)
}

func (is *inode_stats_t) stats() string {
	v := reflect.ValueOf(*is)
	s := "inode:"
	for i := 0; i < v.NumField(); i++ {
		n := v.Field(i).Interface().(counter_t)
		s += "\n\t#" + v.Type().Field(i).Name + ": " + strconv.FormatInt(int64(n), 10)
	}
	return s + "\n"
}

type Inode_t struct {
	Iblk *common.Bdev_block_t
	Ioff int
}

// inode file types
const (
	I_FIRST   = 0
	I_INVALID = I_FIRST
	I_FILE    = 1
	I_DIR     = 2
	I_DEV     = 3
	I_VALID   = I_DEV
	// ready to be reclaimed
	I_DEAD = 4
	I_LAST = I_DEAD

	// direct block addresses
	NIADDRS = 9
	// number of words in an inode
	NIWORDS = 7 + NIADDRS
	// number of address in indirect block
	INDADDR = (common.BSIZE / 8)
	ISIZE   = 128
)

func ifield(iidx int, fieldn int) int {
	return iidx*NIWORDS + fieldn
}

// iidx is the inode index; necessary since there are four inodes in one block
func (ind *Inode_t) itype() int {
	it := fieldr(ind.Iblk.Data, ifield(ind.Ioff, 0))
	if it < I_FIRST || it > I_LAST {
		panic(fmt.Sprintf("weird inode type %d", it))
	}
	return it
}

func (ind *Inode_t) linkcount() int {
	return fieldr(ind.Iblk.Data, ifield(ind.Ioff, 1))
}

func (ind *Inode_t) size() int {
	return fieldr(ind.Iblk.Data, ifield(ind.Ioff, 2))
}

func (ind *Inode_t) major() int {
	return fieldr(ind.Iblk.Data, ifield(ind.Ioff, 3))
}

func (ind *Inode_t) minor() int {
	return fieldr(ind.Iblk.Data, ifield(ind.Ioff, 4))
}

func (ind *Inode_t) indirect() int {
	return fieldr(ind.Iblk.Data, ifield(ind.Ioff, 5))
}

func (ind *Inode_t) dindirect() int {
	return fieldr(ind.Iblk.Data, ifield(ind.Ioff, 6))
}

func (ind *Inode_t) addr(i int) int {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 7
	return fieldr(ind.Iblk.Data, ifield(ind.Ioff, addroff+i))
}

func (ind *Inode_t) W_itype(n int) {
	if n < I_FIRST || n > I_LAST {
		panic("weird inode type")
	}
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, 0), n)
}

func (ind *Inode_t) W_linkcount(n int) {
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, 1), n)
}

func (ind *Inode_t) W_size(n int) {
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, 2), n)
}

func (ind *Inode_t) w_major(n int) {
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, 3), n)
}

func (ind *Inode_t) w_minor(n int) {
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, 4), n)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *Inode_t) w_indirect(blk int) {
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, 5), blk)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *Inode_t) w_dindirect(blk int) {
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, 6), blk)
}

func (ind *Inode_t) W_addr(i int, blk int) {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 7
	fieldw(ind.Iblk.Data, ifield(ind.Ioff, addroff+i), blk)
}

// In-memory representation of an inode.
type imemnode_t struct {
	sync.Mutex
	inum common.Inum_t
	fs   *Fs_t

	// XXXPANIC for sanity
	_amlocked bool

	itype  int
	links  int
	size   int
	major  int
	minor  int
	indir  int
	dindir int
	addrs  [NIADDRS]int
	// inode specific metadata blocks
	dentc struct {
		// true iff all non-empty directory entries are cached, thus
		// there is no need to check the disk to lookup a name
		haveall bool
		// true iff all free directory entries are on the free list.
		scanned bool
		// maximum number of cached dents this icache is allowed to
		// have
		max   int
		dents dc_rbh_t
		freel fdelist_t
	}
}

func (idm *imemnode_t) Key() int {
	return int(idm.inum)
}

// No other thread should have a reference to this inode, because its refcnt = 0
// and it was removed from refcache.  Same for Free()
func (idm *imemnode_t) Evict() {
	idm._derelease()
	if fs_debug {
		fmt.Printf("evict: %v\n", idm.inum)
	}
}

func (idm *imemnode_t) Free() {
	// no need to lock...
	idm.Evict()
	if idm.links == 0 {
		idm.ifree()
	}
}

func (idm *imemnode_t) Evictnow() bool {
	idm.Lock()
	r := idm.links == 0
	idm.Unlock()
	return r
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
	//fmt.Printf("ilock: acquired %v %s\n", idm.inum, s)
}

func (idm *imemnode_t) iunlock(s string) {
	//fmt.Printf("iunlock: release %v %v\n", idm.inum, s)
	if !idm._amlocked {
		panic("iunlock:" + s)
	}
	idm._amlocked = false
	idm.Unlock()
}

// Fill in inode
func (idm *imemnode_t) idm_init(inum common.Inum_t) common.Err_t {
	idm.inum = inum
	if fs_debug {
		fmt.Printf("idm_init: read inode %v\n", inum)
	}
	blk, err := idm.idibread()
	if err != 0 {
		return err
	}
	idm.fs.istats.Nifill.inc()
	idm.fill(blk, inum)
	blk.Unlock()
	idm.fs.fslog.Relse(blk, "idm_init")
	return 0
}

func (idm *imemnode_t) iunlock_refdown(s string) {
	idm.iunlock(s)
	idm.fs.icache.Refdown(idm, s)
}

func (idm *imemnode_t) refdown(s string) {
	idm.fs.icache.Refdown(idm, s)
}

func (idm *imemnode_t) _iupdate() common.Err_t {
	idm.fs.istats.Niupdate.inc()
	iblk, err := idm.idibread()
	if err != 0 {
		return err
	}
	if idm.flushto(iblk, idm.inum) {
		iblk.Unlock()
		idm.fs.fslog.Write(iblk)
	} else {
		iblk.Unlock()
	}
	idm.fs.fslog.Relse(iblk, "_iupdate")
	return 0
}

func (idm *imemnode_t) do_trunc(truncto uint) common.Err_t {
	if idm.itype != I_FILE && idm.itype != I_DEV {
		panic("bad truncate")
	}
	err := idm.itrunc(truncto)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_read(dst common.Userio_i, offset int) (int, common.Err_t) {
	return idm.iread(dst, offset)
}

func (idm *imemnode_t) do_write(src common.Userio_i, _offset int, append bool) (int, common.Err_t) {
	if idm.itype == I_DIR {
		panic("write to dir")
	}
	offset := _offset
	if append {
		offset = idm.size
	}

	// break write system calls into one or more calls with no more than
	// maxblkpersys blocks per call.
	max := (MaxBlkPerOp - 1) * common.BSIZE
	sz := src.Totalsz()
	i := 0

	if fs_debug {
		fmt.Printf("dowrite: %v %v %v\n", idm.inum, offset, sz)
	}

	idm.fs.istats.Ndo_write.inc()
	for i < sz {
		gimme := common.Bounds(common.B_IMEMNODE_T_DO_WRITE)
		if !common.Resadd_noblock(gimme) {
			return i, -common.ENOHEAP
		}

		n := sz - i
		if n > max {
			n = max
		}
		idm.fs.fslog.Op_begin("dowrite")
		idm.ilock("iwrite")
		wrote, err := idm.iwrite(src, offset+i, n)
		idm._iupdate()
		idm.iunlock("iwrite")
		idm.fs.fslog.Op_end()

		if err != 0 {
			return i, err
		}

		i += wrote
	}
	return i, 0
}

func (idm *imemnode_t) do_stat(st *common.Stat_t) common.Err_t {
	idm.fs.istats.Nistat.inc()
	st.Wdev(0)
	st.Wino(uint(idm.inum))
	st.Wmode(idm.mkmode())
	st.Wsize(uint(idm.size))
	st.Wrdev(common.Mkdev(idm.major, idm.minor))
	return 0
}

func (idm *imemnode_t) do_mmapi(off, len int, inc bool) ([]common.Mmapinfo_t, common.Err_t) {
	if idm.itype != I_FILE && idm.itype != I_DIR {
		panic("bad mmapinfo")
	}
	return idm.immapinfo(off, len, inc)
}

func (idm *imemnode_t) do_dirchk(wantdir bool) common.Err_t {
	amdir := idm.itype == I_DIR
	if wantdir && !amdir {
		return -common.ENOTDIR
	} else if !wantdir && amdir {
		return -common.EISDIR
	} else if amdir {
		amempty, err := idm.idirempty()
		if err != 0 {
			return err
		}
		if !amempty {
			return -common.ENOTEMPTY
		}
	}
	return 0
}

// unlink cannot encounter OOM since directory page caches are eagerly loaded.
func (idm *imemnode_t) do_unlink(name string) common.Err_t {
	_, err := idm.iunlink(name)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

// create new dir ent with given inode number
func (idm *imemnode_t) do_insert(fn string, n common.Inum_t) common.Err_t {
	err := idm.iinsert(fn, n)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_createnod(fn string, maj, min int) (common.Inum_t, common.Err_t) {
	if idm.itype != I_DIR {
		return 0, -common.ENOTDIR
	}

	itype := I_DEV
	cnext, err := idm.icreate(fn, itype, maj, min)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createfile(fn string) (common.Inum_t, common.Err_t) {
	if idm.itype != I_DIR {
		return 0, -common.ENOTDIR
	}

	itype := I_FILE
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createdir(fn string) (common.Inum_t, common.Err_t) {
	if idm.itype != I_DIR {
		return 0, -common.ENOTDIR
	}

	itype := I_DIR
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (fs *Fs_t) _fullpath(inum common.Inum_t) (string, common.Err_t) {
	c, err := fs.icache.Iref_locked(inum, "_fullpath")
	if err != 0 {
		return "", err
	}
	if c.itype != I_DIR {
		panic("fullpath on non-dir")
	}

	// starting at the target file, walk up to the root prepending the name
	// (which is stored in the parent's directory entry)
	last := c.inum
	acc := ""
	for c.inum != iroot {
		gimme := common.Bounds(common.B_FS_T__FULLPATH)
		if !common.Resadd_noblock(gimme) {
			c.iunlock("do_fullpath_c")
			return "", -common.ENOHEAP
		}
		pari, err := c.ilookup("..")
		if err != 0 {
			c.iunlock("do_fullpath_c")
			return "", err
		}
		par, err := fs.icache.Iref(pari, "do_fullpath_par")
		c.iunlock("do_fullpath_c")
		if err != 0 {
			return "", err
		}
		par.ilock("do_fullpath_par")
		name, err := par._denamefor(last)
		if err != 0 {
			if err == -common.ENOENT {
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

// caller holds lock on idm
func (idm *imemnode_t) _linkdown() {
	idm.links--
	if idm.links <= 0 {
		idm.fs.icache.markOrphan(idm.inum)
	}
	idm._iupdate()
}

func (idm *imemnode_t) _linkup() {
	idm.links++
	idm._iupdate()
}

// metadata block interface; only one inode touches these blocks at a time,
// thus no concurrency control
func (ic *imemnode_t) mbread(blockn int) (*common.Bdev_block_t, common.Err_t) {
	mb, err := ic.fs.fslog.Get_fill(blockn, "mbread", false)
	return mb, err
}

func (ic *imemnode_t) fill(blk *common.Bdev_block_t, inum common.Inum_t) {
	inode := Inode_t{blk, ioffset(inum)}
	ic.itype = inode.itype()
	if ic.itype <= I_FIRST || ic.itype > I_VALID {
		fmt.Printf("itype: %v for %v\n", ic.itype, inum)
	}
	ic.links = inode.linkcount()
	ic.size = inode.size()
	ic.major = inode.major()
	ic.minor = inode.minor()
	ic.indir = inode.indirect()
	ic.dindir = inode.dindirect()
	for i := 0; i < NIADDRS; i++ {
		ic.addrs[i] = inode.addr(i)
	}
}

// returns true if the inode data changed, and thus needs to be flushed to disk
func (ic *imemnode_t) flushto(blk *common.Bdev_block_t, inum common.Inum_t) bool {
	inode := Inode_t{blk, ioffset(inum)}
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
	inode.W_itype(ic.itype)
	inode.W_linkcount(ic.links)
	inode.W_size(ic.size)
	inode.w_major(ic.major)
	inode.w_minor(ic.minor)
	inode.w_indirect(ic.indir)
	inode.w_dindirect(ic.dindir)
	for i := 0; i < NIADDRS; i++ {
		inode.W_addr(i, ic.addrs[i])
	}
	return ret
}

// ensure block exists
func (idm *imemnode_t) ensureb(blkno int, writing bool) (int, bool, common.Err_t) {
	if !writing || blkno != 0 {
		return blkno, false, 0
	}
	nblkno, err := idm.fs.balloc.Balloc()
	return nblkno, true, err
}

// ensure entry in indirect block exists
func (idm *imemnode_t) ensureind(blk *common.Bdev_block_t, slot int, writing bool) (int, common.Err_t) {
	off := slot * 8
	s := blk.Data[:]
	blkn := common.Readn(s, 8, off)
	blkn, isnew, err := idm.ensureb(blkn, writing)
	if err != 0 {
		return 0, err
	}
	if isnew {
		common.Writen(s, 8, off, blkn)
		idm.fs.fslog.Write(blk)
	}
	return blkn, 0
}

// Assumes that every block until b exits
// XXX change to wrap blockiter_t instead
func (idm *imemnode_t) fbn2block(fbn int, writing bool) (int, common.Err_t) {
	if fbn < NIADDRS {
		if idm.addrs[fbn] != 0 {
			return idm.addrs[fbn], 0
		}
		blkn, err := idm.fs.balloc.Balloc()
		if err != 0 {
			return 0, err
		}
		// imemnode_t.iupdate() will make sure the
		// icache is updated on disk
		idm.addrs[fbn] = blkn
		return blkn, 0
	} else {
		fbn -= NIADDRS
		if fbn < INDADDR {
			indno := idm.indir
			indno, isnew, err := idm.ensureb(indno, writing)
			if err != 0 {
				return 0, err
			}
			if isnew {
				// new indirect block will be written to log by iupdate()
				idm.indir = indno
			}
			indblk, err := idm.mbread(indno)
			if err != 0 {
				return 0, err
			}
			blkn, err := idm.ensureind(indblk, fbn, writing)
			idm.fs.fslog.Relse(indblk, "indblk")
			return blkn, err
		} else if fbn < INDADDR*INDADDR {
			fbn -= INDADDR
			dindno := idm.dindir
			dindno, isnew, err := idm.ensureb(dindno, writing)
			if err != 0 {
				return 0, err
			}
			if isnew {
				// new dindirect block will be written to log by iupdate()
				idm.dindir = dindno
			}
			dindblk, err := idm.mbread(dindno)
			if err != 0 {
				return 0, err
			}

			indno, err := idm.ensureind(dindblk, fbn/INDADDR, writing)
			idm.fs.fslog.Relse(dindblk, "dindblk")

			indblk, err := idm.mbread(indno)
			if err != 0 {
				return 0, err
			}
			blkn, err := idm.ensureind(indblk, fbn%INDADDR, writing)
			idm.fs.fslog.Relse(indblk, "indblk2")
			return blkn, err
		} else {
			panic("too big fbn")
			return 0, 0

		}
	}
}

// Allocates blocks from startblock through endblock. Returns blkn of endblock.
func (idm *imemnode_t) bmapfill(lastblk int, whichblk int, writing bool) (int, common.Err_t) {
	blkn := 0
	var err common.Err_t

	if whichblk >= lastblk { // a hole?
		// allocate the missing blocks
		if whichblk > lastblk && writing {
			idm.fs.istats.Nfillhole.inc()
		} else if writing {
			idm.fs.istats.Ngrow.inc()
		}
		for b := lastblk; b <= whichblk; b++ {
			gimme := common.Bounds(common.B_IMEMNODE_T_BMAPFILL)
			if !common.Resadd_noblock(gimme) {
				return 0, -common.ENOHEAP
			}
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

// Takes as input the file offset and whether the operation is a write and
// returns the block number of the block responsible for that offset.
func (idm *imemnode_t) offsetblk(offset int, writing bool) (int, common.Err_t) {
	whichblk := offset / common.BSIZE
	lastblk := idm.size / common.BSIZE
	blkn, err := idm.bmapfill(lastblk, whichblk, writing)
	if err != 0 {
		return blkn, err
	}
	if blkn <= 0 || blkn >= idm.fs.superb.Lastblock() {
		panic("offsetblk: bad data blocks")
	}
	return blkn, 0
}

// Return locked buffer for offset
func (idm *imemnode_t) off2buf(offset int, len int, fillhole bool, fill bool, s string) (*common.Bdev_block_t, common.Err_t) {
	if offset%common.PGSIZE+len > common.PGSIZE {
		panic("off2buf")
	}
	blkno, err := idm.offsetblk(offset, fillhole)
	if err != 0 {
		return nil, err
	}
	var b *common.Bdev_block_t
	if fill {
		b, err = idm.fs.fslog.Get_fill(blkno, s, true)
	} else {
		b, err = idm.fs.fslog.Get_nofill(blkno, s, true)
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

func (idm *imemnode_t) iread(dst common.Userio_i, offset int) (int, common.Err_t) {
	idm.fs.istats.Niread.inc()
	isz := idm.size
	c := 0
	gimme := common.Bounds(common.B_IMEMNODE_T_IREAD)
	for offset < isz && dst.Remain() != 0 {
		if !common.Resadd_noblock(gimme) {
			return c, -common.ENOHEAP
		}
		m := min(common.BSIZE-offset%common.BSIZE, dst.Remain())
		m = min(isz-offset, m)
		b, err := idm.off2buf(offset, m, false, true, "iread")
		if err != 0 {
			return c, err
		}
		s := offset % common.BSIZE
		src := b.Data[s : s+m]

		if fs_debug {
			fmt.Printf("_iread c %v isz %v remain %v offset %v m %v s %v s+m %v\n",
				c, isz, dst.Remain(), offset, m, s, s+m)
		}

		wrote, err := dst.Uiowrite(src)
		c += wrote
		offset += wrote
		b.Unlock()
		idm.fs.fslog.Relse(b, "_iread")
		if err != 0 {
			return c, err
		}
	}
	return c, 0
}

func (idm *imemnode_t) iwrite(src common.Userio_i, offset int, n int) (int, common.Err_t) {
	idm.fs.istats.Niwrite.inc()
	sz := min(src.Totalsz(), n)
	newsz := offset + sz
	c := 0
	gimme := common.Bounds(common.B_IMEMNODE_T_IWRITE)
	for c < sz {
		if !common.Resadd_noblock(gimme) {
			return c, -common.ENOHEAP
		}
		m := min(common.BSIZE-offset%common.BSIZE, sz-c)
		fill := m != common.BSIZE
		b, err := idm.off2buf(offset, m, true, fill, "iwrite")
		if err != 0 {
			return c, err
		}
		s := offset % common.BSIZE

		if fs_debug {
			fmt.Printf("_iwrite c %v sz %v off %v m %v s %v s+m %v\n",
				c, sz, offset, m, s, s+m)
		}

		dst := b.Data[s : s+m]
		read, err := src.Uioread(dst)
		b.Unlock()
		idm.fs.fslog.Write_ordered(b)
		idm.fs.fslog.Relse(b, "iwrite")
		if err != 0 {
			return c, err
		}
		c += read
		offset += read
	}
	wrote := c
	if newsz > idm.size {
		idm.size = newsz
	}
	return wrote, 0
}

func (idm *imemnode_t) itrunc(newlen uint) common.Err_t {
	if newlen > uint(idm.size) {
		// this will cause the hole to filled in with zero blocks which
		// are logged to disk
		_, err := idm.offsetblk(int(newlen), true)
		if err != 0 {
			return err
		}

	}
	idm.fs.istats.Nitrunc.inc()
	// inode is flushed by do_itrunc
	idm.size = int(newlen)
	return 0
}

// reverts icreate(). called after failure to allocate that prevents an FS
// operation from continuing.
func (idm *imemnode_t) create_undo(childi common.Inum_t, childn string) common.Err_t {
	ci, err := idm.iunlink(childn)
	if err != 0 {
		panic("but insert just succeeded")
	}
	if ci != childi {
		panic("inconsistent")
	}
	ib, err := idm.fs.fslog.Get_fill(idm.fs.ialloc.Iblock(childi), "create_undo", true)
	if err != 0 {
		return err
	}
	ni := &Inode_t{ib, ioffset(childi)}
	ni.W_itype(I_DEAD)
	ib.Unlock()
	idm.fs.fslog.Relse(ib, "create_undo")
	idm.fs.ialloc.Ifree(childi)
	return 0
}

func (idm *imemnode_t) icreate(name string, nitype, major, minor int) (common.Inum_t, common.Err_t) {

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
		return de.inum, -common.EEXIST
	}

	idm.fs.istats.Nicreate.inc()

	// allocate new inode
	newinum, err := idm.fs.ialloc.Ialloc()
	newbn := idm.fs.ialloc.Iblock(newinum)
	newioff := ioffset(newinum)
	if err != 0 {
		return 0, err
	}
	newiblk, err := idm.fs.fslog.Get_fill(newbn, "icreate", true)
	if err != 0 {
		return 0, err
	}

	if fs_debug {
		fmt.Printf("ialloc: %v %v %v\n", newbn, newioff, newinum)
	}

	newinode := &Inode_t{newiblk, newioff}
	newinode.W_itype(nitype)
	newinode.W_linkcount(1)
	newinode.W_size(0)
	newinode.w_major(major)
	newinode.w_minor(minor)
	newinode.w_indirect(0)
	newinode.w_dindirect(0)
	for i := 0; i < NIADDRS; i++ {
		newinode.W_addr(i, 0)
	}

	// write new directory entry referencing newinode
	err = idm._deinsert(name, newinum)
	if err != 0 {
		newinode.W_itype(I_DEAD)
		idm.fs.ialloc.Ifree(newinum)
	}
	newiblk.Unlock()
	idm.fs.fslog.Write(newiblk)
	idm.fs.fslog.Relse(newiblk, "icreate")
	return newinum, err
}

func (idm *imemnode_t) immapinfo(offset, len int, mapshared bool) ([]common.Mmapinfo_t, common.Err_t) {
	isz := idm.size
	if (len != -1 && len < 0) || offset < 0 {
		panic("bad off/len")
	}
	if offset >= isz {
		return nil, -common.EINVAL
	}
	if len == -1 || offset+len > isz {
		len = isz - offset
	}

	idm.fs.istats.Nimmap.inc()
	o := common.Rounddown(offset, common.PGSIZE)
	len = common.Roundup(offset+len, common.PGSIZE) - o
	pgc := len / common.PGSIZE
	ret := make([]common.Mmapinfo_t, pgc)
	for i := 0; i < len; i += common.PGSIZE {
		gimme := common.Bounds(common.B_IMEMNODE_T_IMMAPINFO)
		if !common.Resadd_noblock(gimme) {
			return nil, -common.ENOHEAP
		}
		buf, err := idm.off2buf(o+i, common.PGSIZE, false, true, "immapinfo")
		if err != 0 {
			return nil, err
		}
		buf.Unlock()

		// the VM system is going to use the page
		buf.Mem.Refup(buf.Pa)

		// the VM system will map this block writable; make sure the
		// block isn't evicted to ensure that future read(2)s can
		// observe writes through the mapping.
		if mapshared {
			idm.fs.bcache.pin(buf)
		}

		pgn := i / common.PGSIZE
		wpg := (*common.Pg_t)(unsafe.Pointer(buf.Data))
		ret[pgn].Pg = wpg
		ret[pgn].Phys = buf.Pa
		idm.fs.fslog.Relse(buf, "immapinfo")
	}
	return ret, 0
}

func (idm *imemnode_t) idibread() (*common.Bdev_block_t, common.Err_t) {
	return idm.fs.fslog.Get_fill(idm.fs.ialloc.Iblock(idm.inum), "idibread", true)
}

// a type to iterate over the data and indirect blocks of an imemnode_t without
// re-reading and re-locking indirect blocks. it may simultaneously hold
// references to at most two blocks until blockiter_t.release() is called.
type blockiter_t struct {
	idm   *imemnode_t
	which int
	tryevict bool
	dub   *common.Bdev_block_t
	lasti *common.Bdev_block_t
}

func (bl *blockiter_t) bi_init(idm *imemnode_t, tryevict bool) {
	var zbl blockiter_t
	*bl = zbl
	bl.idm = idm
	bl.tryevict = tryevict
}

// if this imemnode_t has a double-indirect block, _isdub loads it and caches
// it and returns true.
func (bl *blockiter_t) _isdub() (*common.Bdev_block_t, bool) {
	if bl.dub != nil {
		return bl.dub, true
	}
	blkno := bl.idm.dindir
	if blkno == 0 {
		return nil, false
	}
	var err common.Err_t
	bl.dub, err = bl.idm.mbread(blkno)
	if err != 0 {
		panic("must reserve")
	}
	return bl.dub, true
}

// returns the indirect block or block number from the given slot in the
// indirect block
func (bl *blockiter_t) _isdubind(dubslot int, fetch bool) (*common.Bdev_block_t, int, bool) {
	dub, ok := bl._isdub()
	if !ok {
		return nil, 0, false
	}

	blkno := common.Readn(dub.Data[:], 8, dubslot*8)
	var ret *common.Bdev_block_t
	ok = blkno != 0
	if fetch {
		ret, ok = bl._isind(blkno)
	}
	return ret, blkno, ok
}

func (bl *blockiter_t) _isind(blkno int) (*common.Bdev_block_t, bool) {
	if blkno == 0 {
		return nil, false
	}
	if bl.lasti != nil {
		if bl.lasti.Block == blkno {
			return bl.lasti, true
		}
		bl.idm.fs.fslog.Relse(bl.lasti, "release")
	}
	var err common.Err_t
	bl.lasti, err = bl.idm.mbread(blkno)
	if err != 0 {
		panic("must reserve")
	}
	if bl.tryevict {
		bl.lasti.Tryevict()
	}
	return bl.lasti, true
}

func (bl *blockiter_t) release() {
	if bl.dub != nil {
		bl.idm.fs.fslog.Relse(bl.dub, "release")
		bl.dub = nil
	}
	if bl.lasti != nil {
		bl.idm.fs.fslog.Relse(bl.lasti, "release")
		bl.lasti = nil
	}
}

// returns block number and the next slot to check for the given slot.
func (bl *blockiter_t) next1(which int) (int, int) {
	const DBLOCKS = NIADDRS + INDADDR + INDADDR*INDADDR
	const INDBLOCKS = DBLOCKS + INDADDR
	const IMD1 = INDBLOCKS
	const IMD2 = INDBLOCKS + 1
	const ALL = INDBLOCKS + 2

	if which >= ALL {
		panic("none left")
	}

	ret := -1
	w := which
	if w < DBLOCKS {
		if w < NIADDRS {
			blkno := bl.idm.addrs[w]
			if blkno == 0 {
				return -1, DBLOCKS
			}
			ret = blkno
		} else if w < NIADDRS+INDADDR {
			w -= NIADDRS
			blkno := 0
			single, ok := bl._isind(bl.idm.indir)
			if ok {
				blkno = common.Readn(single.Data[:], 8, w*8)
			}
			if !ok || blkno == 0 {
				return -1, DBLOCKS
			}
			ret = blkno
		} else {
			w -= NIADDRS + INDADDR
			dslot := w / INDADDR
			islot := w % INDADDR
			blkno := 0
			single, _, ok := bl._isdubind(dslot, true)
			if ok {
				blkno = common.Readn(single.Data[:], 8, islot*8)
			}
			if !ok || blkno == 0 {
				return -1, DBLOCKS
			}
			ret = blkno
		}
	} else if w < INDBLOCKS {
		w -= DBLOCKS
		dslot := w % INDADDR
		_, sblkno, ok := bl._isdubind(dslot, false)
		if !ok || sblkno == 0 {
			return -1, IMD1
		}
		ret = sblkno
	} else if w < ALL {
		switch w {
		default:
			panic("huh?")
		case IMD1:
			if bl.idm.indir == 0 {
				return -1, IMD2
			}
			ret = bl.idm.indir
		case IMD2:
			if bl.idm.dindir == 0 {
				return -1, ALL
			}
			ret = bl.idm.dindir
		}
	} else {
		panic("bad which")
	}
	which++
	return ret, which
}

// returns the next block to free, whether the block is valid, the next slot to
// check, and whether any more blocks remain (so the caller can avoid acquiring
// log admission spuriously).
func (bl *blockiter_t) next(which int) (int, bool, int, bool) {
	const DBLOCKS = NIADDRS + INDADDR + INDADDR*INDADDR
	const INDBLOCKS = DBLOCKS + INDADDR
	const ALL = INDBLOCKS + 2

	ret := -1
	for ret == -1 && which != ALL {
		ret, which = bl.next1(which)
	}
	ok := ret != -1
	remains := false
	for ok && which != ALL {
		d, next := bl.next1(which)
		if d != -1 {
			remains = true
			break
		} else if next == ALL {
			break
		}
		which = next
	}
	return ret, ok, which, remains
}

// free an orphaned inode
func (idm *imemnode_t) ifree() common.Err_t {
	idm.fs.istats.Nifree.inc()
	if fs_debug {
		fmt.Printf("ifree: %d\n", idm.inum)
	}

	// the imemnode_t.major field has a different meaning once a file's
	// link count reaches 0: it becomes a logical index of which (data and
	// indirect) blocks of a file have been freed. specifically, blocks in
	// the range [0, major) have been freed. major refers to a data block
	// when:
	// 	major < DBLOCKS
	// where DBLOCKS is NIADDRS+INDADDR+INDADDR*INDADDR, the indirect
	// blocks referred to by the double-indirect block when
	// 	DBLOCKS <= major < DBLOCKS + INDADDR, and the
	// indirect/double-indirect itself when:
	//	DBLOCKS+INADDR <= major DBLOCKS+INADDR+2

	var ca common.Cacheallocs_t
	gimme := common.Bounds(common.B_IMEMNODE_T_IFREE)
	remains := true
	for remains {
		tryevict := ca.Shouldevict(gimme)
		idm.fs.fslog.Op_begin("ifree")

		// set of blocks that will be written by this transaction;
		// include an entry for the inode block (to update major) and a
		// fake entry for the orphan block conservatively.
		distinct := map[int]bool{idm.fs.ialloc.Iblock(idm.inum): true,
			-1: true}

		which := idm.major

		bliter := &blockiter_t{}
		bliter.bi_init(idm, tryevict)

		for len(distinct) < MaxBlkPerOp && remains {
			blkno := -1
			var ok bool
			blkno, ok, which, remains = bliter.next(which)
			if ok {
				idm.fs.balloc.Bfree(blkno)
				freeblk := idm.fs.balloc.alloc.bitmapblkno(blkno)
				distinct[freeblk] = true
			}
		}
		bliter.release()

		// must lock the inode block before marking it free, to prevent
		// clobbering a newly, concurrently allocated/created inode
		iblk, _ := idm.idibread()

		idm.major = which
		if !remains {
			// all the blocks have been freed
			idm.itype = I_DEAD
			idm.fs.icache.clearOrphan(idm.inum)
			idm.fs.ialloc.Ifree(idm.inum)
		}

		idm.flushto(iblk, idm.inum)
		iblk.Unlock()
		idm.fs.fslog.Write(iblk)
		idm.fs.fslog.Relse(iblk, "ifree")
		idm.fs.fslog.Op_end()
	}

	return 0
}

// used for {,f}stat
func (idm *imemnode_t) mkmode() uint {
	itype := idm.itype
	switch itype {
	case I_DIR, I_FILE:
		return uint(itype << 16)
	case I_DEV:
		// this can happen by fs-internal stats
		return common.Mkdev(idm.major, idm.minor)
	default:
		panic("weird itype")
	}
}

func fieldr(p *common.Bytepg_t, field int) int {
	return common.Readn(p[:], 8, field*8)
}

func fieldw(p *common.Bytepg_t, field int, val int) {
	common.Writen(p[:], 8, field*8, val)
}

//
// Inode cache
//

type icache_t struct {
	refcache     *refcache_t
	fs           *Fs_t
	orphanbitmap *bitmap_t

	sync.Mutex
	// An in-memory list of dead (an inode that doesn't show up in any
	// directory anymore and isn't in any fd). They must be free at the end
	// of a syscall.  We don't call ifree() immediately during an op,
	// because we want to run ifree() as its own op. We don't use a channel
	// and a seperate daemon because want to free the inode and its blocks
	// at the end of the syscall so that the next sys call can use the freed
	// resources.
	dead []*imemnode_t
}

const maxinodepersys = 4

func mkIcache(fs *Fs_t, start, len int) *icache_t {
	icache := &icache_t{}
	icache.refcache = mkRefcache(common.Syslimit.Vnodes, false)
	icache.fs = fs
	icache.orphanbitmap = mkAllocater(fs, start, len, fs.fslog)
	// dead is bounded the number of inodes refdowned in system call, and
	// the system calls admitted to log.
	icache.dead = make([]*imemnode_t, 0)
	return icache
}

// The following are used are used to update the orphanbitmap on disk as part of
// iunlink and ifree. An orphan is an inode that doesn't show up in any
// directory.  These inodes need to be freed on the last close of an fd that
// points to this inode.  However, on recovery, we need to free them explicitly
// (the crash is the "last" close). When links reaches zero the fs marks the
// inode as an orphan and when calling ifree the fs clears the orphan bit.

func (icache *icache_t) markOrphan(inum common.Inum_t) {
	icache.orphanbitmap.Mark(int(inum))
}

func (icache *icache_t) clearOrphan(inum common.Inum_t) {
	icache.orphanbitmap.Unmark(int(inum))
}

func (icache *icache_t) freeOrphan(inum common.Inum_t) {
	if fs_debug {
		fmt.Printf("freeOrphan: %v\n", inum)
	}
	imem, err := icache.Iref(inum, "freeOrphan")
	if err != 0 {
		panic("freeOrphan")
	}
	evicted := icache.refcache.Refdown(imem, "freeOrphan")
	if !evicted {
		panic("link count isn't zero?")
	}
	// we might have crashed during RecoverOrphans and already have
	// reclaimed this inode.
	if imem.itype != I_DEAD {
		imem.Free()
	}
}

func (icache *icache_t) RecoverOrphans() {
	last := common.Inum_t(0)
	done := false
	var err common.Err_t
	for !done {
		inum := common.Inum_t(0)
		done, err = icache.orphanbitmap.apply(int(last), func(b, v int) bool {
			if v != 0 {
				inum = common.Inum_t(b)
				return false
			}
			return true
		})
		if err != 0 {
			panic("RecoverOrphans")
		}
		if inum != 0 {
			// don't free inode inside of apply(), because apply
			// holds the lock on the bitmap block, but free needs
			// to mark clear orphan status.
			icache.freeOrphan(inum)
			last = inum
		}
	}
	// XXX remove once reservation counting is fixed s.t. credit cannot be
	// leaked
	common.Resend()
}

// Refdown() will mark an inode dead, which freeDead() frees in an ifree
// operation.
func (icache *icache_t) addDead(imem *imemnode_t) {
	icache.Lock()
	defer icache.Unlock()
	if len(icache.dead) > icache.fs.fslog.loglen {
		panic("addDead")
	}
	icache.dead = append(icache.dead, imem)
}

// XXX Fs_close() from different threads are contending for icache.dead...
func (icache *icache_t) freeDead() {
	icache.Lock()
	defer icache.Unlock()

	if fs_debug {
		fmt.Printf("freeDead: %v dead inodes\n", len(icache.dead))
	}
	// XXX cache reservation
	for len(icache.dead) > 0 {
		imem := icache.dead[0]
		icache.dead = icache.dead[1:]
		icache.Unlock()
		imem.Free()
		icache.Lock()
	}
	icache.dead = make([]*imemnode_t, 0)
}

func (icache *icache_t) Stats() string {
	s := "icache: size "
	s += strconv.Itoa(len(icache.refcache.refs))
	s += " #evictions "
	s += strconv.Itoa(icache.refcache.nevict)
	s += " #live "
	s += strconv.Itoa(icache.refcache.nlive())
	s += "\n"
	return s
}

// obtain the reference for an inode
func (icache *icache_t) Iref(inum common.Inum_t, s string) (*imemnode_t, common.Err_t) {
	ref, victim, err := icache.refcache.Lookup(int(inum), s)
	if err != 0 {
		return nil, err
	}
	defer ref.Unlock()

	if victim != nil {
		imem := victim.(*imemnode_t)
		if imem.links == 0 {
			panic("linkcount of zero on Lookup!")
		}
		imem.Evict()
	}

	if !ref.valid {
		if fs_debug {
			fmt.Printf("iref load %v %v\n", inum, s)
		}

		imem := &imemnode_t{}
		imem.fs = icache.fs
		imem.inum = inum
		ref.obj = imem
		err := imem.idm_init(inum)
		if err != 0 {
			panic("idm_init")
			return nil, err
		}
		ref.valid = true
	}

	return ref.obj.(*imemnode_t), err
}

// Obtain the reference for an inode with the inode locked
func (icache *icache_t) Iref_locked(inum common.Inum_t, s string) (*imemnode_t, common.Err_t) {
	idm, err := icache.Iref(inum, s)
	if err != 0 {
		return nil, err
	}
	idm.ilock(s)
	return idm, err
}

func (icache *icache_t) Refdown(imem *imemnode_t, s string) {
	evicted := icache.refcache.Refdown(imem, "fs_rename_opar")
	if evicted && imem.links == 0 { // when running always_eager links may not be 0
		icache.addDead(imem)
	}
}

func (icache *icache_t) Refup(imem *imemnode_t, s string) {
	icache.refcache.Refup(imem, "_fs_open")
}

// Grab locks on inodes references in imems.  handles duplicates.
func iref_lockall(imems []*imemnode_t) []*imemnode_t {
	var locked []*imemnode_t
	sort.Slice(imems, func(i, j int) bool { return imems[i].Key() < imems[j].Key() })
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

//
// Inode allocator
//

type ibitmap_t struct {
	alloc    *bitmap_t
	start    int
	len      int
	first    int
	inodelen int
	maxinode int
}

func mkIalloc(fs *Fs_t, start, len, first, inodelen int) *ibitmap_t {
	ialloc := &ibitmap_t{}
	ialloc.alloc = mkAllocater(fs, start, len, fs.fslog)
	ialloc.start = start
	ialloc.len = len
	ialloc.first = first
	ialloc.inodelen = inodelen
	ialloc.maxinode = inodelen * (common.BSIZE / ISIZE)
	fmt.Printf("ialloc: mapstart %v maplen %v inode start %v inode len %v max inode# %v nfree %d\n",
		ialloc.start, ialloc.len, ialloc.first, ialloc.inodelen, ialloc.maxinode,
		ialloc.alloc.nfreebits)
	return ialloc
}

func (ialloc *ibitmap_t) Ialloc() (common.Inum_t, common.Err_t) {
	n, err := ialloc.alloc.FindAndMark()
	if err != 0 {
		return 0, err
	}
	if fs_debug {
		fmt.Printf("ialloc %d freebits %d\n", n, ialloc.alloc.nfreebits)
	}
	// we may have more bits in inode bitmap blocks than inodes on disk
	if n >= ialloc.maxinode {
		panic("Ialloc; higher inodes should have been marked in use")
	}
	inum := common.Inum_t(n)
	return inum, 0
}

// once Ifree() returns, inum can be reallocated by a concurrent operation.
// therefore, the caller must either have the block for inum locked or must not
// further modify the block for inum after calling Ifree.
func (ialloc *ibitmap_t) Ifree(inum common.Inum_t) common.Err_t {
	if fs_debug {
		fmt.Printf("ifree: mark free %d free before %d\n", inum, ialloc.alloc.nfreebits)
	}
	return ialloc.alloc.Unmark(int(inum))
}

func (ialloc *ibitmap_t) Iblock(inum common.Inum_t) int {
	b := int(inum) / (common.BSIZE / ISIZE)
	b += ialloc.first
	if b < ialloc.first || b >= ialloc.first+ialloc.inodelen {
		fmt.Printf("inum=%v b = %d\n", inum, b)
		panic("Iblock: too big inum")
	}
	return b
}

func ioffset(inum common.Inum_t) int {
	o := int(inum) % (common.BSIZE / ISIZE)
	return o
}

func (ialloc *ibitmap_t) Stats() string {
	return "inode " + ialloc.alloc.Stats()
}
