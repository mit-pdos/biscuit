package ufs

import "os"

import "common"

//
// The "driver"
//

type ahci_disk_t struct {
	f *os.File
	t *tracef_t
}

func (ahci *ahci_disk_t) StartTrace() {
	ahci.t = mkTrace()
}

func (ahci *ahci_disk_t) Seek(o int) {
	_, err := ahci.f.Seek(int64(o), 0)
	if err != nil {
		panic(err)
	}
}

func (ahci *ahci_disk_t) Start(req *common.Bdev_req_t) bool {
	switch req.Cmd {
	case common.BDEV_READ:
		if req.Blks.Len() != 1 {
			panic("read: too many blocks")
		}
		blk := req.Blks.FrontBlock()
		ahci.Seek(blk.Block * common.BSIZE)
		b := make([]byte, common.BSIZE)
		n, err := ahci.f.Read(b)
		if n != common.BSIZE || err != nil {
			panic(err)
		}
		blk.Data = &common.Bytepg_t{}
		for i, _ := range b {
			blk.Data[i] = uint8(b[i])
		}
	case common.BDEV_WRITE:
		for b := req.Blks.FrontBlock(); b != nil; b = req.Blks.NextBlock() {
			ahci.Seek(b.Block * common.BSIZE)
			buf := make([]byte, common.BSIZE)
			for i, _ := range buf {
				buf[i] = byte(b.Data[i])
			}
			n, err := ahci.f.Write(buf)
			if n != common.BSIZE || err != nil {
				panic(err)
			}
			if ahci.t != nil {
				ahci.t.write(b.Block, b.Data)
			}
			b.Done("Start")
		}
	case common.BDEV_FLUSH:
		ahci.f.Sync()
		if ahci.t != nil {
			ahci.t.sync()
		}
	}
	return false
}

func (ahci *ahci_disk_t) Stats() string {
	return ""
}

func (ahci *ahci_disk_t) close() {
	if ahci.t != nil {
		ahci.t.close()
	}
	// ahci.f.Sync()
	err := ahci.f.Close()
	if err != nil {
		panic(err)
	}
}

//
// Glue
//

type blockmem_t struct {
}

var blockmem = &blockmem_t{}

func (bm *blockmem_t) Alloc() (common.Pa_t, *common.Bytepg_t, bool) {
	d := &common.Bytepg_t{}
	return common.Pa_t(0), d, true
}

func (bm *blockmem_t) Free(pa common.Pa_t) {
}

func (bm *blockmem_t) Refup(pa common.Pa_t) {
}

type console_t struct {
}

var c console_t

func (c console_t) Cons_read(ub common.Userio_i, offset int) (int, common.Err_t) {
	return -1, 0
}

func (c console_t) Cons_write(src common.Userio_i, off int) (int, common.Err_t) {
	return 0, 0
}
