package main

import "fmt"
import "sync"
import "unsafe"

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
	req := bdev_req_new(b, BDEV_WRITE, true)
	if ahci_start(req) {
		<- req.ackCh
	}
} 

func (b *bdev_block_t) bdev_write_async() {
	if bdev_debug {
		fmt.Printf("bdev_write_async %v %s\n", b.block, b.s)
	}
	if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
		panic("write_async\n")
	}
	ider := bdev_req_new(b, BDEV_WRITE, false)
	ahci_start(ider)
}

func (b *bdev_block_t) bdev_read() {
	ider := bdev_req_new(b, BDEV_READ, true)
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

// XXX XXX XXX must count against mfs limit

type bdev_ref_t struct {
	buf *bdev_block_t
	refcnt int
}

type bdev_cache_t struct {
	refs		map[int]*bdev_ref_t
	sync.Mutex
}

var bdev_cache = bdev_cache_t{}

func bcache_init() {
	bdev_cache.refs = make(map[int]*bdev_ref_t, 30)

	bdev_test()
}

// returns locked buf with refcnt on page bumped up by 1. The caller must call
// bcache_relse when done with buf.  Return true if block was created in the
// cache, false if it was already present.
func bcache_get_empty(blkn int, s string) (*bdev_block_t, bool) {
	if blkn < superb_start {
		panic("no")
	}
	bdev_cache.Lock()

	if ref, ok := bdev_cache.refs[blkn]; ok {
		ref.refcnt++
		bdev_cache.Unlock()
		ref.buf.Lock()
		ref.buf.s = s
		return ref.buf, false
	}
	buf := bdev_make(blkn, pa_t(0), s)
	
	ref := &bdev_ref_t{}
	ref.refcnt = 1
	ref.buf = buf
	
	bdev_cache.refs[blkn] = ref
	buf.Lock()
	bdev_cache.Unlock()
	return buf, true
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_relse when done with buf.
func bcache_get_fill(blkn int, s string, lock bool) *bdev_block_t {
	b, created := bcache_get_empty(blkn, s)

	if bdev_debug {
		fmt.Printf("bcache_get_fill: %v %v created? %v\n", blkn, s, created)
	}

	if created {
		b.new_page()
		b.bdev_read() // fill in new bdev_cache entry
	}
	if !lock {
		b.Unlock()
	}
	return b
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bcache_relse when done with buf
func bcache_get_zero(blkn int, s string, lock bool) *bdev_block_t {
	b, created := bcache_get_empty(blkn, s)
	if bdev_debug {
		fmt.Printf("bcache_get_zero: %v %v %v\n", blkn, s, created)
	}
	if created {
		b.new_page()   // zero
	} 
	if !lock {
		b.Unlock()
	}
	return b
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bcache_relse when done with buf
func bcache_get_nofill(blkn int, s string) *bdev_block_t {
	b, created := bcache_get_empty(blkn, s)
	if bdev_debug {
		fmt.Printf("bcache_get_nofill1: %v %v %v\n", blkn, s, created)
	}
	if created {
		b.new_page()   // XXX a non-zero page would be fine
	}
	return b
}

func bcache_write(b *bdev_block_t) {
	bdev_cache.Lock()
	ref, ok := bdev_cache.refs[b.block]
	if !ok {
		panic("bcache_pin")
	}
	ref.refcnt++
	bdev_cache.Unlock()
	b.bdev_write()
}

func bcache_write_async(b *bdev_block_t) {
	bdev_cache.Lock()
	ref, ok := bdev_cache.refs[b.block]
	if !ok {
		panic("bcache_unpin")
	}
	ref.refcnt++
	bdev_cache.Unlock()
	b.bdev_write_async()
}

func bcache_refup(b *bdev_block_t, s string) {
	bdev_cache.Lock()
	defer bdev_cache.Unlock()

	ref, ok := bdev_cache.refs[b.block]
	if !ok {
		fmt.Printf("bdev_relse: %v %v\n", b.block, s)
		panic("bdev_relse\n")
	}
	ref.refcnt++
}

func bcache_relse(b *bdev_block_t, s string) {
	bdev_cache.Lock()
	defer bdev_cache.Unlock()

	ref, ok := bdev_cache.refs[b.block]
	if !ok {
		fmt.Printf("bdev_relse: %v %v\n", b.block, s)
		panic("bdev_relse\n")
	}
	if ref.buf != b {
		fmt.Printf("bdev_relse: %v %v\n", ref.buf, b)
		panic("bdev_relse\n")
	}
	if bdev_debug {
		fmt.Printf("bcache_relse: %v %v %v %v\n", b.block, s, ok, ref.refcnt)
	}
	ref.refcnt--
	if ref.refcnt < 0 {
		panic("bdev_relse\n")
	}
	if ref.refcnt == 0 {
		if bdev_debug {
			fmt.Printf("bcache_relse: %v delete from cache\n", b.block)
		}
		b.free_page()
		delete(bdev_cache.refs, b.block)
	}
}

func print_live_blocks() {
	fmt.Printf("bcache %v\n", len(bdev_cache.refs))
	for _, r := range bdev_cache.refs {
		fmt.Printf("block %v %v\n", r.buf.block, r.refcnt)
	}
	fmt.Printf("irefcache %v\n", len(irefcache.irefs))
	for _, v := range irefcache.irefs {
		b,i := bidecode(v.imem.priv)
		fmt.Printf("inode %v (%v,%v)\n", v.imem.priv, b, i)
		v.imem.pgcache.pgs.iter(func(pgi *pginfo_t) {
			fmt.Printf("pgi %v\n", pgi.buf.block)
		})
	}
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
			rbuf := bcache_get_fill(b, "read test", false)
			
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
