package main

import "fmt"
import "sync"
import "time"
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

func ip2str(ip uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", ip >> 24, uint8(ip >> 16),
	    uint8(ip >> 8), uint8(ip))
}

func mac2str(m []uint8) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2],
	    m[3], m[4], m[5])
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
	sync.Mutex
	m		map[uint32]*arprec_t
	enttimeout	time.Duration
	restimeout	time.Duration
	// waiters for arp resolution
	waiters		map[uint32][]chan bool
}

type arprec_t struct {
	mac		mac_t
	expire		time.Time
}

func arp_add(ip uint32, mac *mac_t) {
	arptbl.Lock()
	defer arptbl.Unlock()

	now := time.Now()
	for k, v := range arptbl.m {
		if v.expire.Before(now) {
			delete(arptbl.m, k)
		}
	}
	nr := &arprec_t{}
	nr.expire = time.Now().Add(arptbl.enttimeout)
	copy(nr.mac[:], mac[:])
	// don't replace entries in order to mitigate arp spoofing
	if _, ok := arptbl.m[ip]; !ok {
		arptbl.m[ip] = nr
	}

	if wl, ok := arptbl.waiters[ip]; ok {
		delete(arptbl.waiters, ip)
		for i := range wl {
			wl[i] <- true
		}
	}
}

func _arp_lookup(ip uint32) (*arprec_t, bool) {
	ar, ok := arptbl.m[ip]
	if !ok {
		return nil, false
	}
	if ar.expire.Before(time.Now()) {
		delete(arptbl.m, ip)
		return nil, false
	}
	return ar, ok
}

// returns false if resolution timed out
func arp_resolve(ip uint32) (*mac_t, bool) {
	if nic == nil {
		panic("nil nic")
	}

	arptbl.Lock()

	ar, ok := _arp_lookup(ip)
	if ok {
		// found existing entry. update expire.
		ar.expire = time.Now().Add(arptbl.enttimeout)
		arptbl.Unlock()
		return &ar.mac, true
	}

	// buffered channel so that a wakeup racing with timeout doesn't
	// eternally block the waker
	mychan := make(chan bool, 1)
	// start a new arp request?
	wl, ok := arptbl.waiters[ip]
	needstart := !ok
	if needstart {
		arptbl.waiters[ip] = []chan bool{mychan}
		// 18.26.5.49 (bhw)
		durip := uint32(0x121a0531)
		_net_arp_start(ip, nic.mac[:], durip, nic)
	} else {
		arptbl.waiters[ip] = append(wl, mychan)
	}
	arptbl.Unlock()

	var timeout bool
	select {
	case <-mychan:
		timeout = false
	case <-time.After(arptbl.restimeout):
		timeout = true
	}

	arptbl.Lock()

	if timeout {
		// remove my channel from waiters
		wl, ok := arptbl.waiters[ip]
		if ok {
			for i := range wl {
				if wl[i] == mychan {
					copy(wl[i:], wl[i+1:])
					wl = wl[:len(wl) - 1]
					break
				}
			}
			if len(wl) == 0 {
				delete(arptbl.waiters, ip)
			} else {
				arptbl.waiters[ip] = wl
			}
		}
		arptbl.Unlock()
		return nil, false
	}

	ar, ok = _arp_lookup(ip)
	if !ok {
		panic("arp must be set")
	}
	arptbl.Unlock()
	return &ar.mac, true
}

func _net_arp_start(ip uint32, smac []uint8, sip uint32, nic *x540_t) {
	var arp arpv4_t
	arp.init_req(smac, sip, ip)
	buf := arp.bytes()
	nic.tx_wait(buf)
}

func _net_arp_finish(buf []uint8) {
	if uintptr(len(buf)) < ARPLEN {
		panic("short buf")
	}
	arp := (*arpv4_t)(unsafe.Pointer(&buf[0]))
	reply := htons(2)
	if arp.oper != reply {
		panic("not an arp reply")
	}

	ip := sl2ip(&arp.spa)
	mac := &arp.sha
	arp_add(ip, mac)
}

var nic *x540_t

// network stack processing begins here
func net_start(pkt [][]uint8, tlen int) {
	if tlen == 0 {
		return
	}

	// header should always be fully contained in the first slice
	buf := pkt[0]
	if uintptr(len(buf)) >= ARPLEN {
		arp := htons(0x0806)
		etype := uint16(readn(buf, 2, 12))
		arpop := uint16(readn(buf, 2, 20))
		reply := htons(2)
		if etype == arp && arpop == reply {
			_net_arp_finish(buf)
		}
	}
}

func netchk() {
	if unsafe.Sizeof(arpv4_t{}) != 42 || ARPLEN != 42 {
		panic("arp bad size")
	}
}

func net_init() {
	netchk()

	arptbl.m = make(map[uint32]*arprec_t)
	arptbl.waiters = make(map[uint32][]chan bool)
	arptbl.enttimeout = 20*time.Minute
	arptbl.restimeout = 5*time.Second
}
