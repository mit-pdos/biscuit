package hashtable

import "sync/atomic"
import "fmt"
import "hash/fnv"

import "sync"
import "unsafe"

import "ustr"

// A hashtable with a lock-free Get()

type hashtable_i interface {
	Get(key interface{}) (interface{}, bool)
	Set(key interface{}, val interface{}) (interface{}, bool)
	Del(key interface{})
}

type elem_t struct {
	key     interface{}
	value   interface{}
	keyHash uint32
	next    *elem_t
}

type bucket_t struct {
	sync.RWMutex
	first *elem_t
	//_	[64-2*8]uint8
}

func (b *bucket_t) len() int {
	b.RLock()
	defer b.RUnlock()

	l := 0
	for e := b.first; e != nil; e = e.next {
		l++
	}
	return l
}

func (b *bucket_t) elems() []Pair_t {
	b.RLock()
	defer b.RUnlock()

	p := make([]Pair_t, 0)
	for e := b.first; e != nil; e = e.next {
		p = append(p, Pair_t{Key: e.key, Value: e.value})
	}
	return p
}

func (b *bucket_t) iter(f func(interface{}, interface{}) bool) bool {
	for e := b.first; e != nil; e = loadptr(&e.next) {
		if f(e.key, e.value) {
			return true
		}
	}
	return false
}

type Hashtable_t struct {
	table    []*bucket_t
	capacity int
	maxchain int
}

func MkHash(size int) *Hashtable_t {
	ht := &Hashtable_t{}
	ht.capacity = size
	ht.table = make([]*bucket_t, size)
	ht.maxchain = 1
	for i, _ := range ht.table {
		ht.table[i] = &bucket_t{}
	}
	return ht
}

func (ht *Hashtable_t) String() string {
	s := ""
	for i, b := range ht.table {
		if b.first != nil {
			s += fmt.Sprintf("b %d:\n", i)
			for e := b.first; e != nil; e = loadptr(&e.next) {
				s += fmt.Sprintf("(%v, %v), ", e.keyHash, e.key)
			}
			s += fmt.Sprintf("\n")
		}
	}
	return s
}

func (ht *Hashtable_t) Size() int {
	n := 0
	for _, b := range ht.table {
		n += b.len()
	}
	return n
}

type Pair_t struct {
	Key   interface{}
	Value interface{}
}

func (ht *Hashtable_t) Elems() []Pair_t {
	p := make([]Pair_t, 0)
	for _, b := range ht.table {
		n := b.elems()
		if n != nil {
			p = append(p, n...)
		}
	}
	return p
}

func (ht *Hashtable_t) Get(key interface{}) (interface{}, bool) {
	kh := khash(key)
	b := ht.table[ht.hash(kh)]
	n := 0
	for e := loadptr(&b.first); e != nil; e = loadptr(&e.next) {
		if e.keyHash == kh && equal(e.key, key) {
			return e.value, true
		}
		n += 1
		if n > ht.maxchain {
			ht.maxchain = n
			//if n >= 3 {
			//	fmt.Printf("maxchain: %d\n", ht.maxchain)
			//	fmt.Printf("key %s collides with %s\n", key, e.key)
			//}
		}
	}
	return nil, false
}

// For performance comparisons
func (ht *Hashtable_t) GetRLock(key interface{}) (interface{}, bool) {
	kh := khash(key)
	b := ht.table[ht.hash(kh)]
	b.RLock()
	defer b.RUnlock()

	n := 0
	for e := b.first; e != nil; e = e.next {
		if e.keyHash == kh && equal(e.key, key) {
			return e.value, true
		}
		n += 1
		if n > ht.maxchain {
			ht.maxchain = n
			//if n >= 3 {
			//	fmt.Printf("maxchain: %d\n", ht.maxchain)
			//	fmt.Printf("key %s collides with %s\n", key, e.key)
			//}
		}
	}
	return nil, false
}

