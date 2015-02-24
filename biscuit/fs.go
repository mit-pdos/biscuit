package main

import "fmt"
import "runtime"
import "strings"
import "sync"
import "unsafe"

const NAME_MAX    int = 512

// inum is an fs-independent inode identifier. for biscuit fs it contains an
// inode's block/offset pair
type inum int

var superb_start	int
var superb		superblock_t
var iroot		inum
var free_start		int
var free_len		int
var usable_start	int

// file system journal
var fslog	= log_t{}

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
	// find the first fs block; the build system installs it in block 0 for
	// us
	blk := bc_read(0)
	FSOFF := 506
	superb_start = readn(blk.buf.data[:], 4, FSOFF)
	blk = bc_read(superb_start)
	superb = superblock_t{}
	superb.blk = blk
	a, b := superb.rootinode()
	iroot = inum(biencode(a, b))

	free_start = superb.freeblock()
	free_len = superb.freeblocklen()

	ftroot.nodes = make(map[int]*ftnode_t)
	ftroot.l = new(sync.Mutex)

	ll := superb.loglen()
	if ll < 0 {
		panic("bad log len")
	}

	logstart := free_start + free_len
	fslog.init(logstart, ll)

	usable_start = logstart + ll

	fs_recover()

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
	dblk := bc_read(l.logstart)
	for i := 0; i < rlen; i++ {
		bdest := readn(dblk.buf.data[:], 4, 4*i)
		src := bc_read(l.logstart + 1 + i)
		dst := bc_read(bdest)
		dst.buf.data = src.buf.data
		bc_write(dst)
		bc_flush(dst)
	}

	// clear recovery flag
	superb.w_recovernum(0)
	bc_write(superb.blk)
	bc_flush(superb.blk)
	fmt.Printf("restored %v blocks\n", rlen)
}

// caller must have file's filetree node locked
func fs_read(dst []uint8, file *file_t, offset int) (int, int) {
	fib, fio := bidecode(int(file.priv))
	inode := inode_get(fib, fio, true, I_FILE)
	isz := inode.size()
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
		blkn := inode.addr(whichblk)
		blk := bc_read(blkn)
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
		c += len(src)
	}
	return c, 0
}

// caller must have file's filetree node locked
func fs_write(src []uint8, file *file_t, soffset int, append bool) (int, int) {
	fib, fio := bidecode(int(file.priv))
	inode := inode_get(fib, fio, true, I_FILE)
	isz := inode.size()
	offset := soffset
	if append {
		offset = isz
	}
	// XXX if file length is shortened, when to free old blocks?
	sz := len(src)
	c := 0
	for c < sz {
		fileoff := offset + c
		whichblk := fileoff/512
		if whichblk >= NIADDRS {
			panic("need indirect support")
		}
		blkn := inode.addr(whichblk)
		if blkn == 0 {
			// alocate a new block
			blkn = balloc()
			inode.w_addr(whichblk, blkn)
		}
		blk := bc_read(blkn)
		start := fileoff % 512
		bsp := 512 - start
		left := len(src) - c
		if bsp > left {
			bsp = left
		}
		dst := blk.buf.data[start:start+bsp]
		for i := range dst {
			dst[i] = src[c + i]
		}
		bc_write(blk)
		log_write(blk)
		c += len(dst)
	}
	inode.w_size(offset + c)
	bc_write(inode.blk)
	log_write(inode.blk)
	return c, 0
}

func fs_mkdir(path []string, mode int) int {
	l := len(path) - 1
	dirs := path[:l]
	dfile, err := fs_walk(dirs)
	if err != 0 {
		return err
	}
	name := path[l]
	_, err = fs_create(name, dfile.priv, dfile.ftnode, true)
	dfile.fileunlock()
	if err != 0 {
		return err
	}
	return 0
}

