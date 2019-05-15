package fs

import "fmt"
import "sync"

import "bounds"
import "bpath"
import "defs"
import "fd"
import "fdops"
import "limits"
import "mem"
import "proc"
import "res"
import "stat"
import "stats"
import "ustr"
import "util"

const fs_debug = false
const FSOFF = 506
const iroot = defs.Inum_t(0)

var cons proc.Cons_i

type Fs_t struct {
	ahci         Disk_i
	superb_start int
	superb       Superblock_t
	bcache       *bcache_t
	icache       *icache_t
	fslog        *log_t
	ialloc       *ibitmap_t
	balloc       *bbitmap_t
	istats       *inode_stats_t
	root         *imemnode_t
	diskfs       bool // disk or in-mem file system?
}

func StartFS(mem Blockmem_i, disk Disk_i, console proc.Cons_i, diskfs bool) (*fd.Fd_t, *Fs_t) {

	if mem == nil || disk == nil || console == nil {
		panic("nil arg")
	}
	cons = console

	// reset taken
	limits.Syslimit = limits.MkSysLimit()

	fs := &Fs_t{}
	fs.diskfs = diskfs
	fs.ahci = disk
	fs.istats = &inode_stats_t{}
	if !fs.diskfs {
		fmt.Printf("Using MEMORY FS\n")
	}

	fs.bcache = mkBcache(mem, disk)

	// find the first fs block; the build system installs it in block 0 for
	// us
	b := fs.bcache.Get_fill(0, "fsoff", false)
	fs.superb_start = util.Readn(b.Data[:], 4, FSOFF)
	//fmt.Printf("fs.superb_start %v\n", fs.superb_start)
	if fs.superb_start <= 0 {
		panic("bad superblock start")
	}
	fs.bcache.Relse(b, "fs_init")

	// superblock is never changed, so reading before recovery is fine
	b = fs.bcache.Get_fill(fs.superb_start, "super", false) // don't relse b, because superb is global

	fs.superb = Superblock_t{b.Data}

	logstart := fs.superb_start + 1
	loglen := fs.superb.Loglen()
	fs.fslog = StartLog(logstart, loglen, fs.bcache, fs.diskfs)
	if fs.fslog == nil {
		panic("Startlog failed")
	}

	iorphanstart := fs.superb.Iorphanblock()
	iorphanlen := fs.superb.Iorphanlen()
	imapstart := iorphanstart + iorphanlen
	imaplen := fs.superb.Imaplen()
	//fmt.Printf("orphanstart %v orphan len %v\n", iorphanstart, iorphanlen)
	//fmt.Printf("imapstart %v imaplen %v\n", imapstart, imaplen)
	if iorphanlen != imaplen {
		panic("number of iorphan map blocks != inode map block")
	}

	bmapstart := fs.superb.Freeblock()
	bmaplen := fs.superb.Freeblocklen()
	//fmt.Printf("bmapstart %v bmaplen %v\n", bmapstart, bmaplen)

	inodelen := fs.superb.Inodelen()
	//fmt.Printf("inodestart %v inodelen %v\n", bmapstart+bmaplen, inodelen)

	fs.ialloc = mkIalloc(fs, imapstart, imaplen, bmapstart+bmaplen, inodelen)
	fs.balloc = mkBallocater(fs, bmapstart, bmaplen, bmapstart+bmaplen+inodelen)

	fs.icache = mkIcache(fs, iorphanstart, iorphanlen)
	fs.icache.RecoverOrphans()

	fs.Fs_sync() // commits ifrees() and clears orphan bitmap

	fs.root = fs.icache.Iref(iroot, "fs_namei_root")

	return &fd.Fd_t{Fops: &fsfops_t{priv: iroot, fs: fs, count: 1}}, fs
}

func (fs *Fs_t) Sizes() (int, int) {
	return fs.icache.cache.Len(), fs.bcache.cache.Len()
}

func (fs *Fs_t) StopFS() {
	fs.fslog.StopLog()
}

func (fs *Fs_t) Fs_size() (uint, uint) {
	return fs.ialloc.alloc.nfreebits, fs.balloc.alloc.nfreebits
}

func (fs *Fs_t) IrefRoot() *imemnode_t {
	r := fs.root
	r.Refup("IrefRoot")
	return r
}

func (fs *Fs_t) MkRootCwd() *fd.Cwd_t {
	f := &fd.Fd_t{Fops: &fsfops_t{priv: iroot, fs: fs, count: 0}}
	cwd := fd.MkRootCwd(f)
	return cwd
}

func (fs *Fs_t) Unpin(pa mem.Pa_t) {
	fs.bcache.unpin(pa)
}

func (fs *Fs_t) Fs_op_link(old ustr.Ustr, new ustr.Ustr, cwd *fd.Cwd_t) ([]*imemnode_t, defs.Err_t) {
	opid := fs.fslog.Op_begin("Fs_link")
	defer fs.fslog.Op_end(opid)

	if fs_debug {
		fmt.Printf("Fs_link: %v %v %v\n", old, new, cwd)
	}

	fs.istats.Nilink.Inc()

	var deads []*imemnode_t
	orig, dead, err := fs.fs_namei_locked(opid, old, cwd, "Fs_link_org")
	if err != 0 {
		if dead != nil {
			deads = append(deads, dead)
		}
		return deads, err
	}
	if orig.itype != I_FILE {
		if orig.iunlock_refdown("fs_link") {
			deads = append(deads, dead)
		}
		return deads, -defs.EINVAL
	}
	inum := orig.inum
	orig._linkup(opid)
	orig.iunlock("fs_link_orig")

	dirs, fn := bpath.Sdirname(new)
	newd, dead, err := fs.fs_namei_locked(opid, dirs, cwd, "fs_link_newd")
	if err != 0 {
		if dead != nil {
			deads = append(deads, dead)
		}
		goto undo
	}
	err = newd.do_insert(opid, fn, inum)
	newd.iunlock_refdown("fs_link_newd")
	if err != 0 {
		goto undo
	}
	// XXX check for dead and return orig?
	orig.Refdown("fs_link_orig")
	return deads, 0
undo:
	orig.ilock("fs_link_undo")
	orig._linkdown(opid)
	del := orig.iunlock_refdown("fs_link_undo")
	if del {
		deads = append(deads, orig)
	}
	return deads, err
}

