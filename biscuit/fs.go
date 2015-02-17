package main

import "fmt"
import "runtime"
import "strings"
import "sync"
import "unsafe"

const NAME_MAX    int = 512

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

var fsblock_start	int
var superb		superblock_t

// free block bitmap lock
var fblock	= sync.Mutex{}

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
}

// returns fs device identifier and inode
func fs_open(path []string, flags int, mode int) (int, int) {
	fmt.Printf("fs open %q\n", path)

	dirnode, inode := fs_walk(path)
	if flags & O_CREAT == 0 {
		if inode == -1 {
			return 0, -ENOENT
		}
		return inode, 0
	}

	name := path[len(path) - 1]
	inode = bisfs_create(name, dirnode)

	return inode, 0
}

// returns inode and error
func fs_walk(path []string) (int, int) {
	iroot := fsblock_start
	cinode := iroot
	for _, p := range path {
		nexti, err := bisfs_dir_get(cinode, p)
		if err != 0 {
			return 0, err
		}
		cinode = nexti
	}
	return cinode, 0
}

func bisfs_create(name string, dirnode int) int {
	return -1
}

func bisfs_dir_get(dnode int, name string) (int, int) {
	//bn := itobn(dnode)
	blk := bisblk_t{nil}
	inode, err := blk.lookup(name)
	return inode, err
}

type bisblk_t struct {
	data	*[512]byte
}

func (b *bisblk_t) lookup(name string) (int, int) {
	return 0, 0
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
// 16-23,  size in blocks
// 24-31,  major
// 32-39,  minor
// 40-47,  indirect block
// 48-80,  block addresses
type inode_t struct {
	raw	*[512]uint8
}

// inode file types
const(
	I_FIRST = 0
	I_INVALID = I_FIRST
	I_FILE
	I_DIR
	I_DEV
	I_LAST = I_DEV

	NIADDRS = 10
	NIWORDS = 6 + NIADDRS
)

func ifield(iidx int, fieldn int) int {
	return iidx*NIWORDS + fieldn
}

// iidx is the inode index; necessary since there are four inodes in one block
func (ind *inode_t) itype(iidx int) int {
	it := fieldr(ind.raw, ifield(iidx, 0))
	if it < I_FIRST || it > I_LAST {
		panic("weird inode type")
	}
	return it
}

func (ind *inode_t) linkcount(iidx int) int {
	return fieldr(ind.raw, ifield(iidx, 1))
}

func (ind *inode_t) bsize(iidx int) int {
	return fieldr(ind.raw, ifield(iidx, 2))
}

func (ind *inode_t) major(iidx int) int {
	return fieldr(ind.raw, ifield(iidx, 3))
}

func (ind *inode_t) minor(iidx int) int {
	return fieldr(ind.raw, ifield(iidx, 4))
}

func (ind *inode_t) indirect(iidx int) int {
	return fieldr(ind.raw, ifield(iidx, 5))
}

func (ind *inode_t) addr(iidx int, i int) int {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	return fieldr(ind.raw, ifield(iidx, addroff + i))
}

func (ind *inode_t) w_itype(iidx int, n int) {
	if n < I_FIRST || n > I_LAST {
		panic("weird inode type")
	}
	fieldw(ind.raw, ifield(iidx, 0), n)
}

func (ind *inode_t) w_linkcount(iidx int, n int) {
	fieldw(ind.raw, ifield(iidx, 1), n)
}

func (ind *inode_t) w_bsize(iidx int, n int) {
	fieldw(ind.raw, ifield(iidx, 2), n)
}

func (ind *inode_t) w_major(iidx int, n int) {
	fieldw(ind.raw, ifield(iidx, 3), n)
}

func (ind *inode_t) w_minor(iidx int, n int) {
	fieldw(ind.raw, ifield(iidx, 4), n)
}

// blk is the block number and iidx in the index of the inode on block blk.
func (ind *inode_t) w_indirect(iidx int, blk int) {
	fieldw(ind.raw, ifield(iidx, 5), blk)
}

func (ind *inode_t) w_addr(iidx int, i int, blk int) {
	if i < 0 || i > NIADDRS {
		panic("bad inode block index")
	}
	addroff := 6
	fieldw(ind.raw, ifield(iidx, addroff + i), blk)
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
