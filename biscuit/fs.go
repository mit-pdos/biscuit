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

// we manage concurrency between files with the file lock tree made of ftnode_t
// objects. file_t is an object to associate ftnode_t objects with open files.
type file_t struct {
	priv		inum
	parpriv		inum
	ftnode		*ftnode_t
}

var fsblock_start	int
var superb		superblock_t
var iroot		inum

// free block bitmap lock
var fblock	= sync.Mutex{}

func path_sanitize(path string) []string {
	sp := strings.Split(path, "/")
	nn := []string{}
	for _, s := range sp {
		if s != "" {
			nn = append(nn, s)
		}
	}
	return nn
}

func fs_init() {
	// find the first fs block; the build system installs it in block 0 for
	// us
	blk := bc_read(0)
	FSOFF := 506
	fsblock_start = readn(blk.buf.data[:], 4, FSOFF)
	blk = bc_read(fsblock_start)
	superb = superblock_t{}
	superb.raw = &blk.buf.data
	superb.blk = blk
	a, b := superb.rootinode()
	iroot = inum(biencode(a, b))

	ftroot.nodes = make(map[int]*ftnode_t)
}

func fs_read(dst []uint8, file *file_t, offset int) (int, int) {
	fib, fio := bidecode(int(file.priv))
	inode := inode_get(fib, fio, false, 0)
	if inode.itype() != I_FILE {
		panic("no imp")
	}
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
		end := 512
		left := isz - fileoff
		if end > left {
			end = start + left
			readall = true
		}
		src := blk.buf.data[start:end]
		for i, b := range src {
			dst[c + i] = b
		}
		c += len(src)
	}
	return c, 0
}

