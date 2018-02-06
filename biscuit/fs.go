package main

import "fmt"
import "sync"
import "strconv"

const memfs = false     // in-memory file system?
const fs_debug = false
const iroot = 0

var superb_start	int
var superb		superblock_t

func fs_init() *fd_t {
	if memfs {
		fmt.Printf("Using MEMORY FS\n")
	}

	// find the first fs block; the build system installs it in block 0 for
	// us
	b, err := log_get_fill(0, "fsoff", false)
	if err != 0 {
		panic("fs_init")
	}
	FSOFF := 506
	superb_start = readn(b.data[:], 4, FSOFF)
	fmt.Printf("superb_start %v\n", superb_start)
	if superb_start <= 0 {
		panic("bad superblock start")
	}
	bcache_relse(b, "fs_init")

	// superblock is never changed, so reading before recovery is fine
	b, err = log_get_fill(superb_start, "super", false)   // don't relse b, because superb is global
	if err != 0 {
		panic("fs_init")
	}
	superb = superblock_t{b.data}

	logstart := superb_start + 1
	loglen := superb.loglen()
	if loglen <= 0 || loglen > 256 {
		panic("bad log len")
	}
	fmt.Printf("logstart %v loglen %v\n", logstart, loglen)
	err = log_init(logstart, loglen)
	if err != 0 {
		panic("log_init failed")
	}

	imapstart := superb.imapblock()
        imaplen := superb.imaplen()
	fmt.Printf("imapstart %v imaplen %v\n", imapstart, imaplen)

	bmapstart := superb.freeblock()
	bmaplen := superb.freeblocklen()
	fmt.Printf("bmapstart %v bmaplen %v\n", bmapstart, bmaplen)

	ialloc_init(imapstart, imaplen, bmapstart+bmaplen)


	inodelen := superb.inodelen()
	fmt.Printf("inodelen %v\n", inodelen)

	balloc_init(bmapstart, bmaplen, bmapstart + bmaplen + inodelen)

	return &fd_t{fops: &fsfops_t{priv: iroot}}
}

func fs_statistics() string {
	s := inode_stats()
	s += log_stat()
	s += ialloc_stat()
	s += balloc_stat()
	s += bcache_stat()
	s += icache_stat()
	s += ahci_stat()
	return s
}


// a type for an inum
type inum_t int

func fs_link(old string, new string, cwd inum_t) err_t {
	op_begin("fs_link")
	defer op_end()

	if fs_debug {
		fmt.Printf("fs_link: %v %v %v\n", old, new, cwd)
	}

	istats.nilink++

	orig, err := fs_namei_locked(old, cwd, "fs_link_org")
	if err != 0 {
		return err
	}
	if orig.icache.itype != I_FILE {
		orig.iunlock_refdown("fs_link")
		return -EINVAL
	}
	inum := orig.inum
	orig._linkup()
	orig.iunlock_refdown("fs_link_orig")

	dirs, fn := sdirname(new)
	newd, err := fs_namei_locked(dirs, cwd, "fs_link_newd")
	if err != 0 {
		goto undo
	}
	err = newd.do_insert(fn, inum)
	newd.iunlock_refdown("fs_link_newd")
	if err != 0 {
		goto undo
	}
	return 0
undo:
	orig, err1 := iref_locked(inum, "fs_link_undo")
	if err1 != 0 {
		panic("bizare")
	}
	orig._linkdown()
	orig.iunlock_refdown("fs_link_undo")
	return err
}

func fs_unlink(paths string, cwd inum_t, wantdir bool) err_t {
	dirs, fn := sdirname(paths)
	if fn == "." || fn == ".." {
		return -EPERM
	}

	op_begin("fs_unlink")
	defer op_end()

	istats.nunlink++

	if fs_debug {
		fmt.Printf("fs_unlink: %v cwd %v dir? %v\n", paths, cwd, wantdir)
	}

	var child *imemnode_t
	var par *imemnode_t
	var err err_t
	var childi inum_t

	par, err = fs_namei_locked(dirs, cwd, "fs_unlink_par")
	if err != 0 {
		return err
	}
	childi, err = par.ilookup(fn)
	if err != 0 {
		par.iunlock_refdown("fs_unlink_par")
		return err
	}
	child, err = iref(childi, "fs_unlink_child")
	if err != 0 {
		par.iunlock_refdown("fs_unlink_par")
		return err
	}
	// unlock parent after we have a ref to child
	par.iunlock("fs_unlink_par")

	// acquire both locks
	iref_lockall([]*imemnode_t{par, child})
	defer par.iunlock_refdown("fs_unlink_par")
	defer child.iunlock_refdown("fs_unlink_child")
	
	// recheck if child still exists (same name, same inum), since some
	// other thread may have modifed par but par and child won't disappear
	// because we have references to them.
	inum, err := par.ilookup(fn)
	if err != 0 {
		return err
	}
	// name was deleted and recreated?
	if inum != childi {
		return -ENOENT
	}

	err = child.do_dirchk(wantdir)
	if err != 0 {
		return err
	}

	// finally, remove directory entry
	err = par.do_unlink(fn)
	if err != 0 {
		return err
	}
	child._linkdown()

	return 0
}

