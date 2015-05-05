package main

import "fmt"
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
	if len(path) == 0 {
		panic("no path")
	}
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
	dirs := parts[:l]
	name := parts[l]
	return dirs, name
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

	return &file_t{priv: ri}
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

func fs_link(old string, new string, cwdf *file_t) int {
	op_begin()
	defer op_end()

	cwd := cwdf.priv
	// first get inode number and inc ref count
	req := &ireq_t{}
	req.mkget(old, true)
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
		req.mkrefdec()
		idmon := idaemon_ensure(priv)
		idmon.req <- req
		<- req.ack
	}
	return resp.err
}

func fs_unlink(paths string, cwdf *file_t) int {
	_, fn := dirname(paths)
	if fn == "." || fn == ".." {
		return -EPERM
	}

	op_begin()
	defer op_end()

	req := &ireq_t{}
	req.mkunlink(paths)
	req_namei(req, paths, cwdf.priv)
	resp := <- req.ack
	doit := resp.err == 0
	// if the targe file is a directory, only remove its directory entry if
	// it is empty
	if resp.commitwait {
		req.commit <- doit
		// wait for parent inode update to finish
		<- req.commit
	}
	return resp.err
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

	req := &ireq_t{}
	req.mkcreate(paths, I_DIR)
	if len(req.cr_name) > DNAMELEN {
		return -ENAMETOOLONG
	}
	req_namei(req, paths, cwdf.priv)
	resp := <- req.ack
	return resp.err
}

