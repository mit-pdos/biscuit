package fs

import "fmt"
import "sync"
import "unsafe"
import "sort"
import "strconv"
import "common"

var istats inode_stats_t

type inode_stats_t struct {
	nopen     int
	nnamei    int
	nifill    int
	niupdate  int
	nistat    int
	niread    int
	niwrite   int
	ndo_write int
	nfillhole int
	ngrow     int
	nitrunc   int
	nimmap    int
	nifree    int
	nicreate  int
	nilink    int
	nunlink   int
	nrename   int
	nlseek    int
	nmkdir    int
	nclose    int
	nsync     int
	nreopen   int
}

func inode_stats() string {
	s := "inode"
	s += "\n\t#nopen: "
	s += strconv.Itoa(istats.nopen)
	s += " \n\t#namei: "
	s += strconv.Itoa(istats.nnamei)
	s += " \n\t#ifill: "
	s += strconv.Itoa(istats.nifill)
	s += " \n\t#iupdate: "
	s += strconv.Itoa(istats.niupdate)
	s += " \n\t#istat: "
	s += strconv.Itoa(istats.nistat)
	s += " \n\t#iread: "
	s += strconv.Itoa(istats.niread)
	s += " \n\t#do_write: "
	s += strconv.Itoa(istats.ndo_write)
	s += " \n\t#iwrite: "
	s += strconv.Itoa(istats.niwrite)
	s += " \n\t#grow: "
	s += strconv.Itoa(istats.ngrow)
	s += " \n\t#fillhole: "
	s += strconv.Itoa(istats.nfillhole)
	s += " \n\t#trunc: "
	s += strconv.Itoa(istats.nitrunc)
	s += " \n\t#mmap: "
	s += strconv.Itoa(istats.nimmap)
	s += " \n\t#ifree: "
	s += strconv.Itoa(istats.nifree)
	s += " \n\t#icreate: "
	s += strconv.Itoa(istats.nicreate)
	s += " \n\t#link: "
	s += strconv.Itoa(istats.nilink)
	s += " \n\t#unlink: "
	s += strconv.Itoa(istats.nunlink)
	s += " \n\t#rename: "
	s += strconv.Itoa(istats.nrename)
	s += " \n\t#lseek: "
	s += strconv.Itoa(istats.nlseek)
	s += " \n\t#mkdir: "
	s += strconv.Itoa(istats.nmkdir)
	s += " \n\t#close: "
	s += strconv.Itoa(istats.nclose)
	s += " \n\t#reopen: "
	s += strconv.Itoa(istats.nreopen)
	s += "\n"
	return s
}

