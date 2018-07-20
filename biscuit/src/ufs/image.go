package ufs

import "os"
import "fmt"

import "fs"
import "mem"
import "ustr"
import "util"

// Disk image layout:
// optional:
//   boot img (records address of superblock)
//   kernel img
// if no optional imgs
//   bootblock (records address of superblock)
// superblock
// log blocks
// orphan map
// inode map
// block map
// inode blocks
// data blocks

const (
	nbitsperblock = fs.BSIZE * 8
)

func bytepg2byte(d *mem.Bytepg_t) []byte {
	b := make([]byte, len(d))
	for i := 0; i < len(d); i++ {
		b[i] = d[i]
	}
	return b
}

func Tell(f *os.File) int {
	o, err := f.Seek(0, 1)
	if err != nil {
		panic(err)
	}
	return int(o / fs.BSIZE)
}

func mkBlock() []byte {
	return make([]byte, fs.BSIZE)
}

func writeBootBlock(f *os.File, superb int) {
	d := &mem.Bytepg_t{}
	util.Writen(d[:], 4, fs.FSOFF, superb)
	f.Write(bytepg2byte(d))
}

func writeSuperBlock(f *os.File, start int, nlogblks, ninodeblks, ndatablks int) *fs.Superblock_t {
	if Tell(f) != start {
		panic("superblock in wrong location")
	}
	d := &mem.Bytepg_t{}
	sb := fs.Superblock_t{d}
	sb.SetLoglen(nlogblks)
	ninode := ninodeblks * (fs.BSIZE / fs.ISIZE)
	ni := ninode/nbitsperblock + 1
	sb.SetIorphanblock(start + 1 + nlogblks)
	sb.SetIorphanlen(ni)
	sb.SetImaplen(ni)
	sb.SetFreeblock(start + 1 + nlogblks + 2*ni)
	bblock := ndatablks/nbitsperblock + 1
	sb.SetFreeblocklen(bblock)
	sb.SetInodelen(ninodeblks)
	sb.SetLastblock(start + 1 + nlogblks + 2*ni + bblock + ninodeblks + ndatablks)
	f.Write(bytepg2byte(sb.Data))
	return &sb
}

func markAllocated(d []byte, startbit int) {
	for i := (startbit / 8) + 1; i < fs.BSIZE; i++ {
		d[i] = byte(0xff)
	}
	rem := startbit % 8
	for i := rem; i < 8; i++ {
		d[startbit/8] |= 1 << uint(i)
	}
}

func writeLog(f *os.File, nlogblks int) {
	zeroblock := mkBlock()
	for i := 0; i < nlogblks; i++ {
		f.Write(zeroblock)
	}
}

func writeInodeMap(f *os.File, sb *fs.Superblock_t, ninodeblks int) {
	if Tell(f) != sb.Iorphanblock()+sb.Iorphanlen() {
		panic("incorrect inode map start\n")
	}
	ninode := ninodeblks * (fs.BSIZE / fs.ISIZE)
	oneblock := mkBlock()
	oneblock[0] |= 1 << 0 // mark root inode as allocated
	if sb.Imaplen() == 1 {
		markAllocated(oneblock, ninode)
		f.Write(oneblock)
	} else {
		f.Write(oneblock)
		block := mkBlock()
		for i := 1; i < sb.Imaplen()-1; i++ {
			f.Write(block)
		}
		start := nbitsperblock - ninode%nbitsperblock
		markAllocated(block, start)
		f.Write(block)
	}
}

func writeOrphanMap(f *os.File, sb *fs.Superblock_t, ninodeblks int) {
	if Tell(f) != sb.Iorphanblock() {
		panic("incorrect iorphan start\n")
	}
	block := mkBlock()
	for i := 0; i < sb.Iorphanlen(); i++ {
		f.Write(block)
	}
}

func writeBlockMap(f *os.File, sb *fs.Superblock_t, ndatablks int) {
	if Tell(f) != sb.Freeblock() {
		panic("incorrect free block map start\n")
	}

	if sb.Freeblocklen() == 1 {
		block := mkBlock()
		block[0] |= 1 << 0 // mark root dir block as allocated
		markAllocated(block, ndatablks)
		f.Write(block)
	} else {
		block := mkBlock()
		block[0] |= 1 << 0 // mark root dir block as allocated
		f.Write(block)

		block = mkBlock()
		for i := 1; i < sb.Freeblocklen()-1; i++ {
			f.Write(block)
		}

		// write last block
		o := ndatablks % nbitsperblock
		markAllocated(block, o)
		f.Write(block)
	}
	if Tell(f) != sb.Freeblock()+sb.Freeblocklen() {
		panic("incorrect free block map\n")
	}

}