// returns file_t for specified file
func fs_open(path []string, flags int, mode int) (*file_t, int) {
	if flags & O_CREAT != 0 {
		l := len(path) - 1
		dirs := path[:l]
		ret, err := fs_walk(dirs)
		if err != 0 {
			return nil, -ENOENT
		}
		name := path[len(path) - 1]
		cfile, err := fs_create(name, ret.priv, ret.ftnode, false)
		ret.fileunlock()
		return cfile, err
	}
	ret, err := fs_walk(path)
	if err != 0 {
		return nil, err
	}
	ret.fileunlock()
	return ret, 0
}

// returns a file_t with corresponding ftnode_t locked. when err is non-zero,
// no locks are held.
func fs_walk(path []string) (*file_t, int) {
	cftdir := &ftroot
	cftdir.l.Lock()
	cinode := iroot
	var lastinode inum
	for _, p := range path {
		nexti, err := dir_get(p, cinode)
		if err != 0 {
			cftdir.l.Unlock()
			return nil, err
		}
		cftdir = cftdir.txlocknext(nexti)
		lastinode = cinode
		cinode = nexti
	}
	// create the file_t with filetree lock fields
	ret := file_new(cinode, lastinode, cftdir)
	return ret, 0
}

// caller must have parpriv's filetree node locked. create file/directory named
// 'name' under directory parpriv.
func fs_create(name string, parpriv inum, parftnode *ftnode_t,
    isdir bool) (*file_t, int) {
	// check if file exists
	dib, dio := bidecode(int(parpriv))
	dinode := inode_get(dib, dio, true, I_DIR)
	names, _, emptyvalid, emptyb, emptys := dirents_get(dinode)
	for _, n := range names {
		if name == n {
			return nil, -EEXIST
		}
	}

	// allocate new inode and lock it via ftnode
	newbn, newioff := ialloc()
	newftnode := parftnode.ensure(newbn)
	// the new inode may be on the same block as the parent directory.
	// don't try to lock in this case.
	if newftnode.l != parftnode.l {
		newftnode.l.Lock()
		defer newftnode.l.Unlock()
	}

	newiblk := bc_read(newbn)
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
	bc_write(newiblk)
	log_write(newiblk)

	// write new directory entry into parpriv referencing newinode
	var blk *bbuf_t
	var deoff int
	if emptyvalid {
		// use existing dirdata block
		blk = bc_read(emptyb)
		deoff = emptys
	} else {
		// allocate new dir data block
		newddn := balloc()
		oldsz := dinode.size()
		nextslot := oldsz/512
		if nextslot >= NIADDRS {
			panic("need indirect support")
		}
		if dinode.addr(nextslot) != 0 {
			panic("addr slot allocated")
		}
		dinode.w_addr(nextslot, newddn)
		dinode.w_size(oldsz + 512)
		bc_write(dinode.blk)
		log_write(dinode.blk)

		deoff = 0
		// zero new directory data block
		blk = bc_read(newddn)
		for i := range blk.buf.data {
			blk.buf.data[i] = 0
		}
	}

	// write dir entry
	ddata := dirdata_t{blk}
	if ddata.filename(deoff) != "" {
		panic("dir entry slot is not free")
	}
	ddata.w_filename(deoff, name)
	ddata.w_inodenext(deoff, newbn, newioff)
	bc_write(ddata.blk)
	log_write(ddata.blk)

	newinum := inum(biencode(newbn, newioff))
	ret := file_new(newinum, parpriv, newftnode)
	return ret, 0
}

// returns the inum for the file named 'name' in directory inode dirnoden
func dir_get(name string, dirnoden inum) (inum, int) {
	dib, dio := bidecode(int(dirnoden))
	dirnode := inode_get(dib, dio, true, I_DIR)
	denames, deinodes, _, _, _ := dirents_get(dirnode)
	for i, n := range denames {
		if name == n {
			return deinodes[i], 0
		}
	}
	return 0, -ENOENT
}