// per-volume rename mutex. Linux does it so it must be OK!
var _renamelock = sync.Mutex{}

func fs_rename(oldp, newp string, cwd inum_t) err_t {
	odirs, ofn := sdirname(oldp)
	ndirs, nfn := sdirname(newp)

	if err, ok := crname(ofn, -EINVAL); !ok {
		return err
	}
	if err, ok := crname(nfn, -EINVAL); !ok {
		return err
	}

	op_begin("fs_rename")
	defer op_end()

	istats.nrename++
	
	if fs_debug {
		fmt.Printf("fs_rename: src %v dst %v %v\n", oldp, newp, cwd)
	}

	// one rename at the time
	_renamelock.Lock()
	defer _renamelock.Unlock()

	// lookup all inode references, but we will release locks and lock them
	// together when we know all references.  the references to the inodes
	// cannot disppear, however.
	opar, err := fs_namei_locked(odirs, cwd, "fs_rename_opar")
	if err != 0 {
		return err
	}

	childi, err := opar.ilookup(ofn)
	if err != 0 {
		opar.iunlock_refdown("fs_rename_opar")
		return err
	}
	ochild, err := iref(childi, "fs_rename_ochild")
	if err != 0 {
		opar.iunlock_refdown("fs_rename_opar")
		return err
	}
	// unlock par after we have ref to child
	opar.iunlock("fs_rename_par")

	npar, err := fs_namei(ndirs, cwd)
	if err != 0 {
		irefcache.refdown(opar, "fs_rename_opar")
		irefcache.refdown(ochild, "fs_rename_ochild")
		return err
	}
	
	// verify that ochild is not an ancestor of npar, since we would
	// disconnect ochild subtree from root.  it is safe to do without
	// holding locks because unlink cannot modify the path to the root by
	// removing a directory because the directories aren't empty.  it could
	// delete npar and an ancestor, but rename has already a reference to to
	// npar.
	if err = _isancestor(ochild, npar); err != 0 {
		irefcache.refdown(opar, "fs_rename_opar")
		irefcache.refdown(ochild, "fs_rename_ochild")
		irefcache.refdown(npar, "fs_rename_npar")
		return err
	}

	var nchild *imemnode_t
	cnt := 0
	newexists := false
	// lookup newchild and try to lock all inodes involved
	for {
		npar.Lock()
		nchildinum, err := npar.ilookup(nfn)
		if err != 0 && err != -ENOENT {
			irefcache.refdown(opar, "fs_name_opar")
			irefcache.refdown(ochild, "fs_name_ochild")	
			npar.iunlock_refdown("fs_name_npar")
			return err
		}
		var err1 err_t
		nchild, err1 = iref(nchildinum, "fs_rename_ochild")
		if err1 != 0 {
			irefcache.refdown(opar, "fs_name_opar")
			irefcache.refdown(ochild, "fs_name_ochild")	
			npar.iunlock_refdown("fs_name_npar")
			return err
		}
		npar.Unlock()

		var inodes []*imemnode_t
		var locked []*imemnode_t
		if err == 0 {
			newexists = true
			inodes = []*imemnode_t{opar, ochild, npar, nchild}
		} else {
			inodes = []*imemnode_t{opar, ochild, npar}
		}
		
		locked = iref_lockall(inodes)
		// defers are run last-in-first-out
		for _, v := range inodes {
			defer irefcache.refdown(v, "rename")
		}

		for _, v := range locked {
			defer v.iunlock("rename")
		}
		
		// check if the tree is still the same. an unlink or link may
		// have modified the tree.
		childi, err := opar.ilookup(ofn) 
		if err != 0 {
			return err
		}
		// has ofn been removed but a new file ofn has been created?
		if childi != ochild.inum {
			return -ENOENT
		}
		
		childi, err = npar.ilookup(nfn)
		// it existed before and still exists
		if newexists && err == 0 && childi == nchild.inum { 
			break
		}
		// it didn't exist before and still doesn't exist
		if !newexists && err == -ENOENT {
			break
		}
		// it existed but now it doesn't.
		if newexists && err == -ENOENT {
			newexists = false
			nchild.iunlock_refdown("rename_child")
			break
		}

		cnt++
		fmt.Printf("rename: retry %v %v\n", newexists, err)
		if cnt > 100 {
			panic("rename: panic\n")
		}

		// ochildi changed or childi was created; retry, to grab also its lock
		for _, v := range locked {
			v.iunlock("fs_rename_opar")
		}
		if newexists {
			irefcache.refdown(nchild, "fs_rename_nchild")
		}
	}

	// if src and dst are the same file, we are done
	if newexists && ochild.inum == nchild.inum { 
		return 0
	}

	// guarantee that any page allocations will succeed before starting the
	// operation, which will be messy to piece-wise undo.
	b1, err := npar.probe_insert()
	if err != 0 {
		return err
	}
	defer bcache_relse(b1, "probe_insert")
	
	b2, err := opar.probe_unlink(ofn)
	if err != 0 {
		return err
	}
	defer bcache_relse(b2, "probe_unlink_opar")

	odir := ochild.icache.itype == I_DIR
	if odir {
		b3, err := ochild.probe_unlink("..")
		if err != 0 {
			return err
		}
		defer bcache_relse(b3, "probe_unlink_ochild")
	}

	if newexists {
		// make sure old and new are either both files or both
		// directories
		if err := nchild.do_dirchk(odir); err != 0 {
			return err
		}

		// remove pre-existing new
		if err = npar.do_unlink(nfn); err != 0 {
			return err
		}
		nchild._linkdown()
	}

	// finally, do the move
	if opar.do_unlink(ofn) != 0 {
		panic("probed")
	}
	if npar.do_insert(nfn, ochild.inum) != 0 {
		panic("probed")
	}

	// update '..'
	if odir {
		if ochild.do_unlink("..") != 0 {
			panic("probed")
		}
		if ochild.do_insert("..", npar.inum) != 0 {
			panic("insert after unlink must succeed")
		}
	}
	return 0
}

