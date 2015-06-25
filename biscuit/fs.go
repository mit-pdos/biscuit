package main

import "fmt"
import "sort"
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

	bcdaemon.init()
	go bc_daemon(&bcdaemon)

	// find the first fs block; the build system installs it in block 0 for
	// us
	blk0 := bread(0)
	FSOFF := 506
	superb_start = readn(blk0.buf.data[:], 4, FSOFF)
	if superb_start <= 0 {
		panic("bad superblock start")
	}
	blk := bread(superb_start)
	superb = superblock_t{}
	superb.blk = blk
	brelse(blk0)
	ri := superb.rootinode()
	iroot = idaemon_ensure(ri)

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

	return &file_t{ftype: INODE, priv: ri}
}

func fs_recover() {
	l := &fslog
	dblk := bread(l.logstart)
	defer brelse(dblk)
	lh := logheader_t{dblk}
	rlen := lh.recovernum()
	if rlen == 0 {
		fmt.Printf("no FS recovery needed\n")
		return
	}
	fmt.Printf("starting FS recovery...")

	for i := 0; i < rlen; i++ {
		bdest := lh.logdest(i)
		src := bread(l.logstart + 1 + i)
		dst := bread(bdest)
		dst.buf.data = src.buf.data
		dst.writeback()
		brelse(src)
		brelse(dst)
	}

	// clear recovery flag
	lh.w_recovernum(0)
	lh.blk.writeback()
	fmt.Printf("restored %v blocks\n", rlen)
}

// object for managing inode transactions. since rename may need to atomically
// operate on 3 arbitrary inodes, we need a way to lock the inodes but do it
// without deadlocking. the purpose of this class is to lock the inodes, but
// lock them in ascending order of inums. that way deadlock is avoided. it is
// complicated by the fact that we do not know all the inodes inums before hand
// -- we have to look them up. furthermore, one of the inodes that must be
// locked may be contained by one of the inums that needs to be locked, so if
// the lookup inode has a smaller inum, we have to unlock everything and start
// over, making sure to lock the looked up inode before the others. worse yet,
// a child may exist in which case it must be locked -- if it does not exist
// then obviously it is fine to not lock it.

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

type inodetx_t struct {
	dpaths	[]string
	pchilds	map[string][]string
	pmexist	map[string]bool
	lchilds map[string]bool
	cwd	inum
	lchans	map[string]chan *ireq_t
	privs	[]inum
	ptoinum	map[string]inum
}

func (itx *inodetx_t) init(cwdpriv inum) {
	itx.cwd = cwdpriv
	itx.pchilds = make(map[string][]string)
	itx.pmexist = make(map[string]bool)
	itx.lchilds = make(map[string]bool)
	itx.lchans = make(map[string]chan *ireq_t)
	itx.privs = make([]inum, 0, 6)
	itx.dpaths = make([]string, 0, 6)
	itx.ptoinum = make(map[string]inum)
}

func (itx *inodetx_t) addpath(path string) {
	itx.dpaths = append(itx.dpaths, path)
}

func (itx *inodetx_t) addchild(path string, childp string, mustexist bool) {
	if s, ok := itx.pchilds[path]; ok {
		itx.pchilds[path] = append(s, childp)
	} else {
		itx.pchilds[path] = []string{childp}
	}
	itx.pmexist[path + "/" + childp] = mustexist
}

