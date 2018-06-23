package fs

//import "fmt"
import "sync"

// import "sync/atomic"
import "common"

type entry_t struct {
	obj    common.Obj_t
	refcnt int
}

type cache_t struct {
	sync.Mutex
	maxsize int
	cache   map[int]*entry_t
}

func mkCache(size int, async bool) *cache_t {
	c := &cache_t{}
	c.maxsize = size
	c.cache = make(map[int]*entry_t, size)
	return c
}

func (c *cache_t) Lookup(key int, mkobj func(int) common.Obj_t) (common.Obj_t, bool) {
	c.Lock()
	e, ok := c.cache[key]
	if ok {
		e.refcnt++
		c.Unlock()
		return e.obj, false
	}
	e = &entry_t{}
	e.obj = mkobj(key)
	e.refcnt = 1
	c.cache[key] = e
	c.Unlock()
	return e.obj, true
}

// Return true if obj has been evicted
func (c *cache_t) Done(key int) bool {
	c.Lock()
	evicted := false
	if e, ok := c.cache[key]; ok {
		e.refcnt--
		if e.refcnt < 0 {
			panic("Done: double done?")
		}
		if e.refcnt == 0 && e.obj.Evictnow() {
			delete(c.cache, key)
			evicted = true
		}
	} else {
		panic("Done: non existing")
	}
	c.Unlock()
	return evicted
}