// inode format (see mkbfs.py)
type inode_t struct {
	iblk *common.Bdev_block_t
	ioff int
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
func (ind *inode_t) itype() int {
	it := fieldr(ind.iblk.Data, ifield(ind.ioff, 0))
	if it < I_FIRST || it > I_LAST {
		panic(fmt.Sprintf("weird inode type %d", it))
	}
	return it
}

func (ind *inode_t) linkcount() int {
	return fieldr(ind.iblk.Data, ifield(ind.ioff, 1))
}

func (ind *inode_t) size() int {
	return fieldr(ind.iblk.Data, ifield(ind.ioff, 2))
}

func (ind *inode_t) major() int {
	return fieldr(ind.iblk.Data, ifield(ind.ioff, 3))
}

func (ind *inode_t) minor() int {
	return fieldr(ind.iblk.Data, ifield(ind.ioff, 4))
}

func (ind *inode_t) indirect() int {
	return fieldr(ind.iblk.Data, ifield(ind.ioff, 5))
}

func (ind *inode_t) dindirect() int {
	return fieldr(ind.iblk.Data, ifield(ind.ioff, 6))
}

func (ind *inode_t) addr(i int) int {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 7
	return fieldr(ind.iblk.Data, ifield(ind.ioff, addroff+i))
}

func (ind *inode_t) w_itype(n int) {
	if n < I_FIRST || n > I_LAST {
		panic("weird inode type")
	}
	fieldw(ind.iblk.Data, ifield(ind.ioff, 0), n)
}

func (ind *inode_t) w_linkcount(n int) {
	fieldw(ind.iblk.Data, ifield(ind.ioff, 1), n)
}

func (ind *inode_t) w_size(n int) {
	fieldw(ind.iblk.Data, ifield(ind.ioff, 2), n)
}

func (ind *inode_t) w_major(n int) {
	fieldw(ind.iblk.Data, ifield(ind.ioff, 3), n)
}

func (ind *inode_t) w_minor(n int) {
	fieldw(ind.iblk.Data, ifield(ind.ioff, 4), n)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *inode_t) w_indirect(blk int) {
	fieldw(ind.iblk.Data, ifield(ind.ioff, 5), blk)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *inode_t) w_dindirect(blk int) {
	fieldw(ind.iblk.Data, ifield(ind.ioff, 6), blk)
}

func (ind *inode_t) w_addr(i int, blk int) {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 7
	fieldw(ind.iblk.Data, ifield(ind.ioff, addroff+i), blk)
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
		// true iff all directory entries are cached, thus there is no
		// need to check the disk
		haveall bool
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

// No other thread should have a reference to this inode.
func (idm *imemnode_t) Evict() {
	idm._derelease()
	if fs_debug {
		fmt.Printf("evict: %v\n", idm.inum)
	}
	if idm.links == 0 {
		//idm.fs.fslog.Op_begin("evict")
		idm.ifree()
		//idm.fs.fslog.Op_end()
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
	istats.nifill++
	idm.fill(blk, inum)
	blk.Unlock()
	idm.fs.bcache.Relse(blk, "idm_init")
	return 0
}

func (idm *imemnode_t) iunlock_refdown(s string) {
	idm.iunlock(s)
	idm.fs.icache.Refdown(idm, s)
}

func (idm *imemnode_t) _iupdate() common.Err_t {
	istats.niupdate++
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
	idm.fs.bcache.Relse(iblk, "_iupdate")
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
	max := maxblkspersys * common.BSIZE
	sz := src.Totalsz()
	i := 0

	if fs_debug {
		fmt.Printf("dowrite: %v %v %v\n", idm.inum, offset, sz)
	}

	istats.ndo_write++
	for i < sz {
		n := sz - i
		if n > max {
			n = max
		}
		idm.fs.fslog.Op_begin("dowrite")
		wrote, err := idm.iwrite(src, offset+i, n)
		idm._iupdate()
		idm.fs.fslog.Op_end()

		if err != 0 {
			return i, err
		}

		i += wrote
	}
	return i, 0
}

func (idm *imemnode_t) do_stat(st *common.Stat_t) common.Err_t {
	istats.nistat++
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
		pari, err := c.ilookup("..")
		if err != 0 {
			panic(".. must exist")
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

func (idm *imemnode_t) _linkdown() {
	idm.links--
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
	inode := inode_t{blk, ioffset(inum)}
	ic.itype = inode.itype()
	if ic.itype <= I_FIRST || ic.itype > I_VALID {
		fmt.Printf("itype: %v for %v\n", ic.itype, inum)
		panic("bad itype in fill")
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
			idm.fs.bcache.Relse(indblk, "indblk")
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
			idm.fs.bcache.Relse(dindblk, "dindblk")

			indblk, err := idm.mbread(indno)
			if err != 0 {
				return 0, err
			}
			blkn, err := idm.ensureind(indblk, fbn%INDADDR, writing)
			idm.fs.bcache.Relse(indblk, "indblk2")
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
			istats.nfillhole++
		} else if writing {
			istats.ngrow++
		}
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

// Takes as input the file offset and whether the operation is a write and
// returns the block number of the block responsible for that offset.
func (idm *imemnode_t) offsetblk(offset int, writing bool) (int, common.Err_t) {
	whichblk := offset / common.BSIZE
	lastblk := idm.size / common.BSIZE
	blkn, err := idm.bmapfill(lastblk, whichblk, writing)
	if err != 0 {
		return blkn, err
	}
	if blkn <= 0 || blkn >= idm.fs.superb.lastblock() {
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
	istats.niread++
	isz := idm.size
	c := 0
	for offset < isz && dst.Remain() != 0 {
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
		idm.fs.bcache.Relse(b, "_iread")
		if err != 0 {
			return c, err
		}
	}
	return c, 0
}

func (idm *imemnode_t) iwrite(src common.Userio_i, offset int, n int) (int, common.Err_t) {
	istats.niwrite++
	sz := min(src.Totalsz(), n)
	newsz := offset + sz
	c := 0
	for c < sz {
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
		idm.fs.bcache.Relse(b, "iwrite")
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
	istats.nitrunc++
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
	ni := &inode_t{ib, ioffset(childi)}
	ni.w_itype(I_DEAD)
	ib.Unlock()
	idm.fs.bcache.Relse(ib, "create_undo")
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

	istats.nicreate++

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
		newinode.w_itype(I_DEAD)
		idm.fs.ialloc.Ifree(newinum)
	}
	idm.fs.fslog.Write(newiblk)
	idm.fs.bcache.Relse(newiblk, "icreate")
	return newinum, err
}

func (idm *imemnode_t) immapinfo(offset, len int, inc bool) ([]common.Mmapinfo_t, common.Err_t) {
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

	istats.nimmap++
	o := common.Rounddown(offset, common.PGSIZE)
	len = common.Roundup(offset+len, common.PGSIZE) - o
	pgc := len / common.PGSIZE
	ret := make([]common.Mmapinfo_t, pgc)
	for i := 0; i < len; i += common.PGSIZE {
		buf, err := idm.off2buf(o+i, common.PGSIZE, false, true, "immapinfo")
		if err != 0 {
			return nil, err
		}
		buf.Unlock()
		if inc { // the VM system is going to use the page
			// XXX don't release buffer (i.e., pin in cache)
			// so that writes make it to the file. modify vm
			// system to call Bcache.Relse.
			// mem.Refup(buf.Pa)
		}
		pgn := i / common.PGSIZE
		wpg := (*common.Pg_t)(unsafe.Pointer(buf.Data))
		ret[pgn].Pg = wpg
		ret[pgn].Phys = buf.Pa
		// release buffer. the VM system will increase the page count.
		// XXX race? vm system may not do this for a while. maybe we
		// should increase page count here.
		idm.fs.bcache.Relse(buf, "immapinfo")
	}
	return ret, 0
}

func (idm *imemnode_t) idibread() (*common.Bdev_block_t, common.Err_t) {
	return idm.fs.fslog.Get_fill(idm.fs.ialloc.Iblock(idm.inum), "idibread", true)
}

// frees all blocks occupied by idm
func (idm *imemnode_t) ifree() common.Err_t {
	istats.nifree++
	if fs_debug {
		fmt.Printf("ifree: %d\n", idm.inum)
	}

	allb := make([]int, 0, 10)
	add := func(blkno int) {
		if blkno != 0 {
			allb = append(allb, blkno)
		}
	}
	freeind := func(indno int) common.Err_t {
		if indno == 0 {
			return 0
		}
		add(indno)
		blk, err := idm.mbread(indno)
		if err != 0 {
			return err
		}
		data := blk.Data[:]
		for i := 0; i < INDADDR; i++ {
			off := i * 8
			nblkno := common.Readn(data, 8, off)
			add(nblkno)
		}
		idm.fs.bcache.Relse(blk, "ifree1")
		return 0
	}

	for i := 0; i < NIADDRS; i++ {
		add(idm.addrs[i])
	}
	err := freeind(idm.indir)
	if err != 0 {
		panic("freeind")
	}
	dindno := idm.dindir
	if dindno != 0 {
		add(dindno)
		blk, err := idm.mbread(dindno)
		if err != 0 {
			return err
		}
		data := blk.Data[:]
		for i := 0; i < INDADDR; i++ {
			off := i * 8
			nblkno := common.Readn(data, 8, off)
			if nblkno != 0 {
				err := freeind(nblkno)
				if err != 0 {
					panic("freeind")
				}
			}
		}
		idm.fs.bcache.Relse(blk, "dindno")
	}

	iblk, err := idm.idibread()
	if err != 0 {
		return err
	}
	if iblk == nil {
		panic("ifree")
	}
	idm.itype = I_DEAD
	idm.flushto(iblk, idm.inum)
	iblk.Unlock()
	idm.fs.fslog.Write(iblk)
	idm.fs.bcache.Relse(iblk, "ifree2")

	idm.fs.ialloc.Ifree(idm.inum)

	// could batch free
	for _, blkno := range allb {
		idm.fs.balloc.Bfree(blkno)
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

// XXX: remove
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
	refcache *refcache_t
	fs       *Fs_t
}

func mkIcache(fs *Fs_t) *icache_t {
	icache := &icache_t{}
	icache.refcache = mkRefcache(common.Syslimit.Vnodes, false)
	icache.fs = fs
	return icache
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
	ref, err := icache.refcache.Lookup(int(inum), s)
	if err != 0 {
		return nil, err
	}
	defer ref.Unlock()

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
			// delete(refcache.refs, inum)
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
	icache.refcache.Refdown(imem, "fs_rename_opar")
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

type iallocater_t struct {
	alloc *allocater_t
	first int
}

func mkIalloc(fs *Fs_t, start, len, first int) *iallocater_t {
	ialloc := &iallocater_t{}
	ialloc.alloc = mkAllocater(fs, start, len)
	ialloc.first = first
	return ialloc
}

func (ialloc *iallocater_t) Ialloc() (common.Inum_t, common.Err_t) {
	n, err := ialloc.alloc.Alloc()
	inum := common.Inum_t(n)
	if err != 0 {
		return 0, err
	}
	if fs_debug {
		fmt.Printf("ialloc %d\n", inum)
	}
	return inum, 0
}

func (ialloc *iallocater_t) Ifree(inum common.Inum_t) common.Err_t {
	if fs_debug {
		fmt.Printf("ifree: mark free %d\n", inum)
	}
	return ialloc.alloc.Free(int(inum))
}

func (ialloc *iallocater_t) Iblock(inum common.Inum_t) int {
	b := int(inum) / (common.BSIZE / ISIZE)
	b += ialloc.first
	return b
}

func ioffset(inum common.Inum_t) int {
	o := int(inum) % (common.BSIZE / ISIZE)
	return o
}

func (ialloc *iallocater_t) Stats() string {
	return "inode " + ialloc.alloc.Stats()
}
