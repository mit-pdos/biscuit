package common

import "fmt"

// interface for reading/writing from user space memory either via a pointer
// and length or an array of pointers and lengths (iovec)
type Userio_i interface {
	// copy src to user memory
	Uiowrite(src []uint8) (int, Err_t)
	// copy user memory to dst
	Uioread(dst []uint8) (int, Err_t)
	// returns the number of unwritten/unread bytes remaining
	Remain() int
	// the total buffer size
	Totalsz() int
}

// a helper object for read/writing from userspace memory. virtual address
// lookups and reads/writes to those addresses must be atomic with respect to
// page faults.
type Userbuf_t struct {
	userva int
	len    int
	// 0 <= off <= len
	off  int
	proc *Proc_t
}

func (ub *Userbuf_t) ub_init(p *Proc_t, uva, len int) {
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
	ub.proc = p
}

func (ub *Userbuf_t) Remain() int {
	return ub.len - ub.off
}

func (ub *Userbuf_t) Totalsz() int {
	return ub.len
}
func (ub *Userbuf_t) Uioread(dst []uint8) (int, Err_t) {
	ub.proc.Lock_pmap()
	a, b := ub._tx(dst, false)
	ub.proc.Unlock_pmap()
	return a, b
}

func (ub *Userbuf_t) Uiowrite(src []uint8) (int, Err_t) {
	ub.proc.Lock_pmap()
	a, b := ub._tx(src, true)
	ub.proc.Unlock_pmap()
	return a, b
}

// copies the min of either the provided buffer or ub.len. returns number of
// bytes copied and error. if an error occurs in the middle of a read or write,
// the userbuf's state is updated such that the operation can be restarted.
func (ub *Userbuf_t) _tx(buf []uint8, write bool) (int, Err_t) {
	ret := 0
	for len(buf) != 0 && ub.off != ub.len {
		if !Resadd_noblock(Bounds(B_USERBUF_T__TX)) {
			return ret, -ENOHEAP
		}
		va := ub.userva + ub.off
		ubuf, ok := ub.proc.Userdmap8_inner(va, write)
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

type _iove_t struct {
	uva uint
	sz  int
}

type Useriovec_t struct {
	iovs []_iove_t
	tsz  int
	proc *Proc_t
}

func (iov *Useriovec_t) Iov_init(proc *Proc_t, iovarn uint, niovs int) Err_t {
	if niovs > 10 {
		fmt.Printf("many iovecs\n")
		return -EINVAL
	}
	iov.tsz = 0
	iov.iovs = make([]_iove_t, niovs)
	iov.proc = proc

	proc.Lock_pmap()
	defer proc.Unlock_pmap()
	for i := range iov.iovs {
		gimme := Bounds(B_USERIOVEC_T_IOV_INIT)
		if !Resadd_noblock(gimme) {
			return -ENOHEAP
		}
		elmsz := uint(16)
		va := iovarn + uint(i)*elmsz
		dstva, ok1 := proc.userreadn_inner(int(va), 8)
		sz, ok2 := proc.userreadn_inner(int(va)+8, 8)
		if !ok1 || !ok2 {
			return -EFAULT
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

func (iov *Useriovec_t) _tx(buf []uint8, touser bool) (int, Err_t) {
	ub := &Userbuf_t{}
	did := 0
	for len(buf) > 0 && len(iov.iovs) > 0 {
		if !Resadd_noblock(Bounds(B_USERIOVEC_T__TX)) {
			return did, -ENOHEAP
		}
		ciov := &iov.iovs[0]
		ub.ub_init(iov.proc, int(ciov.uva), ciov.sz)
		var c int
		var err Err_t
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

func (iov *Useriovec_t) Uioread(dst []uint8) (int, Err_t) {
	iov.proc.Lock_pmap()
	a, b := iov._tx(dst, false)
	iov.proc.Unlock_pmap()
	return a, b
}

func (iov *Useriovec_t) Uiowrite(src []uint8) (int, Err_t) {
	iov.proc.Lock_pmap()
	a, b := iov._tx(src, true)
	iov.proc.Unlock_pmap()
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

func (fb *Fakeubuf_t) _tx(buf []uint8, tofbuf bool) (int, Err_t) {
	var c int
	if tofbuf {
		c = copy(fb.fbuf, buf)
	} else {
		c = copy(buf, fb.fbuf)
	}
	fb.fbuf = fb.fbuf[c:]
	return c, 0
}

func (fb *Fakeubuf_t) Uioread(dst []uint8) (int, Err_t) {
	return fb._tx(dst, false)
}

func (fb *Fakeubuf_t) Uiowrite(src []uint8) (int, Err_t) {
	return fb._tx(src, true)
}
