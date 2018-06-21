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
	LARGE = 8 * 4096
)

const (
	nlogblks   = 32
	ninodeblks = 1
	ndatablks  = 20
)

func TestCanonicalize(t *testing.T) {
	if "/" != common.Canonicalize("//") {
		t.Fatalf("//")
	}
	if "/" != common.Canonicalize("/////") {
		t.Fatalf("/////")
	}
	if "/" != common.Canonicalize("/./") {
		t.Fatalf("/./")
	}
	if "/a" != common.Canonicalize("/a/./") {
		t.Fatalf("/a")
	}
	if "/a/b/c" != common.Canonicalize("/a/b/c/") {
		t.Fatalf("/a/b/c")
	}
	if "/" != common.Canonicalize("/a/../") {
		t.Fatalf("/")
	}
	if "/a" != common.Canonicalize("/a/b/..") {
		t.Fatalf("/")
	}
	if "/" != common.Canonicalize("/..") {
		t.Fatalf("/")
	}
}

func doTestSimple(tfs *Ufs_t, d string) string {
	//fmt.Printf("doTestSimple %v\n", d)

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
	res, e := tfs.Ls(d)
	if e != 0 {
		t.Fatalf("doLs failed")
	}
	st, ok := res["f1"]
	if !ok {
		t.Fatalf("%s/f1 not present", d)
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

	fmt.Printf("Test FSSimple %v ...\n", dst)

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

func TestFSInodeReuse(t *testing.T) {
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks, ndatablks)

	fmt.Printf("Test FSInodeReuce %v ...\n", dst)

	tfs := BootFS(dst)
	n := ninodeblks * (common.BSIZE / fs.ISIZE)
	for i := 0; i < n; i++ {
		doTestInodeReuse(tfs, 10, t)
	}
	ShutdownFS(tfs)
	os.Remove(dst)
}

//
// Test that inode are reused after freeing
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

func TestFSBlockReuse(t *testing.T) {
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks, ndatablks)

	fmt.Printf("Test FSBlockReuce %v ...\n", dst)

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
// Orphan inodes.  Inodes (and its blocks) should be freed on recovery
//

func doTestOrphans(tfs *Ufs_t, t *testing.T, nfile int) {
	fds := make([]*common.Fd_t, nfile)
	for i := 0; i < nfile; i++ {
		fn := uniqfile(i)
		var err common.Err_t
		fds[i], err = tfs.fs.Fs_open(fn, common.O_CREAT, 0, tfs.fs.MkRootCwd(), 0, 0)
		if err != 0 {
			t.Fatalf("ufs.fs.Fs_open %v failed %v\n", fn, err)
		}
		ub := mkData(uint8(1), SMALL)
		n, err := fds[i].Fops.Write(nil, ub)
		if err != 0 || ub.Remain() != 0 {
			t.Fatalf("Write %v failed %v %d\n", fn, err, n)
		}
		err = tfs.fs.Fs_unlink(fn, tfs.fs.MkRootCwd(), false)
		if err != 0 {
			t.Fatalf("doUnlink %v failed %v\n", fn, err)
		}
	}
	if nfile > 1 {
		// ifree() one
		fds[0].Fops.Close()

	}
}

func doCheckOrphans(tfs *Ufs_t, t *testing.T, nfile int) {
	res, e := tfs.Ls("/")
	if e != 0 {
		t.Fatalf("doLs failed")
	}
	for i := 0; i < nfile; i++ {
		fn := uniqfile(i)
		_, ok := res[fn]
		if ok {
			t.Fatalf("%v present", fn)
		}
	}
}

func TestFSOrphanOne(t *testing.T) {
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks, ndatablks)

	fmt.Printf("Test FSOrphans %v ...\n", dst)

	tfs := BootFS(dst)
	ninode, nblock := tfs.fs.Fs_size()
	doTestOrphans(tfs, t, 1)
	ShutdownFS(tfs) // causes the unlink to be committed
	fmt.Printf("ninode %v nblock %v\n", ninode, nblock)
	tfs = BootFS(dst)
	ninode1, nblock1 := tfs.fs.Fs_size()
	if ninode1 != ninode || nblock1 != nblock {
		t.Fatalf("inode/blocks not freed: free before %d %d free after %d %d\n",
			ninode, nblock, ninode1, nblock1)
	}
	// doCheckOrphans(tfs, t, 1)
	ShutdownFS(tfs)

	fmt.Printf("one more check\n")

	tfs = BootFS(dst) // check that we don't free again
	doCheckOrphans(tfs, t, 1)
	ShutdownFS(tfs)

	os.Remove(dst)
}