// anc and start are in memory
func _isancestor(anc, start *imemnode_t) err_t {
	if anc.inum == iroot {
		panic("root is always ancestor")
	}
	// walk up to iroot
	here, err := iref(start.inum, "_isancestor")
	if err != 0 {
		panic("_isancestor: start must exist")
	}
	for here.inum != iroot {
		if anc == here {
			irefcache.refdown(here, "_isancestor_here")
			return -EINVAL
		}
		here.ilock("_isancestor")
		nexti, err := here.ilookup("..")
		if err != 0 {
			panic(".. must exist")
		}
		if nexti == here.inum {
			here.iunlock("_isancestor")
			panic("xxx")
		} else {
			var next *imemnode_t
			next, err = iref(nexti, "_isancestor_next")
			here.iunlock_refdown("_isancestor")
			if err != 0 {
				return err
			}
			here = next
		}
	}
	irefcache.refdown(here, "_isancestor")
	return 0
}

type fsfops_t struct {
	priv	inum_t
	// protects offset
	sync.Mutex
	offset	int
	append	bool
}

func (fo *fsfops_t) _read(dst userio_i, toff int) (int, err_t) {
	// lock the file to prevent races on offset and closing
	fo.Lock()
	defer fo.Unlock()

	useoffset := toff != -1
	offset := fo.offset
	if useoffset {
		// XXXPANIC
		if toff < 0 {
			panic("neg offset")
		}
		offset = toff
	}
	idm, err := iref_locked(fo.priv, "_read")
	if err != 0 {
		return 0, err
	}
	did, err := idm.do_read(dst, offset)
	if !useoffset && err == 0 {
		fo.offset += did
	}
	idm.iunlock_refdown("_read")
	return did, err
}

func (fo *fsfops_t) read(p *proc_t, dst userio_i) (int, err_t) {
	return fo._read(dst, -1)
}

func (fo *fsfops_t) pread(dst userio_i, offset int) (int, err_t) {
	return fo._read(dst, offset)
}

func (fo *fsfops_t) _write(src userio_i, toff int) (int, err_t) {
	// lock the file to prevent races on offset and closing
	fo.Lock()
	defer fo.Unlock()

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
	idm, err := iref_locked(fo.priv, "_write")
	if err != 0 {
		return 0, err
	}
	did, err := idm.do_write(src, offset, append)
	if !useoffset && err == 0 {
		fo.offset += did
	}
	idm.iunlock_refdown("_write")
	return did, err
}

func (fo *fsfops_t) write(p *proc_t, src userio_i) (int, err_t) {
	return fo._write(src, -1)
}

func (fo *fsfops_t) fullpath() (string, err_t) {
	fp, err := _fullpath(fo.priv)
	return fp, err
}

func (fo *fsfops_t) truncate(newlen uint) err_t {
	op_begin("truncate")
	defer op_end()

	if fs_debug {
		fmt.Printf("truncate: %v %v\n", fo.priv, newlen)
	}

	idm, err := iref_locked(fo.priv, "truncate")
	if err != 0 {
		return err
	}
	err = idm.do_trunc(newlen)
	idm.iunlock_refdown("truncate")
	return err
}

func (fo *fsfops_t) pwrite(src userio_i, offset int) (int, err_t) {
	return fo._write(src, offset)
}

