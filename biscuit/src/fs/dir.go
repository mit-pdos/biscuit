package fs

import "fmt"
import "sync/atomic"
import "unsafe"

import "bounds"
import "defs"
import "hashtable"
import "mem"
import "res"
import "ustr"
import "util"

const NAME_MAX int = 512

var lhits = 0

func crname(path ustr.Ustr, nilpatherr defs.Err_t) (defs.Err_t, bool) {
	if len(path) == 0 {
		return nilpatherr, false
	} else if path.Isdot() || path.Isdotdot() {
		return -defs.EINVAL, false
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

func (dir *Dirdata_t) Filename(didx int) ustr.Ustr {
	st := doffset(didx, 0)
	sl := dir.Data[st : st+DNAMELEN]
	ret := make([]byte, 0, 14)
	for _, c := range sl {
		if c == 0 {
			break
		}
		ret = append(ret, c)
	}
	return ustr.Ustr(ret)
}

func (dir *Dirdata_t) inodenext(didx int) defs.Inum_t {
	st := doffset(didx, 14)
	v := util.Readn(dir.Data[:], 8, st)
	return defs.Inum_t(v)
}

func (dir *Dirdata_t) W_filename(didx int, fn ustr.Ustr) {
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

func (dir *Dirdata_t) W_inodenext(didx int, inum defs.Inum_t) {
	st := doffset(didx, 14)
	util.Writen(dir.Data[:], 8, st, int(inum))
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

// struct to hold the offset, inum of directory entry slots
type icdent_t struct {
	offset int
	inum   defs.Inum_t
	// idm may be nil since it is lazily filled on ilookup
	idm    *imemnode_t
	name   ustr.Ustr
}

func (de *icdent_t) String() string {
	s := ""
	s += fmt.Sprintf("%d %s", de.inum, de.name)
	return s
}

// returns the offset of an empty directory entry. returns error if failed to
// allocate page for the new directory entry.
func (idm *imemnode_t) _denextempty(opid opid_t) (int, defs.Err_t) {
	dc := &idm.dentc
	if ret, ok := dc.freel.remhead(); ok {
		return ret.offset, 0
	}

	// see if we have an empty slot before expanding the directory
	if !idm.dentc.scanned {
		var de *icdent_t
		found, err := idm._descan(opid, func(fn ustr.Ustr, tde *icdent_t) bool {
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

	// current dir blocks are full -- allocate new dirdata block. because
	// the offset of the new block is larger than idm.size, off2buf will
	// zero-fill the block.
	newsz := idm.size + BSIZE
	b, err := idm.off2buf(opid, idm.size, BSIZE, true, true, "_denextempty")
	if err != 0 {
		return 0, err
	}
	newoff := idm.size
	// skip 0 since we will allocate it immediately
	for i := NDIRENTS - 1; i > 0; i-- {
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
func (idm *imemnode_t) _deinsert(opid opid_t, name ustr.Ustr, inum defs.Inum_t) defs.Err_t {
	if !idm._amlocked {
		panic("laksdj")
	}
	noff, err := idm._denextempty(opid)
	if err != 0 {
		return err
	}
	// dennextempty() made the slot so we won't fill
	b, err := idm.off2buf(opid, noff, NDBYTES, true, true, "_deinsert")
	if err != 0 {
		return err
	}
	ddata := Dirdata_t{b.Data[noff%mem.PGSIZE:]}

	ddata.W_filename(0, name)
	ddata.W_inodenext(0, inum)

	b.Unlock()
	idm.fs.fslog.Write(opid, b)
	idm.fs.fslog.Relse(b, "_deinsert")

	icd := &icdent_t{offset: noff, inum: inum, name: name}
	ok := idm._dceadd(name, icd)
	dc := &idm.dentc
	dc.haveall = dc.haveall && ok
	return 0
}

// calls f on each directory entry (including empty ones) until f returns true
// or it has been called on all directory entries. _descan returns true if f
// returned true.
func (idm *imemnode_t) _descan(opid opid_t, f func(fn ustr.Ustr, de *icdent_t) bool) (bool, defs.Err_t) {
	if !idm._amlocked {
		panic("lsjdf")
	}
	found := false
	for i := 0; i < idm.size; i += BSIZE {
		if !res.Resadd_noblock(bounds.Bounds(bounds.B_IMEMNODE_T__DESCAN)) {
			return false, -defs.ENOHEAP
		}
		b, err := idm.off2buf(opid, i, BSIZE, false, true, "_descan")
		if err != 0 {
			return false, err
		}
		dd := Dirdata_t{b.Data[:]}
		for j := 0; j < NDIRENTS; j++ {
			tfn := dd.Filename(j)
			tpriv := dd.inodenext(j)
			tde := &icdent_t{offset: i + j*NDBYTES, inum: tpriv, name: tfn}
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

func (idm *imemnode_t) _delookup(opid opid_t, fn ustr.Ustr) (*icdent_t, defs.Err_t) {
	if !idm._amlocked {
		panic("lsjdf")
	}
	if len(fn) == 0 {
		panic("bad lookup")
	}
	if idm.dentc.dents == nil {
		idm.dentc.dents = hashtable.MkHash(100)
	}
	if de, ok := idm.dentc.dents.Get(fn); ok {
		return de.(*icdent_t), 0
	}
	var zi *icdent_t
	if idm.dentc.haveall {
		// cache negative entries?
		return zi, -defs.ENOENT
	}

	// not in cached dirents
	found := false
	haveall := true
	var de *icdent_t
	_, err := idm._descan(opid, func(tfn ustr.Ustr, tde *icdent_t) bool {
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
		return zi, -defs.ENOENT
	}
	return de, 0
}

func (idm *imemnode_t) _deremove(opid opid_t, fn ustr.Ustr) (*icdent_t, defs.Err_t) {
	var zi *icdent_t
	de, err := idm._delookup(opid, fn)
	if err != 0 {
		return zi, err
	}
	// it's good for the memory FS performance to skip updating the data
	// blocks, but our implementation of readdir(3) requires it (because it
	// reads the directory's contents from the data blocks). therefore,
	// make the data block update unconditional.
	if true || idm.fs.diskfs {
		b, err := idm.off2buf(opid, de.offset, NDBYTES, true, true, "_deremove")
		if err != 0 {
			return zi, err
		}
		dirdata := Dirdata_t{b.Data[de.offset%mem.PGSIZE:]}
		dirdata.W_filename(0, ustr.MkUstr())
		dirdata.W_inodenext(0, defs.Inum_t(0))
		b.Unlock()
		idm.fs.fslog.Write(opid, b)
		idm.fs.fslog.Relse(b, "_deremove")
	}
	idm._deremove_dent(de)
	idm._deaddempty(de.offset)
	return de, 0
}

func (idm *imemnode_t) _deremove_dent(de *icdent_t) {
	if idm.dentc.dents != nil {
		idm.dentc.dents.Del(de.name)
	}
}

// returns true if idm, a directory, is empty (excluding ".." and ".").
func (idm *imemnode_t) _deempty(opid opid_t) (bool, defs.Err_t) {
	if idm.dentc.haveall {
		dentc := &idm.dentc
		hasfiles := dentc.dents.Iter(func(key interface{}, v interface{}) bool {
			dn := key.(ustr.Ustr)
			if !dn.Isdot() && !dn.Isdotdot() {
				return true
			}
			return false
		})
		return !hasfiles, 0
	}
	notempty, err := idm._descan(opid, func(fn ustr.Ustr, de *icdent_t) bool {
		return len(fn) != 0 && !fn.Isdot() && !fn.Isdotdot()
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
	dc.max = 0
	return ret
}

// ensure that an insert/unlink cannot fail i.e. fail to allocate a page. if fn
// == "", look for an empty dirent, otherwise make sure the page containing fn
// is in the page cache.
func (idm *imemnode_t) _deprobe(opid opid_t, fn ustr.Ustr) (*Bdev_block_t, defs.Err_t) {
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

// caching is best-effort. returns true if fn was added to the cache
func (idm *imemnode_t) _dceadd(fn ustr.Ustr, de *icdent_t) bool {
	dc := &idm.dentc
	dc.dents.Set(fn, de)
	return true
}

func (idm *imemnode_t) _deaddempty(off int) {
	dc := &idm.dentc
	dc.freel.addhead(off)
}

// guarantee that there is enough memory to insert at least one directory
// entry.
func (idm *imemnode_t) probe_insert(opid opid_t) (*Bdev_block_t, defs.Err_t) {
	// insert and remove a fake directory entry, forcing a page allocation
	// if necessary.
	b, err := idm._deprobe(opid, ustr.MkUstr())
	if err != 0 {
		return nil, err
	}
	return b, 0
}

// guarantee that there is enough memory to unlink a dirent (which may require
// allocating a page to load the dirents from disk).
func (idm *imemnode_t) probe_unlink(opid opid_t, fn ustr.Ustr) (*Bdev_block_t, defs.Err_t) {
	b, err := idm._deprobe(opid, fn)
	if err != 0 {
		return nil, err
	}
	return b, 0
}

// idm must be locked. if ilookup succeeds, it increments the refcount of the
// target imemnode and returns it unlocked (even for ".")
func (idm *imemnode_t) ilookup(opid opid_t, name ustr.Ustr) (*imemnode_t, defs.Err_t) {
	if !idm._amlocked {
		panic("laksdj")
	}
	// did someone confuse a file with a directory?
	if idm.itype != I_DIR {
		return nil, -defs.ENOTDIR
	}
	de, err := idm._delookup(opid, name)
	if err != 0 {
		return nil, err
	}
	if de.idm == nil {
		var im *imemnode_t
		if name.Isdot() {
			im = idm
			if _, ok := im.Refup(""); !ok {
				panic("parent reffed, must be in icache")
			}
		} else {
			im = idm.fs.icache.Iref(de.inum, "ilookup")
		}
		atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&de.idm)), unsafe.Pointer(im))
	} else {
		if _, ok := de.idm.Refup("ilookup"); !ok {
			// target imemnode was evicted
			i := idm.fs.icache.Iref(de.inum, "ilookup")
			atomic.StorePointer((*unsafe.Pointer)(unsafe.Pointer(&de.idm)), unsafe.Pointer(i))
		}
	}
	return de.idm, 0
}

// both idm and child are locked
func (idm *imemnode_t) ilookup_validate(opid opid_t, name ustr.Ustr, child *imemnode_t) (bool, defs.Err_t) {
	de, err := idm._delookup(opid, name)
	if err != 0 {
		return false, err
	}
	if de.idm == nil {
		if name.Isdot() {
			de.idm = idm
		} else {
			de.idm = idm.fs.icache.Iref(de.inum, "ilookup")
		}
	}
	return de.idm.inum == child.inum, 0
}

// Lookup name in a directory's dcache without holding lock. If verify is false,
// then lookup just returns the found inode. This inode may be stale (it may
// have been deleted from the inode cache) and the caller must be prepared to
// deal with stale inodes (and, in particular, never update it).  If verify is
// true, then lookup will return an inode referenced and locked which is
// guaranteed to be unlinked. For this case, there are two potential races to
// consider: 1) an unlink removes the entry from dcache; 2) evict removes the
// inode from the inode cache. For unlink, lock-free namei will check the link
// count after locking the inode and fail if the file was unlinked. On
// eviction, the refcnt is marked as being deleted, and Refup will return false
// and ilookup_lockfree will fail.
func (idm *imemnode_t) ilookup_lockfree(name ustr.Ustr, verify bool) (*imemnode_t, bool) {
	yay := (*unsafe.Pointer)(unsafe.Pointer(&idm.dentc.dents))
	dc := (*hashtable.Hashtable_t)(atomic.LoadPointer(yay))
	if dc == nil {
		return nil, false
	}
	if e, ok := dc.Get(name); ok {
		de := e.(*icdent_t)
		p := atomic.LoadPointer((*unsafe.Pointer)(unsafe.Pointer(&de.idm)))
		v := (*imemnode_t)(p)
		if v == nil {
			return nil, false
		}
		if verify {
			// lock inode before increasing the refcount, to
			// guarantee that a concurrent unlink must observe the
			// 0 linkcount and 0 refcount.
			v.ilock("")
			if v.links == 0 {
				// this inode has been unlinked and the
				// unlinking operation must observe this and
				// free its blocks.
				v.iunlock("")
				return nil, false
			}
			// or it may have been evicted
			if _, ok := v.Refup(""); !ok {
				v.iunlock("")
				return nil, false
			}
		}
		return v, true
	}
	return nil, false
}

// creates a new directory entry with name "name" and inode number priv
func (idm *imemnode_t) iinsert(opid opid_t, name ustr.Ustr, inum defs.Inum_t) defs.Err_t {
	if idm.itype != I_DIR {
		return -defs.ENOTDIR
	}
	if _, err := idm._delookup(opid, name); err == 0 {
		return -defs.EEXIST
	} else if err != -defs.ENOENT {
		return err
	}
	if inum < 0 {
		panic("iinsert")
	}
	err := idm._deinsert(opid, name, inum)
	return err
}

// returns inode number of unliked inode so caller can decrement its ref count
func (idm *imemnode_t) iunlink(opid opid_t, name ustr.Ustr) (defs.Inum_t, defs.Err_t) {
	if idm.itype != I_DIR {
		panic("unlink to non-dir")
	}
	de, err := idm._deremove(opid, name)
	if err != 0 {
		return 0, err
	}
	return de.inum, 0
}

// returns true if the inode has no directory entries
func (idm *imemnode_t) idirempty(opid opid_t) (bool, defs.Err_t) {
	if idm.itype != I_DIR {
		panic("i am not a dir")
	}
	return idm._deempty(opid)
}
