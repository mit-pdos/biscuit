package fs

import "fmt"
import "sync"
import "sort"
import "unsafe"

import "bounds"
import "defs"
import "fdops"
import "hashtable"
import "limits"
import "mem"
import "res"
import "stat"
import "stats"
import "ustr"
import "util"

type inode_stats_t struct {
	Nopen       stats.Counter_t
	Nnamei      stats.Counter_t
	Nifill      stats.Counter_t
	Niupdate    stats.Counter_t
	Nistat      stats.Counter_t
	Niread      stats.Counter_t
	Niwrite     stats.Counter_t
	Ndo_write   stats.Counter_t
	Nfillhole   stats.Counter_t
	Ngrow       stats.Counter_t
	Nitrunc     stats.Counter_t
	Nimmap      stats.Counter_t
	Nifree      stats.Counter_t
	Nicreate    stats.Counter_t
	Nilink      stats.Counter_t
	Nunlink     stats.Counter_t
	Nrename     stats.Counter_t
	Nlseek      stats.Counter_t
	Nmkdir      stats.Counter_t
	Nclose      stats.Counter_t
	Nsync       stats.Counter_t
	Nreopen     stats.Counter_t
	CWrite      stats.Cycles_t
	Cwrite      stats.Cycles_t
	Ciwrite     stats.Cycles_t
	Ciwritecopy stats.Cycles_t
	Ciupdate    stats.Cycles_t
}

func (is *inode_stats_t) Stats() string {
	s := "inode" + stats.Stats2String(*is)
	*is = inode_stats_t{}
	return s
}

type Inode_t struct {
	Iblk *Bdev_block_t
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
	INDADDR = (BSIZE / 8)
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
	// _l protects all fields except for inum (which is the key for lookup
	// and thus already known).
	_l   sync.Mutex
	inum defs.Inum_t
	fs   *Fs_t
	ref  *Objref_t

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
		dents *hashtable.Hashtable_t
		// dents dc_rbh_t
		freel fdelist_t
	}
}

func (idm *imemnode_t) String() string {
	s := ""
	s += fmt.Sprintf("[inum %d type %d]\n", idm.inum, idm.itype)
	return s
}

func (imem *imemnode_t) Refup(s string) (uint32, bool) {
	return imem.ref.Up()
}

func (imem *imemnode_t) Refdown(s string) bool {
	v := imem.ref.Down()
	if v == 0 && imem.links == 0 { // remove unlinked inodes from cache
		rem := imem.fs.icache.cache.Remove(int(imem.inum))
		if !rem {
			panic("huh")
		}
		return true
	}
	return false
}

// To find all inodes that have de.idm in their dcache
type de_inode_t struct {
	parent *imemnode_t
	de     *icdent_t
}

// Remove idm from other inode's cache and delete its dcache. This causes the
// refcnt of this inode to maybe drop to 0, and it will be truly evicted (i.e.,
// removed from icache).
func (idm *imemnode_t) evictDcache() {
	if fs_debug {
		fmt.Printf("evictDcache: %v\n", idm)
	}
	idm._derelease()
}

// The cache wants to evict this inode because it hasn't been used recently, but
// its links maybe non-zero, so it could be in use.
func (idm *imemnode_t) EvictFromCache() {
	idm.ilock("Evict")
	idm.evictDcache()
	idm.iunlock("Evict")
}

func (idm *imemnode_t) EvictDone() {
	// nothing to do anymore: Evict() already deleted idm's directory cache
}

func (idm *imemnode_t) Free() {
	// no need to lock...
	if idm.links != 0 {
		panic("non-zero links")
	}
	idm.ifree()
}

func (idm *imemnode_t) ilock(s string) {
	// if idm._amlocked {
	//	fmt.Printf("ilocked: warning %v %v\n", idm.inum, s)
	// }
	idm._l.Lock()
	if idm.inum < 0 {
		panic("negative inum")
	}
	idm._amlocked = true
	//fmt.Printf("ilock: acquired %v %s\n", idm.inum, s)
}

func (idm *imemnode_t) iunlock(s string) {
	//fmt.Printf("iunlock: release %v %v\n", idm.inum, s)
	//if !idm._amlocked {
	//	panic("iunlock:" + s)
	//}
	idm._amlocked = false
	idm._l.Unlock()
}

