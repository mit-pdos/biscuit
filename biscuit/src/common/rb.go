package common

type Rbc_t int
const (
	RED	Rbc_t = iota
	BLACK	Rbc_t = iota
)

type Rbh_t struct {
	root	*Rbn_t
}

type Rbn_t struct {
	p	*Rbn_t
	r	*Rbn_t
	l	*Rbn_t
	c	Rbc_t
	vmi	Vminfo_t
}