func (fs *Fs_t) Fs_link(old ustr.Ustr, new ustr.Ustr, cwd *fd.Cwd_t) defs.Err_t {
	deads, err := fs.Fs_op_link(old, new, cwd)
	for _, dead := range deads {
		dead.Free()
	}
	return err
}

func (fs *Fs_t) Fs_op_unlink(paths ustr.Ustr, cwd *fd.Cwd_t, wantdir bool) (*imemnode_t, defs.Err_t) {
	opid := fs.fslog.Op_begin("fs_unlink")
	defer fs.fslog.Op_end(opid)

	dirs, fn := bpath.Sdirname(paths)
	if fn.Isdot() || fn.Isdotdot() {
		return nil, -defs.EPERM
	}

	fs.istats.Nunlink.Inc()

	if fs_debug {
		fmt.Printf("fs_unlink: %v cwd %v dir? %v\n", paths, cwd, wantdir)
	}

	var child *imemnode_t
	var par *imemnode_t
	var err defs.Err_t

	par, dead, err := fs.fs_namei_locked(opid, dirs, cwd, "fs_unlink_par")
	if err != 0 {
		return dead, err
	}
	child, err = par.ilookup(opid, fn)
	if err != 0 {
		par.iunlock_refdown("fs_unlink_par")
		return dead, err
	}

	// unlock parent after we have a ref to child
	par.iunlock("fs_unlink_par")

	// acquire both locks
	iref_lockall([]*imemnode_t{par, child})
	defer par.iunlock_refdown("fs_unlink_par")

	// recheck if child still exists (same name, same inum), since some
	// other thread may have modifed par but par and child won't disappear
	// because we have references to them.
	ok, err := par.ilookup_validate(opid, fn, child)
	if !ok || err != 0 {
		del := child.iunlock_refdown("fs_unlink_child")
		if del {
			dead = child
		}
		if err != 0 {
			return dead, err
		} else {
			return dead, -defs.ENOENT
		}

	}

	err = child.do_dirchk(opid, wantdir)
	if err != 0 {
		del := child.iunlock_refdown("fs_unlink_child")
		if del {
			dead = child
		}
		return dead, err
	}

	// finally, remove directory entry
	err = par.do_unlink(opid, fn)
	if err != 0 {
		del := child.iunlock_refdown("fs_unlink_child")
		if del {
			dead = child
		}
		fmt.Printf("early 3\n")
		return dead, err
	}
	child._linkdown(opid)
	del := child.iunlock_refdown("fs_unlink_child")
	if del {
		dead = child
	}
	return dead, 0
}

func (fs *Fs_t) Fs_unlink(paths ustr.Ustr, cwd *fd.Cwd_t, wantdir bool) defs.Err_t {
	dead, err := fs.Fs_op_unlink(paths, cwd, wantdir)
	if dead != nil {
		dead.Free()
	}
	return err
}

// per-volume rename mutex. Linux does it so it must be OK!
var _renamelock = sync.Mutex{}

