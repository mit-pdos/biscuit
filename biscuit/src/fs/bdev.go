package fs

import "fmt"
import "strconv"

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
}

func mkBcache(mem common.Blockmem_i, disk common.Disk_i) *bcache_t {
	bcache := &bcache_t{}
	bcache.mem = mem
	bcache.disk = disk
	bcache.refcache = mkRefcache(common.Syslimit.Blocks, false)
	return bcache
}

// returns locked buf with refcnt on page bumped up by 1. caller must call
// bdev_relse when done with buf.
func (bcache *bcache_t) Get_fill(blkn int, s string, lock bool) (*common.Bdev_block_t, common.Err_t) {
	b, created, err := bcache.bref(blkn, s)

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
	bcache.refcache.Refup(b, "bcache_write")
	b.Write()
}

func (bcache *bcache_t) Write_async(b *common.Bdev_block_t) {
	bcache.refcache.Refup(b, "bcache_write_async")
	b.Write_async()
}

// blks must be contiguous on disk
func (bcache *bcache_t) Write_async_blks(blks []*common.Bdev_block_t) {
	if bdev_debug {
		fmt.Printf("bcache_write_async_blks %v\n", len(blks))
	}
	if len(blks) == 0 {
		panic("bcache_write_async_blks\n")
	}
	n := blks[0].Block - 1
	for _, b := range blks {
		// sanity check
		if b.Block != n+1 {
			panic("not contiguous\n")
		}
		n++
		bcache.refcache.Refup(b, "bcache_write_async_blks")
	}
	// one request for all blks
	ider := common.MkRequest(blks, common.BDEV_WRITE, false)
	blks[0].Disk.Start(ider)
}

func (bcache *bcache_t) Refup(b *common.Bdev_block_t, s string) {
	bcache.refcache.Refup(b, s)
}

func (bcache *bcache_t) Relse(b *common.Bdev_block_t, s string) {
	if bdev_debug {
		fmt.Printf("bcache_relse: %v %v\n", b.Block, s)
	}
	bcache.refcache.Refdown(b, s)
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
	ref, err := bcache.refcache.Lookup(blk, s)
	if err != 0 {
		// fmt.Printf("bref error %v\n", err)
		return nil, false, err
	}
	defer ref.Unlock()

	created := false
	if !ref.valid {
		if bdev_debug {
			fmt.Printf("bref fill %v %v\n", blk, s)
		}
		buf := common.MkBlock(blk, s, bcache.mem, bcache.disk, bcache)
		ref.obj = buf
		ref.valid = true
		created = true
	}
	b := ref.obj.(*common.Bdev_block_t)
	b.Lock()
	b.Name = s
	return b, created, err
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

type ballocater_t struct {
	fs    *Fs_t
	alloc *allocater_t
	start int
	len   int
	first int
	blen  int
}

func mkBallocater(fs *Fs_t, start, len, first, blen int) *ballocater_t {
	balloc := &ballocater_t{}
	balloc.alloc = mkAllocater(fs, start, len)
	fmt.Printf("bmap start %v bmaplen %v first datablock %v blen %v\n", start, len, first, blen)
	balloc.first = first
	balloc.start = start
	balloc.len = len
	balloc.blen = blen
	balloc.fs = fs
	return balloc
}

func (balloc *ballocater_t) Balloc() (int, common.Err_t) {
	ret, err := balloc.balloc1()
	if err != 0 {
		return 0, err
	}
	if ret < 0 {
		panic("balloc: bad blkn")
	}
	if ret >= balloc.fs.superb.lastblock() {
		fmt.Printf("blkn %v last %v\n", ret, balloc.fs.superb.lastblock())
		return 0, -common.ENOMEM
	}
	blk, err := balloc.fs.bcache.Get_zero(ret, "balloc", true)
	if err != 0 {
		return 0, err
	}
	if bdev_debug {
		fmt.Printf("balloc: %v\n", ret)
	}

	var zdata [common.BSIZE]uint8
	copy(blk.Data[:], zdata[:])
	blk.Unlock()
	balloc.fs.fslog.Write(blk)
	balloc.fs.bcache.Relse(blk, "balloc")
	return ret, 0
}

func (balloc *ballocater_t) Bfree(blkno int) common.Err_t {
	if bdev_debug {
		fmt.Printf("bfree: %v\n", blkno)
	}
	blkno -= balloc.first
	if blkno < 0 {
		panic("bfree")
	}
	if blkno >= balloc.blen*common.BSIZE*8 {
		panic("bfree too large")
	}
	return balloc.alloc.Free(blkno)
}

func (balloc *ballocater_t) Stats() string {
	return "balloc " + balloc.alloc.Stats()
}

// allocates a block, marking it used in the free block bitmap. free blocks and
// log blocks are not accounted for in the free bitmap; all others are. balloc
// should only ever acquire fblock.
func (balloc *ballocater_t) balloc1() (int, common.Err_t) {
	blkn, err := balloc.alloc.Alloc()
	if err != 0 {
		return 0, err
	}
	if blkn >= balloc.blen*common.BSIZE*8 {
		fmt.Printf("balloc1: blkn %v len %v\n", blkn, balloc.blen)
		panic("balloc1: too large blkn\n")
	}
	return blkn + balloc.first, err
}
