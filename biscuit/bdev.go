package main

import "fmt"
import "sync"
import "unsafe"

// A block has a lock, since, it may store an inode block, which has 4 inodes,
// and we need to ensure that hat writes aren't lost due to concurrent inode
// updates.  the inode code is careful about releasing lock.  other types of
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
// Two separately allocated block_dev_t instances may have the same block number
// but not share a physical page.  For example, the log allocates a block_dev_t
// for a block number which may also in the block cache, but has a diferent
// physical page.
type bdev_block_t struct {
	sync.Mutex
	disk	int
	block	int
	pa      pa_t
	data	*[512]uint8
	s       string
}

func (b *bdev_block_t) relse() {
	b.Unlock()
}

func bdev_block_new(block int, s string) *bdev_block_t {
	_, pa, ok := refpg_new()
	if !ok {
		panic("oom during bdev_block_new")
	}
	b := &bdev_block_t{};
	b.block = block
	b.pa = pa
	b.data = (*[512]uint8)(unsafe.Pointer(dmap(pa)))
	b.s = s
	b.bdev_refup("new")
	return b
}

func bdev_block_new_pa(block int, s string, pa pa_t) *bdev_block_t {
	b := &bdev_block_t{};
	b.block = block
	b.pa = pa
	b.data = (*[512]uint8)(unsafe.Pointer(dmap(pa)))
	b.s = s
	b.bdev_refup("new_pa")
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

// bdev_write_async increments ref to keep the page around utnil write completes
func bdev_write(b *bdev_block_t) {
	if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
		panic("write\n")
	}
	b.bdev_refup("bdev_write")
	req := bdev_req_new(b, BDEV_WRITE, true)
	if ahci_start(req) {
		<- req.ackCh
	}
}

// bdev_write_async increments ref to keep the page around until write completes
func bdev_write_async(b *bdev_block_t) {
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

// after bdev_read_block returns, the caller owns the physical page.  the caller
// must call bdev_refdown when it is done with the page in the buffer returned.
func bdev_read_block(blkn int, s string) *bdev_block_t {
	buf := bdev_block_new(blkn, s)
	buf.bdev_read()
	return buf
}

func bdev_flush() {
	ider := bdev_req_new(nil, BDEV_FLUSH, true)
	if ahci_start(ider) {
		<- ider.ackCh
	}
}


// block cache. the block cache stores inode blocks, free bit-map blocks,
// metadata blocks. file blocks are stored in the inode's page cache, which also
// interacts with the VM system.
//
// the cache returns the same pointer to a block_dev_t to callers, and thus the
// callers share the physical page in the block_dev_t. the callers must
// coordinate using the lock of the block.
//
// XXX do eviction.

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

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf.
func bdev_lookup_fill(blkn int, s string) *bdev_block_t {
	if blkn < superb_start {
		panic("no")
	}
	created := false

	bdev_cache.Lock()
	buf, ok := bdev_cache.blks[blkn]
	if !ok {
		buf = bdev_block_new(blkn, s)
		buf.Lock()
		bdev_cache.blks[blkn] = buf
		created = true
	}
	bdev_cache.Unlock()

	if !created {
		buf.Lock()
		buf.s = s
	} else {
		buf.bdev_read() // fill in new bdev_cache entry

	}
	buf.bdev_refup("lookup_fill")
	return buf
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf
func bdev_lookup_zero(blkn int, s string) *bdev_block_t {
	bdev_cache.Lock()
	if obuf, ok := bdev_cache.blks[blkn]; ok {
		bdev_cache.Unlock()
		obuf.Lock()
		obuf.bdev_refup("lookup_zero1")
		var zdata [512]uint8
		*obuf.data = zdata
		obuf.s = s
		return obuf
	}
	buf := bdev_block_new(blkn, s)   // get a zero page
	bdev_cache.blks[blkn] = buf
	buf.bdev_refup("lookup_zero2")
	buf.Lock()
	bdev_cache.Unlock()
	return buf
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_refdown when done with buf
func bdev_lookup_empty(blkn int, s string) *bdev_block_t {
	bdev_cache.Lock()
	if obuf, ok := bdev_cache.blks[blkn]; ok {
		bdev_cache.Unlock()
		obuf.Lock()
		obuf.s = s
		obuf.bdev_refup("lookup_empty 1")
		return obuf
	}
	buf := bdev_block_new(blkn, s)   // XXX get a zero page, not really necesary
	bdev_cache.blks[blkn] = buf
	buf.bdev_refup("lookup_empty 2")
	buf.Lock()
	bdev_cache.Unlock()
	return buf
}


func bdev_purge(b *bdev_block_t) {
	bdev_cache.Lock()
	b.bdev_refdown("bdev_purge")
	// fmt.Printf("bdev_purge %v\n", b.block)
	delete(bdev_cache.blks, b.block)
	bdev_cache.Unlock()
}


func bdev_test() {

	return

	ahci_debug = true

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
			bdev_write_async(wbuf[b])
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
