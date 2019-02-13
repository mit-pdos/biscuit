package fs

import "fmt"
import "sync"
import "sort"

import "bounds"
import "defs"
import "res"
import "stats"

// Bitmap allocater/marker. Used for inodes, blocks, and orphan inodes.

const bitsperblk = BSIZE * 8

type storage_i interface {
	Write(opid_t, *Bdev_block_t)
	Get_fill(int, string, bool) *Bdev_block_t
	Relse(*Bdev_block_t, string)
}

type bitmapstats_t struct {
	Nalloc stats.Counter_t
	Nfree  stats.Counter_t
	Nhit   stats.Counter_t
}

type bitmap_t struct {
	sync.Mutex
	fs        *Fs_t
	freestart int
	freelen   int
	lastbit   int
	nfreebits uint
	storage   storage_i
	stats     bitmapstats_t
	freemap   []uint8
	last      int
}

const NFREE = 1000

func mkAllocater(fs *Fs_t, start, len int, s storage_i) *bitmap_t {
	a := &bitmap_t{}
	a.fs = fs
	a.freestart = start
	a.freelen = len
	a.storage = s
	a.apply(0, func(b, v int) bool {
		if v == 0 {
			a.nfreebits++
		}
		return true
	})
	if !fs.diskfs {
		a.freemap = make([]uint8, (a.freelen * BSIZE))
		a.populateFreeMap()
	}
	return a
}

func blkno(bit int) int {
	return bit / bitsperblk
}

func blkoffset(bit int) int {
	return bit % bitsperblk
}

func byteno(bit int) int {
	return blkoffset(bit) / 8
}

func byteoffset(bit int) int {
	return blkoffset(bit) % 8
}

func (alloc *bitmap_t) bitmapblkno(bit int) int {
	return alloc.freestart + blkno(bit)
}

func (alloc *bitmap_t) Fbread(blockno int) *Bdev_block_t {
	if blockno < 0 || blockno >= alloc.freelen {
		panic("naughty blockno")
	}
	return alloc.storage.Get_fill(alloc.freestart+blockno, "fbread", true)
}

// apply f to every bit starting from start, until f is false.  return true if
// make a complete pass.
func (alloc *bitmap_t) apply(start int, f func(b, v int) bool) bool {
	var ca res.Cacheallocs_t
	gimme := bounds.Bounds(bounds.B_BITMAP_T_APPLY)

	var blk *Bdev_block_t
	var lastbn = -1
	var tryevict bool
	for bit := start; bit < alloc.freelen*bitsperblk; bit++ {
		bn := blkno(bit)
		if bn != lastbn {
			if tryevict && alloc.fs.diskfs {
				blk.Tryevict()
			}
			if blk != nil {
				blk.Unlock()
				alloc.storage.Relse(blk, "alloc apply")
			}
			blk = alloc.Fbread(bn)
			tryevict = ca.Shouldevict(gimme)
		}
		byteoff := byteno(bit)
		bitoff := byteoffset(bit)
		byte := blk.Data[byteoff]
		v := byte & (1 << uint(bitoff))
		if !f(bit, int(v)) {
			blk.Unlock()
			alloc.storage.Relse(blk, "alloc apply")
			return false
		}
		lastbn = bn

	}
	if blk != nil {
		blk.Unlock()
		alloc.storage.Relse(blk, "alloc apply")
	}
	return true
}

func (alloc *bitmap_t) CheckAndMark(opid opid_t) (int, defs.Err_t) {
	bitno := alloc.lastbit
	blkno := blkno(alloc.lastbit)
	byte := byteno(alloc.lastbit)
	bit := byteoffset(alloc.lastbit)

	blk := alloc.Fbread(blkno)
	if blk.Data[byte]&(1<<uint(bit)) == 0 {
		alloc.lastbit++
		blk.Data[byte] |= (1 << uint(bit))
		blk.Unlock()
		alloc.storage.Write(opid, blk)
		alloc.storage.Relse(blk, "CheckAndMark")
		return bitno, 0
	}
	blk.Unlock()
	alloc.storage.Relse(blk, "alloc CheckAndMark")
	return 0, -defs.ENOMEM
}

func (alloc *bitmap_t) populateFreeMap() {
	for bn := 0; bn < alloc.freelen; bn++ {
		blk := alloc.Fbread(bn)
		for i := 0; i < len(blk.Data); i++ {
			alloc.freemap[bn*4096+i] = blk.Data[i]
		}
		blk.Unlock()
		alloc.storage.Relse(blk, "alloc apply")
	}
	//fmt.Printf("freemap %d\n", len(alloc.freemap))
}

func (alloc *bitmap_t) FindFreeMap(opid opid_t) (int, defs.Err_t) {
	alloc.Lock()

	once := false
	for i := alloc.last; true; {
		if alloc.freemap[i] != uint8(0xFF) {
			alloc.last = i
			for j := 0; j < 8; j++ {
				v := alloc.freemap[i] & (1 << uint(j))
				if v == 0 {
					alloc.freemap[i] |= (1 << uint(j))
					alloc.nfreebits--
					alloc.Unlock()
					return (i*8 + j), 0
				}
			}
			panic("FindFreeMap")
		}
		if i+1 >= len(alloc.freemap) {
			i = 0
			if once {
				panic("memfs out of memory")
			}
			once = true
		} else {
			i++
		}

	}
	return 0, -defs.ENOMEM
}

