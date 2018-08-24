package vm

import "fmt"
import "runtime"
import "sync"
import "unsafe"

import "bounds"
import "defs"
import "res"

// a helper object for read/writing from userspace memory. virtual address
// lookups and reads/writes to those addresses must be atomic with respect to
// page faults.
type Userbuf_t struct {
	userva int
	len    int
	// 0 <= off <= len
	off int
	as  *Vm_t
}

func (ub *Userbuf_t) ub_init(as *Vm_t, uva, len int) {
	// XXX fix signedness
	if len < 0 {
		panic("negative length")
	}
	if len >= 1<<39 {
		fmt.Printf("suspiciously large user buffer (%v)\n", len)
	}
	ub.userva = uva
	ub.len = len
	ub.off = 0
	ub.as = as
}

func (ub *Userbuf_t) Remain() int {
	return ub.len - ub.off
}

func (ub *Userbuf_t) Totalsz() int {
	return ub.len
}
func (ub *Userbuf_t) Uioread(dst []uint8) (int, defs.Err_t) {
	ub.as.Lock_pmap()
	a, b := ub._tx(dst, false)
	ub.as.Unlock_pmap()
	return a, b
}

func (ub *Userbuf_t) Uiowrite(src []uint8) (int, defs.Err_t) {
	ub.as.Lock_pmap()
	a, b := ub._tx(src, true)
	ub.as.Unlock_pmap()
	return a, b
}

// copies the min of either the provided buffer or ub.len. returns number of
// bytes copied and error. if an error occurs in the middle of a read or write,
// the userbuf's state is updated such that the operation can be restarted.
func (ub *Userbuf_t) _tx(buf []uint8, write bool) (int, defs.Err_t) {
	ret := 0
	for len(buf) != 0 && ub.off != ub.len {
		if !res.Resadd_noblock(bounds.Bounds(bounds.B_USERBUF_T__TX)) {
			return ret, -defs.ENOHEAP
		}
		va := ub.userva + ub.off
		ubuf, err := ub.as.Userdmap8_inner(va, write)
		if err != 0 {
			return ret, err
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

type _iove_t struct {
	uva uint
	sz  int
}

type Useriovec_t struct {
	iovs []_iove_t
	tsz  int
	as   *Vm_t
}

func (iov *Useriovec_t) Iov_init(as *Vm_t, iovarn uint, niovs int) defs.Err_t {
	if niovs > 10 {
		fmt.Printf("many iovecs\n")
		return -defs.EINVAL
	}
	iov.tsz = 0
	iov.iovs = make([]_iove_t, niovs)
	iov.as = as

	as.Lock_pmap()
	defer as.Unlock_pmap()
	for i := range iov.iovs {
		gimme := bounds.Bounds(bounds.B_USERIOVEC_T_IOV_INIT)
		if !res.Resadd_noblock(gimme) {
			return -defs.ENOHEAP
		}
		elmsz := uint(16)
		va := iovarn + uint(i)*elmsz
		dstva, err := as.userreadn_inner(int(va), 8)
		if err != 0 {
			return err
		}
		sz, err := as.userreadn_inner(int(va)+8, 8)
		if err != 0 {
			return err
		}
		iov.iovs[i].uva = uint(dstva)
		iov.iovs[i].sz = sz
		iov.tsz += sz
	}
	return 0
}

func (iov *Useriovec_t) Remain() int {
	ret := 0
	for i := range iov.iovs {
		ret += iov.iovs[i].sz
	}
	return ret
}

func (iov *Useriovec_t) Totalsz() int {
	return iov.tsz
}

func (iov *Useriovec_t) _tx(buf []uint8, touser bool) (int, defs.Err_t) {
	ub := &Userbuf_t{}
	did := 0
	for len(buf) > 0 && len(iov.iovs) > 0 {
		if !res.Resadd_noblock(bounds.Bounds(bounds.B_USERIOVEC_T__TX)) {
			return did, -defs.ENOHEAP
		}
		ciov := &iov.iovs[0]
		ub.ub_init(iov.as, int(ciov.uva), ciov.sz)
		var c int
		var err defs.Err_t
		if touser {
			c, err = ub._tx(buf, true)
		} else {
			c, err = ub._tx(buf, false)
		}
		ciov.uva += uint(c)
		ciov.sz -= c
		if ciov.sz == 0 {
			iov.iovs = iov.iovs[1:]
		}
		buf = buf[c:]
		did += c
		if err != 0 {
			return did, err
		}
	}
	return did, 0
}

func (iov *Useriovec_t) Uioread(dst []uint8) (int, defs.Err_t) {
	iov.as.Lock_pmap()
	a, b := iov._tx(dst, false)
	iov.as.Unlock_pmap()
	return a, b
}

func (iov *Useriovec_t) Uiowrite(src []uint8) (int, defs.Err_t) {
	iov.as.Lock_pmap()
	a, b := iov._tx(src, true)
	iov.as.Unlock_pmap()
	return a, b
}

// helper type which kernel code can use as userio_i, but is actually a kernel
// buffer (i.e. reading an ELF header from the file system for exec(2)).
type Fakeubuf_t struct {
	fbuf []uint8
	off  int
	len  int
}

func (fb *Fakeubuf_t) Fake_init(buf []uint8) {
	fb.fbuf = buf
	fb.len = len(fb.fbuf)
}

func (fb *Fakeubuf_t) Remain() int {
	return len(fb.fbuf)
}

func (fb *Fakeubuf_t) Totalsz() int {
	return fb.len
}

func (fb *Fakeubuf_t) _tx(buf []uint8, tofbuf bool) (int, defs.Err_t) {
	var c int
	if tofbuf {
		c = copy(fb.fbuf, buf)
	} else {
		c = copy(buf, fb.fbuf)
	}
	fb.fbuf = fb.fbuf[c:]
	return c, 0
}

func (fb *Fakeubuf_t) Uioread(dst []uint8) (int, defs.Err_t) {
	return fb._tx(dst, false)
}

func (fb *Fakeubuf_t) Uiowrite(src []uint8) (int, defs.Err_t) {
	return fb._tx(src, true)
}

var Ubpool = sync.Pool{New: func() interface{} { return new(Userbuf_t) }}

func Mkfxbuf() *[64]uintptr {
	ret := new([64]uintptr)
	n := uintptr(unsafe.Pointer(ret))
	if n&((1<<4)-1) != 0 {
		panic("not 16 byte aligned")
	}
	*ret = runtime.Fxinit
	return ret
}
