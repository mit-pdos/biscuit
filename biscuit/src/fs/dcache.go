package fs

import "fmt"
import "sync"
import "common"

// invariant: if in dcache, then in icache (i.e., refcnt on handle for idm.inum >= 1)

const dcache_debug = false

type dcentry_t struct {
	idm *imemnode_t
}

type dcache_stats_t struct {
	Nhit  common.Counter_t
	Nmiss common.Counter_t
}

type dcache_t struct {
	sync.Mutex
	dcache map[string]*dcentry_t
	stats  dcache_stats_t
}

func (dc *dcache_t) add(pn string, idm *imemnode_t) {
	dc.Lock()
	defer dc.Unlock()

	if dcache_debug {
		fmt.Printf("add: %v %d\n", pn, idm.inum)
	}
	dc.dcache[pn] = &dcentry_t{idm: idm}
	idm.fs.icache.Refup(idm, "dc.add")
}

func (dc *dcache_t) remove(pn string) {
	dc.Lock()
	defer dc.Unlock()

	de, ok := dc.dcache[pn]
	if ok {
		if dcache_debug {
			fmt.Printf("remove: %v\n", pn)
		}
		de.idm.fs.icache.Refdown(de.idm, "dc.remove")
		delete(dc.dcache, pn)
	}
}

func (dc *dcache_t) lookup(pn string) (*imemnode_t, bool) {
	dc.Lock()
	defer dc.Unlock()
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

func (dc *dcache_t) Stats() string {
	s := "dcache" + common.Stats2String(dc.stats)
	dc.stats = dcache_stats_t{}
	return s
}

func mkDcache() *dcache_t {
	c := &dcache_t{}
	c.dcache = make(map[string]*dcentry_t)
	return c
}
