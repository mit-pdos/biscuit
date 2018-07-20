package stat

import "unsafe"

type Stat_t struct {
	_dev    uint
	_ino    uint
	_mode   uint
	_size   uint
	_rdev   uint
	_uid    uint
	_blocks uint
	_m_sec  uint
	_m_nsec uint
}

func (st *Stat_t) Wdev(v uint) {
	st._dev = v
}

func (st *Stat_t) Wino(v uint) {
	st._ino = v
}

func (st *Stat_t) Wmode(v uint) {
	st._mode = v
}

func (st *Stat_t) Wsize(v uint) {
	st._size = v
}

func (st *Stat_t) Wrdev(v uint) {
	st._rdev = v
}

func (st *Stat_t) Mode() uint {
	return st._mode
}

func (st *Stat_t) Size() uint {
	return st._size
}

func (st *Stat_t) Rdev() uint {
	return st._rdev
}

func (st *Stat_t) Rino() uint {
	return st._ino
}

func (st *Stat_t) Bytes() []uint8 {
	const sz = unsafe.Sizeof(*st)
	sl := (*[sz]uint8)(unsafe.Pointer(&st._dev))
	return sl[:]
}
