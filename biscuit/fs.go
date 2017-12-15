package main

import "fmt"
import "strings"
import "sync"
import "unsafe"
import "sort"

const NAME_MAX    int = 512

const LOGINODEBLK     = 5 // 2
const INODEMASK       = (1 << LOGINODEBLK)-1
const INDADDR        = (BSIZE/8)-1

const fs_debug    = false

var superb_start	int
var superb		superblock_t
var iroot		inum
var free_start		int
var free_len		int
var usable_start	int

var fblock	= sync.Mutex{}

// allocation-less pathparts
type pathparts_t struct {
	path	string
	loc	int
}

func (pp *pathparts_t) pp_init(path string) {
	pp.path = path
	pp.loc = 0
}

func (pp *pathparts_t) next() (string, bool) {
	ret := ""
	for ret == "" {
		if pp.loc == len(pp.path) {
			return "", false
		}
		ret = pp.path[pp.loc:]
		nloc := strings.IndexByte(ret, '/')
		if nloc != -1 {
			ret = ret[:nloc]
			pp.loc += nloc + 1
		} else {
			pp.loc += len(ret)
		}
	}
	return ret, true
}

func sdirname(path string) (string, string) {
	fn := path
	l := len(fn)
	// strip all trailing slashes
	for i := l - 1; i >= 0; i-- {
		if fn[i] != '/' {
			break
		}
		fn = fn[:i]
		l--
	}
	s := ""
	for i := l - 1; i >= 0; i-- {
		if fn[i] == '/' {
			// remove the rightmost slash only if it is not the
			// first char (the root).
			if i == 0 {
				s = fn[0:1]
			} else {
				s = fn[:i]
			}
			fn = fn[i+1:]
			break
		}
	}

	return s, fn
}

func crname(path string, nilpatherr err_t) (err_t, bool) {
	if path == "" {
		return nilpatherr, false
	} else if path == "." || path == ".." {
		return -EINVAL, false
	}
	return 0, true
}

func fs_init() *fd_t {

	// if INT_DISK < 0 {
	// 	panic("no disk")
	// }
	// go trap_disk(uint(INT_DISK))
	// // we are now prepared to take disk interrupts
	// irq_unmask(IRQ_DISK)

	bdev_init()

	// find the first fs block; the build system installs it in block 0 for
	// us
	b := bdev_read_block(0, "fsoff")
	FSOFF := 506
	superb_start = readn(b.data[:], 4, FSOFF)
	fmt.Printf("superb_start %v %#x\n", superb_start, superb_start)
	if superb_start <= 0 {
		panic("bad superblock start")
	}
	b.bdev_refdown("fs_init")

	// superblock is never changed, so reading before recovery is fine
	b = bdev_read_block(superb_start, "super")   // don't refdown b, because superb is global
	superb = superblock_t{b.data}
	iroot = superb.rootinode()

	fmt.Printf("root inode %v\n", iroot)

	free_start = superb.freeblock()
	free_len = superb.freeblocklen()

	logstart := free_start + free_len
	loglen := superb.loglen()
	usable_start = logstart + loglen

	if loglen <= 0 || loglen > 63 {
		panic("bad log len")
	}
	// b.bdev_refdown()
	
	fslog.init(logstart, loglen)
	fs_recover()
	go log_daemon(&fslog)

	return &fd_t{fops: &fsfops_t{priv: iroot}}
}

func fs_recover() {
	l := &fslog
	b := bdev_get_fill(l.logstart, "fs_recover_logstart", false)
	lh := logheader_t{b.data}
	rlen := lh.recovernum()
	if rlen == 0 {
		fmt.Printf("no FS recovery needed\n")
		b.bdev_refdown("fs_recover0")
		return
	}
	fmt.Printf("starting FS recovery...")

	for i := 0; i < rlen; i++ {
		bdest := lh.logdest(i)
		lb := bdev_get_fill(l.logstart + 1 + i, "i", false)
		fb := bdev_get_fill(bdest, "bdest", false)
		copy(fb.data[:], lb.data[:])
		fb.bdev_write()
		lb.bdev_refdown("fs_recover1")
		fb.bdev_refdown("fs_recover2")
	}

	// clear recovery flag
	lh.w_recovernum(0)
	b.bdev_write()
	b.bdev_refdown("fs_recover3")
	fmt.Printf("restored %v blocks\n", rlen)
}

var memtime = false

func use_memfs() {
	memtime = true
	fmt.Printf("Using MEMORY FS\n")
}

// a type for an inode block/offset identifier
type inum int

func fs_link(old string, new string, cwd inum) err_t {
	op_begin("fs_link")
	defer op_end()

	if fs_debug {
		fmt.Printf("fs_link: %v %v %v\n", old, new, cwd)
	}

	orig, err := fs_namei_locked(old, cwd, "fs_link_org")
	if err != 0 {
		return err
	}
	if orig.icache.itype != I_FILE {
		orig.iunlock_refdown("fs_link")
		return -EINVAL
	}
	inum := orig.priv
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
	orig, err1 := irefcache.iref_locked(inum, "fs_link_undo")
	if err1 != 0 {
		panic("bizare")
	}
	orig._linkdown()
	orig.iunlock_refdown("fs_link_undo")
	return err
}

func fs_unlink(paths string, cwd inum, wantdir bool) err_t {
	dirs, fn := sdirname(paths)
	if fn == "." || fn == ".." {
		return -EPERM
	}

	op_begin("fs_unlink")
	defer op_end()

	if fs_debug {
		fmt.Printf("fs_unlink: %v %v %v\n", paths, cwd, wantdir)
	}

	var child *imemnode_t
	var par *imemnode_t
	var err err_t
	var childi inum

	par, err = fs_namei_locked(dirs, cwd, "fs_unlink")
	if err != 0 {
		return err
	}
	childi, err = par.ilookup(fn)
	par.iunlock("fs_unlink_par")
	if err != 0 {
		par.refdown("fs_unlink_par")
		return err
	}
	child, err = irefcache.iref(childi, "fs_unlink_child")
	if err != 0 {
		par.refdown("fs_unlink_par")
		return err
	}

	// acquire both locks
	irefcache.lockall([]*imemnode_t{par, child})
	defer par.iunlock_refdown("fs_unlink_par")
	defer child.iunlock_refdown("fs_unlink_child")
	
	// recheck if child still exists, since some other thread may have
	// modifed par but par and child won't disappear because we have
	// references to them.
	childi, err = par.ilookup(fn)
	if err != 0 {
		fmt.Printf("unlink failed child doesn't exist %v %v\n", childi, err)
		return err
	}

	err = child.do_dirchk(wantdir)
	if err != 0 {
		fmt.Printf("unlink failed child dirchk %v %v\n", childi, err)
		return err
	}

	// finally, remove directory
	err = par.do_unlink(fn)
	if err != 0 {
		fmt.Printf("unlink failed unlink %v %v\n", childi, err)
		return err
	}
	child._linkdown()

	return 0
}

// per-volume rename mutex. Linux does it so it must be OK!
var _renamelock = sync.Mutex{}

