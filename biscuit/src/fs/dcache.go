package fs

import "fmt"
import "hash/fnv"
import "sync"
import "common"

// invariant: if in dcache, then in icache (i.e., refcnt on ref for idm >= 1)

const dcache_debug = false

type dcentry_t struct {
	idm *imemnode_t
}

type dcache_stats_t struct {
	Nhit  common.Counter_t
	Nmiss common.Counter_t
	Nadd  common.Counter_t
	Ndel  common.Counter_t
}

type dshard_t struct {
	sync.RWMutex
	dcache map[string]*dcentry_t
	stats  dcache_stats_t
}

func (dc *dshard_t) add(pn string, idm *imemnode_t) {
	dc.Lock()
	defer dc.Unlock()

	if dcache_debug {
		fmt.Printf("add: %v %d\n", pn, idm.inum)
	}
	dc.dcache[pn] = &dcentry_t{idm: idm}
	idm.Refup("add")
	dc.stats.Nadd.Inc()
}

func (dc *dshard_t) remove(pn string) {
	dc.Lock()
	defer dc.Unlock()

	de, ok := dc.dcache[pn]
	if ok {
		if dcache_debug {
			fmt.Printf("remove: %v\n", pn)
		}
		// note implicit locking ordering: dcache, then icache/cache
		// alternate design: caller invokes Refdown()
		de.idm.Refdown("dc.remove")
		delete(dc.dcache, pn)
		dc.stats.Ndel.Inc()
	}
}

func (dc *dshard_t) lookup(pn string) (*imemnode_t, bool) {
	dc.RLock()
	defer dc.RUnlock()

	var idm *imemnode_t
	de, ok := dc.dcache[pn]
	if ok {
		dc.stats.Nhit.Inc()
		idm = de.idm
	} else {
		dc.stats.Nmiss.Inc()
	}
	if dcache_debug && idm != nil {
		fmt.Printf("lookup: %v inum %d ok %v\n", pn, idm.inum, ok)
	}
	return idm, ok
}

func (dc *dshard_t) Stats() string {
	s := common.Stats2String(dc.stats)
	dc.stats = dcache_stats_t{}
	return s
}

func mkDshard() *dshard_t {
	c := &dshard_t{}
	c.dcache = make(map[string]*dcentry_t)
	return c
}

const NSHARD = 100

type dcache_t struct {
	shards []*dshard_t
}

func MkShardTable() *dcache_t {
	s := &dcache_t{}
	s.shards = make([]*dshard_t, NSHARD)
	for i := 0; i < NSHARD; i++ {
		s.shards[i] = mkDshard()
	}
	return s
}

func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

func (dc *dcache_t) Lookup(pn string) (*imemnode_t, bool) {
	i := hash(pn)
	return dc.shards[i%NSHARD].lookup(pn)
}

func (dc *dcache_t) Add(pn string, idm *imemnode_t) {
	i := hash(pn)
	dc.shards[i%NSHARD].add(pn, idm)
}

func (dc *dcache_t) Remove(pn string) {
	i := hash(pn)
	dc.shards[i%NSHARD].remove(pn)
}

func (dc *dcache_t) Stats() string {
	r := "dcache: "
	for _, s := range dc.shards {
		r += s.Stats()
		r += " "
	}
	return r + "\n"

}
