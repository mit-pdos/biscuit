package fs

import "common"

const NAME_MAX int = 512

var lhits = 0

func crname(path common.Ustr, nilpatherr common.Err_t) (common.Err_t, bool) {
	if len(path) == 0 {
		return nilpatherr, false
	} else if path.Isdot() || path.Isdotdot() {
		return -common.EINVAL, false
	}
	return 0, true
}

// directory data format
// 0-13,  file name characters
// 14-21, inode block/offset
// ...repeated, totaling 23 times
type Dirdata_t struct {
	Data []uint8
}

const (
	DNAMELEN = 14
	NDBYTES  = 22
	NDIRENTS = BSIZE / NDBYTES
)

func doffset(didx int, off int) int {
	if didx < 0 || didx >= NDIRENTS {
		panic("bad dirent index")
	}
	return NDBYTES*didx + off
}

func (dir *Dirdata_t) Filename(didx int) common.Ustr {
	st := doffset(didx, 0)
	sl := dir.Data[st : st+DNAMELEN]
	ret := make([]byte, 0, 14)
	for _, c := range sl {
		if c == 0 {
			break
		}
		ret = append(ret, c)
	}
	return common.Ustr(ret)
}

func (dir *Dirdata_t) inodenext(didx int) common.Inum_t {
	st := doffset(didx, 14)
	v := common.Readn(dir.Data[:], 8, st)
	return common.Inum_t(v)
}

func (dir *Dirdata_t) W_filename(didx int, fn common.Ustr) {
	st := doffset(didx, 0)
	sl := dir.Data[st : st+DNAMELEN]
	l := len(fn)
	for i := range sl {
		if i >= l {
			sl[i] = 0
		} else {
			sl[i] = fn[i]
		}
	}
}

func (dir *Dirdata_t) W_inodenext(didx int, inum common.Inum_t) {
	st := doffset(didx, 14)
	common.Writen(dir.Data[:], 8, st, int(inum))
}

type fdent_t struct {
	offset int
	next   *fdent_t
}

// linked list of free directory entries
type fdelist_t struct {
	head *fdent_t
	n    int
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
	offset int
	inum   common.Inum_t
	idm    *imemnode_t
}

