package fs

//import "fmt"
import "strconv"
import "sync"
import "sync/atomic"
import "common"

// Fixed-size cache of objects. Main invariant: an object is in memory once so
// that threads see each other's updates.  The challenging case is that an
// object can be evicted only when no thread has a reference to the object.  To
// keep track of the references to an object, refcache refcounts the references
// to an object.  The client of refcache, must call Refup/Refdown to ensure a
// correct refcount.
//
// In an alternate world, we would use finalizers on an object, and the GC would
// inform refcache_t that an object isn't in use anymore.  Refcache itself would
// use a weak reference to an object, so that the GC could collect the object, if
// it is low on memory.

const refcache_debug = false
const always_eager = false // for testing
const evict_on_lookup = false

type refcache_t struct {
	sync.Mutex
	maxsize     int
	refs        map[int]*common.Ref_t // XXX use fsrb.go instead?
	reflru      reflru_t
	evict_async bool // the caller needs to call Flush or evict on Lookup

	// stats
	Nevict common.Counter_t
}

func mkRefcache(size int, async bool) *refcache_t {
	ic := &refcache_t{}
	ic.maxsize = size
	ic.evict_async = async
	ic.refs = make(map[int]*common.Ref_t, size)
	return ic
}

func (irc *refcache_t) Len() int {
	irc.Lock()
	ret := len(irc.refs)
	irc.Unlock()
	return ret
}

func (irc *refcache_t) Lookup(key int, mkobj func(int, *common.Ref_t) common.Obj_t) (*common.Ref_t, bool, common.Err_t) {
	irc.Lock()

	ref, ok := irc.refs[key]
	if ok {
		// other threads may have a reference to this item and may
		// Ref{up,down}; the increment must therefore be atomic
		atomic.AddInt64(&ref.Refcnt, 1)
		irc.reflru.mkhead(ref)
		irc.Unlock()
		return ref, false, 0
	}

	ref = &common.Ref_t{}
	// no thread can have a reference to this item, no need for atomic
	ref.Refcnt = 1
	ref.Key = key
	ref.Obj = mkobj(key, ref)

	irc.refs[key] = ref
	irc.reflru.mkhead(ref)
	irc.Unlock()

	return ref, true, 0
}

func (irc *refcache_t) Refup(ref *common.Ref_t) {
	if atomic.AddInt64(&ref.Refcnt, 1) == 1 {
		panic("must already have ref")
	}
}

// Return true if refcnt has reached 0 and has been evicted
func (irc *refcache_t) Refdown(ref *common.Ref_t, stable bool) bool {
	v := atomic.AddInt64(&ref.Refcnt, -1)
	if v < 0 {
		panic("refdown")
	}

	evicted := false
	if v == 0 {
		if ref.Obj.Evictnow() {
			irc.Lock()
			if atomic.LoadInt64(&ref.Refcnt) == 0 {
				irc._delete(ref)
				evicted = true
			} else if stable {
				panic("ref ressurection")
			}
			irc.Unlock()
		}
	}
	return evicted
}

//
// Implementation
//

func (irc *refcache_t) nlive() int {
	n := 0
	for _, r := range irc.refs {
		if r.Refcnt > 0 {
			n++
		}
	}
	return n
}

func (irc *refcache_t) Stats() string {
	s := ""
	if common.Stats {
		s := "\n\tsize "
		s += strconv.Itoa(len(irc.refs))
		s += "\n\t#live "
		s += strconv.Itoa(irc.nlive())
	}
	s += common.Stats2String(*irc)
	return s
}

func (irc *refcache_t) _delete(ir *common.Ref_t) {
	delete(irc.refs, ir.Key)
	irc.reflru.remove(ir)
	irc.Nevict.Inc()
}

// evicts up-to half of the objects in the cache. returns the number of cache
// entries remaining.
func (irc *refcache_t) Evict_half() int {
	irc.Lock()
	defer irc.Unlock()

	upto := len(irc.refs)
	did := 0
	var back *common.Ref_t
	for p := irc.reflru.tail; p != nil && did < upto; p = back {
		back = p.Refprev
		if p.Refcnt != 0 {
			continue
		}
		// imemnode with refcount of 0 must have non-zero links and
		// thus cannot be freed.
		irc._delete(p)
		// imemnode eviction acquires no locks and block eviction
		// acquires only a leaf lock (physmem lock). furthermore,
		// neither eviction blocks on IO, thus it is safe to evict here
		// with locks held.
		p.Obj.Evict()
		did++
	}
	return len(irc.refs)
}

// LRU list of references
type reflru_t struct {
	head *common.Ref_t
	tail *common.Ref_t
}

func (rl *reflru_t) mkhead(ir *common.Ref_t) {
	if memfs {
		return
	}
	rl._mkhead(ir)
}

func (rl *reflru_t) _mkhead(ir *common.Ref_t) {
	if rl.head == ir {
		return
	}
	rl._remove(ir)
	if rl.head != nil {
		rl.head.Refprev = ir
	}
	ir.Refnext = rl.head
	rl.head = ir
	if rl.tail == nil {
		rl.tail = ir
	}
}

func (rl *reflru_t) _remove(ir *common.Ref_t) {
	if rl.tail == ir {
		rl.tail = ir.Refprev
	}
	if rl.head == ir {
		rl.head = ir.Refnext
	}
	if ir.Refprev != nil {
		ir.Refprev.Refnext = ir.Refnext
	}
	if ir.Refnext != nil {
		ir.Refnext.Refprev = ir.Refprev
	}
	ir.Refprev, ir.Refnext = nil, nil
}

func (rl *reflru_t) remove(ir *common.Ref_t) {
	if memfs {
		return
	}
	rl._remove(ir)
}
