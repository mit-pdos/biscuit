package fs

import "testing"
import "fmt"
import "unsafe"
import "os"

import "common"

//
// The "driver"
//

var ahci = &ahci_disk_t{}

type ahci_disk_t struct {
	f *os.File
}

func (ahci *ahci_disk_t) MkRequest(blks []*common.Bdev_block_t, cmd common.Bdevcmd_t, sync bool) *common.Bdev_req_t {
	ret := &common.Bdev_req_t{}
	ret.Blks = blks
	ret.AckCh = make(chan bool)
	ret.Cmd = cmd
	ret.Sync = sync
	return ret
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
		ahci.Seek(req.Blks[0].Block * BSIZE)
		b := make([]byte, BSIZE)
		n, err := ahci.f.Read(b)
		if n != BSIZE || err != nil {
			panic("Read failed")
		}
		req.Blks[0].Data = &common.Bytepg_t{}
		for i, _ := range(b) {
			req.Blks[0].Data[i] = uint8(b[i])
		}
		fmt.Printf("read is done\n")
	case common.BDEV_WRITE:
		for _, b := range(req.Blks) {
			ahci.Seek(b.Block * BSIZE)
			buf := make([]byte, BSIZE)
			for i, _ := range(buf) {
				buf[i] = byte(b.Data[i])
			}
			n, err := ahci.f.Write(buf)
			if n != BSIZE || err != nil {
				panic("Write failed")
			}
		}
		fmt.Printf("write is done\n")
	case common.BDEV_FLUSH:
		ahci.f.Sync()
	}
	return false
}

func (ahci *ahci_disk_t) Stats() string {
	return ""
}


//
// Glue
//

type mem_t struct {
}

func (mem mem_t) Refpg_new() (*common.Pg_t, common.Pa_t, bool) {
	// p_pg := &bytecommon.Pg_t{}
	// r := (*pa_t)(unsafe.Pointer(&p_pg))
	// printf("p_pg = %p r = %p\n", &_pgr)
	return nil, 0, true
}

func (mem mem_t) Dmap(p common.Pa_t) *common.Pg_t {
	r := (*common.Pg_t)(unsafe.Pointer(p))
	fmt.Printf("r = %p\n", r)
	return r
}

func (mem mem_t) Refcnt(p_pg common.Pa_t) int {
	return 0
}

func (mem mem_t) Refup(p_pg common.Pa_t) {
}

func (mem mem_t) Refdown(p_pg common.Pa_t) bool {
	return false
}


//
// Test
//

func TestFS(*testing.T) {
	f, err := os.Open("go.img")
	if err != nil {
		panic("couldn't open disk image\n")
	}
	ahci.f = f
	mem := mem_t{}
	fmt.Printf("testFS")
	_ = fs_init(mem, ahci)
}