// Fill in inode
func (idm *imemnode_t) idm_init(inum defs.Inum_t) {
	if fs_debug {
		fmt.Printf("idm_init: read inode %v\n", inum)
	}
	blk := idm.idibread()
	idm.fs.istats.Nifill.Inc()
	idm.fill(blk, inum)
	blk.Unlock()
	idm.fs.fslog.Relse(blk, "idm_init")
}

func (idm *imemnode_t) iunlock_refdown(s string) bool {
	// Refdown before unlocking to guarantee that calling iunlock_refdown
	// on a non-empty directory inode cannot require the caller to free the
	// inode (otherwise a concurrent unlink could occur between the unlock
	// and refdown).
	ret := idm.Refdown(s)
	idm.iunlock(s)
	return ret
}

func (idm *imemnode_t) _iupdate(opid opid_t) defs.Err_t {
	if !idm._amlocked {
		panic("eh?")
	}
	if idm.fs.diskfs {
		idm.fs.istats.Niupdate.Inc()
		iblk := idm.idibread()
		if idm.flushto(iblk, idm.inum) {
			iblk.Unlock()
			idm.fs.fslog.Write(opid, iblk)
		} else {
			iblk.Unlock()
		}
		idm.fs.fslog.Relse(iblk, "_iupdate")

	}
	return 0
}

func (idm *imemnode_t) do_trunc(opid opid_t, truncto uint) defs.Err_t {
	if idm.itype != I_FILE && idm.itype != I_DEV {
		panic("bad truncate")
	}
	err := idm.itrunc(opid, truncto)
	if err == 0 {
		idm._iupdate(opid)
	}
	return err
}

func (idm *imemnode_t) do_read(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return idm.iread(dst, offset)
}

func (idm *imemnode_t) do_write(src fdops.Userio_i, offset int, app bool) (int, defs.Err_t) {
	// break write system calls into one or more calls with no more than
	// maxblkpersys blocks per call. account for indirect blocks.
	max := (MaxBlkPerOp - 3) * BSIZE
	sz := src.Totalsz()
	i := 0

	if fs_debug {
		fmt.Printf("dowrite: %v %v %v\n", idm.inum, offset, sz)
	}

	idm.fs.istats.Ndo_write.Inc()
	for i < sz {
		gimme := bounds.Bounds(bounds.B_IMEMNODE_T_DO_WRITE)
		if !res.Resadd_noblock(gimme) {
			return i, -defs.ENOHEAP
		}

		n := sz - i
		if n > max {
			n = max
		}

		s := stats.Rdtsc()

		opid := idm.fs.fslog.Op_begin("dowrite")

		idm.ilock("")
		if idm.itype == I_DIR {
			panic("write to dir")
		}
		off := offset + i
		if app {
			off = idm.size
		}
		s1 := stats.Rdtsc()
		wrote, err := idm.iwrite(opid, src, off, n)
		idm.fs.istats.Ciwrite.Add(s1)

		s2 := stats.Rdtsc()
		idm._iupdate(opid)
		idm.fs.istats.Ciupdate.Add(s2)

		idm.iunlock("")

		idm.fs.fslog.Op_end(opid)

		t := stats.Rdtsc()
		idm.fs.istats.Cwrite.Add(t - s)

		if err != 0 {
			return i, err
		}

		i += wrote
	}
	return i, 0
}

func (idm *imemnode_t) do_stat(st *stat.Stat_t) defs.Err_t {
	idm.fs.istats.Nistat.Inc()
	st.Wdev(0)
	st.Wino(uint(idm.inum))
	st.Wmode(idm.mkmode())
	st.Wsize(uint(idm.size))
	st.Wrdev(defs.Mkdev(idm.major, idm.minor))
	return 0
}

func (idm *imemnode_t) do_mmapi(off, len int, inc bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	if idm.itype != I_FILE && idm.itype != I_DIR {
		panic("bad mmapinfo")
	}
	return idm.immapinfo(off, len, inc)
}