func (fo *fsfops_t) fstat(st *stat_t) err_t {
	if fs_debug {
		fmt.Printf("fstat: %v %v\n", fo.priv, st)
	}
	idm, err := iref_locked(fo.priv, "fstat")
	if err != 0 {
		return err
	}
	err = idm.do_stat(st)
	idm.iunlock_refdown("fstat")
	return err
}

// XXX log those files that have no fs links but > 0 memory references to the
// journal so that if we crash before freeing its blocks, the blocks can be
// reclaimed.
func (fo *fsfops_t) close() err_t {
	return fs_close(fo.priv)
}

func (fo *fsfops_t) pathi() inum_t {
	return fo.priv
}

func (fo *fsfops_t) reopen() err_t {
	idm, err := iref_locked(fo.priv, "reopen")
	if err != 0 {
		return err
	}
	istats.nreopen++
	irefcache.refup(idm, "reopen")   // close will decrease it
	idm.iunlock_refdown("reopen")
	return 0
}

func (fo *fsfops_t) lseek(off, whence int) (int, err_t) {
	// prevent races on fo.offset
	fo.Lock()
	defer fo.Unlock()

	istats.nlseek++
	
	switch whence {
	case SEEK_SET:
		fo.offset = off
	case SEEK_CUR:
		fo.offset += off
	case SEEK_END:
		st := &stat_t{}
		fo.fstat(st)
		fo.offset = int(st.size()) + off
	default:
		return 0, -EINVAL
	}
	if fo.offset < 0 {
		fo.offset = 0
	}
	return fo.offset, 0
}

// returns the mmapinfo for the pages of the target file. the page cache is
// populated if necessary.
func (fo *fsfops_t) mmapi(offset, len int, inc bool) ([]mmapinfo_t, err_t) {
	idm, err := iref_locked(fo.priv, "mmapi")
	if err != 0 {
		return nil, err
	}
	mmi, err := idm.do_mmapi(offset, len, inc)
	idm.iunlock_refdown("mmapi")
	return mmi, err
}

func (fo *fsfops_t) accept(*proc_t, userio_i) (fdops_i, int, err_t) {
	return nil, 0, -ENOTSOCK
}

func (fo *fsfops_t) bind(*proc_t, []uint8) err_t {
	return -ENOTSOCK
}

func (fo *fsfops_t) connect(proc *proc_t, sabuf []uint8) err_t {
	return -ENOTSOCK
}

func (fo *fsfops_t) listen(*proc_t, int) (fdops_i, err_t) {
	return nil, -ENOTSOCK
}

func (fo *fsfops_t) sendmsg(*proc_t, userio_i, []uint8, []uint8,
    int) (int, err_t) {
	return 0, -ENOTSOCK
}

func (fo *fsfops_t) recvmsg(*proc_t, userio_i,
    userio_i, userio_i, int) (int, int, int, msgfl_t, err_t) {
	return 0, 0, 0, 0, -ENOTSOCK
}

func (fo *fsfops_t) pollone(pm pollmsg_t) (ready_t, err_t) {
	return pm.events & (R_READ | R_WRITE), 0
}

func (fo *fsfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	return int(-ENOSYS)
}

func (fo *fsfops_t) getsockopt(proc *proc_t, opt int, bufarg userio_i,
    intarg int) (int, err_t) {
	return 0, -ENOTSOCK
}

func (fo *fsfops_t) setsockopt(*proc_t, int, int, userio_i, int) err_t {
	return -ENOTSOCK
}

func (fo *fsfops_t) shutdown(read, write bool) err_t {
	return -ENOTSOCK
}

type devfops_t struct {
	maj	int
	min	int
}

func (df *devfops_t) _sane() {
	// make sure this maj/min pair is handled by devfops_t. to handle more
	// devices, we can either do dispatch in devfops_t or we can return
	// device-specific fdops_i in fs_open()
	if df.maj != D_CONSOLE && df.maj != D_DEVNULL && df.maj != D_STAT  {
		panic("bad dev")
	}
}

func (df *devfops_t) read(p *proc_t, dst userio_i) (int, err_t) {
	df._sane()
	if df.maj == D_CONSOLE {
		return cons_read(dst, 0)
	} else if df.maj == D_STAT {
		return stat_read(dst, 0)
	} else {
		return 0, 0
	}
}

func (df *devfops_t) write(p *proc_t, src userio_i) (int, err_t) {
	df._sane()
	if df.maj == D_CONSOLE {
		return cons_write(src, 0)
	} else {
		return src.totalsz(), 0
	}
}

func (df *devfops_t) fullpath() (string, err_t) {
	panic("weird cwd")
}

func (df *devfops_t) truncate(newlen uint) err_t {
	return -EINVAL
}

func (df *devfops_t) pread(dst userio_i, offset int) (int, err_t) {
	df._sane()
	return 0, -ESPIPE
}

