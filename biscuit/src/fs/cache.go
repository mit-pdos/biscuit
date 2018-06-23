package fs

//import "fmt"
import "strconv"
import "sync"

import "sync/atomic"
import "common"

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

type objref_t struct {
	key    int
	obj    common.Obj_t
	refcnt int64
}

func (ref *objref_t) Up() {
	if atomic.AddInt64(&ref.refcnt, 1) == 1 {
		panic("must already have ref")
	}
}

func (ref *objref_t) Down() int64 {
	v := atomic.AddInt64(&ref.refcnt, -1)
	if v < 0 {
		panic("Down")
	}
	return v
}

type cstats_t struct {
	Nevict common.Counter_t
}

type cache_t struct {
	sync.Mutex
	maxsize int
	cache   map[int]*objref_t
	stats   cstats_t
}

func mkCache(size int) *cache_t {
	c := &cache_t{}
	c.maxsize = size
	c.cache = make(map[int]*objref_t, size)
	return c
}

func (c *cache_t) Len() int {
	c.Lock()
	ret := len(c.cache)
	c.Unlock()
	return ret
}

func (c *cache_t) Lookup(key int, mkobj func(int) common.Obj_t) (*objref_t, bool) {
	c.Lock()
	e, ok := c.cache[key]
	if ok {
		e.refcnt++
		c.Unlock()
		return e, false
	}
	e = &objref_t{}
	e.obj = mkobj(key)
	e.key = key
	e.refcnt = 1
	c.cache[key] = e
	c.Unlock()
	return e, true
}

func (c *cache_t) Evict(key int) {
	c.Lock()
	if e, ok := c.cache[key]; ok {
		if e.refcnt < 0 {
			panic("Evict: negative refcnt")
		}
		if e.refcnt == 0 {
			delete(c.cache, key)
			c.stats.Nevict.Inc()
		} else {
			panic("Evict: refcnt > 0")
		}
	} else {
		panic("Evict: non existing")
	}
	c.Unlock()
}

func (c *cache_t) Stats() string {
	s := ""
	if common.Stats {
		s := "\n\tsize "
		s += strconv.Itoa(len(c.cache))
		s += "\n\t#live "
		s += strconv.Itoa(c.nlive())
	}
	s += common.Stats2String(c.stats)
	return s
}

func (c *cache_t) nlive() int {
	n := 0
	for _, e := range c.cache {
		if e.refcnt > 0 {
			n++
		}
	}
	return n
}