const (
	OrphanFiles   = 1000
	ManyInodeBlks = 2000
	ManyDataBlks  = 4000
	ManyLogBlks   = 256
)

func TestFSOrphansMany(t *testing.T) {
	dst := "tmp.img"
	MkDisk(dst, nil, ManyLogBlks, ManyInodeBlks, ManyDataBlks)

	fmt.Printf("Test FSOrphans %v ...\n", dst)

	tfs := BootFS(dst)
	ninode, nblock := tfs.fs.Fs_size()
	doTestOrphans(tfs, t, OrphanFiles)
	ShutdownFS(tfs) // causes the unlink to be committed

	tfs = BootFS(dst)
	ninode1, nblock1 := tfs.fs.Fs_size()
	if ninode1 != ninode || nblock1 != nblock {
		t.Fatalf("inode/blocks not freed: before %d %d after %d %d\n",
			ninode, nblock, ninode1, nblock1)
	}
	doCheckOrphans(tfs, t, OrphanFiles)
	ShutdownFS(tfs)
	os.Remove(dst)
}

//
// Simple concurrent test (for race detector)
//

func concurrent(t *testing.T) {
	n := 8
	dst := "tmp.img"
	MkDisk(dst, nil, nlogblks, ninodeblks*2, ndatablks*10)

	fmt.Printf("Test FSConcur %v\n", dst)

	c := make(chan string)
	tfs := BootFS(dst)
	for i := 0; i < n; i++ {
		go func(id int) {
			d := uniqdir(id)
			s := doTestSimple(tfs, d)
			doCheckSimple(tfs, d, t)
			tfs.Sync()
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

func TestFSConcurNotSame(t *testing.T) {
	concurrent(t)
}

//
// Test concurrent commit of blocks with a new transaction updating same blocks
//

const (
	MoreLogBlks = nlogblks * 4
)

func TestFSConcurSame(t *testing.T) {
	n := 8
	dst := "tmp.img"
	MkDisk(dst, nil, MoreLogBlks, ninodeblks*2, ndatablks*10)
	c := make(chan string)

	tfs := BootFS(dst)
	ub := mkData(1, SMALL)
	e := tfs.MkFile("f1", ub)
	if e != 0 {
		t.Fatalf("mkFile %v failed", "f1")
	}

	for i := 0; i < n; i++ {
		go func(id int) {
			ub := mkData(uint8(id), LARGE)
			e := tfs.Update("f1", ub)
			tfs.Sync()
			s := ""
			if e != 0 {
				s = fmt.Sprintf("Update f1 failed")
			}
			c <- s
		}(i)
	}

	for i := 0; i < n; i++ {
		s := <-c
		if s != "" {
			t.Fatalf("Update failed %s\n", s)
		}
	}

	ShutdownFS(tfs)
}

func TestFSConcurUnlink(t *testing.T) {
	n := 8
	dst := "tmp.img"
	MkDisk(dst, nil, MoreLogBlks, ninodeblks*2, ndatablks*10)
	c := make(chan string)
	tfs := BootFS(dst)
	d := "d"
	e := tfs.MkDir(d)
	if e != 0 {
		t.Fatalf("mkDir %v failed", d)
	}
	for i := 0; i < n; i++ {
		go func(id int) {
			s := ""
			fn := uniqfile(id)
			for j := 0; j < 100 && s == ""; j++ {
				s = func() string {
					p := d + "/" + fn
					ub := mkData(uint8(id), SMALL)
					e = tfs.MkFile(p, ub)
					if e != 0 {
						s = fmt.Sprintf("MkFile failed")
						return s
					}
					tfs.Sync()
					e = tfs.Unlink(p)
					if e != 0 {
						s = fmt.Sprintf("Unlink failed")
						return s
					}
					tfs.Sync()
					return s
				}()
			}
			c <- s
		}(i)
	}

	for i := 0; i < n; i++ {
		s := <-c
		if s != "" {
			t.Fatalf("Func failed %s\n", s)
		}
	}

	ShutdownFS(tfs)
}

//
// Check ordered

const (
	ndatablksordered = 8 // small, to cause block reuse, but at least for 1 byte in free map
	norderedblks     = 7 // this causes reuse
)

func TestOrderedFile(t *testing.T) {
	dst := "tmp.img"

	MkDisk(dst, nil, MoreLogBlks, ninodeblks, ndatablksordered)
	tfs := BootFS(dst)

	ub := mkData(1, common.BSIZE*norderedblks)
	e := tfs.MkFile("f", ub)
	if e != 0 {
		panic("mkFile f failed")
	}
	tfs.Sync()

	ub = mkData(2, common.BSIZE*norderedblks)
	e = tfs.Update("f", ub)
	if e != 0 {
		t.Fatalf("Update %v failed", "g")
	}

	tfs.Sync()

	ShutdownFS(tfs)
	tfs = BootFS(dst) // causes an apply

	d, e := tfs.Read("f")
	for i := 0; i < 7*common.BSIZE; i++ {
		if uint8(d[i]) != 2 {
			t.Fatalf("Wrong data in f %v at %v", d[0], i)
		}
	}
}

func doOrderedDir(t *testing.T, force bool) {
	dst := "tmp.img"
	d := "d"

	MkDisk(dst, nil, MoreLogBlks, ninodeblks, ndatablksordered)
	tfs := BootFS(dst)

	e := tfs.MkDir(d)
	if e != 0 {
		t.Fatalf("mkDir %v failed", d)
	}

	tfs.Sync()

	e = tfs.UnlinkDir(d)
	if e != 0 {
		t.Fatalf("Unlink failed")
	}

	tfs.Sync()

	ub := mkData(3, common.BSIZE*7)
	e = tfs.MkFile("f", ub)
	if e != 0 {
		panic("mkFile f failed")
	}

	if force {
		tfs.SyncApply() // better not overwrite f's blocks
	} else {
		tfs.Sync()
	}

	ShutdownFS(tfs)
	tfs = BootFS(dst) // better not overwrite f's blocks

	data, e := tfs.Read("f")
	for i := 0; i < 7*common.BSIZE; i++ {
		if uint8(data[i]) != 3 {
			t.Fatalf("Wrong data in f %v at %v", data[i], i)
		}
	}
}

func TestOrderderDirRecover(t *testing.T) {
	doOrderedDir(t, false)
}

func TestOrderedDirApply(t *testing.T) {
	doOrderedDir(t, true)
}

// Test ordered to logged writes.  XXX Hard to get at without testing all possible traces
func TestOrderedFileDir(t *testing.T) {
	dst := "tmp.img"
	d := "d"

	MkDisk(dst, nil, MoreLogBlks, ninodeblks, ndatablksordered)
	tfs := BootFS(dst)

	ub := mkData(3, common.BSIZE*7)
	e := tfs.MkFile("f", ub)
	if e != 0 {
		t.Fatalf("Unlink failed")
	}

	// tfs.Sync()

	e = tfs.Unlink("f")
	if e != 0 {
		t.Fatalf("Unlink failed")
	}

	// tfs.Sync()

	e = tfs.MkDir(d)
	if e != 0 {
		t.Fatalf("mkDir %v failed", d)
	}

	// tfs.Sync()

	ShutdownFS(tfs)
	tfs = BootFS(dst) // better not overwrite dir's blocks

	res, e := tfs.Ls(d)
	if e != 0 {
		t.Fatalf("Ls failed\n")
	}
	_, ok := res["."]
	if !ok {
		t.Fatalf(". not present")
	}
}

//
// Check traces for crash safety
//

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
	return err
}

//
// Test: atomic file copy
//

func doAtomicInit(tfs *Ufs_t) {
	ub := mkData(1, common.BSIZE*natomicblks)
	e := tfs.MkFile("f", ub)
	if e != 0 {
		panic("mkFile f failed")
	}
	tfs.Sync()
}

func doTestAtomic(tfs *Ufs_t, t *testing.T) {
	ub := mkData(2, common.BSIZE*natomicblks)
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
		if e != 0 || len(d) != common.BSIZE*natomicblks {
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

//
// Trace generation and checking
//

func genExt(blks []int, o order_t, r orders_t) orders_t {
	if len(blks) == 0 {
		return r
	}
	//fmt.Printf("genExt %v %v %v\n", blks, o, r)
	for i, v := range blks {
		t := make([]int, len(blks))
		copy(t, blks)
		t = append(t[0:i], t[i+1:]...)
		o1 := append(o, v)
		o2 := make([]int, len(o1))
		copy(o2, o1)
		r = append(r, o2)
		r = genExt(t, o1, r)
	}
	return r
}

func genExtensions(blks []int) orders_t {
	r := make(orders_t, 0)
	o := make(order_t, 0)
	r = append(r, []int{})
	r = genExt(blks, o, r)
	fmt.Printf("#extensions: %d\n", len(r))
	return r
}

func genDisk(trace trace_t, dst string) {
	// apply trace
	f, err := os.OpenFile(dst, os.O_RDWR, 0755)
	if err != nil {
		panic(err)
	}
	for i := 0; i < len(trace); i++ {
		r := trace[i]
		if r.Cmd == "write" {
			// fmt.Printf("update block %v\n", r.BlkNo)
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

func applyTrace(trace trace_t, cnt int, t *testing.T, disk string, check func(*Ufs_t) (string, bool)) {
	dst := "tmp" + strconv.Itoa(cnt) + ".img"
	copyDisk(disk, dst)
	genDisk(trace, dst)
	wg.Add(1)
	go func(d string, trace trace_t) {
		defer wg.Done()
		tfs := BootFS(d)
		s, ok := check(tfs)
		ShutdownFS(tfs)
		os.Remove(d)
		if !ok {
			fmt.Printf("failed on disk %s\n", dst)
			trace.printTrace(0, len(trace))
			panic(s)
		}
	}(dst, trace)
}

var wg sync.WaitGroup

func genTraces(trace trace_t, t *testing.T, disk string, apply bool, check func(*Ufs_t) (string, bool)) int {
	cnt := 0
	index := 0
	for index < len(trace) {
		n := trace.findSync(index)
		fmt.Printf("Extensions starting from %d till %d\n", index, n)
		so := make([]int, n-index)
		for i := 0; i < len(so); i++ {
			so[i] = i
		}
		extensions := genExtensions(so)
		ngo := 0
		for _, e := range extensions {
			// fmt.Printf("Ext: %v\n", e)
			tc := trace.permTrace(index, e)
			// tc.printTrace(0, len(tc))
			if apply {
				applyTrace(tc, cnt, t, disk, check)
				ngo++
				if ngo%100 == 0 { // don't get more than 100 disks ahead
					wg.Wait()
				}
			}
			cnt++
		}
		index = n + 1
	}
	wg.Wait()
	return cnt
}

func produceTrace(disk string, t *testing.T, init func(*Ufs_t), run func(*Ufs_t, *testing.T)) {
	fmt.Printf("produceTrace %v ...\n", disk)

	tfs := BootFS(disk)
	init(tfs)
	ShutdownFS(tfs)

	// apply log
	tfs = BootFS(disk)
	ShutdownFS(tfs)

	// now copy disk
	copyDisk(disk, "tmp.img")

	// Now start tracing
	tfs = BootFS("tmp.img")
	tfs.ahci.StartTrace()

	run(tfs, t)
	tfs.fs.StopFS()

	os.Remove("tmp.img")
}

func TestTracesAtomic(t *testing.T) {
	fmt.Printf("Test TracesAtomic ...\n")
	disk := "disk.img"
	MkDisk(disk, nil, nlogblks, ninodeblks, ndatablks)
	produceTrace(disk, t, doAtomicInit, doTestAtomic)
	trace := readTrace("trace.json")
	trace.printTrace(0, len(trace))
	cnt := genTraces(trace, t, disk, true, doCheckAtomic)
	fmt.Printf("#traces = %v\n", cnt)
	os.Remove(disk)
}

//
// Test: big ifree (i.e., several ops, spanning several transactions)
//

const (
	ManyManyDataBlks = 1000000
	FileSizeBlks     = 200
)

var nblock uint

func doFreeInit(tfs *Ufs_t) {
	_, nblock = tfs.fs.Fs_size()

	ub := mkData(1, common.BSIZE*FileSizeBlks)
	e := tfs.MkFile("f", ub)
	if e != 0 {
		panic("mkFile f failed")
	}
	tfs.Sync()
}

func doTestFree(tfs *Ufs_t, t *testing.T) {
	res, e := tfs.Ls("/")
	if e != 0 {

		t.Fatalf("ls failed")
	}
	_, ok := res["f"]
	if !ok {

		t.Fatalf("f not present")
	}

	e = tfs.Unlink("f")
	if e != 0 {
		t.Fatalf("unlink failed")
	}
	tfs.Sync()

	_, nblock1 := tfs.fs.Fs_size()

	if nblock != nblock1 {
		t.Fatalf("nblock %d doesn't match nblock %d\n", nblock, nblock1)
	}
}

func doCheckFree(tfs *Ufs_t) (string, bool) {
	res, e := tfs.Ls("/")
	if e != 0 {
		return "doLs failed", false
	}
	_, ok := res["f"]
	if ok { // nothing to test, f still exists
		return "", true
	}
	_, nblock1 := tfs.fs.Fs_size()
	if nblock != nblock1 {
		return fmt.Sprintf("nblock %d doesn't match nblock %d\n", nblock, nblock1), false

	}
	return "", true
}

func blk2bytepg(d []byte) *common.Bytepg_t {
	b := &common.Bytepg_t{}
	for i := 0; i < len(d); i++ {
		b[i] = d[i]
	}
	return b
}

// mark many blocks as allocated so that creating a file will have to mark many
// bit map blocks, which then ifree will update in several ops, spanning
// several transactions.
func FillDisk(disk string) {
	f, err := os.OpenFile(disk, os.O_RDWR, 0755)
	if err != nil {
		panic(err)
	}
	_, err = f.Seek(common.BSIZE, 0)
	if err != nil {
		panic(err)
	}

	super := mkBlock()
	_, err = f.Read(super)
	if err != nil {
		panic(err)
	}
	blk := blk2bytepg(super)
	sb := fs.Superblock_t{blk}
	_, err = f.Seek(int64(common.BSIZE*sb.Freeblock()), 0)
	if err != nil {
		panic(err)
	}
	for i := 0; i < sb.Freeblocklen(); i++ {
		b := mkBlock()
		_, err = f.Read(b)
		if err != nil {
			panic(err)
		}
		for j := 1; j < common.BSIZE; j++ {
			b[j] = 0xFF // mark as allocated
		}
		_, err = f.Seek(int64(-common.BSIZE), 1)
		if err != nil {
			panic(err)
		}

		_, err = f.Write(b)
		if err != nil {
			panic(err)
		}
	}
	f.Sync()
	f.Close()
}

func genSyncTraces(trace trace_t, t *testing.T, disk string, apply bool, check func(*Ufs_t) (string, bool)) int {
	cnt := 0
	index := 0
	for index < len(trace) {
		n := trace.findSync(index)
		fmt.Printf("Trace starting from %d till %d\n", 0, n)
		tc := trace.copyTrace(0, n)
		// tc.printTrace(0, len(tc))
		if apply {
			applyTrace(tc, cnt, t, disk, check)
		}
		cnt++
		index = n + 1
	}
	wg.Wait()
	return cnt
}

func TestBigFree(t *testing.T) {
	fmt.Printf("Test BigFree ...\n")
	disk := "disk.img"
	MkDisk(disk, nil, nlogblks, ninodeblks, ManyManyDataBlks)
	FillDisk(disk)
	produceTrace(disk, t, doFreeInit, doTestFree)
	trace := readTrace("trace.json")
	trace.printTrace(0, len(trace))
	cnt := genSyncTraces(trace, t, disk, true, doCheckFree)
	fmt.Printf("#traces = %v\n", cnt)
	os.Remove(disk)
}
