package fs

import "fmt"
import "sync"
import "common"

const dcache_debug = false

type dcentry_t struct {
	inum common.Inum_t
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

func (dc *dcache_t) add(pn string, inum common.Inum_t) {
	dc.Lock()
	defer dc.Unlock()

	if dcache_debug {
		fmt.Printf("add: %v %d\n", pn, inum)
	}
	dc.dcache[pn] = &dcentry_t{inum: inum}
}

func (dc *dcache_t) lookup(pn string) (common.Inum_t, bool) {
	dc.Lock()
	defer dc.Unlock()
	inum := common.Inum_t(0)
	de, ok := dc.dcache[pn]
	if ok {
		dc.stats.Nhit.Inc()
		inum = de.inum
	} else {
		dc.stats.Nmiss.Inc()
	}
	if dcache_debug {
		fmt.Printf("lookup: %v inum %d ok %v\n", pn, inum, ok)
	}
	return inum, ok
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
