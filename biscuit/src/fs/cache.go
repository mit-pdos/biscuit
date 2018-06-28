package fs

//import "fmt"
import "runtime"
import "sort"
import "sync"
import "sync/atomic"

import "common"
import "hashtable"

// Fixed-size cache of objects. Main invariant: an object is in memory once so
// that threads see each other's updates.  The challenging case is that an
// object can be evicted only when no thread has a reference to the object.  To
// keep track of the references to an object, cache refcounts the references to
// an object.  The client of cache, must call Lookup/Done to ensure a correct
// refcount.
//
// It is a bummer that we refcnt, instead of relying on GC. n an alternate
// world, we would use finalizers on an object, and the GC would inform
// refcache_t that an object isn't in use anymore.  Refcache itself would use a
// weak reference to an object, so that the GC could collect the object, if it
// is low on memory.

type cstats_t struct {
	Nevict common.Counter_t
	Nhit   common.Counter_t
	Nadd   common.Counter_t
}

type Obj_t interface {
	Evict()
}

type Objref_t struct {
	Key    int
	Obj    Obj_t
	refcnt int64
	tstamp uint64
}

func MkObjref(obj Obj_t, key int) *Objref_t {
	e := &Objref_t{}
	e.Obj = obj
	e.Key = key
	e.refcnt = 1
	e.tstamp = runtime.Rdtsc()
	return e
}

func (ref *Objref_t) Refcnt() int64 {
	c := atomic.LoadInt64(&ref.refcnt)
	return c
}

func (ref *Objref_t) Up() {
	atomic.AddInt64(&ref.refcnt, 1)
}

func (ref *Objref_t) Down() int64 {
	v := atomic.AddInt64(&ref.refcnt, -1)
	if v < 0 {
		panic("Down")
	}
	return v
}

type cache_t struct {
	sync.Mutex
	cache *hashtable.Hashtable_t
	stats cstats_t
}

func mkCache(size int) *cache_t {
	c := &cache_t{}
	c.cache = hashtable.MkHash(size)
	return c
}

func (c *cache_t) Len() int {
	c.Lock()
	ret := 0 // len(c.cache)
	c.Unlock()
	return ret
}

func (c *cache_t) Lookup(key int, mkobj func(int) Obj_t) (*Objref_t, bool) {
	v, ok := c.cache.Get(key)
	if ok {
		e := v.(*Objref_t)
		// other threads may have a reference to this item and may
		// Ref{up,down}; the increment must therefore be atomic
		c.stats.Nhit.Inc()
		e.Up()
		e.tstamp = runtime.Rdtsc()
		return e, false
	}
	e := MkObjref(mkobj(key), key)
	e.Obj = mkobj(key)
	v, ok = c.cache.Set(key, e)
	if !ok { // someone else created it
		e := v.(*Objref_t)
		c.stats.Nhit.Inc()
		e.Up()
		return e, false
	} else {
		c.stats.Nadd.Inc()
		return e, true

	}
}

func (c *cache_t) Remove(key int) {
	if v, ok := c.cache.Get(key); ok {
		e := v.(*Objref_t)
		cnt := e.Refcnt()
		if cnt < 0 {
			panic("Remove: negative refcnt")
		}
		if cnt == 0 {
			c.delete(e)
		} else {
			panic("Remove: refcnt > 0")
		}
	} else {
		panic("Remove: non existing")
	}
}

func (c *cache_t) Stats() string {
	s := ""
	s += common.Stats2String(c.stats)
	return s
}

type ByStamp []hashtable.Pair_t

func (a ByStamp) Len() int      { return len(a) }
func (a ByStamp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByStamp) Less(i, j int) bool {
	e1 := a[i].Value.(*Objref_t)
	e2 := a[2].Value.(*Objref_t)
	return e1.tstamp < e2.tstamp
}

// evicts up-to half of the objects in the cache. returns the number of cache
// entries remaining.
func (c *cache_t) Evict_half() int {
	c.Lock()
	defer c.Unlock()

	upto := c.cache.Size()
	did := 0
	elems := c.cache.Elems()
	sort.Sort(ByStamp(elems))
	for _, p := range elems {
		e := p.Value.(*Objref_t)
		if e.Refcnt() != 0 {
			continue
		}
		// imemnode with refcount of 0 must have non-zero links and
		// thus can be freed.  (in fact, they already have been freed)
		c.delete(e)
		// imemnode eviction acquires no locks and block eviction
		// acquires only a leaf lock (physmem lock). furthermore,
		// neither eviction blocks on IO, thus it is safe to evict here
		// with locks held.
		e.Obj.Evict()
		did++
	}
	return upto - did
}

func (c *cache_t) delete(o *Objref_t) {
	c.cache.Del(o.Key)
	c.stats.Nevict.Inc()
}
