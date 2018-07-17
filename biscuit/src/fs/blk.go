package fs

import "sync"
import "fmt"
import "container/list"

import "mem"

// If you change this, you must change corresponding constants in litc.c
// (fopendir, BSIZE), usertests.c (BSIZE).
const BSIZE = 4096

type Blockmem_i interface {
	Alloc() (mem.Pa_t, *mem.Bytepg_t, bool)
	Free(mem.Pa_t)
	Refup(mem.Pa_t)
}

type Block_cb_i interface {
	Relse(*Bdev_block_t, string)
}

type blktype_t int

const (
	DataBlk   blktype_t = 0
	CommitBlk blktype_t = -1
	RevokeBlk blktype_t = -2
)

type Bdev_block_t struct {
	sync.Mutex
	Block      int
	Type       blktype_t
	_try_evict bool
	Pa         mem.Pa_t
	Data       *mem.Bytepg_t
	Ref        *Objref_t
	Name       string
	Mem        Blockmem_i
	Disk       Disk_i
	Cb         Block_cb_i
}

type Bdevcmd_t uint

const (
	BDEV_WRITE Bdevcmd_t = 1
	BDEV_READ            = 2
	BDEV_FLUSH           = 3
)

// A wrapper around List for blocks
type BlkList_t struct {
	l *list.List
	e *list.Element // iterator
}

func MkBlkList() *BlkList_t {
	bl := &BlkList_t{}
	bl.l = list.New()
	return bl
}

func (bl *BlkList_t) Len() int {
	return bl.l.Len()
}

func (bl *BlkList_t) PushBack(b *Bdev_block_t) {
	bl.l.PushBack(b)
}

func (bl *BlkList_t) FrontBlock() *Bdev_block_t {
	if bl.l.Front() == nil {
		return nil
	} else {
		bl.e = bl.l.Front()
		return bl.e.Value.(*Bdev_block_t)
	}
}

func (bl *BlkList_t) Back() *Bdev_block_t {
	if bl.l.Back() == nil {
		return nil
	} else {
		return bl.l.Back().Value.(*Bdev_block_t)
	}
}

func (bl *BlkList_t) BackBlock() *Bdev_block_t {
	if bl.l.Back() == nil {
		panic("bl.Front")
	} else {
		return bl.l.Back().Value.(*Bdev_block_t)
	}
}

func (bl *BlkList_t) RemoveBlock(block int) {
	var next *list.Element
	for e := bl.l.Front(); e != nil; e = next {
		next = e.Next()
		b := e.Value.(*Bdev_block_t)
		if b.Block == block {
			bl.l.Remove(e)
		}
	}
}

func (bl *BlkList_t) NextBlock() *Bdev_block_t {
	if bl.e == nil {
		return nil
	} else {
		bl.e = bl.e.Next()
		if bl.e == nil {
			return nil
		}
		return bl.e.Value.(*Bdev_block_t)
	}
}

func (bl *BlkList_t) Apply(f func(*Bdev_block_t)) {
	for b := bl.FrontBlock(); b != nil; b = bl.NextBlock() {
		f(b)
	}
}
func (bl *BlkList_t) Print() {
	bl.Apply(func(b *Bdev_block_t) {
		fmt.Printf("b %v\n", b)
	})
}

func (bl *BlkList_t) Append(l *BlkList_t) {
	for b := l.FrontBlock(); b != nil; b = l.NextBlock() {
		bl.PushBack(b)
	}
}

func (bl *BlkList_t) Delete() {
	var next *list.Element
	for e := bl.l.Front(); e != nil; e = next {
		next = e.Next()
		bl.l.Remove(e)
	}
}

type Bdev_req_t struct {
	Cmd   Bdevcmd_t
	Blks  *BlkList_t
	AckCh chan bool
	Sync  bool
}

func MkRequest(blks *BlkList_t, cmd Bdevcmd_t, sync bool) *Bdev_req_t {
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

func (blk *Bdev_block_t) EvictFromCache() {
	// nothing to be done right before being evicted
}

func (blk *Bdev_block_t) EvictDone() {
	if bdev_debug {
		fmt.Printf("Done: block %v %#x\n", blk.Block, blk.Pa)
	}
	blk.Mem.Free(blk.Pa)
}

func (blk *Bdev_block_t) Tryevict() {
	blk._try_evict = true
}

func (blk *Bdev_block_t) Evictnow() bool {
	return blk._try_evict
}

func (blk *Bdev_block_t) Done(s string) {
	if blk.Cb == nil {
		panic("wtf")
	}
	blk.Cb.Relse(blk, s)
}

func (b *Bdev_block_t) Write() {
	if bdev_debug {
		fmt.Printf("bdev_write %v %v\n", b.Block, b.Name)
	}
	//if b.Data[0] == 0xc && b.Data[1] == 0xc { // XXX check
	//	panic("write\n")
	//}
	l := MkBlkList()
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
	l := MkBlkList()
	l.PushBack(b)
	ider := MkRequest(l, BDEV_WRITE, false)
	b.Disk.Start(ider)
}

func (b *Bdev_block_t) Read() {
	l := MkBlkList()
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

func MkBlock(block int, s string, m Blockmem_i, d Disk_i, cb Block_cb_i) *Bdev_block_t {
	b := &Bdev_block_t{}
	b.Block = block
	b.Pa = mem.Pa_t(0)
	b.Data = nil
	//b.Name = s
	b.Mem = m
	b.Disk = d
	b.Cb = cb
	return b
}

func (blk *Bdev_block_t) Free_page() {
	blk.Mem.Free(blk.Pa)
}
