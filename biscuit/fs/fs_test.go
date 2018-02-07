package fs

import "testing"
import "fmt"
import "unsafe"
import "os"

//
// The "driver"
//

const ahci_debug = true
var ahci = &ahci_disk_t{}

type bdevcmd_t uint

const (
	BDEV_WRITE bdevcmd_t = 1
	BDEV_READ = 2
	BDEV_FLUSH = 3
)

type bdev_req_t struct {
	blks     []*bdev_block_t
	ackCh	chan bool
	cmd	bdevcmd_t
	sync    bool
}

type adisk_t interface {
	Start(*bdev_req_t) bool
	Stats() string
	mkRequest([]*bdev_block_t, bdevcmd_t, bool) *bdev_req_t
}

type ahci_disk_t struct {
	f *os.File
}

func (ahci *ahci_disk_t) mkRequest(blks []*bdev_block_t, cmd bdevcmd_t, sync bool) *bdev_req_t {
	ret := &bdev_req_t{}
	ret.blks = blks
	ret.ackCh = make(chan bool)
	ret.cmd = cmd
	ret.sync = sync
	return ret
}

func (ahci *ahci_disk_t) Seek(o int) {
	_, err := ahci.f.Seek(int64(o), 0)
	if err != nil {
		panic("Seek failed")
	}
}

func (ahci *ahci_disk_t) Start(req *bdev_req_t) bool {
	switch req.cmd {
	case BDEV_READ:
		if len(req.blks) != 1 {
			panic("read: too many blocks")
		}
		ahci.Seek(req.blks[0].block * BSIZE)
		b := make([]byte, BSIZE)
		n, err := ahci.f.Read(b)
		if n != BSIZE || err != nil {
			panic("Read failed")
		}
		req.blks[0].data = &bytepg_t{}
		for i, _ := range(b) {
			req.blks[0].data[i] = uint8(b[i])
		}
		fmt.Printf("read is done\n")
	case BDEV_WRITE:
		for _, b := range(req.blks) {
			ahci.Seek(b.block * BSIZE)
			buf := make([]byte, BSIZE)
			for i, _ := range(buf) {
				buf[i] = byte(b.data[i])
			}
			n, err := ahci.f.Write(buf)
			if n != BSIZE || err != nil {
				panic("Write failed")
			}
		}
		fmt.Printf("write is done\n")
	case BDEV_FLUSH:
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

func refpg_new() (*pg_t, pa_t, bool) {
	// p_pg := &bytepg_t{}
	// r := (*pa_t)(unsafe.Pointer(&p_pg))
	// printf("p_pg = %p r = %p\n", &_pgr)
	return nil, 0, true
}

func dmap(p pa_t) *pg_t {
	r := (*pg_t)(unsafe.Pointer(p))
	fmt.Printf("r = %p\n", r)
	return r
}

//
// Test
//

func TestFS(*testing.T) {
	f, err := os.Open("../go.img")
	if err != nil {
		panic("couldn't open disk image\n")
	}
	ahci.f = f
	fmt.Printf("testFS")
	_ = fs_init()
}