// fetch all directory entries from a directory data block. returns dirent
// names, inums, and index of first empty dirent.
func dirents_get(dirnode *inode_t) ([]string, []inum, bool, int, int) {
	if dirnode.itype() != I_DIR {
		panic("not a directory")
	}
	sret := make([]string, 0)
	iret := make([]inum, 0)
	var emptyb int
	var emptys int
	evalid := false
	for bn := 0; bn < dirnode.size()/512; bn++ {
		blkn := dirnode.addr(bn)
		blk := bc_read(blkn)
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
	}
	return sret, iret, evalid, emptyb, emptys
}

// fs lock tree. this lock tree is how concurrent fs syscalls to the same
// dir/file are made atomic -- the block cache does not provide any
// synchronization and thus is not thread-safe. the index to the maps is the
// inode block number. there is one ftnode for each file/dir, though an
// ftnode's lock may be referenced from more than one place in the tree.
type ftnode_t struct {
	l	*sync.Mutex
	nodes	map[int]*ftnode_t
	par	*ftnode_t
}

var ftroot	= ftnode_t{}
// map all blocks to locks. ftnodes for files that share an inode should share
// a lock to synchronize access to the inode. only used when creating ftnodes.
var allftlocks	= map[int]*sync.Mutex{}
var allftl	= sync.Mutex{}

// caller must have ftn locked. locks the ftnode_t corresponding to blkno under
// ftn, creating an entry if necessary, and unlocks the ftn.
func (ftn *ftnode_t) txlocknext(priv inum) *ftnode_t {
	blkno, _ := bidecode(int(priv))
	nextnode, ok := ftn.nodes[blkno]
	if !ok {
		// create
		nextnode = ftn.create(blkno)
	}
	// if a directory and next directory entry are on the same inode, just
	// keep it locked.
	if ftn.l != nextnode.l {
		nextnode.l.Lock()
		ftn.l.Unlock()
	}
	return nextnode
}

// caller must have ftn locked. does not unlock ftn or lock the created ftnode.
func (ftn *ftnode_t) create(blkn int) *ftnode_t {
	if _, ok := ftn.nodes[blkn]; ok {
		panic("ftnode exists")
	}

	ret := &ftnode_t{}
	ret.nodes = make(map[int]*ftnode_t)
	ret.par = ftn

	allftl.Lock()
	ftl, ok := allftlocks[blkn]
	if ok {
		ret.l = ftl
	} else {
		ret.l = new(sync.Mutex)
		allftlocks[blkn] = ret.l
	}
	allftl.Unlock()

	ftn.nodes[blkn] = ret
	return ret
}

// caller must have ftn locked. does not unlock ftn or lock the created ftnode.
func (ftn *ftnode_t) ensure(blkn int) *ftnode_t {
	if ret, ok := ftn.nodes[blkn]; ok {
		return ret
	}
	return ftn.create(blkn)
}

// we manage concurrency between files with the file lock tree made of ftnode_t
// objects. file_t is an object to associate ftnode_t objects with open files.
type file_t struct {
	priv		inum
	parpriv		inum
	ftnode		*ftnode_t
}

func (f *file_t) filelock() {
	f.ftnode.l.Lock()
}

func (f *file_t) fileunlock() {
	f.ftnode.l.Unlock()
}

func file_new(priv inum, parpriv inum, node *ftnode_t) *file_t {
	ret := &file_t{}
	ret.priv = priv
	ret.parpriv = parpriv
	ret.ftnode = node
	return ret
}

func inode_get(block int, ioff int, verify bool, exptype int) *inode_t {
	blk := bc_read(block)
	ret := inode_t{blk, ioff}
	itype := ret.itype()
	if verify && itype != exptype {
		panic(fmt.Sprintf("this is not an inode of type %v", exptype))
	}
	return &ret
}

