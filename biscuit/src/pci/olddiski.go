package pci

const BSIZE = 4096

// XXX delete and the disks that use it?
type Idebuf_t struct {
	Disk  int
	Block int
	Data  *[BSIZE]uint8
}

type Disk_i interface {
	Start(*Idebuf_t, bool)
	Complete([]uint8, bool)
	Intr() bool
	Int_clear()
}
