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
func arp_resolve(sip, dip ip4_t) (*mac_t, err_t) {
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
func (r *routes_t) lookup(dip ip4_t) (ip4_t, ip4_t, err_t) {
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

func (rt *routetbl_t) lookup(dip ip4_t) (ip4_t, ip4_t, err_t) {
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
	cond	*sync.Cond
	pollers	*pollers_t
}

func (tb *tcpbuf_t) tbuf_init(sz int, tcb *tcptcb_t) {
	tb.cbuf.cb_init(sz)
	tb.didseq = false
	tb.cond = sync.NewCond(&tcb.l)
	tb.pollers = &tcb.pollers
}

func (tb *tcpbuf_t) set_seq(seq uint32) {
	if tb.didseq {
		panic("seq already set")
	}
	tb.seq = seq
	tb.didseq = true
}

func (tb *tcpbuf_t) end_seq() uint32 {
	return uint32(uint(tb.seq) + uint(tb.cbuf.used()))
}

func (tb *tcpbuf_t) _sanity() {
	if !tb.didseq {
		panic("no sequence")
	}
}

// writes rxdata to the circular buffer, allows user to read written data (if
// next in the sequence) and returns number of bytes written. cannot fail.
func (tb *tcpbuf_t) syswrite(nseq uint32, rxdata [][]uint8) int {
	tb._sanity()
	if !_seqbetween(tb.seq, nseq, tb.seq + uint32(tb.cbuf.left())) {
		panic("bad sequence number")
	}
	var dlen int
	for _, r := range rxdata {
		dlen += len(r)
	}
	if dlen == 0 {
		panic("nothing to write")
	}
	if dlen > tb.cbuf.left() {
		panic("should be pruned to window")
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
		tb.cond.Broadcast()
		tb.pollers.wakeready(R_READ)
	}
	return did
}

// returns slices referencing the data written by the user (two when the
// buffers wrap the circular buffer) whose total length is at most l. does not
// free up the buffer by advancing the tail (that happens once data is acked).
func (tb *tcpbuf_t) sysread(nseq uint32, l int) ([]uint8, []uint8) {
	tb._sanity()
	if !_seqbetween(tb.seq, nseq, tb.seq + uint32(tb.cbuf.used())) {
		panic("bad sequence number")
	}
	off := _seqdiff(nseq, tb.seq)
	s1, s2 := tb.cbuf._rawread(off)
	rl := len(s1) + len(s2)
	if rl > l {
		totprune := rl - l
		if len(s2) > 0 {
			prune := totprune
			if prune > len(s2) {
				prune = len(s2)
			}
			newlen := len(s2) - prune
			s2 = s2[:newlen]
			totprune -= prune
		}
		newlen := len(s1) - totprune
		s1 = s1[:newlen]
	}
	return s1, s2
}

// advances the circular buffer tail by the difference between rack and the
// current sequence and updates the seqence.
func (tb *tcpbuf_t) ackup(rack uint32) {
	if !_seqbetween(tb.seq, rack, tb.seq + uint32(tb.cbuf.used())) {
		panic("ack out of window")
	}
	sz := _seqdiff(rack, tb.seq)
	if sz == 0 {
		return
	}
	tb.cbuf._advtail(sz)
	tb.seq += uint32(sz)
	tb.cond.Broadcast()
	tb.pollers.wakeready(R_WRITE)
}

type tseg_t struct {
	seq	uint32
	len	uint32
	when	time.Time
}

type tcpsegs_t struct {
	segs	[]tseg_t
	winend	uint32
}

func (ts *tcpsegs_t) _sanity() {
	// XXXPANIC
	for i := range ts.segs {
		seq := ts.segs[i].seq
		slen := ts.segs[i].len
		if i > 0 {
			prev := ts.segs[i-1]
			if prev.seq == seq {
				panic("same seqs")
			}
			if _seqbetween(prev.seq, seq, prev.seq + prev.len - 1) {
				panic("overlap")
			}
		}
		if i < len(ts.segs) - 1 {
			next := ts.segs[i + 1]
			if next.seq == seq {
				panic("same seqs")
			}
			nend := next.seq + next.len
			if _seqbetween(next.seq, seq + slen - 1, nend) {
				panic("overlap")
			}
		}
	}
}

// adds [seq, seq+len) to the segment list with timestamp set to now. seq must
// not already be in the list. if existing segments are contained in the new
// segment, they will be coalesced.  winend is the end of the receive window.
// the segments are sorted in descending order of distance from the window end
// (needed because sequence numbers may wrap).
func (ts *tcpsegs_t) addnow(seq, len, winend uint32) {
	ts._addnoisect(seq, 1, winend)
	ts.reset(seq, len)
}

func (ts *tcpsegs_t) _addnoisect(seq, len, winend uint32) {
	if len == 0 {
		panic("0 length transmit seg?")
	}
	ns := tseg_t{seq: seq, len: len}
	ns.when = time.Now()
	ts.segs = append(ts.segs, ns)
	ts.winend = winend
	sort.Sort(ts)
	ts._sanity()
}

func (ts *tcpsegs_t) ackupto(seq uint32) {
	var rem int
	sdiff := _seqdiff(ts.winend, seq)
	for i := range ts.segs {
		tdiff := _seqdiff(ts.winend, ts.segs[i].seq)
		if sdiff >= tdiff {
			break
		}
		tseq := ts.segs[i].seq
		tlen := ts.segs[i].len
		if _seqbetween(tseq, seq, tseq + tlen - 1) {
			oend := tseq + tlen
			ts.segs[i].seq = seq
			ts.segs[i].len = uint32(_seqdiff(oend, seq))
			break
		}
		rem++
	}
	if rem != 0 {
		copy(ts.segs, ts.segs[rem:])
		newlen := len(ts.segs) - rem
		ts.segs = ts.segs[:newlen]
	}
	ts._sanity()
}

func (ts *tcpsegs_t) reset(seq, nlen uint32) {
	var found bool
	var start int
	for i := range ts.segs {
		if ts.segs[i].seq == seq {
			start = i
			found = true
			break
		}
	}
	if !found {
		panic("seq not found")
	}
	if nlen == ts.segs[start].len {
		return
	}
	if nlen < ts.segs[start].len {
		panic("must exceed original seg length")
	}
	found = false
	prunefrom := start + 1
	for i := start + 1; i < len(ts.segs); i++ {
		tseq := ts.segs[i].seq
		tlen := ts.segs[i].len
		if _seqbetween(tseq, seq + nlen, tseq + tlen - 1) {
			oend := tseq + tlen
			ts.segs[i].seq = seq + nlen
			ts.segs[i].len = uint32(_seqdiff(oend, seq + nlen))
			break
		}
		prunefrom++
	}
	copy(ts.segs[start+1:], ts.segs[prunefrom:])
	rest := len(ts.segs) - prunefrom
	ts.segs = ts.segs[:start + rest + 1]
	ts.segs[start].len = nlen
	ts.segs[start].when = time.Now()
}

// prune unacknowledged sequences if the window has shrunk
func (ts *tcpsegs_t) prune(winend uint32) {
	prunefrom := len(ts.segs)
	for i := range ts.segs {
		seq := ts.segs[i].seq
		l := ts.segs[i].len
		if _seqbetween(seq, winend, seq + l - 1) {
			if seq == winend {
				prunefrom = i
			} else {
				ts.segs[i].len = uint32(_seqdiff(winend, seq))
				prunefrom = i + 1
			}
			break
		}
	}
	ts.segs = ts.segs[:prunefrom]
}

// returns the timestamp of the oldest segment in the list or false if the list
// is empty
func (ts *tcpsegs_t) nextts() (time.Time, bool) {
	var ret time.Time
	if len(ts.segs) > 0 {
		ret = ts.segs[0].when
		for i := range ts.segs {
			if ts.segs[i].when.Before(ret) {
				ret = ts.segs[i].when
			}
		}
	}
	return ret, len(ts.segs) > 0
}

func (ts *tcpsegs_t) Len() int {
	return len(ts.segs)
}

func (ts *tcpsegs_t) Less(i, j int) bool {
	diff1 := _seqdiff(ts.winend, ts.segs[i].seq)
	diff2 := _seqdiff(ts.winend, ts.segs[j].seq)
	return diff1 > diff2
}

func (ts *tcpsegs_t) Swap(i, j int) {
	ts.segs[i], ts.segs[j] = ts.segs[j], ts.segs[i]
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
	// remote sent FIN
	rxdone	bool
	// we sent FIN
	txdone	bool
	openc	int
	pollers	pollers_t
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
		finseq	uint32
		tsegs	tcpsegs_t
	}
	// timer state
	remack struct {
		last		time.Time
		// number of segments received; we try to send 1 ACK per 2 data
		// segments.
		num		uint
		forcedelay	bool
		// outstanding ACK?
		outa		bool
		tstart		bool
	}
	remseg struct {
		tstart		bool
		target		time.Time
	}
	// data to send over the TCP connection
	txbuf	tcpbuf_t
	// data received over the TCP connection
	rxbuf	tcpbuf_t
}

type tcpstate_t uint

const (
	// the following is for newly created tcbs
	TCPNEW		tcpstate_t = iota
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
	CLOSED
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
	TCPNEW: "TCPNEW",
}

func (tc *tcptcb_t) tcb_lock() {
	tc.l.Lock()
	tc.locked = true
}

func (tc *tcptcb_t) tcb_unlock() {
	tc.locked = false
	tc.l.Unlock()
}

// waits on the receive buffer conditional variable
func (tc *tcptcb_t) rbufwait() {
	tc.rxbuf.cond.Wait()
	tc.locked = true
}

func (tc *tcptcb_t) tbufwait() {
	tc.txbuf.cond.Wait()
	tc.locked = true
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
	//fmt.Printf("%v -> %v\n", statestr[old], statestr[news])
}

func (tc *tcptcb_t) _rst() {
	fmt.Printf("tcb reset no imp")
}

func (tc *tcptcb_t) incoming(tk tcpkey_t, ip4 *ip4hdr_t, tcp *tcphdr_t,
    opt tcpopt_t, rest [][]uint8) {
	tc._sanity()
	switch tc.state {
		case SYNSENT:
			tc.synsent(tcp, opt.mss, rest)
		case ESTAB:
			tc.estab(tcp, rest)
			// send window may have increased
			tc.seg_maybe()
			if !tc.txfinished(ESTAB, FINWAIT1) {
				tc.finchk(ip4, tcp, ESTAB, CLOSEWAIT)
			}
		case CLOSEWAIT:
			// user may queue for send, receive is done
			tc.estab(tcp, rest)
			// send window may have increased
			tc.seg_maybe()
			tc.txfinished(CLOSEWAIT, LASTACK)
		case LASTACK:
			// user may queue for send, receive is done
			// XXXPANIC
			if !tc.txdone {
				panic("txdone must be true")
			}
			tc.estab(tcp, rest)
			tc.seg_maybe()
			if tc.finacked() {
				if tc.txbuf.cbuf.used() != 0 {
					panic("closing but txdata remains")
				}
				tc.dead = true
				fmt.Printf("* done!\n")
			}
		case FINWAIT1:
			// user may no longer queue for send, may still receive
			// XXXPANIC
			if !tc.txdone {
				panic("txdone must be true")
			}
			tc.estab(tcp, rest)
			tc.seg_maybe()
			// did they ACK our FIN?
			if tc.finacked() {
				tc._nstate(FINWAIT1, FINWAIT2)
				// see if they also sent FIN
				tc.finchk(ip4, tcp, FINWAIT2, TIMEWAIT)
			} else {
				tc.finchk(ip4, tcp, FINWAIT1, CLOSING)
			}
		case FINWAIT2:
			// user may no longer queue for send, may still receive
			// XXXPANIC
			if !tc.txdone {
				panic("txdone must be true")
			}
			tc.estab(tcp, rest)
			tc.finchk(ip4, tcp, FINWAIT2, TIMEWAIT)
		case CLOSING:
			// user may no longer queue for send, receive is done
			// XXXPANIC
			if !tc.txdone {
				panic("txdone must be true")
			}
			tc.estab(tcp, rest)
			tc.seg_maybe()
			if tc.finacked() {
				tc._nstate(CLOSING, TIMEWAIT)
			}
		case TIMEWAIT:
			// XXX timeout here
		default:
			sn, ok := statestr[tc.state]
			if !ok {
				panic("huh?")
			}
			fmt.Printf("no imp for state %s\n", sn)
			return
	}

	if tc.dead {
		tcpcons.tcb_del(tk)
		return
	} else {
		tc.ack_maybe()
	}
}

// if the packet has FIN set and we've acked all data up to the fin, increase
// rcv.nxt to ACK the FIN and move to the next state.
func (tc *tcptcb_t) finchk(ip4 *ip4hdr_t, tcp *tcphdr_t, os, ns tcpstate_t) {
	hlen := IP4LEN + int(tcp.dataoff >> 4) * 4
	tlen := int(ntohs(ip4.tlen))
	if tlen < hlen {
		panic("bad packet should be pruned")
	}
	paylen := tlen - hlen
	rfinseq := ntohl(tcp.seq) + uint32(paylen)
	if tcp.isfin() && tc.rcv.nxt == rfinseq {
		tc.rcv.nxt++
		tc._nstate(os, ns)
		tc.rxdone = true
		tc.rxbuf.cond.Broadcast()
		tc.pollers.wakeready(R_READ)
		tc.sched_ack()
	}
}

// if all user data has been sent (we must have sent FIN), move to the next
// state. returns true of this function changed the TCB's state.
func (tc *tcptcb_t) txfinished(os, ns tcpstate_t) bool {
	if !tc.txdone {
		return false
	}
	ret := false
	if tc.snd.nxt == tc.snd.finseq {
		tc._nstate(os, ns)
		ret = true
	}
	return ret
}

// returns true if our FIN has been ack'ed
func (tc *tcptcb_t) finacked() bool {
	if !tc.txdone {
		panic("can't have sent FIN yet")
	}
	if tc.snd.una == tc.snd.finseq + 1 {
		return true
	}
	return false
}

// sets flag to send an ack which may be delayed.
func (tc *tcptcb_t) sched_ack() {
	tc.remack.num++
	tc.remack.outa = true
}

func (tc *tcptcb_t) sched_ack_delay() {
	tc.remack.outa = true
	tc.remack.forcedelay = true
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
	if !tc.remack.outa {
		return
	}
	fdelay := tc.remack.forcedelay
	tc.remack.forcedelay = false

	segdelay := tc.remack.num % 2 == 0
	canwait := !sendnow && (segdelay || fdelay)
	if canwait {
		// delay at most 500ms
		now := time.Now()
		deadline := tc.remack.last.Add(500*time.Millisecond)
		if now.Before(deadline) {
			if !tc.remack.tstart {
				// XXX goroutine per timeout bad?
				tc.remack.tstart = true
				left := deadline.Sub(now)
				go func() {
					time.Sleep(left)
					tc.tcb_lock()
					tc.remack.tstart = false
					tc.ack_maybe()
					tc.tcb_unlock()
				}()
			}
			return
		}
	}
	tc.remack.outa = false

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
	tc.remack.last = time.Now()
}

// transmit new segments and retransmits timed-out segments as the window
// allows
func (tc *tcptcb_t) seg_maybe() {
	if tc.txdone && tc.snd.una == tc.snd.finseq + 1 {
		// everything has been acknowledged
		return
	}
	winend := tc.snd.una + uint32(tc.snd.win)
	// prune unacknowledged segments which are now outside of the send
	// window
	tc.snd.tsegs.prune(winend)
	// have retransmit timeouts expired?
	now := time.Now()
	for i := 0; i < len(tc.snd.tsegs.segs); i++ {
		to := time.Second
		deadline := tc.snd.tsegs.segs[i].when.Add(to)
		if now.Before(deadline) {
			continue
		}
		seq := tc.snd.tsegs.segs[i].seq
		// XXXPANIC
		if seq == winend {
			panic("how?")
		}
		did := tc.seg_one(seq)
		// reset() modifies segs[]
		tc.snd.tsegs.reset(seq, uint32(did))
	}
	// XXXPANIC
	{
		// without sacks, gaps cannot occur
		ss := tc.snd.tsegs.segs
		for i := 0; i < len(tc.snd.tsegs.segs) - 1; i++ {
			if ss[i].seq + ss[i].len != ss[i+1].seq {
				panic("htf?")
			}
		}
	}
	// XXX nagle's?
	// transmit any unsent data in the send window
	upto := winend
	if _seqbetween(tc.snd.una, tc.txbuf.end_seq(), upto) {
		upto = tc.txbuf.end_seq()
	}
	sbegin := tc.snd.nxt
	if _seqdiff(upto, sbegin) > 0 {
		did := tc.seg_one(sbegin)
		tc.snd.tsegs.addnow(sbegin, uint32(did), winend)
	}
	tc._txtimeout_start()
}

func (tc *tcptcb_t) seg_one(seq uint32) int {
	winend := tc.snd.una + uint32(tc.snd.win)
	// XXXPANIC
	if !_seqbetween(tc.snd.una, seq, winend) {
		panic("must be in send window")
	}
	nic, ok := nics[tc.lip]
	if !ok {
		panic("NIC gone")
	}
	// the data to send may be larger than MSS
	buf1, buf2 := tc.txbuf.sysread(seq, _seqdiff(winend, seq))
	dlen := len(buf1) + len(buf2)
	if dlen == 0 {
		panic("must send non-zero amount")
	}

	pkt, istso := tc.mkseg(seq, tc.rcv.nxt, dlen)
	eth, ip, thdr := pkt.hdrbytes()

	sgbuf := [][]uint8{eth, ip, thdr, buf1, buf2}
	smss := int(tc.snd.mss)
	if istso {
		nic.tx_tcp_tso(sgbuf, len(thdr), smss)
	} else {
		nic.tx_tcp(sgbuf)
	}

	// we just queued an ack, so clear outstanding ack flag
	tc.remack.outa = false
	tc.remack.forcedelay = false
	tc.remack.last = time.Now()

	send := seq + uint32(dlen)
	ndiff := _seqdiff(winend + 1, seq + uint32(dlen))
	odiff := _seqdiff(winend + 1, tc.snd.nxt)
	if ndiff < odiff {
		tc.snd.nxt = send
	}
	return dlen
}

// is waiting until the oldest seg timesout a good strategy? maybe wait until
// the middle timesout?
func (tc *tcptcb_t) _txtimeout_start() {
	nts, ok := tc.snd.tsegs.nextts()
	if !ok {
		return
	}
	if tc.remseg.tstart {
		// XXXPANIC
		if nts.Before(tc.remseg.target) {
			fmt.Printf("** timeout shrink! %v\n",
			    tc.remseg.target.Sub(nts))
		}
		return
	}
	tc.remseg.tstart = true
	tc.remseg.target = nts
	// XXX rto calculation
	rto := time.Second*5
	deadline := nts.Add(rto)
	now := time.Now()
	if now.After(deadline) {
		fmt.Printf("** timeouts are shat upon!\n")
		return
	}
	go func() {
		time.Sleep(deadline.Sub(now))
		tc.tcb_lock()
		tc.remseg.tstart = false
		tc.seg_maybe()
		tc.tcb_unlock()
	}()
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

func (tc *tcptcb_t) _setfin(tp *tcphdr_t, seq uint32) {
	// set FIN?
	if tc.txdone && seq == tc.snd.finseq {
		fin := uint8(1 << 0)
		tp.flags |= fin
	}
}

func (tc *tcptcb_t) mkack(seq, ack uint32) *tcppkt_t {
	ret := &tcppkt_t{}
	ret.tcphdr.init_ack(tc.lport, tc.rport, seq, ack)
	ret.tcphdr.win = htons(tc.rcv.win)
	tc._setfin(&ret.tcphdr, seq)
	l4len := ret.tcphdr.hdrlen()
	ret.iphdr.init_tcp(l4len, tc.lip, tc.rip)
	ret.ether.init_ip4(tc.smac, tc.dmac)
	ret.crc(l4len, tc.lip, tc.rip)
	return ret
}

// returns the new TCP packet and true if the packet should use TSO
func (tc *tcptcb_t) mkseg(seq, ack uint32, seglen int) (*tcppkt_t, bool) {
	ret := &tcppkt_t{}
	ret.tcphdr.init_ack(tc.lport, tc.rport, seq, ack)
	ret.tcphdr.win = htons(tc.rcv.win)
	tc._setfin(&ret.tcphdr, seq + uint32(seglen))
	l4len := ret.tcphdr.hdrlen() + seglen
	ret.iphdr.init_tcp(l4len, tc.lip, tc.rip)
	ret.ether.init_ip4(tc.smac, tc.dmac)
	istso := seglen > int(tc.snd.mss)
	// packets using TSO do not include the the TCP payload length in the
	// pseudo-header checksum.
	if istso {
		l4len = 0
	}
	ret.crc(l4len, tc.lip, tc.rip)
	return ret, istso
}

func (tc *tcptcb_t) failwake() {
	tc._sanity()
	tc.state = CLOSED
	tc.dead = true
	tc.rxdone = true
	tc.rxbuf.cond.Broadcast()
	tc.txbuf.cond.Broadcast()
	tc.pollers.wakeready(R_READ | R_HUP | R_ERROR)
}

func (tc *tcptcb_t) synsent(tcp *tcphdr_t, mss uint16, rest [][]uint8) {
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
		fmt.Printf("connection refused\n")
		tc.failwake()
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
	tc.data_in(tc.rcv.nxt, ack, rwin, rest, dlen)
	// wakeup threads blocking in connect(2)
	tc.rxbuf.cond.Broadcast()
}

// sanity check the packet and segment processing: increase snd.una according
// to remote ACK, copy data to user buffer, and schedule an ACK of our own.
func (tc *tcptcb_t) estab(tcp *tcphdr_t, rest [][]uint8) {
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
		tc.failwake()
		return
	}
	if tcp.issyn() {
		// XXX fail user reads/writes
		tc._rst()
		tc.failwake()
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

// trims the segment to fit our receive window, copies received data to the
// user buffer, and acks it.
func (tc *tcptcb_t) data_in(rseq, rack uint32, rwin uint16, rest[][]uint8,
    dlen int) {
	// XXXPANIC
	if !tc.seqok(rseq, dlen) {
		panic("must contain in-window data")
	}
	// +1 in case our FIN's sequence number is just outside the send window
	swinend := tc.snd.una + uint32(tc.snd.win) + 1
	if _seqdiff(swinend, rack) < _seqdiff(swinend, tc.snd.una) {
		tc.snd.tsegs.ackupto(rack)
		tc.snd.una = rack
		// distinguish between acks for data and the ack for our FIN
		pack := rack
		if pack > tc.txbuf.end_seq() {
			pack = tc.txbuf.end_seq()
		}
		tc.txbuf.ackup(pack)
	}
	// figure out which bytes are in our window: is the beginning of
	// segment outside our window?
	if !_seqbetween(tc.rcv.nxt, rseq, tc.rcv.nxt + uint32(tc.rcv.win)) {
		prune := _seqdiff(tc.rcv.nxt, rseq)
		rseq += uint32(prune)
		if prune > dlen {
			panic("uh oh")
		}
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
		if prune > dlen {
			panic("uh oh")
		}
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
	tc.rwinupdate(rseq, rack, rwin)
	if rseq == tc.rcv.nxt {
		tc.rcv.nxt += uint32(dlen)
		// XXX add out of order sequences. keep list of seq,lens,
		// remove elements that are contained in this advancing seg,
		// add differences of endpoints with the seg that this one
		// intersects with.
	} else {
		// XXX save out of order sequence and length.
		panic("out of order segment")
	}
	if dlen != 0 {
		tc.rxbuf.syswrite(rseq, rest)
	}
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
	// special case ack of our FIN
	return _seqbetween(tc.snd.una, ack, tc.snd.nxt) ||
	    (tc.txdone && ack == tc.snd.finseq + 1)
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
	rwinend := tc.snd.una + uint32(tc.snd.win) + 1
	wl2less := _seqdiff(rwinend, tc.snd.wl2) >= _seqdiff(rwinend, ack)
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

func (tc *tcptcb_t) uread(dst *userbuf_t) (int, err_t) {
	tc._sanity()
	owin := int(tc.rcv.win)
	wrote, err := tc.rxbuf.cbuf.copyout(dst)
	// did the user consume enough data to reopen the window?
	tc.lwingrow(owin)
	return wrote, err
}

func (tc *tcptcb_t) uwrite(src *userbuf_t) (int, err_t) {
	tc._sanity()
	wrote, err := tc.txbuf.cbuf.copyin(src)
	if tc.state == ESTAB || tc.state == CLOSEWAIT {
		tc.seg_maybe()
	}
	return wrote, err
}

func (tc *tcptcb_t) shutdown(read, write bool) err_t {
	tc._sanity()
	if tc.state != ESTAB && tc.state != CLOSEWAIT {
		fmt.Printf("** shutdown bad state %v\n", statestr[tc.state])
		return -ENOTCONN
	}
	if tc.txdone {
		panic("how")
	}

	tc.txdone = true
	tc.snd.finseq = tc.txbuf.end_seq()
	if tc.snd.nxt == tc.snd.finseq {
		tc.sched_ack()
		tc.ack_now()
	}
	return 0
}

func (tc *tcptcb_t) tcb_init(lip, rip ip4_t, rport uint16, smac, dmac *mac_t) {
	tc.lip = lip
	tc.rip = rip
	// tc.lport set in tcb_insert()
	tc.rport = rport
	tc.state = TCPNEW
	tc.snd.nxt = rand.Uint32()
	tc.snd.una = tc.snd.nxt
	defwin := uint16(4380)
	tc.snd.win = defwin
	tc.dmac = dmac
	tc.smac = smac
	defbufsz := (1 << 14)
	tc.txbuf.tbuf_init(defbufsz, tc)
	tc.txbuf.set_seq(tc.snd.nxt + 1)
	tc.rxbuf.tbuf_init(defbufsz, tc)
	if uint(tc.rxbuf.cbuf.left()) > uint(^uint16(0)) {
		panic("need window shift")
	}
	tc.rcv.win = uint16(tc.rxbuf.cbuf.left())
	tc.rcv.mss = 1460
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

// creates a new TCP with an unused source port
func (tc *tcpcons_t) tcb_insert(tcb *tcptcb_t) err_t {
	tc.Lock()

	// find available source port
	if len(tc.m) == 0xffff {
		panic("out of local ports")
	}
	tk := tcpkey_t{lip: tcb.lip, rip: tcb.rip, rport: tcb.rport}
	tk.lport = uint16(rand.Uint32())
	_, ok := tc.m[tk]
	for ok {
		tk.lport++
		_, ok = tc.m[tk]
	}
	if _, ok = tc.m[tk]; ok {
		panic("key exists")
	}
	tcb.lport = tk.lport
	tc.m[tk] = tcb
	tc.Unlock()

	return 0
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
// the rest of the TCP cksum (offloaded to the NIC).
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

func _tcp_connect(tcb *tcptcb_t, dip ip4_t, dport uint16) err_t {
	localip, routeip, err := routetbl.lookup(dip)
	if err != 0 {
		return err
	}
	nic, ok := nics[localip]
	if !ok {
		return -EHOSTUNREACH
	}
	dmac, err := arp_resolve(localip, routeip)
	if err != 0 {
		return err
	}

	tcb.tcb_init(localip, dip, dport, &nic.mac, dmac)
	// acquires TCB table lock while holding tcb's lock
	err = tcpcons.tcb_insert(tcb)
	if err != 0 {
		return err
	}

	tcb._sanity()
	tcb._nstate(TCPNEW, SYNSENT)
	seq := tcb.snd.nxt
	pkt, opts := tcb.mkconnect(seq)
	tcb.snd.nxt++

	eth, ip, tcp := pkt.hdrbytes()
	sgbuf := [][]uint8{eth, ip, tcp, opts}
	nic.tx_tcp(sgbuf)
	return 0
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
	//tcph.dump(sip, dip, opts)

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

type tcpfops_t struct {
	tcb	tcptcb_t
	options	fdopt_t
}

// to prevent an operation racing with a close
func (tf *tcpfops_t) _closed() (err_t, bool) {
	if tf.tcb.openc == 0 {
		return -EBADF, false
	}
	return 0, true
}

func (tf *tcpfops_t) close() err_t {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if err, ok := tf._closed(); !ok {
		return err
	}

	tf.tcb.openc--
	if tf.tcb.openc < 0 {
		panic("neg ref")
	}
	if tf.tcb.openc == 0 {
		tf.tcb.shutdown(true, true)
	}

	return 0
}

func (tf *tcpfops_t) fstat(*stat_t) err_t {
	panic("no imp")
}

func (tf *tcpfops_t) lseek(int, int) (int, err_t) {
	return 0, -ESPIPE
}

func (tf *tcpfops_t) mmapi(int, int) ([]mmapinfo_t, err_t) {
	return nil, -EINVAL
}

func (tf *tcpfops_t) pathi() *imemnode_t {
	panic("tcp socket cwd")
}

func (tf *tcpfops_t) read(dst *userbuf_t) (int, err_t) {
	tf.tcb.tcb_lock()
	if err, ok := tf._closed(); !ok {
		tf.tcb.tcb_unlock()
		return 0, err
	}

	var read int
	var err err_t
	for {
		read, err = tf.tcb.uread(dst)
		if err != 0 {
			break
		}
		if read != 0 || tf.tcb.rxdone {
			break
		}
		tf.tcb.rbufwait()
	}
	tf.tcb.tcb_unlock()
	return read, err
}

func (tf *tcpfops_t) reopen() err_t {
	tf.tcb.tcb_lock()
	tf.tcb.openc++
	tf.tcb.tcb_unlock()
	return 0
}

func (tf *tcpfops_t) write(src *userbuf_t) (int, err_t) {
	tf.tcb.tcb_lock()
	if err, ok := tf._closed(); !ok {
		tf.tcb.tcb_unlock()
		return 0, err
	}

	var wrote int
	var err err_t
	for {
		if tf.tcb.txdone {
			err = -EPIPE
			break
		}
		var did int
		did, err = tf.tcb.uwrite(src)
		wrote += did
		if src.remain() == 0 || err != 0 {
			break
		}
		tf.tcb.tbufwait()
	}
	tf.tcb.tcb_unlock()
	return wrote, err
}

func (tf *tcpfops_t) fullpath() (string, err_t) {
	panic("weird cwd")
}

func (tf *tcpfops_t) truncate(newlen uint) err_t {
	return -EINVAL
}

func (tf *tcpfops_t) pread(dst *userbuf_t, offset int) (int, err_t) {
	return 0, -ESPIPE
}

func (tf *tcpfops_t) pwrite(src *userbuf_t, offset int) (int, err_t) {
	return 0, -ESPIPE
}

func (tf *tcpfops_t) accept(*proc_t, *userbuf_t) (fdops_i, int, err_t) {
	panic("no imp")
}

func (tf *tcpfops_t) bind(proc *proc_t, saddr []uint8) err_t {
	panic("no imp")
}

func (tf *tcpfops_t) connect(proc *proc_t, saddr []uint8) err_t {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if tf.tcb.state != TCPNEW {
		return -EISCONN
	}
	if err, ok := tf._closed(); !ok {
		return err
	}

	blk := true
	if tf.options & O_NONBLOCK != 0 {
		blk = false
	}
	dip := ip4_t(ntohl(be32(readn(saddr, 4, 4))))
	dport := ntohs(be16(readn(saddr, 2, 2)))
	tcb := &tf.tcb
	err := _tcp_connect(tcb, dip, dport)
	if err != 0 {
		return err
	}
	var ret err_t
	if blk {
		for tcb.state == SYNSENT || tcb.state == SYNRCVD {
			tcb.rbufwait()
		}
		if tcb.state != ESTAB && tcb.state != CLOSED {
			panic("unexpected state")
		}
		if tcb.state == CLOSED {
			ret = -ECONNRESET
		}
	}
	return ret
}

func (tf *tcpfops_t) listen(proc *proc_t, backlog int) (fdops_i, err_t) {
	panic("no imp")
}

func (tf *tcpfops_t) sendto(proc *proc_t, src *userbuf_t,
    toaddr []uint8, flags int) (int, err_t) {
	return tf.write(src)
}

func (tf *tcpfops_t) recvfrom(proc *proc_t, dst *userbuf_t,
    fromsa *userbuf_t) (int, int, err_t) {
	wrote, err := tf.read(dst)
	return wrote, 0, err
}

func (tf *tcpfops_t) pollone(pm pollmsg_t) ready_t {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if _, ok := tf._closed(); !ok {
		return 0
	}

	var ret ready_t
	isdata := !tf.tcb.rxbuf.cbuf.empty()
	if tf.tcb.dead {
		ret |= R_ERROR
	}
	if pm.events & R_READ != 0 && (isdata || tf.tcb.rxdone) {
		ret |= R_READ
	}
	if tf.tcb.txdone {
		if pm.events & R_HUP != 0 {
			ret |= R_HUP
		}
	} else if pm.events & R_WRITE != 0 && !tf.tcb.txbuf.cbuf.full() {
		ret |= R_WRITE
	}
	if ret == 0 && pm.dowait {
		tf.tcb.pollers.addpoller(&pm)
	}
	return ret
}

func (tf *tcpfops_t) fcntl(proc *proc_t, cmd, opt int) int {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	switch cmd {
	case F_GETFL:
		return int(tf.options)
	case F_SETFL:
		tf.options = fdopt_t(opt)
		return 0
	default:
		panic("weird cmd")
	}
}

func (tf *tcpfops_t) getsockopt(proc *proc_t, opt int, bufarg *userbuf_t,
    intarg int) (int, err_t) {
	panic("no imp")
	switch opt {
	case SO_ERROR:
		if !proc.userwriten(bufarg.userva, 4, 0) {
			return 0, -EFAULT
		}
		return 4, 0
	default:
		return 0, -EOPNOTSUPP
	}
}

func (tf *tcpfops_t) shutdown(read, write bool) err_t {
	tf.tcb.tcb_lock()
	ret := tf.tcb.shutdown(read, write)
	tf.tcb.tcb_unlock()
	return ret
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

	ts := tcpsegs_t{}
	ts.addnow(0, 5, 40)
	ts.addnow(5, 5, 40)
	ts.addnow(10, 5, 40)
	ts.addnow(15, 5, 40)
	ts.addnow(20, 5, 40)
	ts.addnow(25, 5, 40)
	ts.addnow(30, 5, 40)
	ts.addnow(35, 5, 40)
	ts.reset(0, 5)
	ts.reset(0, 40)
	ts.ackupto(15)
	//fmt.Printf("%v\n", ts)
}