func (df *devfops_t) pwrite(src userio_i, offset int) (int, err_t) {
	df._sane()
	return 0, -ESPIPE
}

func (df *devfops_t) fstat(st *stat_t) err_t {
	df._sane()
	st.wmode(mkdev(df.maj, df.min))
	return 0
}

func (df *devfops_t) mmapi(int, int, bool) ([]mmapinfo_t, err_t) {
	df._sane()
	return nil, -ENODEV
}

func (df *devfops_t) pathi() inum_t {
	df._sane()
	panic("bad cwd")
}

func (df *devfops_t) close() err_t {
	df._sane()
	return 0
}

func (df *devfops_t) reopen() err_t {
	df._sane()
	return 0
}

func (df *devfops_t) lseek(int, int) (int, err_t) {
	df._sane()
	return 0, -ESPIPE
}

func (df *devfops_t) accept(*proc_t, userio_i) (fdops_i, int, err_t) {
	return nil, 0, -ENOTSOCK
}

func (df *devfops_t) bind(*proc_t, []uint8) err_t {
	return -ENOTSOCK
}

func (df *devfops_t) connect(proc *proc_t, sabuf []uint8) err_t {
	return -ENOTSOCK
}

func (df *devfops_t) listen(*proc_t, int) (fdops_i, err_t) {
	return nil, -ENOTSOCK
}

func (df *devfops_t) sendmsg(*proc_t, userio_i, []uint8, []uint8,
    int) (int, err_t) {
	return 0, -ENOTSOCK
}

func (df *devfops_t) recvmsg(*proc_t, userio_i,
    userio_i, userio_i, int) (int, int, int, msgfl_t, err_t) {
	return 0, 0, 0, 0, -ENOTSOCK
}

func (df *devfops_t) pollone(pm pollmsg_t) (ready_t, err_t) {
	switch df.maj {
	case D_CONSOLE:
		cons.pollc <- pm
		return <- cons.pollret, 0
	case D_DEVNULL:
		return pm.events & (R_READ | R_WRITE), 0
	default:
		panic("which dev")
	}
}

func (df *devfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	return int(-ENOSYS)
}

func (df *devfops_t) getsockopt(proc *proc_t, opt int, bufarg userio_i,
    intarg int) (int, err_t) {
	return 0, -ENOTSOCK
}

func (df *devfops_t) setsockopt(*proc_t, int, int, userio_i, int) err_t {
	return -ENOTSOCK
}

func (df *devfops_t) shutdown(read, write bool) err_t {
	return -ENOTSOCK
}

type rawdfops_t struct {
	sync.Mutex
	minor	int
	offset	int
}

func (raw *rawdfops_t) read(p *proc_t, dst userio_i) (int, err_t) {
	raw.Lock()
	defer raw.Unlock()
	var did int
	for dst.remain() != 0 {
		blkno := raw.offset / BSIZE
		b, err := log_get_fill(blkno, "read", false)
		if err != 0 {
			return 0, err
		}
		boff := raw.offset % BSIZE
		c, err := dst.uiowrite(b.data[boff:])
		if err != 0 {
			return 0, err
		}
		raw.offset += c
		did += c
		bcache_relse(b, "read")
	}
	return did, 0
}

func (raw *rawdfops_t) write(p *proc_t, src userio_i) (int, err_t) {
	raw.Lock()
	defer raw.Unlock()
	var did int
	for src.remain() != 0 {
		blkno := raw.offset / BSIZE
		boff := raw.offset % BSIZE
		// if boff != 0 || src.remain() < 512 {
		//	buf := bdev_read_block(blkno)
		//}
		// XXX don't always have to read block in from disk
		buf, err := log_get_fill(blkno, "write", false)
		if err != 0 {
			return 0, err
		}
		c, err := src.uioread(buf.data[boff:])
		if err != 0 {
			return 0, err
		}
		bcache_write(buf)
		raw.offset += c
		did += c
		bcache_relse(buf, "write")
	}
	return did, 0
}

func (raw *rawdfops_t) fullpath() (string, err_t) {
	panic("weird cwd")
}

func (raw *rawdfops_t) truncate(newlen uint) err_t {
	return -EINVAL
}

func (raw *rawdfops_t) pread(dst userio_i, offset int) (int, err_t) {
	return 0, -ESPIPE
}

func (raw *rawdfops_t) pwrite(src userio_i, offset int) (int, err_t) {
	return 0, -ESPIPE
}

func (raw *rawdfops_t) fstat(st *stat_t) err_t {
	raw.Lock()
	defer raw.Unlock()
	st.wmode(mkdev(D_RAWDISK, raw.minor))
	return 0
}

func (raw *rawdfops_t) mmapi(int, int, bool) ([]mmapinfo_t, err_t) {
	return nil, -ENODEV
}