// first return value is inodes to refdown, second return is inode which needs
// to be freed...
func (fs *Fs_t) Fs_op_rename(oldp, newp ustr.Ustr, cwd *fd.Cwd_t) ([]*imemnode_t, *imemnode_t, defs.Err_t) {
	odirs, ofn := bpath.Sdirname(oldp)
	ndirs, nfn := bpath.Sdirname(newp)
	var refs []*imemnode_t

	if err, ok := crname(ofn, -defs.EINVAL); !ok {
		return refs, nil, err
	}
	if err, ok := crname(nfn, -defs.EINVAL); !ok {
		return refs, nil, err
	}

	opid := fs.fslog.Op_begin("fs_rename")
	defer fs.fslog.Op_end(opid)

	fs.istats.Nrename.Inc()

	if fs_debug {
		fmt.Printf("fs_rename: src %v dst %v %v\n", oldp, newp, cwd)
	}

	// lookup all inode references, but we will release locks and lock them
	// together when we know all references.  the references to the inodes
	// cannot disppear, so unlocking temporarily is fine.
	opar, dead, err := fs.fs_namei_locked(opid, odirs, cwd, "fs_rename_opar")
	if err != 0 {
		return refs, dead, err
	}

	ochild, err := opar.ilookup(opid, ofn)
	if err != 0 {
		opar.iunlock_refdown("fs_rename_opar")
		return refs, nil, err
	}

	// unlock par after we have ref to child
	opar.iunlock("fs_rename_par")

	npar, dead, err := fs.fs_namei_locked(opid, ndirs, cwd, "")
	if err != 0 {
		return []*imemnode_t{opar, ochild}, dead, err
	}

	// prevent orphaned loops due to concurrent renames by serializing on
	// this lock; only renames of directories need to be serialized.
	if ochild.itype == I_DIR {
		_renamelock.Lock()
		defer _renamelock.Unlock()
	}

	// _isancestor unlocks npar during the walk. verify that ochild is not
	// an ancestor of npar, since we would disconnect ochild subtree from
	// root.  it is safe to do without holding locks because unlink cannot
	// modify the path to the root by removing a directory because the
	// directories aren't empty.  it could delete npar and an ancestor, but
	// rename has already a reference to to npar.
	if err = fs._isancestor(opid, ochild, npar); err != 0 {
		// XXX no need to check dead?
		opar.Refdown("fs_rename_opar")
		ochild.Refdown("fs_rename_ochild")
		npar.Refdown("fs_rename_npar")
		return refs, nil, err
	}

	var nchild *imemnode_t
	cnt := 0
	// lookup newchild and try to lock all inodes involved
	for {
		gimme := bounds.Bounds(bounds.B_FS_T_FS_OP_RENAME)
		if !res.Resadd_noblock(gimme) {
			opar.Refdown("fs_name_opar")
			ochild.Refdown("fs_name_ochild")
			return refs, nil, -defs.ENOHEAP
		}
		npar.ilock("")
		nchild, err = npar.ilookup(opid, nfn)
		if err != 0 && err != -defs.ENOENT {
			opar.Refdown("fs_name_opar")
			ochild.Refdown("fs_name_ochild")
			npar.iunlock_refdown("fs_name_npar")
			return refs, nil, err
		}
		npar.iunlock("")

		var inodes []*imemnode_t
		var locked []*imemnode_t
		if err == 0 {
			inodes = []*imemnode_t{opar, ochild, npar, nchild}
		} else {
			inodes = []*imemnode_t{opar, ochild, npar}
		}

		locked = iref_lockall(inodes)
		refs = inodes
		for _, v := range locked {
			defer v.iunlock("rename")
		}

		// check if the tree is still the same. an unlink or link may
		// have modified the tree.
		ok, err := opar.ilookup_validate(opid, ofn, ochild)
		if err != 0 {
			return refs, nil, err
		}
		if !ok {
			return refs, nil, -defs.ENOENT
		}
		if nchild == nil {
			childi, err := npar.ilookup(opid, nfn)
			if err == 0 {
				childi.Refdown("rename")
			}
			if err == -defs.ENOENT {
				// it didn't exist before and still doesn't exist
				break
			}
			// XXX pass other errors ups?
		} else {
			ok, err := npar.ilookup_validate(opid, nfn, nchild)
			if err == 0 && ok {
				// it existed before and still exists
				break
			}
			if err == -defs.ENOENT {
				// it existed but now it doesn't.
				break
			}
			// XXX pass other errors ups?
		}
		fmt.Printf("rename: retry; tree changed\n")
		cnt++
		if cnt > 100 {
			panic("rename: panic\n")
		}

		// ochildi changed or childi was created; retry, to grab also its lock
		for _, v := range locked {
			v.iunlock("fs_rename_opar")
		}
		if nchild != nil {
			nchild.Refdown("fs_rename_nchild")
			nchild = nil
		}
	}

	// if src and dst are the same file, we are done
	if nchild != nil && ochild.inum == nchild.inum {
		return refs, nil, 0
	}

	// guarantee that any page allocations will succeed before starting the
	// operation, which will be messy to piece-wise undo.
	b1, err := npar.probe_insert(opid)
	if err != 0 {
		return refs, nil, err
	}
	defer fs.fslog.Relse(b1, "probe_insert")

	b2, err := opar.probe_unlink(opid, ofn)
	if err != 0 {
		return refs, nil, err
	}
	defer fs.fslog.Relse(b2, "probe_unlink_opar")

	odir := ochild.itype == I_DIR
	if odir {
		b3, err := ochild.probe_unlink(opid, ustr.DotDot)
		if err != 0 {
			return refs, nil, err
		}
		defer fs.fslog.Relse(b3, "probe_unlink_ochild")
	}

	if nchild != nil {
		// make sure old and new are either both files or both
		// directories
		if err := nchild.do_dirchk(opid, odir); err != 0 {
			return refs, nil, err
		}

		// remove pre-existing new
		if err = npar.do_unlink(opid, nfn); err != 0 {
			return refs, nil, err
		}
		nchild._linkdown(opid)

	}

	// finally, do the move
	if opar.do_unlink(opid, ofn) != 0 {
		panic("probed")
	}
	if npar.do_insert(opid, nfn, ochild.inum) != 0 {
		panic("probed")
	}

	// update '..'
	if odir {
		dotdot := ustr.DotDot
		if ochild.do_unlink(opid, dotdot) != 0 {
			panic("probed")
		}
		if ochild.do_insert(opid, dotdot, npar.inum) != 0 {
			panic("insert after unlink must succeed")
		}
	}
	return refs, nil, 0
}

func (fs *Fs_t) Fs_rename(oldp, newp ustr.Ustr, cwd *fd.Cwd_t) defs.Err_t {
	refs, dead, err := fs.Fs_op_rename(oldp, newp, cwd)
	for _, r := range refs {
		del := r.Refdown("Fs_rename")
		if del {
			r.Free()
		}
	}
	if dead != nil {
		dead.Free()
	}
	return err
}

// returns an error if anc is an ancestor directory of directory start. anc and
// start are reffed, but only start is locked. _isancestor unlocks start before
// returning.
func (fs *Fs_t) _isancestor(opid opid_t, anc, start *imemnode_t) defs.Err_t {
	if anc.inum == iroot {
		panic("root is always ancestor")
	}
	if start == fs.root {
		start.iunlock("")
		return 0
	}
	if _, ok := start.Refup(""); !ok {
		panic("start must have been reffed; cannot be removed")
	}
	// walk up to iroot
	here := start
	gimme := bounds.Bounds(bounds.B_FS_T__ISANCESTOR)
	for here != fs.root {
		if !res.Resadd_noblock(gimme) {
			// XXX on dead, return error and dead inode so that
			// _isancestor returns at most one dead inode
			if here.Refdown("") {
				panic("fixme")
			}
			if here == start {
				start.iunlock("")
			}
			return -defs.ENOHEAP
		}
		if anc.inum == here.inum {
			if here.Refdown("") {
				panic("fixme")
			}
			return -defs.EINVAL
		}
		// start is already locked
		if here != start {
			here.ilock("_isancestor")
		}
		next, err := here.ilookup(opid, ustr.DotDot)
		if err != 0 {
			if here.iunlock_refdown("_isancestor") {
				panic("fixme")
			}
			return err
		}
		if next.inum == here.inum {
			panic(".. cannot ref self")
		}
		if here.iunlock_refdown("_isancestor") {
			panic("fixme")
		}
		here = next
	}
	if here.Refdown("_isancestor") {
		panic("root cannot be unlinked")
	}
	return 0
}

type fsfops_t struct {
	priv defs.Inum_t
	fs   *Fs_t
	// protects offset
	sync.Mutex
	offset int
	append bool
	count  int
	//hack	*imemnode_t
}

func (fo *fsfops_t) _read(dst fdops.Userio_i, toff int) (int, defs.Err_t) {
	// lock the file to prevent races on offset and closing
	fo.Lock()
	//defer fo.Unlock()

	if fo.count <= 0 {
		fo.Unlock()
		return 0, -defs.EBADF
	}

	useoffset := toff != -1
	offset := fo.offset
	if useoffset {
		// XXXPANIC
		if toff < 0 {
			panic("neg offset")
		}
		offset = toff
	}
	idm := fo.fs.icache.Iref_locked(fo.priv, "_read")
	did, err := idm.do_read(dst, offset)
	if !useoffset && err == 0 {
		fo.offset += did
	}
	idm.iunlock_refdown("_read")
	fo.Unlock()
	return did, err
}