func inode_chk(priv inum, exptype int) {
	b, ioff := bidecode(int(priv))
	inode_get(b, ioff, true, exptype)
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

func (sb *superblock_t) rootinode() (int, int) {
	v := fieldr(&sb.blk.buf.data, 3)
	return bidecode(v)
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
		blk = bc_read(fst + i)
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
	bc_write(blk)
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
	zblk := bc_read(ifreeblk)
	for i := range zblk.buf.data {
		zblk.buf.data[i] = 0
	}
	bc_write(zblk)
	log_write(zblk)
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

var bcblocks		= make(map[int]*bbuf_t)
var bclock		= sync.Mutex{}

// caller must hold bclock
func chk_evict() {
	nbcbufs := 512
	if len(bcblocks) <= nbcbufs {
		return
	}
	// evict unmodified bbuf
	freed := false
	for i, bb := range bcblocks {
		if !bb.dirty {
			delete(bcblocks, i)
			freed = true
			if len(bcblocks) <= nbcbufs {
				break
			}
		}
	}
	if !freed {
		panic("bc full of dirty blocks")
	}
}

// returns a bbuf_t for the specified block
func bc_read(block int) *bbuf_t {
	if block < 0 {
		panic("bad block")
	}
	bclock.Lock()
	ret, ok := bcblocks[block]
	bclock.Unlock()

	if !ok {
		chk_evict()
		ireq := idereq_new(block, false, nil)
		ide_request <- ireq
		<- ireq.ack
		nb := &bbuf_t{}
		nb.buf = ireq.buf
		nb.dirty = false
		bclock.Lock()
		bcblocks[block] = nb
		bclock.Unlock()
		ret = nb

	}
	return ret
}

func bc_write(b *bbuf_t) {
	bclock.Lock()
	b.dirty = true
	bcblocks[int(b.buf.block)] = b
	chk_evict()
	bclock.Unlock()
}

// write b to disk if it is dirty. returns after b has been written to disk.
func bc_flush(b *bbuf_t) {
	if !b.dirty {
		return
	}
	bclock.Lock()
	bcblocks[int(b.buf.block)] = b
	chk_evict()
	bclock.Unlock()

	block := int(b.buf.block)
	ireq := idereq_new(block, true, b.buf)
	ide_request <- ireq
	<- ireq.ack
	b.dirty = false
}

// for testing only; unsafe
func bc_writeflush(b *bbuf_t) {
	// XXX takes lock twice
	bc_write(b)
	bc_flush(b)
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
	if rnum := superb.recovernum(); rnum != 0  {
		panic(fmt.Sprintf("should have ran recover %v", rnum))
	}
	if log.loglen*4 > 512 {
		panic("log destination array doesn't fit in one block")
	}
	dblk := bc_read(log.logstart)
	for i, lbn := range log.blks {
		// install log destination in the first log block
		writen(dblk.buf.data[:], 4, 4*i, lbn)

		// and write block to the log
		src := bc_read(lbn)
		logblkn := log.logstart + 1 + i
		// XXX unnecessary read
		dst := bc_read(logblkn)
		dst.buf.data = src.buf.data
		bc_write(dst)
		bc_flush(dst)
	}
	// flush first log block
	bc_write(dblk)
	bc_flush(dblk)

	// commit log
	superb.w_recovernum(len(log.blks))
	bc_write(superb.blk)
	bc_flush(superb.blk)

	//rn := superb.recovernum()
	//if rn > 0 {
	//	runtime.Crash()
	//}

	// the log is committed. if we crash while installing the blocks to
	// their destinations, we should be able to recover
	for _, lbn := range log.blks {
		blk := bc_read(lbn)
		if !blk.dirty {
			panic("log blocks must be dirty/in cache")
		}
		bc_flush(blk)
	}

	// success; clear flag indicating to recover from log
	superb.w_recovernum(0)
	bc_write(superb.blk)
	bc_flush(superb.blk)

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
	fslog.incoming <- int(b.buf.block)
}
