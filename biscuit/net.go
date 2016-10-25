package main

import "fmt"
import "math/rand"
import "sync"
import "sync/atomic"
import "sort"
import "time"
import "unsafe"

type ip4_t uint32

type be16 uint16
type be32 uint32

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

func htons(v uint16) be16 {
	return be16(v >> 8 | (v & 0xff) << 8)
}

func htonl(v uint32) be32 {
	r := (v & 0x000000ff) << 24
	r |= (v & 0x0000ff00) << 8
	r |= (v & 0x00ff0000) >> 8
	r |= v >> 24
	return be32(r)
}

func ntohs(v be16) uint16 {
	return uint16(v >> 8 | (v & 0xff) << 8)
}

func ntohl(v be32) uint32 {
	r := (v & 0x000000ff) << 24
	r |= (v & 0x0000ff00) << 8
	r |= (v & 0x00ff0000) >> 8
	r |= v >> 24
	return uint32(r)
}

const ARPLEN = int(unsafe.Sizeof(arpv4_t{}))

// always big-endian
const macsz	int = 6
type mac_t	[macsz]uint8

type arpv4_t struct {
	etherhdr_t
	htype	be16
	ptype	be16
	hlen	uint8
	plen	uint8
	oper	be16
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
	copy(ar.smac[:], smac)
}