func (fo *fsfops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	return fo._read(dst, -1)
}

func (fo *fsfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return fo._read(dst, offset)
}

func (fo *fsfops_t) _write(src fdops.Userio_i, toff int) (int, defs.Err_t) {
	// lock the file to prevent races on offset and closing
	fo.Lock()
	defer fo.Unlock()

	if fo.count <= 0 {
		return 0, -defs.EBADF
	}

	useoffset := toff != -1
	offset := fo.offset
	append := fo.append
	if useoffset {
		// XXXPANIC
		if toff < 0 {
			panic("neg offset")
		}
		offset = toff
		append = false
	}
	idm := fo.fs.icache.Iref(fo.priv, "_write")
	did, err := idm.do_write(src, offset, append)
	if !useoffset && err == 0 {
		fo.offset += did
	}
	idm.Refdown("_write")
	return did, err
}

func (fo *fsfops_t) Write(src fdops.Userio_i) (int, defs.Err_t) {
	t := stats.Rdtsc()
	r, e := fo._write(src, -1)
	fo.fs.istats.CWrite.Add(t)
	return r, e
}

// XXX doesn't free blocks when shrinking file
func (fo *fsfops_t) Truncate(newlen uint) defs.Err_t {
	fo.Lock()
	defer fo.Unlock()
	if fo.count <= 0 {
		return -defs.EBADF
	}

	opid := fo.fs.fslog.Op_begin("truncate")
	defer fo.fs.fslog.Op_end(opid)

	if fs_debug {
		fmt.Printf("truncate: %v %v\n", fo.priv, newlen)
	}

	idm := fo.fs.icache.Iref_locked(fo.priv, "truncate")
	err := idm.do_trunc(opid, newlen)
	idm.iunlock_refdown("truncate")
	return err
}

func (fo *fsfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	return fo._write(src, offset)
}

// caller holds fo lock
func (fo *fsfops_t) fstat(st *stat.Stat_t) defs.Err_t {
	if fs_debug {
		fmt.Printf("fstat: %v %v\n", fo.priv, st)
	}
	idm := fo.fs.icache.Iref_locked(fo.priv, "fstat")
	err := idm.do_stat(st)
	idm.iunlock_refdown("fstat")
	return err
}

func (fo *fsfops_t) Fstat(st *stat.Stat_t) defs.Err_t {
	fo.Lock()
	defer fo.Unlock()
	if fo.count <= 0 {
		return -defs.EBADF
	}
	return fo.fstat(st)
}

func (fo *fsfops_t) Close() defs.Err_t {
	fo.Lock()
	//defer fo.Unlock()
	if fo.count <= 0 {
		fo.Unlock()
		return -defs.EBADF
	}
	fo.count--
	if fo.count <= 0 && fs_debug {
		fmt.Printf("Close: %d cnt %d\n", fo.priv, fo.count)

	}
	fo.Unlock()
	return fo.fs.Fs_close(fo.priv)
}

func (fo *fsfops_t) Pathi() defs.Inum_t {
	// fo.Lock()
	// defer fo.Unlock()
	// if fo.count <= 0 {
	// 	return -defs.EBADF
	// }
	return fo.priv
}

func (fo *fsfops_t) Reopen() defs.Err_t {
	fo.Lock()
	//defer fo.Unlock()
	if fo.count <= 0 {
		fo.Unlock()
		return -defs.EBADF
	}

	idm := fo.fs.icache.Iref_locked(fo.priv, "reopen")
	fo.fs.istats.Nreopen.Inc()
	idm.Refup("reopen") // close will decrease it
	idm.iunlock_refdown("reopen")
	fo.count++
	fo.Unlock()
	return 0
}

func (fo *fsfops_t) Lseek(off, whence int) (int, defs.Err_t) {
	// prevent races on fo.offset
	fo.Lock()
	defer fo.Unlock()
	if fo.count <= 0 {
		return 0, -defs.EBADF
	}

	fo.fs.istats.Nlseek.Inc()

	switch whence {
	case defs.SEEK_SET:
		fo.offset = off
	case defs.SEEK_CUR:
		fo.offset += off
	case defs.SEEK_END:
		st := &stat.Stat_t{}
		fo.fstat(st)
		fo.offset = int(st.Size()) + off
	default:
		return 0, -defs.EINVAL
	}
	if fo.offset < 0 {
		fo.offset = 0
	}
	return fo.offset, 0
}

// returns the mmapinfo for the pages of the target file. the page cache is
// populated if necessary.
func (fo *fsfops_t) Mmapi(offset, len int, inc bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	fo.Lock()
	//defer fo.Unlock()
	if fo.count <= 0 {
		fo.Unlock()
		return nil, -defs.EBADF
	}

	idm := fo.fs.icache.Iref_locked(fo.priv, "mmapi")
	mmi, err := idm.do_mmapi(offset, len, inc)
	idm.iunlock_refdown("mmapi")

	fo.Unlock()
	return mmi, err
}

func (fo *fsfops_t) Accept(fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	return nil, 0, -defs.ENOTSOCK
}

