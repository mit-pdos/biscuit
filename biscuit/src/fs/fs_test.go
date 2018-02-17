package fs

import "testing"
import "fmt"
import "os"
import "io"
import "strconv"
import "encoding/json"
import "log"

import "common"

var diskimg = "../../go.img"

//
//  trace file of writes and syncs
//

type tracef_t struct {
	file *os.File
	enc  *json.Encoder
}

type record_t struct {
	Cmd     string
	BlkNo   int
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
	f.Close()
	return res
}

func (trace trace_t) printTrace(start int, end int) {
	fmt.Printf("trace (%d,%d):\n", start, end)
	for i, r := range trace {
		if i >= start && i < end {
			fmt.Printf("  %d: %v %v\n", i, r.Cmd, r.BlkNo)
		}
	}
}

func (trace trace_t) findSync(index int) int {
	for i := index; i < len(trace); i++ {
		if trace[i].Cmd == "sync" {
			return i
		}
	}
	return -1
}

func (r *record_t) copyRecord() record_t {
	c := record_t{}
	c.BlkNo = r.BlkNo
	c.Cmd = r.Cmd
	c.BlkData = make([]byte, len(r.BlkData))
	copy(c.BlkData, r.BlkData)
	return c
}

func (trace trace_t) copyTrace(start int, end int) trace_t {
	sub := make([]record_t, end-start)
	for i, _ := range sub {
		sub[i] = trace[start+i].copyRecord()
	}
	return sub
}

func (trace trace_t) permTrace(sub trace_t, index int, o order_t) {
	for i, j := range o {
		trace[index+i] = sub[j]

	}
}

func (t *tracef_t) write(n int, v *common.Bytepg_t) {
	r := record_t{}
	r.BlkNo = n
	r.Cmd = "write"
	r.BlkData = make([]byte, common.BSIZE)
	for i, _ := range v {
		r.BlkData[i] = byte(v[i])
	}
	if err := t.enc.Encode(&r); err != nil {
		panic(err)
	}
}