func fs_open(paths string, flags int, mode int, cwdf *file_t) (*file_t, int) {
	if flags & O_CREAT != 0 {
		if paths == "/" {
			return nil, -EPERM
		}
		op_begin()
		defer op_end()

		req := &ireq_t{}
		req.mkcreate(paths, I_FILE)
		if len(req.cr_name) > DNAMELEN {
			return nil, -ENAMETOOLONG
		}
		req_namei(req, paths, cwdf.priv)
		resp := <- req.ack
		if resp.err == -EEXIST && flags & O_EXCL == 0 {
			// nothing
		} else if resp.err != 0 {
			return nil, resp.err
		}
		return &file_t{priv: resp.cnext}, 0
	}

	// send inum get request
	priv, err := namei(paths, cwdf.priv)
	if err != 0 {
		return nil, err
	}

	ret := &file_t{priv: priv}
	return ret, 0
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

func namei(paths string, cwd inum) (inum, int) {
	req := &ireq_t{}
	req.mkget(paths, false)
	req_namei(req, paths, cwd)
	resp := <- req.ack
	return resp.gnext, resp.err
}

func file_new(priv inum) *file_t {
	ret := &file_t{priv: priv}
	return ret
}

type idaemon_t struct {
	req		chan *ireq_t
	ack		chan *iresp_t
	priv		inum
	blkno		int
	ioff		int
	// cache of inode data in case block cache evicts our inode block
	icache		icache_t
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
	// insert op
	insert_name	string
	insert_priv	inum
	// unlink op
	commitwait	bool
	commit		chan bool
	// inc ref count after get
	doinc		bool
	// stat op
	stat_st		*stat_t
}

// if incref is true, the call must be made between op_{begin,end}.
func (r *ireq_t) mkget(name string, incref bool) {
	r.ack = make(chan *iresp_t)
	r.rtype = GET
	r.path = pathparts(name)
	if incref {
		r.doinc = true
	}
}

func (r *ireq_t) mkread(dsts [][]uint8, offset int) {
	r.ack = make(chan *iresp_t)
	r.rtype = READ
	r.rbufs = dsts
	r.offset = offset
}

func (r *ireq_t) mkwrite(srcs [][]uint8, offset int, append bool) {
	r.ack = make(chan *iresp_t)
	r.rtype = WRITE
	r.dbufs = srcs
	r.offset = offset
	r.dappend = append
}

func (r *ireq_t) mkcreate(path string, ntype int) {
	r.ack = make(chan *iresp_t)
	r.rtype = CREATE
	dirs, name := dirname(path)
	r.path = dirs
	r.cr_name = name
	r.cr_type = ntype
}

func (r *ireq_t) mkinsert(path string, priv inum) {
	r.ack = make(chan *iresp_t)
	r.rtype = INSERT
	dirs, name := dirname(path)
	r.path = dirs
	r.insert_name = name
	r.insert_priv = priv
}

func (r *ireq_t) mkrefdec() {
	r.ack = make(chan *iresp_t)
	r.rtype = REFDEC
}

func (r *ireq_t) mkunlink(path string) {
	r.ack = make(chan *iresp_t)
	r.commit = make(chan bool)
	r.rtype = UNLINK
	r.path = pathparts(path)
}

func (r *ireq_t) mkstat(st *stat_t) {
	r.ack = make(chan *iresp_t)
	r.rtype = STAT
	r.stat_st = st
}

type iresp_t struct {
	gnext		inum
	cnext		inum
	count		int
	err		int
	commitwait	bool
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
	irefdown := func() bool {
		idm.icache.links--
		if idm.icache.links < 0 {
			panic("negative links")
		}
		if idm.icache.links != 0 {
			iupdate()
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
	for {
		r := <- idm.req
		switch r.rtype {
		case CREATE:
			if fwded, erred := idm.forwardreq(r); fwded || erred {
				break
			}

			if idm.icache.itype != I_DIR {
				panic("create in non-dir")
			}
			if r.cr_type != I_FILE && r.cr_type != I_DIR {
				panic("no imp")
			}
			isdir := r.cr_type == I_DIR
			cnext, err := idm.icreate(r.cr_name, isdir)
			iupdate()
			// create "." and ".." entry if we made a new directory
			if err == 0 && isdir {
				req := &ireq_t{}
				req.mkinsert("..", idm.priv)
				child := idaemon_ensure(cnext)
				child.req <- req
				resp := <- req.ack
				if resp.err != 0 {
					panic("insert '../' failed")
				}
				req = &ireq_t{}
				req.mkinsert(".", cnext)
				child.req <- req
				resp = <- req.ack
				if resp.err != 0 {
					panic("insert './' failed")
				}
			}
			ret := &iresp_t{cnext: cnext, err: err}
			r.ack <- ret

		case GET:
			// is the requester asking for this daemon?
			if fwded, erred := idm.forwardreq(r); fwded || erred {
				break
			}
			if r.doinc {
				// no hard links on directories
				if idm.icache.itype != I_FILE {
					r.ack <- &iresp_t{err: -EPERM}
					break
				}
				idm.icache.links++
				iupdate()
			}
			// req is for us
			r.ack <- &iresp_t{gnext: idm.priv}

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
			terminate := irefdown()
			r.ack <- &iresp_t{}
			if terminate {
				return
			}

		case STAT:
			st := r.stat_st
			st.wdev(0)
			st.wino(int(idm.priv))
			st.wmode(idm.icache.itype)
			st.wsize(idm.icache.size)
			r.ack <- &iresp_t{}

		case UNLINK:
			// this operation is special: both the target file and
			// the parent directory of the target file process the
			// operation.
			amparent := len(r.path) == 1
			amchild := len(r.path) == 0
			if amparent {
				// parent directory
				name := r.path[0]
				commitch := r.commit
				// check to make sure the operation will
				// succeed before forwarding the operation.
				_, err := idm.iget(name)
				if err != 0 {
					r.ack <- &iresp_t{err: err}
					break
				}
				r.commitwait = true
				_, erred := idm.forwardreq(r)
				if erred {
					panic("forward must succeed")
				}

				// wait for confirmation
				commit := <- commitch
				if commit {
					_, err = idm.iunlink(name)
					iupdate()
				}
				commitch <- commit
			} else if amchild {
				// the destination is us
				// ensure child wakes up parent
				resp := &iresp_t{commitwait: r.commitwait}
				// if we are a directory, make sure we are
				// empty before unlinking.
				if idm.icache.itype == I_DIR &&
				   !idm.idirempty() {
					resp.err = -EPERM
					r.ack <- resp
					break
				}

				// decrement reference count
				terminate := irefdown()
				r.ack <- resp
				if terminate {
					return
				}
			} else {
				idm.forwardreq(r)
			}

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
	idm.icache.size = offset + c
	return c, 0
}

// does not check if name already exists. does not update ds.
func (idm *idaemon_t) dirent_add(ds []*dirdata_t, name string, nblkno int,
    ioff int) {

	var blk *bbuf_t
	var deoff int
	var ddata *dirdata_t
	dorelse := false

	found, eblkidx, eslot := dirent_empty(ds)
	if found {
		// use existing dirdata block
		deoff = eslot
		ddata = ds[eblkidx]
	} else {
		// allocate new dir data block
		newddn := balloc()
		oldsz := idm.icache.size
		nextslot := oldsz/512
		if nextslot >= NIADDRS {
			panic("need indirect support")
		}
		if idm.icache.addrs[nextslot] != 0 {
			panic("addr slot allocated")
		}
		idm.icache.addrs[nextslot] = newddn
		idm.icache.size = oldsz + 512

		deoff = 0
		// zero new directory data block
		blk = bread(newddn)
		for i := range blk.buf.data {
			blk.buf.data[i] = 0
		}
		ddata = &dirdata_t{blk}
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

func (idm *idaemon_t) icreate(name string, isdir bool) (inum, int) {
	// make sure file does not already exist
	ds := idm.all_dirents()
	defer dirent_brelse(ds)

	_, found := dirent_lookup(ds, name)
	if found {
		return 0, -EEXIST
	}

	// allocate new inode
	newbn, newioff := ialloc()

	newiblk := bread(newbn)
	newinode := &inode_t{newiblk, newioff}
	var itype int
	if isdir {
		itype = I_DIR
	} else {
		itype = I_FILE
	}
	newinode.w_itype(itype)
	newinode.w_linkcount(1)
	newinode.w_size(0)
	newinode.w_major(0)
	newinode.w_minor(0)
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
	wtf := make([]int, 0)
	allb := &wtf
	add := func(blkno int) {
		if blkno != 0 {
			wtf = append(*allb, blkno)
			allb = &wtf
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

	for _, blkno := range *allb {
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
func dirent_empty(ds []*dirdata_t) (bool, int, int) {
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