func (raw *rawdfops_t) pathi() inum_t {
	panic("bad cwd")
}

func (raw *rawdfops_t) close() err_t {
	return 0
}

func (raw *rawdfops_t) reopen() err_t {
	return 0
}

func (raw *rawdfops_t) lseek(off, whence int) (int, err_t) {
	raw.Lock()
	defer raw.Unlock()

	switch whence {
	case SEEK_SET:
		raw.offset = off
	case SEEK_CUR:
		raw.offset += off
	//case SEEK_END:
	default:
		return 0, -EINVAL
	}
	if raw.offset < 0 {
		raw.offset = 0
	}
	return raw.offset, 0
}

func (raw *rawdfops_t) accept(*proc_t, userio_i) (fdops_i, int, err_t) {
	return nil, 0, -ENOTSOCK
}

func (raw *rawdfops_t) bind(*proc_t, []uint8) err_t {
	return -ENOTSOCK
}

func (raw *rawdfops_t) connect(proc *proc_t, sabuf []uint8) err_t {
	return -ENOTSOCK
}

func (raw *rawdfops_t) listen(*proc_t, int) (fdops_i, err_t) {
	return nil, -ENOTSOCK
}

func (raw *rawdfops_t) sendmsg(*proc_t, userio_i, []uint8, []uint8,
    int) (int, err_t) {
	return 0, -ENOTSOCK
}

func (raw *rawdfops_t) recvmsg(*proc_t, userio_i,
    userio_i, userio_i, int) (int, int, int, msgfl_t, err_t) {
	return 0, 0, 0, 0, -ENOTSOCK
}

func (raw *rawdfops_t) pollone(pm pollmsg_t) (ready_t, err_t) {
	return pm.events & (R_READ | R_WRITE), 0
}

func (raw *rawdfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	return int(-ENOSYS)
}

func (raw *rawdfops_t) getsockopt(proc *proc_t, opt int, bufarg userio_i,
    intarg int) (int, err_t) {
	return 0, -ENOTSOCK
}

func (raw *rawdfops_t) setsockopt(*proc_t, int, int, userio_i, int) err_t {
	return -ENOTSOCK
}

func (raw *rawdfops_t) shutdown(read, write bool) err_t {
	return -ENOTSOCK
}

func fs_mkdir(paths string, mode int, cwd inum_t) err_t {
	op_begin("fs_mkdir")
	defer op_end()

	istats.nmkdir++

	if fs_debug {
		fmt.Printf("mkdir: %v %v\n", paths, cwd)
	}

	dirs, fn := sdirname(paths)
	if err, ok := crname(fn, -EINVAL); !ok {
		return err
	}
	if len(fn) > DNAMELEN {
		return -ENAMETOOLONG
	}

	par, err := fs_namei_locked(dirs, cwd, "mkdir")
	if err != 0 {
		return err
	}
	defer par.iunlock_refdown("fs_mkdir_par")

	var childi inum_t
	childi, err = par.do_createdir(fn)
	if err != 0 {
		return err
	}

	child, err := iref(childi, "fs_mkdir_child")
	if err != 0 {
		par.create_undo(childi, fn)
		return err
	}

	child.do_insert(".", childi)
	child.do_insert("..", par.inum)
	irefcache.refdown(child, "fs_mkdir3")
	return 0
}

// a type to represent on-disk files
type fsfile_t struct {
	inum	inum_t
	major	int
	minor	int
}

