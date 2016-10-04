package main

import "fmt"
import "sync"
import "sync/atomic"
import "unsafe"

// convert little- to big-endian. also, arpv4_t gets padded due to tpa if tpa
// is a uint32 instead of a byte array.
func ip2sl(sl *[4]uint8, ip uint32) {
	sl[0] = uint8(ip >> 24)
	sl[1] = uint8(ip >> 16)
	sl[2] = uint8(ip >> 8)
	sl[3] = uint8(ip >> 0)
}

func sl2ip(sl *[4]uint8) uint32 {
	ret := uint32(sl[0]) << 24
	ret |= uint32(sl[1]) << 16
	ret |= uint32(sl[2]) << 8
	ret |= uint32(sl[3]) << 0
	return ret
}

func htons(v uint16) uint16 {
	return v >> 8 | (v & 0xff) << 8
}

const ARPLEN = unsafe.Sizeof(arpv4_t{})

// always big-endian
const MACLEN	int = 6
type mac_t	[MACLEN]uint8

type arpv4_t struct {
	dst	mac_t
	src	mac_t
	etype	uint16
	htype	uint16
	ptype	uint16
	hlen	uint8
	plen	uint8
	oper	uint16
	sha	mac_t
	spa	[4]uint8
	tha	mac_t
	tpa	[4]uint8
}

func (ar *arpv4_t) _init(smac []uint8) {
	arp := htons(0x0806)
	ethernet := htons(1)
	ipv4 := htons(0x0800)
	macsz := uint8(6)
	ipv4sz := uint8(4)
	ar.etype = arp
	ar.htype = ethernet
	ar.ptype = ipv4
	ar.hlen = macsz
	ar.plen = ipv4sz
	copy(ar.src[:], smac)
}

func (ar *arpv4_t) init_req(smac []uint8, sip, qip uint32) {
	if len(smac) != MACLEN {
		panic("bad addr")
	}
	ar._init(smac)
	req := htons(1)
	ar.oper = req
	copy(ar.sha[:], smac)
	ip2sl(&ar.spa, sip)
	ip2sl(&ar.tpa, qip)
	var dur mac_t
	copy(ar.tha[:], dur[:])
	for i := range ar.dst {
		ar.dst[i] = 0xff
	}
}

func (ar *arpv4_t) init_reply(smac, dmac []uint8, sip, dip uint32) {
	if len(smac) != MACLEN || len(dmac) != MACLEN {
		panic("bad addr")
	}
	ar._init(smac)
	reply := htons(2)
	ar.oper = reply
	copy(ar.sha[:], smac)
	copy(ar.tha[:], dmac)
	ip2sl(&ar.spa, sip)
	ip2sl(&ar.tpa, dip)
	copy(ar.dst[:], dmac)
}

func (ar *arpv4_t) bytes() []uint8 {
	return (*[ARPLEN]uint8)(unsafe.Pointer(ar))[:]
}

var arptbl struct {
	// writer RCU lock
	sync.Mutex
	m	map[uint32]mac_t
}

func arp_add(ip uint32, mac *mac_t) {
	arptbl.Lock()
	defer arptbl.Unlock()

	nm := make(map[uint32]mac_t, len(arptbl.m))
	for k, v := range arptbl.m {
		nm[k] = v
	}
	nm[ip] = *mac
	// why is this broken?
	//dst := (*unsafe.Pointer)(unsafe.Pointer(&arptbl.m))
	//atomic.StorePointer(dst, unsafe.Pointer(&nm))
	var dur uint32
	atomic.StoreUint32(&dur, 0)
	arptbl.m = nm
}

func arp_lookup(ip uint32) (mac_t, bool) {
	m := arptbl.m
	a, b := m[ip]
	return a, b
}

func arp_start(ip uint32, smac []uint8, sip uint32, nic *x540_t) {
	var arp arpv4_t
	arp.init_req(smac, sip, ip)
	buf := arp.bytes()
	if !nic.tx_enqueue(buf) {
		panic("no imp")
	}
}

func arp_finish(buf []uint8) {
	if uintptr(len(buf)) < ARPLEN {
		fmt.Printf("short buf\n")
		return
	}
	arp := (*arpv4_t)(unsafe.Pointer(&buf[0]))
	reply := htons(2)
	if arp.oper != reply {
		fmt.Printf("not an arp reply\n")
		return
	}

	ip := sl2ip(&arp.spa)
	mac := &arp.sha
	sip := fmt.Sprintf("%d.%d.%d.%d", ip >> 24, uint8(ip >> 16),
	    uint8(ip >> 8), uint8(ip))
	fmt.Printf("Resolved %s -> %02x\n", sip, mac)
	if omac, ok := arp_lookup(ip); ok {
		fmt.Printf("already have arp entry for %s (%02x)\n", sip, omac)
	} else {
		arp_add(ip, mac)
	}
}

func netchk() {
	if unsafe.Sizeof(arpv4_t{}) != 42 || ARPLEN != 42 {
		panic("arp bad size")
	}
}
