package main

import "fmt"
import "runtime"
import "strings"
import "sync"

const NAME_MAX    int = 512

var superb_start	int
var superb		superblock_t
var iroot		*idaemon_t
var free_start		int
var free_len		int
var usable_start	int

// file system journal
var fslog	= log_t{}
var bcdaemon	= bcdaemon_t{}

// free block bitmap lock
var fblock	= sync.Mutex{}

func pathparts(path string) []string {
	sp := strings.Split(path, "/")
	nn := []string{}
	for _, s := range sp {
		if s != "" {
			nn = append(nn, s)
		}
	}
	return nn
}

func dirname(path string) ([]string, string) {
	parts := pathparts(path)
	l := len(parts) - 1
	if l < 0 {
		return nil, ""
	}
	dirs := parts[:l]
	name := parts[l]
	return dirs, name
}

func sdirname(path string) (string, string) {
	ds, fn := dirname(path)
	s := strings.Join(ds, "/")
	if path != "" && path[0] == '/' {
		s = "/" + s
	}
	return s, fn
}

func fs_init() *file_t {
	// we are now prepared to take disk interrupts
	irq_unmask(IRQ_DISK)
	go ide_daemon()

	iblkcache.blks = make(map[int]*ibuf_t, 30)

	bcdaemon.init()
	go bc_daemon(&bcdaemon)

	// find the first fs block; the build system installs it in block 0 for
	// us
	buffer := new([512]uint8)
	bdev_read(0, buffer)
	FSOFF := 506
	superb_start = readn(buffer[:], 4, FSOFF)
	if superb_start <= 0 {
		panic("bad superblock start")
	}
	bdev_read(superb_start, buffer)
	superb = superblock_t{buffer}
	ri := superb.rootinode()
	iroot = idaemon_ensure(ri)

	free_start = superb.freeblock()
	free_len = superb.freeblocklen()

	fblkcache.fblkc_init(free_start, free_len)

	logstart := free_start + free_len
	loglen := superb.loglen()
	usable_start = logstart + loglen
	if loglen < 0 || loglen > 63 {
		panic("bad log len")
	}

	fslog.init(logstart, loglen)
	fs_recover()
	go log_daemon(&fslog)

	return &file_t{ftype: INODE, priv: ri}
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

type memorium_t struct {
	idebuf	idebuf_t
	sync.Mutex
}
var memorium = []memorium_t{}
var memtime = false

func use_memfs() {
	startb := superb_start

	// 20MB disks
	fsblocks := 40960

	fmt.Printf("Populating memfs")
	memorium = make([]memorium_t, fsblocks)

	tpct := fsblocks/100
	buffer := new([512]uint8)
	for i := 1; i < fsblocks; i++ {
		bdev_read(startb + i, buffer)
		memorium[i].idebuf.disk = 0
		memorium[i].idebuf.block = startb + i
		memorium[i].idebuf.data = new([512]uint8)
		*memorium[i].idebuf.data = *buffer
		if (i + 1) % tpct == 0 {
			fmt.Printf(".")
		}
	}
	memtime = true
	fmt.Printf("\ndone! Not using disk for fs\n")
}

func memfsrw(b *idebuf_t, writing bool) {
	idx := b.block - superb_start
	if idx == 0 {
		panic("no superblock")
	}
	if writing {
		*memorium[idx].idebuf.data = *b.data
	} else {
		*b.data = *memorium[idx].idebuf.data
	}
}

// a type for an inode block/offset identifier
type inum int

// object for managing inode transactions. since rename may need to atomically
// operate on 3 arbitrary inodes, we need a way to lock the inodes but do it
// without deadlocking. the purpose of this class is to lock the inodes, but to
// use non-blocking acquires -- if an acquire fails, we release all the locks
// and retry. that way deadlock is avoided.

// the inputs to inodetx_t are 1) paths: the inodes that are NOT contained in
// another inode in this transaction. 2) childs: inodes that are contained in
// another inode in this transaction (like unlink: we must lock the parent dir,
// a path, and the target directory, the child).

// lockall() then resolves the given paths, increments the mem ref count on the
// paths so the idaemons don't terminate out from under us, and looks up the
// inum of all children. the inums are then sorted for the purpose of
// determining the correct locking order. then all the inodes are locked. every
// child has its inum verified by doing a lookup in the containing parent (the
// child inode may have been unlinked before we locked the parents). if all
// child inums are still correct, locking has succeeded. otherwise, we try
// again until we succeed.

// it is OK if multiples paths resolve to the same inode (this is possible with
// rename); lockall() will lock each distinct inode once. that way the
// transaction operations can be completely unaware of the number of distinct
// inodes involved in the transaction.

// to unlock, unlockall() simply sends the unlock to all locked inodes and
// finally decrements the mem ref count.

func noblksend(r *ireq_t, idm *idaemon_t) bool {
	select {
	case idm.req <- r:
		return true
	default:
		return false
	}
}

type inodetx_t struct {
	paths	map[string]*itxpath_t
	bypriv	map[inum]bool
	cwd	inum
}

type itxpath_t struct {
	mustexist	bool
	found		bool
	priv		inum
	lchan		chan *ireq_t
	child		bool
	par		*itxpath_t
	cpath		string
}

func (itx *inodetx_t) _pjoin(p, c string) string {
	if p == "" {
		return c
	} else {
		return p + "/" + c
	}
}

func (itx *inodetx_t) init(cwdpriv inum) {
	itx.cwd = cwdpriv
	itx.paths = make(map[string]*itxpath_t)
}

func (itx *inodetx_t) addpath(path string) {
	if _, ok := itx.paths[path]; ok {
		return
	}
	p := &itxpath_t{}
	p.mustexist = true
	itx.paths[path] = p
}

func (itx *inodetx_t) addchild(par string, childp string, mustexist bool) {
	if childp == "" {
		panic("child cannot be empty string")
	}
	path := itx._pjoin(par, childp)
	if ep, ok := itx.paths[path]; ok {
		old := ep.mustexist
		ep.mustexist = old || mustexist
	} else {
		pp, ok := itx.paths[par]
		if !ok {
			panic("no parent for child")
		}
		np := &itxpath_t{mustexist: mustexist, child: true,
		    par: pp, cpath: childp}
		itx.paths[path] = np
	}
}

func (itx *inodetx_t) childfound(par, child string) bool {
	path := itx._pjoin(par, child)
	if p, ok := itx.paths[path]; ok {
		return p.found
	}
	panic("no such path")
}

func (itx *inodetx_t) privforc(par, child string) inum {
	path := itx._pjoin(par, child)
	if p, ok := itx.paths[path]; ok {
		return p.priv
	}
	panic("no such path")
}

func (itx *inodetx_t) privforp(path string) inum {
	if p, ok := itx.paths[path]; ok {
		return p.priv
	}
	panic("no such path")
}

func (itx *inodetx_t) sendp(path string, req *ireq_t) {
	if p, ok := itx.paths[path]; ok {
		p.lchan <- req
		<- req.ack
		return
	}
	panic("no such path")
}

func (itx *inodetx_t) sendc(par, child string, req *ireq_t) {
	path := itx._pjoin(par, child)
	if p, ok := itx.paths[path]; ok {
		p.lchan <- req
		<- req.ack
		return
	}
	panic("no such path")
}

func (itx *inodetx_t) lockall() int {
	memdec := func(priv inum) {
		req := &ireq_t{}
		req.mkref_direct()
		idm := idaemon_ensure(priv)
		idm.req <- req
		<- req.ack
		if req.resp.err != 0 {
			panic("decref must succeed")
		}
	}

	var refs map[inum]bool
	itx.bypriv = refs
	dodecs := func() {
		for priv := range refs {
			memdec(priv)
		}
	}

	dounlocks := func() {
		req := &ireq_t{}
		req.mkunlock()
		for priv := range refs {
			var ch chan *ireq_t
			for _, ipath := range itx.paths {
				if ipath.priv != priv {
					continue
				}
				if ipath.lchan != nil {
					ch = ipath.lchan
					break
				}
			}
			if ch != nil {
				ch <- req
				<- req.ack
				if req.resp.err != 0 {
					panic("unlocked must succeed")
				}
			}
		}
	}

	req := &ireq_t{}
	fails := 0
	restart:
	for {
		// reset state
		refs = make(map[inum]bool)
		for _, ipath := range itx.paths {
			ipath.found = false
			ipath.lchan = nil
			ipath.priv = 0
		}

		// get memrefs
		for path, ipath := range itx.paths {
			req.mkget_meminc(path)
			req_namei(req, path, itx.cwd)
			<- req.ack
			err := req.resp.err
			if err != 0 {
				if !ipath.mustexist && err == -ENOENT {
					ipath.found = false
				} else {
					dodecs()
					return err
				}
			} else {
				ipath.found = true
			}
			if ipath.found {
				priv := req.resp.gnext
				if refs[priv] {
					memdec(priv)
				}
				refs[priv] = true
				ipath.found = true
				ipath.priv = priv
			}
		}

		// try all locks
		for priv := range refs {
			req.mklock()
			idm := idaemon_ensure(priv)
			failed := true
			// we've just sent the goroutine a memref request; give
			// it a chance to receive again. if we fail on our
			// first try to send instead of yielding after an
			// attempt, we often loop in here 500 times on qemu.
			for i := 0; i < 3; i++ {
				if noblksend(req, idm) {
					failed = false
					break
				}
				runtime.Gosched()
			}
			if failed {
				dounlocks()
				dodecs()
				fails++
				goto restart
			}
			<- req.ack
			if req.resp.err != 0 {
				panic("must succeed")
			}
			// find all paths with this priv and set lchan
			for _, ipath := range itx.paths {
				if ipath.priv != priv {
					continue
				}
				ipath.lchan = req.lock_lchan
			}
		}

		// make sure child existence is the same as what we observed
		// when getting memrefs
		for _, ipath := range itx.paths {
			if !ipath.child {
				continue
			}
			req.mklookup(ipath.cpath)
			if ipath.par.lchan == nil {
				panic("parent must be locked")
			}
			ipath.par.lchan <- req
			<- req.ack
			err := req.resp.err
			if err != -ENOENT && err != 0 {
				dounlocks()
				dodecs()
				return err
			}
			// child availability or inum changed?
			if (err == -ENOENT && ipath.found) ||
			   (err == 0 && !ipath.found) ||
			   (req.resp.gnext != ipath.priv) {
				dounlocks()
				dodecs()
				goto restart
			}
		}

		// we are done!
		itx.bypriv = refs
		if fails >= 3 {
			fmt.Printf("*** fails: %v\n", fails)
		}
		return 0
	}
}

func (itx *inodetx_t) unlockall() {
	// unlock, then send memdecs
	req := &ireq_t{}
	req.mkunlock()
	for priv := range itx.bypriv {
		var ch chan *ireq_t
		for _, ipath := range itx.paths {
			if ipath.priv != priv {
				continue
			}
			if ipath.lchan != nil {
				ch = ipath.lchan
				break
			}
		}
		if ch != nil {
			ch <- req
			<- req.ack
			if req.resp.err != 0 {
				panic("unlocked must succeed")
			}
		}
	}

	req.mkrefdec(true)
	for priv := range itx.bypriv {
		idmn := idaemon_ensure(priv)
		idmn.req <- req
		<- req.ack
		if req.resp.err != 0 {
			panic("decref must succeed")
		}
	}
}

func fs_link(old string, new string, cwdf *file_t) int {
	op_begin()
	defer op_end()

	cwd := cwdf.priv
	// first get inode number and inc ref count
	req := &ireq_t{}
	req.mkget_fsinc(old)
	req_namei(req, old, cwd)
	<- req.ack
	if req.resp.err != 0 {
		return req.resp.err
	}

	// insert directory entry
	priv := req.resp.gnext
	req.mkinsert(new, priv)
	req_namei(req, new, cwd)
	<- req.ack
	// decrement ref count if the insert failed
	if req.resp.err != 0 {
		req2 := &ireq_t{}
		req2.mkrefdec(false)
		idmon := idaemon_ensure(priv)
		idmon.req <- req2
		<- req2.ack
		if req2.resp.err != 0 {
			panic("must succeed")
		}
	}
	return req.resp.err
}

func fs_unlink(paths string, cwdf *file_t) int {
	dirs, fn := sdirname(paths)
	if fn == "." || fn == ".." {
		return -EPERM
	}

	op_begin()
	defer op_end()

	itx := inodetx_t{}
	itx.init(cwdf.priv)
	itx.addpath(dirs)
	itx.addchild(dirs, fn, true)

	err := itx.lockall()
	if err != 0 {
		return err
	}
	defer itx.unlockall()

	req := &ireq_t{}
	req.mkempty()
	itx.sendc(dirs, fn, req)
	if req.resp.err != 0 {
		return req.resp.err
	}

	req.mkunlink(fn)
	itx.sendp(dirs, req)
	if req.resp.err != 0 {
		panic("unlink must succeed")
	}
	req.mkrefdec(false)
	itx.sendc(dirs, fn, req)
	if req.resp.err != 0 {
		panic("fs ref dec must succeed")
	}
	return 0
}

func fs_rename(oldp, newp string, cwdf *file_t) int {
	odirs, ofn := sdirname(oldp)
	ndirs, nfn := sdirname(newp)

	if ofn == "" || nfn == "" {
		return -ENOENT
	}

	op_begin()
	defer op_end()

	itx := inodetx_t{}
	itx.init(cwdf.priv)
	itx.addpath(odirs)
	itx.addpath(ndirs)
	itx.addchild(odirs, ofn, true)
	itx.addchild(ndirs, nfn, false)

	err := itx.lockall()
	if err != 0 {
		return err
	}
	defer itx.unlockall()

	sendc := func(par, child string, req *ireq_t) {
		itx.sendc(par, child, req)
		if req.resp.err != 0 {
			panic("op must succed")
		}
	}

	sendp := func(par string, req *ireq_t) {
		itx.sendp(par, req)
		if req.resp.err != 0 {
			panic("op must succed")
		}
	}

	// is old a dir?
	ost := &stat_t{}
	ost.init()
	req := &ireq_t{}
	req.mkfstat(ost)
	sendc(odirs, ofn, req)
	odir := ost.mode() == I_DIR

	newexists := itx.childfound(ndirs, nfn)
	if newexists {
		// if src and dst are the same file, we are done
		if itx.privforc(odirs, ofn) == itx.privforc(ndirs, nfn) {
			return 0
		}

		// make sure old file and new file are both files, or both
		// directories
		nst := &stat_t{}
		nst.init()
		req.mkfstat(nst)
		sendc(ndirs, nfn, req)

		ndir := nst.mode() == I_DIR
		if odir && !ndir {
			return -ENOTDIR
		} else if !odir && ndir {
			return -EISDIR
		}

		// unlink existing new file
		req.mkempty()
		itx.sendc(ndirs, nfn, req)
		if req.resp.err != 0 {
			return req.resp.err
		}

		req.mkunlink(nfn)
		sendp(ndirs, req)

		req.mkrefdec(false)
		sendc(ndirs, nfn, req)
	}

	// insert new file
	opriv := itx.privforc(odirs, ofn)

	req.mkinsert(nfn, opriv)
	sendp(ndirs, req)

	req.mkunlink(ofn)
	sendp(odirs, req)

	// update '..'; orphaned loop not yet handled.
	if odir {
		req.mkunlink("..")
		sendc(odirs, ofn, req)
		ndpriv := itx.privforp(ndirs)
		req.mkinsert("..", ndpriv)
		sendc(odirs, ofn, req)
	}

	return 0
}

func fs_read(dst *userbuf_t, f *file_t, offset int) (int, int) {
	// send read request to inode daemon owning priv
	req := &ireq_t{}
	req.mkread(dst, offset)

	priv := f.priv
	idmon := idaemon_ensure(priv)
	idmon.req <- req
	<- req.ack
	if req.resp.err != 0 {
		return 0, req.resp.err
	}
	return req.resp.count, 0
}

func fs_write(src *userbuf_t, f *file_t, offset int, append bool) (int, int) {
	op_begin()
	defer op_end()

	priv := f.priv
	// send write request to inode daemon owning priv
	req := &ireq_t{}
	req.mkwrite(src, offset, append)

	idmon := idaemon_ensure(priv)
	idmon.req <- req
	<- req.ack
	if req.resp.err != 0 {
		return 0, req.resp.err
	}
	return req.resp.count, 0
}

func fs_mkdir(paths string, mode int, cwdf *file_t) int {
	op_begin()
	defer op_end()

	dirs, fn := sdirname(paths)
	if len(fn) > DNAMELEN {
		return -ENAMETOOLONG
	}

	// atomically create new dir with insert of "." and ".."
	itx := inodetx_t{}
	itx.init(cwdf.priv)
	itx.addpath(dirs)

	err := itx.lockall()
	if err != 0 {
		return err
	}
	defer itx.unlockall()

	req := &ireq_t{}
	req.mkcreate_dir(fn)
	itx.sendp(dirs, req)
	if req.resp.err != 0 {
		return req.resp.err
	}
	priv := req.resp.cnext

	idm := idaemon_ensure(priv)
	req.mkinsert(".", priv)
	idm.req <- req
	<- req.ack
	if req.resp.err != 0 {
		panic("insert must succeed")
	}
	parpriv := itx.privforp(dirs)
	req.mkinsert("..", parpriv)
	idm.req <- req
	<- req.ack
	if req.resp.err != 0 {
		panic("insert must succeed")
	}
	return 0
}

func fs_open(paths string, flags int, mode int, cwdf *file_t,
    major, minor int) (*file_t, int) {
	fnew := func(priv inum, maj, min int) *file_t {
		r := &file_t{}
		r.ftype = INODE
		// XXX should use type
		if maj != 0 || min != 0 {
			r.ftype = DEV
		}
		r.priv = priv
		r.dev.major = maj
		r.dev.minor = min
		return r
	}

	trunc := flags & O_TRUNC != 0
	creat := flags & O_CREAT != 0
	nodir := false
	// open with O_TRUNC is not read-only
	if trunc || creat {
		op_begin()
		defer op_end()
	}
	var idm *idaemon_t
	priv := inum(-1)
	var maj int
	var min int
	if creat {
		nodir = true
		// creat; must atomically create and open the new file.
		isdev := major != 0 || minor != 0

		// must specify at least one path component
		dirs, fn := sdirname(paths)
		if fn == "" {
			return nil, -EISDIR
		}

		req := &ireq_t{}
		if isdev {
			req.mkcreate_nod(fn, major, minor)
		} else {
			req.mkcreate_file(fn)
		}
		if len(req.cr_name) > DNAMELEN {
			return nil, -ENAMETOOLONG
		}

		// lock containing directory
		itx := inodetx_t{}
		itx.init(cwdf.priv)
		itx.addpath(dirs)

		err := itx.lockall()
		if err != 0 {
			return nil, err
		}
		// create new file
		itx.sendp(dirs, req)

		exists := req.resp.err == -EEXIST
		oexcl := flags & O_EXCL != 0

		priv = req.resp.cnext
		if exists {
			idm = idaemon_ensure(priv)
			if oexcl || isdev {
				itx.unlockall()
				return nil, req.resp.err
			}
			// determine major/minor of existing file
			st := &stat_t{}
			st.init()
			req := &ireq_t{}
			req.mkfstat(st)
			idm.req <- req
			<- req.ack
			if req.resp.err != 0 {
				panic("must succeed")
			}
			maj, min = unmkdev(st.rdev())
		} else {
			if req.resp.err != 0 {
				itx.unlockall()
				return nil, req.resp.err
			}
			idm = idaemon_ensure(priv)
			maj = major
			min = minor
		}

		req.mkref_direct()
		idm.req <- req
		<- req.ack
		if req.resp.err != 0 {
			panic("mem ref inc must succeed")
		}
		// unlock after opening to make sure we open the file that we
		// just created (otherwise O_EXCL is broken)
		itx.unlockall()
	} else {
		// open existing file
		req := &ireq_t{}
		req.mkget_meminc(paths)
		req_namei(req, paths, cwdf.priv)
		<- req.ack
		if req.resp.err != 0 {
			return nil, req.resp.err
		}
		priv = req.resp.gnext
		maj = req.resp.major
		min = req.resp.minor
		idm = idaemon_ensure(priv)
	}

	o_dir := flags & O_DIRECTORY != 0
	wantwrite := flags & (O_WRONLY|O_RDWR) != 0
	if wantwrite {
		nodir = true
	}

	// verify flags: dir cannot be opened with write perms and only dir can
	// be opened with O_DIRECTORY
	if o_dir || nodir {
		memdec := func() {
			req := &ireq_t{}
			req.mkrefdec(true)
			idm.req <- req
			<- req.ack
			if req.resp.err != 0 {
				panic("mem ref dec must succeed")
			}
		}
		st := &stat_t{}
		st.init()
		req := &ireq_t{}
		req.mkfstat(st)
		idm.req <- req
		<- req.ack
		if req.resp.err != 0 {
			panic("stat must succeed")
		}
		itype := st.mode()
		if o_dir && itype != I_DIR {
			memdec()
			return nil, -ENOTDIR
		}
		if nodir && itype == I_DIR {
			memdec()
			return nil, -EISDIR
		}
	}

	if nodir && trunc {
		req := &ireq_t{}
		req.mktrunc()
		idm.req <- req
		<- req.ack
		if req.resp.err != 0 {
			panic("trunc must succeed")
		}
	}

	ret := fnew(priv, maj, min)
	return ret, 0
}

// XXX log those files that have no fs links but > 0 memory references to the
// journal so that if we crash before freeing its blocks, the blocks can be
// reclaimed.
func fs_close(f *file_t, perms int) int {
	op_begin()
	defer op_end()

	req := &ireq_t{}
	req.mkrefdec(true)
	idmon := idaemon_ensure(f.priv)
	idmon.req <- req
	<- req.ack
	return req.resp.err
}

func fs_memref(f *file_t, perms int) {
	idmon := idaemon_ensure(f.priv)
	req := &ireq_t{}
	req.mkref_direct()
	idmon.req <- req
	<- req.ack
	if req.resp.err != 0 {
		panic("mem ref increment of open file failed")
	}
}

func fs_fstat(f *file_t, st *stat_t) int {
	priv := f.priv
	idmon := idaemon_ensure(priv)
	req := &ireq_t{}
	req.mkfstat(st)
	idmon.req <- req
	<- req.ack
	return req.resp.err
}

func fs_stat(path string, st *stat_t, cwdf *file_t) int {
	req := &ireq_t{}
	req.mkstat(path, st)
	req_namei(req, path, cwdf.priv)
	<- req.ack
	return req.resp.err
}

// sends a request requiring a lookup to the correct idaemon: if path is a
// relative path, lookups should start with the idaemon for cwd. if the path is
// absolute, lookups should start with iroot.
func req_namei(req *ireq_t, paths string, cwd inum) {
	dest := iroot
	if len(paths) == 0 || paths[0] != '/' {
		dest = idaemon_ensure(cwd)
	}
	dest.req <- req
}

type pgcache_t struct {
	pgs	[]*[PGSIZE]uint8
	phys	[]int
}

type idaemon_t struct {
	req		chan *ireq_t
	priv		inum
	blkno		int
	ioff		int
	// cache of file data
	pgcache		pgcache_t
	// cache of inode metadata
	icache		icache_t
	memref		int
	// postponed ".." reqs
	_ppdots		[]*ireq_t
	ppdots		[]*ireq_t
}

var allidmons	= map[inum]*idaemon_t{}
var idmonl	= sync.Mutex{}

func idaemon_ensure(priv inum) *idaemon_t {
	if priv <= 0 {
		panic("non-positive priv")
	}
	idmonl.Lock()
	ret, ok := allidmons[priv]
	if !ok {
		ret = &idaemon_t{}
		ret.idm_init(priv)
		allidmons[priv] = ret
		idaemonize(ret)
	}
	idmonl.Unlock()
	return ret
}

// creates a new idaemon struct and fills its inode
func (idm *idaemon_t) idm_init(priv inum) {
	blkno, ioff := bidecode(int(priv))
	idm.priv = priv
	idm.blkno = blkno
	idm.ioff = ioff

	idm.req = make(chan *ireq_t)
	idm._ppdots = make([]*ireq_t, 0, 2)
	idm.ppdots = idm._ppdots

	blk := ibread(blkno)
	idm.icache.fill(blk, ioff)
	if idm.icache.itype == I_DIR {
		ds := idm.all_dirents()
		idm.icache.cache_dirents(ds)
		idm.icache.dirent_brelse(ds)
	}
	ibrelse(blk)
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
	dur := &bbuf_t{}
	dur.buf = &idebuf_t{}
	dur.buf.block = ib.block
	dur.buf.data = ib.data
	log_write(dur)
}

var iblkcache = iblkcache_t{}

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
		if iblk.block != blkn {
			panic("wtf")
		}
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
	if _, ok := iblkcache.blks[blkn]; ok {
		panic("iblk exists")
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

// struct to hold the block/offset of free directory entry slots
type icdent_t struct {
	blkno	int
	slot	int
	priv	inum
}

// metadata block cache; only one idaemon touches these blocks at a time, thus
// no concurrency control
type mdbuf_t struct {
	block	int
	data	[512]uint8
}

func (mb *mdbuf_t) log_write() {
	dur := &bbuf_t{}
	dur.buf = &idebuf_t{}
	dur.buf.block = mb.block
	dur.buf.data = &mb.data
	log_write(dur)
}

func (ic *icache_t) mbread(blockn int) *mdbuf_t {
	mb, ok := ic.metablks[blockn]
	if !ok {
		mb = &mdbuf_t{}
		mb.block = blockn
		bdev_read(blockn, &mb.data)
		ic.metablks[blockn] = mb
	}
	return mb
}

func (ic *icache_t) mbrelse(mb *mdbuf_t) {
}

func (ic *icache_t) dirent_brelse(ds []dirdata_t) {
	for _, d := range ds {
		ic.mbrelse(d.blk)
	}
}

func (ic *icache_t) fill(blk *ibuf_t, ioff int) {
	inode := inode_t{blk, ioff}
	ic.itype = inode.itype()
	if ic.itype <= I_FIRST || ic.itype > I_LAST {
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

func (ic *icache_t) cache_dirents(ds []dirdata_t) {
	ic.dents = make(map[string]icdent_t)
	ic._free_dents = make([]icdent_t, 0, NDIRENTS)
	ic.free_dents = ic._free_dents
	for i := range ds {
		for j := 0; j < NDIRENTS; j++ {
			fn := ds[i].filename(j)
			priv := ds[i].inodenext(j)
			fde := icdent_t{int(ds[i].blk.block), j, priv}
			if fn == "" {
				ic.free_dents = append(ic.free_dents, fde)
			} else {
				ic.dents[fn] = fde
			}
		}
	}
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

type rtype_t int

const (
	GET	rtype_t = iota
	READ
	REFDEC
	WRITE
	CREATE
	INSERT
	LINK
	UNLINK
	STAT
	LOCK
	UNLOCK
	LOOKUP
	EMPTY
	TRUNC
)

type ireq_t struct {
	ack		chan bool
	// request type
	rtype		rtype_t
	// tree traversal
	path		[]string
	// read/write op
	offset		int
	// read op
	rbuf		*userbuf_t
	// writes op
	dbuf		*userbuf_t
	dappend		bool
	// create op
	cr_name		string
	cr_type		int
	cr_mkdir	bool
	cr_major	int
	cr_minor	int
	// insert op
	insert_name	string
	insert_priv	inum
	// inc ref count after get
	fsref		bool
	memref		bool
	// stat op
	stat_st		*stat_t
	// lock
	lock_lchan	chan *ireq_t
	resp		iresp_t
}

type iresp_t struct {
	gnext		inum
	cnext		inum
	count		int
	err		int
	major		int
	minor		int
}

// if incref is true, the call must be made between op_{begin,end}.
func (r *ireq_t) mkget_fsinc(name string) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = GET
	r.path = pathparts(name)
	r.fsref = true
	r.memref = false
}

func (r *ireq_t) mkget_meminc(name string) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = GET
	r.path = pathparts(name)
	r.fsref = false
	r.memref = true
}

func (r *ireq_t) mkref_direct() {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = GET
	r.fsref = false
	r.memref = true
}

func (r *ireq_t) mkread(dst *userbuf_t, offset int) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = READ
	r.rbuf = dst
	r.offset = offset
}

func (r *ireq_t) mkwrite(src *userbuf_t, offset int, append bool) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = WRITE
	r.dbuf = src
	r.offset = offset
	r.dappend = append
}

func (r *ireq_t) mkcreate_dir(path string) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = CREATE
	dirs, name := dirname(path)
	if len(dirs) != 0 {
		panic("no lookup with create now")
	}
	r.cr_name = name
	r.cr_type = I_DIR
	r.cr_mkdir = true
}

