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
	key     int
	obj     common.Obj_t
	refcnt  int64
	refnext *objref_t
	refprev *objref_t
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
	maxsize   int
	cache     map[int]*objref_t
	objreflru objreflru_t
	stats     cstats_t
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
		// other threads may have a reference to this item and may
		// Ref{up,down}; the increment must therefore be atomic
		atomic.AddInt64(&e.refcnt, 1)
		c.objreflru.mkhead(e)
		c.Unlock()
		return e, false
	}
	e = &objref_t{}
	e.obj = mkobj(key)
	e.key = key
	e.refcnt = 1
	c.cache[key] = e
	c.objreflru.mkhead(e)
	c.Unlock()
	return e, true
}

func (c *cache_t) Remove(key int) {
	c.Lock()
	if e, ok := c.cache[key]; ok {
		if e.refcnt < 0 {
			panic("Evict: negative refcnt")
		}
		if e.refcnt == 0 {
			c.delete(e)
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

// evicts up-to half of the objects in the cache. returns the number of cache
// entries remaining.
func (c *cache_t) Evict_half() int {
	c.Lock()
	defer c.Unlock()

	upto := len(c.cache)
	did := 0
	var back *objref_t
	for p := c.objreflru.tail; p != nil && did < upto; p = back {
		back = p.refprev
		if p.refcnt != 0 {
			continue
		}
		// imemnode with refcount of 0 must have non-zero links and
		// thus can be freed.  (in fact, they already have been freed)
		c.delete(p)
		// imemnode eviction acquires no locks and block eviction
		// acquires only a leaf lock (physmem lock). furthermore,
		// neither eviction blocks on IO, thus it is safe to evict here
		// with locks held.
		p.obj.Evict()
		did++
	}
	return len(c.cache)
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

func (c *cache_t) delete(o *objref_t) {
	delete(c.cache, o.key)
	c.objreflru.remove(o)
	c.stats.Nevict.Inc()
}

// LRU list of references
type objreflru_t struct {
	head *objref_t
	tail *objref_t
}

func (rl *objreflru_t) mkhead(ir *objref_t) {
	if memfs {
		return
	}
	rl._mkhead(ir)
}

func (rl *objreflru_t) _mkhead(ir *objref_t) {
	if rl.head == ir {
		return
	}
	rl._remove(ir)
	if rl.head != nil {
		rl.head.refprev = ir
	}
	ir.refnext = rl.head
	rl.head = ir
	if rl.tail == nil {
		rl.tail = ir
	}
}

func (rl *objreflru_t) _remove(ir *objref_t) {
	if rl.tail == ir {
		rl.tail = ir.refprev
	}
	if rl.head == ir {
		rl.head = ir.refnext
	}
	if ir.refprev != nil {
		ir.refprev.refnext = ir.refnext
	}
	if ir.refnext != nil {
		ir.refnext.refprev = ir.refprev
	}
	ir.refprev, ir.refnext = nil, nil
}

func (rl *objreflru_t) remove(ir *objref_t) {
	if memfs {
		return
	}
	rl._remove(ir)
}
