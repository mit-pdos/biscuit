package ufs

import "fmt"
import "testing"

import "bnet"
import "defs"
import "fdops"
import . "inet"
import "log"
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

func TestConnect(t *testing.T) {

	var mem = &pktmem_t{}

	bnet.Net_init(mem)
	res.Kernel = false
	var opts defs.Fdopt_t

	rcv := &bnet.Tcpfops_t{}
	snd := &bnet.Tcpfops_t{}
	rcv.Set(&bnet.Tcptcb_t{}, opts)
	snd.Set(&bnet.Tcptcb_t{}, opts)

	fam := make([]uint8, 2)
	fam[1] = defs.AF_INET

	inip := Ip4_t(defs.INADDR_ANY)
	inipsa := make([]uint8, 4)
	Ip2sl(inipsa, inip)

	sportsa := make([]uint8, 2)
	sport := 1090
	sportsa[0] = uint8(sport >> 8)
	sportsa[1] = uint8(sport >> 0)

	fmt.Printf("TestConnet\n")

	sa := append(fam, sportsa...)
	sa = append(sa, inipsa...)

	err := rcv.Bind(sa)
	if err != 0 {
		t.Fatalf("Bind %d\n", err)
	}

	conn, err := rcv.Listen(10)
	if err != 0 {
		t.Fatalf("Listen %d\n", err)
	}

	ch := make(chan bool)
	go func(conn fdops.Fdops_i, t *testing.T) {
		log.Printf("wait for accept\n")
		for true {
			hdata := make([]uint8, 8)
			ub := &vm.Fakeubuf_t{}
			ub.Fake_init(hdata)

			clnt, _, err := conn.Accept(ub)
			if err != 0 {
				t.Fatalf("accept")
			}
			ipsa := hdata[4:]
			ip := Sl2ip(ipsa)
			p := hdata[2]<<8 | hdata[3]
			log.Printf("accepted clnt (%s,%d)\n", Ip2str(ip), p)
			ch <- true
			clnt.Close()

		}
	}(conn, t)

	log.Printf("call connect %d\n", 1090)

	dip := Ip4_t(0x7f000001)
	dipsa := make([]uint8, 4)
	Ip2sl(dipsa, dip)
	dportsa := make([]uint8, 2)
	dport := 1090
	dportsa[0] = uint8(dport >> 8)
	dportsa[1] = uint8(dport >> 0)

	sa = append(fam, dportsa...)
	sa = append(sa, dipsa...)

	err = snd.Connect(sa)
	if err != 0 {
		t.Fatalf("connect %d", err)
	}

	<-ch

	err = snd.Close()
	if err != 0 {
		t.Fatalf("close %d", err)
	}
}