// returns file_t for specified file
func fs_open(path []string, flags int, mode int) (*file_t, int) {
	ret, err := fs_walk(path)
	// open does not manipulate the file, thus don't need it locked
	if err == 0 {
		ret.ftnode.l.Unlock()
	}
	if flags & O_CREAT == 0 {
		if err != 0 {
			return nil, -ENOENT
		}
		return ret, 0
	}

	name := path[len(path) - 1]
	cfile, err := fs_create(name, ret.parpriv, ret.ftnode.par)
	return cfile, err
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

// create file named 'name' under parent directory parpriv
func fs_create(name string, parpriv inum, parnode *ftnode_t) (*file_t, int) {
	panic("no imp")
	return nil, -1
}

func dir_get(name string, dirnoden inum) (inum, int) {
	dib, dio := bidecode(int(dirnoden))
	dirnode := inode_get(dib, dio, true, I_DIR)
	denames, deinodes := dirents_get(dirnode)
	for i, n := range denames {
		if name == n {
			return deinodes[i], 0
		}
	}
	return 0, -ENOENT
}

func dirents_get(dirnode *inode_t) ([]string, []inum) {
	if dirnode.itype() != I_DIR {
		panic("not a directory")
	}
	sret := make([]string, 0)
	iret := make([]inum, 0)
	for bn := 0; bn < dirnode.size()/512; bn++ {
		blk := bc_read(dirnode.addr(bn))
		dirdata := dirdata_t{&blk.buf.data}
		for i := 0; i < NDIRENTS; i++ {
			fn := dirdata.filename(i)
			if fn == "" {
				continue
			}
			sret = append(sret, fn)
			a, b := dirdata.inodenext(i)
			iret = append(iret, inum(biencode(a, b)))
		}
	}
	return sret, iret
}

// fs lock tree. this lock tree is how concurrent fs syscalls to the same
// dir/file are made atomic -- the block cache does not provide any
// synchronization and thus is not thread-safe. the index to the maps is the
// inode block number.  the inode block number is the key because then accesses
// to inodes on the same block are synchronized.
type ftnode_t struct {
	l	sync.Mutex
	nodes	map[int]*ftnode_t
	par	*ftnode_t
}

var ftroot	= ftnode_t{}

// caller must have ftn locked. locks the ftnode_t corresponding to blkno under
// ftn, creating an entry if necessary, and unlocks the ftn.
func (ftn *ftnode_t) txlocknext(priv inum) *ftnode_t {
	blkno, _ := bidecode(int(priv))
	nextnode, ok := ftn.nodes[blkno]
	if !ok {
		// create
		nnode := &ftnode_t{}
		nnode.nodes = make(map[int]*ftnode_t)
		nnode.par = ftn
		ftn.nodes[blkno] = nnode
		nextnode = nnode
	}
	nextnode.l.Lock()
	ftn.l.Unlock()
	return nextnode
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
	ret := inode_t{&blk.buf.data, ioff}
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
type superblock_t struct {
	l	sync.Mutex
	raw	*[512]uint8
	blk	*bbuf_t
}

func (sb *superblock_t) freeblock() int {
	return fieldr(sb.raw, 0)
}

func (sb *superblock_t) freeblocklen() int {
	return fieldr(sb.raw, 1)
}

func (sb *superblock_t) loglen() int {
	return fieldr(sb.raw, 2)
}

func (sb *superblock_t) rootinode() (int, int) {
	v := fieldr(sb.raw, 3)
	return bidecode(v)
}

func (sb *superblock_t) lastblock() int {
	return fieldr(sb.raw, 4)
}

func (sb *superblock_t) freeinode() int {
	return fieldr(sb.raw, 5)
}

func (sb *superblock_t) w_freeblock(n int) {
	fieldw(sb.raw, 0, n)
}

func (sb *superblock_t) w_freeblocklen(n int) {
	fieldw(sb.raw, 1, n)
}

func (sb *superblock_t) w_loglen(n int) {
	fieldw(sb.raw, 2, n)
}

func (sb *superblock_t) w_rootinode(blk int, iidx int) {
	fieldw(sb.raw, 3, biencode(blk, iidx))
}

func (sb *superblock_t) w_lastblock(n int) {
	fieldw(sb.raw, 4, n)
}

func (sb *superblock_t) w_freeinode(n int) {
	fieldw(sb.raw, 5, n)
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
	raw	*[512]uint8
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
	NIWORDS = 6 + NIADDRS
)

func ifield(iidx int, fieldn int) int {
	return iidx*NIWORDS + fieldn
}

// iidx is the inode index; necessary since there are four inodes in one block
func (ind *inode_t) itype() int {
	it := fieldr(ind.raw, ifield(ind.ioff, 0))
	if it < I_FIRST || it > I_LAST {
		panic(fmt.Sprintf("weird inode type %d", it))
	}
	return it
}

func (ind *inode_t) linkcount() int {
	return fieldr(ind.raw, ifield(ind.ioff, 1))
}

func (ind *inode_t) size() int {
	return fieldr(ind.raw, ifield(ind.ioff, 2))
}

func (ind *inode_t) major() int {
	return fieldr(ind.raw, ifield(ind.ioff, 3))
}

func (ind *inode_t) minor() int {
	return fieldr(ind.raw, ifield(ind.ioff, 4))
}

func (ind *inode_t) indirect() int {
	return fieldr(ind.raw, ifield(ind.ioff, 5))
}

func (ind *inode_t) addr(i int) int {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	return fieldr(ind.raw, ifield(ind.ioff, addroff + i))
}

func (ind *inode_t) w_itype(n int) {
	if n < I_FIRST || n > I_LAST {
		panic("weird inode type")
	}
	fieldw(ind.raw, ifield(ind.ioff, 0), n)
}

func (ind *inode_t) w_linkcount(n int) {
	fieldw(ind.raw, ifield(ind.ioff, 1), n)
}

func (ind *inode_t) w_size(n int) {
	fieldw(ind.raw, ifield(ind.ioff, 2), n)
}

func (ind *inode_t) w_major(n int) {
	fieldw(ind.raw, ifield(ind.ioff, 3), n)
}

func (ind *inode_t) w_minor(n int) {
	fieldw(ind.raw, ifield(ind.ioff, 4), n)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *inode_t) w_indirect(blk int) {
	fieldw(ind.raw, ifield(ind.ioff, 5), blk)
}

func (ind *inode_t) w_addr(i int, blk int) {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	fieldw(ind.raw, ifield(ind.ioff, addroff + i), blk)
}

// directory data format
// 0-13,  file name characters
// 14-21, inode block/offset
// ...repeated, totaling 23 times
type dirdata_t struct {
	raw	*[512]uint8
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
	sl := dir.raw[st : st + DNAMELEN]
	ret := make([]byte, 0)
	for _, c := range sl {
		if c != 0 {
			ret = append(ret, c)
		}
	}
	return string(ret)
}

func (dir *dirdata_t) inodenext(didx int) (int, int) {
	st := doffset(didx, 14)
	v := readn(dir.raw[:], 8, st)
	return bidecode(v)
}

func (dir *dirdata_t) w_filename(didx int, fn string) {
	st := doffset(didx, 0)
	sl := dir.raw[st : st + DNAMELEN]
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
	writen(dir.raw[:], 8, st, v)
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
// log blocks are not accounted for in the free bitmap; all others are
func balloc() int {
	fst := superb.freeblock()
	flen := superb.freeblocklen()
	loglen := superb.loglen()

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

	boffset := fst + flen + loglen
	bitsperblk := 512*8
	return boffset + blkn*bitsperblk + oct*8 + int(bit)
}

func init_8259() {
	// the piix3 provides two 8259 compatible pics. the runtime masks all
	// irqs for us.
	outb := func(reg int, val int) {
		runtime.Outb(int32(reg), int32(val))
		cdelay(1)
	}
	pic1 := 0x20
	pic1d := pic1 + 1
	pic2 := 0xa0
	pic2d := pic2 + 1

	runtime.Cli()

	// master pic
	// start icw1: icw4 required
	outb(pic1, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	outb(pic1d, IRQ_BASE)
	// icw3, cascaded mode
	outb(pic1d, 4)
	// icw4, auto eoi, intel arch mode.
	outb(pic1d, 3)

	// slave pic
	// start icw1: icw4 required
	outb(pic2, 0x11)
	// icw2, int base -- irq # will be added to base, then delivered to cpu
	outb(pic2d, IRQ_BASE + 8)
	// icw3, slave identification code
	outb(pic2d, 2)
	// icw4, auto eoi, intel arch mode.
	outb(pic2d, 3)

	// ocw3, enable "special mask mode" (??)
	outb(pic1, 0x68)
	// ocw3, read irq register
	outb(pic1, 0x0a)

	// ocw3, enable "special mask mode" (??)
	outb(pic2, 0x68)
	// ocw3, read irq register
	outb(pic2, 0x0a)

	// enable slave 8259
	irq_unmask(2)
	// all irqs go to CPU 0 until redirected

	runtime.Sti()
}

var intmask uint16 	= 0xffff

func irq_unmask(irq int) {
	if irq < 0 || irq > 16 {
		panic("weird irq")
	}
	pic1 := int32(0x20)
	pic1d := pic1 + 1
	pic2 := int32(0xa0)
	pic2d := pic2 + 1
	intmask = intmask & ^(1 << uint(irq))
	dur := int32(intmask)
	runtime.Outb(pic1d, dur)
	runtime.Outb(pic2d, dur >> 8)
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

func ide_init() {
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
	} else {
		fmt.Printf("no IDE disk\n");
	}
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

func bc_writeflush(b *bbuf_t) {
	// XXX takes lock twice
	bc_write(b)
	bc_flush(b)
}