func _fs_open(paths string, flags fdopt_t, mode int, cwd inum_t,  major, minor int) (fsfile_t, err_t) {
	trunc := flags & O_TRUNC != 0
	creat := flags & O_CREAT != 0
	nodir := false

	if fs_debug {
		fmt.Printf("fs_open: %v %v %v\n", paths, cwd, creat)
	}

	// open with O_TRUNC is not read-only
	if trunc || creat {
		op_begin("fs_open")
		defer op_end()
	}
	var ret fsfile_t
	var idm *imemnode_t
	if creat {
		nodir = true
		// creat w/execl; must atomically create and open the new file.
		isdev := major != 0 || minor != 0

		// must specify at least one path component
		dirs, fn := sdirname(paths)
		if err, ok := crname(fn, -EEXIST); !ok {
			return ret, err
		}

		if len(fn) > DNAMELEN {
			return ret, -ENAMETOOLONG
		}

		var exists bool
		// with O_CREAT, the file may exist. use itrylock and
		// unlock/retry to avoid deadlock.
		for {
			par, err := fs_namei_locked(dirs, cwd, "_fs_open")
			if err != 0 {
				return ret, err
			}
			defer par.iunlock_refdown("_fs_open_par")

			var childi inum_t
			if isdev {
				childi, err = par.do_createnod(fn, major, minor)
			} else {
				childi, err = par.do_createfile(fn)
			}
			if err != 0 && err != -EEXIST {
				return ret, err
			}
			exists = err == -EEXIST
			if childi <= 0 {
				panic("non-positive childi\n")
			}
			idm, err = iref_locked(childi, "_fs_open_child")
			if err != 0 {
				par.create_undo(childi, fn)
				return ret, err
			}
			break
		}
		oexcl := flags & O_EXCL != 0
		if exists {
			if oexcl || isdev {
				idm.iunlock_refdown("_fs_open2")
				return ret, -EEXIST
			}
		}
	} else {
		// open existing file
		var err err_t
		idm, err = fs_namei_locked(paths, cwd, "_fs_open_existing")
		if err != 0 {
			return ret, err
		}
		// idm is locked
	}
	defer idm.iunlock_refdown("_fs_open_idm")

	itype := idm.icache.itype

	o_dir := flags & O_DIRECTORY != 0
	wantwrite := flags & (O_WRONLY|O_RDWR) != 0
	if wantwrite {
		nodir = true
	}

	// verify flags: dir cannot be opened with write perms and only dir can
	// be opened with O_DIRECTORY
	if o_dir || nodir {
		if o_dir && itype != I_DIR {
			return ret, -ENOTDIR
		}
		if nodir && itype == I_DIR {
			return ret, -EISDIR
		}
	}

	if nodir && trunc {
		idm.do_trunc(0)
	}

	irefcache.refup(idm, "_fs_open")

	ret.inum = idm.inum
	ret.major = idm.icache.major
	ret.minor = idm.icache.minor
	return ret, 0
}

// socket files cannot be open(2)'ed (must use connect(2)/sendto(2) etc.)
var _denyopen = map[int]bool{ D_SUD: true, D_SUS: true}

func fs_open(paths string, flags fdopt_t, mode int, cwd inum_t,  major, minor int) (*fd_t, err_t) {
	istats.nopen++
	fsf, err := _fs_open(paths, flags, mode, cwd, major, minor)
	if err != 0 {
		return nil, err
	}

	// some special files (sockets) cannot be opened with fops this way
	if denied := _denyopen[fsf.major]; denied {
		if fs_close(fsf.inum) != 0 {
			panic("must succeed")
		}
		return nil, -EPERM
	}

	// convert on-disk file to fd with fops
	priv := fsf.inum
	maj := fsf.major
	min := fsf.minor
	ret := &fd_t{}
	if maj != 0 {
		// don't need underlying file open
		if fs_close(fsf.inum) != 0 {
			panic("must succeed")
		}
		switch maj {
		case D_CONSOLE, D_DEVNULL, D_STAT:
			if maj == D_STAT {
				stats_string = fs_statistics()
			}
			ret.fops = &devfops_t{maj: maj, min: min}
		case D_RAWDISK:
			ret.fops = &rawdfops_t{minor: min}
		default:
			panic("bad dev")
		}
	} else {
		apnd := flags & O_APPEND != 0
		ret.fops = &fsfops_t{priv: priv, append: apnd}
	}
	return ret, 0
}

func fs_close(priv inum_t) err_t {
	defer op_end()
	op_begin("fs_close")

	istats.nclose++
	
	if fs_debug {
		fmt.Printf("fs_close: %v\n", priv)
	}

	idm, err := iref_locked(priv, "fs_close")
	if err != 0 {
		return err
	}
	irefcache.refdown(idm, "fs_close")
	idm.iunlock_refdown("fs_close")
	return 0
}

func fs_stat(path string, st *stat_t, cwd inum_t) err_t {
	if fs_debug {
		fmt.Printf("fstat: %v %v\n", path, cwd)
	}
	idm, err := fs_namei_locked(path, cwd, "fs_stat")
	if err != 0 {
		return err
	}
	err = idm.do_stat(st)
	idm.iunlock_refdown("fs_stat")
	return err
}

func fs_sync() err_t {
	if memfs {
		return 0
	}
	istats.nsync++
	log_force()
	return 0
}

// if the path resolves successfully, returns the idaemon locked. otherwise,
// locks no idaemon.
func fs_namei(paths string, cwd inum_t) (*imemnode_t, err_t) {
	var start *imemnode_t
	var err err_t
	istats.nnamei++
	if len(paths) == 0 || paths[0] != '/' {
		start, err = iref(cwd, "fs_namei_cwd")
		if err != 0 {
			panic("cannot load cwd")
		}
	} else {
		start, err = iref(iroot, "fs_namei_root")
		if err != 0 {
			panic("cannot load iroot")
		}
	}
	idm := start
	pp := pathparts_t{}
	pp.pp_init(paths)
	for cp, ok := pp.next(); ok; cp, ok = pp.next() {
		idm.ilock("fs_namei")
		n, err := idm.ilookup(cp)
		if err != 0 {
			idm.iunlock_refdown("fs_namei_ilookup")
			return nil, err
		}
		if n != idm.inum {
			next, err := iref(n, "fs_namei_next")
			idm.iunlock_refdown("fs_namei_idm")
			if err != 0 {
				return nil, err
			}
			idm = next
		} else {
			idm.iunlock("fs_namei_idm_next")
		}
	}
	return idm, 0
}