func writeInodes(f *os.File, sb *fs.Superblock_t) {
	b := fs.MkBlock(0, "", nil, nil, nil)
	b.Data = &mem.Bytepg_t{}
	root := fs.Inode_t{b, 0}

	firstdata := sb.Freeblock() + sb.Freeblocklen() + sb.Inodelen()
	root.W_itype(fs.I_DIR)
	root.W_linkcount(1)
	root.W_size(fs.BSIZE)
	root.W_addr(0, firstdata)
	block := bytepg2byte(b.Data)

	if Tell(f) != sb.Freeblock()+sb.Freeblocklen() {
		fmt.Printf("%v %v\n", Tell(f), sb.Freeblock()+sb.Freeblocklen())
		panic("inodes don't line up")
	}

	f.Write(block)
	zeroblock := mkBlock()
	for i := 1; i < sb.Inodelen(); i++ {
		f.Write(zeroblock)
	}
}

func writeDataBlocks(f *os.File, sb *fs.Superblock_t, ndatablks int) {
	// Root directory data
	data := &mem.Bytepg_t{}
	ddata := fs.Dirdata_t{data[:]}
	ddata.W_filename(0, ustr.Ustr("."))
	ddata.W_inodenext(0, 0)
	ddata.W_filename(1, ustr.Ustr(".."))
	ddata.W_inodenext(1, 0)
	for i := 2; i < fs.NDIRENTS; i++ {
		ddata.W_filename(i, ustr.MkUstr())
		ddata.W_inodenext(i, 0)
	}
	d := bytepg2byte(data)

	if Tell(f) != sb.Freeblock()+sb.Freeblocklen()+sb.Inodelen() {
		panic("inodes don't line up")
	}

	f.Write(d) // first block for root
	zeroblock := mkBlock()
	for i := 1; i < ndatablks; i++ {
		f.Write(zeroblock)
	}
}

func addimg(img string, f *os.File) {
	s, err := os.Open(img)
	if err != nil {
		panic(err)
	}
	for {
		b := make([]byte, fs.BSIZE)
		n, err := s.Read(b)
		if err != nil {
			return
		}
		if n == 0 {
			return
		}
		_, err = f.Write(b[0:n])
		if err != nil {
			panic(err)
		}
	}
	if err := s.Close(); err != nil {
		panic(err)
	}
}

func pad(f *os.File) {
	o, err := f.Seek(0, 1)
	if err != nil {
		panic(err)
	}

	n := util.Roundup(int(o), fs.BSIZE)
	n = n - int(o)
	b := make([]byte, fs.BSIZE)
	_, err = f.Write(b)
	if err != nil {
		panic(err)
	}
}

func pokeboot(f *os.File, start int) {
	// seek to boot block
	_, err := f.Seek(0, 0)
	if err != nil {
		panic(err)
	}
	b := make([]byte, fs.BSIZE)
	n, err := f.Read(b)
	if err != nil {
		panic(err)
	}
	if n != fs.BSIZE {
		panic("short read")
	}

	util.Writen(b[:], 4, fs.FSOFF, start)

	// Replace boot block with b
	_, err = f.Seek(0, 0)
	if err != nil {
		panic(err)
	}

	f.Write(b)
	if err != nil {
		panic(err)
	}

	// seek back
	_, err = f.Seek(int64(start*fs.BSIZE), 0)
	if err != nil {
		panic(err)
	}
	s := Tell(f)
	if s != start {
		panic("wrong position")
	}
}

func MkDisk(disk string, images []string, nlogblks, ninodeblks, ndatablks int) {
	fmt.Printf("Make FS disk %s\n", disk)
	f, err := os.Create(disk)
	if err != nil {
		panic(err)
	}

	start := 1
	if len(images) > 0 {
		for _, i := range images {
			addimg(i, f)
		}
		pad(f)

		start = Tell(f)
		pokeboot(f, start)
	} else {
		writeBootBlock(f, start)
	}

	fmt.Printf("superblock at block %d\n", start)
	sb := writeSuperBlock(f, start, nlogblks, ninodeblks, ndatablks)
	writeLog(f, nlogblks)
	writeOrphanMap(f, sb, ninodeblks)
	writeInodeMap(f, sb, ninodeblks)
	writeBlockMap(f, sb, ndatablks)
	writeInodes(f, sb)
	writeDataBlocks(f, sb, ndatablks)

	f.Sync()
	f.Close()
}