func (r *ireq_t) mkcreate_file(path string) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = CREATE
	dirs, name := dirname(path)
	if len(dirs) != 0 {
		panic("no lookup with create now")
	}
	r.cr_name = name
	r.cr_type = I_FILE
	r.cr_mkdir = false
}

func (r *ireq_t) mkcreate_nod(path string, maj, min int) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = CREATE
	dirs, name := dirname(path)
	if len(dirs) != 0 {
		panic("no lookup with create now")
	}
	r.cr_name = name
	r.cr_type = I_DEV
	r.cr_mkdir = false
	r.cr_major = maj
	r.cr_minor = min
}

func (r *ireq_t) mkinsert(path string, priv inum) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = INSERT
	dirs, name := dirname(path)
	r.path = dirs
	r.insert_name = name
	r.insert_priv = priv
}

func (r *ireq_t) mkrefdec(memref bool) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = REFDEC
	r.fsref = false
	r.memref = false
	if memref {
		r.memref = true
	} else {
		r.fsref = true
	}
}

func (r *ireq_t) mkunlink(path string) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = UNLINK
	dirs, fn := dirname(path)
	if len(dirs) != 0 {
		panic("no lookup with unlink now")
	}
	r.path = []string{fn}
}

func (r *ireq_t) mkfstat(st *stat_t) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = STAT
	r.stat_st = st
}