func fs_namei_locked(paths string, cwd inum_t, s string) (*imemnode_t, err_t) {
	idm, err := fs_namei(paths, cwd)
	if err != 0 {
		return nil, err
	}
	idm.ilock(s + "/fs_namei_locked")
	return idm, 0
}


// superblock format (see mkbdisk.py)
type superblock_t struct {
	data	*bytepg_t
}

func (sb *superblock_t) loglen() int {
	return fieldr(sb.data, 0)
}

func (sb *superblock_t) imapblock() int {
	return fieldr(sb.data, 1)
}

func (sb *superblock_t) imaplen() int {
	return fieldr(sb.data, 2)
}

func (sb *superblock_t) freeblock() int {
	return fieldr(sb.data, 3)
}

func (sb *superblock_t) freeblocklen() int {
	return fieldr(sb.data, 4)
}

func (sb *superblock_t) inodelen() int {
	return fieldr(sb.data, 5)
}

func (sb *superblock_t) lastblock() int {
	return fieldr(sb.data, 6)
}

// Bitmap allocater. Used for inodes and blocks
type allocater_t struct {
	sync.Mutex
	freestart  int
	freelen  int
	lastblk int
	lastbyte int

	// stats
	nalloc int
	nfree int
	nhit  int
}

func make_allocater(start, len int) (*allocater_t) {
	a := &allocater_t{}
	a.freestart = start
	a.freelen = len
	return a
}

func freebit(b uint8) uint {
	for m := uint(0); m < 8; m++ {
		if (1 << m) & b == 0 {
			return m
		}
	}
	panic("no 0 bit?")
}

func (alloc *allocater_t) fbread(blockno int) (*bdev_block_t, err_t) {
	if blockno < alloc.freestart || blockno >= alloc.freestart+alloc.freelen {
		panic("naughty blockno")
	}
	return log_get_fill(blockno, "fbread", true)
}

func (alloc *allocater_t) alloc() (int, err_t) {
	alloc.Lock()
	defer alloc.Unlock()

	found := false
	hit := true
	var bit uint
	var blk *bdev_block_t
	var blkn int
	var oct int
	var err err_t
	
	// 0 is free, 1 is allocated
	for b := 0; b < alloc.freelen && !found; b++ {
		i := (alloc.lastblk + b) % alloc.freelen
		if blk != nil {
			blk.Unlock()
			bcache_relse(blk, "alloc")
		}
		blk, err = alloc.fbread(alloc.freestart + i)
		if err != 0 {
			return 0, err
		}
		start := 0
		if b == 0 {
			start = alloc.lastbyte
		}
		for idx := start; idx < len(blk.data); idx++ {
			c := blk.data[idx]
			if c != 0xff {
				alloc.lastblk = i
				alloc.lastbyte = idx
				bit = freebit(c)
				blkn = i
				oct = idx
				found = true
				break
			} else {
				hit = false
			}
		}
	}
	if !found {
		panic("no free entries")
	}
	alloc.nalloc++
	if hit {
		alloc.nhit++
	}

	// mark as allocated
	blk.data[oct] |= 1 << bit
	blk.Unlock()
	blk.log_write()
	bcache_relse(blk, "balloc1")

	bitsperblk := BSIZE*8
	blkn = blkn*bitsperblk + oct*8 + int(bit)
	return blkn, 0
}


func (alloc *allocater_t) free(blkno int) err_t {
	alloc.Lock()
	defer alloc.Unlock()

	if fs_debug {
		fmt.Printf("bfree: %v\n", blkno)
	}
	
	if blkno < 0 {
		panic("free bad blockno")
	}

	bit := blkno
	bitsperblk := BSIZE*8
	fblkno := alloc.freestart + bit/bitsperblk
	fbit := bit%bitsperblk
	fbyteoff := fbit/8
	fbitoff := uint(fbit%8)
	if fblkno >= alloc.freestart + alloc.freelen {
		panic("free: bad blockno")
	}
	fblk, err := alloc.fbread(fblkno)
	if err != 0 {
		return err
	}
	fblk.data[fbyteoff] &= ^(1 << fbitoff)
	fblk.Unlock()
	fblk.log_write()
	bcache_relse(fblk, "free")
	alloc.nfree++
	return 0
}

func (alloc *allocater_t) stat() string {
	s := "allocater: #alloc "
	s += strconv.Itoa(alloc.nalloc)
	s += " #free "
	s += strconv.Itoa(alloc.nfree)
	s += " #hit "
	s += strconv.Itoa(alloc.nhit)
	s += "\n"
	return s
}