func (idm *imemnode_t) do_dirchk(opid opid_t, wantdir bool) defs.Err_t {
	amdir := idm.itype == I_DIR
	if wantdir && !amdir {
		return -defs.ENOTDIR
	} else if !wantdir && amdir {
		return -defs.EISDIR
	} else if amdir {
		amempty, err := idm.idirempty(opid)
		if err != 0 {
			return err
		}
		if !amempty {
			return -defs.ENOTEMPTY
		}
	}
	return 0
}

// unlink cannot encounter OOM since directory page caches are eagerly loaded.
func (idm *imemnode_t) do_unlink(opid opid_t, name ustr.Ustr) defs.Err_t {
	_, err := idm.iunlink(opid, name)
	if err == 0 {
		idm._iupdate(opid)
	}
	return err
}

// create new dir ent with given inode number
func (idm *imemnode_t) do_insert(opid opid_t, fn ustr.Ustr, n defs.Inum_t) defs.Err_t {
	err := idm.iinsert(opid, fn, n)
	if err == 0 {
		idm._iupdate(opid)
	}
	return err
}

func (idm *imemnode_t) do_createnod(opid opid_t, fn ustr.Ustr, maj, min int) (*imemnode_t, defs.Err_t) {
	if idm.itype != I_DIR {
		return nil, -defs.ENOTDIR
	}

	itype := I_DEV
	child, err := idm.icreate(opid, fn, itype, maj, min)
	idm._iupdate(opid)
	return child, err
}

func (idm *imemnode_t) do_createfile(opid opid_t, fn ustr.Ustr) (*imemnode_t, defs.Err_t) {
	if idm.itype != I_DIR {
		return nil, -defs.ENOTDIR
	}

	itype := I_FILE
	child, err := idm.icreate(opid, fn, itype, 0, 0)
	idm._iupdate(opid)
	return child, err
}

func (idm *imemnode_t) do_createdir(opid opid_t, fn ustr.Ustr) (*imemnode_t, defs.Err_t) {
	if idm.itype != I_DIR {
		return nil, -defs.ENOTDIR
	}

	itype := I_DIR
	child, err := idm.icreate(opid, fn, itype, 0, 0)
	idm._iupdate(opid)
	return child, err
}

// caller holds lock on idm
func (idm *imemnode_t) _linkdown(opid opid_t) {
	idm.links--
	if idm.links <= 0 {
		idm.fs.icache.markOrphan(opid, idm.inum)
	}
	idm._iupdate(opid)
}

func (idm *imemnode_t) _linkup(opid opid_t) {
	idm.links++
	idm._iupdate(opid)
}

// metadata block interface; only one inode touches these blocks at a time,
// thus no concurrency control
func (ic *imemnode_t) mbread(blockn int) *Bdev_block_t {
	mb := ic.fs.fslog.Get_fill(blockn, "mbread", false)
	return mb
}

