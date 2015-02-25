package main

import "fmt"
import "runtime"
import "strings"
import "sync"
import "unsafe"

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
// free inode lock
var filock	= sync.Mutex{}

func path_sanitize(path string) ([]string, bool) {
	sp := strings.Split(path, "/")
	nn := []string{}
	for _, s := range sp {
		if s != "" {
			nn = append(nn, s)
		}
	}
	if len(nn) == 0 {
		return nil, true
	}
	return nn, false
}

func fs_init() {
	go ide_daemon()

	bcdaemon.init()
	go bc_daemon(&bcdaemon)

	// find the first fs block; the build system installs it in block 0 for
	// us
	blk0 := bread(0)
	FSOFF := 506
	superb_start = readn(blk0.buf.data[:], 4, FSOFF)
	if superb_start == 0 {
		panic("bad superblock start")
	}
	blk := bread(superb_start)
	superb = superblock_t{}
	superb.blk = blk
	brelse(blk0)
	brelse(blk)
	ri := superb.rootinode()
	iroot = idaemon_ensure(ri)

	free_start = superb.freeblock()
	free_len = superb.freeblocklen()
	logstart := free_start + free_len
	loglen := superb.loglen()
	usable_start = logstart + loglen
	if loglen < 0 {
		panic("bad log len")
	}

	fs_recover()

	fslog.init(logstart, loglen)
	go log_daemon(&fslog)
}

func fs_recover() {
	rlen := superb.recovernum()
	if rlen == 0 {
		fmt.Printf("no FS recovery needed\n")
		return
	}

	l := &fslog
	fmt.Printf("starting FS recovery...")
	dblk := bread(l.logstart)
	for i := 0; i < rlen; i++ {
		bdest := readn(dblk.buf.data[:], 4, 4*i)
		src := bread(l.logstart + 1 + i)
		dst := bread(bdest)
		dst.buf.data = src.buf.data
		dst.writeback()
		brelse(src)
		brelse(dst)
	}

	brelse(dblk)
	// clear recovery flag
	superb.w_recovernum(0)
	superb.blk.writeback()
	fmt.Printf("restored %v blocks\n", rlen)
}

func fs_read(dsts [][]uint8, priv inum, offset int) (int, int) {
	// send read request to inode daemon owning inum
	req := &ireq_t{}
	req.mkread(dsts, offset)

	idmon := idaemon_ensure(priv)
	idmon.req <- req
	resp := <- req.ack
	if resp.err != 0 {
		return 0, resp.err
	}
	return resp.count, 0
}

// caller must have file's filetree node locked
//func fs_write(src []uint8, file *file_t, soffset int, append bool) (int, int) {
//	fib, fio := bidecode(int(file.priv))
//	inode := inode_get(fib, fio, true, I_FILE)
//	defer inode_put(inode)
//	isz := inode.size()
//	offset := soffset
//	if append {
//		offset = isz
//	}
//	// XXX if file length is shortened, when to free old blocks?
//	sz := len(src)
//	c := 0
//	for c < sz {
//		fileoff := offset + c
//		whichblk := fileoff/512
//		if whichblk >= NIADDRS {
//			panic("need indirect support")
//		}
//		blkn := inode.addr(whichblk)
//		if blkn == 0 {
//			// alocate a new block
//			blkn = balloc()
//			inode.w_addr(whichblk, blkn)
//		}
//		blk := bread(blkn)
//		start := fileoff % 512
//		bsp := 512 - start
//		left := len(src) - c
//		if bsp > left {
//			bsp = left
//		}
//		dst := blk.buf.data[start:start+bsp]
//		for i := range dst {
//			dst[i] = src[c + i]
//		}
//		log_write(blk)
//		brelse(blk)
//		c += len(dst)
//	}
//	inode.w_size(offset + c)
//	log_write(inode.blk)
//	return c, 0
//}

func fs_mkdir(path []string, mode int) int {
	panic("no imp")
	//l := len(path) - 1
	//dirs := path[:l]
	//dfile, err := fs_walk(dirs)
	//if err != 0 {
	//	return err
	//}
	//name := path[l]
	//_, err = fs_create(name, dfile.priv, nil, true)
	//dfile.fileunlock()
	//if err != 0 {
	//	return err
	//}
	//return 0
}

