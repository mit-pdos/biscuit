package main

import "fmt"
import "sync"
import "sync/atomic"
import "sort"
import "time"
import "unsafe"

type ip4_t uint32

// convert little- to big-endian. also, arpv4_t gets padded due to tpa if tpa
// is a uint32 instead of a byte array.
func ip2sl(sl []uint8, ip ip4_t) {
	sl[0] = uint8(ip >> 24)
	sl[1] = uint8(ip >> 16)
	sl[2] = uint8(ip >> 8)
	sl[3] = uint8(ip >> 0)
}

func sl2ip(sl []uint8) ip4_t {
	ret := ip4_t(sl[0]) << 24
	ret |= ip4_t(sl[1]) << 16
	ret |= ip4_t(sl[2]) << 8
	ret |= ip4_t(sl[3]) << 0
	return ret
}

func ip2str(ip ip4_t) string {
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

func (ar *arpv4_t) init_req(smac []uint8, sip, qip ip4_t) {
	if len(smac) != MACLEN {
		panic("bad addr")
	}
	ar._init(smac)
	req := htons(1)
	ar.oper = req
	copy(ar.sha[:], smac)
	ip2sl(ar.spa[:], sip)
	ip2sl(ar.tpa[:], qip)
	var dur mac_t
	copy(ar.tha[:], dur[:])
	for i := range ar.dst {
		ar.dst[i] = 0xff
	}
}

func (ar *arpv4_t) init_reply(smac, dmac []uint8, sip, dip ip4_t) {
	if len(smac) != MACLEN || len(dmac) != MACLEN {
		panic("bad addr")
	}
	ar._init(smac)
	reply := htons(2)
	ar.oper = reply
	copy(ar.sha[:], smac)
	copy(ar.tha[:], dmac)
	ip2sl(ar.spa[:], sip)
	ip2sl(ar.tpa[:], dip)
	copy(ar.dst[:], dmac)
}

func (ar *arpv4_t) bytes() []uint8 {
	return (*[ARPLEN]uint8)(unsafe.Pointer(ar))[:]
}

var arptbl struct {
	sync.Mutex
	m		map[ip4_t]*arprec_t
	enttimeout	time.Duration
	restimeout	time.Duration
	// waiters for arp resolution
	waiters		map[ip4_t][]chan bool
}

type arprec_t struct {
	mac		mac_t
	expire		time.Time
}

func arp_add(ip ip4_t, mac *mac_t) {
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

func _arp_lookup(ip ip4_t) (*arprec_t, bool) {
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
func arp_resolve(sip, dip ip4_t) (*mac_t, int) {
	nic, ok := nics[sip]
	if !ok {
		return nil, -ENETDOWN
	}

	arptbl.Lock()

	ar, ok := _arp_lookup(dip)
	if ok {
		arptbl.Unlock()
		return &ar.mac, 0
	}

	// buffered channel so that a wakeup racing with timeout doesn't
	// eternally block the waker
	mychan := make(chan bool, 1)
	// start a new arp request?
	wl, ok := arptbl.waiters[dip]
	needstart := !ok
	if needstart {
		arptbl.waiters[dip] = []chan bool{mychan}
		_net_arp_start(dip, nic)
	} else {
		arptbl.waiters[dip] = append(wl, mychan)
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
		wl, ok := arptbl.waiters[dip]
		if ok {
			for i := range wl {
				if wl[i] == mychan {
					copy(wl[i:], wl[i+1:])
					wl = wl[:len(wl) - 1]
					break
				}
			}
			if len(wl) == 0 {
				delete(arptbl.waiters, dip)
			} else {
				arptbl.waiters[dip] = wl
			}
		}
		arptbl.Unlock()
		return nil, -ETIMEDOUT
	}

	ar, ok = _arp_lookup(dip)
	if !ok {
		panic("arp must be set")
	}
	arptbl.Unlock()
	return &ar.mac, 0
}

func _net_arp_start(qip ip4_t, nic *x540_t) {
	var arp arpv4_t
	arp.init_req(nic.mac[:], nic.ip, qip)
	buf := arp.bytes()
	sgbuf := [][]uint8{buf}
	nic.tx_raw(sgbuf)
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

	ip := sl2ip(arp.spa[:])
	mac := &arp.sha
	arp_add(ip, mac)
}

type routes_t struct {
	// sorted (ascending order) slice of bit number of least significant
	// bit in each subnet mask
	subnets		[]int
	// map of subnets to the owning IP of the destination ethernet MAC
	routes		map[ip4_t]rtentry_t
	defgw		struct {
		myip	ip4_t
		ip	ip4_t
		valid	bool
	}
}

type rtentry_t struct {
	myip		ip4_t
	gwip		ip4_t
	// netmask shift
	shift		int
	// if gateway is true, ip is the IP of the gateway for this subnet
	gateway		bool
}

func (r *routes_t) init() {
	r.routes = make(map[ip4_t]rtentry_t)
	r.defgw.valid = false
}

func (r *routes_t) defaultgw(myip, gwip ip4_t) {
	r.defgw.myip = myip
	r.defgw.ip = gwip
	r.defgw.valid = true
}

func (r *routes_t) _insert(myip, netip, netmask, gwip ip4_t, isgw bool) {
	if netmask == 0 || netmask == ^ip4_t(0) {
		panic("not a subnet or default gw")
	}
	var bit int
	for bit = 0; bit < 32; bit++ {
		if netmask & (1 << uint(bit)) != 0 {
			break
		}
	}
	found := false
	for _, s := range r.subnets {
		if s == bit {
			found = true
		}
	}
	if !found {
		r.subnets = append(r.subnets, bit)
		sort.Ints(r.subnets)
	}
	nrt := rtentry_t{myip: myip, gwip: gwip, shift: bit, gateway: isgw}
	key := netip >> uint(bit)
	if _, ok := r.routes[key]; ok {
		panic("subnet must be unique")
	}
	r.routes[key] = nrt
}

func (r *routes_t) insert_gateway(myip, netip, netmask, gwip ip4_t) {
	r._insert(myip, netip, netmask, gwip, true)
}

func (r *routes_t) insert_local(myip, netip, netmask ip4_t) {
	r._insert(myip, netip, netmask, 0, false)
}

func (r *routes_t) copy() *routes_t {
	ret := &routes_t{}
	ret.subnets = make([]int, len(r.subnets), cap(r.subnets))
	for i := range r.subnets {
		ret.subnets[i] = r.subnets[i]
	}
	ret.routes = make(map[ip4_t]rtentry_t)
	for a, b := range r.routes {
		ret.routes[a] = b
	}
	ret.defgw = r.defgw
	return ret
}

func (r *routes_t) dump() {
	fmt.Printf("\nRoutes:\n")
	fmt.Printf("  %20s    %16s  %16s\n", "net", "NIC IP", "gateway")
	if r.defgw.valid {
		net := ip2str(0) + "/0"
		mine := ip2str(r.defgw.myip)
		dip := ip2str(r.defgw.ip)
		fmt.Printf("  %20s -> %16s  %16s\n", net, mine, dip)
	}
	for sub, rt := range r.routes {
		s := rt.shift
		net := ip2str(sub << uint(s)) + fmt.Sprintf("/%d", 32 - s)
		mine := ip2str(rt.myip)
		dip := "X"
		if rt.gateway {
			dip = ip2str(rt.gwip)
		}
		fmt.Printf("  %20s -> %16s  %16s\n", net, mine, dip)
	}
}

// returns the local IP assigned to the NIC where the destination is reachable,
// the IP whose MAC address the packet to the destination IP should be sent,
// and error
func (r *routes_t) lookup(dip ip4_t) (ip4_t, ip4_t, int) {
	for _, shift := range r.subnets {
		try := dip >> uint(shift)
		if rtent, ok := r.routes[try]; ok {
			realdest := dip
			if rtent.gateway {
				realdest = rtent.gwip
			}
			return rtent.myip, realdest, 0
		}
	}
	if !r.defgw.valid {
		return 0, 0, -EHOSTUNREACH
	}
	return r.defgw.myip, r.defgw.ip, 0
}

// RCU protected routing table
type routetbl_t struct {
	// lock for RCU writers
	sync.Mutex
	routes	*routes_t
}

func (rt *routetbl_t) init() {
	rt.routes = &routes_t{}
	rt.routes.init()
}

func (rt *routetbl_t) insert_gateway(myip, netip, netmask, gwip ip4_t) {
	rt.Lock()
	defer rt.Unlock()

	newroutes := rt.routes.copy()
	newroutes.insert_gateway(myip, netip, netmask, gwip)
	rt.commit(newroutes)
}

func (rt *routetbl_t) insert_local(myip, netip, netmask ip4_t) {
	rt.Lock()
	defer rt.Unlock()

	newroutes := rt.routes.copy()
	newroutes.insert_local(myip, netip, netmask)
	rt.commit(newroutes)
}

func (rt *routetbl_t) defaultgw(myip, gwip ip4_t) {
	rt.Lock()
	defer rt.Unlock()

	newroutes := rt.routes.copy()
	newroutes.defaultgw(myip, gwip)
	rt.commit(newroutes)
}

func (rt *routetbl_t) commit(newroutes *routes_t) {
	dst := (* unsafe.Pointer)(unsafe.Pointer(&rt.routes))
	v := unsafe.Pointer(newroutes)
	atomic.StorePointer(dst, v)
	// store-release on x86, so long as go compiler doesn't reorder the
	// store with the caller
}

func (rt *routetbl_t) lookup(dip ip4_t) (ip4_t, ip4_t, int) {
	src := (* unsafe.Pointer)(unsafe.Pointer(&rt.routes))
	// load-acquire on x86
	p := atomic.LoadPointer(src)
	troutes := (* routes_t)(p)
	a, b, c := troutes.lookup(dip)
	return a, b, c
}

var routetbl routetbl_t

const IP4LEN = unsafe.Sizeof(ip4hdr_t{})

// no options
type ip4hdr_t struct {
	vers_hdr	uint8
	dscp		uint8
	tlen		uint16
	ident		uint16
	fl_frag		uint16
	ttl		uint8
	proto		uint8
	cksum		uint16
	sip		[4]uint8
	dip		[4]uint8
}

func (i4 *ip4hdr_t) _init(l4len int, sip, dip ip4_t, proto uint8) {
	var z ip4hdr_t
	*i4 = z
	i4.vers_hdr = 0x45
	i4.tlen = htons(uint16(l4len) + uint16(IP4LEN))
	//dontfrag := uint16(1 << 14)
	//i4.fl_frag = htons(dontfrag)
	i4.ttl = 0xff
	i4.proto = proto
	ip2sl(i4.sip[:], sip)
	ip2sl(i4.dip[:], dip)
}

func (i4 *ip4hdr_t) init_icmp(icmplen int, sip, dip ip4_t) {
	icmp := uint8(0x01)
	i4._init(icmplen, sip, dip, icmp)
}

func (i4 *ip4hdr_t) bytes() []uint8 {
	return (*[IP4LEN]uint8)(unsafe.Pointer(i4))[:]
}

const ETHERLEN = unsafe.Sizeof(etherhdr_t{})

type etherhdr_t struct {
	dmac	mac_t
	smac	mac_t
	etype	uint16
}

func (e *etherhdr_t) bytes() []uint8 {
	return (*[ETHERLEN]uint8)(unsafe.Pointer(e))[:]
}

var nics = map[ip4_t]*x540_t{}

// network stack processing begins here
func net_start(pkt [][]uint8, tlen int) {
	if tlen == 0 {
		return
	}

	// header should always be fully contained in the first slice
	buf := pkt[0]
	etype := uint16(readn(buf, 2, 12))
	hlen := len(buf)
	if len(buf) < 14 {
		return
	}
	arp := htons(0x0806)
	if etype == arp && hlen >= int(ARPLEN) {
		arpop := uint16(readn(buf, 2, 20))
		reply := htons(2)
		if etype == arp && arpop == reply {
			_net_arp_finish(buf)
		}
		return
	}
	ip4 := htons(0x0800)
	if etype == ip4 && hlen >= int(34) {
		icmp_reply := uint16(0)
		icmp_type := uint16(readn(buf, 2, 14+20))
		if icmp_reply == icmp_type {
			fromip := sl2ip(buf[14+12:])
			fmt.Printf("** ping reply from %s\n", ip2str(fromip))
		}
		return
	}
}

func netchk() {
	if  ARPLEN != 42 {
		panic("arp bad size")
	}
	if IP4LEN != 20 {
		panic("bad ip4 header size")
	}
	if ETHERLEN != 14 {
		panic("bad ethernet header size")
	}
}

func net_init() {
	netchk()

	arptbl.m = make(map[ip4_t]*arprec_t)
	arptbl.waiters = make(map[ip4_t][]chan bool)
	arptbl.enttimeout = 20*time.Minute
	arptbl.restimeout = 5*time.Second

	routetbl.init()
	//net_test()
}

func net_test() {
	me := ip4_t(0x121a0531)
	netmask := ip4_t(0xfffffe00)
	// 18.26.5.1
	gw := ip4_t(0x121a0401)
	routetbl.defaultgw(me, gw)
	net := me & netmask
	routetbl.insert_local(me, net, netmask)

	net = ip4_t(0x0a000000)
	netmask = ip4_t(0xffffff00)
	gw1 := ip4_t(0x0a000001)
	routetbl.insert_gateway(me, net, netmask, gw1)

	net = ip4_t(0x0a000000)
	netmask = ip4_t(0xffff0000)
	gw2 := ip4_t(0x0a000002)
	routetbl.insert_gateway(me, net, netmask, gw2)

	routetbl.routes.dump()

	dip := ip4_t(0x0a000003)
	a, b, c := routetbl.lookup(dip)
	if c != 0 {
		panic("error")
	}
	if a != me {
		panic("bad local")
	}
	if b != gw1 {
		panic("exp gw1")
	}

	dip = ip4_t(0x0a000103)
	a, b, c = routetbl.lookup(dip)
	if c != 0 {
		panic("error")
	}
	if a != me {
		panic("bad local")
	}
	if b != gw2 {
		fmt.Printf("** %x %x\n", b, gw1)
		fmt.Printf("** %v\n", routetbl.routes.subnets)
		panic("exp gw2")
	}
}
