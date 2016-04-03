package main

import "fmt"
import "runtime"
import "strings"
import "sync"
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
	if l > 0 && fn[l-1] == '/' {
		fn = fn[0:l-1]
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

func crname(path string, nilpatherr int) (int, bool) {
	if path == "" {
		return nilpatherr, false
	} else if path == "." || path == ".." {
		return -EINVAL, false
	}
	return 0, true
}

func fs_init() *fd_t {
	// we are now prepared to take disk interrupts
	irq_unmask(IRQ_DISK)
	go ide_daemon()

	iblkcache.blks = make(map[int]*ibuf_t, 30)

	// find the first fs block; the build system installs it in block 0 for
	// us
	buffer := new([512]uint8)
	bdev_read(0, buffer)
	FSOFF := 506
	superb_start = readn(buffer[:], 4, FSOFF)
	if superb_start <= 0 {
		panic("bad superblock start")
	}
	// superblock is never changed, so reading before recovery is fine
	bdev_read(superb_start, buffer)
	superb = superblock_t{buffer}
	ri := superb.rootinode()

	free_start = superb.freeblock()
	free_len = superb.freeblocklen()

	logstart := free_start + free_len
	loglen := superb.loglen()
	usable_start = logstart + loglen
	if loglen < 0 || loglen > 63 {
		panic("bad log len")
	}

	fslog.init(logstart, loglen)
	fs_recover()
	go log_daemon(&fslog)

	// anything that reads disk blocks needs to happen after recovery
	fblkcache.fblkc_init(free_start, free_len)
	iroot = idaemon_ensure(ri)

	return &fd_t{fops: &fsfops_t{priv: iroot}}
}

// XXX why not method of log_t???
func fs_recover() {
	l := &fslog
	bdev_read(l.logstart, &l.tmpblk)
	lh := logheader_t{&l.tmpblk}
	rlen := lh.recovernum()
	if rlen == 0 {
		fmt.Printf("no FS recovery needed\n")
		return
	}
	fmt.Printf("starting FS recovery...")

	buffer := new([512]uint8)
	for i := 0; i < rlen; i++ {
		bdest := lh.logdest(i)
		bdev_read(l.logstart + 1 + i, buffer)
		bdev_write(bdest, buffer)
	}

	// clear recovery flag
	lh.w_recovernum(0)
	bdev_write(l.logstart, &l.tmpblk)
	fmt.Printf("restored %v blocks\n", rlen)
}

var memtime = false

func use_memfs() {
	memtime = true
	fmt.Printf("Using MEMORY FS\n")
}

// a type for an inode block/offset identifier
type inum int

func fs_link(old string, new string, cwd *imemnode_t) int {
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

func fs_unlink(paths string, cwd *imemnode_t) int {
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
	var err int
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

		child = idaemon_ensure(childi)
		if child.itrylock() {
			break
		}
		par.iunlock()
		runtime.Gosched()
	}
	defer par.iunlock()
	defer child.iunlock()

	err = child.do_chkempty()
	if err != 0 {
		return err
	}
	dorem := child._linkdown()

	// finally, remove directory
	err = par.do_unlink(fn)
	if err != 0 {
		panic("must succeed")
	}
	if dorem {
		idaemon_remove(childi)
	}
	return 0
}

// per-volume rename mutex. Linux does it so it must be OK!
var _renamelock = sync.Mutex{}

func fs_rename(oldp, newp string, cwd *imemnode_t) int {
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

	odir := ochild.icache.itype == I_DIR
	if newexists {
		if !nchild.ilock_namei() {
			return -ENOENT
		}
		defer nchild.iunlock()

		// make sure old and new are either both files or both
		// directories
		ndir := nchild.icache.itype == I_DIR
		if odir && !ndir {
			return -ENOTDIR
		} else if !odir && ndir {
			return -EISDIR
		}

		// remove pre-existing new
		if err = nchild.do_chkempty(); err != 0 {
			return err
		}
		if nchild._linkdown() {
			idaemon_remove(nchild.priv)
		}
		npar.do_unlink(nfn)
	}

	// finally, do the move
	if opar.do_unlink(ofn) != 0 {
		panic("must succeed")
	}
	if npar.do_insert(nfn, ochild.priv) != 0 {
		panic("must succeed")
	}

	// update '..'
	if odir {
		if ochild.do_unlink("..") != 0 {
			panic("must succeed")
		}
		if ochild.do_insert("..", npar.priv) != 0 {
			panic("must succeed")
		}
	}
	return 0
}

func _isancestor(anc, here *imemnode_t) int {
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
		next := idaemon_ensure(nexti)
		here.iunlock()
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

func (fo *fsfops_t) _read(dst *userbuf_t, toff int) (int, int) {
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

func (fo *fsfops_t) read(dst *userbuf_t) (int, int) {
	return fo._read(dst, -1)
}

func (fo *fsfops_t) pread(dst *userbuf_t, offset int) (int, int) {
	return fo._read(dst, offset)
}

func (fo *fsfops_t) _write(src *userbuf_t, toff int) (int, int) {
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

func (fo *fsfops_t) write(src *userbuf_t) (int, int) {
	return fo._write(src, -1)
}

func (fo *fsfops_t) fullpath() (string, int) {
	if !fo.priv.ilock_opened() {
		return "", -EBADF
	}
	fp, err := fo.priv.do_fullpath()
	fo.priv.iunlock()
	return fp, err
}

func (fo *fsfops_t) truncate(newlen uint) int {
	op_begin()
	defer op_end()
	if !fo.priv.ilock_opened() {
		return -EBADF
	}
	err := fo.priv.do_trunc(newlen)
	fo.priv.iunlock()
	return err
}

func (fo *fsfops_t) pwrite(src *userbuf_t, offset int) (int, int) {
	return fo._write(src, offset)
}

func (fo *fsfops_t) fstat(st *stat_t) int {
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
func (fo *fsfops_t) close() int {
	return fs_close(fo.priv)
}

func (fo *fsfops_t) pathi() *imemnode_t {
	return fo.priv
}

func (fo *fsfops_t) reopen() int {
	if !fo.priv.ilock_opened() {
		return -EBADF
	}
	fo.priv.opencount++
	fo.priv.iunlock()
	return 0
}

func (fo *fsfops_t) lseek(off, whence int) int {
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
		st.init()
		fo.fstat(st)
		fo.offset = st.size() + off
	default:
		return -EINVAL
	}
	if fo.offset < 0 {
		fo.offset = 0
	}
	return fo.offset
}

// returns the mmapinfo for the pages of the target file. the page cache is
// populated if necessary.
func (fo *fsfops_t) mmapi(offset int) ([]mmapinfo_t, int) {
	if !fo.priv.ilock_opened() {
		return nil, -EBADF
	}
	mmi, err := fo.priv.do_mmapi(offset)
	fo.priv.iunlock()
	return mmi, err
}

func (fo *fsfops_t) accept(*proc_t, *userbuf_t) (fdops_i, int, int) {
	return nil, 0, -ENOTSOCK
}

func (fo *fsfops_t) bind(*proc_t, []uint8) int {
	return -ENOTSOCK
}

func (fo *fsfops_t) connect(proc *proc_t, sabuf []uint8) int {
	return -ENOTSOCK
}

func (fo *fsfops_t) listen(*proc_t, int) (fdops_i, int) {
	return nil, -ENOTSOCK
}

func (fo *fsfops_t) sendto(*proc_t, *userbuf_t, []uint8, int) (int, int) {
	return 0, -ENOTSOCK
}

func (fo *fsfops_t) recvfrom(*proc_t, *userbuf_t, *userbuf_t) (int, int, int) {
	return 0, 0, -ENOTSOCK
}

func (fo *fsfops_t) pollone(pm pollmsg_t) ready_t {
	return pm.events & (R_READ | R_WRITE)
}

func (fo *fsfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	return -ENOSYS
}

func (fo *fsfops_t) getsockopt(proc *proc_t, opt int, bufarg *userbuf_t,
    intarg int) (int, int) {
	return 0, -ENOTSOCK
}

type devfops_t struct {
	priv	*imemnode_t
	maj	int
	min	int
}

func (df *devfops_t) _sane() {
	// make sure this maj/min pair is handled by devfops_t. to handle more
	// devices, we can either do dispatch in devfops_t or we can return
	// device-specific fdops_i in fs_open()
	if df.maj != D_CONSOLE {
		panic("bad dev")
	}
}

func (df *devfops_t) read(dst *userbuf_t) (int, int) {
	df._sane()
	return cons_read(dst, 0)
}

func (df *devfops_t) write(src *userbuf_t) (int, int) {
	df._sane()
	return cons_write(src, 0)
}

func (df *devfops_t) fullpath() (string, int) {
	panic("weird cwd")
}

func (df *devfops_t) truncate(newlen uint) int {
	return -EINVAL
}

func (df *devfops_t) pread(dst *userbuf_t, offset int) (int, int) {
	df._sane()
	return 0, -ESPIPE
}

func (df *devfops_t) pwrite(src *userbuf_t, offset int) (int, int) {
	df._sane()
	return 0, -ESPIPE
}

func (df *devfops_t) fstat(st *stat_t) int {
	df._sane()
	panic("no imp")
}

func (df *devfops_t) mmapi(offset int) ([]mmapinfo_t, int) {
	df._sane()
	return nil, -ENODEV
}

func (df *devfops_t) pathi() *imemnode_t {
	df._sane()
	panic("bad cwd")
}

func (df *devfops_t) close() int {
	df._sane()
	return 0
}

func (df *devfops_t) reopen() int {
	df._sane()
	return 0
}

func (df *devfops_t) lseek(int, int) int {
	df._sane()
	return -ESPIPE
}

func (df *devfops_t) accept(*proc_t, *userbuf_t) (fdops_i, int, int) {
	return nil, 0, -ENOTSOCK
}

func (df *devfops_t) bind(*proc_t, []uint8) int {
	return -ENOTSOCK
}

func (df *devfops_t) connect(proc *proc_t, sabuf []uint8) int {
	return -ENOTSOCK
}

func (df *devfops_t) listen(*proc_t, int) (fdops_i, int) {
	return nil, -ENOTSOCK
}

func (df *devfops_t) sendto(*proc_t, *userbuf_t, []uint8, int) (int, int) {
	return 0, -ENOTSOCK
}

func (df *devfops_t) recvfrom(*proc_t, *userbuf_t, *userbuf_t) (int, int, int) {
	return 0, 0, -ENOTSOCK
}

func (df *devfops_t) pollone(pm pollmsg_t) ready_t {
	return pm.events & (R_READ | R_WRITE)
}

func (df *devfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	return -ENOSYS
}

func (df *devfops_t) getsockopt(proc *proc_t, opt int, bufarg *userbuf_t,
    intarg int) (int, int) {
	return 0, -ENOTSOCK
}

func fs_mkdir(paths string, mode int, cwd *imemnode_t) int {
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

	child := idaemon_ensure(childi)
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

func _fs_open(paths string, flags int, mode int, cwd *imemnode_t,
    major, minor int) (fsfile_t, int) {
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
			idm = idaemon_ensure(childi)
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
		var err int
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

func fs_open(paths string, flags, mode int, cwd *imemnode_t,
    major, minor int) (*fd_t, int) {
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
		ret.fops = &devfops_t{priv: priv, maj: maj, min: min}
	} else {
		apnd := flags & O_APPEND != 0
		ret.fops = &fsfops_t{priv: priv, append: apnd}
	}
	return ret, 0
}

func fs_close(priv *imemnode_t) int {
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

func fs_stat(path string, st *stat_t, cwd *imemnode_t) int {
	idm, err := fs_namei(path, cwd)
	if err != 0 {
		return err
	}
	err = idm.do_stat(st)
	idm.iunlock()
	return err
}

func fs_sync() int {
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
func fs_namei(paths string, cwd *imemnode_t) (*imemnode_t, int) {
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
		next := idaemon_ensure(n)
		idm.iunlock()
		idm = next
		if !idm.ilock_namei() {
			return nil, -ENOENT
		}
	}
	return idm, 0
}

// XXX don't need to fill the destination page if the write covers the whole
// page
// XXX need a mechanism for evicting page cache...
type pgcache_t struct {
	pgs	[]*[PGSIZE]uint8
	pginfo	[]pgcinfo_t
	// the unit size of underlying device
	blocksz		int
	// fill is used to fill empty pages with data from the underlying
	// device or layer. its arguments are the destination slice and offset
	// and returns error. fill may write fewer bytes than the length of the
	// destination buffer.
	_fill	func([]uint8, int) int
	// flush writes the given buffer to the specified offset of the device.
	_flush	func([]uint8, int) int
}

type pgcinfo_t struct {
	phys		int
	dirtyblocks	[]bool
}

func (pc *pgcache_t) pgc_init(bsz int, fill func([]uint8, int) int,
    flush func([]uint8, int) int) {
	if fill == nil || flush == nil {
		panic("invalid page func")
	}
	pc._fill = fill
	pc._flush = flush
	if PGSIZE % bsz != 0 {
		panic("block size does not divide pgsize")
	}
	pc.blocksz = bsz
}

// takes an offset, returns the page number and starting block
func (pc *pgcache_t) _pgblock(offset int) (int, int) {
	pgn := offset / PGSIZE
	block := (offset % PGSIZE) / pc.blocksz
	return pgn, block
}

// mark the range [offset, end) as dirty
func (pc *pgcache_t) pgdirty(offset, end int) {
	for i := offset; i < end; i += pc.blocksz {
		pgn, chn := pc._pgblock(i)
		pgi := pc.pginfo[pgn]
		if pc.pgs[pgn] == nil {
			panic("pg not init'ed")
		}
		pgi.dirtyblocks[chn] = true
		pc.pginfo[pgn] = pgi
	}
}

// returns the raw page and physical address of requested page. fills the page
// if necessary.
func (pc *pgcache_t) pgraw(offset int) (*[512]int, int) {
	pgn := offset / PGSIZE
	err := pc._ensurefill(pgn)
	if err != 0 {
		panic("must succeed")
	}
	pgi := pc.pginfo[pgn]
	wpg := (*[512]int)(unsafe.Pointer(pc.pgs[pgn]))
	return wpg, pgi.phys
}

func (pc *pgcache_t) _ensureslot(pgn int) bool {
	// XXXPANIC
	if len(pc.pgs) != len(pc.pginfo) {
		panic("weird lens")
	}
	// make arrays large enough to hold this page
	if pgn >= len(pc.pgs) {
		nlen := pgn + 3
		npgs := make([]*[PGSIZE]uint8, nlen)
		copy(npgs, pc.pgs)
		npgi := make([]pgcinfo_t, nlen)
		copy(npgi, pc.pginfo)
		pc.pgs = npgs
		pc.pginfo = npgi
	}
	created := false
	if pc.pgs[pgn] == nil {
		created = true
		//npg, p_npg := pg_new()
		npg, p_npg := refpg_new()
		refup(uintptr(p_npg))
		bpg := (*[PGSIZE]uint8)(unsafe.Pointer(npg))
		pc.pgs[pgn] = bpg
		var pgi pgcinfo_t
		pgi.phys = p_npg
		pgi.dirtyblocks = make([]bool, PGSIZE / pc.blocksz)
		pc.pginfo[pgn] = pgi
	}
	return created
}

// return error
func (pc *pgcache_t) _ensurefill(pgn int) int {
	needsfill := pc._ensureslot(pgn)
	pgva := pc.pgs[pgn]
	if needsfill {
		devoffset := pgn * PGSIZE
		err := pc._fill(pgva[:], devoffset)
		if err != 0 {
			panic("must succeed")
		}
	}
	return 0
}

// offset <= end. if offset lies on the same page as end, the returned slice is
// trimmed (bytes >= end are removed).
func (pc *pgcache_t) pgfor(offset, end int) ([]uint8, int) {
	if offset > end {
		panic("offset must be less than end")
	}
	pgn := offset / PGSIZE
	err := pc._ensurefill(pgn)
	if err != 0 {
		panic("must succeed")
	}
	pgva := pc.pgs[pgn]
	pg := pgva[:]

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
	for i := range pc.pgs {
		pgva := pc.pgs[i]
		if pgva == nil {
			continue
		}
		pgi := pc.pginfo[i]
		for j, dirty := range pgi.dirtyblocks {
			if !dirty {
				continue
			}
			pgoffset := j*pc.blocksz
			pgend := pgoffset + pc.blocksz
			s := pgva[pgoffset:pgend]
			devoffset := i*PGSIZE + pgoffset
			err := pc._flush(s, devoffset)
			if err != 0 {
				panic("flush must succeed")
			}
			pgi.dirtyblocks[j] = false
		}
	}
}

// a mutex that allows attempting to acuire the mutex without blocking. useful
// for deadlock avoidance for creat/unlink/rename.
type trymutex_t struct {
	m	sync.Mutex
	waiters	int
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
		tm.waiters++
		tm.cond.Wait()
	}
}

func (tm *trymutex_t) Unlock() {
	tm.m.Lock()
	tm.lock = 0
	dowake := tm.waiters > 0
	if dowake {
		tm.waiters--
	}
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

type imemnode_t struct {
	priv		inum
	blkno		int
	ioff		int
	// cache of file data
	pgcache		pgcache_t
	// cache of inode metadata
	icache		icache_t
	//l		sync.Mutex
	l		trymutex_t
	// XXXPANIC for sanity
	_amlocked	bool
	opencount	int
}

var allidmons	= map[inum]*imemnode_t{}
var idmonl	= sync.Mutex{}

func idaemon_ensure(priv inum) *imemnode_t {
	if priv <= 0 {
		panic("non-positive priv")
	}
	idmonl.Lock()
	ret, ok := allidmons[priv]
	if !ok {
		ret = &imemnode_t{}
		ret.idm_init(priv)
		allidmons[priv] = ret
	}
	idmonl.Unlock()
	return ret
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
func (idm *imemnode_t) idm_init(priv inum) {
	blkno, ioff := bidecode(int(priv))
	idm.priv = priv
	idm.blkno = blkno
	idm.ioff = ioff

	idm.l.tm_init()

	idm.pgcache.pgc_init(512, idm.fs_fill, idm.fs_flush)

	blk := ibread(blkno)
	idm.icache.fill(blk, ioff)
	ibrelse(blk)
	if idm.icache.itype == I_DIR {
		idm.cache_dirents()
	}
}

func (idm *imemnode_t) cache_dirents() {
	ic := &idm.icache
	ic.dents = make(map[string]icdent_t)
	ic._free_dents = make([]icdent_t, 0, NDIRENTS)
	ic.free_dents = ic._free_dents
	for i := 0; i < idm.icache.size; i+= 512 {
		pg, err := idm.pgcache.pgfor(i, i+512)
		if err != 0 {
			panic("must succeed")
		}
		// XXXPANIC
		if len(pg) != 512 {
			panic("wut")
		}
		dd := dirdata_t{pg}
		for j := 0; j < NDIRENTS; j++ {
			fn := dd.filename(j)
			priv := dd.inodenext(j)
			nde := icdent_t{i+j*NDBYTES, priv}
			if fn == "" {
				ic.free_dents = append(ic.free_dents, nde)
			} else {
				ic.dents[fn] = nde
			}
		}
	}
}

// if returns true, the inode is valid (not freed) and locked. otherwise the inode
// is unlocked. it is for locking an imemnode that that has been opened (no name
// resolution required). checking opencount is to detect races between threads on
// a single fd.
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

// lock a node for path resolution
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
	iblk := ibread(idm.blkno)
	if idm.icache.flushto(iblk, idm.ioff) {
		iblk.log_write()
	}
	ibrelse(iblk)
}

// functions prefixed with "do_" are the conversions of the idaemon CSP code in
// daemonize.
func (idm *imemnode_t) do_trunc(truncto uint) int {
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

func (idm *imemnode_t) do_read(dst *userbuf_t, offset int) (int, int) {
	idm._locked()
	return idm.iread(dst, offset)
}

func (idm *imemnode_t) do_write(src *userbuf_t, _offset int,
    append bool) (int, int) {
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

func (idm *imemnode_t) do_stat(st *stat_t) int {
	idm._locked()
	st.wdev(0)
	st.wino(int(idm.priv))
	st.wmode(idm.mkmode())
	st.wsize(idm.icache.size)
	st.wrdev(mkdev(idm.icache.major, idm.icache.minor))
	return 0
}

func (idm *imemnode_t) do_mmapi(off int) ([]mmapinfo_t, int) {
	idm._locked()
	if idm.icache.itype != I_FILE && idm.icache.itype != I_DIR {
		panic("bad mmapinfo")
	}
	return idm.immapinfo(off)
}

func (idm *imemnode_t) do_chkempty() int {
	idm._locked()
	err := 0
	if idm.icache.itype == I_DIR && !idm.idirempty() {
		err = -ENOTEMPTY
	}
	return err
}

func (idm *imemnode_t) do_unlink(name string) int {
	idm._locked()
	_, err := idm.iunlink(name)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_insert(fn string, n inum) int {
	idm._locked()
	// create new dir ent with given inode number
	err := idm.iinsert(fn, n)
	if err == 0 {
		idm._iupdate()
	}
	return err
}

func (idm *imemnode_t) do_createnod(fn string, maj, min int) (inum, int) {
	idm._locked()
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DEV
	cnext, err := idm.icreate(fn, itype, maj, min)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createfile(fn string) (inum, int) {
	idm._locked()
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_FILE
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_createdir(fn string) (inum, int) {
	idm._locked()
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}

	itype := I_DIR
	cnext, err := idm.icreate(fn, itype, 0, 0)
	idm._iupdate()
	return cnext, err
}

func (idm *imemnode_t) do_fullpath() (string, int) {
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
		par := idaemon_ensure(pari)
		c.iunlock()
		if !par.ilock_namei() {
			return "", -ENOENT
		}
		found := false
		for name, icd := range par.icache.dents {
			if icd.priv == last {
				// POSIX: "no unnecessary slashes"
				if acc == "" {
					acc = name
				} else {
					acc = name + "/" + acc
				}
				found = true
				break
			}
		}
		// XXXPANIC
		if !found {
			panic("child must exist")
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

// inode block cache. there are 4 inodes per inode block. we need a block cache
// so that writes aren't lost due to concurrent inode updates.
type ibuf_t struct {
	block		int
	data		*[512]uint8
	sync.Mutex
}

type iblkcache_t struct {
	// inode blkno -> disk contents
	blks		map[int]*ibuf_t
	sync.Mutex
}

func (ib *ibuf_t) log_write() {
	dur := idebuf_t{}
	dur.block = ib.block
	dur.data = ib.data
	log_write(dur)
}

var iblkcache = iblkcache_t{}

// XXX shouldn't synchronize; idaemon's don't share anything but we need to
// combine the writes of different inodes that share the same block.
func ibread(blkn int) *ibuf_t {
	if blkn < superb_start {
		panic("no")
	}
	created := false

	iblkcache.Lock()
	iblk, ok := iblkcache.blks[blkn]
	if !ok {
		iblk = &ibuf_t{}
		iblk.Lock()
		iblkcache.blks[blkn] = iblk
		created = true
	}
	iblkcache.Unlock()

	if !created {
		iblk.Lock()
	} else {
		// fill new iblkcache entry
		iblk.data = new([512]uint8)
		iblk.block = blkn
		bdev_read(blkn, iblk.data)
	}
	return iblk
}

func ibempty(blkn int) *ibuf_t {
	iblkcache.Lock()
	if oiblk, ok := iblkcache.blks[blkn]; ok {
		iblkcache.Unlock()
		oiblk.Lock()
		var zdata [512]uint8
		*oiblk.data = zdata
		return oiblk
	}
	iblk := &ibuf_t{}
	iblk.block = blkn
	iblk.data = new([512]uint8)
	iblkcache.blks[blkn] = iblk
	iblk.Lock()
	iblkcache.Unlock()
	return iblk
}

func ibrelse(ib *ibuf_t) {
	ib.Unlock()
}

func ibpurge(ib *ibuf_t) {
	iblkcache.Lock()
	delete(iblkcache.blks, ib.block)
	iblkcache.Unlock()
}

type icache_t struct {
	itype		int
	links		int
	size		int
	major		int
	minor		int
	indir		int
	addrs		[NIADDRS]int
	dents		map[string]icdent_t
	_free_dents	[]icdent_t
	free_dents	[]icdent_t
	// inode specific metadata blocks
	metablks	map[int]*mdbuf_t
}

// struct to hold the offset/priv of directory entry slots
type icdent_t struct {
	offset	int
	priv	inum
}

// metadata block cache; only one idaemon touches these blocks at a time, thus
// no concurrency control
type mdbuf_t struct {
	block	int
	data	[512]uint8
}

func (ic *icache_t) _mbensure(blockn int, fill bool) *mdbuf_t {
	mb, ok := ic.metablks[blockn]
	if !ok {
		mb = &mdbuf_t{}
		mb.block = blockn
		if fill {
			bdev_read(blockn, &mb.data)
		}
		ic.metablks[blockn] = mb
	}
	return mb
}

func (mb *mdbuf_t) log_write() {
	dur := idebuf_t{}
	dur.block = mb.block
	dur.data = &mb.data
	log_write(dur)
}

func (ic *icache_t) mbread(blockn int) *mdbuf_t {
	return ic._mbensure(blockn, true)
}

func (ic *icache_t) mbrelse(mb *mdbuf_t) {
}

func (ic *icache_t) mbempty(blockn int) *mdbuf_t {
	if _, ok := ic.metablks[blockn]; ok {
		panic("block present")
	}
	return ic._mbensure(blockn, false)
}

func (ic *icache_t) fill(blk *ibuf_t, ioff int) {
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
	ic.metablks = make(map[int]*mdbuf_t, 5)
}

// returns true if the inode data changed, and thus needs to be flushed to disk
func (ic *icache_t) flushto(blk *ibuf_t, ioff int) bool {
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
func (idm *imemnode_t) offsetblk(offset int, writing bool) int {
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
	ensureind := func(blk *mdbuf_t, idx int) {
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
			idm.icache.mbrelse(indblk)
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
		idm.icache.mbrelse(indblk)
	} else {
		blkn = idm.icache.addrs[whichblk]
	}
	if blkn <= 0 || blkn >= superb.lastblock() {
		panic("bad data blocks")
	}
	return blkn
}

func (idm *imemnode_t) iread(dst *userbuf_t, offset int) (int, int) {
	isz := idm.icache.size
	c := 0
	for offset + c < isz && dst.remain() != 0 {
		src, err := idm.pgcache.pgfor(offset + c, isz)
		if err != 0 {
			return c, err
		}
		wrote, err := dst.write(src)
		c += wrote
		if err != 0 {
			return c, err
		}
	}
	return c, 0
}

func (idm *imemnode_t) iwrite(src *userbuf_t, offset int) (int, int) {
	sz := src.len
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
		read, err := src.read(dst)
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
func (idm *imemnode_t) _preventhole(_oldlen int, newlen uint) int {
	// XXX fix sign
	oldlen := uint(_oldlen)
	if newlen > oldlen {
		// extending file; fill new content in page cache with zeros
		first := true
		c := oldlen
		for c < newlen {
			pg, err := idm.pgcache.pgfor(int(c), int(newlen))
			if err != 0 {
				panic("must succeed")
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
		idm.pgcache.pgdirty(int(oldlen), int(newlen))
	}
	return 0
}

func (idm *imemnode_t) itrunc(newlen uint) int {
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
func (idm *imemnode_t) fs_fill(pgdst []uint8, fileoffset int) int {
	isz := idm.icache.size
	c := 0
	for len(pgdst) != 0 && fileoffset + c < isz {
		blkno := idm.offsetblk(fileoffset + c, false)
		// XXXPANIC
		if len(pgdst) < 512 {
			panic("no")
		}
		p := (*[512]uint8)(unsafe.Pointer(&pgdst[0]))
		bdev_read(blkno, p)
		c += len(p)
		pgdst = pgdst[len(p):]
	}
	return 0
}

// flush only the parts of pages whose offset is < file size
func (idm *imemnode_t) fs_flush(pgsrc []uint8, fileoffset int) int {
	if memtime {
		return 0
	}
	isz := idm.icache.size
	c := 0
	for len(pgsrc) != 0 && fileoffset + c < isz {
		blkno := idm.offsetblk(fileoffset + c, true)
		// XXXPANIC
		if len(pgsrc) < 512 {
			panic("no")
		}
		p := (*[512]uint8)(unsafe.Pointer(&pgsrc[0]))
		dur := idebuf_t{block: blkno, data: p}
		log_write(dur)
		wrote := len(p)
		c += wrote
		pgsrc = pgsrc[wrote:]
	}
	return 0
}

func (idm *imemnode_t) dirent_add(name string, nblkno int, ioff int) {
	if _, ok := idm.icache.dents[name]; ok {
		panic("dirent already exists")
	}
	var ddata dirdata_t
	noff := idm.dirent_getempty()
	pg, err := idm.pgcache.pgfor(noff, noff+NDBYTES)
	if err != 0 {
		panic("must succeed")
	}
	ddata.data = pg

	// write dir entry
	if ddata.filename(0) != "" {
		panic("dir entry slot is not free")
	}
	ddata.w_filename(0, name)
	ddata.w_inodenext(0, nblkno, ioff)
	idm.pgcache.pgdirty(noff, noff+NDBYTES)
	idm.pgcache.flush()
	de := icdent_t{noff, inum(biencode(nblkno, ioff))}
	idm.icache.dents[name] = de
}

func (idm *imemnode_t) icreate(name string, nitype, major,
    minor int) (inum, int) {
	if nitype <= I_INVALID || nitype > I_VALID {
		fmt.Printf("itype: %v\n", nitype)
		panic("bad itype!")
	}
	if name == "" {
		panic("icreate with no name")
	}
	if nitype != I_DEV && (major != 0 || minor != 0) {
		panic("inconsistent args")
	}
	// make sure file does not already exist
	if de, ok := idm.icache.dents[name]; ok {
		return de.priv, -EEXIST
	}

	// allocate new inode
	newbn, newioff := ialloc()

	newiblk := ibread(newbn)
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

	newiblk.log_write()
	ibrelse(newiblk)

	// write new directory entry referencing newinode
	idm.dirent_add(name, newbn, newioff)

	newinum := inum(biencode(newbn, newioff))
	return newinum, 0
}

func (idm *imemnode_t) iget(name string) (inum, int) {
	// did someone confuse a file with a directory?
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}
	if de, ok := idm.icache.dents[name]; ok {
		return de.priv, 0
	}
	return 0, -ENOENT
}

// creates a new directory entry with name "name" and inode number priv
func (idm *imemnode_t) iinsert(name string, priv inum) int {
	if idm.icache.itype != I_DIR {
		return -ENOTDIR
	}
	if _, ok := idm.icache.dents[name]; ok {
		return -EEXIST
	}
	a, b := bidecode(int(priv))
	idm.dirent_add(name, a, b)
	return 0
}

// returns inode number of unliked inode so caller can decrement its ref count
func (idm *imemnode_t) iunlink(name string) (inum, int) {
	if idm.icache.itype != I_DIR {
		panic("unlink to non-dir")
	}
	de, ok := idm.icache.dents[name]
	if !ok {
		return 0, -ENOENT
	}
	pg, err := idm.pgcache.pgfor(de.offset, de.offset + NDBYTES)
	if err != 0 {
		panic("must succeed")
	}
	dirdata := dirdata_t{pg}
	dirdata.w_filename(0, "")
	dirdata.w_inodenext(0, 0, 0)
	idm.pgcache.pgdirty(de.offset, de.offset + NDBYTES)
	idm.pgcache.flush()
	// add back to free dents
	delete(idm.icache.dents, name)
	icd := icdent_t{de.offset, 0}
	idm.icache.free_dents = append(idm.icache.free_dents, icd)
	return de.priv, 0
}

// returns true if the inode has no directory entries
func (idm *imemnode_t) idirempty() bool {
	canrem := func(s string) bool {
		if s == ".." || s == "." {
			return false
		}
		return true
	}
	empty := true
	for fn := range idm.icache.dents {
		if canrem(fn) {
			empty = false
		}
	}
	return empty
}

func (idm *imemnode_t) immapinfo(offset int) ([]mmapinfo_t, int) {
	isz := idm.icache.size
	pgc := roundup(isz, PGSIZE) / PGSIZE
	ret := make([]mmapinfo_t, pgc)
	for i := 0; i < isz; i += PGSIZE {
		pg, phys := idm.pgcache.pgraw(i)
		pgn := i / PGSIZE
		ret[pgn].pg = pg
		ret[pgn].phys = phys
	}
	return ret, 0
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
		idm.icache.mbrelse(blk)
	}

	// mark this inode as dead and free the inode block if all inodes on it
	// are marked dead.
	alldead := func(blk *ibuf_t) bool {
		bwords := 512/8
		ret := true
		for i := 0; i < bwords/NIWORDS; i++ {
			icache := inode_t{blk, i}
			if icache.itype() != I_DEAD {
				ret = false
				break
			}
		}
		return ret
	}
	iblk := ibread(idm.blkno)
	idm.icache.itype = I_DEAD
	idm.icache.flushto(iblk, idm.ioff)
	if alldead(iblk) {
		add(idm.blkno)
		ibpurge(iblk)
	} else {
		iblk.log_write()
	}
	ibrelse(iblk)

	// could batch free
	for _, blkno := range allb {
		bfree(blkno)
	}
	// free pagecache pages
	for i := range idm.pgcache.pginfo {
		if idm.pgcache.pginfo[i].phys == 0 {
			continue
		}
		refdown(uintptr(idm.pgcache.pginfo[i].phys))
	}
}

// returns the offset of an empty directory entry
func (idm *imemnode_t) dirent_getempty() int {
	frd := idm.icache.free_dents
	if len(frd) > 0 {
		ret := frd[0]
		idm.icache.free_dents = frd[1:]
		if len(idm.icache.free_dents) == 0 {
			idm.icache.free_dents = idm.icache._free_dents
		}
		return ret.offset
	}

	// current dir blocks are full -- allocate new dirdata block. make
	// sure its in the page cache but not fill'ed from disk
	newsz := idm.icache.size + 512
	_, err := idm.pgcache.pgfor(idm.icache.size, newsz)
	if err != 0 {
		panic("must succeed")
	}
	idm.pgcache.pgdirty(idm.icache.size, newsz)
	newoff := idm.icache.size
	// NDIRENTS - 1 because we return slot 0 directly
	idm.icache.free_dents = idm.icache._free_dents
	for i := 1; i < NDIRENTS; i++ {
		fde := icdent_t{newoff + NDBYTES*i, 0}
		idm.icache.free_dents = append(idm.icache.free_dents, fde)
	}
	idm.icache.size = newsz
	return newoff
}

// used for {,f}stat
func (idm *imemnode_t) mkmode() int {
	itype := idm.icache.itype
	switch itype {
	case I_DIR, I_FILE:
		return itype
	case I_DEV:
		return mkdev(idm.icache.major, idm.icache.minor)
	default:
		fmt.Printf("itype: %v\n", itype)
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

func bidecode(val int) (int, int) {
	logisperblk := uint(2)
	blk := int(uint(val) >> logisperblk)
	iidx := val & 0x3
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
type inode_t struct {
	iblk	*ibuf_t
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
	blks		[]*ibuf_t
	free_start	int
}

var fblkcache = fblkcache_t{}

func (fbc *fblkcache_t) fblkc_init(free_start, free_len int) {
	fbc.blks = make([]*ibuf_t, free_len)
	fbc.free_start = free_start
	for i := range fbc.blks {
		blkno := free_start + i
		fb := &ibuf_t{}
		fbc.blks[i] = fb
		fb.block = blkno
		fb.data = new([512]uint8)
		bdev_read(blkno, fb.data)
	}
}

func fbread(blockno int) *ibuf_t {
	if blockno < superb_start {
		panic("naughty blockno")
	}
	n := blockno - fblkcache.free_start
	fblkcache.blks[n].Lock()
	return fblkcache.blks[n]
}

func fbrelse(fb *ibuf_t) {
	fb.Unlock()
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
	var blk *ibuf_t
	var blkn int
	var oct int
	// 0 is free, 1 is allocated
	for b := 0; b < flen && !found; b++ {
		i := (_lastblkno + b) % flen
		if blk != nil {
			fbrelse(blk)
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
	fbrelse(blk)

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
	fbrelse(fblk)

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
	zblk := ibempty(ifreeblk)
	zblk.log_write()
	ibrelse(zblk)
	reti := ifreeoff
	ifreeoff++
	return ifreeblk, reti
}

// our actual disk
var disk	disk_t

type idebuf_t struct {
	disk	int
	block	int
	data	*[512]uint8
}

type idereq_t struct {
	buf	*idebuf_t
	ack	chan bool
	write	bool
}

var ide_int_done	= make(chan bool)
var ide_request		= make(chan *idereq_t)

func ide_daemon() {
	for {
		req := <- ide_request
		if req.buf == nil {
			panic("nil idebuf")
		}
		writing := req.write
		disk.start(req.buf, writing)
		<- ide_int_done
		disk.complete(req.buf.data[:], writing)
		disk.int_clear()
		req.ack <- true
	}
}

func idereq_new(block int, write bool, data *[512]uint8) *idereq_t {
	ret := &idereq_t{}
	ret.ack = make(chan bool)
	ret.write = write
	ret.buf = &idebuf_t{}
	ret.buf.block = block
	ret.buf.data = data
	if data == nil {
		panic("no nil")
	}
	return ret
}

// list of dirty blocks that are pending commit.
type log_t struct {
	blks		[]idebuf_t
	lhead		int
	logstart	int
	loglen		int
	incoming	chan idebuf_t
	admission	chan bool
	done		chan bool
	force		chan bool
	commitwait	chan bool
	tmpblk		[512]uint8
}

func (log *log_t) init(ls int, ll int) {
	log.lhead = 0
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = ll - 1
	log.blks = make([]idebuf_t, log.loglen)
	for i := range log.blks {
		log.blks[i].data = new([512]uint8)
	}
	log.incoming = make(chan idebuf_t)
	log.admission = make(chan bool)
	log.done = make(chan bool)
	log.force = make(chan bool)
	log.commitwait = make(chan bool)
}

func (log *log_t) addlog(buf idebuf_t) {
	// log absorption
	for i := 0; i < log.lhead; i++ {
		b := log.blks[i]
		if b.block == buf.block {
			*log.blks[i].data = *buf.data
			return
		}
	}
	lhead := log.lhead
	if lhead >= len(log.blks) {
		panic("log overflow")
	}
	log.blks[lhead].disk = buf.disk
	log.blks[lhead].block = buf.block
	*log.blks[lhead].data = *buf.data
	log.lhead++
}

func (log *log_t) commit() {
	if log.lhead == 0 {
		// nothing to commit
		return
	}

	lh := logheader_t{&log.tmpblk}
	for i := 0; i < log.lhead; i++ {
		lb := log.blks[i]
		// install log destination in the first log block
		lh.w_logdest(i, lb.block)

		// and write block to the log
		logblkn := log.logstart + 1 + i
		bdev_write(logblkn, lb.data)
	}

	lh.w_recovernum(log.lhead)

	// commit log: flush log header
	bdev_write(log.logstart, &log.tmpblk)

	//rn := lh.recovernum()
	//if rn > 0 {
	//	runtime.Crash()
	//}

	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover
	for i := 0; i < log.lhead; i++ {
		lb := log.blks[i]
		bdev_write(lb.block, lb.data)
	}

	// success; clear flag indicating to recover from log
	lh.w_recovernum(0)
	bdev_write(log.logstart, &log.tmpblk)

	log.lhead = 0
}

func log_daemon(l *log_t) {
	// an upperbound on the number of blocks written per system call. this
	// is necessary in order to guarantee that the log is long enough for
	// the allowed number of concurrent fs syscalls.
	maxblkspersys := 10
	for {
		tickets := l.loglen / maxblkspersys
		adm := l.admission

		done := false
		given := 0
		t := 0
		waiters := 0
		for !done {
			select {
			case nb := <- l.incoming:
				if t <= 0 {
					panic("log write without admission")
				}
				l.addlog(nb)
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
			}
		}
		l.commit()
		// wake up waiters
		if waiters > 0 {
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

func log_write(b idebuf_t) {
	if memtime {
		return
	}
	fslog.incoming <- b
}

func bdev_write(blkn int, src *[512]uint8) {
	ider := idereq_new(blkn, true, src)
	ide_request <- ider
	<- ider.ack
}

func bdev_read(blkn int, dst *[512]uint8) {
	ider := idereq_new(blkn, false, dst)
	ide_request <- ider
	<- ider.ack
}
