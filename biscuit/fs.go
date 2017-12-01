package main

import "fmt"
import "runtime"
import "strings"
import "sync"
import "sync/atomic"
import "unsafe"

const NAME_MAX    int = 512

var superb_start	int
var superb		superblock_t
var iroot		*imemnode_t
var free_start		int
var free_len		int
var usable_start	int

// file system journal
var fslog	= log_t{}

// free block bitmap lock
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

	disk_test()
	bdev_init()

	// find the first fs block; the build system installs it in block 0 for
	// us
	b := bdev_read_block(0, "fsoff")   
	FSOFF := 506
	superb_start = readn(b.data[:], 4, FSOFF)
	if superb_start <= 0 {
		panic("bad superblock start")
	}
	b.bdev_refdown("fs_init")

	// superblock is never changed, so reading before recovery is fine
	b = bdev_read_block(superb_start, "super")   // don't refdown b, because superb is global
	superb = superblock_t{b.data}
	ri := superb.rootinode()

	free_start = superb.freeblock()
	free_len = superb.freeblocklen()

	logstart := free_start + free_len
	loglen := superb.loglen()
	usable_start = logstart + loglen
	if loglen < 0 || loglen > 63 {
		panic("bad log len")
	}
	// b.bdev_refdown()
	
	fslog.init(logstart, loglen)
	fs_recover()
	go log_daemon(&fslog)

	// anything that reads disk blocks needs to happen after recovery
	// fblkcache.fblkc_init(free_start, free_len)
	var err err_t
	iroot, err = idaemon_ensure(ri)
	if err != 0 {
		panic("error loading root inode")
	}

	return &fd_t{fops: &fsfops_t{priv: iroot}}
}