// we don't need to inc memref of child paths until the end because a child
// path cannot be freed out from under us since we have the parent directory of
// all child paths locked (if a child is unlinked before our lock, we will
// observe that the lookup for the child fails and thus will try to lock
// again).
func (itx *inodetx_t) lockall() int {
	inums := make(map[string]inum)
	for _, p := range itx.dpaths {
		req := &ireq_t{}
		req.mkget_meminc(p)
		req_namei(req, p, itx.cwd)
		resp := <- req.ack
		if resp.err != 0 {
			return resp.err
		}
		inums[p] = resp.gnext
	}

	send := func(req *ireq_t, priv inum) *iresp_t {
		idm := idaemon_ensure(priv)
		idm.req <- req
		return <- req.ack
	}

	var lchans map[inum]chan *ireq_t
	var childpairs map[string]inum
	var sorted []int
	done := false
	for !done {
		sorted = make([]int, 0, 6)
		for _, in := range inums {
			sorted = append(sorted, int(in))
		}

		cadds := make(map[string]bool)
		// add children to sorted inums
		childpairs = make(map[string]inum)
		for path, priv := range inums {
			childs, ok := itx.pchilds[path]
			if !ok {
				continue
			}
			for _, child := range childs {
				req := &ireq_t{}
				req.mklookup(child)
				resp := send(req, priv)
				if resp.err == -ENOENT {
					if itx.pmexist[path + "/" + child] {
						return resp.err
					}
					continue
				} else if resp.err != 0 {
					return resp.err
				}
				childpriv := resp.gnext
				sorted = append(sorted, int(childpriv))
				childpairs[child] = childpriv
				cadds[child] = true
			}
		}

		lchans = make(map[inum]chan *ireq_t)
		// sort inums (no duplicates), lock in order
		sort.Sort(sort.IntSlice(sorted))
		nodups := make(map[int]bool)
		for _, in := range sorted {
			if _, ok := nodups[in]; ok {
				continue
			}
			req := &ireq_t{}
			req.mklock()
			lchans[inum(in)] = req.lock_lchan
			resp := send(req, inum(in))
			if resp.err != 0 {
				panic("lock must succeed")
			}
		}

		unlockf := func(priv inum) {
			req := &ireq_t{}
			req.mkunlock()
			lchans[priv] <- req
			resp := <- req.ack
			if resp.err != 0 {
				panic("unlock must succeed")
			}
		}
		unlockall := func() {
			for _, in := range sorted {
				unlockf(inum(in))
			}
		}

		// verify that child inums haven't been changed and that
		// children that were not locked because they didn't exist do
		// not now exist
		done = true
		for path, priv := range inums {
			childs, ok := itx.pchilds[path]
			if !ok {
				continue
			}
			for _, child := range childs {
				req := &ireq_t{}
				req.mklookup(child)
				lchans[priv] <- req
				resp := <- req.ack
				if resp.err == 0 && !cadds[child] {
					unlockall()
					done = false
					break
				} else if resp.err == -ENOENT {
					if cadds[child] {
						unlockall()
						done = false
						break
					}
					continue
				} else if resp.err != 0 {
					unlockall()
					return resp.err
				}
				cpriv := resp.gnext
				old, ok := childpairs[child]
				if !ok {
					panic("child pair must exist")
				}
				if old != cpriv {
					// child inode was moved before we locked it
					unlockall()
					done = false
					break
				}
			}
		}
	}
	for _, in := range sorted {
		itx.privs = append(itx.privs, inum(in))
	}
	// finally, build mapping from path names to lock channels and inc mem
	// ref of child paths
	for path, priv := range inums {
		itx.lchans[path] = lchans[priv]
		itx.ptoinum[path] = priv
	}
	meminc := func(lchan chan *ireq_t) {
		req := &ireq_t{}
		req.mkref_direct()
		lchan <- req
		resp := <- req.ack
		if resp.err != 0 {
			panic("child memref inc must succeed")
		}
	}
	for path, _ := range inums {
		childs, ok := itx.pchilds[path]
		if !ok {
			continue
		}
		for _, child := range childs {
			priv, ok := childpairs[child]
			if !ok {
				continue
			}
			meminc(lchans[priv])
			fp := path + "/" + child
			itx.lchans[fp] = lchans[priv]
			itx.ptoinum[fp] = priv
			itx.lchilds[fp] = true
		}
	}
	return 0
}

func (itx *inodetx_t) childlocked(fpath string) bool {
	return itx.lchilds[fpath]
}

func (itx *inodetx_t) privfor(path string) inum {
	ret, ok := itx.ptoinum[path]
	if !ok {
		panic("no such path")
	}
	return ret
}

func (itx *inodetx_t) send(fpath string, req *ireq_t) *iresp_t {
	if itx.lchans == nil {
		panic("nothing locked")
	}
	lchan, ok := itx.lchans[fpath]
	if !ok {
		panic("no such path")
	}
	lchan <- req
	return <- req.ack
}