// returns file_t for specified file
func fs_open(path []string, flags int, mode int) (*file_t, int) {
	if flags & O_CREAT != 0 {
		panic("no imp")
		//l := len(path) - 1
		//dirs := path[:l]
		//ret, err := fs_walk(dirs)
		//if err != 0 {
		//	return nil, -ENOENT
		//}
		//name := path[len(path) - 1]
		//cfile, err := fs_create(name, ret.priv, nil, false)
		//ret.fileunlock()
		//return cfile, err
	}

	// send inum get request to root inode daemon
	req := &ireq_t{}
	req.mkget(path)
	iroot.req <- req
	resp := <- req.ack

	if resp.err != 0 {
		return nil, resp.err
	}

	ret := &file_t{resp.gnext}
	return ret, 0
}

// caller must have parpriv's filetree node locked. create file/directory named
// 'name' under directory parpriv.
//func fs_create(name string, parpriv inum, parftnode *ftnode_t,
//    isdir bool) (*file_t, int) {
//	// check if file exists
//	dib, dio := bidecode(int(parpriv))
//	dinode := inode_get(dib, dio, true, I_DIR)
//	defer inode_put(dinode)
//	names, _, emptyvalid, emptyb, emptys := dirents_get(dinode)
//	for _, n := range names {
//		if name == n {
//			return nil, -EEXIST
//		}
//	}
//
//	// allocate new inode
//	newbn, newioff := ialloc()
//	newftnode := parftnode.ensure(newbn)
//	// the new inode may be on the same block as the parent directory.
//	// don't try to lock in this case.
//	if newftnode.l != parftnode.l {
//		newftnode.l.Lock()
//		defer newftnode.l.Unlock()
//	}
//
//	newiblk := bread(newbn)
//	newinode := &inode_t{newiblk, newioff}
//	var itype int
//	if isdir {
//		itype = I_DIR
//	} else {
//		itype = I_FILE
//	}
//	newinode.w_itype(itype)
//	newinode.w_linkcount(1)
//	newinode.w_size(0)
//	newinode.w_major(0)
//	newinode.w_minor(0)
//	newinode.w_indirect(0)
//	for i := 0; i < NIADDRS; i++ {
//		newinode.w_addr(i, 0)
//	}
//	log_write(newiblk)
//	brelse(newiblk)
//
//	// write new directory entry into parpriv referencing newinode
//	var blk *bbuf_t
//	var deoff int
//	if emptyvalid {
//		// use existing dirdata block
//		blk = bread(emptyb)
//		deoff = emptys
//	} else {
//		// allocate new dir data block
//		newddn := balloc()
//		oldsz := dinode.size()
//		nextslot := oldsz/512
//		if nextslot >= NIADDRS {
//			panic("need indirect support")
//		}
//		if dinode.addr(nextslot) != 0 {
//			panic("addr slot allocated")
//		}
//		dinode.w_addr(nextslot, newddn)
//		dinode.w_size(oldsz + 512)
//		log_write(dinode.blk)
//
//		deoff = 0
//		// zero new directory data block
//		blk = bread(newddn)
//		for i := range blk.buf.data {
//			blk.buf.data[i] = 0
//		}
//	}
//
//	// write dir entry
//	ddata := dirdata_t{blk}
//	if ddata.filename(deoff) != "" {
//		panic("dir entry slot is not free")
//	}
//	ddata.w_filename(deoff, name)
//	ddata.w_inodenext(deoff, newbn, newioff)
//	log_write(ddata.blk)
//	brelse(ddata.blk)
//
//	newinum := inum(biencode(newbn, newioff))
//	ret := file_new(newinum, parpriv, newftnode)
//	return ret, 0
//}

type file_t struct {
	priv	inum
}