func (ic *imemnode_t) fill(blk *Bdev_block_t, inum defs.Inum_t) {
	inode := Inode_t{blk, ioffset(inum)}
	ic.itype = inode.itype()
	if ic.itype <= I_FIRST || ic.itype > I_VALID {
		fmt.Printf("itype: %v for %v\n", ic.itype, inum)
		// we will soon panic
		panic("no")
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
	if ic.itype == I_DIR {
		ic.dentc.dents = hashtable.MkHash(100)
	}
}

// returns true if the inode data changed, and thus needs to be flushed to disk
func (ic *imemnode_t) flushto(blk *Bdev_block_t, inum defs.Inum_t) bool {
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
func (idm *imemnode_t) ensureb(opid opid_t, blkno int, writing bool) (int, bool, defs.Err_t) {
	if !writing || blkno != 0 {
		return blkno, false, 0
	}
	nblkno, err := idm.fs.balloc.Balloc(opid)
	return nblkno, true, err
}

// ensure entry in indirect block exists
func (idm *imemnode_t) ensureind(opid opid_t, blk *Bdev_block_t, slot int, writing bool) (int, defs.Err_t) {
	off := slot * 8
	s := blk.Data[:]
	blkn := util.Readn(s, 8, off)
	blkn, isnew, err := idm.ensureb(opid, blkn, writing)
	if err != 0 {
		return 0, err
	}
	if isnew {
		util.Writen(s, 8, off, blkn)
		idm.fs.fslog.Write(opid, blk)
	}
	return blkn, 0
}

// Assumes that every block until b exits
// XXX change to wrap blockiter_t instead
func (idm *imemnode_t) fbn2block(opid opid_t, fbn int, writing bool) (int, bool, defs.Err_t) {
	if fbn < NIADDRS {
		if idm.addrs[fbn] != 0 {
			return idm.addrs[fbn], false, 0
		}
		blkn, err := idm.fs.balloc.Balloc(opid)
		if err != 0 {
			return 0, false, err
		}
		// imemnode_t.iupdate() will make sure the
		// icache is updated on disk
		idm.addrs[fbn] = blkn
		return blkn, true, 0
	} else {
		fbn -= NIADDRS
		if fbn < INDADDR {
			indno := idm.indir
			indno, isnew, err := idm.ensureb(opid, indno, writing)
			if err != 0 {
				return 0, false, err
			}
			if isnew {
				// new indirect block will be written to log by iupdate()
				idm.indir = indno
			}
			indblk := idm.mbread(indno)
			blkn, err := idm.ensureind(opid, indblk, fbn, writing)
			idm.fs.fslog.Relse(indblk, "indblk")
			return blkn, false, err
		} else if fbn < INDADDR*INDADDR {
			fbn -= INDADDR
			dindno := idm.dindir
			dindno, isnew, err := idm.ensureb(opid, dindno, writing)
			if err != 0 {
				return 0, false, err
			}
			if isnew {
				// new dindirect block will be written to log by iupdate()
				idm.dindir = dindno
			}
			dindblk := idm.mbread(dindno)
			indno, err := idm.ensureind(opid, dindblk, fbn/INDADDR, writing)
			idm.fs.fslog.Relse(dindblk, "dindblk")

			indblk := idm.mbread(indno)
			blkn, err := idm.ensureind(opid, indblk, fbn%INDADDR, writing)
			idm.fs.fslog.Relse(indblk, "indblk2")
			return blkn, false, err
		} else {
			panic("too big fbn")
			return 0, false, 0

		}
	}
}

func (idm *imemnode_t) bmapfill(opid opid_t, lastblk int, whichblk int, writing bool) (int, bool, defs.Err_t) {
	blkn := 0
	new := false
	var err defs.Err_t

	if whichblk >= lastblk { // a hole?
		// allocate the missing blocks
		if whichblk > lastblk && writing {
			idm.fs.istats.Nfillhole.Inc()
		} else if writing {
			idm.fs.istats.Ngrow.Inc()
		}
		for b := lastblk; b <= whichblk; b++ {
			gimme := bounds.Bounds(bounds.B_IMEMNODE_T_BMAPFILL)
			if !res.Resadd_noblock(gimme) {
				return 0, false, -defs.ENOHEAP
			}
			// XXX we could remember where the last slot was
			blkn, new, err = idm.fbn2block(opid, b, writing)
			if err != 0 {
				return blkn, new, err
			}
		}
	} else {
		blkn, new, err = idm.fbn2block(opid, whichblk, writing)
		if err != 0 {
			return blkn, new, err
		}
	}
	return blkn, new, 0
}

// Takes as input the file offset and whether the operation is a write and
// returns the block number of the block responsible for that offset.
func (idm *imemnode_t) offsetblk(opid opid_t, offset int, writing bool) (int, bool, defs.Err_t) {
	if writing && opid == 0 && idm.fs.diskfs {
		panic("offsetblk: writing but no opid\n")
	}
	whichblk := offset / BSIZE
	lastblk := idm.size / BSIZE
	blkn, new, err := idm.bmapfill(opid, lastblk, whichblk, writing)
	if err != 0 {
		return blkn, new, err
	}
	if blkn <= 0 || blkn >= idm.fs.superb.Lastblock() {
		panic("offsetblk: bad data blocks")
	}
	return blkn, new, 0
}

// Return locked buffer for offset
func (idm *imemnode_t) off2buf(opid opid_t, offset int, len int, fillhole bool, fill bool, s string) (*Bdev_block_t, defs.Err_t) {
	if offset%mem.PGSIZE+len > mem.PGSIZE {
		panic("off2buf")
	}
	blkno, new, err := idm.offsetblk(opid, offset, fillhole)
	if err != 0 {
		return nil, err
	}
	var b *Bdev_block_t
	if fill && !new {
		b = idm.fs.fslog.Get_fill(blkno, s, true)
	} else {
		b = idm.fs.fslog.Get_nofill(blkno, s, true)
	}
	return b, 0
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (idm *imemnode_t) iread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	idm.fs.istats.Niread.Inc()
	isz := idm.size
	c := 0
	gimme := bounds.Bounds(bounds.B_IMEMNODE_T_IREAD)
	for offset < isz && dst.Remain() != 0 {
		if !res.Resadd_noblock(gimme) {
			return c, -defs.ENOHEAP
		}
		m := min(BSIZE-offset%BSIZE, dst.Remain())
		m = min(isz-offset, m)
		b, err := idm.off2buf(opid_t(0), offset, m, false, true, "iread")
		if err != 0 {
			return c, err
		}
		s := offset % BSIZE
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

func (idm *imemnode_t) iwrite(opid opid_t, src fdops.Userio_i, offset int, n int) (int, defs.Err_t) {
	idm.fs.istats.Niwrite.Inc()
	sz := min(src.Totalsz(), n)
	newsz := offset + sz
	c := 0
	gimme := bounds.Bounds(bounds.B_IMEMNODE_T_IWRITE)
	for c < sz {
		if !res.Resadd_noblock(gimme) {
			return c, -defs.ENOHEAP
		}
		m := min(BSIZE-offset%BSIZE, sz-c)
		fill := m != BSIZE
		b, err := idm.off2buf(opid, offset, m, true, fill, "iwrite")
		if err != 0 {
			return c, err
		}
		s := offset % BSIZE

		if fs_debug {
			fmt.Printf("_iwrite c %v sz %v off %v m %v s %v s+m %v\n",
				c, sz, offset, m, s, s+m)
		}

		dst := b.Data[s : s+m]
		ts := stats.Rdtsc()
		read, err := src.Uioread(dst)
		b.Unlock()
		idm.fs.istats.Ciwritecopy.Add(ts)
		idm.fs.fslog.Write_ordered(opid, b)
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

func (idm *imemnode_t) itrunc(opid opid_t, newlen uint) defs.Err_t {
	if newlen > uint(idm.size) {
		// this will cause the hole to filled in with zero blocks which
		// are logged to disk
		_, _, err := idm.offsetblk(opid, int(newlen), true)
		if err != 0 {
			return err
		}

	}
	idm.fs.istats.Nitrunc.Inc()
	// inode is flushed by do_itrunc
	idm.size = int(newlen)
	return 0
}

// reverts icreate(). called after failure to allocate that prevents an FS
// operation from continuing.
func (idm *imemnode_t) create_undo(opid opid_t, childi defs.Inum_t, childn ustr.Ustr) defs.Err_t {
	ci, err := idm.iunlink(opid, childn)
	if err != 0 {
		panic("but insert just succeeded")
	}
	if ci != childi {
		panic("inconsistent")
	}
	ib := idm.fs.fslog.Get_fill(idm.fs.ialloc.Iblock(childi), "create_undo", true)
	ni := &Inode_t{ib, ioffset(childi)}
	ni.W_itype(I_DEAD)
	ib.Unlock()
	idm.fs.fslog.Relse(ib, "create_undo")
	idm.fs.ialloc.Ifree(opid, childi)
	return 0
}

func (idm *imemnode_t) icreate(opid opid_t, name ustr.Ustr, nitype, major, minor int) (*imemnode_t, defs.Err_t) {
	// XXX XXX fail if links == 0
	if !idm._amlocked {
		panic("lsjdf")
	}

	if nitype <= I_INVALID || nitype > I_VALID {
		panic("bad itype!")
	}
	if len(name) == 0 {
		panic("icreate with no name")
	}
	if nitype != I_DEV && (major != 0 || minor != 0) {
		panic("inconsistent args")
	}
	// make sure file does not already exist
	child, err := idm.ilookup(opid, name)
	if err == 0 {
		return child, -defs.EEXIST
	}

	idm.fs.istats.Nicreate.Inc()

	// allocate new inode
	newinum, err := idm.fs.ialloc.Ialloc(opid)
	var newidm *imemnode_t
	var newinode *Inode_t
	if idm.fs.diskfs {
		newbn := idm.fs.ialloc.Iblock(newinum)
		newioff := ioffset(newinum)
		if err != 0 {
			return nil, err
		}
		newiblk := idm.fs.fslog.Get_fill(newbn, "icreate", true)
		if fs_debug {
			fmt.Printf("ialloc: %v %v %v\n", newbn, newioff, newinum)
		}

		newinode = &Inode_t{newiblk, newioff}
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
		newiblk.Unlock()
		idm.fs.fslog.Write(opid, newiblk)
		idm.fs.fslog.Relse(newiblk, "icreate")
		newidm = idm.fs.icache.Iref(newinum, "icreate")
	} else {
		// insert in icache
		newidm = idm.fs.icache.Iref_locked_nofill(newinum, "icreate")
		newidm.itype = nitype
		newidm.links = 1
		newidm.major = major
		newidm.minor = minor
		if newidm.itype == I_DIR {
			newidm.dentc.dents = hashtable.MkHash(100)
		}
		newidm.iunlock("icreate")
	}
	// write new directory entry referencing newinode
	err = idm._deinsert(opid, name, newinum)
	if err != 0 {
		fmt.Printf("deinsert failed\n")
		if idm.fs.diskfs {
			newinode.W_itype(I_DEAD)
		}
		newidm.itype = I_DEAD
		idm.fs.ialloc.Ifree(opid, newinum)
	}
	return newidm, err
}

func (idm *imemnode_t) immapinfo(offset, len int, mapshared bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	isz := idm.size
	if (len != -1 && len < 0) || offset < 0 {
		panic("bad off/len")
	}
	if offset >= isz {
		return nil, -defs.EINVAL
	}
	if len == -1 || offset+len > isz {
		len = isz - offset
	}

	idm.fs.istats.Nimmap.Inc()
	o := util.Rounddown(offset, mem.PGSIZE)
	len = util.Roundup(offset+len, mem.PGSIZE) - o
	pgc := len / mem.PGSIZE
	ret := make([]mem.Mmapinfo_t, pgc)
	for i := 0; i < len; i += mem.PGSIZE {
		gimme := bounds.Bounds(bounds.B_IMEMNODE_T_IMMAPINFO)
		if !res.Resadd_noblock(gimme) {
			return nil, -defs.ENOHEAP
		}
		buf, err := idm.off2buf(opid_t(0), o+i, mem.PGSIZE, false, true, "immapinfo")
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

		pgn := i / mem.PGSIZE
		wpg := (*mem.Pg_t)(unsafe.Pointer(buf.Data))
		ret[pgn].Pg = wpg
		ret[pgn].Phys = buf.Pa
		idm.fs.fslog.Relse(buf, "immapinfo")
	}
	return ret, 0
}

func (idm *imemnode_t) idibread() *Bdev_block_t {
	return idm.fs.fslog.Get_fill(idm.fs.ialloc.Iblock(idm.inum), "idibread", true)
}

// a type to iterate over the data and indirect blocks of an imemnode_t without
// re-reading and re-locking indirect blocks. it may simultaneously hold
// references to at most two blocks until blockiter_t.release() is called.
type blockiter_t struct {
	idm      *imemnode_t
	which    int
	tryevict bool
	dub      *Bdev_block_t
	lasti    *Bdev_block_t
}

func (bl *blockiter_t) bi_init(idm *imemnode_t, tryevict bool) {
	var zbl blockiter_t
	*bl = zbl
	bl.idm = idm
	bl.tryevict = tryevict
}

// if this imemnode_t has a double-indirect block, _isdub loads it and caches
// it and returns true.
func (bl *blockiter_t) _isdub() (*Bdev_block_t, bool) {
	if bl.dub != nil {
		return bl.dub, true
	}
	blkno := bl.idm.dindir
	if blkno == 0 {
		return nil, false
	}
	bl.dub = bl.idm.mbread(blkno)
	return bl.dub, true
}

// returns the indirect block or block number from the given slot in the
// indirect block
func (bl *blockiter_t) _isdubind(dubslot int, fetch bool) (*Bdev_block_t, int, bool) {
	dub, ok := bl._isdub()
	if !ok {
		return nil, 0, false
	}

	blkno := util.Readn(dub.Data[:], 8, dubslot*8)
	var ret *Bdev_block_t
	ok = blkno != 0
	if fetch {
		ret, ok = bl._isind(blkno)
	}
	return ret, blkno, ok
}

func (bl *blockiter_t) _isind(blkno int) (*Bdev_block_t, bool) {
	if blkno == 0 {
		return nil, false
	}
	if bl.lasti != nil {
		if bl.lasti.Block == blkno {
			return bl.lasti, true
		}
		bl.idm.fs.fslog.Relse(bl.lasti, "release")
	}
	bl.lasti = bl.idm.mbread(blkno)
	if bl.tryevict && bl.idm.fs.diskfs {
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
				blkno = util.Readn(single.Data[:], 8, w*8)
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
				blkno = util.Readn(single.Data[:], 8, islot*8)
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
func (idm *imemnode_t) ifree() defs.Err_t {
	idm.fs.istats.Nifree.Inc()
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

	var ca res.Cacheallocs_t
	gimme := bounds.Bounds(bounds.B_IMEMNODE_T_IFREE)
	remains := true
	var tryevict bool
	for remains {
		tryevict = tryevict || ca.Shouldevict(gimme)
		opid := idm.fs.fslog.Op_begin("ifree")

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
				idm.fs.balloc.Bfree(opid, blkno)
				freeblk := idm.fs.balloc.alloc.bitmapblkno(blkno)
				distinct[freeblk] = true
			}
		}
		bliter.release()

		idm.major = which
		if !remains {
			// all the blocks have been freed
			idm.itype = I_DEAD
			idm.fs.ialloc.Ifree(opid, idm.inum)
			idm.fs.icache.clearOrphan(opid, idm.inum)
		}

		if idm.fs.diskfs {
			// must lock the inode block before marking it free, to prevent
			// clobbering a newly, concurrently allocated/created inode
			iblk := idm.idibread()
			if tryevict {
				iblk.Tryevict()
			}
			idm.flushto(iblk, idm.inum)
			iblk.Unlock()
			idm.fs.fslog.Write(opid, iblk)
			idm.fs.fslog.Relse(iblk, "ifree")
		}
		idm.fs.fslog.Op_end(opid)
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
		return defs.Mkdev(idm.major, idm.minor)
	default:
		panic("weird itype")
	}
}

func fieldr(p *mem.Bytepg_t, field int) int {
	return util.Readn(p[:], 8, field*8)
}

func fieldw(p *mem.Bytepg_t, field int, val int) {
	util.Writen(p[:], 8, field*8, val)
}

//
// Inode cache
//

type icache_t struct {
	cache        *cache_t
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
	// dead []*imemnode_t
}

const maxinodepersys = 4

func mkIcache(fs *Fs_t, start, len int) *icache_t {
	icache := &icache_t{}
	icache.cache = mkCache(limits.Syslimit.Vnodes)
	icache.fs = fs
	icache.orphanbitmap = mkAllocater(fs, start, len, fs.fslog)
	return icache
}

// The following are used are used to update the orphanbitmap on disk as part of
// iunlink and ifree. An orphan is an inode that doesn't show up in any
// directory.  These inodes need to be freed on the last close of an fd that
// points to this inode.  However, on recovery, we need to free them explicitly
// (the crash is the "last" close). When links reaches zero the fs marks the
// inode as an orphan and when calling ifree the fs clears the orphan bit.

func (icache *icache_t) markOrphan(opid opid_t, inum defs.Inum_t) {
	if icache.fs.diskfs {
		icache.orphanbitmap.Mark(opid, int(inum))
	}
}

func (icache *icache_t) clearOrphan(opid opid_t, inum defs.Inum_t) {
	if icache.fs.diskfs {
		icache.orphanbitmap.Unmark(opid, int(inum))
	}
}

func (icache *icache_t) freeOrphan(inum defs.Inum_t) {
	if fs_debug {
		fmt.Printf("freeOrphan: %v\n", inum)
	}
	imem := icache.Iref(inum, "freeOrphan")
	v := imem.ref.Down()
	if v != 0 {
		panic("freeOrphan")
	}
	icache.cache.Remove(int(imem.inum))
	// evicted := icache.cache.DoneKey(int(inum))
	// if !evicted {
	//	panic("link count isn't zero?")
	//}
	// we might have crashed during RecoverOrphans and already have
	// reclaimed this inode.
	if imem.itype != I_DEAD {
		imem.Free()
	}
}

func (icache *icache_t) RecoverOrphans() {
	last := defs.Inum_t(0)
	done := false
	for !done {
		inum := defs.Inum_t(0)
		done = icache.orphanbitmap.apply(int(last), func(b, v int) bool {
			if v != 0 {
				inum = defs.Inum_t(b)
				return false
			}
			return true
		})
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
	res.Resend()
}

func (icache *icache_t) Stats() string {
	return "icache " + icache.cache.Stats()
}

func (icache *icache_t) Iref(inum defs.Inum_t, s string) *imemnode_t {
	return icache._iref(inum, true, false)
}

func (icache *icache_t) Iref_locked(inum defs.Inum_t, s string) *imemnode_t {
	return icache._iref(inum, true, true)
}

func (icache *icache_t) Iref_locked_nofill(inum defs.Inum_t, s string) *imemnode_t {
	return icache._iref(inum, false, true)
}

func (icache *icache_t) _iref(inum defs.Inum_t, fill bool, lock bool) *imemnode_t {
	ref, created := icache.cache.Lookup(int(inum),
		func(in int) Obj_t {
			ret := &imemnode_t{}
			// inum can be read without ilock, and therefore must be
			// initialized before the refcache unlocks.
			ret.inum = defs.Inum_t(in)
			ret.ilock("")
			return ret
		})

	ret := ref.Obj.(*imemnode_t)
	if created {
		// ret is locked
		ret.fs = icache.fs
		ret.ref = ref
		ret.inum = inum
		if fill {
			ret.idm_init(inum)
		}
	}
	locked := created
	if locked != lock {
		if lock {
			ret.ilock("")
		} else {
			ret.iunlock("")
		}
	}
	return ret
}

// Grab locks on inodes references in imems.  handles duplicates.
func iref_lockall(imems []*imemnode_t) []*imemnode_t {
	locked := make([]*imemnode_t, 0, 4)
	sort.Slice(imems, func(i, j int) bool { return imems[i].inum < imems[j].inum })
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
	ialloc.maxinode = inodelen * (BSIZE / ISIZE)
	//fmt.Printf("ialloc: mapstart %v maplen %v inode start %v inode len %v max inode# %v nfree %d\n",
	//	ialloc.start, ialloc.len, ialloc.first, ialloc.inodelen, ialloc.maxinode,
	//	ialloc.alloc.nfreebits)
	return ialloc
}

func (ialloc *ibitmap_t) Ialloc(opid opid_t) (defs.Inum_t, defs.Err_t) {
	n, err := ialloc.alloc.FindAndMark(opid)
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
	inum := defs.Inum_t(n)
	return inum, 0
}

// once Ifree() returns, inum can be reallocated by a concurrent operation.
// therefore, the caller must either have the block for inum locked or must not
// further modify the block for inum after calling Ifree.
func (ialloc *ibitmap_t) Ifree(opid opid_t, inum defs.Inum_t) {
	if fs_debug {
		fmt.Printf("ifree: mark free %d free before %d\n", inum, ialloc.alloc.nfreebits)
	}
	ialloc.alloc.Unmark(opid, int(inum))
}

func (ialloc *ibitmap_t) Iblock(inum defs.Inum_t) int {
	b := int(inum) / (BSIZE / ISIZE)
	b += ialloc.first
	if b < ialloc.first || b >= ialloc.first+ialloc.inodelen {
		fmt.Printf("inum=%v b = %d\n", inum, b)
		panic("Iblock: too big inum")
	}
	return b
}

func ioffset(inum defs.Inum_t) int {
	o := int(inum) % (BSIZE / ISIZE)
	return o
}

func (ialloc *ibitmap_t) Stats() string {
	s := "inode " + ialloc.alloc.Stats()
	ialloc.alloc.ResetStats()
	return s
}
