package fs

import "fmt"
import "sync"
import "common"

// Fixed-size cache of objects. Main invariant: an object is in memory once so
// that threads see each other's updates.  An object can be evicted only when no
// thread has a reference to the object.

const refcache_debug = false
const always_eager = false // for testing

// Objects in the cache must support the following interface:
type obj_t interface {
	Evictnow() bool
	Key() int
	Evict()
}

// The cache contains refcounted references to obj
type ref_t struct {
	sync.Mutex // only there to initialize obj
	obj        obj_t
	refcnt     int
	key        int
	valid      bool
	s          string
	refnext    *ref_t
	refprev    *ref_t
}

type refcache_t struct {
	sync.Mutex
	maxsize     int
	refs        map[int]*ref_t // XXX use fsrb.go instead?
	reflru      reflru_t
	evict_async bool // the caller needs to call Flush or evict on Lookup

	// stats
	nevict int
}

//
// Public interface
//

func mkRefcache(size int, async bool) *refcache_t {
	ic := &refcache_t{}
	ic.maxsize = size
	ic.evict_async = async
	ic.refs = make(map[int]*ref_t, size)
	return ic
}

// returns a locked ref
func (irc *refcache_t) Lookup(key int, s string) (*ref_t, obj_t, common.Err_t) {
	irc.Lock()

	ref, ok := irc.refs[key]
	if ok {
		ref.refcnt++
		if refcache_debug {
			fmt.Printf("ref hit %v %v %v\n", key, ref.refcnt, s)
		}
		irc.reflru.mkhead(ref)
		irc.Unlock()
		ref.Lock()
		return ref, nil, 0
	}

	var victim obj_t
	if len(irc.refs) >= irc.maxsize {
		victim = irc.replace()
		if victim == nil {
			fmt.Printf("refs in use %v limited %v\n", len(irc.refs), irc.maxsize)
			irc.Unlock()
			return nil, nil, -common.ENOMEM
		}
	}

	ref = &ref_t{}
	ref.refcnt = 1
	ref.key = key
	ref.valid = false
	ref.s = s
	ref.Lock()

	irc.refs[key] = ref
	irc.reflru.mkhead(ref)

	if refcache_debug {
		fmt.Printf("ref miss %v cnt %v %s\n", key, ref.refcnt, s)
	}

	irc.Unlock()

	return ref, victim, 0
}

func (irc *refcache_t) Refup(o obj_t, s string) {
	irc.Lock()
	defer irc.Unlock()

	ref, ok := irc.refs[o.Key()]
	if !ok {
		panic("refup")
	}

	if refcache_debug {
		fmt.Printf("refdup %v cnt %v %s\n", o.Key(), ref.refcnt, s)
	}

	ref.refcnt++
}

// Return true if refcnt has reached 0 and has been evicted
func (irc *refcache_t) Refdown(o obj_t, s string) bool {
	irc.Lock()

	ref, ok := irc.refs[o.Key()]
	if !ok {
		panic("refdown: key not present")
	}
	if o != ref.obj {
		panic("refdown: different obj")
	}

	if refcache_debug {
		fmt.Printf("refdown %v cnt %v %s\n", o.Key(), ref.refcnt, s)
	}

	ref.refcnt--
	if ref.refcnt < 0 {
		panic("refdown")
	}

	evicted := false
	if ref.refcnt == 0 {
		now := ref.obj.Evictnow()
		if now || always_eager {
			irc._delete(ref)
			evicted = true
		}
	}

	irc.Unlock()

	return evicted
}

//
// Implementation
//

func (irc *refcache_t) nlive() int {
	n := 0
	for _, r := range irc.refs {
		if r.refcnt > 0 {
			n++
		}
	}
	return n
}

func (irc *refcache_t) _delete(ir *ref_t) {
	delete(irc.refs, ir.key)
	irc.reflru.remove(ir)
	irc.nevict++
}

func (irc *refcache_t) replace() obj_t {
	for ir := irc.reflru.tail; ir != nil; ir = ir.refprev {
		// fmt.Printf("%v %v %s\n", ir.key, ir.refcnt, ir.s)
		if ir.refcnt == 0 {
			if refcache_debug {
				fmt.Printf("_replace: victim %v %v\n", ir.key, ir.s)
			}
			irc._delete(ir)
			return ir.obj
		}
	}
	return nil
}

// evicts up-to half of the objects in the cache. returns the number of cache
// entries remaining.
func (irc *refcache_t) Evict_half() int {
	irc.Lock()
	defer irc.Unlock()

	upto := len(irc.refs)
	did := 0
	var back *ref_t
	for p := irc.reflru.tail; p != nil && did < upto; p = back {
		back = p.refprev
		if p.refcnt != 0 {
			continue
		}
		// imemnode with refcount of 0 must have non-zero links and
		// thus cannot be freed.
		irc._delete(p)
		// imemnode eviction acquires no locks and block eviction
		// acquires only a leaf lock (physmem lock). furthermore,
		// neither eviction blocks on IO, thus it is safe to evict here
		// with locks held.
		p.obj.Evict()
		did++
	}
	return len(irc.refs)
}

// LRU list of references
type reflru_t struct {
	head *ref_t
	tail *ref_t
}

func (rl *reflru_t) mkhead(ir *ref_t) {
	if memfs {
		return
	}
	rl._mkhead(ir)
}

func (rl *reflru_t) _mkhead(ir *ref_t) {
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

func (rl *reflru_t) _remove(ir *ref_t) {
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

func (rl *reflru_t) remove(ir *ref_t) {
	if memfs {
		return
	}
	rl._remove(ir)
}
