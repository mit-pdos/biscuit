package ufs

import "testing"
import "fmt"
import "os"
import "strconv"
import "sync"
import "io"

import "common"
import "fs"

const (
	SMALL = 512
)

const (
	nlogblks   = 32
	ninodeblks = 1
	ndatablks  = 10
)

func doTestSimple(tfs *Ufs_t, d string) string {
	fmt.Printf("doTestSimple %v\n", d)

	e := tfs.MkDir(d)
	if e != 0 {
		return fmt.Sprintf("mkDir %v failed", d)
	}

	ub := mkData(1, SMALL)
	e = tfs.MkFile(d+"f1", ub)
	if e != 0 {
		return fmt.Sprintf("mkFile %v failed", "f1")
	}

	ub = mkData(2, SMALL)
	e = tfs.MkFile(d+"f2", ub)
	if e != 0 {
		return fmt.Sprintf("mkFile %v failed", "f2")
	}

	e = tfs.MkDir(d + "d0")
	if e != 0 {
		return fmt.Sprintf("Mkdir %v failed", "d0")
	}

	e = tfs.MkDir(d + "d0/d1")
	if e != 0 {
		return fmt.Sprintf("Mkdir %v failed", "d1")
	}

	e = tfs.Rename(d+"d0/d1", d+"e0")
	if e != 0 {
		return fmt.Sprintf("Rename failed")
	}

	ub = mkData(3, SMALL)
	e = tfs.Append(d+"f1", ub)
	if e != 0 {
		return fmt.Sprintf("Append failed")
	}

	e = tfs.Unlink(d + "f2")
	if e != 0 {
		return fmt.Sprintf("Unlink failed")
	}
	return ""
}

func doCheckSimple(tfs *Ufs_t, d string, t *testing.T) {
	fmt.Printf("doCheckSimple %v\n", d)
	res, e := tfs.Ls(d)
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
	res, e = tfs.Ls(d + "/d0")
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

func doTestInodeReuse(tfs *Ufs_t, n int, t *testing.T) {
	for i := 0; i < n; i++ {
		e := tfs.MkFile(uniqfile(i), nil)
		if e != 0 {
			t.Fatalf("mkFile %v failed", i)
		}
	}

	for i := 0; i < n; i++ {
		e := tfs.Unlink(uniqfile(i))
		if e != 0 {
			t.Fatalf("Unlink %v failed", i)
		}
	}
}

//
// For testing block reuse
//

func doTestBlockReuse(tfs *Ufs_t, n int, t *testing.T) {
	for i := 0; i < n; i++ {
		ub := mkData(uint8(i), SMALL)
		e := tfs.MkFile(uniqfile(i), ub)
		if e != 0 {
			t.Fatalf("mkFile %v failed", i)
		}
	}

	for i := 0; i < n; i++ {
		e := tfs.Unlink(uniqfile(i))
		if e != 0 {
			t.Fatalf("Unlink %v failed", i)
		}
	}
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

//
// Simple test
//

func TestFSSimple(t *testing.T) {
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks, ndatablks)

	fmt.Printf("testFS %v ...\n", dst)

	tfs := BootFS(dst)
	s := doTestSimple(tfs, "d/")
	if s != "" {
		t.Fatalf("doTestSimple failed %s\n", s)
	}
	doCheckSimple(tfs, "d/", t)
	ShutdownFS(tfs)

	tfs = BootFS(dst)
	doCheckSimple(tfs, "d/", t)
	ShutdownFS(tfs)
	os.Remove(dst)
}

//
// Test that inode are reused after freeing
//

func TestFSInodeReuse(t *testing.T) {
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks, ndatablks)

	fmt.Printf("testFSInodeReuce %v ...\n", dst)

	tfs := BootFS(dst)
	n := ninodeblks * (common.BSIZE / fs.ISIZE)
	fmt.Printf("max inode %v\n", n)
	for i := 0; i < n; i++ {
		doTestInodeReuse(tfs, 10, t)
	}
	ShutdownFS(tfs)
	os.Remove(dst)
}

//
// Test that inode are reused after freeing
//

func TestFSBlockReuse(t *testing.T) {
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks, ndatablks)

	fmt.Printf("testFSBlockReuce %v ...\n", dst)

	tfs := BootFS(dst)
	n := ndatablks
	fmt.Printf("max #blks %v\n", n)
	for i := 0; i < n*2; i++ {
		doTestBlockReuse(tfs, 5, t)
	}
	ShutdownFS(tfs)
	os.Remove(dst)
}

//
// Simple concurrent test (for race detector)
//

func TestFSConcur(t *testing.T) {
	n := 2
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks, ndatablks)

	fmt.Printf("testFSConcur %v ...\n", dst)

	c := make(chan string)
	tfs := BootFS(dst)
	for i := 0; i < n; i++ {
		go func(id int) {
			d := uniqdir(id)
			s := doTestSimple(tfs, d)
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

	ShutdownFS(tfs)
	tfs = BootFS(dst)
	for i := 0; i < n; i++ {
		d := uniqdir(i)
		doCheckSimple(tfs, d, t)
	}
	ShutdownFS(tfs)
}

//
// Check traces for crash safety
//

const ndatablksordered = 10
const nblksordered = ndatablksordered/2 + 1
const natomicblks = 2

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
	//      return err
	//}
	return err
}

func doAtomicInit(tfs *Ufs_t) {
	ub := mkData(1, common.BSIZE*2)
	e := tfs.MkFile("f", ub)
	if e != 0 {
		panic("mkFile f failed")
	}
	tfs.Sync()
}

