package main

import "fmt"
import "sync"
import "unsafe"
import "strconv"

// A block has a lock, since, it may store an inode block, which has 4 inodes,
// and we need to ensure that hat writes aren't lost due to concurrent inode
// updates.  The inode code is careful about releasing lock.  Other types of
// blocks not shared and the caller releases the lock immediately.
//
// The data of a block is a page allocated from the page allocator.  The disk
// DMAs to and from the physical address for the page.  data is the virtual
// address for the page.
//

// If you change this, you must change corresponding constants in mkbdisk.py,
// fs.go, litc.c (fopendir, BSIZE), usertests.c (BSIZE).
const BSIZE=4096

const bdev_debug = false
	
type bdev_block_t struct {
	sync.Mutex
	disk	int
	block	int
	pa      pa_t
	data	*bytepg_t
	s       string
}

func (blk *bdev_block_t) key() int {
	return blk.block
}

func (blk *bdev_block_t) evict() {
	if bdev_debug {
		fmt.Printf("evict: block %v %#x %v\n", blk.block, blk.pa, refcnt(blk.pa))
	}
	blk.free_page()
}

func (blk *bdev_block_t) evictnow() bool {
	return false
}

func bdev_make(block int, pa pa_t, s string) *bdev_block_t {
	b := &bdev_block_t{};
	b.block = block
	b.pa = pa
	b.data = (*bytepg_t)(unsafe.Pointer(dmap(pa)))
	b.s = s
	return b
}

func (blk *bdev_block_t) new_page() {
	_, pa, ok := refpg_new()
	if !ok {
		panic("oom during bdev.new_page")
	}
	blk.pa = pa
	blk.data = (*bytepg_t)(unsafe.Pointer(dmap(pa)))
	refup(blk.pa)
}

func (blk *bdev_block_t) free_page() {
	refdown(blk.pa)
}

func bdev_block_new(block int, s string) *bdev_block_t {
	b := bdev_make(block, pa_t(0), s)
	b.new_page()
	return b
}

func (b *bdev_block_t) bdev_write() {
	if bdev_debug {
		fmt.Printf("bdev_write %v %v\n", b.block, b.s)
	}
	if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
		panic("write\n")
	}
	req := bdev_req_new([]*bdev_block_t{b}, BDEV_WRITE, true)
	if ahci_start(req) {
		<- req.ackCh
	}
} 

func (b *bdev_block_t) bdev_write_async() {
	if bdev_debug {
		fmt.Printf("bdev_write_async %v %s\n", b.block, b.s)
	}
	// if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
	//	panic("write_async\n")
	//}
	ider := bdev_req_new([]*bdev_block_t{b}, BDEV_WRITE, false)
	ahci_start(ider)
}

func (b *bdev_block_t) bdev_read() {
	ider := bdev_req_new([]*bdev_block_t{b}, BDEV_READ, true)
	if ahci_start(ider) {
		<- ider.ackCh
	}
	if bdev_debug {
		fmt.Printf("bdev_read %v %v %#x %#x\n", b.block, b.s, b.data[0], b.data[1])
	}
	
	// XXX sanity check, but ignore it during recovery
	if b.data[0] == 0xc && b.data[1] == 0xc {
		fmt.Printf("WARNING: %v %v\n", b.s, b.block)
	}
	
}

func bdev_flush() {
	ider := bdev_req_new(nil, BDEV_FLUSH, true)
	if ahci_start(ider) {
		<- ider.ackCh
	}
}


// block cache, all device interactions run through block cache.
//
// The cache returns a pointer to a block_dev_t.  There is *one* bdev_block_t
// for a block number (and physical page associated with that blockno).  Callers
// share same block_dev_t (and physical page) for a block. The callers must
// coordinate using the lock of the block.
//
// When a reference to a bdev block in the cache is sent to the log daemon, or
// handed to the disk driver, we increment the refcount on the physical page
// using bcache_refup(). When code (including driver) is done with a bdev_block,
// it decrements the reference count with bdev_relse (e.g., in the driver
// interrupt handler).
//

var brefcache = make_refcache(syslimit.blocks, false)

