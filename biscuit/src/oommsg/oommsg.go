package oommsg

var OomCh chan Oommsg_t = make(chan Oommsg_t)

type Oommsg_t struct {
	Need   int
	Resume chan bool
}
