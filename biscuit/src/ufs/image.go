package ufs

import "os"
import "fmt"

import "common"
import "fs"

// Disk image layout:
// bootblock (records address of superblock)
// bootimg
// superblock
// log blocks
// inode map
// block map
// inode blocks
// data blocks

const (
	nlogblks      = 256
	ninodeblks    = 50
	ndatablks     = 40000
	nbitsperblock = common.BSIZE * 8
)

func bytepg2byte(d *common.Bytepg_t) []byte {
	b := make([]byte, len(d))
	for i := 0; i < len(d); i++ {
		b[i] = d[i]
	}
	return b
}

func tell(f *os.File) int {
	o, err := f.Seek(0, 1)
	if err != nil {
		panic(err)
	}
	return int(o / common.BSIZE)
}

func mkBlock() []byte {
	return make([]byte, common.BSIZE)
}

func writeBootBlock(f *os.File, superb int) {
	d := &common.Bytepg_t{}
	common.Writen(d[:], 4, fs.FSOFF, superb)
	f.Write(bytepg2byte(d))
}

func writeSuperBlock(f *os.File, start int) *fs.Superblock_t {
	d := &common.Bytepg_t{}
	sb := fs.Superblock_t{d}
	sb.SetLoglen(nlogblks)
	sb.SetImapblock(start + 1 + nlogblks)
	ninode := ninodeblks * (common.BSIZE / fs.ISIZE)
	ni := ninode/nbitsperblock + 1
	sb.SetImaplen(ni)
	sb.SetFreeblock(start + 1 + nlogblks + ni)
	bblock := ndatablks/nbitsperblock + 1
	sb.SetFreeblocklen(bblock)
	sb.SetInodelen(ninodeblks)
	sb.SetLastblock(start + 1 + nlogblks + ni + bblock + ninodeblks + ndatablks)
	f.Write(bytepg2byte(sb.Data))
	return &sb
}

func markAllocated(d []byte, startbit int) {
	// mark a few extra as allocated
	// fmt.Printf("mark allocated from %d\n", startbit)
	for i := startbit / 8; i < common.BSIZE; i++ {
		d[i] = byte(0xff)
	}
}

func writeLog(f *os.File) {
	zeroblock := mkBlock()
	for i := 0; i < nlogblks; i++ {
		f.Write(zeroblock)
	}
}

func writeInodeMap(f *os.File, sb *fs.Superblock_t) {
	ninode := ninodeblks * (common.BSIZE / fs.ISIZE)
	oneblock := mkBlock()
	oneblock[0] |= 1 << 0 // mark root inode as allocated
	if sb.Imaplen() == 1 {
		markAllocated(oneblock, ninode)
		f.Write(oneblock)
	} else {
		f.Write(oneblock)
		block := mkBlock()
		for i := 1; i < sb.Imaplen()-2; i++ {
			f.Write(block)
		}
		start := nbitsperblock - ninode%nbitsperblock
		markAllocated(block, start)
		f.Write(block)
	}
}

func writeBlockMap(f *os.File, sb *fs.Superblock_t) {
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
		for i := 1; i < sb.Freeblocklen()-2; i++ {
			f.Write(block)
		}
		// write last block
		o := ndatablks % nbitsperblock
		markAllocated(block, o)
		f.Write(block)
	}
}

func writeInodes(f *os.File, sb *fs.Superblock_t) {
	b := common.MkBlock(0, "", nil, nil, nil)
	b.Data = &common.Bytepg_t{}
	root := fs.Inode_t{b, 0}

	firstdata := sb.Freeblock() + sb.Freeblocklen() + sb.Inodelen()
	root.W_itype(fs.I_DIR)
	root.W_linkcount(1)
	root.W_size(common.BSIZE)
	root.W_addr(0, firstdata)
	block := bytepg2byte(b.Data)

	if tell(f) != sb.Freeblock()+sb.Freeblocklen() {
		panic("inodes don't line up")
	}

	f.Write(block)
	zeroblock := mkBlock()
	for i := 1; i < sb.Inodelen(); i++ {
		f.Write(zeroblock)
	}
}

func writeDataBlocks(f *os.File, sb *fs.Superblock_t) {
	// Root directory data
	data := &common.Bytepg_t{}
	ddata := fs.Dirdata_t{data[:]}
	ddata.W_filename(0, ".")
	ddata.W_inodenext(0, 0)
	ddata.W_filename(1, "..")
	ddata.W_inodenext(1, 0)
	d := bytepg2byte(data)

	if tell(f) != sb.Freeblock()+sb.Freeblocklen()+sb.Inodelen() {
		panic("inodes don't line up")
	}

	f.Write(d) // first block for root
	zeroblock := mkBlock()
	for i := 1; i < ndatablks; i++ {
		f.Write(zeroblock)
	}
}

// A disk with an empty file system (i.e., only root directory)
func MkDisk(image string) {
	fmt.Printf("Make FS image %s\n", image)
	f, err := os.Create(image)
	if err != nil {
		panic(err)
	}

	start := 1
	writeBootBlock(f, start)
	sb := writeSuperBlock(f, start)
	writeLog(f)
	writeInodeMap(f, sb)
	writeBlockMap(f, sb)
	writeInodes(f, sb)
	writeDataBlocks(f, sb)

	f.Sync()
	f.Close()
}
