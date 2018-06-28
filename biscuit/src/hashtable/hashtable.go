package common

import "sync/atomic"
import "fmt"
import "hash/fnv"
import "sync"
import "unsafe"

type elem_t struct {
	key     interface{}
	value   interface{}
	keyHash uint32
	next    *elem_t
}

type bucket_t struct {
	sync.Mutex
	first *elem_t
}

type hashtable_t struct {
	table []*bucket_t
}

func MkHash(size int) *hashtable_t {
	ht := &hashtable_t{}
	ht.table = make([]*bucket_t, size)
	for i, _ := range ht.table {
		ht.table[i] = &bucket_t{}
	}
	return ht
}

func (ht *hashtable_t) String() string {
	s := ""
	for i, b := range ht.table {
		if b.first != nil {
			s += fmt.Sprintf("b %d:\n", i)
			for e := b.first; e != nil; e = e.next {
				s += fmt.Sprintf("(%v, %v), ", e.keyHash, e.key)
			}
			s += fmt.Sprintf("\n")
		}
	}
	return s
}

func (ht *hashtable_t) Get(key interface{}) (interface{}, bool) {
	kh := khash(key)
	b := ht.table[ht.hash(kh)]

	for e := b.first; e != nil; e = loadptr(&e.next) {
		if e.keyHash == kh && e.key == key {
			return e.value, true
		}
	}
	return nil, false
}

func (ht *hashtable_t) Put(key interface{}, value interface{}) {
	kh := khash(key)
	b := ht.table[ht.hash(kh)]
	b.Lock()
	defer b.Unlock()

	add := func(last *elem_t, b *bucket_t) {
		if last == nil {
			n := &elem_t{key: key, value: value, keyHash: kh, next: b.first}
			storeptr(&b.first, n)
			// b.first = n
		} else {
			n := &elem_t{key: key, value: value, keyHash: kh, next: last.next}
			// last.next = n
			storeptr(&last.next, n)
		}
	}

	var last *elem_t
	for e := b.first; e != nil; e = e.next {
		if e.keyHash == kh && e.key == key {
			e.value = value
			return
		}
		if kh < e.keyHash {
			add(last, b)
			return
		}
		last = e
	}
	add(last, b)
}

func (ht *hashtable_t) Del(key interface{}) {
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
		if e.keyHash == kh && e.key == key {
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

func (ht *hashtable_t) hash(keyHash uint32) int {
	return int(keyHash % uint32(len(ht.table)))
}

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
	// *p = n
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
	case string:
		return hashString(x)
	case int:
		return uint32(x)
	case int32:
		return uint32(x)
	}
	panic(fmt.Errorf("unsupported key type %T", key))
}