func file_new(priv inum) *file_t {
	ret := &file_t{priv}
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

func (ic *icache_t) flushto(blk *bbuf_t, ioff int) {
	inode := inode_t{blk, ioff}
	inode.w_itype(ic.itype)
	inode.w_linkcount(ic.links)
	inode.w_size(ic.size)
	inode.w_major(ic.major)
	inode.w_minor(ic.minor)
	inode.w_indirect(ic.indir)
	for i := 0; i < NIADDRS; i++ {
		inode.w_addr(i, ic.addrs[i])
	}
}

type rtype_t int

const (
	GET	rtype_t = iota
	READ
	WRITE
	CREATE
)

type ireq_t struct {
	// request type
	rtype		rtype_t
	get_path	[]string
	rbufs		[][]uint8
	dbufs		[][]uint8
	offset		int
	cr_name		string
	cr_type		int
	ack		chan *iresp_t
}

func (r *ireq_t) mkget(name []string) {
	r.ack = make(chan *iresp_t)
	r.rtype = GET
	r.get_path = name
}

func (r *ireq_t) mkread(dsts [][]uint8, offset int) {
	r.ack = make(chan *iresp_t)
	r.rtype = READ
	r.rbufs = dsts
	r.offset = offset
}

func (r *ireq_t) mkwrite(srcs [][]uint8, offset int) {
	r.ack = make(chan *iresp_t)
	r.rtype = WRITE
	r.dbufs = srcs
	r.offset = offset
}

func (r *ireq_t) mkcreate(nname string, ntype int) {
	r.ack = make(chan *iresp_t)
	r.rtype = CREATE
	r.cr_name = nname
	r.cr_type = ntype
}

type iresp_t struct {
	gnext	inum
	cnext	inum
	count	int
	err	int
}

// a type for an inode block/offset identifier
type inum int

var allidmons	= map[inum]*idaemon_t{}
var idmonl	= sync.Mutex{}

func idaemon_ensure(priv inum) *idaemon_t {
	idmonl.Lock()
	ret, ok := allidmons[priv]
	if !ok {
		ret = idaemon_new(priv)
		allidmons[priv] = ret
		go idaemon(ret)
	}
	idmonl.Unlock()
	return ret
}

// creates a new idaemon struct and fills its inode
func idaemon_new(priv inum) *idaemon_t {
	blkno, ioff := bidecode(int(priv))
	ret := &idaemon_t{}
	ret.priv = priv
	ret.blkno = blkno
	ret.ioff = ioff

	ret.req = make(chan *ireq_t)
	ret.ack = make(chan *iresp_t)

	blk := bread(blkno)
	ret.icache.fill(blk, ioff)
	brelse(blk)
	return ret
}

func idaemon(idm *idaemon_t) {
	iupdate := func() {
		blk := bread(idm.blkno)
		idm.icache.flushto(blk, idm.ioff)
		log_write(blk)
		brelse(blk)
	}
	for {
		r := <- idm.req
		switch r.rtype {
		case GET:
			// is the requester asking for this daemon?
			if len(r.get_path) == 0 {
				ret := &iresp_t{gnext: idm.priv}
				r.ack <- ret
				break
			}
			// lookup next idaemon
			next := r.get_path[0]
			r.get_path = r.get_path[1:]
			npriv, err := idm.fs_get(next)
			if err != 0 {
				ret := &iresp_t{err: err}
				r.ack <- ret
				break
			}
			nextidm := idaemon_ensure(npriv)
			// forward request
			nextidm.req <- r

		case READ:
			read, err := idm.iread(r.rbufs, r.offset)
			ret := &iresp_t{count: read, err: err}
			r.ack <- ret

		case WRITE:
			panic("no imp")
			iupdate()
		case CREATE:
			panic("no imp")
			iupdate()
		default:
			panic("bad req type")
		}
	}
}

func (idm *idaemon_t) iread(dsts [][]uint8, offset int) (int, int) {
	c := 0
	for _, d := range dsts {
		read, err := idm.iread1(d, offset + c)
		c += read
		if err != 0 {
			return c, err
		}
		if c != len(d) {
			break
		}
	}
	return c, 0
}

func (idm *idaemon_t) iread1(dst []uint8, offset int) (int, int) {
	isz := idm.icache.size
	if offset > isz {
		return 0, 0
	}

	sz := len(dst)
	c := 0
	readall := false
	for c < sz && !readall {
		fileoff := offset + c
		whichblk := fileoff/512
		if whichblk >= NIADDRS {
			panic("need indirect support")
		}
		blkn := idm.icache.addrs[whichblk]
		blk := bread(blkn)
		start := fileoff % 512
		bsp := 512 - start
		left := isz - fileoff
		if bsp > left {
			bsp = left
			readall = true
		}
		src := blk.buf.data[start:start+bsp]
		for i, b := range src {
			dst[c + i] = b
		}
		brelse(blk)
		c += len(src)
	}
	return c, 0
}

func (idm *idaemon_t) fs_get(name string) (inum, int) {
	denames, deinodes, _, _, _ := idm.dirents_get()
	for i, n := range denames {
		if name == n {
			return deinodes[i], 0
		}
	}
	return 0, -ENOENT
}

// fetch all directory entries from a directory data block. returns dirent
// names, inums, and index of first empty dirent.
func (idm *idaemon_t) dirents_get() ([]string, []inum, bool, int, int) {
	if idm.icache.itype != I_DIR {
		panic("not a directory")
	}
	isz := idm.icache.size
	sret := make([]string, 0)
	iret := make([]inum, 0)
	var emptyb int
	var emptys int
	evalid := false
	for bn := 0; bn < isz/512; bn++ {
		blkn := idm.icache.addrs[bn]
		blk := bread(blkn)
		dirdata := dirdata_t{blk}
		for i := 0; i < NDIRENTS; i++ {
			fn := dirdata.filename(i)
			if fn == "" {
				if !evalid {
					evalid = true
					emptyb = blkn
					emptys = i
				}
				continue
			}
			sret = append(sret, fn)
			inde := dirdata.inodenext(i)
			iret = append(iret, inde)
		}
		brelse(blk)
	}
	return sret, iret, evalid, emptyb, emptys
}

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

func (sb *superblock_t) recovernum() int {
	return fieldr(&sb.blk.buf.data, 6)
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

func (sb *superblock_t) w_recovernum(n int) {
	fieldw(&sb.blk.buf.data, 6, n)
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
func balloc() int {
	fst := free_start
	flen := free_len
	if fst == 0 || flen == 0 {
		panic("fs not initted")
	}

	fblock.Lock()
	defer fblock.Unlock()

	found := false
	var bit uint
	var blk *bbuf_t
	var blkn int
	var oct int
	// 0 is free, 1 is allocated
	for i := 0; i < flen && !found; i++ {
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
		brelse(blk)
	}
	if !found {
		panic("no free blocks")
	}

	// mark as allocated
	blk.buf.data[oct] |= 1 << bit
	log_write(blk)

	boffset := usable_start
	bitsperblk := 512*8
	return boffset + blkn*bitsperblk + oct*8 + int(bit)
}

var ifreeblk int	= 0
var ifreeoff int 	= 0

// returns block/index of free inode. callers must synchronize access to this
// block via filetree locks.
func ialloc() (int, int) {
	filock.Lock()
	defer filock.Unlock()

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

	ifreeblk = balloc()
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

// use ata pio for fair comparisons against xv6, but i want to use ahci (or
// something) eventually. unlike xv6, we always use disk 0

const(
	ide_bsy = 0x80
	ide_drdy = 0x40
	ide_df = 0x20
	ide_err = 0x01

	ide_cmd_read = 0x20
	ide_cmd_write = 0x30

	ide_rbase = 0x1f0
	ide_rdata = ide_rbase + 0
	ide_rerr = ide_rbase + 1
	ide_rcount = ide_rbase + 2
	ide_rsect = ide_rbase + 3
	ide_rclow = ide_rbase + 4
	ide_rchigh = ide_rbase + 5
	ide_rdrive = ide_rbase + 6
	ide_rcmd = ide_rbase + 7

	ide_allstatus = 0x3f6
)

func ide_wait(chk bool) bool {
	var r int
	for {
		r = runtime.Inb(ide_rcmd)
		if r & (ide_bsy | ide_drdy) == ide_drdy {
			break
		}
	}
	if chk && r & (ide_df | ide_err) != 0 {
		return false
	}
	return true
}

func ide_init() bool {
	irq_unmask(IRQ_DISK)
	ide_wait(false)

	found := false
	for i := 0; i < 1000; i++ {
		r := runtime.Inb(ide_rcmd)
		if r == 0xff {
			fmt.Printf("floating bus!\n")
			break
		} else if r != 0 {
			found = true
			break
		}
	}
	if found {
		fmt.Printf("IDE disk detected\n");
		return true
	}

	fmt.Printf("no IDE disk\n");
	return false
}

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

// it is possible that a goroutine is context switched to a new CPU while doing
// this port io; does this matter? doesn't seem to for qemu...
func ide_start(b *idebuf_t, write bool) {
	ide_wait(false)
	outb := runtime.Outb
	outb(ide_allstatus, 0)
	outb(ide_rcount, 1)
	outb(ide_rsect, b.block & 0xff)
	outb(ide_rclow, (b.block >> 8) & 0xff)
	outb(ide_rchigh, (b.block >> 16) & 0xff)
	outb(ide_rdrive, 0xe0 | ((b.disk & 1) << 4) | (b.block >> 24) & 0xf)
	if write {
		outb(ide_rcmd, ide_cmd_write)
		runtime.Outsl(ide_rdata, unsafe.Pointer(&b.data[0]), 512/4)
	} else {
		outb(ide_rcmd, ide_cmd_read)
	}
}

func ide_daemon() {
	for {
		req := <- ide_request
		if req.buf == nil {
			panic("nil idebuf")
		}
		writing := req.write
		ide_start(req.buf, writing)
		<- ide_int_done
		if !writing {
			// read sector
			if ide_wait(true) {
				runtime.Insl(ide_rdata,
				    unsafe.Pointer(&req.buf.data[0]), 512/4)
			}
		}
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
	ireq := idereq_new(int(b.buf.block), b.dirty, b.buf)
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
	done		chan int
	bcblocks	map[int]*bbuf_t
	waiters		map[int][]*chan *bbuf_t
}

func (bc *bcdaemon_t) init() {
	bc.req = make(chan *bcreq_t)
	bc.done = make(chan int)
	bc.bcblocks = make(map[int]*bbuf_t)
	bc.waiters = make(map[int][]*chan *bbuf_t)
}

// returns a bbuf_t for the specified block
func (bc *bcdaemon_t) bc_read(block int) *bbuf_t {
	if block < 0 {
		panic("bad block")
	}
	ret, ok := bc.bcblocks[block]

	if !ok {
		ireq := idereq_new(block, false, nil)
		ide_request <- ireq
		<- ireq.ack
		nb := &bbuf_t{}
		nb.buf = ireq.buf
		nb.dirty = false
		bc.bcblocks[block] = nb
		bc.chk_evict()
		ret = nb
	}
	return ret
}

func (bc *bcdaemon_t) qadd(blkno int, ack *chan *bbuf_t) {
	q, ok := bc.waiters[blkno]
	if !ok {
		q = make([]*chan *bbuf_t, 1, 8)
		bc.waiters[blkno] = q
		q[0] = ack
		return
	}
	bc.waiters[blkno] = append(q, ack)
}

func (bc *bcdaemon_t) qpop(blkno int) (*chan *bbuf_t, bool) {
	q, ok := bc.waiters[blkno]
	if !ok || len(q) == 0 {
		return nil, false
	}
	ret := q[0]
	bc.waiters[blkno] = q[1:]
	return ret, true
}

func bc_daemon(bc *bcdaemon_t) {
	given := make(map[int]bool)
	for {
		select {
		case r := <- bc.req:
			// block request
			inuse := given[r.blkno]
			if !inuse {
				given[r.blkno] = true
				buf := bc.bc_read(r.blkno)
				r.ack <- buf
			} else {
				bc.qadd(r.blkno, &r.ack)
			}
		case bfin := <- bc.done:
			// relinquish block
			inuse := given[bfin]
			if !inuse {
				panic("relinquish unused block")
			}
			nextc, ok := bc.qpop(bfin)
			if ok {
				buf := bc.bc_read(bfin)
				*nextc <- buf
			} else {
				given[bfin] = false
			}
		}
	}
}

func (bc *bcdaemon_t) chk_evict() {
	nbcbufs := 512
	if len(bc.bcblocks) <= nbcbufs {
		return
	}
	// evict unmodified bbuf
	for i, bb := range bc.bcblocks {
		if !bb.dirty {
			delete(bc.bcblocks, i)
			if len(bc.bcblocks) <= nbcbufs {
				break
			}
		}
	}
	if len(bc.bcblocks) >= nbcbufs {
		panic("bc full of dirty blocks")
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

	if rnum := superb.recovernum(); rnum != 0  {
		panic(fmt.Sprintf("should have ran recover %v", rnum))
	}
	if log.loglen*4 > 512 {
		panic("log destination array doesn't fit in one block")
	}
	dblk := bread(log.logstart)
	for i, lbn := range log.blks {
		// install log destination in the first log block
		writen(dblk.buf.data[:], 4, 4*i, lbn)

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
	// flush first log block
	dblk.writeback()
	brelse(dblk)

	// commit log
	superb.w_recovernum(len(log.blks))
	superb.blk.writeback()

	//rn := superb.recovernum()
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
	superb.w_recovernum(0)
	superb.blk.writeback()

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