func doTestAtomic(tfs *Ufs_t, t *testing.T) {
	ub := mkData(2, common.BSIZE*2)
	e := tfs.MkFile("tmp", ub)
	if e != 0 {
		t.Fatalf("mkFile %v failed", "tmp")
	}
	tfs.Sync()
	e = tfs.Rename("tmp", "f")
	if e != 0 {
		t.Fatalf("Rename failed")
	}
	tfs.Sync()
}

func doCheckAtomic(tfs *Ufs_t) (string, bool) {
	res, e := tfs.Ls("/")
	if e != 0 {
		return "doLs failed", false
	}
	_, ok := res["f"]
	if !ok {
		return "f not present", false
	}
	if ok {
		d, e := tfs.Read("f")
		if e != 0 || len(d) != common.BSIZE*2 {
			return "Read f failed", false
		}
		v := d[0]
		for i := range d {
			if uint8(d[i]) != v {
				return fmt.Sprintf("Mixed data in f %v %v", v, d[i]), false
			}
		}
	}
	return "", true
}

// check that f2 doesn't contain f2's data XXX it may be possible to fail this
// with current FS impl; probably must be overwrites (which we don't have in ufs
// interface)
func doTestOrdered(tfs *Ufs_t, t *testing.T) {
	ub := mkData(1, common.BSIZE*natomicblks)
	e := tfs.MkFile("f1", ub)
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f1")
	}
	e = tfs.Unlink("f1")
	if e != 0 {
		t.Fatalf("Unlink failed")
	}
	ub = mkData(2, common.BSIZE*natomicblks)
	e = tfs.MkFile("f2", ub)
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f2")
	}
}

func doCheckOrdered(tfs *Ufs_t) (string, bool) {
	res, e := tfs.Ls("/")
	if e != 0 {
		return "doLs failed", false
	}
	_, ok1 := res["f1"]
	st2, ok2 := res["f2"]
	if ok1 && ok2 {
		return "f1 and f2 present", false
	}
	if ok2 {
		if !(st2.Size() == 0 || st2.Size() == common.BSIZE*natomicblks) {
			return fmt.Sprintf("Wrong size for f2 %v", st2.Size()), false
		}
		if st2.Size() > 0 {
			d, e := tfs.Read("f2")
			if e != 0 || len(d) != int(st2.Size()) {
				return "Read f2 failed", false
			}
			if uint8(d[0]) != 2 {
				return fmt.Sprintf("Wrong data in f2 %v", d[0]), false
			}
		}
	}
	return "", true
}

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

func genDisk(trace trace_t, crash int, dst string) {
	// apply trace
	f, err := os.OpenFile(dst, os.O_RDWR, 0755)
	if err != nil {
		panic(err)
	}
	trace.printTrace(0, crash)
	for i := 0; i < crash; i++ {
		r := trace[i]
		if r.Cmd == "write" {
			fmt.Printf("update block %v\n", r.BlkNo)
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
	err = f.Close()
	if err != nil {
		panic(err)
	}
}

func applyTrace(trace trace_t, start int, end int, cnt int, t *testing.T, disk string, apply bool) int {
	for i := start; i <= end; i++ {
		dst := "tmp" + strconv.Itoa(cnt) + ".img"
		cnt++
		// fmt.Printf("gen disk for running till entry %d\n", i)
		if apply {
			copyDisk(disk, dst)
			genDisk(trace, i, dst)
			wg.Add(1)
			func(d string, trace trace_t) {
				defer wg.Done()
				tfs := BootFS(d)
				s, ok := doCheckAtomic(tfs)
				ShutdownFS(tfs)
				os.Remove(d)
				if !ok {
					fmt.Printf("failed on disk %s\n", dst)
					trace.printTrace(0, len(trace))
					panic(s)
				}
			}(dst, trace.copyTrace(0, i))
		}
	}
	return cnt
}

var wg sync.WaitGroup

func genTraces(trace trace_t, t *testing.T, disk string, apply bool) int {
	cnt := 0
	index := 0
	for index < len(trace) {
		n := trace.findSync(index)
		// fmt.Printf("traces starting from %d till %d\n", index, n)
		so := make([]int, n-index)
		for i := 0; i < len(so); i++ {
			so[i] = i
		}
		orders := genOrders(so)
		subtrace := trace.copyTrace(index, n)
		for _, o := range orders {
			trace.permTrace(subtrace, index, o)
			cnt = applyTrace(trace, index, n, cnt, t, disk, apply)
		}
		index = n + 1
	}
	wg.Wait()
	return cnt
}

func produceTrace(disk string, t *testing.T) {
	fmt.Printf("produceTrace %v ...\n", disk)

	// Creat a disk with an inial file system state
	MkDisk(disk, nil, nlogblks, ninodeblks, ndatablks)
	tfs := BootFS(disk)
	doAtomicInit(tfs)
	ShutdownFS(tfs)

	// apply log
	tfs = BootFS(disk)
	ShutdownFS(tfs)

	// now copy disk
	copyDisk(disk, "tmp.img")

	// Now start tracing
	tfs = BootFS("tmp.img")
	tfs.ahci.StartTrace()
	doTestAtomic(tfs, t)
	tfs.fs.StopFS()

	os.Remove("tmp.img")
}

func TestTraces(t *testing.T) {
	fmt.Printf("testTraces ...\n")
	disk := "disk.img"
	produceTrace(disk, t)
	trace := readTrace("trace.json")
	cnt := genTraces(trace, t, disk, true)
	fmt.Printf("#traces = %v\n", cnt)
}