func fs_recover() {
	l := &fslog
	b := bdev_read_block(l.logstart, "logstart")
	lh := logheader_t{b.data}
	rlen := lh.recovernum()
	if rlen == 0 {
		fmt.Printf("no FS recovery needed\n")
		return
	}
	fmt.Printf("starting FS recovery...")

	for i := 0; i < rlen; i++ {
		bdest := lh.logdest(i)
		lb := bdev_read_block(l.logstart + 1 + i, "i")
		fb := bdev_read_block(bdest, "bdest")
		copy(fb.data[:], lb.data[:])
		bdev_write(fb)
		lb.bdev_refdown("fs_recover1")
		fb.bdev_refdown("fs_recover2")
	}

	// clear recovery flag
	lh.w_recovernum(0)
	bdev_write(b)
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

func fs_link(old string, new string, cwd *imemnode_t) err_t {
	op_begin()
	defer op_end()

	orig, err := fs_namei(old, cwd)
	if err != 0 {
		return err
	}
	if orig.icache.itype != I_FILE {
		orig.iunlock()
		return -EINVAL
	}
	orig._linkup()
	orig.iunlock()

	dirs, fn := sdirname(new)
	newd, err := fs_namei(dirs, cwd)
	if err != 0 {
		goto undo
	}
	err = newd.do_insert(fn, orig.priv)
	newd.iunlock()
	if err != 0 {
		goto undo
	}
	return 0
undo:
	if !orig.ilock_namei() {
		panic("must succeed")
	}
	if orig._linkdown() {
		idaemon_remove(orig.priv)
	}
	orig.iunlock()
	return err
}

func fs_unlink(paths string, cwd *imemnode_t, wantdir bool) err_t {
	dirs, fn := sdirname(paths)
	if fn == "." || fn == ".." {
		return -EPERM
	}

	op_begin()
	defer op_end()

	// lock parent and attempt to acquire child. if we fail, unlock parent
	// and yield to avoid deadlock.
	var par *imemnode_t
	var child *imemnode_t
	var err err_t
	var childi inum
	for {
		par, err = fs_namei(dirs, cwd)
		if err != 0 {
			return err
		}
		childi, err = par.iget(fn)
		if err != 0 {
			par.iunlock()
			return err
		}

		child, err = idaemon_ensure(childi)
		if err != 0 {
			par.iunlock()
			return err
		}
		if child.itrylock() {
			break
		}
		par.iunlock()
		runtime.Gosched()
	}
	defer par.iunlock()
	defer child.iunlock()

	err = child.do_dirchk(wantdir)
	if err != 0 {
		return err
	}

	// finally, remove directory
	err = par.do_unlink(fn)
	if err != 0 {
		return err
	}
	dorem := child._linkdown()
	if dorem {
		idaemon_remove(childi)
	}
	return 0
}

// per-volume rename mutex. Linux does it so it must be OK!
var _renamelock = sync.Mutex{}

func fs_rename(oldp, newp string, cwd *imemnode_t) err_t {
	odirs, ofn := sdirname(oldp)
	ndirs, nfn := sdirname(newp)

	if err, ok := crname(ofn, -EINVAL); !ok {
		return err
	}
	if err, ok := crname(nfn, -EINVAL); !ok {
		return err
	}

	op_begin()
	defer op_end()

	_renamelock.Lock()
	defer _renamelock.Unlock()

	// lookup all four files, but don't lock them yet. since we hold the
	// rename lock, none of them can be moved out from under us. they can
	// be unlinked though.
	opar, err := fs_namei(odirs, cwd)
	if err != 0 {
		return err
	}
	opar.iunlock()

	ochild, err := fs_namei(oldp, cwd)
	if err != 0 {
		return err
	}
	ochild.iunlock()

	npar, err := fs_namei(ndirs, cwd)
	if err != 0 {
		return err
	}
	npar.iunlock()

	// the new file may not exist
	newexists := false
	var newinum inum
	nchild, err := fs_namei(newp, cwd)
	if err == 0 {
		newexists = true
		newinum = nchild.priv
		nchild.iunlock()
	} else if err != 0 && err != -ENOENT {
		return err
	}

	// if src and dst are the same file, we are done
	if newexists && ochild.priv == newinum {
		return 0
	}

	// verify that ochild is not an ancestor of npar
	if err = _isancestor(ochild, npar); err != 0 {
		return err
	}

	// lock inodes. all other sanity checks.
	if !opar.ilock_namei() {
		return -ENOENT
	}
	defer opar.iunlock()

	if !ochild.ilock_namei() {
		return -ENOENT
	}
	defer ochild.iunlock()

	diffdirs := npar.priv != opar.priv
	if diffdirs {
		if !npar.ilock_namei() {
			return -ENOENT
		}
		defer npar.iunlock()
	} else {
		// XXXPANIC
		if npar != opar {
			panic("wut")
		}
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
		if !nchild.ilock_namei() {
			return -ENOENT
		}
		defer nchild.iunlock()

		// make sure old and new are either both files or both
		// directories
		if err := nchild.do_dirchk(odir); err != 0 {
			return err
		}
		// remove pre-existing new
		if err = npar.do_unlink(nfn); err != 0 {
			return err
		}
		if nchild._linkdown() {
			idaemon_remove(nchild.priv)
		}
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

func _isancestor(anc, here *imemnode_t) err_t {
	if anc == iroot {
		panic("root is always ancestor")
	}
	for here != iroot {
		if anc == here {
			return -EINVAL
		}
		if !here.ilock_namei() {
			return -ENOENT
		}
		nexti, err := here.iget("..")
		if err != 0 {
			panic(".. must exist")
		}
		next, err := idaemon_ensure(nexti)
		here.iunlock()
		if err != 0 {
			return err
		}
		here = next
	}
	return 0
}

type fsfops_t struct {
	priv	*imemnode_t
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

	if !fo.priv.ilock_opened() {
		return 0, -EBADF
	}
	did, err := fo.priv.do_read(dst, offset)
	if !useoffset && err == 0 {
		fo.offset += did
	}
	fo.priv.iunlock()
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

	op_begin()
	defer op_end()

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

	if !fo.priv.ilock_opened() {
		return 0, -EBADF
	}
	did, err := fo.priv.do_write(src, offset, append)
	if !useoffset && err == 0 {
		fo.offset += did
	}
	fo.priv.iunlock()
	return did, err
}

func (fo *fsfops_t) write(p *proc_t, src userio_i) (int, err_t) {
	return fo._write(src, -1)
}

func (fo *fsfops_t) fullpath() (string, err_t) {
	if !fo.priv.ilock_opened() {
		return "", -EBADF
	}
	fp, err := fo.priv.do_fullpath()
	fo.priv.iunlock()
	return fp, err
}

func (fo *fsfops_t) truncate(newlen uint) err_t {
	op_begin()
	defer op_end()
	if !fo.priv.ilock_opened() {
		return -EBADF
	}
	err := fo.priv.do_trunc(newlen)
	fo.priv.iunlock()
	return err
}

func (fo *fsfops_t) pwrite(src userio_i, offset int) (int, err_t) {
	return fo._write(src, offset)
}

func (fo *fsfops_t) fstat(st *stat_t) err_t {
	if !fo.priv.ilock_opened() {
		return -EBADF
	}
	err := fo.priv.do_stat(st)
	fo.priv.iunlock()
	return err
}

// XXX log those files that have no fs links but > 0 memory references to the
// journal so that if we crash before freeing its blocks, the blocks can be
// reclaimed.
func (fo *fsfops_t) close() err_t {
	return fs_close(fo.priv)
}

func (fo *fsfops_t) pathi() *imemnode_t {
	return fo.priv
}

func (fo *fsfops_t) reopen() err_t {
	if !fo.priv.ilock_opened() {
		return -EBADF
	}
	fo.priv.opencount++
	fo.priv.iunlock()
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
	if !fo.priv.ilock_opened() {
		return nil, -EBADF
	}
	mmi, err := fo.priv.do_mmapi(offset, len)
	fo.priv.iunlock()
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

func (df *devfops_t) pathi() *imemnode_t {
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
		blkno := raw.offset / 512
		b := bdev_read_block(blkno, "read")
		boff := raw.offset % 512
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
		blkno := raw.offset / 512
		boff := raw.offset % 512
		// if boff != 0 || src.remain() < 512 {
		//	buf := bdev_read_block(blkno)
		//}
		// XXX don't always have to read block in from disk
		buf := bdev_read_block(blkno, "write")
		c, err := src.uioread(buf.data[boff:])
		if err != 0 {
			return 0, err
		}
		bdev_write(buf)
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

func (raw *rawdfops_t) pathi() *imemnode_t {
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

func fs_mkdir(paths string, mode int, cwd *imemnode_t) err_t {
	op_begin()
	defer op_end()

	dirs, fn := sdirname(paths)
	if err, ok := crname(fn, -EINVAL); !ok {
		return err
	}
	if len(fn) > DNAMELEN {
		return -ENAMETOOLONG
	}

	par, err := fs_namei(dirs, cwd)
	if err != 0 {
		return err
	}

	var childi inum
	childi, err = par.do_createdir(fn)
	if err != 0 {
		par.iunlock()
		return err
	}

	child, err := idaemon_ensure(childi)
	if err != 0 {
		par.create_undo(childi, fn)
		par.iunlock()
		return err
	}
	if !child.ilock_namei() {
		panic("cannot be free")
	}
	pari := par.priv
	par.iunlock()

	child.do_insert(".", childi)
	child.do_insert("..", pari)
	child.iunlock()
	return 0
}

// a type to represent on-disk files
type fsfile_t struct {
	priv	*imemnode_t
	major	int
	minor	int
}

func _fs_open(paths string, flags fdopt_t, mode int, cwd *imemnode_t,
    major, minor int) (fsfile_t, err_t) {
	trunc := flags & O_TRUNC != 0
	creat := flags & O_CREAT != 0
	nodir := false
	// open with O_TRUNC is not read-only
	if trunc || creat {
		op_begin()
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
			par, err := fs_namei(dirs, cwd)
			if err != 0 {
				return ret, err
			}
			var childi inum
			if isdev {
				childi, err = par.do_createnod(fn, major, minor)
			} else {
				childi, err = par.do_createfile(fn)
			}
			if err != 0 && err != -EEXIST {
				par.iunlock()
				return ret, err
			}
			exists = err == -EEXIST
			idm, err = idaemon_ensure(childi)
			if err != 0 {
				par.create_undo(childi, fn)
				par.iunlock()
				return ret, err
			}
			got := idm.itrylock()
			par.iunlock()
			if got {
				break
			}
			runtime.Gosched()
		}
		oexcl := flags & O_EXCL != 0
		if exists {
			if oexcl || isdev {
				idm.iunlock()
				return ret, -EEXIST
			}
		}
	} else {
		// open existing file
		var err err_t
		idm, err = fs_namei(paths, cwd)
		if err != 0 {
			return ret, err
		}
		// idm is locked
	}
	defer idm.iunlock()

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
	ret.priv = idm
	ret.major = idm.icache.major
	ret.minor = idm.icache.minor
	return ret, 0
}

// socket files cannot be open(2)'ed (must use connect(2)/sendto(2) etc.)
var _denyopen = map[int]bool{ D_SUD: true, D_SUS: true}

func fs_open(paths string, flags fdopt_t, mode int, cwd *imemnode_t,
    major, minor int) (*fd_t, err_t) {
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

func fs_close(priv *imemnode_t) err_t {
	op_begin()

	if !priv.ilock_opened() {
		op_end()
		return -EBADF
	}
	priv.opencount--
	// iunlock() frees underlying inode and blocks, if necessary
	priv.iunlock()
	op_end()
	return 0
}

func fs_stat(path string, st *stat_t, cwd *imemnode_t) err_t {
	idm, err := fs_namei(path, cwd)
	if err != 0 {
		return err
	}
	err = idm.do_stat(st)
	idm.iunlock()
	return err
}

func print_live_blocks() {
	fmt.Printf("bdev\n")
	for _, b := range bdev_cache.blks {
		fmt.Printf("block %v\n", b)
	}
}

func fs_sync() err_t {
	// print_live_blocks()
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
func fs_namei(paths string, cwd *imemnode_t) (*imemnode_t, err_t) {
	start := iroot
	if len(paths) == 0 || paths[0] != '/' {
		start = cwd
	}
	if !start.ilock_namei() {
		// XXXPANIC
		if start == iroot {
			panic("root cannot be freed")
		}
		return nil, -ENOENT
	}
	idm := start
	pp := pathparts_t{}
	pp.pp_init(paths)
	for cp, ok := pp.next(); ok; cp, ok = pp.next() {
		n, err := idm.iget(cp)
		if err != 0 {
			idm.iunlock()
			return nil, err
		}
		// get reference to imemnode_t before unlocking, to make sure
		// the inode isn't repurposed.
		next, err := idaemon_ensure(n)
		idm.iunlock()
		if err != 0 {
			return nil, err
		}
		idm = next
		if !idm.ilock_namei() {
			return nil, -ENOENT
		}
	}
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
	chklog		bool
	log_gen		uint8
	// fill is used to fill empty pages with data from the underlying
	// device or layer. fill may write fewer bytes than the length of the
	// destination buffer.
	_fill	func(pa_t, int) (int, err_t)
	// flush writes the given buffer to the specified offset of the device.
	_flush	func(pa_t, int) err_t
}

type pginfo_t struct {
	pgn		pgn_t
	pg		*bytepg_t
	p_pg		pa_t
	dirtyblocks	[]bool
}

func (pc *pgcache_t) pgc_init(bsz int, fill func(pa_t, int) (int, err_t),
    flush func(pa_t, int) err_t) {
	if fill == nil || flush == nil {
		panic("invalid page func")
	}
	pc._fill = fill
	pc._flush = flush
	if PGSIZE % bsz != 0 {
		panic("block size does not divide pgsize")
	}
	pc.blocksz = bsz
	pc.pglim = 0
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
func (pc *pgcache_t) _ensureslot(pgn pgn_t) (bool, bool) {
	if pc.pgs.nodes >= pc.pglim {
		if !syslimit.mfspgs.take() {
			return false, false
		}
		pc.pglim++
	}
	created := false
	if _, ok := pc.pgs.lookup(pgn); !ok {
		created = true
		var pg *pg_t
		var p_pg pa_t
		for {
			var ok bool
			pg, p_pg, ok = refpg_new_nozero()
			if ok {
				break
			}
			if !memreclaim() {
				return false, false
			}
		}
		refup(p_pg)
		bpg := pg2bytes(pg)
		pgi := &pginfo_t{pgn: pgn, pg: bpg, p_pg: p_pg}
		pgi.dirtyblocks = make([]bool, PGSIZE / pc.blocksz)
		pc.pgs.insert(pgi)
	}
	return created, true
}

func (pc *pgcache_t) _ensurefill(pgn pgn_t) err_t {
	needsfill, ok := pc._ensureslot(pgn)
	if !ok {
		return -ENOMEM
	}
	if needsfill {
		pgi, ok := pc.pgs.lookup(pgn)
		if !ok {
			panic("eh")
		}
		devoffset := (int(pgi.pgn) << PGSHIFT)
		c, err := pc._fill(pgi.p_pg, devoffset)
		if err != 0 {
			panic("must succeed")
		}
		copy(pgi.pg[c:], pg2bytes(zeropg)[:])
	}
	return 0
}

// returns the raw page and physical address of requested page. fills the page
// if necessary.
func (pc *pgcache_t) pgraw(offset int) (*pg_t, pa_t, err_t) {
	pgn, _ := pc._pgblock(offset)
	err := pc._ensurefill(pgn)
	if err != 0 {
		return nil, 0, err
	}
	pgi, ok := pc.pgs.lookup(pgn)
	if !ok {
		panic("eh")
	}
	wpg := (*pg_t)(unsafe.Pointer(pgi.pg))
	return wpg, pgi.p_pg, 0
}

// offset <= end. if offset lies on the same page as end, the returned slice is
// trimmed (bytes >= end are removed).
func (pc *pgcache_t) pgfor(offset, end int) ([]uint8, err_t) {
	if offset > end {
		panic("offset must be less than end")
	}
	pgva, _, err := pc.pgraw(offset)
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
			pgoffset := j*pc.blocksz
			devoffset := (int(pgi.pgn) << PGSHIFT) + pgoffset
			pa := uintptr(pgi.p_pg) + uintptr(pgoffset)
			err := pc._flush(pa_t(pa), devoffset)

			if err != 0 {
				panic("flush must succeed")
			}
			pgi.dirtyblocks[j] = false
		}
	})
}

// decrements the refcounts of all pages in the page cache. returns the number
// of pages that the pgcache contained.
func (pc *pgcache_t) release() int {
	pgs := 0
	pc.pgs.iter(func(pgi *pginfo_t) {
		refdown(pgi.p_pg)
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
	failed := false
	pc.pgs.iter(func(pgi *pginfo_t) {
		ref, _ := _refaddr(pgi.p_pg)
		if atomic.LoadInt32(ref) != 1 {
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
		pgs = pc.release()
		// make sure any reads for the just evicted blocks read the
		// latest version of the block from the log, not the stale
		// version out on disk.
		pc.chklog = true
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
	l		trymutex_t
	// XXXPANIC for sanity
	_amlocked	bool
	opencount	int
}

var allidmons	= make(map[inum]*imemnode_t, 1e6)
var idmonl	= sync.Mutex{}

// idaemon_ensure must only be called while the imemnode_t which contains the
// directory entry for priv is locked. this protocol ensures that any calls to
// idaemon_ensure that race with unlinks must observe the imemnode object with
// links == 0 instead of finding no entry for priv in allidmons, assuming that
// priv just hasn't been read in from disk yet, and then reopening priv which
// is now a free inum.
func idaemon_ensure(priv inum) (*imemnode_t, err_t) {
	if priv <= 0 {
		panic("non-positive priv")
	}
	idmonl.Lock()
	ret, ok := allidmons[priv]
	if !ok {
		for {
			if len(allidmons) < syslimit.vnodes {
				break
			}
			if !memreclaim() {
				idmonl.Unlock()
				return nil, -ENOMEM
			}
		}
		ret = &imemnode_t{}
		err := ret.idm_init(priv)
		if err != 0 {
			idmonl.Unlock()
			return nil, -ENOMEM
		}
		pglru.mkhead(ret)
		allidmons[priv] = ret
	}
	idmonl.Unlock()
	return ret, 0
}

// caller must have containing directory locked and the directory entry for
// priv removed. furthermore, the icache.links for priv's imemnode_t must be 0.
func idaemon_remove(priv inum) {
	idmonl.Lock()
	if _, ok := allidmons[priv]; !ok {
		panic("remove of non-existant idaemon")
	}
	delete(allidmons, priv)
	idmonl.Unlock()
}

// creates a new idaemon struct and fills its inode
func (idm *imemnode_t) idm_init(priv inum) err_t {
	blkno, ioff := bidecode(priv)
	idm.priv = priv
	idm.blkno = blkno
	idm.ioff = ioff

	idm.l.tm_init()

	idm.pgcache.pgc_init(512, idm.fs_fill, idm.fs_flush)

	blk := idm.idibread()
	idm.icache.fill(blk, ioff)
	blk.bdev_refdown("idm_init")
	blk.relse()
	return 0
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
	idm.l.Lock()
	if idm.opencount == 0 && idm.icache.links == 0 {
		idm.l.Unlock()
		return false
	}
	idm._amlocked = true
	return true
}

// lock a node for path resolution. a racing thread that unlinks the file
// causes our open to fail.
func (idm *imemnode_t) ilock_namei() bool {
	idm.l.Lock()
	if idm.icache.links == 0 {
		idm.l.Unlock()
		return false
	}
	idm._amlocked = true
	return true
}

func (idm *imemnode_t) itrylock() bool {
	if !idm.l.trylock() {
		return false
	}
	if idm.icache.links == 0 {
		idm.l.Unlock()
		return false
	}
	idm._amlocked = true
	return true
}

// unlocks and frees the inode's blocks if it is both has no links and is not
// opened
func (idm *imemnode_t) iunlock() {
	if idm.opencount == 0 && idm.icache.links == 0 {
		idm.ifree()
		idm._derelease()
		pglru.remove(idm)
	}
	idm._amlocked = false
	idm.l.Unlock()
}

func (idm *imemnode_t) _locked() {
	if !idm._amlocked {
		panic("idaemon must be locked")
	}
}

func (idm *imemnode_t) _iupdate() {
	iblk := idm.idibread()
	if idm.icache.flushto(iblk, idm.ioff) {
		iblk.log_write()
	}
	iblk.bdev_refdown("_iupdate")
	iblk.relse()
}

// functions prefixed with "do_" are the conversions of the idaemon CSP code in
// daemonize.
func (idm *imemnode_t) do_trunc(truncto uint) err_t {
	idm._locked()
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
	idm._locked()
	return idm.iread(dst, offset)
}

func (idm *imemnode_t) do_write(src userio_i, _offset int,
    append bool) (int, err_t) {
	idm._locked()
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
	idm._locked()
	st.wdev(0)
	st.wino(uint(idm.priv))
	st.wmode(idm.mkmode())
	st.wsize(uint(idm.icache.size))
	st.wrdev(mkdev(idm.icache.major, idm.icache.minor))
	return 0
}

func (idm *imemnode_t) do_mmapi(off, len int) ([]mmapinfo_t, err_t) {
	idm._locked()
	if idm.icache.itype != I_FILE && idm.icache.itype != I_DIR {
		panic("bad mmapinfo")
	}
	return idm.immapinfo(off, len)
}

func (idm *imemnode_t) do_dirchk(wantdir bool) err_t {
	idm._locked()
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
	idm._locked()
	_, err := idm.iunlink(name)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_insert(fn string, n inum) err_t {
	idm._locked()
	// create new dir ent with given inode number
	err := idm.iinsert(fn, n)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_createnod(fn string, maj, min int) (inum, err_t) {
	idm._locked()
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DEV
	cnext, err := idm.icreate(fn, itype, maj, min)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createfile(fn string) (inum, err_t) {
	idm._locked()
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_FILE
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createdir(fn string) (inum, err_t) {
	idm._locked()
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DIR
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_fullpath() (string, err_t) {
	idm._locked()
	if idm.icache.itype != I_DIR {
		panic("fullpath on non-dir")
	}
	// starting at the target file, walk up to the root prepending the name
	// (which is stored in the parent's directory entry)
	c := idm
	last := idm.priv
	acc := ""
	for c != iroot {
		pari, err := c.iget("..")
		if err != 0 {
			panic(".. must exist")
		}
		par, err := idaemon_ensure(pari)
		c.iunlock()
		if err != 0 {
			return "", err
		}
		if !par.ilock_namei() {
			return "", -ENOENT
		}
		name, err := par._denamefor(last)
		if err != 0 {
			if err == -ENOENT {
				panic("child must exist")
			}
			par.iunlock()
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
	c.iunlock()
	acc = "/" + acc
	return acc, 0
}

// returns true if link count is 0
func (idm *imemnode_t) _linkdown() bool {
	idm._locked()
	idm.icache.links--
	idm._iupdate()
	return idm.icache.links == 0
}

func (idm *imemnode_t) _linkup() {
	idm._locked()
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
		mb = bdev_lookup_fill(blockn, "_mbensure")
	} else {
		mb = bdev_lookup_zero(blockn, "_mbensure")
	}
	mb.relse()
	return mb
}

func (ic *icache_t) mbread(blockn int) *bdev_block_t {
	return ic._mbensure(blockn, true)
}

// ensure the block is in the bdev cache and zero-ed
// XXX ifree writes it out, but evict should too, if we had evict.
func (ic *icache_t) mbempty(blockn int) {
	if ok := bdev_cache_present(blockn); ok {
		panic("block present")
	}
	b := ic._mbensure(blockn, false)
	b.bdev_refdown("mbempty")
}

func (ic *icache_t) fill(blk *bdev_block_t, ioff int) {
	inode := inode_t{blk, ioff}
	ic.itype = inode.itype()
	if ic.itype <= I_FIRST || ic.itype > I_VALID {
		fmt.Printf("itype: %v for %v\n", ic.itype,
		    biencode(blk.block, ioff))
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
				if idx == 63 {
					idm.icache.mbempty(blkn)
				}
			}
		}
		if added {
			blk.log_write()
		}
	}

	whichblk := offset/512
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
		slotpb := 63
		nextindb := 63*8
		indno := idm.icache.indir
		// get first indirect block
		indno, isnew := ensureb(indno)
		if isnew {
			idm.icache.mbempty(indno)
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
		src, err := idm.pgcache.pgfor(offset + c, isz)
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
		dst, err := idm.pgcache.pgfor(noff, newsz)
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
			pg, err := idm.pgcache.pgfor(int(c), int(newlen))
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

// fills the parts of pages whose offset < the file size (extending the file
// shouldn't read any blocks).
func (idm *imemnode_t) fs_fill(pa pa_t, fileoffset int) (int, err_t) {
	isz := idm.icache.size
	c := 0
	for c < 4096 && fileoffset + c < isz {
		blkno, err := idm.offsetblk(fileoffset + c, false)
		if err != 0 {
			return c, err
		}
		had := false
		a := uintptr(pa) + (uintptr)(c)
		if idm.pgcache.chklog {
			fmt.Printf("fs_fill check log\n")
			// our pages were evicted; avoid reading the block from
			// the disk which may now be stale.
			b := bdev_block_new_pa(blkno, "log_read", pa_t(a))
			loggen := idm.pgcache.log_gen
			filled, logcommitted := log_read(loggen, b)
			if logcommitted {
				idm.pgcache.chklog = false
			}
			had = filled
		}
		if !had {
			b := bdev_block_new_pa(blkno, "fs_fill", pa_t(a))
			b.bdev_read()
			b.bdev_refdown("fs_fill")
		}
		c += 512
	}
        return c, 0
}

// write the data at physical address pa to the block backing this pa
func (idm *imemnode_t) fs_flush(pa pa_t, fileoffset int) err_t {
	if memtime {
		return 0
	}
	blkno, err := idm.offsetblk(fileoffset, true)
	if err != 0 {
		return err
	}
	dur := bdev_block_new_pa(blkno, "dur", pa) 
	idm.pgcache.log_gen = dur.log_write()
	dur.bdev_refdown("fs_flush")
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
	newsz := idm.icache.size + 512
	_, err := idm.pgcache.pgfor(idm.icache.size, newsz)
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
	pg, err := idm.pgcache.pgfor(noff, noff+NDBYTES)
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
func (idm *imemnode_t) _descan(
   f func(fn string, de icdent_t)bool) (bool, err_t) {
	for i := 0; i < idm.icache.size; i+= 512 {
		pg, err := idm.pgcache.pgfor(i, i+512)
		if err != 0 {
			return false, err
		}
		// XXXPANIC
		if len(pg) != 512 {
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
	pg, err := idm.pgcache.pgfor(de.offset, de.offset + NDBYTES)
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
		return fn != "" && fn != "." && fn != ".."
	})
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
		_, err = idm.pgcache.pgfor(noff, noff+NDBYTES)
		return err
	}
	noff, err := idm._denextempty()
	if err != 0 {
		return err
	}
	_, err = idm.pgcache.pgfor(noff, noff+NDBYTES)
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
	bwords := 512/8
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
		bdev_purge(ib)
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
	ib := bdev_lookup_fill(bn, "create_undo")
	ni := &inode_t{ib, ioff}
	_iallocundo(bn, ni, ib)
	ib.bdev_refdown("create_undo")
	ib.relse()
}

func (idm *imemnode_t) icreate(name string, nitype, major,
    minor int) (inum, err_t) {
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
	newiblk := bdev_lookup_fill(newbn, "icreate")
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

	// write new directory entry referencing newinode
	err = idm._deinsert(name, newbn, newioff)
	if err != 0 {
		_iallocundo(newbn, newinode, newiblk)
	}
	newiblk.log_write()
	newiblk.relse()
	newiblk.bdev_refdown("icreate")

	newinum := inum(biencode(newbn, newioff))
	return newinum, err
}

func (idm *imemnode_t) iget(name string) (inum, err_t) {
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
		pg, phys, err := idm.pgcache.pgraw(offset + i)
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
	return bdev_lookup_fill(idm.blkno, "idibread")
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
		for i := 0; i < 63; i++ {
			off := i*8
			nblkno := readn(blks, 8, off)
			add(nblkno)
		}
		nextoff := 63*8
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
		bdev_purge(iblk)
	} else {
		iblk.log_write()
	}
	iblk.bdev_refdown("ifree2")
	iblk.relse()

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
func fieldr(p *[512]uint8, field int) int {
	return readn(p[:], 8, field*8)
}

func fieldw(p *[512]uint8, field int, val int) {
	writen(p[:], 8, field*8, val)
}

func biencode(blk int, iidx int) int {
	logisperblk := uint(2)
	isperblk := 1 << logisperblk
	if iidx < 0 || iidx >= isperblk {
		panic("bad inode index")
	}
	return int(blk << logisperblk | iidx)
}

func bidecode(val inum) (int, int) {
	logisperblk := uint(2)
	blk := int(uint(val) >> logisperblk)
	iidx := int(val) & 0x3
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
	data	*[512]uint8
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
	data	*[512]uint8
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
	NDIRENTS = 512/NDBYTES
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
	return bdev_lookup_fill(blockno, "fbread")
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
			blk.bdev_refdown("balloc1")
			blk.relse()
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
	blk.log_write()
	blk.bdev_refdown("balloc1")
	blk.relse()

	boffset := usable_start
	bitsperblk := 512*8
	return boffset + blkn*bitsperblk + oct*8 + int(bit)
}

func balloc() int {
	fblock.Lock()
	ret := balloc1()
	fblock.Unlock()
	return ret
}

func bfree(blkno int) {
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
	bitsperblk := 512*8
	fblkno := fst + bit/bitsperblk
	fbit := bit%bitsperblk
	fbyteoff := fbit/8
	fbitoff := uint(fbit%8)
	if fblkno >= fst + flen {
		panic("bad blockno")
	}
	fblk := fbread(fblkno)
	fblk.data[fbyteoff] &= ^(1 << fbitoff)
	fblk.log_write()
	fblk.bdev_refdown("bfree")
	fblk.relse()

	// force allocation of free inode block
	if ifreeblk == blkno {
		ifreeblk = 0
	}
}

var ifreeblk int	= 0
var ifreeoff int 	= 0

// returns block/index of free inode.
func ialloc() (int, int) {
	fblock.Lock()
	defer fblock.Unlock()

	if ifreeblk != 0 {
		ret := ifreeblk
		retoff := ifreeoff
		ifreeoff++
		blkwords := 512/8
		if ifreeoff >= blkwords/NIWORDS {
			// allocate a new inode block next time
			ifreeblk = 0
		}
		return ret, retoff
	}

	ifreeblk = balloc1()
	ifreeoff = 0
	zblk := bdev_lookup_zero(ifreeblk, "ialloc")
	zblk.s += "+log_write"
	zblk.log_write()
	zblk.bdev_refdown("ialloc")
	zblk.s += "-log_write"
	if zblk.bdev_refcnt() != 1 {
		panic("xxxx")
	}
	zblk.relse()
	reti := ifreeoff
	ifreeoff++
	return ifreeblk, reti
}

// our actual disk
var disk	disk_t   // XXX delete?
var adisk	adisk_t

// XXX delete and the disks that use it?
type idebuf_t struct {
	disk	int
	block	int
	data	*[512]uint8
}

type logread_t struct {
	loggen		uint8
	buf		*bdev_block_t
	had		bool
	gencommit	bool
}

type log_entry_t struct {
	block           int            // the final destination of buf
	buf             *bdev_block_t 
}

// list of dirty blocks that are pending commit.
type log_t struct {
	log		[]log_entry_t
	lhead		int
	logstart	int
	loglen		int
	incoming	chan bdev_block_t
	incgen		chan uint8
	logread		chan logread_t
	logreadret	chan logread_t
	admission	chan bool
	done		chan bool
	force		chan bool
	commitwait	chan bool
	tmpblk		*bdev_block_t
}

func (log *log_t) init(ls int, ll int) {
	log.lhead = 0
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = ll - 1
	log.log = make([]log_entry_t, log.loglen)
	log.tmpblk = bdev_block_new(log.logstart, "logstart")
	log.incoming = make(chan bdev_block_t)
	log.incgen = make(chan uint8)
	log.logread = make(chan logread_t)
	log.logreadret = make(chan logread_t)
	log.admission = make(chan bool)
	log.done = make(chan bool)
	log.force = make(chan bool)
	log.commitwait = make(chan bool)
}

func (log *log_t) addlog(buf bdev_block_t) {
	// log absorption
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		if l.block == buf.block {
			copy(l.buf.data[:], buf.data[:])
			buf.bdev_refdown("addlog1")
			buf.s += "-addlog"
			return
		}
	}

	// copy the data into lb's buf, because caller may modify it in a
	// later transaction.  XXX should use COW.

	lhead := log.lhead
	lb := bdev_block_new(log.logstart+lhead+1, "addlog")
	copy(lb.data[:], buf.data[:])

	if lhead >= len(log.log) {
		panic("log overflow")
	}
	log.log[lhead] = log_entry_t{buf.block, lb}
	log.lhead++

	buf.bdev_refdown("addlog2")
	buf.s += "-addlog"
}

func (log *log_t) commit() {
	if log.lhead == 0 {
		// nothing to commit
		return
	}

	lh := logheader_t{log.tmpblk.data}
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		// install log destination in the first log block
		lh.w_logdest(i, l.block)

		// write block into log
		bdev_write_async(l.buf)
	}
	
	bdev_flush()   // flush log

	lh.w_recovernum(log.lhead)

	// commit log: flush log header
	bdev_write(log.tmpblk)

	bdev_flush()   // commit log

	// rn := lh.recovernum()
	// if rn > 0 {
	// 	runtime.Crash()
	// }

	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover
	for i := 0; i < log.lhead; i++ {
		l := log.log[i]
		b := bdev_block_new_pa(l.block, "logapply", l.buf.pa)
		bdev_write_async(b)
		b.bdev_refdown("logapply1")
		l.buf.bdev_refdown("logapply2")
	}

	bdev_flush()  // flush apply
	
	// success; clear flag indicating to recover from log
	lh.w_recovernum(0)
	bdev_write(log.tmpblk)
	
	bdev_flush()  // flush cleared commit
	log.lhead = 0
}

func log_daemon(l *log_t) {
	// an upperbound on the number of blocks written per system call. this
	// is necessary in order to guarantee that the log is long enough for
	// the allowed number of concurrent fs syscalls.
	maxblkspersys := 10
	loggen := uint8(0)
	for {
		tickets := (l.loglen - l.lhead)/ maxblkspersys
		adm := l.admission

		done := false
		given := 0
		t := 0
		waiters := 0

                if l.loglen-l.lhead < maxblkspersys {
                       panic("must flush. not enough space left")
                }
		
		for !done {
			select {
			case nb := <- l.incoming:
				if t <= 0 {
					panic("log write without admission")
				}
				l.addlog(nb)
				l.incgen <- loggen
			case <- l.done:
				t--
				if t == 0 {
					done = true
				}
			case adm <- true:
				given++
				t++
				if given == tickets {
					adm = nil
				}
			case <- l.force:
				waiters++
				adm = nil
				if t == 0 {
					done = true
				}
			case lr := <- l.logread:
				if lr.loggen != loggen {
					ret := logread_t{}
					ret.had, ret.gencommit = false, true
					l.logreadret <- ret
					continue
				}
				want := lr.buf.block
				had := false
				for _, lblk := range l.log {
					if lblk.buf.block == want {
						copy(lr.buf.data[:], lblk.buf.data[:])
						had = true
						break
					}
				}
				l.logreadret <- logread_t{had: had}
			}
		}
		// XXX because writei doesn't break big writes up in smaller transactions
		// commit every transaction, instead of inside the if statement below
		l.commit()
		loggen++
		if waiters > 0 || l.loglen-l.lhead < maxblkspersys {
			fmt.Printf("commit %v %v\n", l.lhead, l.loglen)
			// l.commit()   // XX make asynchrounous
			// loggen++
			// wake up waiters
			go func() {
				for i := 0; i < waiters; i++ {
					l.commitwait <- true
				}
			}()
		}
	}
}

func op_begin() {
	if memtime {
		return
	}
	<- fslog.admission
}

func op_end() {
	if memtime {
		return
	}
	fslog.done <- true
}


// returns whether the log contained the requested block and whether the log
// identified by loggen has been committed (i.e. there is no need to check the
// log for blocks).
func log_read(loggen uint8, b *bdev_block_t) (bool, bool) {
	if memtime {
		return false, true
	}
	lr := logread_t{loggen: loggen, buf: b}
	fslog.logread <- lr
	ret := <- fslog.logreadret
	if ret.had && ret.gencommit {
		panic("bad state")
	}
	return ret.had, ret.gencommit
}


type bdevcmd_t uint

const (
	BDEV_WRITE  bdevcmd_t = 1
	BDEV_READ = 2
	BDEV_FLUSH = 3
)

type bdev_req_t struct {
	blk     *bdev_block_t
	ackCh	chan bool
	cmd	bdevcmd_t
	sync    bool
}

func bdev_req_new(blk *bdev_block_t, cmd bdevcmd_t, sync bool) *bdev_req_t {
	ret := &bdev_req_t{}
	ret.blk = blk
	ret.ackCh = make(chan bool)
	ret.cmd = cmd
	ret.sync = sync
	return ret
}
	
func bdev_start(req *bdev_req_t) bool {
	if req.blk != nil {
		a := (uintptr)(unsafe.Pointer(req.blk.data))
		if uint64(a) != 0 && uint64(a) < uint64(_vdirect) {
			panic("bdev_start")
		}
	}
	r := adisk.start(req)
	return r
}

// A block has a lock, since, it may store an inode block, which has 4 inodes,
// and we need to ensure that hat writes aren't lost due to concurrent inode
// updates.  the inode code is careful about releasing lock.  other types of
// blocks not shared and the caller releases the lock immediately.
//
// The data of a block is either a page in an inode's page cache or a page
// allocated from the page allocator.  The disk DMAs to and from the physical
// address for the page.  data is the virtual address for the page.
//
// When a bdev_block is initialized with a page, is sent to the log daemon, or
// handed to the disk driver, we increment the refcount on the physical page
// using bdev_refup(). When code (including driver) is done with a bdev_block,
// it decrements the reference count with bdev_refdown (e.g., in the driver
// interrupt handler).
//
// Two separately allocated block_dev_t instances may have the same block number
// but not share a physical page.  For example, the log allocates a block_dev_t
// for a block number which may also in the block cache, but has a diferent
// physical page.
type bdev_block_t struct {
	sync.Mutex
	disk	int
	block	int
	pa      pa_t
	data	*[512]uint8
	s       string
}

func (b *bdev_block_t) relse() {
	b.Unlock()
}

func bdev_block_new(block int, s string) *bdev_block_t {
	_, pa, ok := refpg_new()
	if !ok {
		panic("oom during bdev_block_new")
	}
	b := &bdev_block_t{};
	b.block = block
	b.pa = pa
	b.data = (*[512]uint8)(unsafe.Pointer(dmap(pa)))
	b.s = s
	b.bdev_refup("new")
	return b
}

func bdev_block_new_pa(block int, s string, pa pa_t) *bdev_block_t {
	b := &bdev_block_t{};
	b.block = block
	b.pa = pa
	b.data = (*[512]uint8)(unsafe.Pointer(dmap(pa)))
	b.s = s
	b.bdev_refup("new_pa")
	return b
}

func (blk *bdev_block_t) bdev_refcnt() int {
	ref, _ := _refaddr(blk.pa)
	return int(*ref)
}

func (blk *bdev_block_t) bdev_refup(s string) {
	refup(blk.pa)
	// fmt.Printf("bdev_refup: %s block %v %v\n", s, blk.block, blk.s)
}

func (blk *bdev_block_t) bdev_refdown(s string) {
	// fmt.Printf("bdev_refdown: %v %v %v\n", s, blk.block, blk.s)
	if blk.bdev_refcnt() == 0 {
		fmt.Printf("bdev_refdown %s: ref is 0 block %v %v\n", s, blk.block, blk.s)
		panic("ouch")
	}
	refdown(blk.pa)
}

// bdev_write_async increments ref to keep the page around utnil write completes
func bdev_write(b *bdev_block_t) {
	if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
		panic("write\n")
	}
	b.bdev_refup("bdev_write")
	req := bdev_req_new(b, BDEV_WRITE, true)
	if bdev_start(req) {
		<- req.ackCh
	}
}

// bdev_write_async increments ref to keep the page around until write completes
func bdev_write_async(b *bdev_block_t) {
	if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
		panic("write_async\n")
	}
	b.bdev_refup("bdev_write_async")
	ider := bdev_req_new(b, BDEV_WRITE, false)
	bdev_start(ider)
}

func (b *bdev_block_t) bdev_read() {
	ider := bdev_req_new(b, BDEV_READ, true)
	if bdev_start(ider) {
		<- ider.ackCh
	}
	//fmt.Printf("fill %v %#x %#x\n", buf.block, buf.data[0], buf.data[1])
	if b.data[0] == 0xc && b.data[1] == 0xc {
		fmt.Printf("fall: %v %v\n", b.s, b.block)
		panic("xxx\n")
	}
	
}

// after bdev_read_block returns, the caller owns the physical page.  the caller
// must call bdev_refdown when it is done with the page in the buffer returned.
func bdev_read_block(blkn int, s string) *bdev_block_t {
	buf := bdev_block_new(blkn, s)
	buf.bdev_read()
	return buf
}

func bdev_flush() {
	ider := bdev_req_new(nil, BDEV_FLUSH, true)
	if bdev_start(ider) {
		<- ider.ackCh
	}
}

// log_write increments ref so that the log has always a valid ref to the buf's page
// the logging layer refdowns when it it is done with the page
func (b *bdev_block_t) log_write() uint8 {
	if memtime {
		return 0
	}
	b.bdev_refup("log_write")
	fslog.incoming <- *b
	return <- fslog.incgen
}

// block cache. the block cache stores inode blocks, free bit-map blocks,
// metadata blocks. file blocks are stored in the inode's page cache, which also
// interacts with the VM system.
//
// the cache returns the same pointer to a block_dev_t to callers, and thus the
// callers share the physical page in the block_dev_t. the callers must
// coordinate using the lock of the block.
//
// XXX do eviction.

// XXX XXX XXX must count against mfs limit
type bdev_cache_t struct {
	// inode blkno -> disk contents
	blks		map[int]*bdev_block_t
	sync.Mutex
}

var bdev_cache = bdev_cache_t{}

func bdev_init() {
	bdev_cache.blks = make(map[int]*bdev_block_t, 30)
}

func bdev_cache_present(blkn int) bool {
	bdev_cache.Lock()
	_, ok := bdev_cache.blks[blkn]
	bdev_cache.Unlock()
	return ok
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf.
func bdev_lookup_fill(blkn int, s string) *bdev_block_t {
	if blkn < superb_start {
		panic("no")
	}
	created := false

	bdev_cache.Lock()
	buf, ok := bdev_cache.blks[blkn]
	if !ok {
		buf = bdev_block_new(blkn, s)
		buf.Lock()
		bdev_cache.blks[blkn] = buf
		created = true
	}
	bdev_cache.Unlock()

	if !created {
		buf.Lock()
		buf.s = s
	} else {
		buf.bdev_read() // fill in new bdev_cache entry

	}
	buf.bdev_refup("lookup_fill")
	return buf
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf
func bdev_lookup_zero(blkn int, s string) *bdev_block_t {
	bdev_cache.Lock()
	if obuf, ok := bdev_cache.blks[blkn]; ok {
		bdev_cache.Unlock()
		obuf.Lock()
		obuf.bdev_refup("lookup_zero1")
		var zdata [512]uint8
		*obuf.data = zdata
		obuf.s = s
		return obuf
	}
	buf := bdev_block_new(blkn, s)   // get a zero page
	bdev_cache.blks[blkn] = buf
	buf.bdev_refup("lookup_zero2")
	buf.Lock()
	bdev_cache.Unlock()
	return buf
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf
func bdev_lookup_empty(blkn int, s string) *bdev_block_t {
	bdev_cache.Lock()
	if obuf, ok := bdev_cache.blks[blkn]; ok {
		bdev_cache.Unlock()
		obuf.Lock()
		obuf.s = s
		obuf.bdev_refup("lookup_empty 1")
		return obuf
	}
	buf := bdev_block_new(blkn, s)   // XXX get a zero page, not really necesary
	bdev_cache.blks[blkn] = buf
	buf.bdev_refup("lookup_empty 2")
	buf.Lock()
	bdev_cache.Unlock()
	return buf
}


func bdev_purge(b *bdev_block_t) {
	bdev_cache.Lock()
	b.bdev_refdown("bdev_purge")
	// fmt.Printf("bdev_purge %v\n", b.block)
	delete(bdev_cache.blks, b.block)
	bdev_cache.Unlock()
}


func memreclaim() bool {
	if memtime {
		// cannot evict vnodes on a memory file system!
		return false
	}
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
		h.iunlock()
	}
	pglru.Unlock()
	return got > 0
}