// returns the offset of an empty directory entry. returns error if failed to
// allocate page for the new directory entry.
func (idm *imemnode_t) _denextempty(opid opid_t) (int, common.Err_t) {
	dc := &idm.dentc
	if ret, ok := dc.freel.remhead(); ok {
		return ret.offset, 0
	}

	// see if we have an empty slot before expanding the directory
	if !idm.dentc.scanned {
		var de *icdent_t
		found, err := idm._descan(opid, func(fn common.Ustr, tde *icdent_t) bool {
			if len(fn) == 0 {
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
	// there are no free directory entries. there is no need to scan the
	// blocks even again since we can allocate all free directory entries
	// from the free list.
	idm.dentc.scanned = true

	// current dir blocks are full -- allocate new dirdata block
	newsz := idm.size + BSIZE
	b, err := idm.off2buf(opid, idm.size, BSIZE, true, true, "_denextempty")
	if err != 0 {
		return 0, err
	}
	newoff := idm.size
	// start from 1 since we return slot 0 directly
	for i := 1; i < NDIRENTS; i++ {
		noff := newoff + NDBYTES*i
		idm._deaddempty(noff)
	}

	b.Unlock()
	idm.fs.fslog.Write(opid, b) // log empty dir block, later writes absorpt it hopefully
	idm.fs.fslog.Relse(b, "_denextempty")

	idm.size = newsz
	return newoff, 0
}

// if _deinsert fails to allocate a page, idm is left unchanged.
func (idm *imemnode_t) _deinsert(opid opid_t, name common.Ustr, inum common.Inum_t) common.Err_t {
	noff, err := idm._denextempty(opid)
	if err != 0 {
		return err
	}
	// dennextempty() made the slot so we won't fill
	b, err := idm.off2buf(opid, noff, NDBYTES, true, true, "_deinsert")
	if err != 0 {
		return err
	}
	ddata := Dirdata_t{b.Data[noff%common.PGSIZE:]}

	ddata.W_filename(0, name)
	ddata.W_inodenext(0, inum)

	b.Unlock()
	idm.fs.fslog.Write(opid, b)
	idm.fs.fslog.Relse(b, "_deinsert")

	icd := &icdent_t{noff, inum, nil}
	ok := idm._dceadd(name, icd)
	dc := &idm.dentc
	dc.haveall = dc.haveall && ok

	return 0
}

// calls f on each directory entry (including empty ones) until f returns true
// or it has been called on all directory entries. _descan returns true if f
// returned true.
func (idm *imemnode_t) _descan(opid opid_t, f func(fn common.Ustr, de *icdent_t) bool) (bool, common.Err_t) {
	found := false
	for i := 0; i < idm.size; i += BSIZE {
		if !common.Resadd_noblock(common.Bounds(common.B_IMEMNODE_T__DESCAN)) {
			return false, -common.ENOHEAP
		}
		b, err := idm.off2buf(opid, i, BSIZE, false, true, "_descan")
		if err != 0 {
			return false, err
		}
		dd := Dirdata_t{b.Data[:]}
		for j := 0; j < NDIRENTS; j++ {
			tfn := dd.Filename(j)
			tpriv := dd.inodenext(j)
			tde := &icdent_t{i + j*NDBYTES, tpriv, nil}
			if f(tfn, tde) {
				found = true
				break
			}
		}
		b.Unlock()
		idm.fs.fslog.Relse(b, "_descan")
	}
	return found, 0
}

func (idm *imemnode_t) _delookup(opid opid_t, fn common.Ustr) (*icdent_t, common.Err_t) {
	if len(fn) == 0 {
		panic("bad lookup")
	}
	if de, ok := idm.dentc.dents.Get(fn); ok {
		return de.(*icdent_t), 0
	}
	var zi *icdent_t
	if idm.dentc.haveall {
		// cache negative entries?
		return zi, -common.ENOENT
	}

	// not in cached dirents
	found := false
	haveall := true
	var de *icdent_t
	_, err := idm._descan(opid, func(tfn common.Ustr, tde *icdent_t) bool {
		if len(tfn) == 0 {
			return false
		}
		if tfn.Eq(fn) {
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
	idm.dentc.haveall = haveall
	if !found {
		return zi, -common.ENOENT
	}
	return de, 0
}

func (idm *imemnode_t) _deremove(opid opid_t, fn common.Ustr) (*icdent_t, common.Err_t) {
	var zi *icdent_t
	de, err := idm._delookup(opid, fn)
	if err != 0 {
		return zi, err
	}
	if idm.fs.diskfs {
		b, err := idm.off2buf(opid, de.offset, NDBYTES, true, true, "_deremove")
		if err != 0 {
			return zi, err
		}
		dirdata := Dirdata_t{b.Data[de.offset%common.PGSIZE:]}
		dirdata.W_filename(0, common.MkUstr())
		dirdata.W_inodenext(0, common.Inum_t(0))
		b.Unlock()
		idm.fs.fslog.Write(opid, b)
		idm.fs.fslog.Relse(b, "_deremove")
	}
	// add back to free dents
	idm.dentc.dents.Del(fn)
	idm._deaddempty(de.offset)
	return de, 0
}

// returns true if idm, a directory, is empty (excluding ".." and ".").
func (idm *imemnode_t) _deempty(opid opid_t) (bool, common.Err_t) {
	if idm.dentc.haveall {
		dentc := &idm.dentc
		hasfiles := dentc.dents.Iter(func(key interface{}, v interface{}) bool {
			dn := key.(common.Ustr)
			if !dn.Isdot() && !dn.Isdotdot() {
				return true
			}
			return false
		})
		return !hasfiles, 0
	}
	notempty, err := idm._descan(opid, func(fn common.Ustr, de *icdent_t) bool {
		return len(fn) != 0 && !fn.Isdot() && fn.Isdotdot()
	})
	if err != 0 {
		return false, err
	}
	return !notempty, 0
}

// empties the dirent cache, returning the number of dents purged.
func (idm *imemnode_t) _derelease() int {
	dc := &idm.dentc
	dc.haveall = false
	dc.dents = nil
	dc.freel.head = nil
	ret := dc.max
	common.Syslimit.Dirents.Given(uint(ret))
	dc.max = 0
	return ret
}

// ensure that an insert/unlink cannot fail i.e. fail to allocate a page. if fn
// == "", look for an empty dirent, otherwise make sure the page containing fn
// is in the page cache.
func (idm *imemnode_t) _deprobe(opid opid_t, fn common.Ustr) (*Bdev_block_t, common.Err_t) {
	if len(fn) != 0 {
		de, err := idm._delookup(opid, fn)
		if err != 0 {
			return nil, err
		}
		noff := de.offset
		b, err := idm.off2buf(opid, noff, NDBYTES, true, true, "_deprobe_fn")
		b.Unlock()
		return b, err
	}
	noff, err := idm._denextempty(opid)
	if err != 0 {
		return nil, err
	}
	b, err := idm.off2buf(opid, noff, NDBYTES, true, true, "_deprobe_nil")
	if err != 0 {
		return nil, err
	}
	b.Unlock()
	idm._deaddempty(noff)
	return b, 0
}

// returns true if this idm has enough free cache space for a single dentry
func (idm *imemnode_t) _demayadd() bool {
	dc := &idm.dentc
	//have := len(dc.dents) + len(dc.freem)
	// have := dc.dents.nodes + dc.freel.count()
	have := dc.freel.count()
	if have+1 < dc.max {
		return true
	}
	// reserve more directory entries
	take := 64
	if common.Syslimit.Dirents.Taken(uint(take)) {
		dc.max += take
		return true
	}
	lhits++
	return false
}

// caching is best-effort. returns true if fn was added to the cache
func (idm *imemnode_t) _dceadd(fn common.Ustr, de *icdent_t) bool {
	dc := &idm.dentc
	if !idm._demayadd() {
		return false
	}
	dc.dents.Set(fn, de)
	return true
}

func (idm *imemnode_t) _deaddempty(off int) {
	if !idm._demayadd() {
		return
	}
	dc := &idm.dentc
	dc.freel.addhead(off)
}

// guarantee that there is enough memory to insert at least one directory
// entry.
func (idm *imemnode_t) probe_insert(opid opid_t) (*Bdev_block_t, common.Err_t) {
	// insert and remove a fake directory entry, forcing a page allocation
	// if necessary.
	b, err := idm._deprobe(opid, common.MkUstr())
	if err != 0 {
		return nil, err
	}
	return b, 0
}

// guarantee that there is enough memory to unlink a dirent (which may require
// allocating a page to load the dirents from disk).
func (idm *imemnode_t) probe_unlink(opid opid_t, fn common.Ustr) (*Bdev_block_t, common.Err_t) {
	b, err := idm._deprobe(opid, fn)
	if err != 0 {
		return nil, err
	}
	return b, 0
}

func (idm *imemnode_t) ilookup(opid opid_t, name common.Ustr) (*imemnode_t, common.Err_t) {
	// did someone confuse a file with a directory?
	if idm.itype != I_DIR {
		return nil, -common.ENOTDIR
	}
	de, err := idm._delookup(opid, name)
	if err != 0 {
		return nil, err
	}
	if de.idm == nil {
		de.idm = idm.fs.icache.Iref(de.inum, "ilookup")
	}
	de.idm.Refup("ilookup")
	return de.idm, 0
}

func (idm *imemnode_t) ilookup_lockfree(name common.Ustr) (*imemnode_t, bool) {
	if idm.dentc.dents == nil {
		return nil, false
	}
	if e, ok := idm.dentc.dents.Get(name); ok {
		de := e.(*icdent_t)
		if de.idm == nil { // XXX maybe use atomic load?
			de.idm = idm.fs.icache.Iref(de.inum, "ilookup")
		}
		if de.idm != nil {
			de.idm.Refup("ilookup")
			return de.idm, true
		}
	}
	return nil, false
}

// creates a new directory entry with name "name" and inode number priv
func (idm *imemnode_t) iinsert(opid opid_t, name common.Ustr, inum common.Inum_t) common.Err_t {
	if idm.itype != I_DIR {
		return -common.ENOTDIR
	}
	if _, err := idm._delookup(opid, name); err == 0 {
		return -common.EEXIST
	} else if err != -common.ENOENT {
		return err
	}
	if inum < 0 {
		panic("iinsert")
	}
	err := idm._deinsert(opid, name, inum)
	return err
}

// returns inode number of unliked inode so caller can decrement its ref count
func (idm *imemnode_t) iunlink(opid opid_t, name common.Ustr) (common.Inum_t, common.Err_t) {
	if idm.itype != I_DIR {
		panic("unlink to non-dir")
	}
	de, err := idm._deremove(opid, name)
	if err != 0 {
		return 0, err
	}
	if de.idm != nil {
		de.idm.Refdown("iunlink")
	}
	return de.inum, 0
}

// returns true if the inode has no directory entries
func (idm *imemnode_t) idirempty(opid opid_t) (bool, common.Err_t) {
	if idm.itype != I_DIR {
		panic("i am not a dir")
	}
	return idm._deempty(opid)
}