func (itx *inodetx_t) unlockall() {
	// unlock
	if itx.lchans == nil {
		panic("nothing locked")
	}
	for _, lchan := range itx.lchans {
		req := &ireq_t{}
		req.mkunlock()
		lchan <- req
		resp := <- req.ack
		if resp.err != 0 {
			panic("unlock must succeed")
		}
	}
	// dec mem ref
	for _, priv := range itx.privs {
		req := &ireq_t{}
		req.mkrefdec(true)
		idm := idaemon_ensure(priv)
		idm.req <- req
		resp := <- req.ack
		if resp.err != 0 {
			panic("dec ref must succeed")
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
	resp := <- req.ack
	if resp.err != 0 {
		return resp.err
	}

	// insert directory entry
	priv := resp.gnext
	req = &ireq_t{}
	req.mkinsert(new, priv)
	req_namei(req, new, cwd)
	resp = <- req.ack
	// decrement ref count if the insert failed
	if resp.err != 0 {
		req = &ireq_t{}
		req.mkrefdec(false)
		idmon := idaemon_ensure(priv)
		idmon.req <- req
		<- req.ack
	}
	return resp.err
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
	cp := dirs + "/" + fn
	resp := itx.send(cp, req)
	if resp.err != 0 {
		return resp.err
	}

	req.mkunlink(fn)
	resp = itx.send(dirs, req)
	if resp.err != 0 {
		panic("unlink must succeed")
	}
	req.mkrefdec(false)
	resp = itx.send(cp, req)
	if resp.err != 0 {
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

	sendd := func(path string, req *ireq_t) *iresp_t {
		resp := itx.send(path, req)
		if resp.err != 0 {
			panic("op must succed")
		}
		return resp
	}

	ofp := odirs + "/" + ofn
	nfp := ndirs + "/" + nfn
	newexists := itx.childlocked(nfp)
	if newexists {
		// make sure old file and new file are both files, or both
		// directories
		ost := &stat_t{}
		ost.init()
		req := &ireq_t{}
		req.mkstat(ost)
		sendd(ofp, req)

		nst := &stat_t{}
		nst.init()
		req.mkstat(nst)
		sendd(nfp, req)

		odir := ost.mode() == I_DIR
		ndir := nst.mode() == I_DIR
		if odir && !ndir {
			return -ENOTDIR
		} else if !odir && ndir {
			return -EISDIR
		}

		// unlink existing new file
		req.mkunlink(nfn)
		sendd(ndirs, req)

		req.mkrefdec(false)
		sendd(nfp, req)
	}

	// insert new file
	opriv := itx.privfor(ofp)

	req := &ireq_t{}
	req.mkinsert(nfn, opriv)
	sendd(ndirs, req)

	req.mkunlink(ofn)
	sendd(odirs, req)

	return 0
}

func fs_read(dsts [][]uint8, f *file_t, offset int) (int, int) {
	// send read request to inode daemon owning priv
	req := &ireq_t{}
	req.mkread(dsts, offset)

	priv := f.priv
	idmon := idaemon_ensure(priv)
	idmon.req <- req
	resp := <- req.ack
	if resp.err != 0 {
		return 0, resp.err
	}
	return resp.count, 0
}

func fs_write(srcs [][]uint8, f *file_t, offset int, append bool) (int, int) {
	op_begin()
	defer op_end()

	priv := f.priv
	// send write request to inode daemon owning priv
	req := &ireq_t{}
	req.mkwrite(srcs, offset, append)

	idmon := idaemon_ensure(priv)
	idmon.req <- req
	resp := <- req.ack
	if resp.err != 0 {
		return 0, resp.err
	}
	return resp.count, 0
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
	resp := itx.send(dirs, req)
	if resp.err != 0 {
		return resp.err
	}
	priv := resp.cnext

	idm := idaemon_ensure(priv)
	req.mkinsert(".", priv)
	idm.req <- req
	resp = <- req.ack
	if resp.err != 0 {
		panic("insert must succeed")
	}
	parpriv := itx.privfor(dirs)
	req.mkinsert("..", parpriv)
	idm.req <- req
	resp = <- req.ack
	if resp.err != 0 {
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
		resp := itx.send(dirs, req)

		oexcl := flags & O_EXCL != 0
		if oexcl && resp.err == -EEXIST {
			itx.unlockall()
			return nil, resp.err
		} else if resp.err != 0 && resp.err != -EEXIST {
			itx.unlockall()
			return nil, resp.err
		}

		// open file
		priv = resp.cnext
		idm = idaemon_ensure(priv)
		maj = major
		min = minor

		req.mkref_direct()
		idm.req <- req
		resp = <- req.ack
		if resp.err != 0 {
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
		resp := <- req.ack
		if resp.err != 0 {
			return nil, resp.err
		}
		priv = resp.gnext
		maj = resp.major
		min = resp.minor
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
			resp := <- req.ack
			if resp.err != 0 {
				panic("mem ref dec must succeed")
			}
		}
		st := &stat_t{}
		st.init()
		req := &ireq_t{}
		req.mkstat(st)
		idm.req <- req
		resp := <- req.ack
		if resp.err != 0 {
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
		resp := <- req.ack
		if resp.err != 0 {
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
	resp := <- req.ack
	return resp.err
}

func fs_memref(f *file_t, perms int) {
	idmon := idaemon_ensure(f.priv)
	req := &ireq_t{}
	req.mkref_direct()
	idmon.req <- req
	resp := <- req.ack
	if resp.err != 0 {
		panic("mem ref increment of open file failed")
	}
}

func fs_stat(f *file_t, st *stat_t) int {
	priv := f.priv
	idmon := idaemon_ensure(priv)
	req := &ireq_t{}
	req.mkstat(st)
	idmon.req <- req
	resp := <- req.ack
	return resp.err
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

type idaemon_t struct {
	req		chan *ireq_t
	ack		chan *iresp_t
	priv		inum
	blkno		int
	ioff		int
	// cache of inode data in case block cache evicts our inode block
	icache		icache_t
	memref		int
}

var allidmons	= map[inum]*idaemon_t{}
var idmonl	= sync.Mutex{}

func idaemon_ensure(priv inum) *idaemon_t {
	idmonl.Lock()
	ret, ok := allidmons[priv]
	if !ok {
		ret = &idaemon_t{}
		ret.init(priv)
		allidmons[priv] = ret
		idaemonize(ret)
	}
	idmonl.Unlock()
	return ret
}

// creates a new idaemon struct and fills its inode
func (idm *idaemon_t) init(priv inum) {
	blkno, ioff := bidecode(int(priv))
	idm.priv = priv
	idm.blkno = blkno
	idm.ioff = ioff

	idm.req = make(chan *ireq_t)
	idm.ack = make(chan *iresp_t)

	blk := bread(blkno)
	idm.icache.fill(blk, ioff)
	brelse(blk)
}


type icache_t struct {
	itype	int
	links	int
	size	int
	major	int
	minor	int
	indir	int
	addrs	[NIADDRS]int
}

func (ic *icache_t) fill(blk *bbuf_t, ioff int) {
	inode := inode_t{blk, ioff}
	ic.itype = inode.itype()
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
func (ic *icache_t) flushto(blk *bbuf_t, ioff int) bool {
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
	ack		chan *iresp_t
	// request type
	rtype		rtype_t
	// tree traversal
	path		[]string
	// read/write op
	offset		int
	// read op
	rbufs		[][]uint8
	// writes op
	dbufs		[][]uint8
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
	lock_lchan	chan *ireq_t }

// if incref is true, the call must be made between op_{begin,end}.
func (r *ireq_t) mkget_fsinc(name string) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = GET
	r.path = pathparts(name)
	r.fsref = true
	r.memref = false
}

func (r *ireq_t) mkget_meminc(name string) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = GET
	r.path = pathparts(name)
	r.fsref = false
	r.memref = true
}

func (r *ireq_t) mkref_direct() {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = GET
	r.fsref = false
	r.memref = true
}

func (r *ireq_t) mkread(dsts [][]uint8, offset int) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = READ
	r.rbufs = dsts
	r.offset = offset
}

func (r *ireq_t) mkwrite(srcs [][]uint8, offset int, append bool) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = WRITE
	r.dbufs = srcs
	r.offset = offset
	r.dappend = append
}

func (r *ireq_t) mkcreate_dir(path string) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
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
	r.ack = make(chan *iresp_t)
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
	r.ack = make(chan *iresp_t)
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
	r.ack = make(chan *iresp_t)
	r.rtype = INSERT
	dirs, name := dirname(path)
	r.path = dirs
	r.insert_name = name
	r.insert_priv = priv
}

func (r *ireq_t) mkrefdec(memref bool) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
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
	r.ack = make(chan *iresp_t)
	r.rtype = UNLINK
	dirs, fn := dirname(path)
	if len(dirs) != 0 {
		panic("no lookup with unlink now")
	}
	r.path = []string{fn}
}

func (r *ireq_t) mkstat(st *stat_t) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = STAT
	r.stat_st = st
}

func (r *ireq_t) mklock() {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = LOCK
	r.lock_lchan = make(chan *ireq_t)
}

func (r *ireq_t) mkunlock() {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = UNLOCK
}

func (r *ireq_t) mklookup(path string) {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
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
	r.ack = make(chan *iresp_t)
	r.rtype = EMPTY
}

func (r *ireq_t) mktrunc() {
	var z ireq_t
	*r = z
	r.ack = make(chan *iresp_t)
	r.rtype = TRUNC
}

type iresp_t struct {
	gnext		inum
	cnext		inum
	count		int
	err		int
	commitwait	bool
	major		int
	minor		int
}

// a type for an inode block/offset identifier
type inum int

// returns true if the request was forwarded or if an error occured. if an
// error occurs, writes the error back to the requester.
func (idm *idaemon_t) forwardreq(r *ireq_t) (bool, bool) {
	if len(r.path) == 0 {
		// req is for this idaemon
		return false, false
	}

	next := r.path[0]
	r.path = r.path[1:]
	npriv, err := idm.iget(next)
	if err != 0 {
		r.ack <- &iresp_t{err: err}
		return false, true
	}
	nextidm := idaemon_ensure(npriv)
	// forward request. if we are sending to our parent via "../" or to
	// ourselves via "./", do it in a separate goroutine so that we don't
	// deadlock.
	send := func() {
		nextidm.req <- r
	}
	if next == ".." || next == "." {
		go send()
	} else {
		send()
	}
	return true, false
}

func idaemonize(idm *idaemon_t) {
	iupdate := func() {
		blk := bread(idm.blkno)
		if idm.icache.flushto(blk, idm.ioff) {
			log_write(blk)
		}
		brelse(blk)
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
		if idm.icache.links != 0 || idm.memref != 0 {
			if !memref {
				iupdate()
			}
			return false
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
		r := <- ch
		switch r.rtype {
		case CREATE:
			if fwded, erred := idm.forwardreq(r); fwded || erred {
				break
			}

			if r.cr_type != I_FILE && r.cr_type != I_DIR &&
			   r.cr_type != I_DEV {
				panic("no imp")
			}

			if idm.icache.itype != I_DIR {
				r.ack <- &iresp_t{err: -ENOTDIR}
				break
			}

			itype := r.cr_type
			maj := r.cr_major
			min := r.cr_minor
			cnext, err := idm.icreate(r.cr_name, itype, maj, min)
			iupdate()

			resp := &iresp_t{cnext: cnext, err: err,
			    major: maj, minor: min}
			r.ack <- resp

		case GET:
			// is the requester asking for this daemon?
			if fwded, erred := idm.forwardreq(r); fwded || erred {
				break
			}

			// not open, just increase links
			if r.fsref {
				// no hard links on directories
				if idm.icache.itype != I_FILE {
					r.ack <- &iresp_t{err: -EPERM}
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
			r.ack <- &iresp_t{gnext: idm.priv, major: maj,
			    minor: min}

		case INSERT:
			// create new dir ent with given inode number
			if fwded, erred := idm.forwardreq(r); fwded || erred {
				break
			}
			err := idm.iinsert(r.insert_name, r.insert_priv)
			resp := &iresp_t{err: err}
			iupdate()
			r.ack <- resp

		case READ:
			read, err := idm.iread(r.rbufs, r.offset)
			ret := &iresp_t{count: read, err: err}
			r.ack <- ret

		case REFDEC:
			// decrement reference count
			terminate := irefdown(r.memref)
			r.ack <- &iresp_t{}
			if terminate {
				return
			}

		case STAT:
			st := r.stat_st
			st.wdev(0)
			st.wino(int(idm.priv))
			st.wmode(idm.mkmode())
			st.wsize(idm.icache.size)
			st.wrdev(mkdev(idm.icache.major, idm.icache.minor))
			r.ack <- &iresp_t{}

		case UNLINK:
			name := r.path[0]
			_, err := idm.iunlink(name)
			if err == 0 {
				iupdate()
			}
			ret := &iresp_t{err: err}
			r.ack <- ret

		case WRITE:
			if idm.icache.itype == I_DIR {
				panic("write to dir")
			}
			offset := r.offset
			if r.dappend {
				offset = idm.icache.size
			}
			read, err := idm.iwrite(r.dbufs, offset)
			// iupdate() must come before the response is sent.
			// otherwise the idaemon and the requester race to
			// log_write()/op_end().
			iupdate()
			ret := &iresp_t{count: read, err: err}
			r.ack <- ret

		// new uops
		case LOCK:
			if r.lock_lchan == nil {
				panic("lock with nil lock chan")
			}
			if ch != idm.req {
				panic("double lock")
			}
			ch = r.lock_lchan
			resp := &iresp_t{err: 0}
			r.ack <- resp

		case UNLOCK:
			if ch == idm.req {
				panic("already unlocked")
			}
			ch = idm.req
			resp := &iresp_t{err: 0}
			r.ack <- resp

		case LOOKUP:
			if len(r.path) != 1 {
				panic("path for lookup should be one component")
			}
			name := r.path[0]
			npriv, err := idm.iget(name)
			resp := &iresp_t{gnext: npriv, err: err}
			r.ack <- resp

		case EMPTY:
			err := 0
			if idm.icache.itype == I_DIR && !idm.idirempty() {
				err = -EPERM
			}
			resp := &iresp_t{err: err}
			r.ack <- resp

		case TRUNC:
			if idm.icache.itype != I_FILE &&
			   idm.icache.itype != I_DEV {
				panic("bad truncate")
			}
			idm.icache.size = 0
			iupdate()
			resp := &iresp_t{err: 0}
			r.ack <- resp

		default:
			panic("bad req type")
		}
	}}()
}

func (idm *idaemon_t) offsetblk(offset int, writing bool) int {
	zeroblock := func(blkno int) {
		// zero block
		zblk := bread(blkno)
		for i := range zblk.buf.data {
			zblk.buf.data[i] = 0
		}
		log_write(zblk)
		brelse(zblk)
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
	whichblk := offset/512
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
		indblk := bread(indno)
		for i := 0; i < indslot/slotpb; i++ {
			indno = readn(indblk.buf.data[:], 8, nextindb)
			indno, isnew = ensureb(indno, true)
			if isnew {
				writen(indblk.buf.data[:], 8, nextindb, indno)
				log_write(indblk)
			}
			brelse(indblk)
			indblk = bread(indno)
		}
		// finally get data block from indirect block
		noff := (indslot % slotpb)*8
		blkn = readn(indblk.buf.data[:], 8, noff)
		blkn, isnew = ensureb(blkn, false)
		if isnew {
			writen(indblk.buf.data[:], 8, noff, blkn)
			log_write(indblk)
		}
		brelse(indblk)
	} else {
		blkn = idm.icache.addrs[whichblk]
		if writing && blkn == 0 {
			blkn = balloc()
			idm.icache.addrs[whichblk] = blkn
		}
	}
	return blkn
}

// if writing, allocate a block if necessary and don't trim the slice to the
// size of the file
func (idm *idaemon_t) blkslice(offset int, writing bool) ([]uint8, *bbuf_t) {
	blkn := idm.offsetblk(offset, writing)
	if blkn < superb_start && idm.icache.size > 0 {
		panic("bad block")
	}
	blk := bread(blkn)
	start := offset % 512
	bsp := 512 - start
	if !writing {
		// trim src buffer to end of file
		left := idm.icache.size - offset
		if bsp > left {
			bsp = left
		}
	}
	src := blk.buf.data[start:start+bsp]
	return src, blk
}

func (idm *idaemon_t) iread(dsts [][]uint8, offset int) (int, int) {
	c := 0
	for _, d := range dsts {
		read, err := idm.iread1(d, offset + c)
		c += read
		if err != 0 {
			return c, err
		}
		if read != len(d) {
			break
		}
	}
	return c, 0
}

func (idm *idaemon_t) iread1(dst []uint8, offset int) (int, int) {
	isz := idm.icache.size
	if offset >= isz {
		return 0, 0
	}

	sz := len(dst)
	c := 0
	dstfull := false
	for c < sz {
		src, blk := idm.blkslice(offset + c, false)
		// copy upto len(dst) bytes
		ub := len(src)
		if len(dst) < ub {
			ub = len(dst)
			dstfull = true
		}
		for i := 0; i < ub; i++ {
			dst[i] = src[i]
		}
		brelse(blk)
		c += ub
		dst = dst[ub:]
		if offset + c == isz || dstfull {
			break
		}
	}
	return c, 0
}

func (idm *idaemon_t) iwrite(srcs [][]uint8, offset int) (int, int) {
	c := 0
	for _, s := range srcs {
		wrote, err := idm.iwrite1(s, offset + c)
		c += wrote
		if err != 0 {
			return c, err
		}
		// short write
		if wrote != len(s) {
			panic("short write")
		}
	}
	return c, 0
}

func (idm *idaemon_t) iwrite1(src []uint8, offset int) (int, int) {
	// XXX if file length is shortened, when to free old blocks?
	sz := len(src)
	c := 0
	for c < sz {
		dst, blk := idm.blkslice(offset + c, true)
		ub := len(dst)
		if len(src) < ub {
			ub = len(src)
		}
		for i := 0; i < ub; i++ {
			dst[i] = src[i]
		}
		log_write(blk)
		brelse(blk)
		c += ub
		src = src[ub:]
	}
	newsz := offset + c
	if newsz > idm.icache.size {
		idm.icache.size = newsz
	}
	return c, 0
}

// does not check if name already exists. does not update ds.
func (idm *idaemon_t) dirent_add(ds []*dirdata_t, name string, nblkno int,
    ioff int) {

	var deoff int
	var ddata *dirdata_t
	dorelse := false

	found, eblkidx, eslot := dirent_getempty(ds)
	if found {
		// use existing dirdata block
		deoff = eslot
		ddata = ds[eblkidx]
	} else {
		// allocate new dir data block
		blkn := idm.offsetblk(idm.icache.size, true)
		idm.icache.size += 512
		blk := bread(blkn)
		for i := range blk.buf.data {
			blk.buf.data[i] = 0
		}
		ddata = &dirdata_t{blk}
		deoff = 0
		dorelse = true
	}
	// write dir entry
	if ddata.filename(deoff) != "" {
		panic("dir entry slot is not free")
	}
	ddata.w_filename(deoff, name)
	ddata.w_inodenext(deoff, nblkno, ioff)
	log_write(ddata.blk)
	if dorelse {
		brelse(ddata.blk)
	}
}

func (idm *idaemon_t) icreate(name string, nitype, major,
    minor int) (inum, int) {
	if name == "" {
		panic("icreate with no name")
	}
	if nitype != I_DEV && (major != 0 || minor != 0) {
		panic("inconsistent args")
	}
	// make sure file does not already exist
	ds := idm.all_dirents()
	defer dirent_brelse(ds)

	foundinum, found := dirent_lookup(ds, name)
	if found {
		return foundinum, -EEXIST
	}

	// allocate new inode
	newbn, newioff := ialloc()

	newiblk := bread(newbn)
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

	log_write(newiblk)
	brelse(newiblk)

	// write new directory entry referencing newinode
	idm.dirent_add(ds, name, newbn, newioff)

	newinum := inum(biencode(newbn, newioff))
	return newinum, 0
}

func (idm *idaemon_t) iget(name string) (inum, int) {
	// did someone confuse a file with a directory?
	if idm.icache.itype != I_DIR {
		return 0, -ENOTDIR
	}
	ds := idm.all_dirents()
	priv, found := dirent_lookup(ds, name)
	dirent_brelse(ds)
	if found {
		return priv, 0
	}
	return 0, -ENOENT
}

// creates a new directory entry with name "name" and inode number priv
func (idm *idaemon_t) iinsert(name string, priv inum) int {
	ds := idm.all_dirents()
	defer dirent_brelse(ds)

	_, found := dirent_lookup(ds, name)
	if found {
		return -EEXIST
	}
	a, b := bidecode(int(priv))
	idm.dirent_add(ds, name, a, b)
	return 0
}

// returns inode number of unliked inode so caller can decrement its ref count
func (idm *idaemon_t) iunlink(name string) (inum, int) {
	ds := idm.all_dirents()
	defer dirent_brelse(ds)

	priv, found := dirent_erase(ds, name)
	if !found {
		return 0, -ENOENT
	}
	return priv, 0
}

// returns true if the inode has no directory entries
func (idm *idaemon_t) idirempty() bool {
	ds := idm.all_dirents()
	defer dirent_brelse(ds)

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
		add(idm.icache.indir)
		blk := bread(indno)
		nextoff := 63*8
		indno = readn(blk.buf.data[:], 8, nextoff)
		brelse(blk)
	}

	// mark this inode as dead and free the inode block if all inodes on it
	// are marked dead.
	alldead := func(blk *bbuf_t) bool {
		bwords := 512/8
		ret := true
		for i := 0; i < bwords/NIWORDS; i++ {
			ic := icache_t{}
			ic.fill(blk, i)
			if ic.itype != I_INVALID {
				ret = false
				break
			}
		}
		return ret
	}
	iblk := bread(idm.blkno)
	idm.icache.itype = I_INVALID
	idm.icache.flushto(iblk, idm.ioff)
	if alldead(iblk) {
		add(idm.blkno)
	} else {
		log_write(iblk)
	}
	brelse(iblk)

	for _, blkno := range allb {
		bfree(blkno)
	}
}

// returns a slice of all directory data blocks. caller must brelse all
// underlying blocks.
func (idm *idaemon_t) all_dirents() ([]*dirdata_t) {
	if idm.icache.itype != I_DIR {
		panic("not a directory")
	}
	isz := idm.icache.size
	ret := make([]*dirdata_t, 0)
	for i := 0; i < isz; i += 512 {
		_, blk := idm.blkslice(i, false)
		dirdata := &dirdata_t{blk}
		ret = append(ret, dirdata)
	}
	return ret
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
		panic("weird itype")
	}
}

// returns the inode number for the specified filename
func dirent_lookup(ds []*dirdata_t, name string) (inum, bool) {
	for i := 0; i < len(ds); i++ {
		for j := 0; j < NDIRENTS; j++ {
			if name == ds[i].filename(j) {
				return ds[i].inodenext(j), true
			}
		}
	}
	return 0, false
}

func dirent_brelse(ds []*dirdata_t) {
	for _, d := range ds {
		brelse(d.blk)
	}
}

// returns an empty directory entry
func dirent_getempty(ds []*dirdata_t) (bool, int, int) {
	for i := 0; i < len(ds); i++ {
		for j := 0; j < NDIRENTS; j++ {
			if ds[i].filename(j) == "" {
				return true, i, j
			}
		}
	}
	return false, 0, 0
}

// erases the specified directory entry
func dirent_erase(ds []*dirdata_t, name string) (inum, bool) {
	for i := 0; i < len(ds); i++ {
		for j := 0; j < NDIRENTS; j++ {
			if name == ds[i].filename(j) {
				ret := ds[i].inodenext(j)
				ds[i].w_filename(j, "")
				ds[i].w_inodenext(j, 0, 0)
				log_write(ds[i].blk)
				return ret, true
			}
		}
	}
	return 0, false
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
	l	sync.Mutex
	blk	*bbuf_t
}

func (sb *superblock_t) freeblock() int {
	return fieldr(&sb.blk.buf.data, 0)
}

func (sb *superblock_t) freeblocklen() int {
	return fieldr(&sb.blk.buf.data, 1)
}

func (sb *superblock_t) loglen() int {
	return fieldr(&sb.blk.buf.data, 2)
}

func (sb *superblock_t) rootinode() inum {
	v := fieldr(&sb.blk.buf.data, 3)
	return inum(v)
}

func (sb *superblock_t) lastblock() int {
	return fieldr(&sb.blk.buf.data, 4)
}

func (sb *superblock_t) freeinode() int {
	return fieldr(&sb.blk.buf.data, 5)
}

func (sb *superblock_t) w_freeblock(n int) {
	fieldw(&sb.blk.buf.data, 0, n)
}

func (sb *superblock_t) w_freeblocklen(n int) {
	fieldw(&sb.blk.buf.data, 1, n)
}

func (sb *superblock_t) w_loglen(n int) {
	fieldw(&sb.blk.buf.data, 2, n)
}

func (sb *superblock_t) w_rootinode(blk int, iidx int) {
	fieldw(&sb.blk.buf.data, 3, biencode(blk, iidx))
}

func (sb *superblock_t) w_lastblock(n int) {
	fieldw(&sb.blk.buf.data, 4, n)
}

func (sb *superblock_t) w_freeinode(n int) {
	fieldw(&sb.blk.buf.data, 5, n)
}

// first log header block format
// bytes, meaning
// 0-7,   valid log blocks
// 8-511, log destination (63)
type logheader_t struct {
	blk	*bbuf_t
}

func (lh *logheader_t) recovernum() int {
	return fieldr(&lh.blk.buf.data, 0)
}

func (lh *logheader_t) w_recovernum(n int) {
	fieldw(&lh.blk.buf.data, 0, n)
}

func (lh *logheader_t) logdest(p int) int {
	if p < 0 || p > 62 {
		panic("bad dnum")
	}
	return fieldr(&lh.blk.buf.data, 8 + p)
}

func (lh *logheader_t) w_logdest(p int, n int) {
	if p < 0 || p > 62 {
		panic("bad dnum")
	}
	fieldw(&lh.blk.buf.data, 8 + p, n)
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
	blk	*bbuf_t
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
	it := fieldr(&ind.blk.buf.data, ifield(ind.ioff, 0))
	if it < I_FIRST || it > I_LAST {
		panic(fmt.Sprintf("weird inode type %d", it))
	}
	return it
}

func (ind *inode_t) linkcount() int {
	return fieldr(&ind.blk.buf.data, ifield(ind.ioff, 1))
}

func (ind *inode_t) size() int {
	return fieldr(&ind.blk.buf.data, ifield(ind.ioff, 2))
}

func (ind *inode_t) major() int {
	return fieldr(&ind.blk.buf.data, ifield(ind.ioff, 3))
}

func (ind *inode_t) minor() int {
	return fieldr(&ind.blk.buf.data, ifield(ind.ioff, 4))
}

func (ind *inode_t) indirect() int {
	return fieldr(&ind.blk.buf.data, ifield(ind.ioff, 5))
}

func (ind *inode_t) addr(i int) int {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	return fieldr(&ind.blk.buf.data, ifield(ind.ioff, addroff + i))
}

func (ind *inode_t) w_itype(n int) {
	if n < I_FIRST || n > I_LAST {
		panic("weird inode type")
	}
	fieldw(&ind.blk.buf.data, ifield(ind.ioff, 0), n)
}

func (ind *inode_t) w_linkcount(n int) {
	fieldw(&ind.blk.buf.data, ifield(ind.ioff, 1), n)
}

func (ind *inode_t) w_size(n int) {
	fieldw(&ind.blk.buf.data, ifield(ind.ioff, 2), n)
}

func (ind *inode_t) w_major(n int) {
	fieldw(&ind.blk.buf.data, ifield(ind.ioff, 3), n)
}

func (ind *inode_t) w_minor(n int) {
	fieldw(&ind.blk.buf.data, ifield(ind.ioff, 4), n)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *inode_t) w_indirect(blk int) {
	fieldw(&ind.blk.buf.data, ifield(ind.ioff, 5), blk)
}

func (ind *inode_t) w_addr(i int, blk int) {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	fieldw(&ind.blk.buf.data, ifield(ind.ioff, addroff + i), blk)
}

// directory data format
// 0-13,  file name characters
// 14-21, inode block/offset
// ...repeated, totaling 23 times
type dirdata_t struct {
	blk	*bbuf_t
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
	sl := dir.blk.buf.data[st : st + DNAMELEN]
	ret := make([]byte, 0)
	for _, c := range sl {
		if c != 0 {
			ret = append(ret, c)
		}
	}
	return string(ret)
}

func (dir *dirdata_t) inodenext(didx int) inum {
	st := doffset(didx, 14)
	v := readn(dir.blk.buf.data[:], 8, st)
	return inum(v)
}

func (dir *dirdata_t) w_filename(didx int, fn string) {
	st := doffset(didx, 0)
	sl := dir.blk.buf.data[st : st + DNAMELEN]
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
	writen(dir.blk.buf.data[:], 8, st, v)
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
	var blk *bbuf_t
	var blkn int
	var oct int
	// 0 is free, 1 is allocated
	for i := 0; i < flen && !found; i++ {
		if blk != nil {
			brelse(blk)
		}
		blk = bread(fst + i)
		for idx, c := range blk.buf.data {
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
	blk.buf.data[oct] |= 1 << bit
	log_write(blk)
	brelse(blk)

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
	fblk := bread(fblkno)
	fblk.buf.data[fbyteoff] &= ^(1 << fbitoff)
	log_write(fblk)
	brelse(fblk)

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
	zblk := bread(ifreeblk)
	for i := range zblk.buf.data {
		zblk.buf.data[i] = 0
	}
	log_write(zblk)
	brelse(zblk)
	reti := ifreeoff
	ifreeoff++
	return ifreeblk, reti
}

// our actual disk
var disk	disk_t

type idebuf_t struct {
	disk	int32
	block	int32
	data	[512]uint8
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

func idereq_new(block int, write bool, ibuf *idebuf_t) *idereq_t {
	ret := idereq_t{}
	ret.ack = make(chan bool)
	if ibuf == nil {
		ret.buf = &idebuf_t{}
		ret.buf.block = int32(block)
	} else {
		ret.buf = ibuf
		if block != int(ibuf.block) {
			panic("block mismatch")
		}
	}
	ret.write = write
	return &ret
}

type bbuf_t struct {
	buf	*idebuf_t
	dirty	bool
}

func (b *bbuf_t) writeback() {
	ireq := idereq_new(int(b.buf.block), true, b.buf)
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
			ireq := idereq_new(blkno, false, nil)
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
			blkno := int(nb.buf.block)
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

func bread(blkno int) *bbuf_t {
	req := bcreq_new(blkno)
	bcdaemon.req <- req
	return <- req.ack
}

func brelse(b *bbuf_t) {
	bcdaemon.done <- int(b.buf.block)
}

// list of dirty blocks that are pending commit.
type log_t struct {
	blks		[]int
	logstart	int
	loglen		int
	incoming	chan int
	admission	chan bool
	done		chan bool
}

func (log *log_t) init(ls int, ll int) {
	log.logstart = ls
	// first block of the log is an array of log block destinations
	log.loglen = ll - 1
	log.blks = make([]int, 0, log.loglen)
	log.incoming = make(chan int)
	log.admission = make(chan bool)
	log.done = make(chan bool)
}

func (log *log_t) append(blkn int) {
	// log absorption
	for _, b := range log.blks {
		if b == blkn {
			return
		}
	}
	log.blks = append(log.blks, blkn)
	if len(log.blks) >= log.loglen {
		panic("log larger than log len")
	}
}

func (log *log_t) commit() {
	if len(log.blks) == 0 {
		// nothing to commit
		return
	}

	dblk := bread(log.logstart)
	lh := logheader_t{dblk}
	if rnum := lh.recovernum(); rnum != 0  {
		panic(fmt.Sprintf("should have ran recover %v", rnum))
	}
	for i, lbn := range log.blks {
		// install log destination in the first log block
		lh.w_logdest(i, lbn)

		// and write block to the log
		src := bread(lbn)
		logblkn := log.logstart + 1 + i
		// XXX unnecessary read
		dst := bread(logblkn)
		dst.buf.data = src.buf.data
		dst.writeback()
		brelse(src)
		brelse(dst)
	}

	lh.w_recovernum(len(log.blks))

	// commit log: flush log header
	lh.blk.writeback()

	//rn := lh.recovernum()
	//if rn > 0 {
	//	runtime.Crash()
	//}

	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover
	for _, lbn := range log.blks {
		blk := bread(lbn)
		if !blk.dirty {
			panic("log blocks must be dirty/in cache")
		}
		blk.writeback()
		brelse(blk)
	}

	// success; clear flag indicating to recover from log
	lh.w_recovernum(0)
	lh.blk.writeback()
	brelse(dblk)

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
				if t <= 0 {
					panic("oh noes")
				}
				l.append(nb)
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
	<- fslog.admission
}

func op_end() {
	fslog.done <- true
}

func log_write(b *bbuf_t) {
	b.dirty = true
	fslog.incoming <- int(b.buf.block)
}
