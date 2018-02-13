package fs

import "testing"
import "fmt"
import "os"
import "io"

import "encoding/json"
import "common"

var diskimg = "../../go.img"

//
//  trace file of writes and syncs
//


type tracef_t struct {
	file *os.File
	enc *json.Encoder
}

type record_t struct {
	Cmd string
	BlkNo int
	BlkData []byte
}

type trace_t []record_t
type order_t []int
type orders_t []order_t

func mkTrace() *tracef_t {
	t := &tracef_t{}
	f, uerr := os.Create("trace.json")
	if uerr != nil {
		panic(uerr)
	}
	t.file = f
	t.enc = json.NewEncoder(f)
	return t
}

func readTrace(p string) []record_t {
	res := make([]record_t, 0)
	f, uerr := os.Open("trace.json")
	if uerr != nil {
		panic(uerr)
	}
	dec := json.NewDecoder(f)
	for {
		var r record_t
		if err := dec.Decode(&r); err != nil {
			break
		}
		res = append(res, r)
		
	}
	return res
}	

func (t *tracef_t) write(n int, v *common.Bytepg_t) {
	r := record_t{}
	r.BlkNo = n
	r.Cmd = "write"
	r.BlkData = make([]byte, common.BSIZE)
	for i, _ := range(v) {
		r.BlkData[i] = byte(v[i])
	}
	if err := t.enc.Encode(&r); err != nil {
		panic(err)
        }
}

func (t  *tracef_t) sync() {
	r := record_t{}
	r.BlkNo = 0
	r.Cmd = "sync"
	if err := t.enc.Encode(&r); err != nil {
		panic(err)
        }
}


func (t *tracef_t) close() {
	t.file.Sync()
	t.file.Close()
}

//
// The "driver"
//

type ahci_disk_t struct {
	f *os.File
	t *tracef_t
}

