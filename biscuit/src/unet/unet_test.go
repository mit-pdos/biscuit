package ufs

import "fmt"
import "testing"

import "bnet"
import "defs"
import "fdops"
import . "inet"
import "log"
import "mem"

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

	var opts defs.Fdopt_t

	rcv := &bnet.Tcpfops_t{}
	snd := &bnet.Tcpfops_t{}
	rcv.Set(&bnet.Tcptcb_t{}, opts)
	snd.Set(&bnet.Tcptcb_t{}, opts)

	fam := make([]uint8, 2)
	fam[1] = defs.AF_INET

	lip := Ip4_t(0x7f000001)
	lipsa := make([]uint8, 4)
	Ip2sl(lipsa, lip)

	sportsa := make([]uint8, 2)
	sport := 1090
	sportsa[0] = uint8(sport >> 8)
	sportsa[1] = uint8(sport >> 0)

	fmt.Printf("TestConnet %s\n", Ip2str(lip))

	sa := append(fam, sportsa...)
	sa = append(sa, lipsa...)

	err := rcv.Bind(sa)
	if err != 0 {
		t.Fatalf("Bind %d\n", err)
	}

	ch := make(chan bool)
	go func() {
		for true {
			log.Printf("listen\n")
			ch <- true
			conn, err := rcv.Listen(10)
			if err != 0 {
				t.Fatalf("Listen %d\n", err)
			}
			go func(conn fdops.Fdops_i) {
				log.Printf("close connection\n")
				conn.Close()
			}(conn)
		}
	}()

	<-ch

	log.Printf("call connect\n")

	dportsa := make([]uint8, 2)
	dport := 1090
	dportsa[0] = uint8(dport >> 8)
	dportsa[1] = uint8(dport >> 0)

	sa = append(fam, dportsa...)
	sa = append(sa, lipsa...)

	err = snd.Connect(sa)
	if err != 0 {
		t.Fatalf("connect %d", err)
	}
	err = snd.Close()
	if err != 0 {
		t.Fatalf("close %d", err)
	}
}
