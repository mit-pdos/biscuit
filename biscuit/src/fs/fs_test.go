package fs

import "testing"
import "fmt"
import "os"

import "common"

//
// The "driver"
//

var ahci = &ahci_disk_t{}

type ahci_disk_t struct {
	f *os.File
}


func (ahci *ahci_disk_t) Seek(o int) {
	_, err := ahci.f.Seek(int64(o), 0)
	if err != nil {
		panic("Seek failed")
	}
}

func (ahci *ahci_disk_t) Start(req *common.Bdev_req_t) bool {
	switch req.Cmd {
	case common.BDEV_READ:
		if len(req.Blks) != 1 {
			panic("read: too many blocks")
		}
		ahci.Seek(req.Blks[0].Block * common.BSIZE)
		b := make([]byte, common.BSIZE)
		n, err := ahci.f.Read(b)
		if n != common.BSIZE || err != nil {
			panic("Read failed")
		}
		req.Blks[0].Data = &common.Bytepg_t{}
		for i, _ := range(b) {
			req.Blks[0].Data[i] = uint8(b[i])
		}
		fmt.Printf("read is done\n")
	case common.BDEV_WRITE:
		for _, b := range(req.Blks) {
			ahci.Seek(b.Block * common.BSIZE)
			buf := make([]byte, common.BSIZE)
			for i, _ := range(buf) {
				buf[i] = byte(b.Data[i])
			}
			n, err := ahci.f.Write(buf)
			if n != common.BSIZE || err != nil {
				panic("Write failed")
			}
		}
		fmt.Printf("write is done\n")
	case common.BDEV_FLUSH:
		ahci.f.Sync()
		fmt.Printf("flush is done\n")
	}
	return false
}

func (ahci *ahci_disk_t) Stats() string {
	return ""
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



type console_t struct {
}
var c console_t

func (c console_t) Cons_read(ub common.Userio_i, offset int) (int, common.Err_t) {
	return -1, 0
}

func (c console_t) Cons_write(src common.Userio_i, off int) (int, common.Err_t) {
	return 0, 0
}

//
// Test
//

func TestFS(*testing.T) {
	f, uerr := os.OpenFile("../../go.img", os.O_RDWR, 0755)
	if uerr != nil {
		panic("couldn't open disk image\n")
	}
	ahci.f = f
	fmt.Printf("testFS")
	_ = MkFS(blockmem, ahci, c)
	
	fd, err := Fs_open("f1", common.O_CREAT, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("Fs_open f1 failed %v\n", err)
	}
	
	hdata := make([]uint8, 512)
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)

	n, err := fd.Fops.Write(nil, ub)
	if err != 0 || n != len(hdata) {
		fmt.Printf("Write f1 failed %v %d\n", err, n)
	}
	
	err = fd.Fops.Close()
	if err != 0 {
		fmt.Printf("Close f1 failed %v\n", err)
	}

	err = Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
	}

}