func fs_rename(oldp, newp string, cwd inum) err_t {
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

	if fs_debug {
		fmt.Printf("fs_rename: %v %v %v\n", oldp, newp, cwd)
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
	opar.iunlock("fs_rename_par")
	if err != 0 {
		opar.refdown("fs_rename_opar")
		return err
	}
	ochild, err := irefcache.iref(childi, "fs_rename_ochild")
	if err != 0 {
		opar.refdown("fs_rename_opar")
		return err
	}

	ochild.iunlocked("ochild1")
	
	npar, err := fs_namei(ndirs, cwd)
	if err != 0 {
		opar.refdown("fs_rename_opar")
		ochild.refdown("fs_rename_ochild")
		return err
	}
	
	// verify that ochild is not an ancestor of npar, since we would
	// disconnect ochild subtree from root.  it is safe to do without
	// holding locks because unlink cannot modify the path to the root by
	// removing a directory because the directories aren't empty.  it could
	// delete npar and an ancestor, but rename has already a reference to to
	// npar.
	if err = _isancestor(ochild, npar); err != 0 {
		opar.refdown("fs_rename_opar")
		ochild.refdown("fs_rename_ochild")
		npar.refdown("fs_rename_npar")
		return err
	}

	ochild.iunlocked("ochild1")

	var nchild *imemnode_t
	cnt := 0
	newexists := false
	// lookup newchild and try to lock all inodes involved
	for {
		nchild, err = fs_namei(newp, cwd)
		if err != 0 && err != -ENOENT {
			opar.refdown("fs_name_opar")
			ochild.refdown("fs_name_ochild")	
			npar.refdown("fs_name_npar")
			return err
		}
		
		ochild.iunlocked("ochild1")
		npar.iunlocked("npar")
		opar.iunlocked("opar")

		var locked []*imemnode_t
		if err == 0 {
			newexists = true
			locked = irefcache.lockall([]*imemnode_t{opar, ochild, npar, nchild})
		} else {
			locked = irefcache.lockall([]*imemnode_t{opar, ochild, npar})
		}
		for _, v := range locked {
			defer v.iunlock_refdown("rename_opar")
		}
		
		// check if the tree is still the same. an unlink or link may
		// have modified the tree.
		childi, err := opar.ilookup(ofn) 
		if err != 0 {
			return err
		}
		if childi != ochild.priv {
			panic("fs_rename\n")
		}
		
		_, err = npar.ilookup(nfn)
		// it existed before and still exists
		if newexists && err == 0 { 
			break
		}
		// it didn't exist before and still doesn't exist
		if !newexists && err == -ENOENT {
			break
		}
		// it existed but now it doesn't. but, we now have
		// parent locked and we don't need nchild lock.
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
	}

	// if src and dst are the same file, we are done
	if newexists && ochild.priv == nchild.priv { 
		return 0
	}

	// guarantee that any page allocations will succeed before starting the
	// operation, which will be messy to piece-wise undo.
	if err := npar.probe_insert(); err != 0 {
		return err
	}

	if err = opar.probe_unlink(ofn); err != 0 {
		return err
	}

	odir := ochild.icache.itype == I_DIR
	if odir {
		if err = ochild.probe_unlink(".."); err != 0 {
			return err
		}
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
	if npar.do_insert(nfn, ochild.priv) != 0 {
		panic("probed")
	}

	// update '..'
	if odir {
		if ochild.do_unlink("..") != 0 {
			panic("probed")
		}
		if ochild.do_insert("..", npar.priv) != 0 {
			panic("insert after unlink must succeed")
		}
	}
	return 0
}

// end and start are locked
func _isancestor(anc, start *imemnode_t) err_t {
	if anc.priv == iroot {
		panic("root is always ancestor")
	}
	// walk up to iroot
	here, err := irefcache.iref(start.priv, "_isancestor")
	if err != 0 {
		panic("_isancestor: start must exist")
	}
	for here.priv != iroot {
		if anc == here {
			here.refdown("_isancestor_here")
			return -EINVAL
		}
		here.ilock("_isancestor")
		nexti, err := here.ilookup("..")
		if err != 0 {
			panic(".. must exist")
		}
		var next *imemnode_t
		next, err = irefcache.iref(nexti, "_isancestor_next")
		here.iunlock("_isancestor")
		if err != 0 {
			here.refdown("_isancestor_here")
			return err
		}
		here = next
	}
	here.refdown("_isancestor")
	return 0
}

type fsfops_t struct {
	priv	inum // *imemnode_t
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
	idm, err := irefcache.iref_locked(fo.priv, "_read")
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

	op_begin("_write")
	defer op_end()

	if fs_debug {
		fmt.Printf("_write: %v %v\n", fo.priv, toff)
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
	idm, err := irefcache.iref_locked(fo.priv, "_write")
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

	idm, err := irefcache.iref_locked(fo.priv, "truncate")
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
	idm, err := irefcache.iref_locked(fo.priv, "fstat")
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

func (fo *fsfops_t) pathi() inum {
	return fo.priv
}

func (fo *fsfops_t) reopen() err_t {
	idm, err := irefcache.iref_locked(fo.priv, "reopen")
	if err != 0 {
		return err
	}
	idm.opencount++   // close will decrease it
	idm.iunlock_refdown("reopen")
	return 0
}

func (fo *fsfops_t) lseek(off, whence int) (int, err_t) {
	// prevent races on fo.offset
	fo.Lock()
	defer fo.Unlock()

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
func (fo *fsfops_t) mmapi(offset, len int) ([]mmapinfo_t, err_t) {
	idm, err := irefcache.iref_locked(fo.priv, "mmapi")
	if err != 0 {
		return nil, err
	}
	mmi, err := idm.do_mmapi(offset, len)
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
	if df.maj != D_CONSOLE && df.maj != D_DEVNULL {
		panic("bad dev")
	}
}

func (df *devfops_t) read(p *proc_t, dst userio_i) (int, err_t) {
	df._sane()
	if df.maj == D_CONSOLE {
		return cons_read(dst, 0)
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

func (df *devfops_t) mmapi(int, int) ([]mmapinfo_t, err_t) {
	df._sane()
	return nil, -ENODEV
}

func (df *devfops_t) pathi() inum {
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
		b := bdev_read_block(blkno, "read")
		boff := raw.offset % BSIZE
		c, err := dst.uiowrite(b.data[boff:])
		if err != 0 {
			return 0, err
		}
		raw.offset += c
		did += c
		b.bdev_refdown("read")
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
		buf := bdev_read_block(blkno, "write")
		c, err := src.uioread(buf.data[boff:])
		if err != 0 {
			return 0, err
		}
		buf.bdev_write()
		raw.offset += c
		did += c
		buf.bdev_refdown("write")
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

func (raw *rawdfops_t) mmapi(int, int) ([]mmapinfo_t, err_t) {
	return nil, -ENODEV
}

func (raw *rawdfops_t) pathi() inum {
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

func fs_mkdir(paths string, mode int, cwd inum) err_t {
	op_begin("fs_mkdir")
	defer op_end()

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

	var childi inum
	childi, err = par.do_createdir(fn)
	if err != 0 {
		return err
	}

	child, err := irefcache.iref(childi, "fs_mkdir_child")
	if err != 0 {
		par.create_undo(childi, fn)
		return err
	}

	child.do_insert(".", childi)
	child.do_insert("..", par.priv)
	child.refdown("fs_mkdir3")
	return 0
}

// a type to represent on-disk files
type fsfile_t struct {
	priv	inum
	major	int
	minor	int
}

func _fs_open(paths string, flags fdopt_t, mode int, cwd inum,  major, minor int) (fsfile_t, err_t) {
	trunc := flags & O_TRUNC != 0
	creat := flags & O_CREAT != 0
	nodir := false

	if fs_debug {
		fmt.Printf("fs_open: %v %v\n", paths, cwd)
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

			var childi inum
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
			idm, err = irefcache.iref_locked(childi, "_fs_open_child")
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

	idm.opencount++

	ret.priv = idm.priv
	ret.major = idm.icache.major
	ret.minor = idm.icache.minor
	return ret, 0
}

// socket files cannot be open(2)'ed (must use connect(2)/sendto(2) etc.)
var _denyopen = map[int]bool{ D_SUD: true, D_SUS: true}

func fs_open(paths string, flags fdopt_t, mode int, cwd inum,  major, minor int) (*fd_t, err_t) {
	fsf, err := _fs_open(paths, flags, mode, cwd, major, minor)
	if err != 0 {
		return nil, err
	}

	// some special files (sockets) cannot be opened with fops this way
	if denied := _denyopen[fsf.major]; denied {
		if fs_close(fsf.priv) != 0 {
			panic("must succeed")
		}
		return nil, -EPERM
	}

	// convert on-disk file to fd with fops
	priv := fsf.priv
	maj := fsf.major
	min := fsf.minor
	ret := &fd_t{}
	if maj != 0 {
		// don't need underlying file open
		if fs_close(fsf.priv) != 0 {
			panic("must succeed")
		}
		switch maj {
		case D_CONSOLE, D_DEVNULL:
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

func fs_close(priv inum) err_t {
	defer op_end()
	op_begin("fs_close")

	if fs_debug {
		fmt.Printf("fs_close: %v\n", priv)
	}

	idm, err := irefcache.iref_locked(priv, "fs_close")
	if err != 0 {
		return err
	}
	idm.opencount--
	idm.iunlock_refdown("fs_close")
	return 0
}

func fs_stat(path string, st *stat_t, cwd inum) err_t {
	idm, err := fs_namei_locked(path, cwd, "fs_stat")
	err = idm.do_stat(st)
	idm.iunlock_refdown("fs_stat")
	return err
}

func fs_sync() err_t {
	print_live_blocks()
	if memtime {
		return 0
	}
	// ensure any fs ops in the journal preceding this sync call are
	// flushed to disk by waiting for log commit.
	fslog.force <- true
	<- fslog.commitwait
	return 0
}

// if the path resolves successfully, returns the idaemon locked. otherwise,
// locks no idaemon.
func fs_namei(paths string, cwd inum) (*imemnode_t, err_t) {
	var start *imemnode_t
	var err err_t
	if len(paths) == 0 || paths[0] != '/' {
		start, err = irefcache.iref(cwd, "fs_namei_cwd")
		if err != 0 {
			panic("cannot load cwd")
		}
	} else {
		start, err = irefcache.iref(iroot, "fs_namei_root")
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
		if n != idm.priv {
			next, err := irefcache.iref(n, "fs_namei_next")
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

func fs_namei_locked(paths string, cwd inum, s string) (*imemnode_t, err_t) {
	idm, err := fs_namei(paths, cwd)
	if err != 0 {
		return nil, err
	}
	idm.ilock(s + "/fs_namei_locked")
	return idm, 0
}


type pgn_t uint

func off2pgn(off int) pgn_t {
	return pgn_t(off) >> PGSHIFT
}

// XXX don't need to fill the destination page if the write covers the whole
// page
type pgcache_t struct {
	pgs	frbh_t
	// the unit size of underlying device
	blocksz		int
	pglim		int
	idm             *imemnode_t
}

type pginfo_t struct {
	pgn		pgn_t
	pg		*bytepg_t
	dirtyblocks	[]bool
	buf             *bdev_block_t
}

func (pc *pgcache_t) pgc_init(bsz int, idm *imemnode_t) {
	if idm == nil {
		panic("invalid idm")
	}
	if PGSIZE % bsz != 0 {
		panic("block size does not divide pgsize")
	}
	pc.blocksz = bsz
	pc.pglim = 0
	pc.idm = idm
}

// takes an offset, returns the page number and starting block
func (pc *pgcache_t) _pgblock(offset int) (pgn_t, int) {
	pgn := off2pgn(offset)
	block := (offset % PGSIZE) / pc.blocksz
	return pgn, block
}

// mark the range [offset, end) as dirty
func (pc *pgcache_t) pgdirty(offset, end int) {
	for i := offset; i < end; i += pc.blocksz {
		pgn, chn := pc._pgblock(i)
		pgi, ok := pc.pgs.lookup(pgn)
		if !ok {
			panic("dirtying absent page?")
		}
		pgi.dirtyblocks[chn] = true
	}
}

// returns whether the page for pgn needs to be filled from disk and success.
func (pc *pgcache_t) _ensureslot(pgn pgn_t, fill bool) bool {
	if pc.pgs.nodes >= pc.pglim {
		if !syslimit.mfspgs.take() {
			return false
		}
		pc.pglim++
	}
	if _, ok := pc.pgs.lookup(pgn); !ok {
		offset := (int(pgn) << PGSHIFT)
		blkno, err := pc.idm.offsetblk(offset, true)
		if fs_debug {
			fmt.Printf("_ensureslot %v %v\n", pgn, blkno)
		}
		if err != 0 {
			return false
		}
		var b *bdev_block_t
		if fill {
			b = bdev_get_fill(blkno, "_ensureslot1", false)
		} else {
			b = bdev_get_zero(blkno, "_ensureslot2", false)
		}
		pgi := &pginfo_t{pgn: pgn, pg: b.data, buf: b}
		pgi.dirtyblocks = make([]bool, PGSIZE / pc.blocksz)
		pc.pgs.insert(pgi)
	}
	return true
}

func (pc *pgcache_t) _ensurefill(pgn pgn_t,fill bool) err_t {
	ok := pc._ensureslot(pgn, fill)
	if !ok {
		return -ENOMEM
	}
	return 0
}

// returns the raw page and physical address of requested page. fills the page
// if necessary.
func (pc *pgcache_t) pgraw(offset int, fill bool) (*pg_t, pa_t, err_t) {
	pgn, _ := pc._pgblock(offset)
	err := pc._ensurefill(pgn, fill)
	if err != 0 {
		return nil, 0, err
	}
	pgi, ok := pc.pgs.lookup(pgn)
	if !ok {
		panic("eh")
	}
	wpg := (*pg_t)(unsafe.Pointer(pgi.pg))
	return wpg, pgi.buf.pa, 0
}

// offset <= end. if offset lies on the same page as end, the returned slice is
// trimmed (bytes >= end are removed).
func (pc *pgcache_t) pgfor(offset, end int, fill bool) ([]uint8, err_t) {
	if offset > end {
		panic("offset must be less than end")
	}
	pgva, _, err := pc.pgraw(offset, fill)
	if err != 0 {
		return nil, err
	}
	pg := pg2bytes(pgva)[:]

	pgoff := offset % PGSIZE
	pg = pg[pgoff:]
	if offset + len(pg) > end {
		left := end - offset
		pg = pg[:left]
	}
	return pg, 0
}

// write all dirty pages back to device
func (pc *pgcache_t) flush() {
	if memtime {
		return
	}
	pc.pgs.iter(func(pgi *pginfo_t) {
		for j, dirty := range pgi.dirtyblocks {
			if !dirty {
				continue
			}
			pgi.buf.log_write()
			pgi.dirtyblocks[j] = false
		}
	})
}

// decrements the refcounts of all pages in the page cache. returns the number
// of pages that the pgcache contained.
func (pc *pgcache_t) release() int {
	pgs := 0
	pc.pgs.iter(func(pgi *pginfo_t) {
		pgi.buf.bdev_refdown("release")
		pgs++
	})
	pc.pgs.clear()
	if pc.pglim != 0 {
		syslimit.mfspgs.given(uint(pc.pglim))
		pc.pglim = 0
	}
	return pgs
}

// frees all pgcache pages iff no pages are also mapped. it would be better to
// fail the release only when at least one page is mapped shared, instead.
// returns the number of pages whose ref count was decreased (and thus should
// be freed). the pages cannot be dirty because the eviction code locks each
// imemnode and imemnodes must be flushed before they are unlocked.
func (pc *pgcache_t) evict() int {
	fmt.Printf("evict\n")
	failed := false
	pc.pgs.iter(func(pgi *pginfo_t) {
		if pgi.buf.bdev_refcnt() > 2 {   // one for the cache and one for VM?
			failed = true
		}
		for _, d := range pgi.dirtyblocks {
			if d {
				panic("cannot be dirty")
			}
		}
	})
	var pgs int
	if !failed {
		fmt.Printf("evict: release\n")
		pgs = pc.release()
	}
	return pgs
}

// a mutex that allows attempting to acuire the mutex without blocking. useful
// for deadlock avoidance for creat/unlink/rename.
type trymutex_t struct {
	m	sync.Mutex
	lock	uint
	cond	*sync.Cond
}

func (tm *trymutex_t) tm_init() {
	tm.cond = sync.NewCond(&tm.m)
}

func (tm *trymutex_t) Lock() {
	tm.m.Lock()
	for {
		if tm.lock == 0 {
			tm.lock = 1
			tm.m.Unlock()
			return
		}
		tm.cond.Wait()
	}
}

func (tm *trymutex_t) Unlock() {
	tm.m.Lock()
	tm.lock = 0
	tm.m.Unlock()
	tm.cond.Signal()
}

func (tm *trymutex_t) trylock() bool {
	tm.m.Lock()
	ret := false
	if tm.lock == 0 {
		tm.lock = 1
		ret = true
	}
	tm.m.Unlock()
	return ret
}

// pglru_t's lock is a leaf lock
type pglru_t struct {
	sync.Mutex
	head	*imemnode_t
	tail	*imemnode_t
}

func (pl *pglru_t) mkhead(im *imemnode_t) {
	if memtime {
		return
	}
	pl.Lock()
	pl._mkhead(im)
	pl.Unlock()
}

func (pl *pglru_t) _mkhead(im *imemnode_t) {
	if pl.head == im {
		return
	}
	pl._remove(im)
	if pl.head != nil {
		pl.head.pgprev = im
	}
	im.pgnext = pl.head
	pl.head = im
	if pl.tail == nil {
		pl.tail = im
	}
}

func (pl *pglru_t) _remove(im *imemnode_t) {
	if pl.tail == im {
		pl.tail = im.pgprev
	}
	if pl.head == im {
		pl.head = im.pgnext
	}
	if im.pgprev != nil {
		im.pgprev.pgnext = im.pgnext
	}
	if im.pgnext != nil {
		im.pgnext.pgprev = im.pgprev
	}
	im.pgprev, im.pgnext = nil, nil
}

func (pl *pglru_t) remove(im *imemnode_t) {
	if memtime {
		return
	}
	pl.Lock()
	pl._remove(im)
	pl.Unlock()
}

var pglru pglru_t


type imemnode_t struct {
	sync.Mutex
	priv		inum
	blkno		int
	ioff		int
	// cache of file data
	pgcache		pgcache_t
	// pgnext and pgprev can only be read/modified while holding the lru
	// list lock
	pgnext		*imemnode_t
	pgprev		*imemnode_t
	// cache of inode metadata
	icache		icache_t
	//l		sync.Mutex
	//l		trymutex_t
	// XXXPANIC for sanity
	_amlocked	bool
	opencount	int
}


type iref_t struct {
	imem           *imemnode_t
	refcnt          int
}

type irefcache_t struct {
	sync.Mutex
	irefs          map[inum]*iref_t
}


func make_irefcache() *irefcache_t {
	ic := &irefcache_t{}
	ic.irefs = make(map[inum]*iref_t, 1e6)
	return ic
}

var irefcache	= make_irefcache()

func (irc *irefcache_t) iref(priv inum, s string) (*imemnode_t, err_t) {
	irc.Lock()
	b, i :=  bidecode(priv)
	if priv <= 0 {
		panic("non-positive priv")
	}
	iref, ok := irc.irefs[priv]
	if ok {
		iref.refcnt++
		if fs_debug {
			fmt.Printf("iref hit %v (%v/%v) cnt %v %s\n", priv, b, i, iref.refcnt, s)
		}
		irc.Unlock()
		return iref.imem, 0
	}
	
	for {
		if len(irc.irefs) < syslimit.vnodes {
			break
		}
		panic("too many inodes")
	}

	imem := &imemnode_t{}
	imem.opencount = 0
	imem.priv = priv

	iref = &iref_t{}
	iref.refcnt = 1
	iref.imem = imem
	irc.irefs[priv] = iref
	
	if fs_debug {
		fmt.Printf("iref miss %v (%v/%v) cnt %v %s\n", priv, b, i, iref.refcnt, s)
	}

	imem.ilock("iref")
	irc.Unlock()

	// holding inode lock while reading it from disk
	err := imem.idm_init(priv)
	if err != 0 {
		return nil, err
	}
	pglru.mkhead(imem)
	
	imem.iunlock("iref")
	return imem, 0
}

func (irc *irefcache_t) iref_locked(priv inum, s string) (*imemnode_t, err_t) {
	idm, err := irc.iref(priv, s)
	if err != 0 {
		return nil, err
	}
	idm.ilock(s)
	return idm, err
}

func (irc *irefcache_t) getrefcnt(priv inum) int {
	defer irefcache.Unlock()
	irefcache.Lock()
	ref, ok := irefcache.irefs[priv]
	if  !ok {
		panic("remove of non-existant idaemon")
	}
	return ref.refcnt
}

func (irc *irefcache_t) decrefcnt(priv inum, removable bool) {
	defer irefcache.Unlock()
	irefcache.Lock()
	ref, ok := irefcache.irefs[priv]
	if  !ok {
		panic("decrefcnt of non-existant inode")
	}
	ref.refcnt--
	if ref.refcnt == 0 && removable {
		// fmt.Printf("decrefcnt: delete inode %v %v from icache\n", priv, ref.imem)
		if ref.imem.opencount > 0 {
			panic("decrefcnt")
		}
		ref.imem.priv = 0
		delete(irefcache.irefs, priv)
	}
}

func print_locks(locks []*imemnode_t) {
	for _, l := range locks {
		fmt.Printf("%v ", l.priv)
	}
	fmt.Printf("\n")
}

func (irc *irefcache_t) lockall(imems []*imemnode_t) []*imemnode_t {
	//fmt.Printf("lockall: ")
	//print_locks(imems)

	var locked []*imemnode_t
	sort.Slice(imems, func(i, j int) bool { return imems[i].priv < imems[j].priv })
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
	
	//fmt.Printf("lockall: locked: ")
	//print_locks(locked)
	
	return locked
}

func (idm *imemnode_t) refdown(s string) {
	idm.ilock(s+"_refdown")
	refcnt := irefcache.getrefcnt(idm.priv)
	priv := idm.priv

	if fs_debug {
		fmt.Printf("refdown %v refcnt %v opencount %v %v\n", priv, refcnt, idm.opencount, s)
	}

	if refcnt < 1 {
		panic(s + " refdown\n")
	}

	removable := false
	if refcnt == 1 && idm.opencount == 0 {
		removable = true
		if idm.icache.links == 0 {
			idm.evict()
		}
	}
	idm.iunlock("refdown")
	irefcache.decrefcnt(priv, removable)
}

func (idm *imemnode_t) iunlock_refdown(s string) {
	idm.iunlock(s + "/iunlock_refdown")
	idm.refdown(s)
}


// opencount should be 0
func (idm *imemnode_t) evict() {
	fmt.Printf("evict %v\n", idm.priv)
	// XXX hold idm lock across free
	idm.ifree()
	idm._derelease()
	pglru.remove(idm)
}

func (idm *imemnode_t) ilock(s string) {
	if idm._amlocked {
		fmt.Printf("ilocked: warning %v %v\n", idm.priv, s)
	}
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
	if idm.priv <= 0 && s != "downref" {
		panic("non-positive priv")
	}
	idm._amlocked = false
	idm.Unlock()
}

func (idm *imemnode_t) iunlocked(s string) {
	idm.Lock()
	if idm._amlocked {
		panic("iunlock: is locked " + s)
	}
	idm.Unlock()
}


// biscuit FS locking protocol: rename acquires imemnode locks in any order.
// all other fs ops that acquire two or more imemnode locks avoid deadlocking
// with a concurrent rename via a non-blocking acquire that fails if the lock
// is taken (trylock). pathname lookup (namei) does not use hand-over-hand
// locking; see the comment for idaemon_ensure for how namei is correct without
// it.

// ilock_opened returns true when the inode was locked and the fd referencing
// this imemnode hasn't been closed out from under us by another thread of the
// same process. otherwise the inode is unlocked. opencount is used to detect
// when a thread raced and beat us to close(2) (why not put the
// racing-close-detection into fsfops like other fds?).
//
// in general: ilock_opened() is for fds, the rest should use ilock_namei().

func (idm *imemnode_t) ilock_opened() bool {
	panic("ilock_opened")
	if idm._amlocked {
		fmt.Printf("ilocked_opened  warning %v\n", idm.priv)
	}
	//idm.l.Lock()
	fmt.Printf("ilock_opened %v\n", idm.priv)
	if idm.opencount == 0 && idm.icache.links == 0 {
		// idm.l.Unlock()
		return false
	}
	idm._amlocked = true
	fmt.Printf("ilock_opened %v done\n", idm.priv)
	return true
}

// lock a node for path resolution. a racing thread that unlinks the file
// causes our open to fail.
func (idm *imemnode_t) ilock_namei() bool {
	panic("ilock_namei")
	// idm.l.Lock()
	if idm.icache.links == 0 {
		// idm.l.Unlock()
		return false
	}
	idm._amlocked = true
	return true
}

func (idm *imemnode_t) itrylock() bool {
	//if !idm.l.trylock() {
	//	return false
	//}
	if idm.icache.links == 0 {
		// idm.l.Unlock()
		return false
	}
	idm._amlocked = true
	return true
}



// creates a new idaemon struct and fills its inode
func (idm *imemnode_t) idm_init(priv inum) err_t {
	blkno, ioff := bidecode(priv)
	idm.priv = priv
	idm.blkno = blkno
	idm.ioff = ioff

	idm.pgcache.pgc_init(BSIZE, idm)

	if fs_debug {
		fmt.Printf("idm_init: readinode block %v(%v,%v)\n", priv, blkno, ioff)
	}
	blk := idm.idibread()
	idm.icache.fill(blk, ioff)

	blk.bdev_refdown("idm_init")
	blk.Unlock()
	return 0
}


func (idm *imemnode_t) _iupdate() {
	iblk := idm.idibread()
	iblk.Unlock()
	if idm.icache.flushto(iblk, idm.ioff) {
		iblk.log_write()
	}
	iblk.bdev_refdown("_iupdate")
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

func (idm *imemnode_t) do_mmapi(off, len int) ([]mmapinfo_t, err_t) {
	if idm.icache.itype != I_FILE && idm.icache.itype != I_DIR {
		panic("bad mmapinfo")
	}
	return idm.immapinfo(off, len)
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
	c, err := irefcache.iref_locked(priv, "_fullpath")
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
		par, err := irefcache.iref(pari, "do_fullpath_par")
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

type fdent_t struct {
	offset	int
	next	*fdent_t
}

// linked list of free directory entries
type fdelist_t struct {
	head	*fdent_t
	n	int
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
	offset	int
	priv	inum
}

// metadata block interface; only one idaemon touches these blocks at a time,
// thus no concurrency control

func (ic *icache_t) _mbensure(blockn int, fill bool) *bdev_block_t {
	var mb *bdev_block_t
	if fill {
		mb = bdev_get_fill(blockn, "_mbensure", false)
	} else {
		mb = bdev_get_zero(blockn, "_mbensure", false)
	}
	return mb
}

func (ic *icache_t) mbread(blockn int) *bdev_block_t {
	return ic._mbensure(blockn, true)
}

// ensure the block is in the bdev cache and zero-ed
// XXX ifree writes it out, but evict should too, if we had evict.
func (ic *icache_t) mbempty(blockn int) {
	if ok := bdev_cache_present(blockn); ok {
		fmt.Printf("mbemtpy: blockno %v\n", blockn)
		panic("block present")
	}
	b := ic._mbensure(blockn, false)
	b.bdev_refdown("mbempty")
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
	ensureb := func(blkno int) (int, bool) {
		if !writing || blkno != 0 {
			return blkno, false
		}
		nblkno := balloc()
		return nblkno, true
	}
	// this function makes sure that every slot up to and including idx of
	// the given indirect block has a block allocated. it is necessary to
	// allocate a bunch of zero'd blocks for a file when someone lseek()s
	// past the end of the file and then writes.
	ensureind := func(blk *bdev_block_t, idx int) {
		if !writing {
			return
		}
		added := false
		for i := 0; i <= idx; i++ {
			slot := i * 8
			s := blk.data[:]
			blkn := readn(s, 8, slot)
			blkn, isnew := ensureb(blkn)
			if isnew {
				writen(s, 8, slot, blkn)
				added = true
				// make sure to zero indirect pointer block if
				// allocated
				if idx == INDADDR {
					fmt.Printf("indirect indx\n")
					// idm.icache.mbempty(blkn)
				}
			}
		}
		if added {
			blk.log_write()
		}
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
			tblkn := balloc()
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
		indno, isnew := ensureb(indno)
		if isnew {
			// idm.icache.mbempty(indno)
			// new indirect block will be written to log below
			idm.icache.indir = indno
		}
		// follow indirect block chain
		indblk := idm.icache.mbread(indno)
		for i := 0; i < indslot/slotpb; i++ {
			// make sure the indirect block has no empty spaces if
			// we are writing past the end of the file. if
			// necessary, ensureind also allocates the next
			// indirect pointer block.
			ensureind(indblk, slotpb)

			indno = readn(indblk.data[:], 8, nextindb)
			indblk.bdev_refdown("offsetblk1")
			indblk = idm.icache.mbread(indno)
		}
		// finally get data block from indirect block
		slotoff := (indslot % slotpb)
		ensureind(indblk, slotoff)
		noff := (slotoff)*8
		s := indblk.data[:]
		blkn = readn(s, 8, noff)
		blkn, isnew = ensureb(blkn)
		if isnew {
			writen(s, 8, noff, blkn)
			indblk.log_write()
		}
		indblk.bdev_refdown("offsetblk2")
	} else {
		blkn = idm.icache.addrs[whichblk]
	}
	if blkn <= 0 || blkn >= superb.lastblock() {
		panic("bad data blocks")
	}
	return blkn, 0
}

func (idm *imemnode_t) iread(dst userio_i, offset int) (int, err_t) {
	pglru.mkhead(idm)
	isz := idm.icache.size
	c := 0
	for offset + c < isz && dst.remain() != 0 {
		src, err := idm.pgcache.pgfor(offset + c, isz, true)
		if err != 0 {
			return c, err
		}
		wrote, err := dst.uiowrite(src)
		c += wrote
		if err != 0 {
			return c, err
		}
	}
	return c, 0
}

func (idm *imemnode_t) iwrite(src userio_i, offset int) (int, err_t) {
	pglru.mkhead(idm)
	sz := src.totalsz()
	newsz := offset + sz
	err := idm._preventhole(idm.icache.size, uint(newsz))
	if err != 0 {
		return 0, err
	}
	c := 0
	for c < sz {
		noff := offset + c
		dst, err := idm.pgcache.pgfor(noff, newsz, false)
		if err != 0 {
			return c, err
		}
		read, err := src.uioread(dst)
		if err != 0 {
			return c, err
		}
		idm.pgcache.pgdirty(noff, noff + read)
		c += read
	}
	wrote := c
	if newsz > idm.icache.size {
		idm.icache.size = newsz
	}
	idm.pgcache.flush()
	return wrote, 0
}

// if a progam 1) lseeks past end of file and writes or 2) extends file size
// via ftruncate/truncate, make sure the page cache is filled with zeros for
// the new bytes.
func (idm *imemnode_t) _preventhole(_oldlen int, newlen uint) err_t {
	// XXX fix sign
	oldlen := uint(_oldlen)
	ret := err_t(0)
	if newlen > oldlen {
		// extending file; fill new content in page cache with zeros
		first := true
		c := oldlen
		for c < newlen {
			pg, err := idm.pgcache.pgfor(int(c), int(newlen), false)
			if err != 0 {
				ret = -ENOMEM
				break
			}
			if first {
				// XXX go doesn't seem to have a good way to
				// zero a slice
				for i := range pg {
					pg[i] = 0
				}
				first = false
			}
			c += uint(len(pg))
		}
		idm.pgcache.pgdirty(int(oldlen), int(c))
	}
	return ret
}

func (idm *imemnode_t) itrunc(newlen uint) err_t {
	err := idm._preventhole(idm.icache.size, newlen)
	if err != 0 {
		return err
	}
	// it's important that the icache.size is updated after filling the
	// page cache since fs_fill reads from disk any pages whose offset is
	// <= icache.size
	idm.icache.size = int(newlen)
	idm.pgcache.flush()
	return 0
}


// returns the offset of an empty directory entry. returns error if failed to
// allocate page for the new directory entry.
func (idm *imemnode_t) _denextempty() (int, err_t) {
	dc := &idm.icache.dentc
	if ret, ok := dc.freel.remhead(); ok {
		return ret.offset, 0
	}

	// see if we have an empty slot before expanding the directory
	if !idm.icache.dentc.haveall {
		var de icdent_t
		found, err := idm._descan(func(fn string, tde icdent_t) bool {
			if fn == "" {
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

	// current dir blocks are full -- allocate new dirdata block. make
	// sure its in the page cache but not fill'ed from disk
	newsz := idm.icache.size + BSIZE
	_, err := idm.pgcache.pgfor(idm.icache.size, newsz, false)
	if err != 0 {
		return 0, err
	}
	idm.pgcache.pgdirty(idm.icache.size, newsz)
	newoff := idm.icache.size
	// start from 1 since we return slot 0 directly
	for i := 1; i < NDIRENTS; i++ {
		noff := newoff + NDBYTES*i
		idm._deaddempty(noff)
	}
	idm.icache.size = newsz
	return newoff, 0
}

// if _deinsert fails to allocate a page, idm is left unchanged.
func (idm *imemnode_t) _deinsert(name string, nblkno int, ioff int) err_t {
	// XXXPANIC
	//if _, err := idm._delookup(name); err == 0 {
	//	panic("dirent already exists")
	//}
	var ddata dirdata_t
	noff, err := idm._denextempty()
	if err != 0 {
		return err
	}
        // dennextempty() made the slot so we won't fill
	pg, err := idm.pgcache.pgfor(noff, noff+NDBYTES, true) 
	if err != 0 {
		return err
	}
	ddata.data = pg

	// write dir entry
	//if ddata.filename(0) != "" {
	//	panic("dir entry slot is not free")
	//}
	ddata.w_filename(0, name)
	ddata.w_inodenext(0, nblkno, ioff)
	idm.pgcache.pgdirty(noff, noff+NDBYTES)
	idm.pgcache.flush()
	icd := icdent_t{noff, inum(biencode(nblkno, ioff))}
	ok := idm._dceadd(name, icd)
	dc := &idm.icache.dentc
	dc.haveall = dc.haveall && ok
	return 0
}

// calls f on each directory entry (including empty ones) until f returns true
// or it has been called on all directory entries. _descan returns true if f
// returned true.
func (idm *imemnode_t) _descan(f func(fn string, de icdent_t) bool) (bool, err_t) {
	for i := 0; i < idm.icache.size; i+= BSIZE {
		pg, err := idm.pgcache.pgfor(i, i+BSIZE, true)
		if err != 0 {
			return false, err
		}
		// XXXPANIC
		if len(pg) != BSIZE {
			panic("wut")
		}
		dd := dirdata_t{pg}
		for j := 0; j < NDIRENTS; j++ {
			tfn := dd.filename(j)
			tpriv := dd.inodenext(j)
			tde := icdent_t{i+j*NDBYTES, tpriv}
			if f(tfn, tde) {
				return true, 0
			}
		}
	}
	return false, 0
}

func (idm *imemnode_t) _delookup(fn string) (icdent_t, err_t) {
	if fn == "" {
		panic("bad lookup")
	}
	if de, ok := idm.icache.dentc.dents.lookup(fn); ok {
		return de, 0
	}
	var zi icdent_t
	if idm.icache.dentc.haveall {
		// cache negative entries?
		return zi, -ENOENT
	}

	// not in cached dirents
	found := false
	haveall := true
	var de icdent_t
	_, err := idm._descan(func(tfn string, tde icdent_t) bool {
		if tfn == "" {
			return false
		}
		if tfn == fn {
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
	idm.icache.dentc.haveall = haveall
	if !found {
		return zi, -ENOENT
	}
	return de, 0
}

func (idm *imemnode_t) _deremove(fn string) (icdent_t, err_t) {
	var zi icdent_t
	de, err := idm._delookup(fn)
	if err != 0 {
		return zi, err
	}
	pg, err := idm.pgcache.pgfor(de.offset, de.offset + NDBYTES, true)
	if err != 0 {
		return zi, err
	}
	dirdata := dirdata_t{pg}
	dirdata.w_filename(0, "")
	dirdata.w_inodenext(0, 0, 0)
	idm.pgcache.pgdirty(de.offset, de.offset + NDBYTES)
	idm.pgcache.flush()
	// add back to free dents
	idm.icache.dentc.dents.remove(fn)
	idm._deaddempty(de.offset)
	return de, 0
}

// returns the filename mapping to tnum
func (idm *imemnode_t) _denamefor(tnum inum) (string, err_t) {
	// check cache first
	var fn string
	found := idm.icache.dentc.dents.iter(func(dn string, de icdent_t) bool {
		if de.priv == tnum {
			fn = dn
			return true
		}
		return false
	})
	if found {
		return fn, 0
	}

	// not in cache; shucks!
	var de icdent_t
	found, err := idm._descan(func(tfn string, tde icdent_t) bool {
		if tde.priv == tnum {
			fn = tfn
			de = tde
			return true
		}
		return false
	})
	if err != 0 {
		return "", err
	}
	if !found {
		return "", -ENOENT
	}
	idm._dceadd(fn, de)
	return fn, 0
}

// returns true if idm, a directory, is empty (excluding ".." and ".").
func (idm *imemnode_t) _deempty() (bool, err_t) {
	fmt.Printf("_deempty %v: %v\n", idm.priv, idm.icache.dentc.haveall)
	
	if idm.icache.dentc.haveall {
		dentc := &idm.icache.dentc
		hasfiles := dentc.dents.iter(func(dn string, de icdent_t) bool {
			if dn != "." && dn != ".." {
				return true
			}
			return false
		})
		return !hasfiles, 0
	}
	notempty, err := idm._descan(func(fn string, de icdent_t) bool {
		fmt.Printf("eempty: %v %v\n", fn, de)
		return fn != "" && fn != "." && fn != ".."
	})
	fmt.Printf("noteempty: %v\n", notempty)
	if err != 0 {
		return false, err
	}
	return !notempty, 0
}

// empties the dirent cache, returning the number of dents purged.
func (idm *imemnode_t) _derelease() int {
	dc := &idm.icache.dentc
	dc.haveall = false
	dc.dents.clear()
	dc.freel.head = nil
	ret := dc.max
	syslimit.dirents.given(uint(ret))
	dc.max = 0
	return ret
}

// ensure that an insert/unlink cannot fail i.e. fail to allocate a page. if fn
// == "", look for an empty dirent, otherwise make sure the page containing fn
// is in the page cache.
func (idm *imemnode_t) _deprobe(fn string) err_t {
	if fn != "" {
		de, err := idm._delookup(fn)
		if err != 0 {
			return err
		}
		noff := de.offset
		_, err = idm.pgcache.pgfor(noff, noff+NDBYTES, true)
		return err
	}
	noff, err := idm._denextempty()
	if err != 0 {
		return err
	}
	// dennextempty() made the slot so we won't fill
	_, err = idm.pgcache.pgfor(noff, noff+NDBYTES, true)
	if err != 0 {
		return err
	}
	idm._deaddempty(noff)
	return 0
}

// returns true if this idm has enough free cache space for a single dentry
func (idm *imemnode_t) _demayadd() bool {
	dc := &idm.icache.dentc
	//have := len(dc.dents) + len(dc.freem)
	have := dc.dents.nodes + dc.freel.count()
	if have + 1 < dc.max {
		return true
	}
	// reserve more directory entries
	take := 64
	if syslimit.dirents.taken(uint(take)) {
		dc.max += take
		return true
	}
	lhits++
	return false
}

// caching is best-effort. returns true if fn was added to the cache
func (idm *imemnode_t) _dceadd(fn string, de icdent_t) bool {
	dc := &idm.icache.dentc
	if !idm._demayadd() {
		return false
	}
	dc.dents.insert(fn, de)
	return true
}

func (idm *imemnode_t) _deaddempty(off int) {
	if !idm._demayadd() {
		return
	}
	dc := &idm.icache.dentc
	dc.freel.addhead(off)
}

// guarantee that there is enough memory to insert at least one directory
// entry.
func (idm *imemnode_t) probe_insert() err_t {
	// insert and remove a fake directory entry, forcing a page allocation
	// if necessary.
	if err := idm._deprobe(""); err != 0 {
		return err
	}
	return 0
}

// guarantee that there is enough memory to unlink a dirent (which may require
// allocating a page to load the dirents from disk).
func (idm *imemnode_t) probe_unlink(fn string) err_t {
	if err := idm._deprobe(fn); err != 0 {
		return err
	}
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
func (idm *imemnode_t) create_undo(childi inum, childn string) {
	ci, err := idm.iunlink(childn)
	if err != 0 {
		panic("but insert just succeeded")
	}
	if ci != childi {
		panic("inconsistent")
	}
	bn, ioff := bidecode(childi)
	ib := bdev_get_fill(bn, "create_undo", true)
	ni := &inode_t{ib, ioff}
	_iallocundo(bn, ni, ib)
	ib.Unlock()
	ib.bdev_refdown("create_undo")
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

	// allocate new inode
	newbn, newioff := ialloc()
	newiblk := bdev_get_zero(newbn, "icreate", true)

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
	newiblk.bdev_refdown("icreate")

	newinum := inum(biencode(newbn, newioff))

	if newinum <= 0 {
		panic("icreate")
	}
	return newinum, err
}

func (idm *imemnode_t) ilookup(name string) (inum, err_t) {
	pglru.mkhead(idm)
	// did someone confuse a file with a directory?
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}
	de, err := idm._delookup(name)
	if err != 0 {
		return 0, err
	}
	return de.priv, 0
}

// creates a new directory entry with name "name" and inode number priv
func (idm *imemnode_t) iinsert(name string, priv inum) err_t {
	if idm.icache.itype != I_DIR {
		return -ENOTDIR
	}
	if _, err := idm._delookup(name); err == 0 {
		return -EEXIST
	} else if err != -ENOENT {
		return err
	}
	if priv <= 0 {
		fmt.Printf("insert: non-positive inum %v %v\n", name, priv)
		panic("iinsert")
	}
	a, b := bidecode(priv)
	err := idm._deinsert(name, a, b)
	return err
}

// returns inode number of unliked inode so caller can decrement its ref count
func (idm *imemnode_t) iunlink(name string) (inum, err_t) {
	if idm.icache.itype != I_DIR {
		panic("unlink to non-dir")
	}
	de, err := idm._deremove(name)
	if err != 0 {
		return 0, err
	}
	return de.priv, 0
}

// returns true if the inode has no directory entries
func (idm *imemnode_t) idirempty() (bool, err_t) {
	if idm.icache.itype != I_DIR {
		panic("i am not a dir")
	}
	return idm._deempty()
}

func (idm *imemnode_t) immapinfo(offset, len int) ([]mmapinfo_t, err_t) {
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
	len = roundup(offset + len, PGSIZE) - rounddown(offset, PGSIZE)
	pgc := len / PGSIZE
	ret := make([]mmapinfo_t, pgc)
	for i := 0; i < len; i += PGSIZE {
		pg, phys, err := idm.pgcache.pgraw(offset + i, true)
		if err != 0 {
			return nil, err
		}
		pgn := i / PGSIZE
		ret[pgn].pg = pg
		ret[pgn].phys = phys
	}
	return ret, 0
}

func (idm *imemnode_t) idibread() *bdev_block_t {
	return bdev_get_fill(idm.blkno, "idibread", true)
}

// frees all blocks occupied by idm
func (idm *imemnode_t) ifree() {
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
		blk := idm.icache.mbread(indno)
		blks := blk.data[:]
		for i := 0; i < INDADDR; i++ {
			off := i*8
			nblkno := readn(blks, 8, off)
			add(nblkno)
		}
		nextoff := INDADDR*8
		indno = readn(blks, 8, nextoff)
		blk.bdev_refdown("ifree1")
	}

	iblk := idm.idibread()
	if iblk == nil {
		panic("ifree")
	}
	idm.icache.itype = I_DEAD
	idm.icache.flushto(iblk, idm.ioff)
	if _alldead(iblk) {
		add(idm.blkno)
		iblk.Unlock()
	} else {
		iblk.Unlock()
		iblk.log_write()
	}
	iblk.bdev_refdown("ifree2")

	// could batch free
	for _, blkno := range allb {
		bfree(blkno)
	}

	idm.pgcache.release()
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

// superblock format:
// bytes, meaning
// 0-7,   freeblock start
// 8-15,  freeblock length
// 16-23, number of log blocks
// 24-31, root inode
// 32-39, last block
// 40-47, inode block that may have room for an inode
// 48-55, recovery log length; if non-zero, recovery procedure should run
type superblock_t struct {
	data	*bytepg_t
}

func (sb *superblock_t) freeblock() int {
	return fieldr(sb.data, 0)
}

func (sb *superblock_t) freeblocklen() int {
	return fieldr(sb.data, 1)
}

func (sb *superblock_t) loglen() int {
	return fieldr(sb.data, 2)
}

func (sb *superblock_t) rootinode() inum {
	v := fieldr(sb.data, 3)
	return inum(v)
}

func (sb *superblock_t) lastblock() int {
	return fieldr(sb.data, 4)
}

func (sb *superblock_t) freeinode() int {
	return fieldr(sb.data, 5)
}

func (sb *superblock_t) w_freeblock(n int) {
	fieldw(sb.data, 0, n)
}

func (sb *superblock_t) w_freeblocklen(n int) {
	fieldw(sb.data, 1, n)
}

func (sb *superblock_t) w_loglen(n int) {
	fieldw(sb.data, 2, n)
}

func (sb *superblock_t) w_rootinode(blk int, iidx int) {
	fieldw(sb.data, 3, biencode(blk, iidx))
}

func (sb *superblock_t) w_lastblock(n int) {
	fieldw(sb.data, 4, n)
}

func (sb *superblock_t) w_freeinode(n int) {
	fieldw(sb.data, 5, n)
}

// first log header block format
// bytes, meaning
// 0-7,   valid log blocks
// 8-511, log destination (63)
type logheader_t struct {
	data	*bytepg_t
}

func (lh *logheader_t) recovernum() int {
	return fieldr(lh.data, 0)
}

func (lh *logheader_t) w_recovernum(n int) {
	fieldw(lh.data, 0, n)
}

func (lh *logheader_t) logdest(p int) int {
	if p < 0 || p > 62 {
		panic("bad dnum")
	}
	return fieldr(lh.data, 8 + p)
}

func (lh *logheader_t) w_logdest(p int, n int) {
	if p < 0 || p > 62 {
		panic("bad dnum")
	}
	fieldw(lh.data, 8 + p, n)
}

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

// directory data format
// 0-13,  file name characters
// 14-21, inode block/offset
// ...repeated, totaling 23 times
type dirdata_t struct {
	data	[]uint8
}

const(
	DNAMELEN = 14
	NDBYTES  = 22
	NDIRENTS = BSIZE/NDBYTES
)

func doffset(didx int, off int) int {
	if didx < 0 || didx >= NDIRENTS {
		panic("bad dirent index")
	}
	return NDBYTES*didx + off
}

func (dir *dirdata_t) filename(didx int) string {
	st := doffset(didx, 0)
	sl := dir.data[st : st + DNAMELEN]
	ret := make([]byte, 0, 14)
	for _, c := range sl {
		if c == 0 {
			break
		}
		ret = append(ret, c)
	}
	return string(ret)
}

func (dir *dirdata_t) inodenext(didx int) inum {
	st := doffset(didx, 14)
	v := readn(dir.data[:], 8, st)
	return inum(v)
}

func (dir *dirdata_t) w_filename(didx int, fn string) {
	st := doffset(didx, 0)
	sl := dir.data[st : st + DNAMELEN]
	l := len(fn)
	for i := range sl {
		if i >= l {
			sl[i] = 0
		} else {
			sl[i] = fn[i]
		}
	}
}

func (dir *dirdata_t) w_inodenext(didx int, blk int, iidx int) {
	st := doffset(didx, 14)
	v := biencode(blk, iidx)
	writen(dir.data[:], 8, st, v)
}

type fblkcache_t struct {
	blks		[]*bdev_block_t
	free_start	int
}

func fbread(blockno int) *bdev_block_t {
	if blockno < superb_start {
		panic("naughty blockno")
	}
	return bdev_get_fill(blockno, "fbread", true)
}


func freebit(b uint8) uint {
	for m := uint(0); m < 8; m++ {
		if (1 << m) & b == 0 {
			return m
		}
	}
	panic("no 0 bit?")
}

// save last byte we found that had free blocks
var _lastblkno int
var _lastbyte int

// allocates a block, marking it used in the free block bitmap. free blocks and
// log blocks are not accounted for in the free bitmap; all others are. balloc
// should only ever acquire fblock.
func balloc1() int {
	fst := free_start
	flen := free_len
	if fst == 0 || flen == 0 {
		panic("fs not initted")
	}

	found := false
	var bit uint
	var blk *bdev_block_t
	var blkn int
	var oct int
	// 0 is free, 1 is allocated
	for b := 0; b < flen && !found; b++ {
		i := (_lastblkno + b) % flen
		if blk != nil {
			blk.Unlock()
			blk.bdev_refdown("balloc1")
		}
		blk = fbread(fst + i)
		start := 0
		if b == 0 {
			start = _lastbyte
		}
		for idx := start; idx < len(blk.data); idx++ {
			c := blk.data[idx]
			if c != 0xff {
				_lastblkno = i
				_lastbyte = idx
				bit = freebit(c)
				blkn = i
				oct = idx
				found = true
				break
			}
		}
	}
	if !found {
		panic("no free blocks")
	}

	// mark as allocated
	blk.data[oct] |= 1 << bit
	blk.Unlock()
	blk.log_write()
	blk.bdev_refdown("balloc1")

	boffset := usable_start
	bitsperblk := BSIZE*8
	return boffset + blkn*bitsperblk + oct*8 + int(bit)
}

func balloc() int {
	fblock.Lock()
	ret := balloc1()
	fblock.Unlock()

	// zero the block in the buffer cache
	blk := bdev_get_zero(ret, "balloc", true)
	
	if fs_debug {
		fmt.Printf("balloc: %v\n", ret)
	}

	var zdata [BSIZE]uint8
	copy(blk.data[:], zdata[:])
	blk.Unlock()
	blk.bdev_refdown("balloc")
	
	return ret
}

func bfree(blkno int) {

	if fs_debug {
		fmt.Printf("bfree: %v\n", blkno)
	}
	fst := free_start
	flen := free_len
	if fst == 0 || flen == 0 {
		panic("fs not initted")
	}
	if blkno < 0 {
		panic("bad blockno")
	}

	fblock.Lock()
	defer fblock.Unlock()

	bit := blkno - usable_start
	bitsperblk := BSIZE*8
	fblkno := fst + bit/bitsperblk
	fbit := bit%bitsperblk
	fbyteoff := fbit/8
	fbitoff := uint(fbit%8)
	if fblkno >= fst + flen {
		panic("bad blockno")
	}
	fblk := fbread(fblkno)
	fblk.data[fbyteoff] &= ^(1 << fbitoff)
	fblk.Unlock()
	fblk.log_write()
	fblk.bdev_refdown("bfree")

	// force allocation of free inode block
	if ifreeblk == blkno {
		ifreeblk = 0
	}
}

var ifreeblk int	= 0
var ifreeoff int 	= 0

// returns block/index of free inode.
//
// XXX biscuit will not use free inodes if a crash happens after we allocated
// one inode but then crash.  on recovery ifreeblk will be zero and we will
// allocate a new block and not use the remaining free entries of ifreeblk
// before crash.
func ialloc() (int, int) {
	fblock.Lock()
	defer fblock.Unlock()

	if ifreeblk != 0 {
		ret := ifreeblk
		retoff := ifreeoff
		ifreeoff++
		blkwords := BSIZE/8
		if ifreeoff >= blkwords/NIWORDS {
			// allocate a new inode block next time
			ifreeblk = 0
		}
		return ret, retoff
	}

	ifreeblk = balloc1()
	ifreeoff = 0

	zblk := bdev_get_zero(ifreeblk, "ialloc", true)
	
	// block may have been int the cache and is now reused, zero it in the cache
	var zdata [BSIZE]uint8
	copy(zblk.data[:], zdata[:])
	zblk.Unlock()
	
	// zblk.log_write()
	zblk.bdev_refdown("ialloc")
	
	if zblk.bdev_refcnt() < 1 {
		fmt.Printf("ref_cnt = %v\n", zblk.bdev_refcnt())
		panic("xxxx")
	}
	reti := ifreeoff
	ifreeoff++
	return ifreeblk, reti
}

func memreclaim() bool {
	if memtime {
		// cannot evict vnodes on a memory file system!
		return false
	}
	
	fmt.Printf("memreclaim\n")
	
	// memreclaim can be called while an inode is locked. a caller may also
	// created a defer statement that tries to close the file on which an
	// operation failed to allocate. thus we panic while the file is locked
	// and the defer statement deadlocks trying to close the file.
	got := 0
	want := 10
	pglru.Lock()
	for h := pglru.tail; h != nil && got < want; h = h.pgprev {
		// the fs locking order requires that imemnode is locked before
		// pglru. thus avoid deadlock via itrylock.
		if !h.itrylock() {
			continue
		}
		got += h.pgcache.evict()
		// iunlock cannot attempt to acquire the pglru lock to remove h
		// from the list because this code does not modify the
		// opencount or linkcount and the itrylock succeeded; therefore
		// the file cannot be unlinked.
		if h.opencount == 0 && h.icache.links == 0 {
			panic("how?")
		}
		h.refdown("memreclaim")
	}
	pglru.Unlock()
	return got > 0
}
