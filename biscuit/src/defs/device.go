package defs

const (
	D_CONSOLE int = 1
	// UNIX domain sockets
	D_SUD     = 2
	D_SUS     = 3
	D_DEVNULL = 4
	D_RAWDISK = 5
	D_STAT    = 6
	D_PROF    = 7
	D_FIRST   = D_CONSOLE
	D_LAST    = D_SUS
)

func Mkdev(_maj, _min int) uint {
	maj := uint(_maj)
	min := uint(_min)
	if min > 0xff {
		panic("bad minor")
	}
	m := maj<<8 | min
	return uint(m << 32)
}

func Unmkdev(d uint) (int, int) {
	return int(d >> 40), int(uint8(d >> 32))
}