func (r *ireq_t) mkstat(path string, st *stat_t) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = STAT
	r.stat_st = st
	r.path = pathparts(path)
}

func (r *ireq_t) mklock() {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = LOCK
	r.lock_lchan = make(chan *ireq_t)
}

func (r *ireq_t) mkunlock() {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = UNLOCK
}

func (r *ireq_t) mklookup(path string) {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = LOOKUP
	dn, fn := dirname(path)
	if len(dn) != 0 {
		panic("dirname given to lookup")
	}
	r.path = []string{fn}
}

func (r *ireq_t) mkempty() {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = EMPTY
}

func (r *ireq_t) mktrunc() {
	var z ireq_t
	*r = z
	r.ack = make(chan bool)
	r.rtype = TRUNC
}

func (idm *idaemon_t) _dotsadd(r *ireq_t, pidm *idaemon_t) {
	// r has ".." removed from path
	// try to send to parent immediately. if it would block, postpone.
	if !noblksend(r, pidm) {
		idm.ppdots = append(idm.ppdots, r)
	}
}

// while waiting for a new ireq, try to send postponed ".." ireqs to parent
func (idm *idaemon_t) nextreq(rin chan *ireq_t) *ireq_t {
	// don't send postponded dotdots if locked
	if len(idm.ppdots) != 0 && rin == idm.req {
		priv, err := idm.iget("..")
		if err != 0 {
			panic("must succeed")
		}
		pidm := idaemon_ensure(priv)
		for len(idm.ppdots) != 0 {
			pp := idm.ppdots[0]
			select {
			case r := <- rin:
				return r
			case pidm.req <- pp:
				idm.ppdots = idm.ppdots[1:]
				if len(idm.ppdots) == 0 {
					idm.ppdots = idm._ppdots
				}
			}
		}
	}

	return <- rin
}