func (alloc *bitmap_t) FindDiskMap(opid opid_t) (int, defs.Err_t) {
	alloc.Lock()

	bit, err := alloc.CheckAndMark(opid)
	if err == 0 {
		alloc.stats.Nhit.Inc()
	} else {
		alloc.apply(0, func(b, v int) bool {
			if v == 0 {
				alloc.lastbit = b
				return false
			}
			return true
		})
		bit, err = alloc.CheckAndMark(opid)
		if err != 0 {
			panic("FindAndMark")
		}
	}
	alloc.stats.Nalloc.Inc()
	alloc.nfreebits--
	alloc.Unlock()
	return bit, 0
}

func (alloc *bitmap_t) FindAndMark(opid opid_t) (int, defs.Err_t) {
	if alloc.fs.diskfs {
		return alloc.FindDiskMap(opid)
	} else {
		return alloc.FindFreeMap(opid)
	}
}

func (alloc *bitmap_t) Unmark(opid opid_t, bit int) {
	alloc.Lock()

	if fs_debug {
		fmt.Printf("Unmark: %v\n", bit)
	}

	if bit < 0 {
		panic("Unmark bad bit")
	}

	if !alloc.fs.diskfs {
		i := bit / 8
		j := bit % 8
		alloc.freemap[i] &= ^(1 << uint(j))
		alloc.nfreebits++
		alloc.Unlock()
		return
	}

	fblkno := blkno(bit)
	fbyteoff := byteno(bit)
	fbitoff := byteoffset(bit)
	fblk := alloc.Fbread(fblkno)
	fblk.Data[fbyteoff] &= ^(1 << uint(fbitoff))
	fblk.Unlock()
	alloc.storage.Write(opid, fblk)
	alloc.storage.Relse(fblk, "Unmark")
	alloc.stats.Nfree.Inc()
	alloc.nfreebits++
	alloc.Unlock()
}

func (alloc *bitmap_t) Mark(opid opid_t, bit int) {
	alloc.Lock()
	defer alloc.Unlock()

	if fs_debug {
		fmt.Printf("Mark: %v\n", bit)
	}

	if bit < 0 {
		panic("Mark bad blockno")
	}

	fblkno := blkno(bit)
	fbyteoff := byteno(bit)
	fbitoff := byteoffset(bit)
	fblk := alloc.Fbread(fblkno)
	fblk.Data[fbyteoff] |= 1 << uint(fbitoff)
	fblk.Unlock()
	alloc.storage.Write(opid, fblk)
	alloc.storage.Relse(fblk, "Mark")
}

type mark_t int

const (
	MARK   mark_t = 1
	UNMARK mark_t = 2
)

func smallest(mark, unmark []int) (int, mark_t) {
	var n int
	var m mark_t
	if len(mark) > 0 {
		n = mark[0]
		m = MARK
		if len(unmark) > 0 {
			if unmark[0] < n {
				n = unmark[0]
				m = UNMARK
			}
		}
	} else {
		n = unmark[0]
		m = UNMARK
	}
	return n, m
}

// mark and umark bits in a single shot.
func (alloc *bitmap_t) MarkUnmark(opid opid_t, mark, unmark []int) {
	alloc.Lock()
	defer alloc.Unlock()

	sort.Ints(mark)
	sort.Ints(unmark)

	if fs_debug {
		fmt.Printf("Mark: %v Unmark %v\n", mark, unmark)
	}

	var blk *Bdev_block_t
	for len(mark) > 0 || len(unmark) > 0 {
		bit, op := smallest(mark, unmark)
		if bit < 0 {
			panic("MarkUnmark bad bit")
		}
		fblkno := blkno(bit)
		fbyteoff := byteno(bit)
		fbitoff := byteoffset(bit)

		// done with this block
		if blk != nil && blk.Block != fblkno {
			blk.Unlock()
			alloc.storage.Write(opid, blk)
			alloc.storage.Relse(blk, "MarkUnmark")
			blk = nil
		}
		if blk == nil {
			blk = alloc.Fbread(fblkno)
		}
		if op == MARK {
			blk.Data[fbyteoff] |= 1 << uint(fbitoff)
			mark = mark[1:]
		} else {
			blk.Data[fbyteoff] &= ^(1 << uint(fbitoff))
			unmark = unmark[1:]
		}
	}
	if blk != nil {
		blk.Unlock()
		alloc.storage.Write(opid, blk)
		alloc.storage.Relse(blk, "MarkUnmark")
	}
}

func (alloc *bitmap_t) Stats() string {
	return "allocator " + stats.Stats2String(alloc.stats)
}

func (alloc *bitmap_t) ResetStats() {
	alloc.stats = bitmapstats_t{}
}
