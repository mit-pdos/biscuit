package fs

//import "fmt"
import "runtime"
import "sort"
import "sync"
import "sync/atomic"

import "hashtable"
import "stats"

// Cache of objects. Main invariant: an object is in memory once so that threads
// see each other's updates.  The challenging case is that an object can be
// evicted only when no thread has a reference to the object.  To keep track of
// the references to an object, cache refcounts the references to an object.
// The client of cache, must call Lookup/Done to ensure a correct refcount.
//
// It is a bummer that we refcnt, instead of relying on GC. In an alternate
// world, we would use finalizers on an object, and the GC would inform
// refcache_t that an object isn't in use anymore.  Refcache itself would use a
// weak reference to an object, so that the GC could collect the object, if it
// is low on memory.

const REMOVE = uint32(0xF0000000)

type cstats_t struct {
	Nevict stats.Counter_t
	Nhit   stats.Counter_t
	Nadd   stats.Counter_t
}

type Obj_t interface {
	EvictFromCache() // Cache will evict this object, if refcnt = 0 after calling Evict()
	EvictDone()      // Cache has evicted the object, the refcnt was 0
}

type Objref_t struct {
	Key    int
	Obj    Obj_t
	refcnt uint32
	tstamp uint64
}

func MkObjref(obj Obj_t, key int) *Objref_t {
	e := &Objref_t{}
	e.Obj = obj
	e.Key = key
	e.refcnt = uint32(1)
	e.tstamp = runtime.Rdtsc()
	return e
}

func (ref *Objref_t) Refcnt() uint32 {
	c := atomic.LoadUint32(&ref.refcnt)
	return c
}

// Up returns whether Up increased refcnt and, if so, the old value of the
// refcnt. Clients of cache can use this to implement other higher-level caches.
// For example, lock-free lockup in the directory cache uses the return values
// to decide if the lookup races with an unlink or evict.  Many clients of cache
// don't use Up() directly, because they use Lookup(), or they ignore return
// value of Up(), because they know that the refcnt > 0 (because the Up()
// follows a Lookup()).
func (ref *Objref_t) Up() (uint32, bool) {
	for {
		v := atomic.LoadUint32(&ref.refcnt)
		if REMOVE&v != 0 {
			return 0, false
		}
		if int(int32(v)) < 0 {
			//fmt.Printf("%v %#x\n", v, v)
			panic("no")
		}
		if atomic.CompareAndSwapUint32(&ref.refcnt, v, v+1) {
			//if res.Kernel && v != REMOVE && v+1 > 101 {
			//	panic("wtlaskjfd")
			//}
			return v, true
		}
	}
}

func (ref *Objref_t) Down() uint32 {
	v := atomic.AddUint32(&ref.refcnt, ^uint32(0))
	if int(int32(v)) < 0 {
		//fmt.Printf("%v %#x\n", v, v)
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
	return c.cache.Size()
}

// Other threads may have a reference to this item and may Ref{up,down}; the
// increment must therefore be atomic. Furthermore, the evictor maybe trying to
// remove the key from the cache, and we need to arrange that lookup of a key
// and incrementing a refcnt is atomic with respect to removing the key.  We
// guarantee this by having Remove set the REMOVE flag in the refcnt and using
// CAS to increment only when refcnt hasn't changed.
func (c *cache_t) lookupinc(key int) (*Objref_t, bool) {
	for {
		v, ok := c.cache.Get(key)
		if !ok {
			return nil, ok
		}
		e := v.(*Objref_t)
		refcnt := atomic.LoadUint32(&e.refcnt)
		new := refcnt + 1
		if refcnt & REMOVE != new & REMOVE {
			panic("no")
		}
		if int(int32(refcnt)) < 0 {
			//fmt.Printf("%v %#x\n", refcnt, refcnt)
			panic("nuts")
		}
		if refcnt&REMOVE == 0 && atomic.CompareAndSwapUint32(&e.refcnt, refcnt, new) {
			return e, ok
		}
	}
}

func (c *cache_t) Lookup(key int, mkobj func(int) Obj_t) (*Objref_t, bool) {
	for {
		e, ok := c.lookupinc(key)
		if ok {
			c.stats.Nhit.Inc()
			atomic.StoreUint64(&e.tstamp, runtime.Rdtsc())
			return e, false
		}
		e = MkObjref(mkobj(key), key)
		_, ok = c.cache.Set(key, e)
		if ok {
			c.stats.Nadd.Inc()
			return e, true
		}
		// someone else created it, try lookup again
	}
}

// Note remove can fail, because a concurrent lookup resurrects the refcnt (or
// the refcnt is already larger than 0).
func (c *cache_t) Remove(key int) bool {
	return c._remove(key, true)
}

// Eviction may try to evict an entry that has since been deleted from the
// cache. We could instead CAS to add REMOVE immediately and then delete on
// success, but this is fine.
func (c *cache_t) TryRemove(key int) bool {
	return c._remove(key, false)
}

func (c *cache_t) _remove(key int, mustexist bool) bool {
	if v, ok := c.cache.Get(key); ok {
		e := v.(*Objref_t)
		cnt := e.Refcnt()
		if cnt == 0 && atomic.CompareAndSwapUint32(&e.refcnt, cnt, cnt|REMOVE) {
			c.delete(e)
			return true
		}
		return false
	} else {
		if mustexist {
			panic("Remove: non existing")
		}
		return false
	}
}

func (c *cache_t) Stats() string {
	s := ""
	s += stats.Stats2String(c.stats)
	return s
}

type ByStamp []hashtable.Pair_t

func (a ByStamp) Len() int      { return len(a) }
func (a ByStamp) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByStamp) Less(i, j int) bool {
	e1 := a[i].Value.(*Objref_t)
	e2 := a[j].Value.(*Objref_t)
	v1 := atomic.LoadUint64(&e1.tstamp)
	v2 := atomic.LoadUint64(&e2.tstamp)
	return v1 < v2
}

// Evicts up-to half of the objects in the cache. returns the number of cache
// entries remaining and the number evicted.
func (c *cache_t) Evict_half() (int, int) {
	c.Lock()
	defer c.Unlock()

	upto := c.cache.Size()
	did := 0
	elems := c.cache.Elems()
	sort.Sort(ByStamp(elems))
	for _, p := range elems { // XXX only need the keys, not complete elems
		e := p.Value.(*Objref_t)
		// evict each inode's dcache before setting REMOVE to ensure
		// that a concurrent lock-free namei can't succeed on an
		// evicted inode
		e.Obj.EvictFromCache()
		if !c.TryRemove(e.Key) {
			continue
		}
		e.Obj.EvictDone()
		did++
	}
	return upto - did, did
}

func (c *cache_t) delete(o *Objref_t) {
	c.cache.Del(o.Key)
	c.stats.Nevict.Inc()
}