func (fo *fsfops_t) Bind([]uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (fo *fsfops_t) Connect(sabuf []uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (fo *fsfops_t) Listen(int) (fdops.Fdops_i, defs.Err_t) {
	return nil, -defs.ENOTSOCK
}

func (fo *fsfops_t) Sendmsg(fdops.Userio_i, []uint8, []uint8,
	int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (fo *fsfops_t) Recvmsg(fdops.Userio_i,
	fdops.Userio_i, fdops.Userio_i, int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	return 0, 0, 0, 0, -defs.ENOTSOCK
}

func (fo *fsfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	return pm.Events & (fdops.R_READ | fdops.R_WRITE), 0
}

func (fo *fsfops_t) Fcntl(cmd, opt int) int {
	return int(-defs.ENOSYS)
}

func (fo *fsfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (fo *fsfops_t) Setsockopt(int, int, fdops.Userio_i, int) defs.Err_t {
	return -defs.ENOTSOCK
}

func (fo *fsfops_t) Shutdown(read, write bool) defs.Err_t {
	return -defs.ENOTSOCK
}

type Devfops_t struct {
	Maj int
	Min int
}

func (df *Devfops_t) _sane() {
	// make sure this maj/min pair is handled by Devfops_t. to handle more
	// devices, we can either do dispatch in Devfops_t or we can return
	// device-specific fdops.Fdops_i in fs_open()
	if df.Maj != defs.D_CONSOLE && df.Maj != defs.D_DEVNULL &&
		df.Maj != defs.D_STAT && df.Maj != defs.D_PROF {
		panic("bad dev")
	}
}

var stats_string = ""

func stat_read(ub fdops.Userio_i, offset int) (int, defs.Err_t) {
	sz := ub.Remain()
	s := stats_string
	if len(s) > sz {
		s = s[:sz]
		stats_string = stats_string[sz:]
	} else {
		stats_string = ""
	}
	kdata := []byte(s)
	ret, err := ub.Uiowrite(kdata)
	if err != 0 || ret != len(kdata) {
		panic("dropped stats")
	}
	return ret, 0
}

type Perfrips_t struct {
	Rips  []uintptr
	Times []int
}

func (pr *Perfrips_t) Init(m map[uintptr]int) {
	l := len(m)
	pr.Rips = make([]uintptr, l)
	pr.Times = make([]int, l)
	idx := 0
	for k, v := range m {
		pr.Rips[idx] = k
		pr.Times[idx] = v
		idx++
	}
}

func (pr *Perfrips_t) Reset() {
	pr.Rips, pr.Times = nil, nil
}

func (pr *Perfrips_t) Len() int {
	return len(pr.Rips)
}

func (pr *Perfrips_t) Less(i, j int) bool {
	return pr.Times[i] < pr.Times[j]
}

func (pr *Perfrips_t) Swap(i, j int) {
	pr.Rips[i], pr.Rips[j] = pr.Rips[j], pr.Rips[i]
	pr.Times[i], pr.Times[j] = pr.Times[j], pr.Times[i]
}

var Profdev struct {
	sync.Mutex
	Bts   []uintptr
	Prips Perfrips_t
	rem   []uint8
}

func _prof_read(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	Profdev.Lock()
	defer Profdev.Unlock()

	did := 0
	prips := &Profdev.Prips
	for {
		for len(Profdev.rem) > 0 && dst.Remain() > 0 {
			c, err := dst.Uiowrite(Profdev.rem)
			did += c
			Profdev.rem = Profdev.rem[c:]
			if err != 0 {
				return did, err
			}
		}
		if dst.Remain() == 0 ||
			(prips.Len() == 0 && len(Profdev.Bts) == 0) {
			return did, 0
		}
		if prips.Len() > 0 {
			r := prips.Rips[0]
			t := prips.Times[0]
			prips.Rips = prips.Rips[1:]
			prips.Times = prips.Times[1:]
			d := fmt.Sprintf("%0.16x -- %10v\n", r, t)
			Profdev.rem = ([]uint8)(d)
		} else if len(Profdev.Bts) > 0 {
			rip := Profdev.Bts[0]
			Profdev.Bts = Profdev.Bts[1:]
			d := fmt.Sprintf("%0.16x\n", rip)
			Profdev.rem = []uint8(d)
		} else {
			return did, 0
		}
	}
}

func (df *Devfops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	df._sane()
	if df.Maj == defs.D_CONSOLE {
		return cons.Cons_read(dst, 0)
	} else if df.Maj == defs.D_STAT {
		return stat_read(dst, 0)
	} else if df.Maj == defs.D_PROF {
		return _prof_read(dst, 0)
	} else {
		return 0, 0
	}
}

func (df *Devfops_t) Write(src fdops.Userio_i) (int, defs.Err_t) {
	df._sane()
	if df.Maj == defs.D_CONSOLE {
		return cons.Cons_write(src, 0)
	} else {
		return src.Totalsz(), 0
	}
}

func (df *Devfops_t) Truncate(newlen uint) defs.Err_t {
	return -defs.EINVAL
}

func (df *Devfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	df._sane()
	return 0, -defs.ESPIPE
}

func (df *Devfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	df._sane()
	return 0, -defs.ESPIPE
}

func (df *Devfops_t) Fstat(st *stat.Stat_t) defs.Err_t {
	df._sane()
	st.Wmode(defs.Mkdev(df.Maj, df.Min))
	return 0
}

func (df *Devfops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	df._sane()
	return nil, -defs.ENODEV
}

func (df *Devfops_t) Pathi() defs.Inum_t {
	df._sane()
	panic("bad cwd")
}

func (df *Devfops_t) Close() defs.Err_t {
	df._sane()
	return 0
}

func (df *Devfops_t) Reopen() defs.Err_t {
	df._sane()
	return 0
}

func (df *Devfops_t) Lseek(int, int) (int, defs.Err_t) {
	df._sane()
	return 0, -defs.ESPIPE
}

func (df *Devfops_t) Accept(fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	return nil, 0, -defs.ENOTSOCK
}

func (df *Devfops_t) Bind([]uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (df *Devfops_t) Connect(sabuf []uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (df *Devfops_t) Listen(int) (fdops.Fdops_i, defs.Err_t) {
	return nil, -defs.ENOTSOCK
}

func (df *Devfops_t) Sendmsg(fdops.Userio_i, []uint8, []uint8,
	int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (df *Devfops_t) Recvmsg(fdops.Userio_i,
	fdops.Userio_i, fdops.Userio_i, int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	return 0, 0, 0, 0, -defs.ENOTSOCK
}

func (df *Devfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	switch df.Maj {
	case defs.D_CONSOLE:
		return cons.Cons_poll(pm)
	case defs.D_PROF:
		// XXX
		return pm.Events & fdops.R_READ, 0
	case defs.D_DEVNULL:
		return pm.Events & (fdops.R_READ | fdops.R_WRITE), 0
	default:
		panic("which dev")
	}
}

func (df *Devfops_t) Fcntl(cmd, opt int) int {
	return int(-defs.ENOSYS)
}

func (df *Devfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (df *Devfops_t) Setsockopt(int, int, fdops.Userio_i, int) defs.Err_t {
	return -defs.ENOTSOCK
}

func (df *Devfops_t) Shutdown(read, write bool) defs.Err_t {
	return -defs.ENOTSOCK
}

type rawdfops_t struct {
	sync.Mutex
	minor  int
	offset int
	fs     *Fs_t
}

func (raw *rawdfops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	raw.Lock()
	defer raw.Unlock()
	var did int
	for dst.Remain() != 0 {
		blkno := raw.offset / BSIZE
		b := raw.fs.fslog.Get_fill(blkno, "read", false)
		boff := raw.offset % BSIZE
		c, err := dst.Uiowrite(b.Data[boff:])
		if err != 0 {
			raw.fs.fslog.Relse(b, "read")
			return 0, err
		}
		raw.offset += c
		did += c
		raw.fs.fslog.Relse(b, "read")
	}
	return did, 0
}

func (raw *rawdfops_t) Write(src fdops.Userio_i) (int, defs.Err_t) {
	raw.Lock()
	defer raw.Unlock()
	var did int
	for src.Remain() != 0 {
		blkno := raw.offset / BSIZE
		boff := raw.offset % BSIZE
		buf, err := raw.fs.bcache.raw(blkno)
		if err != 0 {
			return 0, err
		}
		if boff != 0 || src.Remain() < BSIZE {
			buf.Read()
		}
		c, err := src.Uioread(buf.Data[boff:])
		if err != 0 {
			buf.EvictDone()
			return 0, err
		}
		buf.Write()
		raw.offset += c
		did += c
		buf.EvictDone()
	}
	return did, 0
}

func (raw *rawdfops_t) Truncate(newlen uint) defs.Err_t {
	return -defs.EINVAL
}

func (raw *rawdfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (raw *rawdfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (raw *rawdfops_t) Fstat(st *stat.Stat_t) defs.Err_t {
	raw.Lock()
	defer raw.Unlock()
	st.Wmode(defs.Mkdev(defs.D_RAWDISK, raw.minor))
	return 0
}

func (raw *rawdfops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	return nil, -defs.ENODEV
}

func (raw *rawdfops_t) Pathi() defs.Inum_t {
	panic("bad cwd")
}

func (raw *rawdfops_t) Close() defs.Err_t {
	return 0
}

func (raw *rawdfops_t) Reopen() defs.Err_t {
	return 0
}

func (raw *rawdfops_t) Lseek(off, whence int) (int, defs.Err_t) {
	raw.Lock()
	defer raw.Unlock()

	switch whence {
	case defs.SEEK_SET:
		raw.offset = off
	case defs.SEEK_CUR:
		raw.offset += off
	//case defs.SEEK_END:
	default:
		return 0, -defs.EINVAL
	}
	if raw.offset < 0 {
		raw.offset = 0
	}
	return raw.offset, 0
}

func (raw *rawdfops_t) Accept(fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	return nil, 0, -defs.ENOTSOCK
}

func (raw *rawdfops_t) Bind([]uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (raw *rawdfops_t) Connect(sabuf []uint8) defs.Err_t {
	return -defs.ENOTSOCK
}

func (raw *rawdfops_t) Listen(int) (fdops.Fdops_i, defs.Err_t) {
	return nil, -defs.ENOTSOCK
}

func (raw *rawdfops_t) Sendmsg(fdops.Userio_i, []uint8, []uint8,
	int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (raw *rawdfops_t) Recvmsg(fdops.Userio_i,
	fdops.Userio_i, fdops.Userio_i, int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	return 0, 0, 0, 0, -defs.ENOTSOCK
}

func (raw *rawdfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	return pm.Events & (fdops.R_READ | fdops.R_WRITE), 0
}

func (raw *rawdfops_t) Fcntl(cmd, opt int) int {
	return int(-defs.ENOSYS)
}

func (raw *rawdfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	return 0, -defs.ENOTSOCK
}

func (raw *rawdfops_t) Setsockopt(int, int, fdops.Userio_i, int) defs.Err_t {
	return -defs.ENOTSOCK
}

func (raw *rawdfops_t) Shutdown(read, write bool) defs.Err_t {
	return -defs.ENOTSOCK
}

func (fs *Fs_t) Fs_mkdir(paths ustr.Ustr, mode int, cwd *fd.Cwd_t) defs.Err_t {
	refs, dead, err := fs.Fs_op_mkdir(paths, mode, cwd)
	for _, ref := range refs {
		if ref.Refdown("") {
			ref.Free()
		}
	}
	if dead != nil {
		dead.Free()
	}
	return err
}

// returns refs, dead, and error...
func (fs *Fs_t) Fs_op_mkdir(paths ustr.Ustr, mode int, cwd *fd.Cwd_t) ([]*imemnode_t, *imemnode_t, defs.Err_t) {
	opid := fs.fslog.Op_begin("fs_mkdir")
	defer fs.fslog.Op_end(opid)

	fs.istats.Nmkdir.Inc()

	if fs_debug {
		fmt.Printf("mkdir: %v %v\n", paths, cwd)
	}

	dirs, fn := bpath.Sdirname(paths)
	if err, ok := crname(fn, -defs.EINVAL); !ok {
		return nil, nil, err
	}
	if len(fn) > DNAMELEN {
		return nil, nil, -defs.ENAMETOOLONG
	}

	par, dead, err := fs.fs_namei_locked(opid, dirs, cwd, "mkdir")
	if err != 0 {
		return nil, dead, err
	}

	child, err := par.do_createdir(opid, fn)
	if err != 0 {
		par.iunlock("fs_mkdir_par")
		return []*imemnode_t{par}, nil, err
	}
	defer par.iunlock("fs_mkdir_par")
	// cannot deadlock since par is locked and concurrent lookup must lock
	// par to get a handle to child
	child.ilock("")
	defer child.iunlock("")

	if err = child.do_insert(opid, ustr.MkUstrDot(), child.inum); err != 0 {
		goto outunlink
	}
	if err = child.do_insert(opid, ustr.DotDot, par.inum); err != 0 {
		goto outunlink
	}
	return []*imemnode_t{par, child}, nil, 0
outunlink:
	// could do insert last; this cannot fail since par's dirent page is
	// reffed by the log transaction
	_, nerr := par.iunlink(opid, fn)
	if nerr != 0 {
		panic("must succeed")
	}
	child._linkdown(opid)
	return []*imemnode_t{par, child}, nil, err
}

// a type to represent on-disk files
type Fsfile_t struct {
	Inum  defs.Inum_t
	Major int
	Minor int
}

func (fs *Fs_t) Fs_open_inner(paths ustr.Ustr, flags defs.Fdopt_t, mode int, cwd *fd.Cwd_t, major, minor int) (Fsfile_t, defs.Err_t) {
	ret, dead, err := fs._fs_open_inner(paths, flags, mode, cwd, major, minor)
	if dead != nil {
		dead.Free()
	}
	return ret, err
}

// returns the file, a dead inode (non-nil only on error) and error
func (fs *Fs_t) _fs_open_inner(paths ustr.Ustr, flags defs.Fdopt_t, mode int, cwd *fd.Cwd_t, major, minor int) (Fsfile_t, *imemnode_t, defs.Err_t) {
	trunc := flags&defs.O_TRUNC != 0
	creat := flags&defs.O_CREAT != 0
	nodir := false

	if fs_debug {
		fmt.Printf("fs_open: %v %v %v\n", paths, cwd, creat)
	}

	// open with O_TRUNC is not read-only
	var opid opid_t
	if trunc || creat {
		opid = fs.fslog.Op_begin("fs_open")
		defer fs.fslog.Op_end(opid)
	}
	var ret Fsfile_t
	var idm *imemnode_t
	if creat {
		nodir = true
		// creat w/execl; must atomically create and open the new file.
		isdev := major != 0 || minor != 0

		// must specify at least one path component
		dirs, fn := bpath.Sdirname(paths)
		if err, ok := crname(fn, -defs.EEXIST); !ok {
			return ret, nil, err
		}

		if len(fn) > DNAMELEN {
			return ret, nil, -defs.ENAMETOOLONG
		}

		// with O_CREAT, the file may exist.
		par, dead, err := fs.fs_namei_locked(opid, dirs, cwd, "Fs_open_inner")
		if err != 0 {
			return ret, dead, err
		}
		if isdev {
			idm, err = par.do_createnod(opid, fn, major, minor)
		} else {
			idm, err = par.do_createfile(opid, fn)
		}
		if err != 0 && err != -defs.EEXIST {
			// XXX must check dead
			par.iunlock_refdown("Fs_open_inner_par")
			return ret, nil, err
		}
		exists := err == -defs.EEXIST
		par.iunlock_refdown("Fs_open_inner_par")
		idm.ilock("child")

		oexcl := flags&defs.O_EXCL != 0
		if exists {
			if oexcl || isdev {
				idm.iunlock_refdown("Fs_open_inner2")
				return ret, nil, -defs.EEXIST
			}
		}
	} else {
		// open existing file
		var err defs.Err_t
		var dead *imemnode_t
		idm, dead, err = fs.fs_namei_locked(opid, paths, cwd, "Fs_open_inner_existing")
		if err != 0 {
			return ret, dead, err
		}
		// idm is locked
	}
	defer idm.iunlock_refdown("Fs_open_inner_idm")

	itype := idm.itype

	o_dir := flags&defs.O_DIRECTORY != 0
	wantwrite := flags&(defs.O_WRONLY|defs.O_RDWR) != 0
	if wantwrite {
		nodir = true
	}

	// verify flags: dir cannot be opened with write perms and only dir can
	// be opened with O_DIRECTORY
	if o_dir || nodir {
		if o_dir && itype != I_DIR {
			return ret, nil, -defs.ENOTDIR
		}
		if nodir && itype == I_DIR {
			return ret, nil, -defs.EISDIR
		}
	}

	if nodir && trunc {
		idm.do_trunc(opid, 0)
	}

	idm.Refup("Fs_open_inner")

	ret.Inum = idm.inum
	ret.Major = idm.major
	ret.Minor = idm.minor
	return ret, nil, 0
}

func (fs *Fs_t) Makefake() *fd.Fd_t {
	return nil
	//ret := &fd.Fd_t{}
	////priv := defs.Inum_t(iroot)
	//// guess for a file inode...
	//priv := defs.Inum_t(2)
	//fake := &fsfops_t{priv: iroot, fs: fs, count: 1}
	//fake.hack = &imemnode_t{}
	//fake.hack.inum = priv
	//fake.hack.fs = fs
	//fake.hack.idm_init(priv)
	//if fake.hack.itype == I_DIR {
	//	panic("isdir!")
	//}
	//ret.Fops = fake
	//return ret
}

// socket files cannot be open(2)'ed (must use connect(2)/sendto(2) etc.)
var _denyopen = map[int]bool{defs.D_SUD: true, defs.D_SUS: true}

func (fs *Fs_t) Fs_open(paths ustr.Ustr, flags defs.Fdopt_t, mode int, cwd *fd.Cwd_t, major, minor int) (*fd.Fd_t, defs.Err_t) {
	fs.istats.Nopen.Inc()
	fsf, err := fs.Fs_open_inner(paths, flags, mode, cwd, major, minor)
	if err != 0 {
		return nil, err
	}

	// some special files (sockets) cannot be opened with fops this way
	if denied := _denyopen[fsf.Major]; denied {
		if fs.Fs_close(fsf.Inum) != 0 {
			panic("must succeed")
		}
		return nil, -defs.EPERM
	}

	// convert on-disk file to fd with fops
	priv := fsf.Inum
	maj := fsf.Major
	min := fsf.Minor
	ret := &fd.Fd_t{}
	if maj != 0 {
		// don't need underlying file open
		if fs.Fs_close(fsf.Inum) != 0 {
			panic("must succeed")
		}
		switch maj {
		case defs.D_CONSOLE, defs.D_DEVNULL, defs.D_STAT, defs.D_PROF:
			if maj == defs.D_STAT {
				stats_string = fs.Fs_statistics()
			}
			ret.Fops = &Devfops_t{Maj: maj, Min: min}
		case defs.D_RAWDISK:
			ret.Fops = &rawdfops_t{minor: min, fs: fs}
		default:
			panic("bad dev")
		}
	} else {
		apnd := flags&defs.O_APPEND != 0
		ret.Fops = &fsfops_t{priv: priv, fs: fs, append: apnd, count: 1}
	}
	return ret, 0
}

func (fs *Fs_t) Fs_close(priv defs.Inum_t) defs.Err_t {
	opid := fs.fslog.Op_begin("Fs_close")

	fs.istats.Nclose.Inc()

	if fs_debug {
		fmt.Printf("Fs_close: %d %v\n", opid, priv)
	}

	idm := fs.icache.Iref_locked(priv, "Fs_close")
	idm.Refdown("Fs_close")
	del := idm.iunlock_refdown("Fs_close")

	fs.fslog.Op_end(opid)

	if del {
		idm.Free()
	}

	return 0
}

func (fs *Fs_t) Fs_stat(path ustr.Ustr, st *stat.Stat_t, cwd *fd.Cwd_t) defs.Err_t {
	opid := opid_t(0)

	if fs_debug {
		fmt.Printf("fstat: %v %v\n", path, cwd)
	}
	idm, dead, err := fs.fs_namei_locked(opid, path, cwd, "Fs_stat")
	if err != 0 {
		if dead != nil {
			dead.Free()
		}
		return err
	}
	err = idm.do_stat(st)
	del := idm.iunlock_refdown("Fs_stat")
	if del {
		idm.Free()
	}
	return err
}

// Sync the file system to disk. XXX If Biscuit supported fsync, we could be
// smarter and flush only the dirty blocks of particular inode.
func (fs *Fs_t) Fs_sync() defs.Err_t {
	if !fs.diskfs {
		return 0
	}
	fs.istats.Nsync.Inc()
	fs.fslog.Force(false)
	return 0
}

func (fs *Fs_t) Fs_syncapply() defs.Err_t {
	if !fs.diskfs {
		return 0
	}
	fs.istats.Nsync.Inc()
	fs.fslog.Force(true)
	return 0
}

// if the path resolves successfully, returns target inode with incremented
// refcount and locked. the caller must always be prepared to free the returned
// imemnode after calling Refdown. if the lookup fails, the second returned
// inode may be non-nil and must be freed by the caller. since the slow path
// acquires locks on inodes, the caller must not have any other inode locked,
// otherwise namei may deadlock.
func (fs *Fs_t) _fs_namei_locked(opid opid_t, paths ustr.Ustr, cwd *fd.Cwd_t) (*imemnode_t, *imemnode_t, defs.Err_t) {
	var start *imemnode_t
	fs.istats.Nnamei.Inc()
	// ref lookup directory
	if len(paths) == 0 || paths[0] != '/' {
		start = fs.icache.Iref(cwd.Fd.Fops.Pathi(), "fs_namei_cwd")
	} else {
		start = fs.IrefRoot()
	}
	idm := start
	var pp bpath.Pathparts_t
	pp.Pp_init(paths)
	var next ustr.Ustr
	var nextok bool
	// lock-free fast path
	for cp, ok := pp.Next(); ok; cp, ok = next, nextok {
		// make sure slow path continues on this component if the
		// lock-free lookup fails
		next, nextok = pp.Next()
		lastc := !nextok
		n, found := idm.ilookup_lockfree(cp, lastc)
		if !found {
			break
		}
		idm = n
		if lastc {
			// ilookup_lockfree already locked n
			if start != n {
				if start.Refdown("") {
					// unlucky; cwd unlinked out from under
					// us
					if n.iunlock_refdown("") {
						panic("huh?")
					}
					return nil, start, -defs.ENOENT
				}
			}
			return n, nil, 0
		}
		// "start" is the only imemnode whose refcount is incremented
		if !res.Resadd_noblock(bounds.Bounds(bounds.B_FS_T_FS_NAMEI)) {
			err := -defs.ENOHEAP
			if start.Refdown("") {
				return nil, start, err
			}
			return nil, nil, err
		}
	}
	// couldn't ref idm; restart completely
	idm = start
	pp.Pp_init(paths)

	// lock-full slow path
	for cp, ok := pp.Next(); ok; cp, ok = next, nextok {
		next, nextok = pp.Next()

		idm.ilock("fs_namei")
		// for simplicity, conservatively fail the lookup if links==0
		// so that namei can return at most one dead inode.
		var n *imemnode_t
		var err defs.Err_t
		if idm.links != 0 {
			n, err = idm.ilookup(opid, cp)
		} else {
			err = -defs.ENOENT
		}
		var dead *imemnode_t
		// ilookup always increments the refcnt, even on "."
		if idm.iunlock_refdown("") {
			// idm can only be unlinked if the lookup failed or if
			// we looked up ".."
			dead = idm
			if err == 0 {
				if !cp.Isdotdot() {
					panic("how?")
				}
				err = -defs.ENOENT
			}
		}
		if err != 0 {
			return nil, dead, err
		}
		idm = n
		if !res.Resadd_noblock(bounds.Bounds(bounds.B_FS_T_FS_NAMEI)) {
			err := -defs.ENOHEAP
			if idm.Refdown("") {
				return nil, idm, err
			}
			return nil, nil, err
		}
	}
	idm.ilock("")
	return idm, nil, 0
}

func (fs *Fs_t) fs_namei_locked(opid opid_t, paths ustr.Ustr, cwd *fd.Cwd_t, s string) (*imemnode_t, *imemnode_t, defs.Err_t) {
	return fs._fs_namei_locked(opid, paths, cwd)
}

func (fs *Fs_t) Fs_evict() (int, int) {
	if !fs.diskfs {
		panic("no evict")
	}
	_, a := fs.bcache.cache.Evict_half()
	_, b := fs.icache.cache.Evict_half()
	fmt.Printf("FS EVICT blk %v imem %v\n", a, b)
	return fs.Sizes()
}

func (fs *Fs_t) Fs_statistics() string {
	s := fs.istats.Stats()
	if fs.diskfs {
		s += fs.fslog.Stats()
	}
	s += fs.ialloc.Stats()
	s += fs.balloc.Stats()
	s += fs.bcache.Stats()
	s += fs.icache.Stats()
	s += fs.ahci.Stats()
	return s
}
