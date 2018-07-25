package unet

import "bnet"
import "defs"
import "fdops"
import . "inet"
import "mem"
import "res"
import "vm"

type pktmem_t struct {
}

func (pm *pktmem_t) Refpg_new() (*mem.Pg_t, mem.Pa_t, bool) {
	d := &mem.Pg_t{}
	return d, mem.Pa_t(0), true
}

func (pm *pktmem_t) Refpg_new_nozero() (*mem.Pg_t, mem.Pa_t, bool) {
	d := &mem.Pg_t{}
	return d, mem.Pa_t(0), true
}

func (pm *pktmem_t) Refcnt(pa mem.Pa_t) int {
	return 0
}

func (pm *pktmem_t) Dmap(pa mem.Pa_t) *mem.Pg_t {
	return nil
}

func (pm *pktmem_t) Refup(pa mem.Pa_t) {
}

func (pm *pktmem_t) Refdown(pa mem.Pa_t) bool {
	return false
}

func net_init() {
	var mem = &pktmem_t{}
	bnet.Net_init(mem)
	res.Kernel = false
}

func mkUbuf(buf []uint8) *vm.Fakeubuf_t {
	ub := &vm.Fakeubuf_t{}
	ub.Fake_init(buf)
	return ub
}

func mkData(v uint8, n int) *vm.Fakeubuf_t {
	hdata := make([]uint8, n)
	for i := range hdata {
		hdata[i] = v
	}
	ub := &vm.Fakeubuf_t{}
	ub.Fake_init(hdata)
	return ub
}

func mkSaddr(port int, ip int) []uint8 {
	fam := make([]uint8, 2)
	fam[1] = defs.AF_INET
	ipsa := make([]uint8, 4)
	Ip2sl(ipsa, Ip4_t(ip))
	portsa := make([]uint8, 2)
	portsa[0] = uint8(port >> 8)
	portsa[1] = uint8(port >> 0)
	sa := append(fam, portsa...)
	sa = append(sa, ipsa...)
	return sa
}

func unSaddr(sa []uint8) (uint8, Ip4_t) {
	ipsa := sa[4:]
	ip := Sl2ip(ipsa)
	p := sa[2]<<8 | sa[3]
	return p, ip
}

func mkTcpfops() fdops.Fdops_i {
	var opts defs.Fdopt_t
	tcp := &bnet.Tcpfops_t{}
	tcp.Set(&bnet.Tcptcb_t{}, opts)
	return tcp
}
