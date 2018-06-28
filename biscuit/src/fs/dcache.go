package fs

import "fmt"

import "common"
import "hashtable"

// invariant: if in dcache, then in icache (i.e., refcnt on ref for idm >= 1)

const dcache_debug = false
const dcache_size = 10000

type dcache_stats_t struct {
	Nhit  common.Counter_t
	Nmiss common.Counter_t
	Nadd  common.Counter_t
	Ndel  common.Counter_t
}

type dcache_t struct {
	dcache *hashtable.Hashtable_t
	stats  dcache_stats_t
}

func (dc *dcache_t) Add(pn string, idm *imemnode_t) {
	if dcache_debug {
		fmt.Printf("add: %v %d\n", pn, idm.inum)
	}
	dc.dcache.Put(pn, idm)
	idm.Refup("add")
	dc.stats.Nadd.Inc()
}

func (dc *dcache_t) Remove(pn string) {
	if dcache_debug {
		fmt.Printf("remove: %v\n", pn)
	}
	v, ok := dc.dcache.Get(pn)
	if ok {
		// note implicit locking ordering: dcache, then icache/cache
		// alternate design: caller invokes Refdown()
		idm := v.(*imemnode_t)
		idm.Refdown("dc.remove")
		dc.dcache.Del(pn)
		dc.stats.Ndel.Inc()
	}
}

func (dc *dcache_t) Lookup(pn string) (*imemnode_t, bool) {
	v, ok := dc.dcache.Get(pn)
	var idm *imemnode_t
	if ok {
		dc.stats.Nhit.Inc()
		idm = v.(*imemnode_t)
	} else {
		dc.stats.Nmiss.Inc()
	}
	if dcache_debug && idm != nil {
		fmt.Printf("lookup: %v inum %d ok %v\n", pn, idm.inum, ok)
	}
	return idm, ok
}

func (dc *dcache_t) Stats() string {
	s := common.Stats2String(dc.stats)
	dc.stats = dcache_stats_t{}
	return s
}

func mkDcache() *dcache_t {
	c := &dcache_t{}
	c.dcache = hashtable.MkHash(dcache_size)
	return c
}
