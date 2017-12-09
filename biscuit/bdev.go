package main

import "fmt"
import "sync"
import "unsafe"

// A block has a lock, since, it may store an inode block, which has 4 inodes,
// and we need to ensure that hat writes aren't lost due to concurrent inode
// updates.  The inode code is careful about releasing lock.  Other types of
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

// If you change this, you must change corresponding constants in mkbdisk.py,
// fs.go, litc.c (fopendir, BSIZE), usertests.c (BSIZE).
const BSIZE=4096

type bdev_block_t struct {
	sync.Mutex
	disk	int
	block	int
	pa      pa_t
	data	*[BSIZE]uint8
	s       string
}

func bdev_make(block int, pa pa_t, s string) *bdev_block_t {
	b := &bdev_block_t{};
	b.block = block
	b.pa = pa
	b.data = (*[BSIZE]uint8)(unsafe.Pointer(dmap(pa)))
	b.s = s
	return b
}

func (b *bdev_block_t) relse() {
	// fmt.Printf("bdev.unlock %v\n", b.block)
	b.Unlock()
}

func (blk *bdev_block_t) new_page() {
	_, pa, ok := refpg_new()
	if !ok {
		panic("oom during bdev.new_page")
	}
	blk.pa = pa
	blk.data = (*[BSIZE]uint8)(unsafe.Pointer(dmap(pa)))
	blk.bdev_refup("new_page")
}

func bdev_block_new(block int, s string) *bdev_block_t {
	b := bdev_make(block, pa_t(0), s)
	b.new_page()
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

func (b *bdev_block_t) purge() {
	fmt.Printf("bdev_purge %v %v\n", b.block, b.bdev_refcnt())
	// b.bdev_refdown("bdev_purge")
	// delete(bdev_cache.blks, b.block)  when we are done 
}

// increment ref to keep the page around until write completes. interrupt routine
// decrecements.
func (b *bdev_block_t) bdev_write() {
	if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
		panic("write\n")
	}
	b.bdev_refup("bdev_write")
	req := bdev_req_new(b, BDEV_WRITE, true)
	if ahci_start(req) {
		<- req.ackCh
	}
} 

// increment ref to keep the page around until write completes.  interrupt
// routine decrecements.
func (b *bdev_block_t) bdev_write_async() {
	if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
		panic("write_async\n")
	}
	b.bdev_refup("bdev_write_async")
	ider := bdev_req_new(b, BDEV_WRITE, false)
	ahci_start(ider)
}

func (b *bdev_block_t) bdev_read() {
	ider := bdev_req_new(b, BDEV_READ, true)
	if ahci_start(ider) {
		<- ider.ackCh
	}
	//fmt.Printf("fill %v %#x %#x\n", buf.block, buf.data[0], buf.data[1])
	if b.data[0] == 0xc && b.data[1] == 0xc {
		fmt.Printf("fall: %v %v\n", b.s, b.block)
		panic("xxx\n")
	}
	
}

// after bdev_read_block returns, the caller owns the block.  the caller must
// call bdev_refdown when it is done with the buffer
func bdev_read_block(blkn int, s string) *bdev_block_t {
	buf := bdev_make(blkn, pa_t(0), s)
	buf.new_page()
	buf.bdev_read()
	return buf
}

func bdev_flush() {
	ider := bdev_req_new(nil, BDEV_FLUSH, true)
	if ahci_start(ider) {
		<- ider.ackCh
	}
}


// block cache. The block cache stores inode blocks, free bit-map blocks,
// metadata blocks. File blocks are stored in the inode's page cache, which also
// interacts with the VM system.
//
// The cache returns a pointer to a block_dev_t.  There is *one* bdev_block_t
//for a block number (and physical page associated with that blockno).  Callers
//share same block_dev_t (and physical page) for a block. The callers must
//coordinate using the lock of the block.
//
// The log layer's log_write pins blocks in the cache to avoid eviction, so that
// a read always sees the last write.
//
// XXX need to support dirty bit too and eviction.
//

// XXX XXX XXX must count against mfs limit
type bdev_cache_t struct {
	// inode blkno -> disk contents
	blks		map[int]*bdev_block_t
	sync.Mutex
}

var bdev_cache = bdev_cache_t{}

func bdev_init() {
	bdev_cache.blks = make(map[int]*bdev_block_t, 30)

	bdev_test()
}

func bdev_cache_present(blkn int) bool {
	bdev_cache.Lock()
	_, ok := bdev_cache.blks[blkn]
	bdev_cache.Unlock()
	return ok
}

// returns locked buf with refcnt on page bumped up by 1. The caller must call
// bdev_refdown when done with buf.  Return true if block was created in the
// cache, false if it was already present.
func bdev_get_empty(blkn int, s string) (*bdev_block_t, bool) {
	if blkn < superb_start {
		panic("no")
	}
	bdev_cache.Lock()

	// fmt.Printf("bdev_get_empty: %v\n", blkn)
	
	if obuf, ok := bdev_cache.blks[blkn]; ok {
		bdev_cache.Unlock()
		obuf.Lock()
		obuf.s = s
		return obuf, false
	}
	buf := bdev_make(blkn, pa_t(0), s)
	bdev_cache.blks[blkn] = buf
	buf.Lock()
	bdev_cache.Unlock()
	return buf, true
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf.
func bdev_get_fill(blkn int, s string, pa pa_t) *bdev_block_t {
	b, created := bdev_get_empty(blkn, s)

	// fmt.Printf("bdev_get_fill: %v\n", blkn)

	if created {
		if pa == 0 {
			b.new_page()
		} else {
			b.pa = pa
			b.bdev_refup("bdev_get_fill1")
		}
		b.bdev_read() // fill in new bdev_cache entry
	}
	if pa != 0 && pa != b.pa {
		panic("bdev_get_fill")
	}
	b.bdev_refup("bdev_get_fill2")
	return b
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf
func bdev_get_zero(blkn int, s string) *bdev_block_t {
	b, created := bdev_get_empty(blkn, s)
	// fmt.Printf("bdev_get_zero: %v\n", blkn)
	if created {
		b.new_page()   // zero
	} else {
		var zdata [BSIZE]uint8
		*b.data = zdata
	}
	b.bdev_refup("bdev_get_zero")
	return b
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf
func bdev_get_nofill(blkn int, s string) *bdev_block_t {
	b, created := bdev_get_empty(blkn, s)
	// fmt.Printf("bdev_get_zero: %v\n", blkn)
	if created {
		b.new_page()   // XXX a non-zero page would be fine
	}
	b.bdev_refup("bdev_get_nofill")
	return b
}

func print_live_blocks() {
	fmt.Printf("bdev cache\n")
	for _, b := range bdev_cache.blks {
		fmt.Printf("block %v\n", b)
	}
}

func bdev_test() {
	return
	
	fmt.Printf("disk test\n")

	ahci_debug = true
	
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
			rbuf := bdev_read_block(b, "read test")
			
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