// returns true if the request is for the caller. if an error occurs, writes
// the error back to the requester.
func (idm *idaemon_t) forwardreq(r *ireq_t) bool {
	// skip "."
	var next string
	for {
		if len(r.path) == 0 {
			// req is for this idaemon
			return true
		}

		next = r.path[0]
		r.path = r.path[1:]
		// iroot's ".." is special: it is just like "."
		if next != "."  && (next != ".." || idm != iroot) {
			break
		}
	}
	npriv, err := idm.iget(next)
	if err != 0 {
		r.resp.err = err
		r.ack <- true
		return false
	}
	nextidm := idaemon_ensure(npriv)
	// if necessary, postpone ".." to avoid deadlock.
	if next == ".." {
		idm._dotsadd(r, nextidm)
	} else {
		nextidm.req <- r
	}
	return false
}

func idaemonize(idm *idaemon_t) {
	iupdate := func() {
		iblk := ibread(idm.blkno)
		if idm.icache.flushto(iblk, idm.ioff) {
			iblk.log_write()
		}
		ibrelse(iblk)
	}
	irefdown := func(memref bool) bool {
		if memref {
			idm.memref--
		} else {
			idm.icache.links--
		}
		if idm.icache.links < 0 || idm.memref < 0 {
			panic("negative links")
		}
		// XXX if links == 0 and we are a directory, remove ".."
		if idm.icache.links != 0 || idm.memref != 0 {
			if !memref {
				iupdate()
			}
			return false
		}
		// fail postponed dotdots
		for _, pp := range idm.ppdots {
			pp.resp.err = -ENOENT
			pp.ack <- true
		}
		idm.ifree()
		idmonl.Lock()
		delete(allidmons, idm.priv)
		idmonl.Unlock()
		return true
	}

	// simple operations
	go func() {
	ch := idm.req
	for {
		r := idm.nextreq(ch)
		switch r.rtype {
		case CREATE:
			if forme := idm.forwardreq(r); !forme {
				break
			}

			if r.cr_type != I_FILE && r.cr_type != I_DIR &&
			   r.cr_type != I_DEV {
				panic("no imp")
			}

			if idm.icache.itype != I_DIR {
				r.resp.err = -ENOTDIR
				r.ack <- true
				break
			}

			itype := r.cr_type
			maj := r.cr_major
			min := r.cr_minor
			cnext, err := idm.icreate(r.cr_name, itype, maj, min)
			iupdate()

			r.resp.cnext = cnext
			r.resp.err = err
			r.resp.major = maj
			r.resp.minor = min
			r.ack <- true

		case GET:
			// is the requester asking for this daemon?
			if forme := idm.forwardreq(r); !forme {
				break
			}

			// not open, just increase links
			if r.fsref {
				// no hard links on directories
				if idm.icache.itype != I_FILE {
					//r.ack <- &iresp_t{err: -EPERM}
					r.resp.err = -EPERM
					r.ack <- true
					break
				}
				idm.icache.links++
				iupdate()
			} else if r.memref {
				idm.memref++
			}

			// req is for us
			maj := idm.icache.major
			min := idm.icache.minor
			r.resp.gnext = idm.priv
			r.resp.major = maj
			r.resp.minor = min
			r.resp.err = 0
			r.ack <- true

		case INSERT:
			// create new dir ent with given inode number
			if forme := idm.forwardreq(r); !forme {
				break
			}
			err := idm.iinsert(r.insert_name, r.insert_priv)
			r.resp.err = err
			iupdate()
			//r.ack <- resp
			r.ack <- true

		case READ:
			read, err := idm.iread(r.rbuf, r.offset)
			r.resp.count = read
			r.resp.err = err
			r.ack <- true

		case REFDEC:
			// decrement reference count
			terminate := irefdown(r.memref)
			r.resp.err = 0
			r.ack <- true
			if terminate {
				return
			}

		case STAT:
			if forme := idm.forwardreq(r); !forme {
				break
			}
			st := r.stat_st
			st.wdev(0)
			st.wino(int(idm.priv))
			st.wmode(idm.mkmode())
			st.wsize(idm.icache.size)
			st.wrdev(mkdev(idm.icache.major, idm.icache.minor))
			r.resp.err = 0
			r.ack <- true

		case UNLINK:
			name := r.path[0]
			_, err := idm.iunlink(name)
			if err == 0 {
				iupdate()
			}
			r.resp.err = err
			r.ack <- true

		case WRITE:
			if idm.icache.itype == I_DIR {
				panic("write to dir")
			}
			offset := r.offset
			if r.dappend {
				offset = idm.icache.size
			}
			wrote, err := idm.iwrite(r.dbuf, offset)
			// iupdate() must come before the response is sent.
			// otherwise the idaemon and the requester race to
			// log_write()/op_end().
			iupdate()
			r.resp.count = wrote
			r.resp.err = err
			r.ack <- true

		// new uops
		case LOCK:
			if r.lock_lchan == nil {
				panic("lock with nil lock chan")
			}
			if ch != idm.req {
				panic("double lock")
			}
			ch = r.lock_lchan
			r.resp.err = 0
			r.ack <- true

		case UNLOCK:
			if ch == idm.req {
				panic("already unlocked")
			}
			ch = idm.req
			r.resp.err = 0
			r.ack <- true

		case LOOKUP:
			if len(r.path) != 1 {
				panic("path for lookup should be one component")
			}
			name := r.path[0]
			npriv, err := idm.iget(name)
			r.resp.gnext = npriv
			r.resp.err = err
			r.ack <- true

		case EMPTY:
			err := 0
			if idm.icache.itype == I_DIR && !idm.idirempty() {
				err = -ENOTEMPTY
			}
			r.resp.err = err
			r.ack <- true

		case TRUNC:
			if idm.icache.itype != I_FILE &&
			   idm.icache.itype != I_DEV {
				panic("bad truncate")
			}
			idm.icache.size = 0
			iupdate()
			r.resp.err = 0
			r.ack <- true

		default:
			panic("bad req type")
		}
	}}()
}