// Set returns false if key already exists
func (ht *Hashtable_t) Set(key interface{}, value interface{}) (interface{}, bool) {
	kh := khash(key)
	b := ht.table[ht.hash(kh)]
	b.Lock()
	defer b.Unlock()

	add := func(last *elem_t, b *bucket_t) {
		if last == nil {
			n := &elem_t{key: key, value: value, keyHash: kh, next: b.first}
			storeptr(&b.first, n)
		} else {
			n := &elem_t{key: key, value: value, keyHash: kh, next: last.next}
			storeptr(&last.next, n)
		}
	}

	var last *elem_t
	for e := b.first; e != nil; e = e.next {
		if e.keyHash == kh && equal(e.key, key) {
			return e.value, false
		}
		if kh < e.keyHash {
			add(last, b)
			return value, true
		}
		last = e
	}
	add(last, b)
	return value, true
}

// returns true if the key was removed
func (ht *Hashtable_t) Del(key interface{}) {
	kh := khash(key)
	b := ht.table[ht.hash(kh)]
	b.Lock()
	defer b.Unlock()

	rem := func(last *elem_t, b *bucket_t, n *elem_t) {
		if last == nil {
			// b.first = n.next
			storeptr(&b.first, n.next)
		} else {
			// last.next = n.next
			storeptr(&last.next, n.next)
		}
	}

	var last *elem_t
	for e := b.first; e != nil; e = e.next {
		if e.keyHash == kh && equal(e.key, key) {
			rem(last, b, e)
			return
		}
		if kh < e.keyHash {
			panic("del of non-existing key")
		}
		last = e
	}
	panic("del of non-existing key")
}

// Returns true if at least one call to f returned true. stops iterating once f
// returns true.
func (ht *Hashtable_t) Iter(f func(interface{}, interface{}) bool) bool {
	for _, b := range ht.table {
		if b.iter(f) {
			return true
		}
	}
	return false
}

func (ht *Hashtable_t) hash(keyHash uint32) int {
	return int(keyHash % uint32(len(ht.table)))
}

// Without an explicit memory model, it is hard to know if this code is
// correct. LoadPointer/StorePointer don't issue a memory fence, but for
// traversing pointers in Get() and updating them in Set()/Del(), this might be
// ok on x86. The Go compiler also hopefully doesn't reorder loads
// wrt. LoadPointer.
func loadptr(e **elem_t) *elem_t {
	ptr := (*unsafe.Pointer)(unsafe.Pointer(e))
	p := atomic.LoadPointer(ptr)
	n := (*elem_t)(unsafe.Pointer(p))
	return n
}

func storeptr(p **elem_t, n *elem_t) {
	ptr := (*unsafe.Pointer)(unsafe.Pointer(p))
	v := (unsafe.Pointer)(n)
	atomic.StorePointer(ptr, v)
}

func hashUstr(s ustr.Ustr) uint32 {
	h := fnv.New32a()
	h.Write(s)
	return h.Sum32()
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func khash(key interface{}) uint32 {
	h := hash(key)
	return uint32(2654435761) * h
}

func hash(key interface{}) uint32 {
	switch x := key.(type) {
	case ustr.Ustr:
		return hashUstr(x)
	case int:
		return uint32(x)
	case int32:
		return uint32(x)
	case string:
		return hashString(x)
	}
	panic(fmt.Errorf("unsupported key type %T", key))
}

func equal(key1 interface{}, key2 interface{}) bool {
	switch x := key1.(type) {
	case ustr.Ustr:
		us1 := key1.(ustr.Ustr)
		us2 := key2.(ustr.Ustr)
		return us1.Eq(us2)
	case int32:
		n1 := int32(x)
		n2 := key2.(int32)
		return n1 == n2
	case int:
		n1 := int(x)
		n2 := key2.(int)
		return n1 == n2
	case string:
		s1 := key1.(string)
		s2 := key2.(string)
		return s1 == s2
	}
	panic(fmt.Errorf("unsupported key type %T", key1))
}
