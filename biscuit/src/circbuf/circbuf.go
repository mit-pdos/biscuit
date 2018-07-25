package circbuf

import "defs"
import "fdops"
import "mem"

// a circular buffer that is read/written by userspace. not thread-safe -- it
// is intended to be used by one daemon.
type Circbuf_t struct {
	mem   mem.Page_i
	Buf   []uint8
	bufsz int
	// XXX uint
	head int
	tail int
	p_pg mem.Pa_t
}

func (cb *Circbuf_t) Bufsz() int {
	return cb.bufsz
}

func (cb *Circbuf_t) Set(nb []uint8, did int, m mem.Page_i) {
	cb.mem = m
	cb.Buf = nb
	cb.bufsz = len(nb)
	cb.head = did
	cb.tail = 0
}

// may fail to allocate a page for the buffer. when cb's life is over, someone
// must free the buffer page by calling cb_release().
func (cb *Circbuf_t) Cb_init(sz int, m mem.Page_i) defs.Err_t {
	bufmax := int(mem.PGSIZE)
	if sz <= 0 || sz > bufmax {
		panic("bad circbuf size")
	}
	cb.mem = m
	cb.bufsz = sz
	cb.head, cb.tail = 0, 0
	// lazily allocated the buffers. it is easier to handle an error at the
	// time of read or write instead of during the initialization of the
	// object using a circbuf.
	return 0
}

// provide the page for the buffer explicitly; useful for guaranteeing that
// read/writes won't fail to allocate memory.
func (cb *Circbuf_t) Cb_init_phys(v []uint8, p_pg mem.Pa_t, m mem.Page_i) {
	cb.mem = m
	cb.mem.Refup(p_pg)
	cb.p_pg = p_pg
	cb.Buf = v
	cb.bufsz = len(cb.Buf)
	cb.head, cb.tail = 0, 0
}

func (cb *Circbuf_t) Cb_release() {
	if cb.Buf == nil {
		return
	}
	cb.mem.Refdown(cb.p_pg)
	cb.p_pg = 0
	cb.Buf = nil
	cb.head, cb.tail = 0, 0
}

func (cb *Circbuf_t) Cb_ensure() defs.Err_t {
	if cb.Buf != nil {
		return 0
	}
	if cb.bufsz == 0 {
		panic("not initted")
	}
	pg, p_pg, ok := cb.mem.Refpg_new_nozero()
	if !ok {
		return -defs.ENOMEM
	}
	bpg := mem.Pg2bytes(pg)[:]
	bpg = bpg[:cb.bufsz]
	cb.Cb_init_phys(bpg, p_pg, cb.mem)
	return 0
}

func (cb *Circbuf_t) Full() bool {
	return cb.head-cb.tail == cb.bufsz
}

func (cb *Circbuf_t) Empty() bool {
	return cb.head == cb.tail
}

func (cb *Circbuf_t) Left() int {
	used := cb.head - cb.tail
	rem := cb.bufsz - used
	return rem
}

func (cb *Circbuf_t) Used() int {
	used := cb.head - cb.tail
	return used
}

func (cb *Circbuf_t) Copyin(src fdops.Userio_i) (int, defs.Err_t) {
	if err := cb.Cb_ensure(); err != 0 {
		return 0, err
	}
	if cb.Full() {
		return 0, 0
	}
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	c := 0
	// wraparound?
	if ti <= hi {
		dst := cb.Buf[hi:]
		wrote, err := src.Uioread(dst)
		if err != 0 {
			return 0, err
		}
		if wrote != len(dst) {
			cb.head += wrote
			return wrote, 0
		}
		c += wrote
		hi = (cb.head + wrote) % cb.bufsz
	}
	// XXXPANIC
	if hi > ti {
		panic("wut?")
	}
	dst := cb.Buf[hi:ti]
	wrote, err := src.Uioread(dst)
	c += wrote
	if err != 0 {
		return c, err
	}
	cb.head += c
	return c, 0
}

