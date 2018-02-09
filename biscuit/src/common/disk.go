package common

import "sync"
import "fmt"
import "unsafe"

const bdev_debug = true

// If you change this, you must change corresponding constants in mkbdisk.py,
// fs.go, litc.c (fopendir, BSIZE), usertests.c (BSIZE).
const BSIZE=4096

type Bdev_block_t struct {
	sync.Mutex
	Block	int
	Pa      Pa_t
	Data	*Bytepg_t
	Name    string
	Mem     Page_i
	Disk    Disk_i
}

type Bdevcmd_t uint

const (
	BDEV_WRITE Bdevcmd_t = 1
	BDEV_READ = 2
	BDEV_FLUSH = 3
)

type Bdev_req_t struct {
	Blks     []*Bdev_block_t
	AckCh	chan bool
	Cmd	Bdevcmd_t
	Sync    bool
}

func MkRequest(blks []*Bdev_block_t, cmd Bdevcmd_t, sync bool) *Bdev_req_t {
	ret := &Bdev_req_t{}
	ret.Blks = blks
	ret.AckCh = make(chan bool)
	ret.Cmd = cmd
	ret.Sync = sync
	return ret
}

type Disk_i interface {
	Start(*Bdev_req_t) bool
	Stats() string
}

func (blk *Bdev_block_t) Key() int {
	return blk.Block
}

func (blk *Bdev_block_t) Evict() {
	if bdev_debug {
		fmt.Printf("evict: block %v %#x %v\n", blk.Block, blk.Pa, blk.Mem.Refcnt(blk.Pa))
	}
	blk.free_page()
}

func (blk *Bdev_block_t) Evictnow() bool {
	return false
}

func MkBlock_newpage(block int, s string, mem Page_i, d Disk_i) *Bdev_block_t {
	b := MkBlock(block, Pa_t(0), s, mem, d)
	b.New_page()
	return b
}

func (b *Bdev_block_t) Write() {
	if bdev_debug {
		fmt.Printf("bdev_write %v %v\n", b.Block, b.Name)
	}
	if b.Data[0] == 0xc && b.Data[1] == 0xc {  // XXX check
		panic("write\n")
	}
	req := MkRequest([]*Bdev_block_t{b}, BDEV_WRITE, true)
	if b.Disk.Start(req) {
		<- req.AckCh
	}
} 

func (b *Bdev_block_t) Write_async() {
	if bdev_debug {
		fmt.Printf("bdev_write_async %v %s\n", b.Block, b.Name)
	}
	// if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
	//	panic("write_async\n")
	//}
	ider := MkRequest([]*Bdev_block_t{b}, BDEV_WRITE, false)
	b.Disk.Start(ider)
}

func (b *Bdev_block_t) Read() {
	ider := MkRequest([]*Bdev_block_t{b}, BDEV_READ, true)
	if b.Disk.Start(ider) {
		<- ider.AckCh
	}
	fmt.Printf("Read done %v %p\n", len(b.Data), b.Data)
	if bdev_debug {
		fmt.Printf("bdev_read %v %v %#x %#x\n", b.Block, b.Name, b.Data[0], b.Data[1])
	}
	
	// XXX sanity check, but ignore it during recovery
	if b.Data[0] == 0xc && b.Data[1] == 0xc {
		fmt.Printf("WARNING: %v %v\n", b.Name, b.Block)
	}
	
}

func (blk *Bdev_block_t) New_page() {
	_, pa, ok := blk.Mem.Refpg_new()
	fmt.Printf("New_page %v\n", pa)
	if !ok {
		panic("oom during bdev.new_page")
	}
	blk.Pa = pa
	blk.Data = (*Bytepg_t)(unsafe.Pointer(blk.Mem.Dmap(pa)))
	blk.Mem.Refup(blk.Pa)
}

func MkBlock(block int, pa Pa_t, s string, mem Page_i, d Disk_i) *Bdev_block_t {
	b := &Bdev_block_t{};
	b.Block = block
	b.Pa = pa
	b.Data = (*Bytepg_t)(unsafe.Pointer(mem.Dmap(pa)))
	b.Name = s
	b.Mem = mem
	b.Disk = d
	return b
}


func (blk *Bdev_block_t) free_page() {
	blk.Mem.Refdown(blk.Pa)
}

