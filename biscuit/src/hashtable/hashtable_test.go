package common

import "fmt"
import "math/rand"
import "strconv"
import "sync"
import "testing"
import "time"

func fill(t *testing.T, ht *hashtable_t, n int) {
	for i := 0; i < n; i++ {
		k := strconv.Itoa(i)
		ht.Put(k, i)
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
const NOP = 1000000

func doop(t *testing.T, ht *hashtable_t, k string, v int) {

	ht.Put(k, v)
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

func writer(t *testing.T, ht *hashtable_t, id int) {
	for i := 0; i < NOP; i++ {
		v := rand.Intn(SZ)
		k := strconv.Itoa(v)
		k = fmt.Sprintf("%d.%s", id, k)
		doop(t, ht, k, v)
	}
}

func reader(t *testing.T, ht *hashtable_t) {
	v := rand.Intn(SZ)
	k := strconv.Itoa(v)
	r, ok := ht.Get(k)
	if !ok {
		t.Fatalf("%v key", k)
	}
	if v != r {
		t.Fatalf("%v val", v)
	}
}

func TestManyWriter(t *testing.T) {
	ht := MkHash(SZ)

	rand.Seed(SZ)

	fill(t, ht, SZ)

	var wg sync.WaitGroup

	start := time.Now()
	for p := 0; p < NPROC; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			writer(t, ht, id)
		}(p)
	}
	wg.Wait()
	stop := time.Now()
	fmt.Printf("TestMany took %v\n", stop.Sub(start))
}

func TestManyReader(t *testing.T) {
	ht := MkHash(SZ)

	rand.Seed(SZ)

	fill(t, ht, SZ)

	var wg sync.WaitGroup

	start := time.Now()
	for p := 0; p < NPROC; p++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			reader(t, ht)
		}(p)
	}
	wg.Wait()
	stop := time.Now()
	fmt.Printf("TestManyReader took %v\n", stop.Sub(start))
}

func TestManyReaderOneWriter(t *testing.T) {
	ht := MkHash(SZ)

	rand.Seed(SZ)

	fill(t, ht, SZ)

	var wg sync.WaitGroup

	start := time.Now()
	for p := 0; p < NPROC; p++ {
		go func(id int) {
			defer wg.Done()
			if id == 0 {
				writer(t, ht, id)
			} else {
				reader(t, ht)
			}
		}(p)
	}
	wg.Wait()
	stop := time.Now()
	fmt.Printf("TestManyReaderOneWriter took %v\n", stop.Sub(start))
}