// returns the reference to a locked buffer
func bref(blk int, s string) (*bdev_block_t, bool, err_t) {
	ref, err := brefcache.lookup(blk, s)
	if err != 0 {
		fmt.Printf("bref %v\n", err)
		return nil, false, err
	}
	defer ref.Unlock()

	created := false
	if !ref.valid {
		if bdev_debug {
			fmt.Printf("bref fill %v %v\n", blk, s)
		}
		buf := bdev_make(blk, pa_t(0), s)
		ref.obj = buf
		ref.valid = true
		created = true
	}
	b := ref.obj.(*bdev_block_t)
	b.Lock()
	b.s = s
	return b, created, err
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_relse when done with buf.
func bcache_get_fill(blkn int, s string, lock bool) (*bdev_block_t, err_t) {
	b, created, err := bref(blkn, s)

	if bdev_debug {
		fmt.Printf("bcache_get_fill: %v %v created? %v\n", blkn, s, created)
	}

	if err != 0 {
		return nil, err
	}
	
	if created {
		b.new_page()
		b.bdev_read() // fill in new bdev_cache entry
	}
	if !lock {
		b.Unlock()
	}
	return b, 0
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bcache_relse when done with buf
func bcache_get_zero(blkn int, s string, lock bool) (*bdev_block_t, err_t) {
	b, created, err := bref(blkn, s)
	if bdev_debug {
		fmt.Printf("bcache_get_zero: %v %v %v\n", blkn, s, created)
	}
	if err != 0 {
		return nil, err
	}
	if created {
		b.new_page()   // zero
	} 
	if !lock {
		b.Unlock()
	}
	return b, 0
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bcache_relse when done with buf
func bcache_get_nofill(blkn int, s string, lock bool) (*bdev_block_t, err_t) {
	b, created, err := bref(blkn, s)
	if bdev_debug {
		fmt.Printf("bcache_get_nofill1: %v %v %v\n", blkn, s, created)
	}
	if err != 0 {
		return nil, err
	}
	if created {
		b.new_page()   // XXX a non-zero page would be fine
	}
	if !lock {
		b.Unlock()
	}
	return b, 0
}

func bcache_write(b *bdev_block_t) {
	brefcache.refup(b, "bcache_write")
	b.bdev_write()
}

func bcache_write_async(b *bdev_block_t) {
	brefcache.refup(b, "bcache_write_async")
	b.bdev_write_async()
}

// blks must be contiguous on disk
func bcache_write_async_blks(blks []*bdev_block_t) {
	if bdev_debug {
		fmt.Printf("bcache_write_async_blks %v\n", len(blks))
	}
	if len(blks) == 0  {
		panic("bcache_write_async_blks\n")
	}
	n := blks[0].block-1
	for _, b := range blks {
		// sanity check
		if b.block != n + 1 {
			panic("not contiguous\n")
		}
		n++
		brefcache.refup(b, "bcache_write_async_blks")
	}
	// one request for all blks
	ider := bdev_req_new(blks, BDEV_WRITE, false)
	ahci_start(ider)
}


func bcache_refup(b *bdev_block_t, s string) {
	brefcache.refup(b, s)
}

func bcache_relse(b *bdev_block_t, s string) {
	brefcache.refdown(b, s)
	if bdev_debug {
		fmt.Printf("bcache_relse: %v %v\n", b.block, s)
	}
}

func bcache_stat() string {
	s := "bcache: size "
	s += strconv.Itoa(len(brefcache.refs))
	s += " #evictions "
	s += strconv.Itoa(brefcache.nevict)
	s += " #live "
	s += strconv.Itoa(brefcache.nlive())
	s += "\n"
	return s
}

func bdev_test() {
	return
	
	fmt.Printf("disk test\n")

	const N = 3

	wbuf := new([N]*bdev_block_t)

	for b := 0; b < N; b++ {
		wbuf[b] = bdev_block_new(b, "disktest")
	}
	for j := 0; j < 100; j++ {

		for b := 0; b < N; b++ {
			fmt.Printf("req %v,%v\n", j, b)

			for i,_ := range wbuf[b].data {
				wbuf[b].data[i] = uint8(b)
			}
			wbuf[b].bdev_write_async()
		}
		bdev_flush()
		for b := 0; b < N; b++ {
			rbuf, err := bcache_get_fill(b, "read test", false)
			if err != 0 {
				panic("bdev_test\n")
			}
			for i, v := range rbuf.data {
				if v != uint8(b) {
					fmt.Printf("buf %v i %v v %v\n", j, i, v)
					panic("bdev_test\n")
				}
			}
		}
	}
	panic("disk test passed\n")
}


// Block allocator

type ballocater_t struct {
	alloc *allocater_t
	first int
}
var ballocater *ballocater_t

func balloc_init(start,len, first int) {
	ballocater = &ballocater_t{}
	ballocater.alloc = make_allocater(start, len)
	fmt.Printf("first datablock %v\n", first)
	ballocater.first = first
}

// allocates a block, marking it used in the free block bitmap. free blocks and
// log blocks are not accounted for in the free bitmap; all others are. balloc
// should only ever acquire fblock.
func balloc1() (int, err_t) {
	blkn, err := ballocater.alloc.alloc()
	if err != 0 {
		return 0, err
	}
	return blkn+ballocater.first, err
}

func balloc() (int, err_t) {
	ret, err := balloc1()
	if err != 0 {
		return 0, err
	}
	if ret < 0 {
		panic("balloc: bad blkn")
	}
	if ret >= superb.lastblock() {
		fmt.Printf("blkn %v last %v\n", ret, superb.lastblock())
		return 0, -ENOMEM
	}
	blk, err := bcache_get_zero(ret, "balloc", true)
	if err != 0 {
		return 0, err
	}
	if bdev_debug {
		fmt.Printf("balloc: %v\n", ret)
	}

	var zdata [BSIZE]uint8
	copy(blk.data[:], zdata[:])
	blk.Unlock()
	blk.log_write()
	bcache_relse(blk, "balloc")
	return ret, 0
}

func bfree(blkno int) err_t {
	if bdev_debug {
		fmt.Printf("bfree: %v\n", blkno)
	}
	blkno -= ballocater.first
	if blkno < 0 {
		panic("bfree")
	}
	return ballocater.alloc.free(blkno)
}

func balloc_stat() string {
	return "balloc " + ballocater.alloc.stat()
}
