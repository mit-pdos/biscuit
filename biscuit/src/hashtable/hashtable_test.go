package hashtable

import "fmt"
import "math/rand"
import "strconv"
import "sync"
import "sync/atomic"
import "testing"
import "time"

func fill(t *testing.T, ht hashtable_i, n int) {
	for i := 0; i < n; i++ {
		k := strconv.Itoa(i)
		ht.Set(k, i)
		v, ok := ht.Get(k)
		if !ok {
			t.Fatalf("%v key", k)
		}
		if v != i {
			t.Fatalf("%v val", k)
		}
	}
	// fmt.Printf("table: %v\n", ht)
}

const SZ = 10

func TestSimple(t *testing.T) {
	ht := MkHash(SZ)

	fill(t, ht, 3*SZ)
	for i := 1; i < 3*SZ; i++ {
		k0 := strconv.Itoa(0)
		k := strconv.Itoa(i)
		ht.Del(k)
		v, ok := ht.Get(k0)
		if !ok {
			t.Fatalf("%v key", k0)
		}
		if v != 0 {
			t.Fatalf("%v val", k0)
		}
		v, ok = ht.Get(k)
		if ok {
			t.Fatalf("%v key", k0)
		}
	}

	fmt.Printf("Pass TestSimple\n")
}

const NPROC = 4
const NSEC = 1

func doop(t *testing.T, ht hashtable_i, k string, v int) {
	_, b := ht.Set(k, v)
	if !b {
		t.Fatalf("%v key already exists", k)
	}
	r, ok := ht.Get(k)
	if !ok {
		t.Fatalf("%v key", k)
	}
	if v != r {
		t.Fatalf("%v val", v)
	}
	ht.Del(k)
	r, ok = ht.Get(k)
	if ok {
		t.Fatalf("%v key", k)
	}
}

func writer(t *testing.T, ht hashtable_i, id int, done *int32) int {
	n := 0
	for atomic.LoadInt32(done) == 0 {
		v := rand.Intn(SZ)
		k := strconv.Itoa(v)
		k = fmt.Sprintf("%d.%s", id, k)
		doop(t, ht, k, v)
		n++
	}
	return n
}

func reader(t *testing.T, ht hashtable_i, done *int32) int {
	n := 0
	for atomic.LoadInt32(done) == 0 {
		v := rand.Intn(SZ)
		k := strconv.Itoa(v)
		r, ok := ht.Get(k)
		if !ok {
			t.Fatalf("%v key", k)
		}
		if v != r {
			t.Fatalf("%v val", v)
		}
		n++
	}
	return n
}

func TestManyWriter(t *testing.T) {
	ht := MkHash(SZ)

	rand.Seed(SZ)

	fill(t, ht, SZ)

	var wg sync.WaitGroup

	total := 0
	done := int32(0)
	for p := 0; p < NPROC; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			n := writer(t, ht, id, &done)
			total += n
		}(p)
	}

	time.Sleep(NSEC * time.Second)
	atomic.StoreInt32(&done, 1)
	wg.Wait()
	fmt.Printf("TestManyWriter: %d/s\n", total)
}

func doTestManyReader(ht hashtable_i, t *testing.T) {
	rand.Seed(SZ)

	fill(t, ht, SZ)

	var wg sync.WaitGroup
	done := int32(0)
	total := 0
	for p := 0; p < NPROC; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			n := reader(t, ht, &done)
			total += n
		}(p)
	}
	time.Sleep(NSEC * time.Second)
	atomic.StoreInt32(&done, 1)
	wg.Wait()
	fmt.Printf("TestManyReader:  %d/s\n", total)
}

func TestManyReader(t *testing.T) {
	ht := MkHash(SZ)
	doTestManyReader(ht, t)
}

func TestManyReaderStd(t *testing.T) {
	ht := MkHashString(SZ)
	doTestManyReader(ht, t)
}

func doTestManyReaderOneWriter(ht hashtable_i, t *testing.T) {
	rand.Seed(SZ)

	fill(t, ht, SZ)

	var wg sync.WaitGroup
	done := int32(0)
	nreads := 0
	nwrites := 0
	for p := 0; p < NPROC; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if id == 0 {
				nwrites += writer(t, ht, id, &done)
			} else {
				nreads += reader(t, ht, &done)
			}
		}(p)
	}
	time.Sleep(NSEC * time.Second)
	atomic.StoreInt32(&done, 1)
	wg.Wait()
	fmt.Printf("TestManyReaderOneWriter: reads %d/s writes %d/s\n", nreads, nwrites)
}

func TestManyReaderOneWriter(t *testing.T) {
	ht := MkHash(SZ)
	doTestManyReaderOneWriter(ht, t)
}

func TestManyReaderOneWriterStd(t *testing.T) {
	ht := MkHashString(SZ)
	doTestManyReaderOneWriter(ht, t)
}

func TestManyReaderOneWriterSyncMap(t *testing.T) {
	ht := MkSyncMap(SZ)
	doTestManyReaderOneWriter(ht, t)
}

// For performance comparisons

type hashtablestring_t struct {
	sync.RWMutex
	table map[string]int
}

func MkHashString(size int) *hashtablestring_t {
	ht := &hashtablestring_t{}
	ht.table = make(map[string]int, size)
	return ht
}

func (ht *hashtablestring_t) Get(key interface{}) (interface{}, bool) {
	ht.RLock()
	defer ht.RUnlock()

	k := key.(string)
	v, ok := ht.table[k]
	return v, ok
}

func (ht *hashtablestring_t) Set(key interface{}, val interface{}) (interface{}, bool) {
	ht.Lock()
	defer ht.Unlock()

	k := key.(string)
	v := val.(int)
	ht.table[k] = v
	return v, true
}

func (ht *hashtablestring_t) Del(key interface{}) {
	ht.Lock()
	defer ht.Unlock()

	k := key.(string)
	delete(ht.table, k)
}

type SyncMap_t struct {
	m sync.Map
}

func MkSyncMap(size int) *SyncMap_t {
	ht := &SyncMap_t{}
	return ht
}

func (ht *SyncMap_t) Get(key interface{}) (interface{}, bool) {
	v, ok := ht.m.Load(key)
	return v, ok
}

func (ht *SyncMap_t) Set(key interface{}, val interface{}) (interface{}, bool) {
	ht.m.Store(key, val)
	return val, true
}

func (ht *SyncMap_t) Del(key interface{}) {
	ht.m.Delete(key)
}