func (ar *arpv4_t) init_req(smac []uint8, sip, qip ip4_t) {
	if len(smac) != macsz {
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
	for i := range ar.dmac {
		ar.dmac[i] = 0xff
	}
}

func (ar *arpv4_t) init_reply(smac, dmac []uint8, sip, dip ip4_t) {
	if len(smac) != macsz || len(dmac) != macsz {
		panic("bad addr")
	}
	ar._init(smac)
	reply := htons(2)
	ar.oper = reply
	copy(ar.sha[:], smac)
	copy(ar.tha[:], dmac)
	ip2sl(ar.spa[:], sip)
	ip2sl(ar.tpa[:], dip)
	copy(ar.dmac[:], dmac)
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

// the Rx path can try to immediately queue an ARP response for Tx since no ARP
// resolution is necessary (which may block).
func net_arp(pkt [][]uint8, tlen int) {
	buf := pkt[0]
	hlen := len(buf)

	if hlen < ARPLEN {
		return
	}
	buf = buf[ETHERLEN:]
	// is it for us?
	toip := sl2ip(buf[24:])
	nic, ok := nics[toip]
	if !ok {
		return
	}
	fromip := sl2ip(buf[14:])
	var frommac mac_t
	copy(frommac[:], buf[8:])

	arpop := be16(readn(buf, 2, 6))
	request := htons(1)
	reply := htons(2)
	switch arpop {
	case reply:
		arp_add(fromip, &frommac)
	case request:
		// add the sender to our arp table
		arp_add(fromip, &frommac)
		var rep arpv4_t
		rep.init_reply(nic.mac[:], frommac[:], nic.ip, fromip)
		sgbuf := [][]uint8{rep.bytes()}
		nic.tx_raw(sgbuf)
	}
}

type routes_t struct {
	// sorted (ascending order) slice of bit number of least significant
	// bit in each subnet mask
	subnets		[]int
	// map of subnets to the owning IP of the destination ethernet MAC
	routes		map[uint64]rtentry_t
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
	// if gateway is true, gwip is the IP of the gateway for this subnet
	gateway		bool
}

func (r *routes_t) init() {
	r.routes = make(map[uint64]rtentry_t)
	r.defgw.valid = false
}

func (r *routes_t) defaultgw(myip, gwip ip4_t) {
	r.defgw.myip = myip
	r.defgw.ip = gwip
	r.defgw.valid = true
}

func (r *routes_t) _insert(myip, netip, netmask, gwip ip4_t, isgw bool) {
	if netmask == 0 || netmask == ^ip4_t(0) || netip & netmask == 0 {
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
	key := uint64(netip >> uint(bit))
	key |= uint64(netmask) << 32
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
	ret.routes = make(map[uint64]rtentry_t)
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
		net := ip2str(ip4_t(sub) << uint(s)) +
		    fmt.Sprintf("/%d", 32 - s)
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
		s := uint(shift)
		try := uint64(dip >> s)
		try |= ^uint64((1 << (s + 32)) - 1)
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

const IP4LEN = int(unsafe.Sizeof(ip4hdr_t{}))

// no options
type ip4hdr_t struct {
	vers_hdr	uint8
	dscp		uint8
	tlen		be16
	ident		be16
	fl_frag		be16
	ttl		uint8
	proto		uint8
	cksum		be16
	sip		[4]uint8
	dip		[4]uint8
}

func (i4 *ip4hdr_t) _init(l4len int, sip, dip ip4_t, proto uint8) {
	var z ip4hdr_t
	*i4 = z
	i4.vers_hdr = 0x45
	i4.tlen = htons(uint16(l4len) + uint16(IP4LEN))
	// may want to toggle don't frag, specifically in TCP after not
	// receiving acks
	dontfrag := uint16(1 << 14)
	i4.fl_frag = htons(dontfrag)
	i4.ttl = 0xff
	i4.proto = proto
	ip2sl(i4.sip[:], sip)
	ip2sl(i4.dip[:], dip)
}

func (i4 *ip4hdr_t) init_icmp(icmplen int, sip, dip ip4_t) {
	icmp := uint8(0x01)
	i4._init(icmplen, sip, dip, icmp)
}

func (i4 *ip4hdr_t) init_tcp(tcplen int, sip, dip ip4_t) {
	tcp := uint8(0x06)
	i4._init(tcplen, sip, dip, tcp)
}

func (i4 *ip4hdr_t) bytes() []uint8 {
	return (*[IP4LEN]uint8)(unsafe.Pointer(i4))[:]
}

func sl2iphdr(buf []uint8) (*ip4hdr_t, []uint8, bool) {
	if len(buf) < IP4LEN {
		return nil, nil, false
	}
	p := (*ip4hdr_t)(unsafe.Pointer(&buf[0]))
	rest := buf[IP4LEN:]
	return p, rest, true
}

const ETHERLEN = int(unsafe.Sizeof(etherhdr_t{}))

type etherhdr_t struct {
	dmac	mac_t
	smac	mac_t
	etype	be16
}

func (et *etherhdr_t) _init(smac, dmac *mac_t, etype uint16) {
	copy(et.smac[:], smac[:])
	copy(et.dmac[:], dmac[:])
	et.etype = htons(etype)
}

func (et *etherhdr_t) init_ip4(smac, dmac *mac_t) {
	etype := uint16(0x0800)
	et._init(smac, dmac, etype)
}

func (e *etherhdr_t) bytes() []uint8 {
	return (*[ETHERLEN]uint8)(unsafe.Pointer(e))[:]
}

type icmppkt_t struct {
	ether	etherhdr_t
	iphdr	ip4hdr_t
	typ	uint8
	code	uint8
	// cksum is endian agnostic, so long as the used endianness is
	// consistent throughout the checksum computation
	cksum	uint16
	ident	be16
	seq	be16
	data	[]uint8
}

func (ic *icmppkt_t) init(smac, dmac *mac_t, sip, dip ip4_t, typ uint8,
    data []uint8) {
	var z icmppkt_t
	*ic = z
	l4len := len(data) + 2*1 + 3*2
	ic.ether.init_ip4(smac, dmac)
	ic.iphdr.init_icmp(l4len, sip, dip)
	ic.typ = typ
	ic.data = data
}

func (ic *icmppkt_t) crc() {
	sum := uint32(ic.typ) + uint32(ic.code) << 8
	sum += uint32(ic.cksum)
	sum += uint32(ic.ident)
	sum += uint32(ic.seq)
	buf := ic.data
	for len(buf) > 1 {
		sum += uint32(buf[0]) + uint32(buf[1]) << 8
		buf = buf[2:]
	}
	if len(buf) == 1 {
		sum += uint32(buf[0])
	}
	lm := uint32(^uint16(0))
	for (sum &^ 0xffff) != 0 {
		sum = (sum & lm) + (sum >> 16)
	}
	sum = ^sum
	ret := uint16(sum)
	ic.cksum = ret
}

func (ic *icmppkt_t) hdrbytes() []uint8 {
	const hdrsz = ETHERLEN + IP4LEN + 2*1 + 3*2
	return (*[hdrsz]uint8)(unsafe.Pointer(ic))[:]
}

// i don't want our packet-processing event-loop to block on ARP resolution (or
// anything that may take seconds), so i use a different goroutine to
// resolve/transmit to an address.
var icmp_echos = make(chan []uint8, 30)

func icmp_daemon() {
	for {
		buf := <- icmp_echos
		buf = buf[ETHERLEN:]

		fromip := sl2ip(buf[12:])
		//fmt.Printf("** GOT PING from %s\n", ip2str(fromip))

		localip, routeip, err := routetbl.lookup(fromip)
		if err != 0 {
			panic("routing failed")
		}
		nic, ok := nics[localip]
		if !ok {
			panic("no such nic")
		}
		dmac, err := arp_resolve(localip, routeip)
		if err != 0 {
			panic("arp failed for ICMP echo")
		}

		ident := be16(readn(buf, 2, IP4LEN + 4))
		seq := be16(readn(buf, 2, IP4LEN + 6))
		origdata := buf[IP4LEN + 8:]
		echodata := make([]uint8, len(origdata))
		copy(echodata, origdata)
		var reply icmppkt_t
		reply.init(&nic.mac, dmac, nic.ip, fromip, 0, echodata)
		reply.ident = ident
		reply.seq = seq
		reply.crc()
		txpkt := [][]uint8{reply.hdrbytes(), echodata}
		nic.tx_ipv4(txpkt)
	}
}

func net_icmp(pkt [][]uint8, tlen int) {
	buf := pkt[0]
	buf = buf[ETHERLEN:]

	// min ICMP header length is 8
	if len(buf) < IP4LEN + 8 {
		return
	}
	icmp_reply := uint8(0)
	icmp_echo := uint8(8)

	// for us?
	toip := sl2ip(buf[16:])
	if _, ok := nics[toip]; !ok {
		return
	}

	fromip := sl2ip(buf[12:])
	icmp_type := buf[IP4LEN]
	switch icmp_type {
	case icmp_reply:
		when := int64(readn(buf, 8, IP4LEN + 8))
		elap := float64(time.Now().UnixNano() - when)
		elap /= 1000
		fmt.Printf("** ping reply from %s took %v us\n",
		    ip2str(fromip), elap)
	case icmp_echo:
		// copy out of DMA buffer
		data := make([]uint8, tlen)
		tmp := data
		c := 0
		for i := range pkt {
			src := pkt[i]
			did := copy(tmp, src)
			tmp = tmp[did:]
			c += did
		}
		if c != tlen {
			panic("total len mismatch")
		}
		select {
		case icmp_echos <- data:
		default:
			fmt.Printf("dropped ICMP echo\n")
		}
	}
}

type tcpbuf_t struct {
	cbuf	circbuf_t
	seq	uint32
	didseq	bool
}

func (tb *tcpbuf_t) tbuf_init(sz int) {
	tb.cbuf.cb_init(sz)
	tb.didseq = false
}

func (tb *tcpbuf_t) set_seq(seq uint32) {
	if tb.didseq {
		panic("seq already set")
	}
	tb.seq = seq
	tb.didseq = true
}

func (tb *tcpbuf_t) _sanity() {
	if !tb.didseq {
		panic("no sequence")
	}
}

// allows user to read written data (if next in the sequence) and returns
// number of bytes written. cannot fail.
func (tb *tcpbuf_t) syswrite(nseq uint32, rxdata [][]uint8) int {
	tb._sanity()
	if !_seqbetween(tb.seq, nseq, tb.seq + uint32(tb.cbuf.left())) {
		panic("bad sequence number")
	}
	var dlen int
	for _, r := range rxdata {
		dlen += len(r)
	}
	if dlen >= tb.cbuf.left() {
		panic("data out of window")
	}
	off := _seqdiff(nseq, tb.seq)
	var did int
	for _, r := range rxdata {
		dst1, dst2 := tb.cbuf._rawwrite(off + did, len(r))
		did1 := copy(dst1, r)
		did2 := copy(dst2, r[did1:])
		did += did1 + did2
	}
	// XXXPANIC
	if did != dlen {
		panic("nyet")
	}
	if nseq == tb.seq {
		tb.seq += uint32(did)
		tb.cbuf._advhead(did)
	}
	return did
}

type tcphdr_t struct {
	sport		be16
	dport		be16
	seq		be32
	ack		be32
	dataoff		uint8
	flags		uint8
	win		be16
	cksum		be16
	urg		be16
}

const TCPLEN = int(unsafe.Sizeof(tcphdr_t{}))

type tcpopt_t struct {
	wshift	uint
	tsval	uint32
	tsecr	uint32
	mss	uint16
	sackok	bool
}

func _sl2tcpopt(buf []uint8) tcpopt_t {
	// len = 1
	oend := uint8(0)
	// len = 1
	onop := uint8(1)
	// len = 4
	omss := uint8(2)
	// len = 3
	owsopt := uint8(3)
	// len = 2
	osackok := uint8(4)
	// len = >2
	osacks := uint8(5)
	// len = 10
	otsopt := uint8(8)

	var ret tcpopt_t
outer:
	for len(buf) != 0 {
		switch buf[0] {
		case oend:
			break outer
		case onop:
			buf = buf[1:]
		case omss:
			if len(buf) < 4 {
				break outer
			}
			ret.mss = ntohs(be16(readn(buf, 2, 2)))
			buf = buf[4:]
		case owsopt:
			if len(buf) < 3 {
				break outer
			}
			ret.wshift = uint(buf[2])
			buf = buf[3:]
		case osackok:
			ret.sackok = true
			buf = buf[2:]
		case osacks:
			l := int(buf[1])
			if len(buf) < 2 || len(buf) < l {
				break outer
			}
			fmt.Printf("got SACKS (%d)\n", buf[1])
			buf = buf[l:]
		case otsopt:
			if len(buf) < 10 {
				break outer
			}
			ret.tsval = ntohl(be32(readn(buf, 4, 2)))
			ret.tsecr = ntohl(be32(readn(buf, 4, 6)))
			buf = buf[10:]
		}
	}
	return ret
}

func sl2tcphdr(buf []uint8) (*tcphdr_t, tcpopt_t, []uint8, bool) {
	if len(buf) < TCPLEN {
		var z tcpopt_t
		return nil, z, nil, false
	}
	p := (*tcphdr_t)(unsafe.Pointer(&buf[0]))
	rest := buf[TCPLEN:]
	var opt tcpopt_t
	doff := int(p.dataoff >> 4)*4
	if doff > TCPLEN && doff <= len(buf) {
		opt = _sl2tcpopt(buf[TCPLEN: doff])
		rest = buf[doff:]
	}
	return p, opt, rest, true
}

func (t *tcphdr_t) _init(sport, dport uint16, seq, ack uint32) {
	var z tcphdr_t
	*t = z
	t.sport = htons(sport)
	t.dport = htons(dport)
	t.seq = htonl(seq)
	t.ack = htonl(ack)
	t.dataoff = 0x50
}

func (t *tcphdr_t) init_syn(sport, dport uint16, seq uint32) {
	t._init(sport, dport, seq, 0)
	synf := uint8(1 << 1)
	t.flags |= synf
}

func (t *tcphdr_t) init_synack(sport, dport uint16, seq, ack uint32) {
	t._init(sport, dport, seq, ack)
	synf := uint8(1 << 1)
	ackf := uint8(1 << 4)
	t.flags |= synf | ackf
}

func (t *tcphdr_t) init_ack(sport, dport uint16, seq, ack uint32) {
	t._init(sport, dport, seq, ack)
	ackf := uint8(1 << 4)
	t.flags |= ackf
}

func (t *tcphdr_t) set_opt(opt []uint8) {
	if len(opt) % 4 != 0 {
		panic("options must be 32bit aligned")
	}
	words := len(opt) / 4
	t.dataoff = uint8(TCPLEN/4 + words) << 4
}

func (t *tcphdr_t) hdrlen() int {
	return int(t.dataoff >> 4) * 4
}

func (t *tcphdr_t) issyn() bool {
	synf := uint8(1 << 1)
	return t.flags & synf != 0
}

func (t *tcphdr_t) isack() (uint32, bool) {
	ackf := uint8(1 << 4)
	return ntohl(t.ack), t.flags & ackf != 0
}

func (t *tcphdr_t) isrst() bool {
	rst := uint8(1 << 2)
	return t.flags & rst != 0
}

func (t *tcphdr_t) isfin() bool {
	fin := uint8(1 << 0)
	return t.flags & fin != 0
}

func (t *tcphdr_t) ispush() bool {
	pu := uint8(1 << 3)
	return t.flags & pu != 0
}

func (t *tcphdr_t) bytes() []uint8 {
	return (*[TCPLEN]uint8)(unsafe.Pointer(t))[:]
}

func (t *tcphdr_t) dump(sip, dip ip4_t, opt tcpopt_t) {
	s := fmt.Sprintf("%s:%d -> %s:%d", ip2str(sip), ntohs(t.sport),
	    ip2str(dip), ntohs(t.dport))
	if t.issyn() {
		s += fmt.Sprintf(", S")
	}
	s += fmt.Sprintf(" [%v]", ntohl(t.seq))
	if ack, ok := t.isack(); ok {
		s += fmt.Sprintf(", A [%v]", ack)
	}
	if t.isrst() {
		s += fmt.Sprintf(", R")
	}
	if t.isfin() {
		s += fmt.Sprintf(", F")
	}
	if t.ispush() {
		s += fmt.Sprintf(", P")
	}
	s += fmt.Sprintf(", win=%v", ntohs(t.win))
	if opt.sackok {
		s += fmt.Sprintf(", SACKok")
	}
	if opt.wshift != 0 {
		s += fmt.Sprintf(", wshift=%v", opt.wshift)
	}
	if opt.tsval != 0 {
		s += fmt.Sprintf(", timestamp=%v", opt.tsval)
	}
	if opt.mss != 0 {
		s += fmt.Sprintf(", MSS=%v", opt.mss)
	}
	fmt.Printf("%s\n", s)
}

type tcptcb_t struct {
	l	sync.Mutex
	locked	bool
	// local/remote ip/ports
	lip	ip4_t
	rip	ip4_t
	lport	uint16
	rport	uint16
	smac	*mac_t
	dmac	*mac_t
	state	tcpstate_t
	// dead must only be true after both sides have acknowledged all data
	// (i.e. on receiving reset or after LASTACK/TIMEWAIT)
	dead	bool
	rcv struct {
		nxt	uint32
		win	uint16
		mss	uint16
	}
	snd struct {
		nxt	uint32
		una	uint32
		win	uint16
		mss	uint16
		wl1	uint32
		wl2	uint32
	}
	// remembered stuff to send
	rem struct {
		last		time.Time
		// number of segments received; we try to send 1 ACK per 2 data
		// segments.
		num		uint
		forcedelay	bool
		// outstanding ACK?
		outa		bool
		tstart		bool
	}
	// data to send over the TCP connection
	txbuf	tcpbuf_t
	// data received over the TCP connection
	rxbuf	tcpbuf_t
}

type tcpstate_t uint

const (
	// the following is for newly created tcbs
	INVALID		tcpstate_t = iota
	SYNSENT
	SYNRCVD
	LISTEN
	ESTAB
	FINWAIT1
	FINWAIT2
	CLOSING
	CLOSEWAIT
	LASTACK
	TIMEWAIT
)

var statestr = map[tcpstate_t]string {
	SYNSENT: "SYNSENT",
	SYNRCVD: "SYNRCVD",
	LISTEN: "LISTEN",
	ESTAB: "ESTAB",
	FINWAIT1: "FINWAIT1",
	FINWAIT2: "FINWAIT2",
	CLOSING: "CLOSING",
	CLOSEWAIT: "CLOSEWAIT",
	LASTACK: "LASTACK",
	TIMEWAIT: "TIMEWAIT",
	INVALID: "INVALID",
}

func (tc *tcptcb_t) tcb_lock() {
	tc.l.Lock()
	tc.locked = true
}

func (tc *tcptcb_t) tcb_unlock() {
	tc.locked = false
	tc.l.Unlock()
}

func (tc *tcptcb_t) _sanity() {
	if !tc.locked {
		panic("tcb must be locked")
	}
}

func (tc *tcptcb_t) _nstate(old, news tcpstate_t) {
	tc._sanity()
	if tc.state != old {
		panic("bad state transition")
	}
	tc.state = news
}

func (tc *tcptcb_t) _rst() {
	fmt.Printf("tcb reset no imp")
}

func (tc *tcptcb_t) incoming(tk tcpkey_t, ip4 *ip4hdr_t, tcp *tcphdr_t,
    opt tcpopt_t, rest [][]uint8) {
	tc._sanity()
	switch tc.state {
		case SYNSENT:
			tc.synsent(ip4, tcp, opt.mss, rest)
		case ESTAB:
			tc.estab(ip4, tcp, rest)
		default:
			sn, ok := statestr[tc.state]
			if !ok {
				panic("huh?")
			}
			fmt.Printf("no imp for state %s\n", sn)
			return
	}

	tc.ack_maybe()

	if tc.dead {
		tcpcons.tcb_del(tk)
		return
	}
}

// sets flag to send an ack which may be delayed.
func (tc *tcptcb_t) sched_ack() {
	tc.rem.num++
	tc.rem.outa = true
}

func (tc *tcptcb_t) sched_ack_delay() {
	tc.rem.outa = true
	tc.rem.forcedelay = true
}

func (tc *tcptcb_t) ack_maybe() {
	tc._acktime(false)
}

func (tc *tcptcb_t) ack_now() {
	tc._acktime(true)
}

// sendnow overrides the forcedelay flag
func (tc *tcptcb_t) _acktime(sendnow bool) {
	tc._sanity()
	if !tc.rem.outa {
		return
	}
	fdelay := tc.rem.forcedelay
	tc.rem.forcedelay = false

	segdelay := tc.rem.num % 2 == 0
	canwait := !sendnow && (segdelay || fdelay)
	if canwait {
		// delay at most 500ms
		now := time.Now()
		deadline := tc.rem.last.Add(500*time.Millisecond)
		if now.Before(deadline) {
			if !tc.rem.tstart {
				// XXX goroutine per timeout bad?
				tc.rem.tstart = true
				left := deadline.Sub(now)
				go func() {
					time.Sleep(left)
					tc.tcb_lock()
					tc.rem.tstart = false
					tc.ack_maybe()
					tc.tcb_unlock()
				}()
			}
			return
		}
	}
	tc.rem.outa = false

	pkt := tc.mkack(tc.snd.nxt, tc.rcv.nxt)
	nic, ok := nics[tc.lip]
	if !ok {
		fmt.Printf("NIC gone!\n")
		tc.dead = true
		return
	}
	eth, ip, th := pkt.hdrbytes()
	sgbuf := [][]uint8{eth, ip, th}
	nic.tx_tcp(sgbuf)
	tc.rem.last = time.Now()
}

// returns TCP header and TCP option slice
func (tc *tcptcb_t) mkconnect(seq uint32) (*tcppkt_t, []uint8) {
	ret := &tcppkt_t{}
	ret.tcphdr.init_syn(tc.lport, tc.rport, seq)
	ret.tcphdr.win = htons(tc.rcv.win)
	// which TCP options are necessary? linux and openbsd SYN/ACK even if
	// we send no options.
	opt := []uint8{
	    // mss = 1460
	    2, 4, 0x5, 0xb4,
	    // sackok
	    //4, 2, 1, 1,
	    // wscale = 3
	    //3, 3, 3, 1,
	    // timestamp
	    //8, 10, 0, 0, 0xff, 0xff, 0, 0, 0, 0,
	    // timestamp pad
	    //1, 1,
	}
	ret.tcphdr.set_opt(opt)
	l4len := ret.tcphdr.hdrlen()
	ret.iphdr.init_tcp(l4len, tc.lip, tc.rip)
	ret.ether.init_ip4(tc.smac, tc.dmac)
	ret.crc(l4len, tc.lip, tc.rip)
	return ret, opt
}

func (tc *tcptcb_t) mkack(seq, ack uint32) *tcppkt_t {
	ret := &tcppkt_t{}
	ret.tcphdr.init_ack(tc.lport, tc.rport, seq, ack)
	ret.tcphdr.win = htons(tc.rcv.win)
	l4len := ret.tcphdr.hdrlen()
	ret.iphdr.init_tcp(l4len, tc.lip, tc.rip)
	ret.ether.init_ip4(tc.smac, tc.dmac)
	ret.crc(l4len, tc.lip, tc.rip)
	return ret
}

func (tc *tcptcb_t) synsent(ip4 *ip4hdr_t, tcp *tcphdr_t, mss uint16,
    rest [][]uint8) {
	tc._sanity()
	if ack, ok := tcp.isack(); ok {
		if !tc.ackok(ack) {
			if tcp.isrst() {
				return
			}
			tc._rst()
			return
		}
	}
	if tcp.isrst() {
		// XXX wakeup connect(3) thread
		fmt.Printf("connection refused\n")
		tc.dead = true
		return
	}
	// should we allow simultaneous connect?
	if !tcp.issyn() {
		return
	}
	ack, ok := tcp.isack()
	if !ok {
		// XXX make SYN/ACK, move to SYNRCVD
		panic("simultaneous connect")
	}
	tc._nstate(SYNSENT, ESTAB)
	theirseq := ntohl(tcp.seq)
	tc.rcv.nxt = theirseq + 1
	tc.rxbuf.set_seq(tc.rcv.nxt)
	tc.snd.mss = mss
	tc.snd.una = ack
	var dlen int
	for _, r := range rest {
		dlen += len(r)
	}
	// snd.wl[12] are bogus, force window update
	rwin := ntohs(tcp.win)
	tc.snd.wl1 = theirseq
	tc.snd.wl2 = ack
	tc.snd.win = rwin
	tc.sched_ack()
	if dlen != 0 {
		tc.data_in(tc.rcv.nxt, ack, rwin, rest, dlen)
	}
}

func (tc *tcptcb_t) estab(ip4 *ip4hdr_t, tcp *tcphdr_t, rest [][]uint8) {
	seq := ntohl(tcp.seq)
	// does this segment contain data in our receive window?
	var dlen int
	for _, r := range rest {
		dlen += len(r)
	}
	if !tc.seqok(seq, dlen) {
		if !tcp.isrst() {
			return
		}
		tc.sched_ack()
		return
	}
	if tcp.isrst() {
		// XXX fail user reads/writes
		tc.dead = true
		return
	}
	if tcp.issyn() {
		// XXX fail user reads/writes
		tc._rst()
		tc.dead = true
		return
	}
	ack, ok := tcp.isack()
	if !ok {
		return
	}
	if !tc.ackok(ack) {
		tc.sched_ack()
		return
	}
	tc.data_in(seq, ack, ntohs(tcp.win), rest, dlen)
}

// trims the segment to fit our receive window and acks it
func (tc *tcptcb_t) data_in(rseq, rack uint32, rwin uint16, rest[][]uint8,
    dlen int) {
	// XXXPANIC
	if !tc.seqok(rseq, dlen) {
		panic("must contain in-window data")
	}
	tc.snd.una = rack
	// figure out which bytes are in our window: is the beginning of
	// segment outside our window?
	var startoff uint32
	if !_seqbetween(tc.rcv.nxt, rseq, tc.rcv.nxt + uint32(tc.rcv.win)) {
		prune := _seqdiff(tc.rcv.nxt, rseq)
		startoff = uint32(prune)
		dlen -= prune
		for i, r := range rest {
			ub := prune
			if ub > len(r) {
				ub = len(r)
			}
			rest[i] = r[ub:]
			prune -= ub
			if prune == 0 {
				break
			}
		}
		// XXXPANIC
		if prune != 0 {
			panic("can't be in window")
		}
	}
	// prune so the end is within our window
	winend := tc.rcv.nxt + uint32(tc.rcv.win)
	if !_seqbetween(tc.rcv.nxt, rseq + uint32(dlen), winend) {
		prune := _seqdiff(rseq + uint32(dlen), winend)
		dlen -= prune
		for i := len(rest) - 1; i >= 0; i-- {
			r := rest[i]
			ub := prune
			if ub > len(r) {
				ub = len(r)
			}
			newlen := len(r) - ub
			rest[i] = r[:newlen]
			prune -= ub
			if prune == 0 {
				break
			}
		}
		// XXXPANIC
		if prune != 0 {
			panic("can't be in window")
		}
	}
	realseq := rseq + startoff
	tc.rwinupdate(realseq, rack, rwin)
	if realseq == tc.rcv.nxt {
		tc.rcv.nxt += uint32(dlen)
		// XXX add out of order sequences. keep list of seq,lens,
		// remove elements that are contained in this advancing seg,
		// add differences of endpoints with the seg that this one
		// intersects with.
	} else {
		// XXX save out of order sequence and length.
		panic("out of order segment")
	}
	tc.rxbuf.syswrite(realseq, rest)
	// we received data, update our window; avoid silly window syndrome.
	// delay acks when the window shrinks to less than an MSS since we will
	// send an immediate ack once the window reopens due to the user
	// reading from the receive buffer.
	delayack := tc.lwinshrink(dlen)
	if delayack {
		tc.sched_ack_delay()
	} else {
		tc.sched_ack()
	}
}

// is ACK in my send window?
func (tc *tcptcb_t) ackok(ack uint32) bool {
	tc._sanity()
	return _seqbetween(tc.snd.una, ack, tc.snd.nxt)
}

// is seq in my receive window?
func (tc *tcptcb_t) seqok(seq uint32, seglen int) bool {
	tc._sanity()
	winstart := tc.rcv.nxt
	winend := tc.rcv.nxt + uint32(tc.rcv.win)
	segend := seq + uint32(seglen)
	return _seqbetween(winstart, seq, winend) ||
	    _seqbetween(winstart, segend, winend)
}

// returns true when low <= mid <= hi on uints (i.e. even when sequences
// numbers wrap to 0)
func _seqbetween(low, mid, hi uint32) bool {
	if hi >= low {
		return mid >= low && mid <= hi
	} else {
		// wrapped around
		return mid >= low || mid <= hi
	}
}

// returns difference of big - small with unsigned wrapping
func _seqdiff(big, small uint32) int {
	if big >= small {
		return int(uint(big - small))
	} else {
		diff := ^uint32(0) - small + 1
		return int(uint(diff + big))
	}
}

// update remote receive window
func (tc *tcptcb_t) rwinupdate(seq, ack uint32, win uint16) {
	// see if seq is the larger than wl1. does the window wrap?
	lwinend := tc.rcv.nxt + uint32(tc.rcv.win)
	w1less := _seqdiff(lwinend, tc.snd.wl1) > _seqdiff(lwinend, seq)
	rwinend := tc.snd.nxt + uint32(tc.snd.win)
	wl2less := _seqdiff(rwinend, tc.snd.wl2) > _seqdiff(rwinend, ack)
	if w1less || (tc.snd.wl1 == seq && wl2less) {
		tc.snd.win = win
		tc.snd.wl1 = seq
		tc.snd.wl2 = ack
	}
}

// returns true if the receive window is less than an MSS
func (tc *tcptcb_t) lwinshrink(dlen int) bool {
	tc._sanity()
	var ret bool
	left := tc.rxbuf.cbuf.left()
	if left - int(tc.rcv.win) >= int(tc.rcv.mss) {
		tc.rcv.win = uint16(left)
		ret = false
	} else {
		// keep window static to encourage sender to send MSS sized
		// segments
		if uint16(dlen) > tc.rcv.win {
			panic("how? segments are pruned to window")
		}
		tc.rcv.win -= uint16(dlen)
		ret = true
	}
	return ret
}

func (tc *tcptcb_t) lwingrow(oldwin int) {
	tc._sanity()
	left := tc.rxbuf.cbuf.left()
	mss := int(tc.rcv.mss)
	if oldwin < mss && left >= mss {
		tc.rcv.win = uint16(left)
		// does it make sense to delay the ack increasing the receive
		// window?
		tc.sched_ack()
		tc.ack_now()
	}
}

func (tc *tcptcb_t) uread(dst *userbuf_t) (int, int) {
	tc._sanity()
	owin := int(tc.rcv.win)
	wrote, err := tc.rxbuf.cbuf.copyout(dst)
	// did the user consume enough data to reopen the window?
	tc.lwingrow(owin)
	return wrote, err
}

type tcpkey_t struct {
	lip	ip4_t
	rip	ip4_t
	lport	uint16
	rport	uint16
}

type tcpcons_t struct {
	sync.Mutex
	m	map[tcpkey_t]*tcptcb_t
}

func (tc *tcpcons_t) init() {
	tc.m = make(map[tcpkey_t]*tcptcb_t)
}

func (tc *tcpcons_t) tcb_new(tk tcpkey_t, smac, dmac *mac_t) (*tcptcb_t, bool) {
	tc.Lock()
	defer tc.Unlock()

	if _, ok := tc.m[tk]; ok {
		return nil, false
	}
	ret := &tcptcb_t{lip: tk.lip, rip: tk.rip, lport: tk.lport,
	    rport: tk.rport}
	ret.state = INVALID
	ret.snd.nxt = rand.Uint32()
	ret.snd.una = ret.snd.nxt
	defwin := uint16(4380)
	ret.snd.win = defwin
	ret.dmac = dmac
	ret.smac = smac
	defbufsz := (1 << 11)
	ret.txbuf.tbuf_init(defbufsz)
	ret.txbuf.set_seq(ret.snd.nxt + 1)
	ret.rxbuf.tbuf_init(defbufsz)
	if uint(ret.rxbuf.cbuf.left()) > uint(^uint16(0)) {
		panic("need window shift")
	}
	ret.rcv.win = uint16(ret.rxbuf.cbuf.left())
	ret.rcv.mss = 1460

	tc.m[tk] = ret
	return ret, true
}

func (tc *tcpcons_t) tcb_lookup(tk tcpkey_t) (*tcptcb_t, bool) {
	tc.Lock()
	defer tc.Unlock()
	t, ok := tc.m[tk]
	return t, ok
}

func (tc *tcpcons_t) tcb_del(tk tcpkey_t) {
	tc.Lock()
	defer tc.Unlock()

	_, ok := tc.m[tk]
	if !ok {
		panic("tk doesn't exist")
	}
	delete(tc.m, tk)
}

var tcpcons tcpcons_t

type tcppkt_t struct {
	ether	etherhdr_t
	iphdr	ip4hdr_t
	tcphdr	tcphdr_t
}

// writes pseudo header partial cksum to the TCP header cksum field. the sum is
// not complemented so that the partial pseudo header cksum can be summed with
// the rest of the TCP cksum by the NIC.
func (tp *tcppkt_t) crc(l4len int, sip, dip ip4_t) {
	sum := uint32(uint16(sip))
	sum += uint32(uint16(sip >> 16))
	sum += uint32(uint16(dip))
	sum += uint32(uint16(dip >> 16))
	sum += uint32(tp.iphdr.proto)
	sum += uint32(l4len)
	lm := uint32(^uint16(0))
	for sum &^ 0xffff != 0 {
		sum = (sum >> 16) + (sum & lm)
	}
	tp.tcphdr.cksum = htons(uint16(sum))
}

func (tp *tcppkt_t) hdrbytes() ([]uint8, []uint8, []uint8) {
	return tp.ether.bytes(), tp.iphdr.bytes(), tp.tcphdr.bytes()
}

func _tcp_connect(dip ip4_t, sport, dport uint16) (int, *tcptcb_t) {
	localip, routeip, err := routetbl.lookup(dip)
	if err != 0 {
		return err, nil
	}
	nic, ok := nics[localip]
	if !ok {
		return -EHOSTUNREACH, nil
	}
	dmac, err := arp_resolve(localip, routeip)
	if err != 0 {
		return err, nil
	}

	k := tcpkey_t{lip: localip, rip: dip, lport: sport, rport: dport}
	tcb, ok := tcpcons.tcb_new(k, &nic.mac, dmac)
	if !ok {
		return -EADDRINUSE, nil
	}

	tcb.tcb_lock()
	tcb._nstate(INVALID, SYNSENT)
	seq := tcb.snd.nxt
	pkt, opts := tcb.mkconnect(seq)
	tcb.snd.nxt++
	tcb.tcb_unlock()

	eth, ip, tcp := pkt.hdrbytes()
	sgbuf := [][]uint8{eth, ip, tcp, opts}
	nic.tx_tcp(sgbuf)
	return 0, tcb
}

func _send_rst(ip4 *ip4hdr_t, tcp *tcphdr_t) {
	fmt.Printf("RESET!\n")
}

func net_tcp(pkt [][]uint8, tlen int) {
	hdr := pkt[0]
	if len(hdr) < ETHERLEN {
		return
	}
	rest := hdr[ETHERLEN:]
	ip4, rest, ok := sl2iphdr(rest)
	if !ok {
		return
	}
	tcph, opts, rest, ok := sl2tcphdr(rest)
	if !ok {
		return
	}

	sip := sl2ip(ip4.sip[:])
	dip := sl2ip(ip4.dip[:])
	tcph.dump(sip, dip, opts)

	localip := dip
	k := tcpkey_t{lip: localip, rip: sip, lport: ntohs(tcph.dport),
	    rport: ntohs(tcph.sport)}
	tcb, ok := tcpcons.tcb_lookup(k)
	if !ok {
		_send_rst(ip4, tcph)
		return
	}

	pkt[0] = rest
	tcb.tcb_lock()
	tcb.incoming(k, ip4, tcph, opts, pkt)
	tcb.tcb_unlock()
}

var nics = map[ip4_t]*x540_t{}

// network stack processing begins here. pkt references DMA memory and will be
// clobbered once net_start returns to the caller.
func net_start(pkt [][]uint8, tlen int) {
	// header should always be fully contained in the first slice
	buf := pkt[0]
	hlen := len(buf)
	if hlen < ETHERLEN {
		return
	}

	etype := ntohs(be16(readn(buf, 2, 12)))
	ip4 := uint16(0x0800)
	arp := uint16(0x0806)
	switch etype {
	case arp:
		net_arp(pkt, tlen)
	case ip4:
		// strip ethernet header
		ippkt, _, ok := sl2iphdr(buf[ETHERLEN:])
		if !ok {
			// short IP4 header
			return
		}
		if ippkt.vers_hdr & 0xf0 != 0x40 {
			// not IP4?
			return
		}
		// no IP options yet
		if ippkt.vers_hdr & 0xf != 0x5 {
			fmt.Printf("no imp\n")
			return
		}

		// the length of a packet received by my NIC is 4byte aligned.
		// thus prune bytes in excess of IP4 length.
		reallen := int(ntohs(ippkt.tlen)) + ETHERLEN
		if tlen > reallen {
			prune := tlen - reallen
			lasti := len(pkt) - 1
			last := pkt[lasti]
			if len(last) < prune {
				panic("many extra bytes!")
			}
			newlen := len(last) - prune
			pkt[lasti] = last[:newlen]
			tlen = reallen
		}

		proto := ippkt.proto
		icmp := uint8(0x01)
		tcp := uint8(0x06)
		switch proto {
		case icmp:
			net_icmp(pkt, tlen)
		case tcp:
			net_tcp(pkt, tlen)
		}
	}
}

func netchk() {
	if ARPLEN != 42 {
		panic("arp bad size")
	}
	if IP4LEN != 20 {
		panic("bad ip4 header size")
	}
	if ETHERLEN != 14 {
		panic("bad ethernet header size")
	}
	if TCPLEN != 20 {
		panic("bad tcp header size")
	}
}

func net_init() {
	netchk()

	arptbl.m = make(map[ip4_t]*arprec_t)
	arptbl.waiters = make(map[ip4_t][]chan bool)
	arptbl.enttimeout = 20*time.Minute
	arptbl.restimeout = 5*time.Second

	routetbl.init()

	go icmp_daemon()

	tcpcons.init()

	net_test()
}

func net_test() {
	if false {
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

	// test ACK check wrapping
	tcb := &tcptcb_t{}
	tcb.tcb_lock()
	t := func(l, h, m uint32, e bool) {
		tcb.snd.una = l
		tcb.snd.nxt = h
		if tcb.ackok(m) != e {
			panic(fmt.Sprintf("fail %v %v %v %v", l, h, m, e))
		}
	}

	t(0, 10, 0, true)
	t(1, 10, 0, false)
	t(0, 10, 1, true)
	t(0, 10, 10, true)
	t(0, 0, 0, true)
	t(0xfffffff0, 10, 0, true)
	t(0xfffffff0, 10, 10, true)
	t(0xfffffff0, 10, 0xfffffff1, true)
	t(0xfffffff0, 10, 0xfffffff0, true)
	t(0xfffffff0, 0xffffffff, 0, false)

	d := func(b, s uint32, e int) {
		r := _seqdiff(b, s)
		if r != e {
			panic(fmt.Sprintf("%#x != %#x", r, e))
		}
	}
	d(1, 0, 1)
	d(0, 0xfffffff0, 0x10)
	d(30, 0xfffffff0, 0x10 + 30)
}
