package ufs

import "fmt"
import "os"

import "log"

import "common"
import "fs"

//
// FS
//

type Ufs_t struct {
	ahci *ahci_disk_t
	fs   *fs.Fs_t
	cwd  *common.Cwd_t
}

func mkData(v uint8, n int) *common.Fakeubuf_t {
	hdata := make([]uint8, n)
	for i := range hdata {
		hdata[i] = v
	}
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)
	return ub
}

func MkBuf(b []byte) *common.Fakeubuf_t {
	hdata := make([]uint8, len(b))
	for i := range hdata {
		hdata[i] = uint8(b[i])
	}
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)
	return ub
}

func (ufs *Ufs_t) Sync() common.Err_t {
	err := ufs.fs.Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func (ufs *Ufs_t) SyncApply() common.Err_t {
	err := ufs.fs.Fs_syncapply()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func (ufs *Ufs_t) MkFile(p string, ub *common.Fakeubuf_t) common.Err_t {
	fd, err := ufs.fs.Fs_open(p, common.O_CREAT, 0, ufs.cwd, 0, 0)
	if err != 0 {
		fmt.Printf("ufs.fs.Fs_open %v failed %v\n", p, err)
		return err
	}
	if ub != nil {
		n, err := fd.Fops.Write(nil, ub)
		if err != 0 || ub.Remain() != 0 {
			fmt.Printf("Write %s failed %v %d\n", p, err, n)
			return err
		}
	}
	err = fd.Fops.Close()
	if err != 0 {
		fmt.Printf("Close %s failed %v\n", p, err)
		return err
	}
	return err
}

func (ufs *Ufs_t) MkDir(p string) common.Err_t {
	err := ufs.fs.Fs_mkdir(p, 0755, ufs.cwd)
	if err != 0 {
		fmt.Printf("mkDir %v failed %v\n", p, err)
		return err
	}
	return err
}

func (ufs *Ufs_t) Rename(oldp, newp string) common.Err_t {
	err := ufs.fs.Fs_rename(oldp, newp, ufs.cwd)
	if err != 0 {
		fmt.Printf("doRename %v %v failed %v\n", oldp, newp, err)
	}
	return err
}

// update (XXX check that ub < len(file)?)
func (ufs *Ufs_t) Update(p string, ub *common.Fakeubuf_t) common.Err_t {
	fd, err := ufs.fs.Fs_open(p, common.O_RDWR, 0, ufs.cwd, 0, 0)
	if err != 0 {
		fmt.Printf("ufs.fs.Fs_open %v failed %v\n", p, err)
	}
	n, err := fd.Fops.Write(nil, ub)
	if err != 0 || ub.Remain() != 0 {
		fmt.Printf("Write %s failed %v %d\n", p, err, n)
		return err
	}

	err = fd.Fops.Close()
	if err != 0 {
		fmt.Printf("Close %s failed %v\n", p, err)
		return err
	}
	return err
}

func (ufs *Ufs_t) Append(p string, ub *common.Fakeubuf_t) common.Err_t {
	fd, err := ufs.fs.Fs_open(p, common.O_RDWR, 0, ufs.cwd, 0, 0)
	if err != 0 {
		fmt.Printf("ufs.fs.Fs_open %v failed %v\n", p, err)
	}

	_, err = fd.Fops.Lseek(0, common.SEEK_END)
	if err != 0 {
		fmt.Printf("Lseek %v failed %v\n", p, err)
		return err
	}

	n, err := fd.Fops.Write(nil, ub)
	if err != 0 || ub.Remain() != 0 {
		fmt.Printf("Write %s failed %v %d\n", p, err, n)
		return err
	}

	err = fd.Fops.Close()
	if err != 0 {
		fmt.Printf("Close %s failed %v\n", p, err)
		return err
	}
	return err
}

func (ufs *Ufs_t) Unlink(p string) common.Err_t {
	err := ufs.fs.Fs_unlink(p, ufs.cwd, false)
	if err != 0 {
		fmt.Printf("doUnlink %v failed %v\n", p, err)
		return err
	}
	return err
}

func (ufs *Ufs_t) UnlinkDir(p string) common.Err_t {
	err := ufs.fs.Fs_unlink(p, ufs.cwd, true)
	if err != 0 {
		fmt.Printf("doUnlink %v failed %v\n", p, err)
		return err
	}
	return err
}

func (ufs *Ufs_t) Stat(p string) (*common.Stat_t, common.Err_t) {
	s := &common.Stat_t{}
	err := ufs.fs.Fs_stat(p, s, ufs.cwd)
	if err != 0 {
		return nil, err
	}
	return s, err
}

func (ufs *Ufs_t) Read(p string) ([]byte, common.Err_t) {
	st, err := ufs.Stat(p)
	if err != 0 {
		fmt.Printf("doStat %v failed %v\n", p, err)
		return nil, err
	}
	fd, err := ufs.fs.Fs_open(p, common.O_RDONLY, 0, ufs.cwd, 0, 0)
	if err != 0 {
		fmt.Printf("ufs.fs.Fs_open %v failed %v\n", p, err)
		return nil, err
	}
	hdata := make([]uint8, st.Size())
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)

	n, err := fd.Fops.Read(nil, ub)
	if err != 0 || n != len(hdata) {
		fmt.Printf("Read %s failed %v %d\n", p, err, n)
		return nil, err
	}
	v := make([]byte, st.Size())
	for i, _ := range hdata {
		v[i] = byte(hdata[i])
	}
	return v, err
}

func (ufs *Ufs_t) Ls(p string) (map[string]*common.Stat_t, common.Err_t) {
	res := make(map[string]*common.Stat_t, 100)
	d, e := ufs.Read(p)
	if e != 0 {
		return nil, e
	}
	for i := 0; i < len(d)/fs.BSIZE; i++ {
		dd := fs.Dirdata_t{d[i*fs.BSIZE:]}
		for j := 0; j < fs.NDIRENTS; j++ {
			tfn := dd.Filename(j)
			if len(tfn) > 0 {
				f := p + "/" + tfn
				st, e := ufs.Stat(f)
				if e != 0 {
					return nil, e
				}
				res[tfn] = st
			}
		}
	}
	return res, 0
}

func (ufs *Ufs_t) Statistics() string {
	return ufs.fs.Fs_statistics()
}

func openDisk(d string) *ahci_disk_t {
	a := &ahci_disk_t{}
	f, uerr := os.OpenFile(d, os.O_RDWR, 0755)
	if uerr != nil {
		panic(uerr)
	}
	a.f = f
	return a
}

func BootFS(dst string) *Ufs_t {
	log.Printf("reboot %v ...\n", dst)
	ufs := &Ufs_t{}
	ufs.ahci = openDisk(dst)
	ufs.cwd = ufs.fs.MkRootCwd()
	_, ufs.fs = fs.StartFS(blockmem, ufs.ahci, c)
	return ufs
}

func ShutdownFS(ufs *Ufs_t) {
	// fmt.Printf("Shutdown: %s\n", ufs.Statistics())
	ufs.fs.StopFS()
	ufs.ahci.close()
}