func mkDisk(d string, doTrace bool) *ahci_disk_t {
	a := &ahci_disk_t{}
	f, uerr := os.OpenFile(d, os.O_RDWR, 0755)
	if uerr != nil {
		panic(uerr)
	}
	a.f = f
	if doTrace {
		a.t = mkTrace()
	}
	return a
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
		if len(req.Blks) != 1 {
			panic("read: too many blocks")
		}
		ahci.Seek(req.Blks[0].Block * common.BSIZE)
		b := make([]byte, common.BSIZE)
		n, err := ahci.f.Read(b)
		if n != common.BSIZE || err != nil {
			panic(err)
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
				panic(err)
			}
			if ahci.t != nil {
				ahci.t.write(b.Block, b.Data)
			}

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
	ahci.f.Sync()
	ahci.f.Close()
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

type testfs_t struct {
	fs *Fs_t
}

func (tfs *testfs_t) mkFile(p string) common.Err_t {
	fd, err := tfs.fs.Fs_open(p, common.O_CREAT, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("tfs.fs.Fs_open %v failed %v\n", p, err)
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

	err = tfs.fs.Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func (tfs *testfs_t) mkDir(p string) common.Err_t {
	err := tfs.fs.Fs_mkdir(p, 0755, 0)
	if err != 0 {
		fmt.Printf("mkDir %v failed %v\n", p, err)
		return err
	}
	err = tfs.fs.Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func (tfs *testfs_t) doRename(oldp, newp string) common.Err_t {
	err := tfs.fs.Fs_rename(oldp, newp, 0)
	if err != 0 {
		fmt.Printf("doRename %v %v failed %v\n", oldp, newp, err)
	}
	err = tfs.fs.Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
	}
	return err
}

func (tfs *testfs_t) doAppend(p string) common.Err_t {
	fd, err := tfs.fs.Fs_open(p, common.O_RDWR, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("tfs.fs.Fs_open %v failed %v\n", p, err)
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
	err = tfs.fs.Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func (tfs *testfs_t) doUnlink(p string) common.Err_t {
	err := tfs.fs.Fs_unlink(p, 0, false)
	if err != 0 {
		fmt.Printf("doUnlink %v failed %v\n", p, err)
		return err
	}
	err = tfs.fs.Fs_sync()
	if err != 0 {
		fmt.Printf("Sync failed %v\n", err)
		return err
	}
	return err
}

func (tfs *testfs_t) doStat(p string) (*common.Stat_t, common.Err_t) {
	s := &common.Stat_t{}
	err := tfs.fs.Fs_stat(p, s, 0)
	if err != 0 {
		fmt.Printf("doStat %v failed %v\n", p, err)
		return nil, err
	}
	return s, err
}

func (tfs *testfs_t) doRead(p string) ([]byte, common.Err_t) {
	st, err := tfs.doStat(p)
	if err != 0 {
		fmt.Printf("doStat %v failed %v\n", p, err)
		return nil, err
	}
	fd, err := tfs.fs.Fs_open(p, common.O_RDONLY, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("tfs.fs.Fs_open %v failed %v\n", p, err)
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

func (tfs *testfs_t) doLs(p string) (map[string]*common.Stat_t, common.Err_t) {
	res := make(map[string]*common.Stat_t, 100)
	d, e := tfs.doRead(p)
	if e != 0 {
		return nil, e
	}
	for i := 0; i < len(d)/common.BSIZE; i++ {
		dd := dirdata_t{d[i*common.BSIZE:]}
		for j := 0; j < NDIRENTS; j++ {
			tfn := dd.filename(j)
			if len(tfn) > 0 {
				f := p + "/" + tfn
				st, e := tfs.doStat(f)
				if e != 0 {
					return nil, e
				}
				res[tfn] = st
			}
		}
	}
	return res, 0
}

func (tfs *testfs_t) doTest(t *testing.T) {
	e := tfs.mkFile("f1")
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f1")
	}
	
	e = tfs.mkFile("f2")
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f2")
	}
	
	// e = mkDir("d0")
	// if e != 0 {
	// 	t.Fatalf("Mkdir %v failed", "d0")
	// }

	// e = mkDir("d0/d1")
	// if e != 0 {
	// 	t.Fatalf("Mkdir %v failed", "d1")
	// }

	// e = doRename("d0/d1", "e0")
	// if e != 0 {
	// 	t.Fatalf("Rename failed")
	// }
	
	// e = doAppend("f1")
	// if e != 0 {
	// 	t.Fatalf("Append failed")
	// }
	
	// e = doUnlink("f2")
	// if e != 0 {
	// 	t.Fatalf("Unlink failed")
	//}
}


func (tfs *testfs_t) doCheck(t *testing.T) {
	res, e := tfs.doLs("/")
	if e != 0 {
		t.Fatalf("doLs failed")
	}
	st, ok := res["f1"]
	if !ok {
		t.Fatalf("f1 not present")
	}
	if st.Size() != 512 {    // 1024
	 	t.Fatalf("f1 wrong size")
	}
	
	// st, ok = res["f2"]
	// if ok {
	// 	t.Fatalf("f2 present")
	// }
	// st, ok = res["d0"]
	// if !ok {
	// 	t.Fatalf("d0 not present")
	// }
	// st, ok = res["e0"]
	// if !ok {
	// 	t.Fatalf("e0 not present")
	// }
	// res, e = doLs("/d0")
	// if e != 0 {
	// 	t.Fatalf("doLs d0 failed")
	// }
	// st, ok = res["e0"]
	// if ok {
	// 	t.Fatalf("e0 present in d0")
	// }
}

func copyFileContents(src, dst string) (err error) {
    in, err := os.Open(src)
    if err != nil {
        return
    }
    defer in.Close()
    out, err := os.Create(dst)
    if err != nil {
        return
    }
    defer func() {
        cerr := out.Close()
        if err == nil {
            err = cerr
        }
    }()
    if _, err = io.Copy(out, in); err != nil {
        return
    }
    err = out.Sync()
    return
}

func check(t *testing.T, d string) {
	tfs := &testfs_t{}
	fmt.Printf("reboot and check %v ...\n", d)
	ahci := mkDisk(d, false)
	_, tfs.fs = StartFS(blockmem, ahci, c)
	tfs.doCheck(t)
	tfs.fs.StopFS()
	ahci.close()
}

func TestFS(t *testing.T) {
	dst := "tmp.img"
	copyFileContents(diskimg, dst)

	ahci := mkDisk(dst, true)
	
	fmt.Printf("testFS %v ...\n", dst)
	
	tfs := &testfs_t{}
	_, tfs.fs = StartFS(blockmem, ahci, c)
	tfs.doTest(t)
	tfs.fs.StopFS()
	ahci.close()
}

func genOrder(blks []int, o order_t, r orders_t) orders_t {
	if len(blks) == 0 {
		// fmt.Printf("find order: %v\n", o)
		r = append(r, o)
		return r
	}
	// fmt.Printf("genOrder %v %v %v\n", blks, o, r)
	for i, v := range(blks) {
		t := make([]int, len(blks))
		copy(t, blks)
		t = append(t[0:i], t[i+1:]...)
		o1 := append(o, v)
		r = genOrder(t, o1, r)
	}
	return r
}

func genOrders(blks []int) orders_t {
	r := make(orders_t, 0)
	o := make(order_t, 0)
	r = genOrder(blks, o, r)
	// fmt.Printf("r=%v\n", r)
	return r
}

func printTrace(t trace_t, start int, end int) {
	fmt.Printf("trace (%d,%d):\n", start, end)
	for i, r := range(t) {
		if i >= start && i < end {
			fmt.Printf("  %d: %v %v\n", i, r.Cmd, r.BlkNo)
		}
	}
}

func findSync(t trace_t, index int) int {
	for i := index; i < len(t); i++ {
		if t[i].Cmd == "sync" {
			return i
		}
	}
	return -1
}

func copyRecord(r record_t) record_t {
	c := record_t{}
	c.BlkNo = r.BlkNo
	c.Cmd = r.Cmd
	c.BlkData = make([]byte, len(r.BlkData))
	copy(c.BlkData, r.BlkData)
	return c
}

func copyTrace(t trace_t, start int, end int) trace_t {
	sub := make([]record_t, end-start)
	for i, _ := range(sub) {
		sub[i] = copyRecord(t[start+i])
	}
	return sub
}
	
func permTrace(t trace_t, sub trace_t, index int, o order_t) {
	// fmt.Printf("o = %v\n", o)
	for i, j := range(o) {
		t[index+i] = sub[j]
		
	}
	// printTrace(t, index, index+len(o))
}

func genDisk(trace trace_t, dst string) {
	copyFileContents(diskimg, dst)
	f, uerr := os.OpenFile(dst, os.O_RDWR, 0755)
	if uerr != nil {
		panic(uerr)
	}
	for _, r := range(trace) {
		if r.Cmd == "write" {
			fmt.Printf("update block %v\n", r.BlkNo)
			f.Seek(int64(r.BlkNo * common.BSIZE), 0)
			buf := make([]byte, common.BSIZE)
			for i, _ := range(buf) {
				buf[i] = byte(r.BlkData[i])
			}
			n, err := f.Write(buf)
			if n != common.BSIZE || err != nil {
				panic(err)
			}
		}
	}
	f.Sync()
	f.Close()
}

func checkTrace(dst string, trace trace_t, t *testing.T) {
	fmt.Printf("reboot and check %v ...\n", dst)
	ahci := mkDisk(dst, false)
	tfs := &testfs_t{}
	_, tfs.fs = StartFS(blockmem, ahci, c)
	tfs.doCheck(t)
	tfs.fs.StopFS()
	ahci.close()
}

type msg_t struct {
	trace trace_t
	t *testing.T
	dst string
}

var checkChan chan msg_t
var okChan chan bool

func Checker() {
	for true {
		m := <- checkChan
		fmt.Printf("Run checker\n")
		genDisk(m.trace, m.dst)
		checkTrace(m.dst, m.trace, m.t)
		okChan <- true
	}
}

func applyTrace(trace trace_t, t *testing.T) {
	fmt.Printf("Send trace to Checker:\n")
	printTrace(trace, 0, len(trace))
	dst := "tmp.img"
	// tracecp := make([]record_t, len(trace))
	// copy(tracecp, trace)
	// checkChan <- msg_t{trace, t, dst}
	//<- okChan
	genDisk(trace, dst)
	checkTrace(dst, trace, t)
}

func genTraces(trace trace_t, index int, t *testing.T) {
	if index >= len(trace) {
		applyTrace(trace, t)
		return
	}
	n := findSync(trace, index)
	so := make([]int, n-index)
	for i := 0; i < len(so); i++ {
		so[i] = i
	}
	orders := genOrders(so)
	subtrace := copyTrace(trace, index, n)
	// printTrace(subtrace, 0, len(subtrace))
	for _, o := range(orders) {
		permTrace(trace, subtrace, index, o)
		genTraces(trace, n+1, t)
	}
}
	
func TestTraces(t *testing.T) {
	fmt.Printf("testTraces ...\n")

	checkChan = make(chan msg_t)
	okChan = make(chan bool)
	go Checker()
	trace := readTrace("trace.json")
	printTrace(trace, 0, len(trace))
	genTraces(trace, 0, t)
}

