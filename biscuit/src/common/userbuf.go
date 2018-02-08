package common

import "fmt"

// a helper object for read/writing from userspace memory. virtual address
// lookups and reads/writes to those addresses must be atomic with respect to
// page faults.
type userbuf_t struct {
	userva	int
	len	int
	// 0 <= off <= len
	off	int
	proc	*Proc_t
}

func (ub *userbuf_t) ub_init(p *Proc_t, uva, len int) {
	// XXX fix signedness
	if len < 0 {
		panic("negative length")
	}
	if len >= 1 << 39 {
		fmt.Printf("suspiciously large user buffer (%v)\n", len)
	}
	ub.userva = uva
	ub.len = len
	ub.off = 0
	ub.proc = p
}

func (ub *userbuf_t) remain() int {
	return ub.len - ub.off
}

func (ub *userbuf_t) totalsz() int {
	return ub.len
}
func (ub *userbuf_t) uioread(dst []uint8) (int, Err_t) {
	ub.proc.Lock_pmap()
	a, b := ub._tx(dst, false)
	ub.proc.Unlock_pmap()
	return a, b
}

func (ub *userbuf_t) uiowrite(src []uint8) (int, Err_t) {
	ub.proc.Lock_pmap()
	a, b := ub._tx(src, true)
	ub.proc.Unlock_pmap()
	return a, b
}

// copies the min of either the provided buffer or ub.len. returns number of
// bytes copied and error.
func (ub *userbuf_t) _tx(buf []uint8, write bool) (int, Err_t) {
	ret := 0
	for len(buf) != 0 && ub.off != ub.len {
		va := ub.userva + ub.off
		ubuf, ok := ub.proc.userdmap8_inner(va, write)
		if !ok {
			return ret, -EFAULT
		}
		end := ub.off + len(ubuf)
		if end > ub.len {
			left := ub.len - ub.off
			ubuf = ubuf[:left]
		}
		var c int
		if write {
			c = copy(ubuf, buf)
		} else {
			c = copy(buf, ubuf)
		}
		buf = buf[c:]
		ub.off += c
		ret += c
	}
	return ret, 0
}
