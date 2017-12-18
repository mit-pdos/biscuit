package main

import "fmt"
import "sync"
import "sort"

// Cache of in-mmory objects. Main invariant: an object is in memory once so
// that threads see each other's updates.

type obj_t interface {
	evict()
	key() int
	ilock(s string)
	iunlock(s string)
}

// The cache contains refcounted references to obj
type ref_t struct {
	sync.Mutex      // only there to initialize obj
	obj             obj_t
	refcnt          int
	key             int
	valid           bool
	refnext		*ref_t
	refprev		*ref_t
}

type refcache_t struct {
	sync.Mutex
	size           int
	refs           map[int]*ref_t
	reflru         reflru_t
}

func make_refcache(size int) *refcache_t {
	ic := &refcache_t{}
	ic.size = size
	ic.refs = make(map[int]*ref_t, size)
	return ic
}

func print_live_refs() {
	fmt.Printf("refs %v\n", len(irefcache.refs))
	for _, r := range irefcache.refs {
		fmt.Printf("inode %v refcnt %v opencnt %v\n", r)
	}
}

func (irc *refcache_t) _replace() obj_t {
	for ir := irc.reflru.tail; ir != nil; ir = ir.refprev {
		if ir.refcnt == 0 {
			if fs_debug {
				fmt.Printf("_replace: victim %v\n", ir.key)
			}
			delete(irc.refs, ir.key)
			irc.reflru.remove(ir)
			return ir.obj
		}
	}
	return nil
}

// returns a locked ref
func (irc *refcache_t) lookup(key int, s string) (*ref_t, err_t) {
	irc.Lock()
	
	ref, ok := irc.refs[key]
	if ok {
		ref.refcnt++
		if fs_debug {
			fmt.Printf("ref hit %v %v %v\n", key, ref.refcnt, s)
		}
		irc.reflru.mkhead(ref)
		irc.Unlock()
		ref.Lock()
		return ref, 0
	}

	var victim obj_t
	if len(irc.refs) >= irc.size {
		victim = irc._replace()
		if victim == nil {
			fmt.Printf("inodes in use %v limited %v\n", len(irc.refs), irc.size)
			panic("too many inodes")
			return nil, -ENOMEM
		}
        }
 	

	ref = &ref_t{}
	ref.refcnt = 1
	ref.key = key
	ref.valid = false
	ref.Lock()
	
	irc.refs[key] = ref
	irc.reflru.mkhead(ref)
	
	if fs_debug {
		fmt.Printf("ref miss %v cnt %v %s\n", key, ref.refcnt, s)
	}

	
	irc.Unlock()

	// release cache lock, now free victim
	if victim != nil {
		victim.evict()   // XXX in another thread?
	}

	return ref, 0
}


func (irc *refcache_t) refup(o obj_t) {
	irc.Lock()
	defer irc.Unlock()
	
	iref, ok := irc.refs[o.key()]
	if !ok {
		panic("refup")
	}
	iref.refcnt++
}

func (irc *refcache_t) refdown(o obj_t, s string) {
	irc.Lock()
	defer irc.Unlock()
	
	iref, ok := irc.refs[o.key()]
	if !ok {
		panic("refdown: key not present")
	}
	if o != iref.obj {
		panic("refdown: different obj")
	}
	
	iref.refcnt--
	if iref.refcnt < 0 {
		panic("refdown")
	}
}

func (irc *refcache_t) lockall(imems []obj_t) []obj_t {
	var locked []obj_t
	sort.Slice(imems, func(i, j int) bool { return imems[i].key() < imems[j].key() })
	for _, imem := range imems {
		dup := false
		for _, l := range locked {
			if imem.key() == l.key() {
				dup = true
			}
		}
		if !dup {
			locked = append(locked, imem)
			imem.ilock("lockall")
		}
	}
	return locked
}

// LRU list of references
type reflru_t struct {
	head	*ref_t
	tail	*ref_t
}

func (rl *reflru_t) mkhead(ir *ref_t) {
	if memtime {
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
	if memtime {
		return
	}
	rl._remove(ir)
}