func (t *tracef_t) sync() {
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
		for i, _ := range b {
			req.Blks[0].Data[i] = uint8(b[i])
		}
	case common.BDEV_WRITE:
		for _, b := range req.Blks {
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
	ahci *ahci_disk_t
	fs   *Fs_t
}

func mkData(v uint8) *common.Fakeubuf_t {
	hdata := make([]uint8, 512)
	for i := range hdata {
		hdata[i] = v
	}
	ub := &common.Fakeubuf_t{}
	ub.Fake_init(hdata)
	return ub
}

func (tfs *testfs_t) mkFile(p string, ub *common.Fakeubuf_t) common.Err_t {
	fd, err := tfs.fs.Fs_open(p, common.O_CREAT, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("tfs.fs.Fs_open %v failed %v\n", p, err)
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

func (tfs *testfs_t) doAppend(p string, ub *common.Fakeubuf_t) common.Err_t {
	fd, err := tfs.fs.Fs_open(p, common.O_RDWR, 0, common.Inum_t(0), 0, 0)
	if err != 0 {
		fmt.Printf("tfs.fs.Fs_open %v failed %v\n", p, err)
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

//
// A simple test
//

func (tfs *testfs_t) doTestSimple(d string) string {
	fmt.Printf("doTestSimple %v\n", d)

	e := tfs.mkDir(d)
	if e != 0 {
		return fmt.Sprintf("mkDir %v failed", d)
	}

	ub := mkData(1)
	e = tfs.mkFile(d+"f1", ub)
	if e != 0 {
		return fmt.Sprintf("mkFile %v failed", "f1")
	}

	ub = mkData(2)
	e = tfs.mkFile(d+"f2", ub)
	if e != 0 {
		return fmt.Sprintf("mkFile %v failed", "f2")
	}

	e = tfs.mkDir(d + "d0")
	if e != 0 {
		return fmt.Sprintf("Mkdir %v failed", "d0")
	}

	e = tfs.mkDir(d + "d0/d1")
	if e != 0 {
		return fmt.Sprintf("Mkdir %v failed", "d1")
	}

	e = tfs.doRename(d+"d0/d1", d+"e0")
	if e != 0 {
		return fmt.Sprintf("Rename failed")
	}

	ub = mkData(3)
	e = tfs.doAppend(d+"f1", ub)
	if e != 0 {
		return fmt.Sprintf("Append failed")
	}

	e = tfs.doUnlink(d + "f2")
	if e != 0 {
		return fmt.Sprintf("Unlink failed")
	}
	return ""
}

func (tfs *testfs_t) doCheckSimple(d string, t *testing.T) {
	fmt.Printf("doCheckSimple %v\n", d)
	res, e := tfs.doLs(d)
	if e != 0 {
		t.Fatalf("doLs failed")
	}
	st, ok := res["f1"]
	if !ok {
		t.Fatalf("f1 not present")
	}
	if st.Size() != 1024 {
		t.Fatalf("f1 wrong size")
	}

	st, ok = res["f2"]
	if ok {
		t.Fatalf("f2 present")
	}
	st, ok = res["d0"]
	if !ok {
		t.Fatalf("d0 not present")
	}
	st, ok = res["e0"]
	if !ok {
		t.Fatalf("e0 not present")
	}
	res, e = tfs.doLs(d + "/d0")
	if e != 0 {
		t.Fatalf("doLs d0 failed")
	}
	st, ok = res["e0"]
	if ok {
		t.Fatalf("e0 present in d0")
	}
}

//
// For testing inode reuse
//

func (tfs *testfs_t) doTestInodeReuse(n int, t *testing.T) {
	for i := 0; i < n; i++ {
		e := tfs.mkFile(uniqfile(i), nil)
		if e != 0 {
			t.Fatalf("mkFile %v failed", i)
		}
	}

	for i := 0; i < n; i++ {
		e := tfs.doUnlink(uniqfile(i))
		if e != 0 {
			t.Fatalf("Unlink %v failed", i)
		}
	}
}

//
// For testing block reuse and crash safety
//

func (tfs *testfs_t) doTestReuse(t *testing.T) {
	ub := mkData(1)
	e := tfs.mkFile("f1", ub)
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f1")
	}
	e = tfs.doUnlink("f1")
	if e != 0 {
		t.Fatalf("Unlink failed")
	}
	ub = mkData(2)
	e = tfs.mkFile("f2", ub)
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f2")
	}
}

func (tfs *testfs_t) doCheckReuse() (string, bool) {
	res, e := tfs.doLs("/")
	if e != 0 {
		return "doLs failed", false
	}
	st, ok := res["f1"]
	if ok {
		return "f1 present", false
	}
	st, ok = res["f2"]
	if !ok {
		return "f2 not present", false
	}
	if st.Size() != 512 {
		return "f2 wrong size", false
	}
	d, e := tfs.doRead("f2")
	if e != 0 || len(d) != int(st.Size()) {
		return "Read f2 failed", false
	}
	if uint8(d[0]) != 2 {
		return "Wrong data in f2", false
	}
	return "", true
}

//
// Util
//

func uniqdir(id int) string {
	return "d" + strconv.Itoa(id) + "/"
}

func uniqfile(id int) string {
	return "f" + strconv.Itoa(id)
}

func copyDisk(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	//err = out.Sync()
	//if err != nil {
	//	return err
	//}
	return err
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

func bootFS(dst string) *testfs_t {
	log.Printf("reboot and check %v ...\n", dst)
	tfs := &testfs_t{}
	tfs.ahci = mkDisk(dst, false)
	_, tfs.fs = StartFS(blockmem, tfs.ahci, c)
	return tfs
}

func shutdownFS(tfs *testfs_t) {
	tfs.fs.StopFS()
	tfs.ahci.close()
}

//
// Simple test
//

func TestFSSimple(t *testing.T) {
	dst := "tmp.img"
	err := copyDisk(diskimg, dst)
	if err != nil {
		panic(err)
	}

	fmt.Printf("testFS %v ...\n", dst)

	tfs := bootFS(dst)
	s := tfs.doTestSimple("d/")
	if s != "" {
		t.Fatalf("doTestSimple failed %s\n", s)
	}
	tfs.doCheckSimple("d/", t)
	shutdownFS(tfs)

	tfs = bootFS(dst)
	tfs.doCheckSimple("d/", t)
	shutdownFS(tfs)
}

//
// Test that inode are reused after freeing
//

func TestFSInodeReuse(t *testing.T) {
	dst := "tmp.img"
	err := copyDisk(diskimg, dst)
	if err != nil {
		panic(err)
	}

	fmt.Printf("testFSInodeReuce %v ...\n", dst)

	tfs := bootFS(dst)
	m := tfs.fs.ialloc.inodelen * common.BSIZE / ISIZE
	fmt.Printf("max inode %v\n", m)
	for i := 0; i < m; i += 100 {
		tfs.doTestInodeReuse(100, t)
	}
	shutdownFS(tfs)
}

//
// Simple concurrent test
//

func TestFSConcur(t *testing.T) {
	n := 2
	dst := "tmp.img"
	err := copyDisk(diskimg, dst)
	if err != nil {
		panic(err)
	}

	fmt.Printf("testFSConcur %v ...\n", dst)

	c := make(chan string)
	tfs := bootFS(dst)
	for i := 0; i < n; i++ {
		go func(id int) {
			d := uniqdir(id)
			s := tfs.doTestSimple(d)
			c <- s
		}(i)
	}
	for i := 0; i < n; i++ {
		s := <-c
		d := uniqdir(i)
		if s != "" {
			t.Fatalf("doTestSimple %v failed %s\n", d, s)
		}
	}

	shutdownFS(tfs)
	tfs = bootFS(dst)
	for i := 0; i < n; i++ {
		d := uniqdir(i)
		tfs.doCheckSimple(d, t)
	}
	shutdownFS(tfs)
}

//
// Check traces
//

var cnt int

func genOrder(blks []int, o order_t, r orders_t) orders_t {
	if len(blks) == 0 {
		// fmt.Printf("find order: %v\n", o)
		r = append(r, o)
		return r
	}
	// fmt.Printf("genOrder %v %v %v\n", blks, o, r)
	for i, v := range blks {
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

func genDisk(trace trace_t, dst string) {
	copyDisk(diskimg, dst)
	f, err := os.OpenFile(dst, os.O_RDWR, 0755)
	if err != nil {
		panic(err)
	}
	for _, r := range trace {
		if r.Cmd == "write" {
			// log.Printf("update block %v\n", r.BlkNo)
			f.Seek(int64(r.BlkNo*common.BSIZE), 0)
			buf := make([]byte, common.BSIZE)
			for i, _ := range buf {
				buf[i] = byte(r.BlkData[i])
			}
			n, err := f.Write(buf)
			if n != common.BSIZE || err != nil {
				panic(err)
			}
		}
	}
	//err = f.Sync()
	//if err != nil {
	//	panic(err)
	//}
	err = f.Close()
	if err != nil {
		panic(err)
	}
}

func applyTrace(trace trace_t, t *testing.T) {
	trace.printTrace(0, len(trace))
	log.Printf("applyTrace")
	dst := "tmp" + strconv.Itoa(cnt) + ".img"
	cnt++
	genDisk(trace, dst)
	go func() {
		tfs := bootFS(dst)
		s, ok := tfs.doCheckReuse()
		shutdownFS(tfs)
		os.Remove(dst)
		if !ok {
			panic(s)
		}
	}()
}

// Recursively generate all possible traces, for any order of writes between two
// syncs.
func genTraces(trace trace_t, index int, t *testing.T, apply bool) int {
	if index >= len(trace) {
		if apply {
			applyTrace(trace, t)
		}
		return 1
	}
	cnt := 0
	n := trace.findSync(index)
	so := make([]int, n-index)
	for i := 0; i < len(so); i++ {
		so[i] = i
	}
	orders := genOrders(so)
	subtrace := trace.copyTrace(index, n)
	for _, o := range orders {
		trace.permTrace(subtrace, index, o)
		cnt += genTraces(trace, n+1, t, apply)
	}
	return cnt
}

func produceTrace(t *testing.T) {
	dst := "tmp.img"
	err := copyDisk(diskimg, dst)
	if err != nil {
		panic(err)
	}
	ahci := mkDisk(dst, true)

	fmt.Printf("produceTrace %v ...\n", dst)
	tfs := &testfs_t{}
	_, tfs.fs = StartFS(blockmem, ahci, c)
	tfs.doTestReuse(t)
	tfs.fs.StopFS()
	ahci.close()
}

func TestTraces(t *testing.T) {
	fmt.Printf("testTraces ...\n")
	produceTrace(t)
	trace := readTrace("trace.json")
	// trace.printTrace(0, len(trace))
	cnt := genTraces(trace, 0, t, true)
	fmt.Printf("#traces = %v\n", cnt)
}
