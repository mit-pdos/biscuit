package fs

import "fmt"
import "sync"
import "strconv"

import "common"

// Bitmap allocater/marker. Used for inodes, blocks, and orphan inodes.

const bitsperblk = common.BSIZE * 8

type allocater_t struct {
	sync.Mutex

	fs        *Fs_t
	freestart int
	freelen   int
	lastbit   int
	nfreebits uint

	// XXX maybe interface
	Write    func(*common.Bdev_block_t)
	Get_fill func(int, string, bool) (*common.Bdev_block_t, common.Err_t)
	Relse    func(*common.Bdev_block_t, string)

	// stats
	nalloc int
	nfree  int
	nhit   int
}

func mkAllocater(fs *Fs_t, start, len int, Write func(*common.Bdev_block_t),
	Get_fill func(int, string, bool) (*common.Bdev_block_t, common.Err_t), Relse func(*common.Bdev_block_t, string)) *allocater_t {
	a := &allocater_t{}
	a.fs = fs
	a.freestart = start
	a.freelen = len
	a.Write = Write
	a.Get_fill = Get_fill
	a.Relse = Relse
	a.nfreebits = 0
	err := a.apply(func(b, v int) bool {
		if v == 0 {
			a.nfreebits++
		}
		return true
	})
	if err != 0 {
		panic("apply failed")
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

func (alloc *allocater_t) Fbread(blockno int) (*common.Bdev_block_t, common.Err_t) {
	// fmt.Printf("read %d\n", alloc.freestart+blockno)
	if blockno < 0 || blockno >= alloc.freelen {
		panic("naughty blockno")
	}
	return alloc.Get_fill(alloc.freestart+blockno, "fbread", true)
}

// apply f to every bit until f is false
func (alloc *allocater_t) apply(f func(b, v int) bool) common.Err_t {
	var blk *common.Bdev_block_t
	var err common.Err_t
	for bn := 0; bn < alloc.freelen; bn++ {
		if blk != nil {
			blk.Unlock()
			alloc.Relse(blk, "alloc apply")
		}
		blk, err = alloc.Fbread(bn)
		if err != 0 {
			return err
		}
		for idx := 0; idx < len(blk.Data); idx++ {
			for m := 0; m < 8; m++ {
				b := bn*bitsperblk + idx*8 + m
				byte := blk.Data[idx]
				v := byte & (1 << uint(m))
				if !f(b, int(v)) {
					blk.Unlock()
					alloc.Relse(blk, "alloc apply")
					return 0
				}
			}
		}
	}
	blk.Unlock()
	alloc.Relse(blk, "alloc apply")
	return 0
}

func (alloc *allocater_t) quickalloc() (int, common.Err_t) {
	bitno := alloc.lastbit
	blkno := blkno(alloc.lastbit)
	byte := byteno(alloc.lastbit)
	bit := byteoffset(alloc.lastbit)

	blk, err := alloc.Fbread(blkno)
	if err != 0 {
		return 0, err
	}
	if blk.Data[byte]&(1<<uint(bit)) == 0 {
		alloc.lastbit++
		blk.Data[byte] |= (1 << uint(bit))
		blk.Unlock()
		alloc.Write(blk)
		alloc.Relse(blk, "alloc quickcheck")
		return bitno, 0
	}
	blk.Unlock()
	alloc.Relse(blk, "alloc quickcheck")
	return 0, -common.ENOMEM
}

func (alloc *allocater_t) Alloc() (int, common.Err_t) {
	alloc.Lock()
	defer alloc.Unlock()

	bit, err := alloc.quickalloc()
	if err == 0 {
		alloc.nhit++
	} else {
		err = alloc.apply(func(b, v int) bool {
			if v == 0 {
				alloc.lastbit = b
				return false
			}
			return true
		})
		if err != 0 {
			return 0, err
		}
		bit, err = alloc.quickalloc()
		if err != 0 {
			panic("quickmark")
		}
	}
	alloc.nalloc++
	alloc.nfreebits--
	return bit, 0
}

func (alloc *allocater_t) Free(bit int) common.Err_t {
	alloc.Lock()
	defer alloc.Unlock()

	if fs_debug {
		fmt.Printf("Free: %v\n", bit)
	}

	if bit < 0 {
		panic("free bad bit")
	}

	fblkno := blkno(bit)
	fbyteoff := byteno(bit)
	fbitoff := byteoffset(bit)
	fblk, err := alloc.Fbread(fblkno)
	if err != 0 {
		return err
	}
	fblk.Data[fbyteoff] &= ^(1 << uint(fbitoff))
	fblk.Unlock()
	alloc.Write(fblk)
	alloc.Relse(fblk, "free")
	alloc.nfree++
	alloc.nfreebits++
	return 0
}

func (alloc *allocater_t) Mark(bit int) common.Err_t {
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
	fblk, err := alloc.Fbread(fblkno)
	if err != 0 {
		return err
	}
	fblk.Data[fbyteoff] |= 1 << uint(fbitoff)
	fblk.Unlock()
	alloc.Write(fblk)
	alloc.Relse(fblk, "free")
	return 0
}

func (alloc *allocater_t) Stats() string {
	s := "allocater: #alloc "
	s += strconv.Itoa(alloc.nalloc)
	s += " #free "
	s += strconv.Itoa(alloc.nfree)
	s += " #hit "
	s += strconv.Itoa(alloc.nhit)
	s += "\n"
	return s
}