// takes as input the file offset and whether the operation is a write and
// returns the block number of the block responsible for that offset.
func (idm *idaemon_t) offsetblk(offset int, writing bool) int {
	zeroblock := func(blkno int) {
		// zero block
		zblk := idm.icache.mbread(blkno)
		for i := range zblk.data {
			zblk.data[i] = 0
		}
		zblk.log_write()
		idm.icache.mbrelse(zblk)
	}
	ensureb := func(blkno int, dozero bool) (int, bool) {
		if !writing || blkno != 0 {
			return blkno, false
		}
		nblkno := balloc()
		if dozero {
			zeroblock(nblkno)
		}
		return nblkno, true
	}
	// this function makes sure that every slot up to and including idx of
	// the given indirect block has a block allocated. it is necessary to
	// allocate a bunch of zero'd blocks for a file when someone lseek()s
	// past the end of the file but writes later.
	ensureind := func(blk *mdbuf_t, idx int) {
		if !writing {
			return
		}
		added := false
		for i := 0; i <= idx; i++ {
			slot := i * 8
			s := blk.data[:]
			blkn := readn(s, 8, slot)
			blkn, isnew := ensureb(blkn, true)
			if isnew {
				writen(s, 8, slot, blkn)
				added = true
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
			// idaemon_t.iupdate() will make sure the
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
		indno, isnew := ensureb(indno, true)
		if isnew {
			idm.icache.indir = indno
		}
		// follow indirect block chain
		indblk := idm.icache.mbread(indno)
		for i := 0; i < indslot/slotpb; i++ {
			// make sure the indirect block has no empty spaces if
			// we are writing past the end of the file
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
		blkn, isnew = ensureb(blkn, false)
		if isnew {
			writen(s, 8, noff, blkn)
			indblk.log_write()
		}
		idm.icache.mbrelse(indblk)
	} else {
		blkn = idm.icache.addrs[whichblk]
	}
	return blkn
}

// if writing, allocate a block if necessary and don't trim the slice to the
// size of the file
func (idm *idaemon_t) blkslice(offset int, writing bool) ([]uint8, *mdbuf_t) {
	blkn := idm.offsetblk(offset, writing)
	if blkn < superb_start && idm.icache.size > 0 {
		panic("bad block")
	}
	blk := idm.icache.mbread(blkn)
	start := offset % 512
	bsp := 512 - start
	if !writing {
		// trim src buffer to end of file
		left := idm.icache.size - offset
		if bsp > left {
			bsp = left
		}
	}
	src := blk.data[start:start+bsp]
	return src, blk
}

func (idm *idaemon_t) iread(dst *userbuf_t, offset int) (int, int) {
	return idm.iread1(dst, offset)
}

func (idm *idaemon_t) iread1(dst *userbuf_t, offset int) (int, int) {
	isz := idm.icache.size
	if offset >= isz {
		return 0, 0
	}

	c := 0
	for offset + c < isz && dst.remain() != 0 {
		src, blk := idm.blkslice(offset + c, false)
		wrote, err := dst.write(src)
		idm.icache.mbrelse(blk)
		c += wrote
		if err != 0 {
			return c, err
		}
	}
	return c, 0
}

func (idm *idaemon_t) iwrite(src *userbuf_t, offset int) (int, int) {
	return idm.iwrite1(src, offset)
}

func (idm *idaemon_t) iwrite1(src *userbuf_t, offset int) (int, int) {
	// XXX if file length is shortened, when to free old blocks?
	sz := src.len
	c := 0
	for c < sz {
		dst, blk := idm.blkslice(offset + c, true)
		read, err := src.read(dst)
		blk.log_write()
		idm.icache.mbrelse(blk)
		c += read
		if err != 0 {
			return c, err
		}
	}
	newsz := offset + c
	if newsz > idm.icache.size {
		idm.icache.size = newsz
	}
	return c, 0
}

func (idm *idaemon_t) dirent_add(name string, nblkno int, ioff int) {
	if _, ok := idm.icache.dents[name]; ok {
		panic("dirent already exists")
	}
	var ddata dirdata_t
	blkno, deoff := idm.dirent_getempty()
	ddata.blk = idm.icache.mbread(blkno)
	// write dir entry
	if ddata.filename(deoff) != "" {
		panic("dir entry slot is not free")
	}
	ddata.w_filename(deoff, name)
	ddata.w_inodenext(deoff, nblkno, ioff)
	ddata.blk.log_write()
	de := icdent_t{blkno, deoff, inum(biencode(nblkno, ioff))}
	idm.icache.dents[name] = de
	idm.icache.mbrelse(ddata.blk)
}

func (idm *idaemon_t) icreate(name string, nitype, major,
    minor int) (inum, int) {
	if nitype <= I_INVALID || nitype > I_LAST {
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

func (idm *idaemon_t) iget(name string) (inum, int) {
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
func (idm *idaemon_t) iinsert(name string, priv inum) int {
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
func (idm *idaemon_t) iunlink(name string) (inum, int) {
	de, ok := idm.icache.dents[name]
	if !ok {
		return 0, -ENOENT
	}
	blk := idm.icache.mbread(de.blkno)
	dirdata := dirdata_t{blk}
	dirdata.w_filename(de.slot, "")
	dirdata.w_inodenext(de.slot, 0, 0)
	blk.log_write()
	idm.icache.mbrelse(blk)
	// add back to free dents
	delete(idm.icache.dents, name)
	icd := icdent_t{blkno: de.blkno, slot: de.slot}
	idm.icache.free_dents = append(idm.icache.free_dents, icd)
	return de.priv, 0
}

// returns true if the inode has no directory entries
func (idm *idaemon_t) idirempty() bool {
	ds := idm.all_dirents()
	defer idm.icache.dirent_brelse(ds)

	canrem := func(s string) bool {
		if s == "" || s == ".." || s == "." {
			return false
		}
		return true
	}
	empty := true
	for i := 0; i < len(ds); i++ {
		for j := 0; j < NDIRENTS; j++ {
			if canrem(ds[i].filename(j)) {
				empty = false
			}
		}
	}
	return empty
}

// frees all blocks occupied by idm
func (idm *idaemon_t) ifree() {
	allb := make([]int, 0)
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
			if icache.itype() != I_INVALID {
				ret = false
				break
			}
		}
		return ret
	}
	iblk := ibread(idm.blkno)
	idm.icache.itype = I_INVALID
	idm.icache.flushto(iblk, idm.ioff)
	if alldead(iblk) {
		add(idm.blkno)
		ibpurge(iblk)
	} else {
		iblk.log_write()
	}
	ibrelse(iblk)

	for _, blkno := range allb {
		bfree(blkno)
	}
}

// returns a slice of all directory data blocks. caller must brelse all
// underlying blocks.
func (idm *idaemon_t) all_dirents() ([]dirdata_t) {
	if idm.icache.itype != I_DIR {
		panic("not a directory")
	}
	isz := idm.icache.size
	ret := make([]dirdata_t, 0, 10)
	for i := 0; i < isz; i += 512 {
		_, blk := idm.blkslice(i, false)
		dirdata := dirdata_t{blk}
		ret = append(ret, dirdata)
	}
	return ret
}

// returns an empty directory entry
func (idm *idaemon_t) dirent_getempty() (int, int) {
	frd := idm.icache.free_dents
	if len(frd) > 0 {
		ret := frd[0]
		idm.icache.free_dents = frd[1:]
		if len(idm.icache.free_dents) == 0 {
			idm.icache.free_dents = idm.icache._free_dents
		}
		return ret.blkno, ret.slot
	}

	// current dir blocks are full -- allocate new dirdata block
	blkno := idm.offsetblk(idm.icache.size, true)
	idm.icache.size += 512
	blk := idm.icache.mbread(blkno)
	var zbuf [512]uint8
	blk.data = zbuf
	blk.log_write()
	// NDIRENTS - 1 because we return slot 0 directly
	idm.icache.free_dents = make([]icdent_t, NDIRENTS - 1)
	for i := 0; i < NDIRENTS - 1; i++ {
		fde := icdent_t{blkno, i + 1, 0}
		idm.icache.free_dents[i] = fde
	}
	idm.icache.mbrelse(blk)
	return blkno, 0
}

// used for {,f}stat
func (idm *idaemon_t) mkmode() int {
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
	I_LAST = I_DEV

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
	blk	*mdbuf_t
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
	sl := dir.blk.data[st : st + DNAMELEN]
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
	v := readn(dir.blk.data[:], 8, st)
	return inum(v)
}

func (dir *dirdata_t) w_filename(didx int, fn string) {
	st := doffset(didx, 0)
	sl := dir.blk.data[st : st + DNAMELEN]
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
	writen(dir.blk.data[:], 8, st, v)
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
	for i := 0; i < flen && !found; i++ {
		if blk != nil {
			fbrelse(blk)
		}
		blk = fbread(fst + i)
		for idx, c := range blk.data {
			if c != 0xff {
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
		if memtime {
			memfsrw(req.buf, writing)
			req.ack <- true
			continue
		}
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

type bbuf_t struct {
	buf	*idebuf_t
	dirty	bool
}

func (b *bbuf_t) writeback() {
	ireq := idereq_new(b.buf.block, true, b.buf.data)
	ide_request <- ireq
	<- ireq.ack
	b.dirty = false
}

type bcreq_t struct {
	blkno	int
	ack	chan *bbuf_t
}

func bcreq_new(blkno int) *bcreq_t {
	return &bcreq_t{blkno, make(chan *bbuf_t)}
}

type bcdaemon_t struct {
	req		chan *bcreq_t
	bnew		chan *bbuf_t
	done		chan int
	blocks		map[int]*bbuf_t
	given		map[int]bool
	waiters		map[int][]*chan *bbuf_t
}

func (blc *bcdaemon_t) init() {
	blc.req = make(chan *bcreq_t)
	blc.bnew = make(chan *bbuf_t)
	blc.done = make(chan int)
	blc.blocks = make(map[int]*bbuf_t)
	blc.given = make(map[int]bool)
	blc.waiters = make(map[int][]*chan *bbuf_t)
}

// returns a bbuf_t for the specified block
func (blc *bcdaemon_t) bc_read(blkno int, ack *chan *bbuf_t) {
	ret, ok := blc.blocks[blkno]
	if !ok {
		// add requester to queue before kicking off disk read
		blc.qadd(blkno, ack)
		go func() {
			data := new([512]uint8)
			ireq := idereq_new(blkno, false, data)
			ide_request <- ireq
			<- ireq.ack
			nb := &bbuf_t{}
			nb.buf = ireq.buf
			blc.bnew <- nb
		}()
		return
	}
	*ack <- ret
}

func (blc *bcdaemon_t) qadd(blkno int, ack *chan *bbuf_t) {
	q, ok := blc.waiters[blkno]
	if !ok {
		q = make([]*chan *bbuf_t, 1, 8)
		blc.waiters[blkno] = q
		q[0] = ack
		return
	}
	blc.waiters[blkno] = append(q, ack)
}

func (blc *bcdaemon_t) qpop(blkno int) (*chan *bbuf_t, bool) {
	q, ok := blc.waiters[blkno]
	if !ok || len(q) == 0 {
		return nil, false
	}
	ret := q[0]
	blc.waiters[blkno] = q[1:]
	return ret, true
}

func bc_daemon(blc *bcdaemon_t) {
	for {
		select {
		case r := <- blc.req:
			// block request
			if r.blkno < 0 {
				panic("bad block")
			}
			inuse := blc.given[r.blkno]
			if !inuse {
				blc.given[r.blkno] = true
				blc.bc_read(r.blkno, &r.ack)
			} else {
				blc.qadd(r.blkno, &r.ack)
			}
		case bfin := <- blc.done:
			// relinquish block
			if bfin < 0 {
				panic("bad block")
			}
			inuse := blc.given[bfin]
			if !inuse {
				panic("relinquish unused block")
			}
			nextc, ok := blc.qpop(bfin)
			if ok {
				// busy blocks cannot be evicted
				blk, _ := blc.blocks[bfin]
				*nextc <- blk
			} else {
				blc.given[bfin] = false
			}
		case nb := <- blc.bnew:
			// disk read finished
			blkno := nb.buf.block
			if _, ok := blc.given[blkno]; !ok {
				panic("bllkno trimmed by cast")
			}
			blc.chk_evict()
			blc.blocks[blkno] = nb
			nextc, _ := blc.qpop(blkno)
			*nextc <- nb
		}
	}
}

func (blc *bcdaemon_t) chk_evict() {
	nbcbufs := 1024
	// how many buffers to evict
	// XXX LRU
	evictn := 2
	if len(blc.blocks) <= nbcbufs {
		return
	}
	// evict unmodified bbuf
	for i, bb := range blc.blocks {
		if !bb.dirty && !blc.given[i] {
			delete(blc.blocks, i)
			evictn--
			if evictn == 0 && len(blc.blocks) <= nbcbufs {
				break
			}
		}
	}
	if len(blc.blocks) > nbcbufs {
		panic("blc full")
	}
}

func membread(blkno int) *bbuf_t {
	idx := blkno - superb_start
	m := &memorium[idx]
	m.Lock()
	b := &bbuf_t{}
	b.buf = &m.idebuf
	b.dirty = true
	return b
}

func membrelse(b *bbuf_t) {
	idx := int(b.buf.block) - superb_start
	m := &memorium[idx]
	m.Unlock()
	return
}

func bread(blkno int) *bbuf_t {
	panic("no")
	if memtime {
		return membread(blkno)
	}
	req := bcreq_new(blkno)
	bcdaemon.req <- req
	return <- req.ack
}

func brelse(b *bbuf_t) {
	panic("no")
	if memtime {
		membrelse(b)
		return
	}
	bcdaemon.done <- int(b.buf.block)
}

// list of dirty blocks that are pending commit.
type log_t struct {
	blks		[]idebuf_t
	logstart	int
	loglen		int
	incoming	chan *bbuf_t
	admission	chan bool
	done		chan bool
	tmpblk		[512]uint8
}

func (log *log_t) init(ls int, ll int) {
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = ll - 1
	log.blks = make([]idebuf_t, 0, log.loglen)
	for i := range log.blks {
		log.blks[i].data = new([512]uint8)
	}
	log.incoming = make(chan *bbuf_t)
	log.admission = make(chan bool)
	log.done = make(chan bool)
}

func (log *log_t) addlog(blk *bbuf_t) {
	// log absorption
	for i, b := range log.blks {
		if b.block == blk.buf.block {
			*log.blks[i].data = *blk.buf.data
			return
		}
	}
	log.blks = append(log.blks, *blk.buf)
	if len(log.blks) >= log.loglen {
		panic("log larger than log len")
	}
}

func (log *log_t) commit() {
	if len(log.blks) == 0 {
		// nothing to commit
		return
	}

	lh := logheader_t{&log.tmpblk}
	for i, lb := range log.blks {
		// install log destination in the first log block
		lh.w_logdest(i, lb.block)

		// and write block to the log
		logblkn := log.logstart + 1 + i
		bdev_write(logblkn, lb.data)
	}

	lh.w_recovernum(len(log.blks))

	// commit log: flush log header
	bdev_write(log.logstart, &log.tmpblk)

	//rn := lh.recovernum()
	//if rn > 0 {
	//	runtime.Crash()
	//}

	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover
	for _, lb := range log.blks {
		bdev_write(lb.block, lb.data)
	}

	// success; clear flag indicating to recover from log
	lh.w_recovernum(0)
	bdev_write(log.logstart, &log.tmpblk)

	log.blks = log.blks[0:0]
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
		for !done {
			select {
			case nb := <- l.incoming:
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
			}
		}
		l.commit()
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

func log_write(b *bbuf_t) {
	if memtime {
		return
	}
	b.dirty = true
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