func (cb *Circbuf_t) Copyout(dst fdops.Userio_i) (int, defs.Err_t) {
	return cb.Copyout_n(dst, 0)
}

func (cb *Circbuf_t) Copyout_n(dst fdops.Userio_i, max int) (int, defs.Err_t) {
	if err := cb.Cb_ensure(); err != 0 {
		return 0, err
	}
	if cb.Empty() {
		return 0, 0
	}
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	c := 0
	// wraparound?
	if hi <= ti {
		src := cb.Buf[ti:]
		if max != 0 && max < len(src) {
			src = src[:max]
		}
		wrote, err := dst.Uiowrite(src)
		if err != 0 {
			return 0, err
		}
		if wrote != len(src) || wrote == max {
			cb.tail += wrote
			return wrote, 0
		}
		c += wrote
		if max != 0 {
			max -= c
		}
		ti = (cb.tail + wrote) % cb.bufsz
	}
	// XXXPANIC
	if ti > hi {
		panic("wut?")
	}
	src := cb.Buf[ti:hi]
	if max != 0 && max < len(src) {
		src = src[:max]
	}
	wrote, err := dst.Uiowrite(src)
	if err != 0 {
		return 0, err
	}
	c += wrote
	cb.tail += c
	return c, 0
}

// returns slices referencing the internal circular buffer [head+offset,
// head+offset+sz) which must be outside [tail, head). returns two slices when
// the returned buffer wraps.
// XXX XXX XXX XXX XXX remove arg
func (cb *Circbuf_t) Rawwrite(offset, sz int) ([]uint8, []uint8) {
	if cb.Buf == nil {
		panic("no lazy allocation for tcp")
	}
	if cb.Left() < sz {
		panic("bad size")
	}
	if sz == 0 {
		return nil, nil
	}
	oi := (cb.head + offset) % cb.bufsz
	oe := (cb.head + offset + sz) % cb.bufsz
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	var r1 []uint8
	var r2 []uint8
	if ti <= hi {
		if (oi >= ti && oi < hi) || (oe > ti && oe <= hi) {
			panic("intersects with user data")
		}
		r1 = cb.Buf[oi:]
		if len(r1) > sz {
			r1 = r1[:sz]
		} else {
			r2 = cb.Buf[:oe]
		}
	} else {
		// user data wraps
		if !(oi >= hi && oi < ti && oe > hi && oe <= ti) {
			panic("intersects with user data")
		}
		r1 = cb.Buf[oi:oe]
	}
	return r1, r2
}

// advances head index sz bytes (allowing the bytes to be copied out)
func (cb *Circbuf_t) Advhead(sz int) {
	if cb.Full() || cb.Left() < sz {
		panic("advancing full cb")
	}
	cb.head += sz
}

// returns slices referencing the circular buffer [tail+offset, tail+offset+sz)
// which must be inside [tail, head). returns two slices when the returned
// buffer wraps.
func (cb *Circbuf_t) Rawread(offset int) ([]uint8, []uint8) {
	if cb.Buf == nil {
		panic("no lazy allocation for tcp")
	}
	oi := (cb.tail + offset) % cb.bufsz
	hi := cb.head % cb.bufsz
	ti := cb.tail % cb.bufsz
	var r1 []uint8
	var r2 []uint8
	if ti < hi {
		if oi >= hi || oi < ti {
			panic("outside user data")
		}
		r1 = cb.Buf[oi:hi]
	} else {
		if oi >= hi && oi < ti {
			panic("outside user data")
		}
		tlen := len(cb.Buf[ti:])
		if tlen > offset {
			r1 = cb.Buf[oi:]
			r2 = cb.Buf[:hi]
		} else {
			roff := offset - tlen
			r1 = cb.Buf[roff:hi]
		}
	}
	return r1, r2
}

// advances head index sz bytes (allowing the bytes to be copied out)
func (cb *Circbuf_t) Advtail(sz int) {
	if sz != 0 && (cb.Empty() || cb.Used() < sz) {
		panic("advancing empty cb")
	}
	cb.tail += sz
}
