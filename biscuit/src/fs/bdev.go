package fs

import "fmt"
import "strconv"
import "sync"

import "runtime"

import "common"

const bdev_debug = false

// A block has a lock, since, it may store an inode block, which has 4 inodes,
// and we need to ensure that hat writes aren't lost due to concurrent inode
// updates.  The inode code is careful about releasing lock.  Other types of
// blocks not shared and the caller releases the lock immediately.
//
// The data of a block is a page allocated from the page allocator.  The disk
// DMAs to and from the physical address for the page.  data is the virtual
// address for the page.
//

// block cache, all device interactions run through block cache.
//
// The cache returns a pointer to a block_dev_t.  There is *one* common.Bdev_block_t
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

type bcache_t struct {
	refcache *refcache_t
	mem      common.Blockmem_i
	disk     common.Disk_i
	sync.Mutex
	pins map[common.Pa_t]*common.Bdev_block_t
}

func mkBcache(mem common.Blockmem_i, disk common.Disk_i) *bcache_t {
	bcache := &bcache_t{}
	bcache.mem = mem
	bcache.disk = disk
	bcache.refcache = mkRefcache(common.Syslimit.Blocks, false)
	bcache.pins = make(map[common.Pa_t]*common.Bdev_block_t)
	return bcache
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_relse when done with buf.
func (bcache *bcache_t) Get_fill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	b, created, err := bcache.bref(blkn, s)
	if b.Evictnow() {
		runtime.Cacheaccount()
	}

	if bdev_debug {
		fmt.Printf("bcache_get_fill: %v %v created? %v\n", blkn, s, created)
	}

	if err != 0 {
		return nil, err
	}

	if created {
		b.New_page()
		b.Read() // fill in new bdev_cache entry
	}
	if !lock {
		b.Unlock()
	}
	return b, 0
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bcache_relse when done with buf
func (bcache *bcache_t) Get_zero(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	b, created, err := bcache.bref(blkn, s)
	if bdev_debug {
		fmt.Printf("bcache_get_zero: %v %v %v\n", blkn, s, created)
	}
	if err != 0 {
		return nil, err
	}
	if created {
		b.New_page() // zero
	}
	if !lock {
		b.Unlock()
	}
	return b, 0
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bcache_relse when done with buf
func (bcache *bcache_t) Get_nofill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	b, created, err := bcache.bref(blkn, s)
	if bdev_debug {
		fmt.Printf("bcache_get_nofill1: %v %v %v\n", blkn, s, created)
	}
	if err != 0 {
		return nil, err
	}
	if created {
		b.New_page() // XXX a non-zero page would be fine
	}
	if !lock {
		b.Unlock()
	}
	return b, 0
}

func (bcache *bcache_t) Write(b *common.Bdev_block_t) {
	bcache.refcache.Refup(b.Ref)
	b.Write()
}

func (bcache *bcache_t) Write_async(b *common.Bdev_block_t) {
	bcache.refcache.Refup(b.Ref)
	b.Write_async()
}

func (bcache *bcache_t) Write_async_through(b *common.Bdev_block_t) {
	b.Write_async()
}

// blks must be contiguous on disk
func (bcache *bcache_t) Write_async_blks(blks *common.BlkList_t) {
	if bdev_debug {
		fmt.Printf("bcache_write_async_blks %v\n", blks.Len())
	}
	if blks.Len() == 0 {
		return
	}
	n := blks.FrontBlock().Block
	for b := blks.FrontBlock(); b != nil; b = blks.NextBlock() {
		// sanity check
		if b.Block != n {
			fmt.Printf("%d %d\n", b.Block, n)
			panic("not contiguous\n")
		}
		n++
		bcache.refcache.Refup(b.Ref)
	}
	// one request for all blks
	ider := common.MkRequest(blks, common.BDEV_WRITE, false)
	blks.FrontBlock().Disk.Start(ider)
}

func (bcache *bcache_t) Write_async_blks_through(blks *common.BlkList_t) {
	if bdev_debug {
		fmt.Printf("bcache_write_async_blk_through s %v\n", blks.Len())
	}
	if blks.Len() == 0 {
		return
	}
	if bdev_debug {
		n := blks.FrontBlock().Block
		for b := blks.FrontBlock(); b != nil; b = blks.NextBlock() {
			// sanity check
			if b.Block != n {
				fmt.Printf("%d %d\n", b.Block, n)
				panic("not contiguous\n")
			}
			n++
		}
	}
	// one request for all blks
	ider := common.MkRequest(blks, common.BDEV_WRITE, false)
	blks.FrontBlock().Disk.Start(ider)
}

func (bcache *bcache_t) Refup(b *common.Bdev_block_t, s string) {
	bcache.refcache.Refup(b.Ref)
}

func (bcache *bcache_t) Relse(b *common.Bdev_block_t, s string) {
	if bdev_debug {
		fmt.Printf("bcache_relse: %v %v\n", b.Block, s)
	}
	evicted := bcache.refcache.Refdown(b.Ref, false)
	if evicted {
		bcache.Lock()
		delete(bcache.pins, b.Pa)
		bcache.Unlock()

		b.Evict()
	}
}

func (bcache *bcache_t) Stats() string {
	s := "bcache: size "
	s += strconv.Itoa(len(bcache.refcache.refs))
	s += " #evictions "
	s += strconv.Itoa(bcache.refcache.nevict)
	s += " #live "
	s += strconv.Itoa(bcache.refcache.nlive())
	s += "\n"
	return s
}

//
// Implementation
//

// returns the reference to a locked buffer
func (bcache *bcache_t) bref(blk int, s string) (*common.Bdev_block_t, bool, common.Err_t) {
	ref, created, err := bcache.refcache.Lookup(blk, func(_ int, ref *common.Ref_t) common.Obj_t {
		ret := common.MkBlock(blk, s, bcache.mem, bcache.disk, bcache)
		ret.Ref = ref
		ret.Lock()
		return ret
	})
	if err != 0 {
		return nil, false, err
	}

	b := ref.Obj.(*common.Bdev_block_t)
	if !created {
		b.Lock()
	}
	return b, created, err
}

type _nop_relse_t struct {
}

func (n *_nop_relse_t) Relse(*common.Bdev_block_t, string) {
}

var _nop_relse = _nop_relse_t{}

func (bcache *bcache_t) raw(blkno int) (*common.Bdev_block_t, common.Err_t) {
	ret := common.MkBlock_newpage(blkno, "raw", bcache.mem, bcache.disk, &_nop_relse)
	return ret, 0
}

func (bcache *bcache_t) pin(b *common.Bdev_block_t) {
	bcache.refcache.Refup(b.Ref)

	bcache.Lock()
	if old, ok := bcache.pins[b.Pa]; ok && old != b {
		panic("uh oh")
	}
	bcache.pins[b.Pa] = b
	bcache.Unlock()
}

func (bcache *bcache_t) unpin(pa common.Pa_t) {
	bcache.Lock()
	defer bcache.Unlock()
	b, ok := bcache.pins[pa]

	if !ok {
		panic("block no pinned")
	}
	bcache.Relse(b, "unpin")
}

func bdev_test(mem common.Blockmem_i, disk common.Disk_i, bcache *bcache_t) {
	return

	fmt.Printf("disk test\n")

	const N = 3

	wbuf := new([N]*common.Bdev_block_t)

	for b := 0; b < N; b++ {
		wbuf[b] = common.MkBlock_newpage(b, "disktest", mem, disk, bcache)
	}
	for j := 0; j < 100; j++ {

		for b := 0; b < N; b++ {
			fmt.Printf("req %v,%v\n", j, b)

			for i, _ := range wbuf[b].Data {
				wbuf[b].Data[i] = uint8(b)
			}
			wbuf[b].Write_async()
		}
		ider := common.MkRequest(nil, common.BDEV_FLUSH, true)
		if disk.Start(ider) {
			<-ider.AckCh
		}
		for b := 0; b < N; b++ {
			rbuf, err := bcache.Get_fill(b, "read test", false)
			if err != 0 {
				panic("bdev_test\n")
			}
			for i, v := range rbuf.Data {
				if v != uint8(b) {
					fmt.Printf("buf %v i %v v %v\n", j, i, v)
					panic("bdev_test\n")
				}
			}
		}
	}
	panic("disk test passed\n")
}

//
// Block allocator interface
//

type bbitmap_t struct {
	fs    *Fs_t
	alloc *bitmap_t
	start int
	len   int
	first int
}

func mkBallocater(fs *Fs_t, start, len, first int) *bbitmap_t {
	balloc := &bbitmap_t{}
	balloc.alloc = mkAllocater(fs, start, len, fs.fslog)
	fmt.Printf("bmap start %v bmaplen %v first datablock %v free %d\n", start, len, first, balloc.alloc.nfreebits)
	balloc.first = first
	balloc.start = start
	balloc.len = len
	balloc.fs = fs
	return balloc
}

func (balloc *bbitmap_t) Balloc(opid opid_t) (int, common.Err_t) {
	ret, err := balloc.balloc1(opid)
	if err != 0 {
		return 0, err
	}
	if ret < 0 {
		panic("balloc: bad blkn")
	}
	if ret >= balloc.fs.superb.Lastblock() {
		fmt.Printf("blkn %v last %v\n", ret, balloc.fs.superb.Lastblock())
		return 0, -common.ENOMEM
	}
	blk, err := balloc.fs.bcache.Get_zero(ret, "balloc", true)
	if err != 0 {
		return 0, err
	}
	if bdev_debug {
		fmt.Printf("balloc: %v free %d\n", ret, balloc.alloc.nfreebits)
	}

	var zdata [common.BSIZE]uint8
	copy(blk.Data[:], zdata[:])
	blk.Unlock()
	balloc.fs.fslog.Write(opid, blk)
	balloc.fs.bcache.Relse(blk, "balloc")
	return ret, 0
}

func (balloc *bbitmap_t) Bfree(opid opid_t, blkno int) common.Err_t {
	blkno -= balloc.first
	if bdev_debug {
		fmt.Printf("bfree: %v free before %d\n", blkno, balloc.alloc.nfreebits)
	}
	if blkno < 0 {
		panic("bfree")
	}
	if blkno >= balloc.len*common.BSIZE*8 {
		panic("bfree too large")
	}
	return balloc.alloc.Unmark(opid, blkno)
}

func (balloc *bbitmap_t) Stats() string {
	return "balloc " + balloc.alloc.Stats()
}

// allocates a block, marking it used in the free block bitmap. free blocks and
// log blocks are not accounted for in the free bitmap; all others are. balloc
// should only ever acquire fblock.
func (balloc *bbitmap_t) balloc1(opid opid_t) (int, common.Err_t) {
	blkn, err := balloc.alloc.FindAndMark(opid)
	if err != 0 {
		fmt.Printf("balloc1: %v\n", err)
		return 0, err
	}
	if blkn >= balloc.len*common.BSIZE*8 {
		fmt.Printf("balloc1: blkn %v len %v\n", blkn, balloc.len)
		panic("balloc1: too large blkn\n")
	}
	if bdev_debug {
		fmt.Printf("balloc1: %v\n", blkn)
	}
	return blkn + balloc.first, err
}
