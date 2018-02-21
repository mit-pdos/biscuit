package common

import "sync"
import "fmt"
import "container/list"

const bdev_debug = false

// If you change this, you must change corresponding constants in mkbdisk.py,
// fs.go, litc.c (fopendir, BSIZE), usertests.c (BSIZE).
const BSIZE = 4096

type Blockmem_i interface {
	Alloc() (Pa_t, *Bytepg_t, bool)
	Free(Pa_t)
}

type Block_cb_i interface {
	Relse(*Bdev_block_t, string)
}

type Bdev_block_t struct {
	sync.Mutex
	Block int
	Pa    Pa_t
	Data  *Bytepg_t
	Name  string
	Mem   Blockmem_i
	Disk  Disk_i
	Cb    Block_cb_i
}

type Bdevcmd_t uint

const (
	BDEV_WRITE Bdevcmd_t = 1
	BDEV_READ            = 2
	BDEV_FLUSH           = 3
)

type Bdev_req_t struct {
	Cmd   Bdevcmd_t
	Blks  *list.List
	AckCh chan bool
	Sync  bool
}

func MkRequest(blks *list.List, cmd Bdevcmd_t, sync bool) *Bdev_req_t {
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
		fmt.Printf("evict: block %v %#x\n", blk.Block, blk.Pa)
	}
	blk.Mem.Free(blk.Pa)
}

func (blk *Bdev_block_t) Evictnow() bool {
	return false
}

func (blk *Bdev_block_t) Done(s string) {
	if blk.Cb != nil {
		blk.Cb.Relse(blk, s)
	}
}

func (b *Bdev_block_t) Write() {
	if bdev_debug {
		fmt.Printf("bdev_write %v %v\n", b.Block, b.Name)
	}
	if b.Data[0] == 0xc && b.Data[1] == 0xc { // XXX check
		panic("write\n")
	}
	l := list.New()
	l.PushBack(b)
	req := MkRequest(l, BDEV_WRITE, true)
	if b.Disk.Start(req) {
		<-req.AckCh
	}
}

func (b *Bdev_block_t) Write_async() {
	if bdev_debug {
		fmt.Printf("bdev_write_async %v %s\n", b.Block, b.Name)
	}
	// if b.data[0] == 0xc && b.data[1] == 0xc {  // XXX check
	//	panic("write_async\n")
	//}
	l := list.New()
	l.PushBack(b)
	ider := MkRequest(l, BDEV_WRITE, false)
	b.Disk.Start(ider)
}

func (b *Bdev_block_t) Read() {
	l := list.New()
	l.PushBack(b)
	ider := MkRequest(l, BDEV_READ, true)
	if b.Disk.Start(ider) {
		<-ider.AckCh
	}
	if bdev_debug {
		fmt.Printf("bdev_read %v %v %#x %#x\n", b.Block, b.Name, b.Data[0], b.Data[1])
	}

	// XXX sanity check, but ignore it during recovery
	if b.Data[0] == 0xc && b.Data[1] == 0xc {
		fmt.Printf("WARNING: %v %v\n", b.Name, b.Block)
	}

}

func (blk *Bdev_block_t) New_page() {
	pa, d, ok := blk.Mem.Alloc()
	if !ok {
		panic("oom during bdev.new_page")
	}
	blk.Pa = pa
	blk.Data = d
}

func MkBlock_newpage(block int, s string, mem Blockmem_i, d Disk_i, cb Block_cb_i) *Bdev_block_t {
	b := MkBlock(block, s, mem, d, cb)
	b.New_page()
	return b
}

func MkBlock(block int, s string, mem Blockmem_i, d Disk_i, cb Block_cb_i) *Bdev_block_t {
	b := &Bdev_block_t{}
	b.Block = block
	b.Pa = Pa_t(0)
	b.Data = nil
	b.Name = s
	b.Mem = mem
	b.Disk = d
	b.Cb = cb
	return b
}

func (blk *Bdev_block_t) free_page() {
	blk.Mem.Free(blk.Pa)
}
