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

func mkFile(p string) common.Err_t {
	fd, err := Fs_open(p, common.O_CREAT, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("Fs_open %v failed %v\n", p, err)
	}
	
	hdata := make([]uint8, 512)
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)

	n, err := fd.Fops.Write(nil, ub)
	if err != 0 || n != len(hdata) {
		fmt.Printf("Write %s failed %v %d\n", p, err, n)
		return err
	}
	
	err = fd.Fops.Close()
	if err != 0 {
		fmt.Printf("Close %s failed %v\n", p, err)
		return err
	}

	err = Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func mkDir(p string) common.Err_t {
	err := Fs_mkdir(p, 0755, 0)
	if err != 0 {
		fmt.Printf("mkDir %v failed %v\n", p, err)
		return err
	}
	err = Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func doRename (oldp, newp string) common.Err_t {
	err := Fs_rename(oldp, newp, 0)
	if err != 0 {
		fmt.Printf("doRename %v %v failed %v\n", oldp, newp, err)
	}
	err = Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
	}
	return err
}

func doAppend(p string) common.Err_t {
	fd, err := Fs_open(p, common.O_RDWR, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("Fs_open %v failed %v\n", p, err)
	}

	_, err = fd.Fops.Lseek(0, common.SEEK_END)
	if err != 0 {
		fmt.Printf("Lseek %v failed %v\n", p, err)
		return err
	}
	
	hdata := make([]uint8, 512)
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)

	n, err := fd.Fops.Write(nil, ub)
	if err != 0 || n != len(hdata) {
		fmt.Printf("Write %s failed %v %d\n", p, err, n)
		return err
	}
	
	err = fd.Fops.Close()
	if err != 0 {
		fmt.Printf("Close %s failed %v\n", p, err)
		return err
	}
	err = Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func doUnlink(p string) common.Err_t {
	err := Fs_unlink(p, 0, false)
	if err != 0 {
		fmt.Printf("doUnlink %v failed %v\n", p, err)
		return err
	}
	err = Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func TestFS(t *testing.T) {
	f, uerr := os.OpenFile("../../go.img", os.O_RDWR, 0755)
	if uerr != nil {
		panic("couldn't open disk image\n")
	}
	ahci.f = f
	fmt.Printf("testFS\n")

	_ = MkFS(blockmem, ahci, c)
	
	e := mkFile("f1")
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f1")
	}
	
	e = mkFile("f2")
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f2")
	}
	
	e = mkDir("d0")
	if e != 0 {
		t.Fatalf("Mkdir %v failed", "d0")
	}

	e = mkDir("d0/d1")
	if e != 0 {
		t.Fatalf("Mkdir %v failed", "d1")
	}

	e = doRename("d0/d1", "e0")
	if e != 0 {
		t.Fatalf("Rename failed")
	}
	
	e = doAppend("f1")
	if e != 0 {
		t.Fatalf("Append failed")
	}
	
	e = doUnlink("f2")
	if e != 0 {
		t.Fatalf("Unlink failed")
	}

	f.Sync()
	f.Close()
}

