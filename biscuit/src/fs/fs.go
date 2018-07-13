package fs

import "fmt"
import "sync"

import "bpath"
import "ustr"
import "common"

const fs_debug = false
const FSOFF = 506
const iroot = common.Inum_t(0)

var cons common.Cons_i

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

func StartFS(mem Blockmem_i, disk Disk_i, console common.Cons_i, diskfs bool) (*common.Fd_t, *Fs_t) {

	cons = console

	// reset taken
	common.Syslimit = common.MkSysLimit()

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
	fs.superb_start = common.Readn(b.Data[:], 4, FSOFF)
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

	return &common.Fd_t{Fops: &fsfops_t{priv: iroot, fs: fs, count: 1}}, fs
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

func (fs *Fs_t) MkRootCwd() *common.Cwd_t {
	fd := &common.Fd_t{Fops: &fsfops_t{priv: iroot, fs: fs, count: 0}}
	cwd := common.MkRootCwd(fd)
	return cwd
}

func (fs *Fs_t) Unpin(pa common.Pa_t) {
	fs.bcache.unpin(pa)
}

func (fs *Fs_t) Fs_op_link(old ustr.Ustr, new ustr.Ustr, cwd *common.Cwd_t) (*imemnode_t, common.Err_t) {
	opid := fs.fslog.Op_begin("Fs_link")
	defer fs.fslog.Op_end(opid)

	if fs_debug {
		fmt.Printf("Fs_link: %v %v %v\n", old, new, cwd)
	}

	fs.istats.Nilink.Inc()

	var dead *imemnode_t
	orig, err := fs.fs_namei_locked(opid, old, cwd, "Fs_link_org")
	if err != 0 {
		return dead, err
	}
	if orig.itype != I_FILE {
		orig.iunlock_refdown("fs_link")
		return dead, -common.EINVAL
	}
	inum := orig.inum
	orig._linkup(opid)
	orig.iunlock("fs_link_orig")

	dirs, fn := bpath.Sdirname(new)
	newd, err := fs.fs_namei_locked(opid, dirs, cwd, "fs_link_newd")
	if err != 0 {
		goto undo
	}
	err = newd.do_insert(opid, fn, inum)
	newd.iunlock_refdown("fs_link_newd")
	if err != 0 {
		goto undo
	}
	orig.Refdown("fs_link_orig")
	return dead, 0
undo:
	orig.ilock("fs_link_undo")
	orig._linkdown(opid)
	del := orig.iunlock_refdown("fs_link_undo")
	if del {
		dead = orig
	}
	return dead, err
}

func (fs *Fs_t) Fs_link(old ustr.Ustr, new ustr.Ustr, cwd *common.Cwd_t) common.Err_t {
	dead, err := fs.Fs_op_link(old, new, cwd)
	if dead != nil {
		dead.Free()
	}
	return err
}

func (fs *Fs_t) Fs_op_unlink(paths ustr.Ustr, cwd *common.Cwd_t, wantdir bool) (*imemnode_t, common.Err_t) {
	opid := fs.fslog.Op_begin("fs_unlink")
	defer fs.fslog.Op_end(opid)

	dirs, fn := bpath.Sdirname(paths)
	if fn.Isdot() || fn.Isdotdot() {
		return nil, -common.EPERM
	}

	fs.istats.Nunlink.Inc()

	if fs_debug {
		fmt.Printf("fs_unlink: %v cwd %v dir? %v\n", paths, cwd, wantdir)
	}

	var dead *imemnode_t
	var child *imemnode_t
	var par *imemnode_t
	var err common.Err_t

	par, err = fs.fs_namei_locked(opid, dirs, cwd, "fs_unlink_par")
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
			return dead, -common.ENOENT
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

func (fs *Fs_t) Fs_unlink(paths ustr.Ustr, cwd *common.Cwd_t, wantdir bool) common.Err_t {
	dead, err := fs.Fs_op_unlink(paths, cwd, wantdir)
	if dead != nil {
		dead.Free()
	}
	return err
}

// per-volume rename mutex. Linux does it so it must be OK!
var _renamelock = sync.Mutex{}

func (fs *Fs_t) Fs_op_rename(oldp, newp ustr.Ustr, cwd *common.Cwd_t) ([]*imemnode_t, common.Err_t) {
	odirs, ofn := bpath.Sdirname(oldp)
	ndirs, nfn := bpath.Sdirname(newp)
	var refs []*imemnode_t

	if err, ok := crname(ofn, -common.EINVAL); !ok {
		return refs, err
	}
	if err, ok := crname(nfn, -common.EINVAL); !ok {
		return refs, err
	}

	opid := fs.fslog.Op_begin("fs_rename")
	defer fs.fslog.Op_end(opid)

	fs.istats.Nrename.Inc()

	// one rename at the time
	_renamelock.Lock()
	defer _renamelock.Unlock()

	if fs_debug {
		fmt.Printf("fs_rename: src %v dst %v %v\n", oldp, newp, cwd)
	}

	// lookup all inode references, but we will release locks and lock them
	// together when we know all references.  the references to the inodes
	// cannot disppear, so unlocking temporarily is fine.
	opar, err := fs.fs_namei_locked(opid, odirs, cwd, "fs_rename_opar")
	if err != 0 {
		return refs, err
	}

	ochild, err := opar.ilookup(opid, ofn)
	if err != 0 {
		opar.iunlock_refdown("fs_rename_opar")
		return refs, err
	}

	// unlock par after we have ref to child
	opar.iunlock("fs_rename_par")

	npar, err := fs.fs_namei(opid, ndirs, cwd)
	if err != 0 {
		opar.Refdown("fs_rename_opar")
		ochild.Refdown("fs_rename_ochild")
		return refs, err
	}

	// verify that ochild is not an ancestor of npar, since we would
	// disconnect ochild subtree from root.  it is safe to do without
	// holding locks because unlink cannot modify the path to the root by
	// removing a directory because the directories aren't empty.  it could
	// delete npar and an ancestor, but rename has already a reference to to
	// npar.
	if err = fs._isancestor(opid, ochild, npar); err != 0 {
		opar.Refdown("fs_rename_opar")
		ochild.Refdown("fs_rename_ochild")
		npar.Refdown("fs_rename_npar")
		return refs, err
	}

	var nchild *imemnode_t
	cnt := 0
	// lookup newchild and try to lock all inodes involved
	for {
		gimme := common.Bounds(common.B_FS_T_FS_RENAME)
		if !common.Resadd_noblock(gimme) {
			opar.Refdown("fs_name_opar")
			ochild.Refdown("fs_name_ochild")
			return refs, -common.ENOHEAP
		}
		npar.ilock("")
		nchild, err = npar.ilookup(opid, nfn)
		if err != 0 && err != -common.ENOENT {
			opar.Refdown("fs_name_opar")
			ochild.Refdown("fs_name_ochild")
			npar.iunlock_refdown("fs_name_npar")
			return refs, err
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
			return refs, err
		}
		if !ok {
			return refs, -common.ENOENT
		}
		if nchild == nil {
			childi, err := npar.ilookup(opid, nfn)
			if err == 0 {
				childi.Refdown("rename")
			}
			if err == -common.ENOENT {
				// it didn't exist before and still doesn't exist
				break
			}
		} else {
			ok, err := npar.ilookup_validate(opid, nfn, nchild)
			if err == 0 && ok {
				// it existed before and still exists
				break
			}
			if err == -common.ENOENT {
				// it existed but now it doesn't.
				break
			}
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
		return refs, 0
	}

	// guarantee that any page allocations will succeed before starting the
	// operation, which will be messy to piece-wise undo.
	b1, err := npar.probe_insert(opid)
	if err != 0 {
		return refs, err
	}
	defer fs.fslog.Relse(b1, "probe_insert")

	b2, err := opar.probe_unlink(opid, ofn)
	if err != 0 {
		return refs, err
	}
	defer fs.fslog.Relse(b2, "probe_unlink_opar")

	odir := ochild.itype == I_DIR
	if odir {
		b3, err := ochild.probe_unlink(opid, ustr.MkUstrDotDot())
		if err != 0 {
			return refs, err
		}
		defer fs.fslog.Relse(b3, "probe_unlink_ochild")
	}

	if nchild != nil {
		// make sure old and new are either both files or both
		// directories
		if err := nchild.do_dirchk(opid, odir); err != 0 {
			return refs, err
		}

		// remove pre-existing new
		if err = npar.do_unlink(opid, nfn); err != 0 {
			return refs, err
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
		dotdot := ustr.MkUstrDotDot()
		if ochild.do_unlink(opid, dotdot) != 0 {
			panic("probed")
		}
		if ochild.do_insert(opid, dotdot, npar.inum) != 0 {
			panic("insert after unlink must succeed")
		}
	}
	return refs, 0
}

func (fs *Fs_t) Fs_rename(oldp, newp ustr.Ustr, cwd *common.Cwd_t) common.Err_t {
	refs, err := fs.Fs_op_rename(oldp, newp, cwd)
	for _, r := range refs {
		del := r.Refdown("Fs_rename")
		if del {
			r.Free()
		}

	}
	return err
}

// anc and start are in memory
func (fs *Fs_t) _isancestor(opid opid_t, anc, start *imemnode_t) common.Err_t {
	if anc.inum == iroot {
		panic("root is always ancestor")
	}
	// walk up to iroot
	here := start
	here.Refup("_isancestor")
	gimme := common.Bounds(common.B_FS_T__ISANCESTOR)
	for here != fs.root {
		if !common.Resadd_noblock(gimme) {
			return -common.ENOHEAP
		}
		if anc.inum == here.inum {
			here.Refdown("_isancestor_here")
			return -common.EINVAL
		}
		here.ilock("_isancestor")
		next, err := here.ilookup(opid, ustr.MkUstrDotDot())
		if err != 0 {
			here.iunlock("_isancestor")
			return err
		}
		if next.inum == here.inum {
			here.iunlock("_isancestor")
			panic("xxx")
		} else {
			here.iunlock_refdown("_isancestor")
			here = next
		}
	}
	here.Refdown("_isancestor")
	return 0
}

type fsfops_t struct {
	priv common.Inum_t
	fs   *Fs_t
	// protects offset
	sync.Mutex
	offset int
	append bool
	count  int
	//hack	*imemnode_t
}

func (fo *fsfops_t) _read(dst common.Userio_i, toff int) (int, common.Err_t) {
	// lock the file to prevent races on offset and closing
	fo.Lock()
	//defer fo.Unlock()

	if fo.count <= 0 {
		fo.Unlock()
		return 0, -common.EBADF
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

func (fo *fsfops_t) Read(p *common.Proc_t, dst common.Userio_i) (int, common.Err_t) {
	return fo._read(dst, -1)
}

func (fo *fsfops_t) Pread(dst common.Userio_i, offset int) (int, common.Err_t) {
	return fo._read(dst, offset)
}

func (fo *fsfops_t) _write(src common.Userio_i, toff int) (int, common.Err_t) {
	// lock the file to prevent races on offset and closing
	fo.Lock()
	defer fo.Unlock()

	if fo.count <= 0 {
		return 0, -common.EBADF
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

func (fo *fsfops_t) Write(p *common.Proc_t, src common.Userio_i) (int, common.Err_t) {
	t := common.Rdtsc()
	r, e := fo._write(src, -1)
	fo.fs.istats.CWrite.Add(t)
	return r, e
}

// XXX doesn't free blocks when shrinking file
func (fo *fsfops_t) Truncate(newlen uint) common.Err_t {
	fo.Lock()
	defer fo.Unlock()
	if fo.count <= 0 {
		return -common.EBADF
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

func (fo *fsfops_t) Pwrite(src common.Userio_i, offset int) (int, common.Err_t) {
	return fo._write(src, offset)
}

// caller holds fo lock
func (fo *fsfops_t) fstat(st *common.Stat_t) common.Err_t {
	if fs_debug {
		fmt.Printf("fstat: %v %v\n", fo.priv, st)
	}
	idm := fo.fs.icache.Iref_locked(fo.priv, "fstat")
	err := idm.do_stat(st)
	idm.iunlock_refdown("fstat")
	return err
}

func (fo *fsfops_t) Fstat(st *common.Stat_t) common.Err_t {
	fo.Lock()
	defer fo.Unlock()
	if fo.count <= 0 {
		return -common.EBADF
	}
	return fo.fstat(st)
}

// XXX log those files that have no fs links but > 0 memory references to the
// journal so that if we crash before freeing its blocks, the blocks can be
// reclaimed.
func (fo *fsfops_t) Close() common.Err_t {
	fo.Lock()
	//defer fo.Unlock()
	if fo.count <= 0 {
		fo.Unlock()
		return -common.EBADF
	}
	fo.count--
	if fo.count <= 0 && fs_debug {
		fmt.Printf("Close: %d cnt %d\n", fo.priv, fo.count)

	}
	fo.Unlock()
	return fo.fs.Fs_close(fo.priv)
}

func (fo *fsfops_t) Pathi() common.Inum_t {
	// fo.Lock()
	// defer fo.Unlock()
	// if fo.count <= 0 {
	// 	return -common.EBADF
	// }
	return fo.priv
}

func (fo *fsfops_t) Reopen() common.Err_t {
	fo.Lock()
	//defer fo.Unlock()
	if fo.count <= 0 {
		fo.Unlock()
		return -common.EBADF
	}

	idm := fo.fs.icache.Iref_locked(fo.priv, "reopen")
	fo.fs.istats.Nreopen.Inc()
	idm.Refup("reopen") // close will decrease it
	idm.iunlock_refdown("reopen")
	fo.count++
	fo.Unlock()
	return 0
}

func (fo *fsfops_t) Lseek(off, whence int) (int, common.Err_t) {
	// prevent races on fo.offset
	fo.Lock()
	defer fo.Unlock()
	if fo.count <= 0 {
		return 0, -common.EBADF
	}

	fo.fs.istats.Nlseek.Inc()

	switch whence {
	case common.SEEK_SET:
		fo.offset = off
	case common.SEEK_CUR:
		fo.offset += off
	case common.SEEK_END:
		st := &common.Stat_t{}
		fo.fstat(st)
		fo.offset = int(st.Size()) + off
	default:
		return 0, -common.EINVAL
	}
	if fo.offset < 0 {
		fo.offset = 0
	}
	return fo.offset, 0
}

// returns the mmapinfo for the pages of the target file. the page cache is
// populated if necessary.
func (fo *fsfops_t) Mmapi(offset, len int, inc bool) ([]common.Mmapinfo_t, common.Err_t) {
	fo.Lock()
	//defer fo.Unlock()
	if fo.count <= 0 {
		fo.Unlock()
		return nil, -common.EBADF
	}

	idm := fo.fs.icache.Iref_locked(fo.priv, "mmapi")
	mmi, err := idm.do_mmapi(offset, len, inc)
	idm.iunlock_refdown("mmapi")

	fo.Unlock()
	return mmi, err
}

func (fo *fsfops_t) Accept(*common.Proc_t, common.Userio_i) (common.Fdops_i, int, common.Err_t) {
	return nil, 0, -common.ENOTSOCK
}

func (fo *fsfops_t) Bind(*common.Proc_t, []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (fo *fsfops_t) Connect(proc *common.Proc_t, sabuf []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (fo *fsfops_t) Listen(*common.Proc_t, int) (common.Fdops_i, common.Err_t) {
	return nil, -common.ENOTSOCK
}

func (fo *fsfops_t) Sendmsg(*common.Proc_t, common.Userio_i, []uint8, []uint8,
	int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (fo *fsfops_t) Recvmsg(*common.Proc_t, common.Userio_i,
	common.Userio_i, common.Userio_i, int) (int, int, int, common.Msgfl_t, common.Err_t) {
	return 0, 0, 0, 0, -common.ENOTSOCK
}

func (fo *fsfops_t) Pollone(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	return pm.Events & (common.R_READ | common.R_WRITE), 0
}

func (fo *fsfops_t) Fcntl(proc *common.Proc_t, cmd, opt int) int {
	return int(-common.ENOSYS)
}

func (fo *fsfops_t) Getsockopt(proc *common.Proc_t, opt int, bufarg common.Userio_i,
	intarg int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (fo *fsfops_t) Setsockopt(*common.Proc_t, int, int, common.Userio_i, int) common.Err_t {
	return -common.ENOTSOCK
}

func (fo *fsfops_t) Shutdown(read, write bool) common.Err_t {
	return -common.ENOTSOCK
}

type Devfops_t struct {
	Maj int
	Min int
}

func (df *Devfops_t) _sane() {
	// make sure this maj/min pair is handled by Devfops_t. to handle more
	// devices, we can either do dispatch in Devfops_t or we can return
	// device-specific common.Fdops_i in fs_open()
	if df.Maj != common.D_CONSOLE && df.Maj != common.D_DEVNULL &&
		df.Maj != common.D_STAT && df.Maj != common.D_PROF {
		panic("bad dev")
	}
}

var stats_string = ""

func stat_read(ub common.Userio_i, offset int) (int, common.Err_t) {
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

func _prof_read(dst common.Userio_i, offset int) (int, common.Err_t) {
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

func (df *Devfops_t) Read(p *common.Proc_t, dst common.Userio_i) (int, common.Err_t) {
	df._sane()
	if df.Maj == common.D_CONSOLE {
		return cons.Cons_read(dst, 0)
	} else if df.Maj == common.D_STAT {
		return stat_read(dst, 0)
	} else if df.Maj == common.D_PROF {
		return _prof_read(dst, 0)
	} else {
		return 0, 0
	}
}

func (df *Devfops_t) Write(p *common.Proc_t, src common.Userio_i) (int, common.Err_t) {
	df._sane()
	if df.Maj == common.D_CONSOLE {
		return cons.Cons_write(src, 0)
	} else {
		return src.Totalsz(), 0
	}
}

func (df *Devfops_t) Truncate(newlen uint) common.Err_t {
	return -common.EINVAL
}

func (df *Devfops_t) Pread(dst common.Userio_i, offset int) (int, common.Err_t) {
	df._sane()
	return 0, -common.ESPIPE
}

func (df *Devfops_t) Pwrite(src common.Userio_i, offset int) (int, common.Err_t) {
	df._sane()
	return 0, -common.ESPIPE
}

func (df *Devfops_t) Fstat(st *common.Stat_t) common.Err_t {
	df._sane()
	st.Wmode(common.Mkdev(df.Maj, df.Min))
	return 0
}

func (df *Devfops_t) Mmapi(int, int, bool) ([]common.Mmapinfo_t, common.Err_t) {
	df._sane()
	return nil, -common.ENODEV
}

func (df *Devfops_t) Pathi() common.Inum_t {
	df._sane()
	panic("bad cwd")
}

func (df *Devfops_t) Close() common.Err_t {
	df._sane()
	return 0
}

func (df *Devfops_t) Reopen() common.Err_t {
	df._sane()
	return 0
}

func (df *Devfops_t) Lseek(int, int) (int, common.Err_t) {
	df._sane()
	return 0, -common.ESPIPE
}

func (df *Devfops_t) Accept(*common.Proc_t, common.Userio_i) (common.Fdops_i, int, common.Err_t) {
	return nil, 0, -common.ENOTSOCK
}

func (df *Devfops_t) Bind(*common.Proc_t, []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (df *Devfops_t) Connect(proc *common.Proc_t, sabuf []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (df *Devfops_t) Listen(*common.Proc_t, int) (common.Fdops_i, common.Err_t) {
	return nil, -common.ENOTSOCK
}

func (df *Devfops_t) Sendmsg(*common.Proc_t, common.Userio_i, []uint8, []uint8,
	int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (df *Devfops_t) Recvmsg(*common.Proc_t, common.Userio_i,
	common.Userio_i, common.Userio_i, int) (int, int, int, common.Msgfl_t, common.Err_t) {
	return 0, 0, 0, 0, -common.ENOTSOCK
}

func (df *Devfops_t) Pollone(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	switch df.Maj {
	// case common.D_CONSOLE:
	// 	cons.pollc <- pm
	// 	return <- cons.pollret, 0
	case common.D_PROF:
		// XXX
		return pm.Events & common.R_READ, 0
	case common.D_DEVNULL:
		return pm.Events & (common.R_READ | common.R_WRITE), 0
	default:
		panic("which dev")
	}
}

func (df *Devfops_t) Fcntl(proc *common.Proc_t, cmd, opt int) int {
	return int(-common.ENOSYS)
}

func (df *Devfops_t) Getsockopt(proc *common.Proc_t, opt int, bufarg common.Userio_i,
	intarg int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (df *Devfops_t) Setsockopt(*common.Proc_t, int, int, common.Userio_i, int) common.Err_t {
	return -common.ENOTSOCK
}

func (df *Devfops_t) Shutdown(read, write bool) common.Err_t {
	return -common.ENOTSOCK
}

type rawdfops_t struct {
	sync.Mutex
	minor  int
	offset int
	fs     *Fs_t
}

func (raw *rawdfops_t) Read(p *common.Proc_t, dst common.Userio_i) (int, common.Err_t) {
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

func (raw *rawdfops_t) Write(p *common.Proc_t, src common.Userio_i) (int, common.Err_t) {
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

func (raw *rawdfops_t) Truncate(newlen uint) common.Err_t {
	return -common.EINVAL
}

func (raw *rawdfops_t) Pread(dst common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (raw *rawdfops_t) Pwrite(src common.Userio_i, offset int) (int, common.Err_t) {
	return 0, -common.ESPIPE
}

func (raw *rawdfops_t) Fstat(st *common.Stat_t) common.Err_t {
	raw.Lock()
	defer raw.Unlock()
	st.Wmode(common.Mkdev(common.D_RAWDISK, raw.minor))
	return 0
}

func (raw *rawdfops_t) Mmapi(int, int, bool) ([]common.Mmapinfo_t, common.Err_t) {
	return nil, -common.ENODEV
}

func (raw *rawdfops_t) Pathi() common.Inum_t {
	panic("bad cwd")
}

func (raw *rawdfops_t) Close() common.Err_t {
	return 0
}

func (raw *rawdfops_t) Reopen() common.Err_t {
	return 0
}

func (raw *rawdfops_t) Lseek(off, whence int) (int, common.Err_t) {
	raw.Lock()
	defer raw.Unlock()

	switch whence {
	case common.SEEK_SET:
		raw.offset = off
	case common.SEEK_CUR:
		raw.offset += off
	//case common.SEEK_END:
	default:
		return 0, -common.EINVAL
	}
	if raw.offset < 0 {
		raw.offset = 0
	}
	return raw.offset, 0
}

func (raw *rawdfops_t) Accept(*common.Proc_t, common.Userio_i) (common.Fdops_i, int, common.Err_t) {
	return nil, 0, -common.ENOTSOCK
}

func (raw *rawdfops_t) Bind(*common.Proc_t, []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (raw *rawdfops_t) Connect(proc *common.Proc_t, sabuf []uint8) common.Err_t {
	return -common.ENOTSOCK
}

func (raw *rawdfops_t) Listen(*common.Proc_t, int) (common.Fdops_i, common.Err_t) {
	return nil, -common.ENOTSOCK
}

func (raw *rawdfops_t) Sendmsg(*common.Proc_t, common.Userio_i, []uint8, []uint8,
	int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (raw *rawdfops_t) Recvmsg(*common.Proc_t, common.Userio_i,
	common.Userio_i, common.Userio_i, int) (int, int, int, common.Msgfl_t, common.Err_t) {
	return 0, 0, 0, 0, -common.ENOTSOCK
}

func (raw *rawdfops_t) Pollone(pm common.Pollmsg_t) (common.Ready_t, common.Err_t) {
	return pm.Events & (common.R_READ | common.R_WRITE), 0
}

func (raw *rawdfops_t) Fcntl(proc *common.Proc_t, cmd, opt int) int {
	return int(-common.ENOSYS)
}

func (raw *rawdfops_t) Getsockopt(proc *common.Proc_t, opt int, bufarg common.Userio_i,
	intarg int) (int, common.Err_t) {
	return 0, -common.ENOTSOCK
}

func (raw *rawdfops_t) Setsockopt(*common.Proc_t, int, int, common.Userio_i, int) common.Err_t {
	return -common.ENOTSOCK
}

func (raw *rawdfops_t) Shutdown(read, write bool) common.Err_t {
	return -common.ENOTSOCK
}

func (fs *Fs_t) Fs_mkdir(paths ustr.Ustr, mode int, cwd *common.Cwd_t) common.Err_t {
	opid := fs.fslog.Op_begin("fs_mkdir")
	defer fs.fslog.Op_end(opid)

	fs.istats.Nmkdir.Inc()

	if fs_debug {
		fmt.Printf("mkdir: %v %v\n", paths, cwd)
	}

	dirs, fn := bpath.Sdirname(paths)
	if err, ok := crname(fn, -common.EINVAL); !ok {
		return err
	}
	if len(fn) > DNAMELEN {
		return -common.ENAMETOOLONG
	}

	par, err := fs.fs_namei_locked(opid, dirs, cwd, "mkdir")
	if err != 0 {
		return err
	}
	defer par.iunlock_refdown("fs_mkdir_par")

	child, err := par.do_createdir(opid, fn)
	if err != 0 {
		return err
	}
	child.do_insert(opid, ustr.MkUstrDot(), child.inum)
	child.do_insert(opid, ustr.MkUstrDotDot(), par.inum)
	child.Refdown("fs_mkdir")
	return 0
}

// a type to represent on-disk files
type Fsfile_t struct {
	Inum  common.Inum_t
	Major int
	Minor int
}

func (fs *Fs_t) Fs_open_inner(paths ustr.Ustr, flags common.Fdopt_t, mode int, cwd *common.Cwd_t, major, minor int) (Fsfile_t, common.Err_t) {
	trunc := flags&common.O_TRUNC != 0
	creat := flags&common.O_CREAT != 0
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
		if err, ok := crname(fn, -common.EEXIST); !ok {
			return ret, err
		}

		if len(fn) > DNAMELEN {
			return ret, -common.ENAMETOOLONG
		}

		// with O_CREAT, the file may exist. use itrylock and
		// unlock/retry to avoid deadlock.
		par, err := fs.fs_namei_locked(opid, dirs, cwd, "Fs_open_inner")
		if err != 0 {
			return ret, err
		}
		if isdev {
			idm, err = par.do_createnod(opid, fn, major, minor)
		} else {
			idm, err = par.do_createfile(opid, fn)
		}
		if err != 0 && err != -common.EEXIST {
			par.iunlock_refdown("Fs_open_inner_par")
			return ret, err
		}
		exists := err == -common.EEXIST
		par.iunlock_refdown("Fs_open_inner_par")
		idm.ilock("child")

		oexcl := flags&common.O_EXCL != 0
		if exists {
			if oexcl || isdev {
				idm.iunlock_refdown("Fs_open_inner2")
				return ret, -common.EEXIST
			}
		}
	} else {
		// open existing file
		var err common.Err_t
		idm, err = fs.fs_namei_locked(opid, paths, cwd, "Fs_open_inner_existing")
		if err != 0 {
			return ret, err
		}
		// idm is locked
	}
	defer idm.iunlock_refdown("Fs_open_inner_idm")

	itype := idm.itype

	o_dir := flags&common.O_DIRECTORY != 0
	wantwrite := flags&(common.O_WRONLY|common.O_RDWR) != 0
	if wantwrite {
		nodir = true
	}

	// verify flags: dir cannot be opened with write perms and only dir can
	// be opened with O_DIRECTORY
	if o_dir || nodir {
		if o_dir && itype != I_DIR {
			return ret, -common.ENOTDIR
		}
		if nodir && itype == I_DIR {
			return ret, -common.EISDIR
		}
	}

	if nodir && trunc {
		idm.do_trunc(opid, 0)
	}

	idm.Refup("Fs_open_inner")

	ret.Inum = idm.inum
	ret.Major = idm.major
	ret.Minor = idm.minor
	return ret, 0
}

func (fs *Fs_t) Makefake() *common.Fd_t {
	return nil
	//ret := &common.Fd_t{}
	//priv := common.Inum_t(iroot)
	//fake := &fsfops_t{priv: iroot, fs: fs, count: 1}
	//fake.hack = &imemnode_t{}
	//fake.hack.inum = priv
	//fake.hack.fs = fs
	//if fake.hack.idm_init(priv) != 0 {
	//	panic("no")
	//}
	//ret.Fops = fake
	//return ret
}

// socket files cannot be open(2)'ed (must use connect(2)/sendto(2) etc.)
var _denyopen = map[int]bool{common.D_SUD: true, common.D_SUS: true}

func (fs *Fs_t) Fs_open(paths ustr.Ustr, flags common.Fdopt_t, mode int, cwd *common.Cwd_t, major, minor int) (*common.Fd_t, common.Err_t) {
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
		return nil, -common.EPERM
	}

	// convert on-disk file to fd with fops
	priv := fsf.Inum
	maj := fsf.Major
	min := fsf.Minor
	ret := &common.Fd_t{}
	if maj != 0 {
		// don't need underlying file open
		if fs.Fs_close(fsf.Inum) != 0 {
			panic("must succeed")
		}
		switch maj {
		case common.D_CONSOLE, common.D_DEVNULL, common.D_STAT, common.D_PROF:
			if maj == common.D_STAT {
				stats_string = fs.Fs_statistics()
			}
			ret.Fops = &Devfops_t{Maj: maj, Min: min}
		case common.D_RAWDISK:
			ret.Fops = &rawdfops_t{minor: min, fs: fs}
		default:
			panic("bad dev")
		}
	} else {
		apnd := flags&common.O_APPEND != 0
		ret.Fops = &fsfops_t{priv: priv, fs: fs, append: apnd, count: 1}
	}
	return ret, 0
}

func (fs *Fs_t) Fs_close(priv common.Inum_t) common.Err_t {
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

func (fs *Fs_t) Fs_stat(path ustr.Ustr, st *common.Stat_t, cwd *common.Cwd_t) common.Err_t {
	opid := opid_t(0)

	if fs_debug {
		fmt.Printf("fstat: %v %v\n", path, cwd)
	}
	idm, err := fs.fs_namei_locked(opid, path, cwd, "Fs_stat")
	if err != 0 {
		return err
	}
	err = idm.do_stat(st)
	idm.iunlock_refdown("Fs_stat")
	return err
}

// Sync the file system to disk. XXX If Biscuit supported fsync, we could be
// smarter and flush only the dirty blocks of particular inode.
func (fs *Fs_t) Fs_sync() common.Err_t {
	if !fs.diskfs {
		return 0
	}
	fs.istats.Nsync.Inc()
	fs.fslog.Force(false)
	return 0
}

func (fs *Fs_t) Fs_syncapply() common.Err_t {
	if !fs.diskfs {
		return 0
	}
	fs.istats.Nsync.Inc()
	fs.fslog.Force(true)
	return 0
}

// if the path resolves successfully, returns target inode
func (fs *Fs_t) fs_namei(opid opid_t, paths ustr.Ustr, cwd *common.Cwd_t) (*imemnode_t, common.Err_t) {
	var start *imemnode_t
	fs.istats.Nnamei.Inc()
	if len(paths) == 0 || paths[0] != '/' {
		start = fs.icache.Iref(cwd.Fd.Fops.Pathi(), "fs_namei_cwd")
	} else {
		start = fs.IrefRoot()
	}
	idm := start
	err := common.Err_t(0)
	pp := bpath.Pathparts_t{}
	pp.Pp_init(paths)
	var next ustr.Ustr
	var nextok bool
	for cp, ok := pp.Next(); ok; cp, ok = next, nextok {
		next, nextok = pp.Next()
		n, found := idm.ilookup_lockfree(cp, !nextok)
		if !found {
			// on failure do locked lookup
			if start != idm {
				// XXX check result of Refup and, on failure, start over again
				idm.Refup("fs_namei")
			}
			idm.ilock("fs_namei")
			n, err = idm.ilookup(opid, cp)
			if !common.Resadd_noblock(common.Bounds(common.B_FS_T_FS_NAMEI)) {
				err = -common.ENOHEAP
			}
			if err != 0 {
				idm.iunlock_refdown("fs_namei_ilookup")
				return nil, err
			}
			idm.iunlock("fs_namei")
			if n != idm {
				idm.Refdown("fs_namei_idm")
			}
		}
		// n may have been deleted from dcache and icache, but namei()
		// just reads value from this inode. The inode won't have been
		// GC-ed so it has values. It is ok to read stale values,
		// because namei() makes sure that it has a valid reference to
		// the target inode.
		idm = n
	}
	return idm, 0
}

func (fs *Fs_t) fs_namei_locked(opid opid_t, paths ustr.Ustr, cwd *common.Cwd_t, s string) (*imemnode_t, common.Err_t) {
	idm, err := fs.fs_namei(opid, paths, cwd)
	if err != 0 {
		return nil, err
	}
	idm.ilock(s + "/fs_namei_locked")
	return idm, 0
}

func (fs *Fs_t) Fs_evict() (int, int) {
	if !fs.diskfs {
		panic("no evict")
	}
	fmt.Printf("FS EVICT\n")
	fs.bcache.cache.Evict_half()
	fs.icache.cache.Evict_half()
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
