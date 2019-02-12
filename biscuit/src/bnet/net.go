package bnet

import "fmt"
import "math/rand"
import "runtime"
import "sync"
import "sync/atomic"
import "sort"
import "time"
import "unsafe"

import "bounds"
import "circbuf"
import "defs"
import "fdops"
import "limits"
import "mem"
import "proc"
import "res"
import "stat"
import "util"
import "vm"

import . "inet"

var pagemem mem.Page_i

var arptbl struct {
	sync.Mutex
	m          map[Ip4_t]*arprec_t
	enttimeout time.Duration
	restimeout time.Duration
	// waiters for arp resolution
	waiters map[Ip4_t][]chan bool
	waittot int
}

type arprec_t struct {
	mac    Mac_t
	expire time.Time
}

func arp_add(ip Ip4_t, mac *Mac_t) {
	arptbl.Lock()
	defer arptbl.Unlock()

	now := time.Now()
	for k, v := range arptbl.m {
		if v.expire.Before(now) {
			delete(arptbl.m, k)
		}
	}
	if len(arptbl.m) >= limits.Syslimit.Arpents {
		// first evict resolved arp entries.
		evict := 0
		for ip := range arptbl.m {
			if _, ok := arptbl.waiters[ip]; !ok {
				delete(arptbl.m, ip)
				evict++
			}
			if evict > 3 {
				break
			}
		}
		// didn't get enough, evict unresolved arp entries. the waiting
		// threads will timeout.
		if evict == 0 {
			for ip, chans := range arptbl.waiters {
				delete(arptbl.waiters, ip)
				arptbl.waittot -= len(chans)
				evict++
				if evict > 3 {
					break
				}
			}
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
		arptbl.waittot -= len(wl)
	}
}

func _arp_lookup(ip Ip4_t) (*arprec_t, bool) {
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

// returns destination mac and error
func Arp_resolve(sip, dip Ip4_t) (*Mac_t, defs.Err_t) {
	nic, ok := Nic_lookup(sip)
	if !ok {
		return nil, -defs.ENETDOWN
	}

	if dip == lo.lip {
		return &lo.mac, 0
	}

	arptbl.Lock()

evictrace:
	ar, ok := _arp_lookup(dip)
	if ok {
		arptbl.Unlock()
		return &ar.mac, 0
	}

	if arptbl.waittot >= limits.Syslimit.Arpents {
		arptbl.Unlock()
		return nil, -defs.ENOMEM
	}

	// buffered channel so that a wakeup racing with timeout doesn't
	// eternally block the waker
	mychan := make(chan bool, 1)
	// start a new arp request?
	wl, ok := arptbl.waiters[dip]
	needstart := !ok
	if needstart {
		arptbl.waiters[dip] = []chan bool{mychan}
		_net_arp_start(nic, sip, dip)
	} else {
		arptbl.waiters[dip] = append(wl, mychan)
	}
	arptbl.waittot++
	arptbl.Unlock()

	var timeout bool
	select {
	case <-mychan:
		timeout = false
	case <-time.After(arptbl.restimeout):
		timeout = true
	}

	arptbl.Lock()

	arptbl.waittot--
	if timeout {
		// remove my channel from waiters
		wl, ok := arptbl.waiters[dip]
		if ok {
			for i := range wl {
				if wl[i] == mychan {
					copy(wl[i:], wl[i+1:])
					wl = wl[:len(wl)-1]
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
		return nil, -defs.ETIMEDOUT
	}

	ar, ok = _arp_lookup(dip)
	if !ok {
		goto evictrace
	}
	arptbl.Unlock()
	return &ar.mac, 0
}

func _net_arp_start(nic nic_i, lip, qip Ip4_t) {
	var arp Arpv4_t
	arp.Init_req(nic.Lmac(), lip, qip)
	buf := arp.Bytes()
	sgbuf := [][]uint8{buf}
	nic.Tx_raw(sgbuf)
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
	toip := Sl2ip(buf[24:])
	nic, ok := Nic_lookup(toip)
	if !ok {
		return
	}
	fromip := Sl2ip(buf[14:])
	var frommac Mac_t
	copy(frommac[:], buf[8:])

	arpop := Be16(util.Readn(buf, 2, 6))
	request := Htons(1)
	reply := Htons(2)
	switch arpop {
	case reply:
		arp_add(fromip, &frommac)
	case request:
		// add the sender to our arp table
		arp_add(fromip, &frommac)
		var rep Arpv4_t
		rep.Init_reply(nic.Lmac(), &frommac, toip, fromip)
		sgbuf := [][]uint8{rep.Bytes()}
		nic.Tx_raw(sgbuf)
	}
}

type routes_t struct {
	// sorted (ascending order) slice of bit number of least significant
	// bit in each subnet mask
	subnets []int
	// map of subnets to the owning IP of the destination ethernet MAC
	routes map[uint64]rtentry_t
	defgw  struct {
		myip  Ip4_t
		ip    Ip4_t
		valid bool
	}
}

type rtentry_t struct {
	myip Ip4_t
	gwip Ip4_t
	// netmask shift
	shift int
	// if gateway is true, gwip is the IP of the gateway for this subnet
	gateway bool
}

func (r *routes_t) init() {
	r.routes = make(map[uint64]rtentry_t)
	r.defgw.valid = false
}

func (r *routes_t) _defaultgw(myip, gwip Ip4_t) {
	r.defgw.myip = myip
	r.defgw.ip = gwip
	r.defgw.valid = true
}

func (r *routes_t) _insert(myip, netip, netmask, gwip Ip4_t, isgw bool) {
	if netmask == 0 || netmask == ^Ip4_t(0) || netip&netmask == 0 {
		panic("not a subnet or default gw")
	}
	var bit int
	for bit = 0; bit < 32; bit++ {
		if netmask&(1<<uint(bit)) != 0 {
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

func (r *routes_t) _insert_gateway(myip, netip, netmask, gwip Ip4_t) {
	r._insert(myip, netip, netmask, gwip, true)
}

func (r *routes_t) _insert_local(myip, netip, netmask Ip4_t) {
	r._insert(myip, netip, netmask, 0, false)
}

// caller must hold r's lock
func (r *routes_t) copy() (*routes_t, defs.Err_t) {
	if len(r.routes) >= limits.Syslimit.Routes {
		return nil, -defs.ENOMEM
	}
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
	return ret, 0
}

func (r *routes_t) dump() {
	fmt.Printf("\nRoutes:\n")
	fmt.Printf("  %20s    %16s  %16s\n", "net", "NIC IP", "gateway")
	if r.defgw.valid {
		net := Ip2str(0) + "/0"
		mine := Ip2str(r.defgw.myip)
		dip := Ip2str(r.defgw.ip)
		fmt.Printf("  %20s -> %16s  %16s\n", net, mine, dip)
	}
	for sub, rt := range r.routes {
		s := rt.shift
		net := Ip2str(Ip4_t(sub)<<uint(s)) +
			fmt.Sprintf("/%d", 32-s)
		mine := Ip2str(rt.myip)
		dip := "X"
		if rt.gateway {
			dip = Ip2str(rt.gwip)
		}
		fmt.Printf("  %20s -> %16s  %16s\n", net, mine, dip)
	}
}

// returns the local IP assigned to the NIC where the destination is reachable,
// the IP whose MAC address the packet to the destination IP should be sent,
// and error
func (r *routes_t) lookup(dip Ip4_t) (Ip4_t, Ip4_t, defs.Err_t) {
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
		return 0, 0, -defs.EHOSTUNREACH
	}
	return r.defgw.myip, r.defgw.ip, 0
}

// RCU protected routing table
type routetbl_t struct {
	// lock for RCU writers
	sync.Mutex
	routes *routes_t
}

func (rt *routetbl_t) init() {
	rt.routes = &routes_t{}
	rt.routes.init()
}

func (rt *routetbl_t) Dump() {
	rt.routes.dump()
}

func (rt *routetbl_t) insert_gateway(myip, netip, netmask, gwip Ip4_t) defs.Err_t {
	rt.Lock()
	defer rt.Unlock()

	newroutes, err := rt.routes.copy()
	if err != 0 {
		return err
	}
	newroutes._insert_gateway(myip, netip, netmask, gwip)
	rt.commit(newroutes)
	return 0
}

func (rt *routetbl_t) Insert_local(myip, netip, netmask Ip4_t) defs.Err_t {
	rt.Lock()
	defer rt.Unlock()

	newroutes, err := rt.routes.copy()
	if err != 0 {
		return err
	}
	newroutes._insert_local(myip, netip, netmask)
	rt.commit(newroutes)
	return 0
}

func (rt *routetbl_t) Defaultgw(myip, gwip Ip4_t) defs.Err_t {
	rt.Lock()
	defer rt.Unlock()

	newroutes, err := rt.routes.copy()
	if err != 0 {
		return err
	}
	newroutes._defaultgw(myip, gwip)
	rt.commit(newroutes)
	return 0
}

func (rt *routetbl_t) commit(newroutes *routes_t) {
	dst := (*unsafe.Pointer)(unsafe.Pointer(&rt.routes))
	v := unsafe.Pointer(newroutes)
	atomic.StorePointer(dst, v)
	// store-release on x86, so long as go compiler doesn't reorder the
	// store with the caller
}

// returns the local IP, the destination IP (gateway or destination host), and
// error
func (rt *routetbl_t) Lookup(dip Ip4_t) (Ip4_t, Ip4_t, defs.Err_t) {
	if dip == lo.lip {
		return lo.lip, lo.lip, 0
	}
	src := (*unsafe.Pointer)(unsafe.Pointer(&rt.routes))
	// load-acquire on x86
	p := atomic.LoadPointer(src)
	troutes := (*routes_t)(p)
	a, b, c := troutes.lookup(dip)
	return a, b, c
}

var Routetbl routetbl_t

type rstmsg_t struct {
	k      tcpkey_t
	seq    uint32
	ack    uint32
	useack bool
}

var _rstchan chan rstmsg_t

func rst_daemon() {
	for rmsg := range _rstchan {
		res.Kunresdebug()
		res.Kresdebug(res.Onek, "rst daemon")
		localip, routeip, err := Routetbl.Lookup(rmsg.k.rip)
		if err != 0 {
			continue
		}
		nic, ok := Nic_lookup(localip)
		if !ok {
			continue
		}
		dmac, err := Arp_resolve(localip, routeip)
		if err != 0 {
			continue
		}
		pkt := _mkrst(rmsg.seq, rmsg.k.lip, rmsg.k.rip, rmsg.k.lport,
			rmsg.k.rport, nic.Lmac(), dmac)
		if rmsg.useack {
			ackf := uint8(1 << 4)
			pkt.Tcphdr.Flags |= ackf
			pkt.Tcphdr.Ack = Htonl(rmsg.ack)
		}
		eth, iph, tcph := pkt.Hdrbytes()
		sgbuf := [][]uint8{eth, iph, tcph}
		nic.Tx_tcp(sgbuf)
	}
}

// i don't want our packet-processing event-loop to block on ARP resolution (or
// anything that may take seconds), so i use a different goroutine to
// resolve/transmit to an address.
var icmp_echos = make(chan []uint8, 30)

func icmp_daemon() {
	for {
		buf := <-icmp_echos
		res.Kunresdebug()
		res.Kresdebug(res.Onek, "icmp daemon")
		buf = buf[ETHERLEN:]

		fromip := Sl2ip(buf[12:])
		//fmt.Printf("** GOT PING from %s\n", Ip2str(fromip))

		localip, routeip, err := Routetbl.Lookup(fromip)
		if err != 0 {
			fmt.Printf("ICMP route failure\n")
			continue
		}
		nic, ok := Nic_lookup(localip)
		if !ok {
			panic("no such nic")
		}
		dmac, err := Arp_resolve(localip, routeip)
		if err != 0 {
			panic("arp failed for ICMP echo")
		}

		ident := Be16(util.Readn(buf, 2, IP4LEN+4))
		seq := Be16(util.Readn(buf, 2, IP4LEN+6))
		origdata := buf[IP4LEN+8:]
		echodata := make([]uint8, len(origdata))
		copy(echodata, origdata)
		var reply Icmppkt_t
		reply.Init(nic.Lmac(), dmac, localip, fromip, 0, echodata)
		reply.Ident = ident
		reply.Seq = seq
		reply.Crc()
		txpkt := [][]uint8{reply.Hdrbytes(), echodata}
		nic.Tx_ipv4(txpkt)
		if len(origdata) >= 34 {
			tmp := origdata[32:]
			// use Linux's or OpenBSD's "ping -p beef" to trigger
			if tmp[0] == 0xbe && tmp[1] == 0xef {
				// ...
			}
		}
	}
}

func net_icmp(pkt [][]uint8, tlen int) {
	buf := pkt[0]
	buf = buf[ETHERLEN:]

	// min ICMP header length is 8
	if len(buf) < IP4LEN+8 {
		return
	}
	icmp_reply := uint8(0)
	icmp_echo := uint8(8)

	// for us?
	toip := Sl2ip(buf[16:])
	if _, ok := Nic_lookup(toip); !ok {
		return
	}

	fromip := Sl2ip(buf[12:])
	icmp_type := buf[IP4LEN]
	switch icmp_type {
	case icmp_reply:
		when := int64(util.Readn(buf, 8, IP4LEN+8))
		elap := float64(time.Now().UnixNano() - when)
		elap /= 1000
		fmt.Printf("** ping reply from %s took %v us\n",
			Ip2str(fromip), elap)
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

// allocates two pages for the send/receive buffers. returns false if it failed
// to allocate pages. does not increase the reference count of the pages
// (circbuf_t should do that).
func tcppgs() ([]uint8, mem.Pa_t, []uint8, mem.Pa_t, bool) {
	spg, sp_pg, ok := pagemem.Refpg_new_nozero()
	if !ok {
		return nil, 0, nil, 0, false
	}
	rpg, rp_pg, ok := pagemem.Refpg_new_nozero()
	if !ok {
		// ...
		pagemem.Refup(sp_pg)
		pagemem.Refdown(sp_pg)
		return nil, 0, nil, 0, false
	}
	return mem.Pg2bytes(spg)[:], sp_pg, mem.Pg2bytes(rpg)[:], rp_pg, true
}

type tcpbuf_t struct {
	cbuf    circbuf.Circbuf_t
	seq     uint32
	didseq  bool
	cond    *sync.Cond
	pollers *fdops.Pollers_t
}

func (tb *tcpbuf_t) tbuf_init(v []uint8, p_pg mem.Pa_t, tcb *Tcptcb_t) {
	tb.cbuf.Cb_init_phys(v, p_pg, pagemem)
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
	return uint32(uint(tb.seq) + uint(tb.cbuf.Used()))
}

func (tb *tcpbuf_t) _sanity() {
	if !tb.didseq {
		panic("no sequence")
	}
}

// writes rxdata to the circular buffer, allows user to read written data (if
// next in the sequence) and returns number of bytes written. cannot fail.
func (tb *tcpbuf_t) syswrite(nseq uint32, rxdata [][]uint8) {
	tb._sanity()
	if !_seqbetween(tb.seq, nseq, tb.seq+uint32(tb.cbuf.Left())) {
		panic("bad sequence number")
	}
	var dlen int
	for _, r := range rxdata {
		dlen += len(r)
	}
	if dlen == 0 {
		panic("nothing to write")
	}
	if dlen > tb.cbuf.Left() {
		panic("should be pruned to window")
	}
	off := _seqdiff(nseq, tb.seq)
	var did int
	for _, r := range rxdata {
		dst1, dst2 := tb.cbuf.Rawwrite(off+did, len(r))
		did1 := copy(dst1, r)
		did2 := copy(dst2, r[did1:])
		did += did1 + did2
	}
	// XXXPANIC
	if did != dlen {
		panic("nyet")
	}
}

// advances the circular buffer head, allowing the user application to read
// bytes [tb.seq, upto), and increases tb.seq.
func (tb *tcpbuf_t) rcvup(upto uint32) {
	if !_seqbetween(tb.seq, upto, tb.seq+uint32(len(tb.cbuf.Buf))) {
		panic("upto not in window")
	}
	did := _seqdiff(upto, tb.seq)
	tb.seq += uint32(did)
	tb.cbuf.Advhead(did)
	tb.cond.Broadcast()
	tb.pollers.Wakeready(fdops.R_READ)
}

// returns slices referencing the data written by the user (two when the
// buffers wrap the circular buffer) whose total length is at most l. does not
// free up the buffer by advancing the tail (that happens once data is acked).
func (tb *tcpbuf_t) sysread(nseq uint32, l int) ([]uint8, []uint8) {
	tb._sanity()
	if !_seqbetween(tb.seq, nseq, tb.seq+uint32(tb.cbuf.Used())) {
		panic("bad sequence number")
	}
	off := _seqdiff(nseq, tb.seq)
	s1, s2 := tb.cbuf.Rawread(off)
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
	if !_seqbetween(tb.seq, rack, tb.seq+uint32(tb.cbuf.Used())) {
		panic("ack out of window")
	}
	sz := _seqdiff(rack, tb.seq)
	if sz == 0 {
		return
	}
	tb.cbuf.Advtail(sz)
	tb.seq += uint32(sz)
	tb.cond.Broadcast()
	tb.pollers.Wakeready(fdops.R_WRITE)
}

type tseg_t struct {
	seq  uint32
	len  uint32
	when millis_t
}

type tcpsegs_t struct {
	segs   []tseg_t
	winend uint32
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
			if _seqbetween(prev.seq, seq, prev.seq+prev.len-1) {
				panic("overlap")
			}
		}
		if i < len(ts.segs)-1 {
			next := ts.segs[i+1]
			if next.seq == seq {
				panic("same seqs")
			}
			nend := next.seq + next.len
			if _seqbetween(next.seq, seq+slen-1, nend) {
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
func (ts *tcpsegs_t) addnow(seq, l, winend uint32) {
	if len(ts.segs) >= limits.Syslimit.Tcpsegs {
		// to prevent allocating too many segments, collapse some
		// segments together, thus we may retransmit some segments
		// sooner than their actual timeout.
		tlen := uint32(0)
		for i := range ts.segs {
			tlen += ts.segs[i].len
		}
		ts.segs[0].len = tlen
		ts.segs = ts.segs[:1]
	}
	ts._addnoisect(seq, 1, winend)
	ts.reset(seq, l)
}

func (ts *tcpsegs_t) _addnoisect(seq, len, winend uint32) {
	if len == 0 {
		panic("0 length transmit seg?")
	}
	ns := tseg_t{seq: seq, len: len}
	ns.when = Fastmillis()
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
		if _seqbetween(tseq, seq, tseq+tlen-1) {
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
		ts.segs[start].when = Fastmillis()
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
		if _seqbetween(tseq, seq+nlen, tseq+tlen-1) {
			oend := tseq + tlen
			ts.segs[i].seq = seq + nlen
			ts.segs[i].len = uint32(_seqdiff(oend, seq+nlen))
			break
		}
		prunefrom++
	}
	copy(ts.segs[start+1:], ts.segs[prunefrom:])
	rest := len(ts.segs) - prunefrom
	ts.segs = ts.segs[:start+rest+1]
	ts.segs[start].len = nlen
	ts.segs[start].when = Fastmillis()
}

// prune unacknowledged sequences if the window has shrunk
func (ts *tcpsegs_t) prune(winend uint32) {
	prunefrom := len(ts.segs)
	for i := range ts.segs {
		seq := ts.segs[i].seq
		l := ts.segs[i].len
		if _seqbetween(seq, winend, seq+l-1) {
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
func (ts *tcpsegs_t) nextts() (millis_t, bool) {
	var ret millis_t
	if len(ts.segs) > 0 {
		ret = ts.segs[0].when
		for i := range ts.segs {
			if ts.segs[i].when < ret {
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

type tcprsegs_t struct {
	segs   []tseg_t
	winend uint32
}

// segment [seq, seq+l) has been received. if seq != rcvnxt, the segment
// has apparently been received out of order; thus record the segment seq and
// len in order to coalesce this segment with the one containing rcv.nxt, once
// it is received.
func (tr *tcprsegs_t) recvd(rcvnxt, winend, seq uint32, l int) uint32 {
	if l == 0 {
		panic("0 len rseg")
	}
	tr.winend = winend
	// expected common case: no recorded received segments because segments
	// have been received in order
	if len(tr.segs) == 0 && rcvnxt == seq {
		return rcvnxt + uint32(l)
	}

	tr.segs = append(tr.segs, tseg_t{seq: seq, len: uint32(l)})
	sort.Sort(tr)
	// segs are now in order by distance from window end to b.seq; coalesce
	// adjacent segments
	for i := 0; i < len(tr.segs)-1; i++ {
		s := tr.segs[i]
		b := tr.segs[i+1]
		d1 := _seqdiff(tr.winend, s.seq+s.len)
		d2 := _seqdiff(tr.winend, b.seq)
		if d1 > d2 {
			// non-intersecting
			continue
		}
		send := s.seq + s.len
		bend := b.seq + b.len
		sd := _seqdiff(tr.winend, send)
		bd := _seqdiff(tr.winend, bend)
		var newend uint32
		if sd > bd {
			newend = bend
		} else {
			newend = send
		}
		s.len = newend - s.seq
		tr.segs[i] = s
		copy(tr.segs[i+1:], tr.segs[i+2:])
		tr.segs = tr.segs[:len(tr.segs)-1]
		i--
	}
	// keep the amount of memory used by remembering out-of-order segments
	// bounded -- forget that we received the non-coalesced segments that
	// are farthest from rcv.nxt (in hopes that we can advance rcv.nxt as
	// far as possible).
	if len(tr.segs) > limits.Syslimit.Tcpsegs {
		tr.segs = tr.segs[:limits.Syslimit.Tcpsegs]
	}
	if rcvnxt == tr.segs[0].seq {
		ret := rcvnxt + tr.segs[0].len
		copy(tr.segs, tr.segs[1:])
		tr.segs = tr.segs[:len(tr.segs)-1]
		return ret
	}
	return rcvnxt
}

func (tr *tcprsegs_t) Len() int {
	return len(tr.segs)
}

func (tr *tcprsegs_t) Less(i, j int) bool {
	diff1 := _seqdiff(tr.winend, tr.segs[i].seq)
	diff2 := _seqdiff(tr.winend, tr.segs[j].seq)
	return diff1 > diff2
}

func (tr *tcprsegs_t) Swap(i, j int) {
	tr.segs[i], tr.segs[j] = tr.segs[j], tr.segs[i]
}

// a type for a listening socket
type tcplisten_t struct {
	l     sync.Mutex
	lip   Ip4_t
	lport uint16
	// map of IP/ports to which we've sent SYN+ACK
	seqs map[tcpkey_t]tcpinc_t
	// ready connections
	rcons struct {
		sl   []*Tcptcb_t
		inum uint
		cnum uint
		cond *sync.Cond
	}
	openc   int
	pollers fdops.Pollers_t
	sndsz   int
	rcvsz   int
}

type tcpinc_t struct {
	lip   Ip4_t
	rip   Ip4_t
	lport uint16
	rport uint16
	smac  *Mac_t
	dmac  *Mac_t
	rcv   struct {
		nxt uint32
	}
	snd struct {
		win uint16
		nxt uint32
	}
	opt  Tcpopt_t
	bufs struct {
		sp    []uint8
		sp_pg mem.Pa_t
		rp    []uint8
		rp_pg mem.Pa_t
	}
}

func (tcl *tcplisten_t) tcl_init(lip Ip4_t, lport uint16, backlog int) {
	tcl.lip = lip
	tcl.lport = lport
	tcl.seqs = make(map[tcpkey_t]tcpinc_t)
	tcl.rcons.sl = make([]*Tcptcb_t, backlog)
	tcl.rcons.inum = 0
	tcl.rcons.cnum = 0
	tcl.rcons.cond = sync.NewCond(&tcl.l)
	tcl.openc = 1
}

func (tcl *tcplisten_t) _conadd(tcb *Tcptcb_t) {
	rc := &tcl.rcons
	l := uint(len(rc.sl))
	if rc.inum-rc.cnum == l {
		// backlog full
		return
	}
	rc.sl[rc.inum%l] = tcb
	rc.inum++
	rc.cond.Signal()
	tcl.pollers.Wakeready(fdops.R_READ)
}

func (tcl *tcplisten_t) _contake() (*Tcptcb_t, bool) {
	rc := &tcl.rcons
	if rc.inum == rc.cnum {
		// no connections
		return nil, false
	}
	l := uint(len(rc.sl))
	ret := rc.sl[rc.cnum%l]
	rc.cnum++
	return ret, true
}

// creates a TCB for tinc and adds it to the table of established connections
func (tcl *tcplisten_t) tcbready(tinc tcpinc_t, rack uint32, rwin uint16,
	ropt Tcpopt_t, rest [][]uint8, sp []uint8, sp_pg mem.Pa_t,
	rp []uint8, rp_pg mem.Pa_t) *Tcptcb_t {
	tcb := &Tcptcb_t{}
	tcb.tcb_init(tinc.lip, tinc.rip, tinc.lport, tinc.rport, tinc.smac,
		tinc.dmac, tinc.snd.nxt, sp, sp_pg, rp, rp_pg)
	tcb.bound = true
	tcb.state = ESTAB
	tcb.set_seqs(tinc.snd.nxt, tinc.rcv.nxt)
	tcb.snd.win = tinc.snd.win
	tcb.snd.mss = tinc.opt.Mss

	tcb.snd.wl1 = tinc.rcv.nxt
	tcb.snd.wl2 = tinc.snd.nxt

	var dlen int
	for _, r := range rest {
		dlen += len(r)
	}

	tcb.tcb_lock()
	tcb.data_in(tcb.rcv.nxt, rack, rwin, rest, dlen, ropt.Tsval)
	tcb.tcb_unlock()

	tcpcons.tcb_linsert(tcb)

	return tcb
}

func (tcl *tcplisten_t) incoming(rmac []uint8, tk tcpkey_t, ip4 *Ip4hdr_t,
	tcp *Tcphdr_t, opt Tcpopt_t, rest [][]uint8) {

	ack, ok := tcp.Isack()
	if ok {
		// handshake complete?
		tinc, ok := tcl.seqs[tk]
		if !ok || tcp.Issyn() {
			send_rst(ack, tk)
			return
		}
		seq := Ntohl(tcp.Seq)
		if tinc.rcv.nxt != seq || tinc.snd.nxt != ack {
			// send ack if rst not set?
			return
		}
		delete(tcl.seqs, tk)
		if tcp.Isrst() {
			return
		}
		// make the connection immediately ready so that received
		// segments that race with accept(2) aren't unnecessarily timed
		// out.
		b := &tinc.bufs
		tcb := tcl.tcbready(tinc, ack, Ntohs(tcp.Win), opt, rest,
			b.sp, b.sp_pg, b.rp, b.rp_pg)
		tcl._conadd(tcb)
		return
	}

	if tcp.Isrst() {
		return
	}

	if !tcp.Issyn() {
		return
	}

	nic, ok := Nic_lookup(tk.lip)
	if !ok {
		panic("no such nic")
	}

	if !opt.Tsok {
		fmt.Printf("no listen ts!\n")
	}

	if !limits.Syslimit.Socks.Take() {
		limits.Lhits++
		return
	}

	sp, sp_pg, rp, rp_pg, ok := tcppgs()
	if !ok {
		limits.Lhits++
		limits.Syslimit.Socks.Give()
		return
	}

	ourseq := rand.Uint32()
	theirseq := Ntohl(tcp.Seq)
	newcon := tcpinc_t{lip: tk.lip, rip: tk.rip, lport: tk.lport,
		rport: tk.rport, opt: opt}
	b := &newcon.bufs
	b.sp, b.sp_pg = sp, sp_pg
	b.rp, b.rp_pg = rp, rp_pg
	newcon.rcv.nxt = theirseq + 1
	newcon.snd.nxt = ourseq + 1
	newcon.snd.win = Ntohs(tcp.Win)
	smac := nic.Lmac()
	dmac := rmac
	newcon.smac = smac
	newcon.dmac = &Mac_t{}
	copy(newcon.dmac[:], dmac)

	tcl.seqs[tk] = newcon
	defwin := uint16(2048)
	pkt, mopt := _mksynack(smac, dmac, tk, defwin, ourseq, theirseq+1,
		opt.Tsval)
	eth, ip, tph := pkt.Hdrbytes()
	sgbuf := [][]uint8{eth, ip, tph, mopt}
	nic.Tx_tcp(sgbuf)
}

// the owning tcb must be locked before calling any tcptlist_t methods.
type tcptlist_t struct {
	next   *tcptlist_t
	prev   *tcptlist_t
	tcb    *Tcptcb_t
	bucket int
}

func (tl *tcptlist_t) linit(tcb *Tcptcb_t) {
	tl.tcb = tcb
	tl.clear()
}

// zeroes the pointers for debugging and returns the next list item
func (tl *tcptlist_t) clear() *Tcptcb_t {
	var ret *Tcptcb_t
	if tl.next != nil {
		ret = tl.next.tcb
	}
	tl.next, tl.prev, tl.bucket = nil, nil, -1
	return ret
}

// timerwheel is not thread-safe
type timerwheel_t struct {
	ep      time.Time
	gran    time.Duration
	lbucket int
	bucks   []*tcptlist_t
}

// must call dormant() before first timer is added in order to determine the
// current bucket.
func (tw *timerwheel_t) twinit(gran, width time.Duration) {
	tw.gran = gran
	sz := width / gran
	tw.bucks = make([]*tcptlist_t, sz)
	tw.lbucket = -1
}

var _ztime time.Time

// inserts tl into the wheel linked list with deadline, updating tcptlist_t's
// pointers and bucket. the containting tcb must be locked.
func (tw *timerwheel_t) toadd(deadline time.Time, tl *tcptlist_t) {
	if tl.bucket != -1 || tl.next != nil || tl.prev != nil || tw.ep == _ztime {
		panic("oh noes")
	}
	bn := int(deadline.Sub(tw.ep)/tw.gran) % len(tw.bucks)
	tl.bucket = bn
	//if bn == tw.lbucket {
	//	fmt.Printf("vewy suspicious widf (diff: %v)\n", time.Until(deadline).Nanoseconds())
	//}
	tl.next = tw.bucks[bn]
	tl.prev = nil
	if tw.bucks[bn] != nil {
		tw.bucks[bn].prev = tl
	}
	tw.bucks[bn] = tl
}

// returns the Time of the oldest timeout or false if there are no more
// timeouts.
func (tw *timerwheel_t) nextto(now time.Time) (time.Time, bool) {
	for i := 0; i < len(tw.bucks); i++ {
		bn := (tw.lbucket + i) % len(tw.bucks)
		if tw.bucks[bn] != nil {
			then := now.Add(time.Duration(i) * tw.gran)
			return then, true
		}
	}
	var z time.Time
	return z, false
}

// returns all the lists between the last bucket processed and the bucket
// belonging to now, if any. also makes sure concurrent list removals won't
// touch the returned lists.
func (tw *timerwheel_t) advance_to(now time.Time) []*Tcptcb_t {
	maxbn := int(now.Sub(tw.ep)/tw.gran) % len(tw.bucks)
	if tw.lbucket == maxbn {
		return nil
	}
	var ret []*Tcptcb_t
	for tw.lbucket != maxbn {
		tw.lbucket = (tw.lbucket + 1) % len(tw.bucks)
		p := tw.bucks[tw.lbucket]
		if p != nil {
			ret = append(ret, p.tcb)
			tw.bucks[tw.lbucket] = nil
		}
	}
	return ret
}

func (tw *timerwheel_t) torem(tl *tcptlist_t) {
	// tl is not in the list
	if tl.bucket == -1 {
		return
	}
	// make sure list removals are allowed. tcbs are not allowed to remove
	// themselves from a list while the timer daemon is processing the
	// timers (though they could be if the pointer zeroing is removed).
	if tw.bucks[tl.bucket] == nil {
		// removes not allowed
		return
	}
	if tl.next != nil {
		tl.next.prev = tl.prev
	}
	if tl.prev != nil {
		tl.prev.next = tl.next
	} else {
		tw.bucks[tl.bucket] = tl.next
	}
	tl.clear()
}

func (tw *timerwheel_t) dormant(now time.Time) {
	tw.ep = now
	tw.lbucket = 0
}

type tcptimers_t struct {
	// tcptimers_t.l is a leaf lock
	l sync.Mutex
	// 1-element buffered channel
	kicker chan bool
	// cvalid is true iff curto is valid; otherwise there is no outstanding
	// timeout
	cvalid bool
	curto  time.Time
	// ackw granularity: 10ms, width: 1s
	ackw timerwheel_t
	// txw granularity: 100ms, width: 10s
	txw timerwheel_t
	// twaitw granularity: 1s, width: 2m
	twaitw timerwheel_t
}

var bigtw = &tcptimers_t{}

func (tt *tcptimers_t) _tcptimers_start() {
	tt.ackw.twinit(10*time.Millisecond, time.Second)
	tt.txw.twinit(100*time.Millisecond, 10*time.Second)
	tt.twaitw.twinit(time.Second, 2*time.Minute)
	tt.kicker = make(chan bool, 1)
	go tt._tcptimers_daemon()
}

func (tt *tcptimers_t) _tcptimers_daemon() {
	var curtoc <-chan time.Time
	r := bounds.Bounds(bounds.B_TCPTIMERS_T__TCPTIMERS_DAEMON)
	res.Kreswait(r, "tcp timers thread")
	for {
		dotos := false
		select {
		case <-curtoc:
			dotos = true
		case <-tt.kicker:
		}

		res.Kunres()
		res.Kreswait(r, "tcp timers thread")

		var acklists []*Tcptcb_t
		var txlists []*Tcptcb_t
		var twaitlists []*Tcptcb_t

		tt.l.Lock()
		now := time.Now()
		if dotos {
			acklists = tt.ackw.advance_to(now)
			txlists = tt.txw.advance_to(now)
			twaitlists = tt.twaitw.advance_to(now)
		}
		tt.cvalid = false
		ws := []*timerwheel_t{&tt.ackw, &tt.txw, &tt.twaitw}
		for _, w := range ws {
			if newto, ok := w.nextto(now); ok {
				tt.cvalid = true
				tt.curto = newto
				break
			}
		}
		if tt.cvalid {
			curtoc = time.After(time.Until(tt.curto))
		}
		tt.l.Unlock()

		if dotos {
			// we cannot process the timeouts while tcptimers_t is
			// locked, since Tcptcb_t locks in the opposite order
			// since it is convenient to add to the timeout list
			// while a tcb is locked.
			for _, list := range acklists {
				var next *Tcptcb_t
				for tcb := list; tcb != nil; tcb = next {
					tcb.tcb_lock()
					tcb.remack.tstart = false
					next = tcb.ackl.clear()
					tcb.ack_maybe()
					tcb.tcb_unlock()
				}
			}
			for _, list := range txlists {
				var next *Tcptcb_t
				for tcb := list; tcb != nil; tcb = next {
					tcb.tcb_lock()
					tcb.remseg.tstart = false
					next = tcb.txl.clear()
					tcb.seg_maybe()
					tcb.tcb_unlock()
				}
			}
			for _, list := range twaitlists {
				var next *Tcptcb_t
				for tcb := list; tcb != nil; tcb = next {
					tcb.tcb_lock()
					next = tcb.twaitl.clear()
					if !tcb.dead {
						tcb.kill()
					}
					tcb.tcb_unlock()
				}
			}
		}
	}
}

// our timewheel doesn't use timer ticks to track the passage of time -- it
// waits until the minimum timeout finishes, then processes all buckets between
// the last timeout and the current bucket.  well if there were previously no
// timers, the current bucket indicies will be garbage. _was_dormant
// reinitializes them.
func (tt *tcptimers_t) _was_dormant() {
	now := time.Now()
	tt.ackw.dormant(now)
	tt.txw.dormant(now)
	tt.twaitw.dormant(now)
}

// tcb must be locked.
func (tt *tcptimers_t) tosched_ack(tcb *Tcptcb_t) {
	tcb._sanity()
	dline := time.Now().Add(500 * time.Millisecond)
	tt._tosched(&tcb.ackl, &tt.ackw, dline)
}

// tcb must be locked.
func (tt *tcptimers_t) tosched_tx(tcb *Tcptcb_t) {
	tcb._sanity()
	dline := time.Now().Add(5 * time.Second)
	tt._tosched(&tcb.txl, &tt.txw, dline)
}

// tcb must be locked.
func (tt *tcptimers_t) tosched_twait(tcb *Tcptcb_t) {
	tcb._sanity()
	dline := time.Now().Add(1 * time.Minute)
	tt._tosched(&tcb.twaitl, &tt.twaitw, dline)
}

func (tt *tcptimers_t) _tosched(tl *tcptlist_t, tw *timerwheel_t,
	dline time.Time) {

	kickit := true
	tt.l.Lock()
	cur := tt.curto
	if tt.cvalid {
		if dline.After(cur) {
			kickit = false
		}
	} else {
		tt._was_dormant()
		tt.cvalid = true
		tt.curto = dline
	}
	tw.toadd(dline, tl)
	tt.l.Unlock()

	// make sure timer goroutine observes our timeout which will apparently
	// expire sooner than all the other timeouts. we cannot simply block on
	// the kicker channel since the timer goroutine may be blocking, trying
	// to lock the caller's tcb, thus we would deadlock. this dance is
	// expected to rarely occur.
	if kickit {
		select {
		case tt.kicker <- true:
		default:
		}
		// the timer goroutine won't sleep on a timeout until it has
		// observed our timer.
	}
}

func (tt *tcptimers_t) tocancel_all(tcb *Tcptcb_t) {
	tcb._sanity()

	tt.l.Lock()
	tt._tocancel(&tcb.ackl, &tt.ackw)
	tt._tocancel(&tcb.txl, &tt.txw)
	tt._tocancel(&tcb.twaitl, &tt.twaitw)
	tt.l.Unlock()
}

func (tt *tcptimers_t) _tocancel(tl *tcptlist_t, tw *timerwheel_t) {
	tw.torem(tl)
}

type Tcptcb_t struct {
	// l protects everything except for the following which can be read
	// without the lock: dead, rxdone, txdone, and the head/tail indicies
	// in rxbuf/txbuf.
	l      sync.Mutex
	locked bool
	ackl   tcptlist_t
	txl    tcptlist_t
	twaitl tcptlist_t
	// local/remote ip/ports
	lip   Ip4_t
	rip   Ip4_t
	lport uint16
	rport uint16
	// embed smac and dmac
	smac  *Mac_t
	dmac  *Mac_t
	state tcpstate_t
	// dead indicates that the connection is terminated and shouldn't use
	// more CPU cycles.
	dead bool
	// remote sent FIN
	rxdone bool
	// we sent FIN
	txdone  bool
	twdeath bool
	bound   bool
	openc   int
	pollers fdops.Pollers_t
	rcv     struct {
		nxt    uint32
		win    uint16
		mss    uint16
		trsegs tcprsegs_t
	}
	snd struct {
		nxt    uint32
		una    uint32
		win    uint16
		mss    uint16
		wl1    uint32
		wl2    uint32
		finseq uint32
		tsegs  tcpsegs_t
	}
	tstamp struct {
		recent  uint32
		acksent uint32
	}
	opt []uint8
	// timer state
	remack struct {
		last millis_t
		// number of segments received; we try to send 1 ACK per 2 data
		// segments.
		num        uint
		forcedelay bool
		// outstanding ACK?
		outa   bool
		tstart bool
	}
	remseg struct {
		tstart bool
		target millis_t
	}
	// data to send over the TCP connection
	txbuf tcpbuf_t
	// data received over the TCP connection
	rxbuf tcpbuf_t
	sndsz int
	rcvsz int
}

type tcpstate_t uint

const (
	// the following is for newly created tcbs
	TCPNEW tcpstate_t = iota
	SYNSENT
	SYNRCVD
	// LISTEN is not really used
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

var statestr = map[tcpstate_t]string{
	SYNSENT:   "SYNSENT",
	SYNRCVD:   "SYNRCVD",
	LISTEN:    "LISTEN",
	ESTAB:     "ESTAB",
	FINWAIT1:  "FINWAIT1",
	FINWAIT2:  "FINWAIT2",
	CLOSING:   "CLOSING",
	CLOSEWAIT: "CLOSEWAIT",
	LASTACK:   "LASTACK",
	TIMEWAIT:  "TIMEWAIT",
	TCPNEW:    "TCPNEW",
}

func (tc *Tcptcb_t) tcb_lock() {
	tc.l.Lock()
	tc.locked = true
}

func (tc *Tcptcb_t) tcb_unlock() {
	tc.locked = false
	tc.l.Unlock()
}

// waits on the receive buffer conditional variable
func (tc *Tcptcb_t) rbufwait() defs.Err_t {
	ret := proc.KillableWait(tc.rxbuf.cond)
	tc.locked = true
	return ret
}

func (tc *Tcptcb_t) tbufwait() defs.Err_t {
	ret := proc.KillableWait(tc.txbuf.cond)
	tc.locked = true
	return ret
}

func (tc *Tcptcb_t) _sanity() {
	if !tc.locked {
		panic("tcb must be locked")
	}
}

func (tc *Tcptcb_t) _nstate(old, news tcpstate_t) {
	tc._sanity()
	if tc.state != old {
		panic("bad state transition")
	}
	tc.state = news
	//fmt.Printf("%v -> %v\n", statestr[old], statestr[news])
}

func (tc *Tcptcb_t) _rst() {
	fmt.Printf("tcb reset no imp")
}

func (tc *Tcptcb_t) _tcp_connect(dip Ip4_t, dport uint16) defs.Err_t {
	tc._sanity()
	localip, routeip, err := Routetbl.Lookup(dip)
	if err != 0 {
		return err
	}

	nic, ok := Nic_lookup(localip)
	if !ok {
		return -defs.EHOSTUNREACH
	}
	dmac, err := Arp_resolve(localip, routeip)
	if err != 0 {
		return err
	}

	var wasany bool
	if tc.bound {
		if tc.lip != defs.INADDR_ANY && tc.lip != localip {
			return -defs.ENETUNREACH
		}
		if tc.lip == defs.INADDR_ANY {
			wasany = true
		}
	} else {
		lport, ok := tcpcons.reserve_ephemeral(localip)
		if !ok {
			return -defs.EADDRNOTAVAIL
		}
		tc.lip = localip
		tc.lport = lport
		tc.bound = true
	}

	// do we have enough buffers?
	sp, sp_pg, rp, rp_pg, ok := tcppgs()
	if !ok {
		return -defs.ENOMEM
	}

	tc.tcb_init(localip, dip, tc.lport, dport, nic.Lmac(), dmac,
		rand.Uint32(), sp, sp_pg, rp, rp_pg)
	tcpcons.tcb_insert(tc, wasany)

	tc._nstate(TCPNEW, SYNSENT)
	// XXX retransmit connect attempts
	seq := tc.snd.nxt
	pkt, opts := tc.mkconnect(seq)
	tc.snd.nxt++

	eth, ip, tcp := pkt.Hdrbytes()
	sgbuf := [][]uint8{eth, ip, tcp, opts}
	nic.Tx_tcp(sgbuf)
	return 0
}

func (tc *Tcptcb_t) incoming(tk tcpkey_t, ip4 *Ip4hdr_t, tcp *Tcphdr_t,
	opt Tcpopt_t, rest [][]uint8) {

	tc._sanity()
	//if !opt.tsok {
	//	fmt.Printf("no ts!\n")
	//}
	switch tc.state {
	case SYNSENT:
		tc.synsent(tcp, opt.Mss, opt, rest)
	case ESTAB:
		tc.estab(tcp, opt, rest)
		// send window may have increased
		tc.seg_maybe()
		if !tc.txfinished(ESTAB, FINWAIT1) {
			tc.finchk(ip4, tcp, ESTAB, CLOSEWAIT)
		}
	case CLOSEWAIT:
		// user may queue for send, receive is done
		tc.estab(tcp, opt, rest)
		// send window may have increased
		tc.seg_maybe()
		tc.txfinished(CLOSEWAIT, LASTACK)
	case LASTACK:
		// user may queue for send, receive is done
		tc.estab(tcp, opt, rest)
		if tc.finacked() {
			if tc.txbuf.cbuf.Used() != 0 {
				panic("closing but txdata remains")
			}
			tc.kill()
		} else {
			tc.seg_maybe()
		}
	case FINWAIT1:
		// user may no longer queue for send, may still receive
		tc.estab(tcp, opt, rest)
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
		tc.estab(tcp, opt, rest)
		tc.finchk(ip4, tcp, FINWAIT2, TIMEWAIT)
	case CLOSING:
		// user may no longer queue for send, receive is done
		tc.estab(tcp, opt, rest)
		tc.seg_maybe()
		if tc.finacked() {
			tc._nstate(CLOSING, TIMEWAIT)
		}
	default:
		sn, ok := statestr[tc.state]
		if !ok {
			panic("huh?")
		}
		fmt.Printf("no imp for state %s\n", sn)
		return
	}

	if tc.state == TIMEWAIT {
		tc.timewaitdeath()
		tc.ack_now()
	} else {
		tc.ack_maybe()
	}
}

// if the packet has FIN set and we've acked all data up to the fin, increase
// rcv.nxt to ACK the FIN and move to the next state.
func (tc *Tcptcb_t) finchk(ip4 *Ip4hdr_t, tcp *Tcphdr_t, os, ns tcpstate_t) {
	if os != ESTAB && !tc.txdone {
		panic("txdone must be set")
	}
	hlen := IP4LEN + int(tcp.Dataoff>>4)*4
	tlen := int(Ntohs(ip4.Tlen))
	if tlen < hlen {
		panic("bad packet should be pruned")
	}
	paylen := tlen - hlen
	rfinseq := Ntohl(tcp.Seq) + uint32(paylen)
	if tcp.Isfin() && tc.rcv.nxt == rfinseq {
		tc.rcv.nxt++
		tc._nstate(os, ns)
		tc.rxdone = true
		if len(tc.rcv.trsegs.segs) != 0 {
			panic("have uncoalesced segs")
		}
		tc.rxbuf.cond.Broadcast()
		tc.pollers.Wakeready(fdops.R_READ)
		tc.sched_ack()
	}
}

// if all user data has been sent (we must have sent FIN), move to the next
// state. returns true if this function changed the TCB's state.
func (tc *Tcptcb_t) txfinished(os, ns tcpstate_t) bool {
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
func (tc *Tcptcb_t) finacked() bool {
	if !tc.txdone {
		panic("can't have sent FIN yet")
	}
	if tc.snd.una == tc.snd.finseq+1 {
		return true
	}
	return false
}

func (tc *Tcptcb_t) _bufrelease() {
	tc._sanity()
	if tc.openc == 0 {
		tc.txbuf.cbuf.Cb_release()
		tc.rxbuf.cbuf.Cb_release()
	}
}

func (tc *Tcptcb_t) kill() {
	tc._sanity()
	if tc.dead {
		panic("uh oh")
	}
	tc.dead = true
	tc._bufrelease()
	limits.Syslimit.Socks.Give()
	tcpcons.tcb_del(tc)
	bigtw.tocancel_all(tc)
}

func (tc *Tcptcb_t) timewaitdeath() {
	tc._sanity()
	if tc.twdeath {
		return
	}
	tc.twdeath = true
	tc._bufrelease()

	bigtw.tosched_twait(tc)
}

// sets flag to send an ack which may be delayed.
func (tc *Tcptcb_t) sched_ack() {
	tc.remack.num++
	tc.remack.outa = true
}

func (tc *Tcptcb_t) sched_ack_delay() {
	tc.remack.outa = true
	tc.remack.forcedelay = true
}

func (tc *Tcptcb_t) ack_maybe() {
	tc._acktime(false)
}

func (tc *Tcptcb_t) ack_now() {
	tc._acktime(true)
}

// sendnow overrides the forcedelay flag
func (tc *Tcptcb_t) _acktime(sendnow bool) {
	tc._sanity()
	if !tc.remack.outa || tc.dead {
		return
	}
	fdelay := tc.remack.forcedelay
	tc.remack.forcedelay = false

	segdelay := tc.remack.num%2 == 0
	canwait := !sendnow && (segdelay || fdelay)
	if canwait {
		// delay at most 500ms
		now := Fastmillis()
		if now < tc.remack.last + 500 {
			if !tc.remack.tstart {
				tc.remack.tstart = true
				bigtw.tosched_ack(tc)
			}
			return
		}
	}
	tc.remack.outa = false

	pkt, opt := tc.mkack(tc.snd.nxt, tc.rcv.nxt)
	tc.tstamp.acksent = Ntohl(pkt.Tcphdr.Ack)
	nic, ok := Nic_lookup(tc.lip)
	if !ok {
		fmt.Printf("NIC gone!\n")
		tc.kill()
		return
	}
	eth, ip, th := pkt.Hdrbytes()
	sgbuf := [][]uint8{eth, ip, th, opt}
	nic.Tx_tcp(sgbuf)
	tc.remack.last = Fastmillis()
}

type millis_t uint

const Secondms millis_t = 1000

func Fastmillis() millis_t {
	ns := millis_t(runtime.Nanotime())
	return ns / 1000000
}

// transmit new segments and retransmits timed-out segments as the window
// allows
func (tc *Tcptcb_t) seg_maybe() {
	tc._sanity()
	if tc.twdeath || tc.dead || (tc.txdone && tc.finacked()) {
		// everything has been acknowledged
		return
	}
	winend := tc.snd.una + uint32(tc.snd.win)
	// prune unacknowledged segments which are now outside of the send
	// window
	tc.snd.tsegs.prune(winend)
	// have retransmit timeouts expired?
	segged := false
	now := Fastmillis()
	for i := 0; i < len(tc.snd.tsegs.segs); i++ {
		if now < tc.snd.tsegs.segs[i].when + Secondms {
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
		segged = true
	}
	// XXXPANIC
	{
		// without sacks, gaps cannot occur
		ss := tc.snd.tsegs.segs
		for i := 0; i < len(tc.snd.tsegs.segs)-1; i++ {
			if ss[i].seq+ss[i].len != ss[i+1].seq {
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
	isdata := !tc.txdone || tc.snd.nxt != tc.snd.finseq+1
	sbegin := tc.snd.nxt
	if isdata && _seqdiff(upto, sbegin) > 0 {
		did := tc.seg_one(sbegin)
		tc.snd.tsegs.addnow(sbegin, uint32(did), winend)
		segged = true
	}
	// send lone FIN only if FIN wasn't already set on a just-transmitted
	// segment
	if !segged && tc.txdone && tc.snd.nxt == tc.snd.finseq {
		tc.seg_one(tc.snd.finseq)
		tc.snd.tsegs.addnow(tc.snd.finseq, 1, winend)
	}
	tc._txtimeout_start(now)
}

func (tc *Tcptcb_t) seg_one(seq uint32) int {
	winend := tc.snd.una + uint32(tc.snd.win)
	// XXXPANIC
	if !_seqbetween(tc.snd.una, seq, winend) {
		panic("must be in send window")
	}
	nic, ok := Nic_lookup(tc.lip)
	if !ok {
		panic("NIC gone")
	}
	var thdr []uint8
	var opt []uint8
	var istso bool
	var dlen int
	var sgbuf [][]uint8

	if tc.txdone && seq == tc.snd.finseq {
		pkt, opts := tc.mkfin(tc.snd.finseq, tc.rcv.nxt)
		eth, ip, tcph := pkt.Hdrbytes()
		sgbuf = [][]uint8{eth, ip, tcph, opts}
		thdr = tcph
		dlen = 1
		istso = false
		opt = opts
	} else {
		// the data to send may be larger than MSS
		buf1, buf2 := tc.txbuf.sysread(seq, _seqdiff(winend, seq))
		dlen = len(buf1) + len(buf2)
		if dlen == 0 {
			panic("must send non-zero amount")
		}
		pkt, opts, tso := tc.mkseg(seq, tc.rcv.nxt, dlen)
		eth, ip, tcph := pkt.Hdrbytes()
		sgbuf = [][]uint8{eth, ip, tcph, opts, buf1, buf2}
		thdr = tcph
		opt = opts
		istso = tso
	}

	if istso {
		smss := int(tc.snd.mss) - len(opt)
		nic.Tx_tcp_tso(sgbuf, len(thdr)+len(opt), smss)
	} else {
		nic.Tx_tcp(sgbuf)
	}

	// we just queued an ack, so clear outstanding ack flag
	tc.remack.outa = false
	tc.remack.forcedelay = false
	tc.remack.last = Fastmillis()

	send := seq + uint32(dlen)
	ndiff := _seqdiff(winend+1, seq+uint32(dlen))
	odiff := _seqdiff(winend+1, tc.snd.nxt)
	if ndiff < odiff {
		tc.snd.nxt = send
	}
	return dlen
}

// is waiting until the oldest seg timesout a good strategy? maybe wait until
// the middle timesout?
func (tc *Tcptcb_t) _txtimeout_start(now millis_t) {
	nts, ok := tc.snd.tsegs.nextts()
	if !ok || tc.dead {
		return
	}
	if tc.remseg.tstart {
		// XXXPANIC
		if nts < tc.remseg.target {
			fmt.Printf("** timeout shrink! %v\n",
				tc.remseg.target - nts)
		}
		return
	}
	tc.remseg.tstart = true
	tc.remseg.target = nts
	// XXX rto calculation
	deadline := nts + Secondms * 5
	if now > deadline {
		fmt.Printf("** timeouts are shat upon!\n")
		return
	}
	bigtw.tosched_tx(tc)
}

var _deftcpopts = []uint8{
	// mss = 1460
	2, 4, 0x5, 0xb4,
	// sackok
	//4, 2, 1, 1,
	// wscale = 3
	//3, 3, 3, 1,
	// timestamp pad
	1, 1,
	// timestamp
	8, 10, 0, 0, 0, 0, 0, 0, 0, 0,
}

// returns TCP header and TCP option slice
func (tc *Tcptcb_t) mkconnect(seq uint32) (*Tcppkt_t, []uint8) {
	tc._sanity()
	ret := &Tcppkt_t{}
	ret.Tcphdr.Init_syn(tc.lport, tc.rport, seq)
	ret.Tcphdr.Win = Htons(tc.rcv.win)
	opt := _deftcpopts
	tsoff := 6
	ret.Tcphdr.Set_opt(opt, opt[tsoff:], 0)
	l4len := ret.Tcphdr.Hdrlen()
	ret.Iphdr.Init_tcp(l4len, tc.lip, tc.rip)
	ret.Ether.Init_ip4(tc.smac[:], tc.dmac[:])
	ret.Crc(l4len, tc.lip, tc.rip)
	return ret, opt
}

func _mksynack(smac *Mac_t, dmac []uint8, tk tcpkey_t, lwin uint16, seq,
	ack, tsecr uint32) (*Tcppkt_t, []uint8) {
	ret := &Tcppkt_t{}
	ret.Tcphdr.Init_synack(tk.lport, tk.rport, seq, ack)
	ret.Tcphdr.Win = Htons(lwin)
	opt := _deftcpopts
	tsoff := 6
	ret.Tcphdr.Set_opt(opt, opt[tsoff:], tsecr)
	l4len := ret.Tcphdr.Hdrlen()
	ret.Iphdr.Init_tcp(l4len, tk.lip, tk.rip)
	ret.Ether.Init_ip4(smac[:], dmac)
	ret.Crc(l4len, tk.lip, tk.rip)
	return ret, opt
}

func _mkrst(seq uint32, lip, rip Ip4_t, lport, rport uint16, smac,
	dmac *Mac_t) *Tcppkt_t {
	ret := &Tcppkt_t{}
	ret.Tcphdr.Init_rst(lport, rport, seq)
	l4len := ret.Tcphdr.Hdrlen()
	ret.Iphdr.Init_tcp(l4len, lip, rip)
	ret.Ether.Init_ip4(smac[:], dmac[:])
	ret.Crc(l4len, lip, rip)
	return ret
}

func (tc *Tcptcb_t) _setfin(tp *Tcphdr_t, seq uint32) {
	// set FIN?
	if tc.txdone && seq == tc.snd.finseq {
		fin := uint8(1 << 0)
		tp.Flags |= fin
	}
}

func (tc *Tcptcb_t) mkack(seq, ack uint32) (*Tcppkt_t, []uint8) {
	tc._sanity()
	ret := &Tcppkt_t{}
	ret.Tcphdr.Init_ack(tc.lport, tc.rport, seq, ack)
	ret.Tcphdr.Win = Htons(tc.rcv.win)
	tsoff := 2
	ret.Tcphdr.Set_opt(tc.opt, tc.opt[tsoff:], tc.tstamp.recent)
	l4len := ret.Tcphdr.Hdrlen()
	ret.Iphdr.Init_tcp(l4len, tc.lip, tc.rip)
	ret.Ether.Init_ip4(tc.smac[:], tc.dmac[:])
	//tc._setfin(&ret.Tcphdr, seq)
	ret.Crc(l4len, tc.lip, tc.rip)
	return ret, tc.opt
}

func (tc *Tcptcb_t) mkfin(seq, ack uint32) (*Tcppkt_t, []uint8) {
	tc._sanity()
	if !tc.txdone {
		panic("mkfin but !txdone")
	}
	ret := &Tcppkt_t{}
	ret.Tcphdr.Init_ack(tc.lport, tc.rport, seq, ack)
	ret.Tcphdr.Win = Htons(tc.rcv.win)
	tsoff := 2
	ret.Tcphdr.Set_opt(tc.opt, tc.opt[tsoff:], tc.tstamp.recent)
	l4len := ret.Tcphdr.Hdrlen()
	ret.Iphdr.Init_tcp(l4len, tc.lip, tc.rip)
	ret.Ether.Init_ip4(tc.smac[:], tc.dmac[:])
	tc._setfin(&ret.Tcphdr, seq)
	ret.Crc(l4len, tc.lip, tc.rip)
	return ret, tc.opt
}

func (tc *Tcptcb_t) mkrst(seq uint32) *Tcppkt_t {
	ret := _mkrst(seq, tc.lip, tc.rip, tc.lport, tc.rport, tc.smac,
		tc.dmac)
	return ret
}

// returns the new TCP packet/options and true if the packet should use TSO
func (tc *Tcptcb_t) mkseg(seq, ack uint32, seglen int) (*Tcppkt_t,
	[]uint8, bool) {
	ret := &Tcppkt_t{}
	ret.Tcphdr.Init_ack(tc.lport, tc.rport, seq, ack)
	ret.Tcphdr.Win = Htons(tc.rcv.win)
	tsoff := 2
	ret.Tcphdr.Set_opt(tc.opt, tc.opt[tsoff:], tc.tstamp.recent)
	tc._setfin(&ret.Tcphdr, seq+uint32(seglen))
	l4len := ret.Tcphdr.Hdrlen() + seglen
	ret.Iphdr.Init_tcp(l4len, tc.lip, tc.rip)
	ret.Ether.Init_ip4(tc.smac[:], tc.dmac[:])
	istso := seglen+len(tc.opt) > int(tc.snd.mss)
	// packets using TSO do not include the the TCP payload length in the
	// pseudo-header checksum.
	if istso {
		l4len = 0
	}
	ret.Crc(l4len, tc.lip, tc.rip)
	return ret, tc.opt, istso
}

func (tc *Tcptcb_t) failwake() {
	tc._sanity()
	tc.state = CLOSED
	tc.kill()
	tc.rxdone = true
	tc.rxbuf.cond.Broadcast()
	tc.txbuf.cond.Broadcast()
	tc.pollers.Wakeready(fdops.R_READ | fdops.R_HUP | fdops.R_ERROR)
}

func (tc *Tcptcb_t) synsent(tcp *Tcphdr_t, mss uint16, ropt Tcpopt_t,
	rest [][]uint8) {
	tc._sanity()
	if ack, ok := tcp.Isack(); ok {
		if !tc.ackok(ack) {
			if tcp.Isrst() {
				return
			}
			tc._rst()
			return
		}
	}
	if tcp.Isrst() {
		fmt.Printf("connection refused\n")
		tc.failwake()
		return
	}
	// should we allow simultaneous connect?
	if !tcp.Issyn() {
		return
	}
	ack, ok := tcp.Isack()
	if !ok {
		// XXX make SYN/ACK, move to SYNRCVD
		panic("simultaneous connect")
	}
	tc._nstate(SYNSENT, ESTAB)
	theirseq := Ntohl(tcp.Seq)
	tc.set_seqs(tc.snd.nxt, theirseq+1)
	tc.snd.mss = mss
	tc.snd.una = ack
	var dlen int
	for _, r := range rest {
		dlen += len(r)
	}
	// snd.wl[12] are bogus, force window update
	rwin := Ntohs(tcp.Win)
	tc.snd.wl1 = theirseq
	tc.snd.wl2 = ack
	tc.snd.win = rwin
	tc.sched_ack()
	tc.data_in(tc.rcv.nxt, ack, rwin, rest, dlen, ropt.Tsval)
	// wakeup threads blocking in connect(2)
	tc.rxbuf.cond.Broadcast()
}

// sanity check the packet and segment processing: increase snd.una according
// to remote ACK, copy data to user buffer, and schedule an ACK of our own.
func (tc *Tcptcb_t) estab(tcp *Tcphdr_t, ropt Tcpopt_t, rest [][]uint8) {
	seq := Ntohl(tcp.Seq)
	// does this segment contain data in our receive window?
	var dlen int
	for _, r := range rest {
		dlen += len(r)
	}
	if !tc.seqok(seq, dlen) {
		if !tcp.Isrst() {
			return
		}
		tc.sched_ack()
		return
	}
	if tcp.Isrst() {
		tc.failwake()
		return
	}
	if tcp.Issyn() {
		tc._rst()
		tc.failwake()
		return
	}
	ack, ok := tcp.Isack()
	if !ok {
		return
	}
	if !tc.ackok(ack) {
		tc.sched_ack()
		return
	}
	tc.data_in(seq, ack, Ntohs(tcp.Win), rest, dlen, ropt.Tsval)
}

// trims the segment to fit our receive window, copies received data to the
// user buffer, and acks it.
func (tc *Tcptcb_t) data_in(rseq, rack uint32, rwin uint16, rest [][]uint8,
	dlen int, rtstamp uint32) {
	// XXXPANIC
	if !tc.seqok(rseq, dlen) {
		panic("must contain in-window data")
	}
	// update echo timestamp
	// XXX how to handle a wrapped timestamp?
	if rtstamp >= tc.tstamp.recent && rseq <= tc.tstamp.acksent {
		tc.tstamp.recent = rtstamp
	}
	// +1 in case our FIN's sequence number is just outside the send window
	swinend := tc.snd.una + uint32(tc.snd.win) + 1
	if _seqbetween(tc.snd.una, rack, swinend) {
		tc.snd.tsegs.ackupto(rack)
		tc.snd.una = rack
		// distinguish between acks for data and the ack for our FIN
		pack := rack
		if tc.txdone && pack == tc.snd.finseq+1 {
			pack = tc.snd.finseq
		}
		tc.txbuf.ackup(pack)
	}
	// figure out which bytes are in our window: is the beginning of
	// segment outside our window?
	if !_seqbetween(tc.rcv.nxt, rseq, tc.rcv.nxt+uint32(tc.rcv.win)) {
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
	if !_seqbetween(tc.rcv.nxt, rseq+uint32(dlen), winend) {
		prune := _seqdiff(rseq+uint32(dlen), winend)
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
	if dlen == 0 {
		return
	}
	tc.rxbuf.syswrite(rseq, rest)
	tc.rcv.nxt = tc.rcv.trsegs.recvd(tc.rcv.nxt, winend, rseq, dlen)
	tc.rxbuf.rcvup(tc.rcv.nxt)
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
func (tc *Tcptcb_t) ackok(ack uint32) bool {
	tc._sanity()
	return _seqbetween(tc.snd.una, ack, tc.snd.nxt)
}

// is seq in my receive window?
func (tc *Tcptcb_t) seqok(seq uint32, seglen int) bool {
	tc._sanity()
	winstart := tc.rcv.nxt
	winend := tc.rcv.nxt + uint32(tc.rcv.win)
	segend := seq + uint32(seglen)
	// XXX XXX segment may overlap in middle with window (check
	// non-intersection)
	return _seqbetween(winstart, seq, winend) ||
		_seqbetween(winstart, segend, winend)
}

// returns true when low <= mid <= hi on uints (i.e. even when sequences
// numbers wrap to 0)
func _seqbetween(low, mid, hi uint32) bool {
	return _seqdiff(hi, mid) <= _seqdiff(hi, low)
}

// returns the uint32 difference of big - small.
// XXX make return type uint since the result is always positive
func _seqdiff(big, small uint32) int {
	return int(uint(big - small))
}

// update remote receive window
func (tc *Tcptcb_t) rwinupdate(seq, ack uint32, win uint16) {
	// see if seq is the larger than wl1. does the window wrap?
	lwinend := tc.rcv.nxt + uint32(tc.rcv.win)
	w1less := _seqdiff(lwinend, tc.snd.wl1) > _seqdiff(lwinend, seq)
	rwinend := tc.snd.una + uint32(tc.snd.win) + 1 //
	wl2less := _seqdiff(rwinend, tc.snd.wl2) >= _seqdiff(rwinend, ack)
	if w1less || (tc.snd.wl1 == seq && wl2less) {
		tc.snd.win = win
		tc.snd.wl1 = seq
		tc.snd.wl2 = ack
	}
}

// returns true if the receive window is less than an MSS
func (tc *Tcptcb_t) lwinshrink(dlen int) bool {
	tc._sanity()
	var ret bool
	left := tc.rxbuf.cbuf.Left()
	if left-int(tc.rcv.win) >= int(tc.rcv.mss) {
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

func (tc *Tcptcb_t) lwingrow(dlen int) {
	tc._sanity()
	left := tc.rxbuf.cbuf.Left()
	mss := int(tc.rcv.mss)
	oldwin := int(tc.rcv.win)
	// don't delay acks that reopen the window
	if oldwin < mss && left >= mss {
		tc.rcv.win = uint16(left)
		tc.sched_ack()
		tc.ack_now()
	} else if left - oldwin >= mss {
		tc.rcv.win = uint16(left)
		tc.sched_ack()
		tc.ack_maybe()
	}
}

func (tc *Tcptcb_t) uread(dst fdops.Userio_i) (int, defs.Err_t) {
	tc._sanity()
	wrote, err := tc.rxbuf.cbuf.Copyout(dst)
	// did the user consume enough data to reopen the window?
	tc.lwingrow(wrote)
	return wrote, err
}

func (tc *Tcptcb_t) uwrite(src fdops.Userio_i) (int, defs.Err_t) {
	tc._sanity()
	wrote, err := tc.txbuf.cbuf.Copyin(src)
	if tc.state == ESTAB || tc.state == CLOSEWAIT {
		tc.seg_maybe()
	}
	return wrote, err
}

func (tc *Tcptcb_t) shutdown(read, write bool) defs.Err_t {
	tc._sanity()
	if tc.dead {
		return -defs.EINVAL
	}
	if tc.state == TCPNEW {
		if tc.bound {
			tcpcons.unreserve(tc.lip, tc.lport)
		}
		tc.dead = true
		// this tcb cannot be in tcpcons
		limits.Syslimit.Socks.Give()
		return 0
	} else if tc.state == SYNSENT || tc.state == SYNRCVD {
		tc.kill()
		return 0
	} else if tc.state != ESTAB && tc.state != CLOSEWAIT {
		return -defs.EINVAL
	}

	tc.txdone = true
	tc.snd.finseq = tc.txbuf.end_seq()

	os := tc.state
	var ns tcpstate_t
	if os == ESTAB {
		ns = FINWAIT1
	} else if os == CLOSEWAIT {
		ns = LASTACK
	} else {
		panic("unexpected tcb state")
	}
	if tc.txfinished(os, ns) {
		tc.seg_maybe()
	}
	return 0
}

var _nilmac Mac_t

func (tc *Tcptcb_t) tcb_init(lip, rip Ip4_t, lport, rport uint16, smac,
	dmac *Mac_t, sndnxt uint32, sv []uint8, sp mem.Pa_t, rv []uint8, rp mem.Pa_t) {
	if lip == defs.INADDR_ANY || rip == defs.INADDR_ANY || lport == 0 || rport == 0 {
		panic("all IPs/ports must be known")
	}
	tc.lip = lip
	tc.rip = rip
	tc.lport = lport
	tc.rport = rport
	tc.state = TCPNEW
	defwin := uint16(4380)
	tc.snd.win = defwin
	tc.dmac = dmac
	tc.smac = smac
	//defbufsz := (1 << 12)
	//if sndsz == 0 {
	//	sndsz = defbufsz
	//}
	//if rcvsz == 0 {
	//	rcvsz = defbufsz
	//}
	tc.txbuf.tbuf_init(sv, sp, tc)
	tc.rxbuf.tbuf_init(rv, rp, tc)
	// cached tcp timestamp option
	_opt := [12]uint8{1, 1, 8, 10, 0, 0, 0, 0, 0, 0, 0, 0}
	tc.opt = _opt[:]
	if uint(tc.rxbuf.cbuf.Left()) > uint(^uint16(0)) {
		panic("need window shift")
	}
	tc.rcv.win = uint16(tc.rxbuf.cbuf.Left())
	// assume 12 bytes of TCP options (nop, nop, timestamp)
	tc.rcv.mss = 1448
	tc.snd.nxt = sndnxt
	tc.snd.una = sndnxt
	tc.ackl.linit(tc)
	tc.txl.linit(tc)
	tc.twaitl.linit(tc)
}

func (tc *Tcptcb_t) set_seqs(sndnxt, rcvnxt uint32) {
	tc.snd.nxt = sndnxt
	tc.snd.una = sndnxt
	tc.txbuf.set_seq(sndnxt)
	tc.rcv.nxt = rcvnxt
	tc.rxbuf.set_seq(rcvnxt)
}

type tcpkey_t struct {
	lip   Ip4_t
	rip   Ip4_t
	lport uint16
	rport uint16
}

type tcplkey_t struct {
	lip   Ip4_t
	lport uint16
}

type tcpcons_t struct {
	l sync.Mutex
	// established connections
	econns map[tcpkey_t]*Tcptcb_t
	// listening sockets
	listns map[tcplkey_t]*tcplisten_t
	// in-use local IP/port pairs. a port may be used on a particular local
	// IP or all local IPs.
	ports map[tcplkey_t]int
}

func (tc *tcpcons_t) init() {
	tc.econns = make(map[tcpkey_t]*Tcptcb_t)
	tc.listns = make(map[tcplkey_t]*tcplisten_t)
	tc.ports = make(map[tcplkey_t]int)
}

// try to reserve the IP/port pair. returns true on success.
func (tc *tcpcons_t) reserve(lip Ip4_t, lport uint16) bool {
	tc.l.Lock()
	defer tc.l.Unlock()

	k := tcplkey_t{lip: lip, lport: lport}
	anyk := k
	anyk.lip = defs.INADDR_ANY
	if tc.ports[k] != 0 || tc.ports[anyk] != 0 {
		return false
	}
	tc.ports[k] = 1
	return true
}

// returns allocated port and true if successful.
func (tc *tcpcons_t) reserve_ephemeral(lip Ip4_t) (uint16, bool) {
	tc.l.Lock()
	defer tc.l.Unlock()

	k := tcplkey_t{lip: lip, lport: uint16(rand.Uint32())}
	if k.lport == 0 {
		k.lport++
	}
	anyk := k
	anyk.lip = defs.INADDR_ANY
	ok := tc.ports[k]|tc.ports[anyk] > 0
	for i := 0; i <= int(^uint16(0)) && ok; i++ {
		k.lport++
		anyk.lport++
		ok = tc.ports[k]|tc.ports[anyk] > 0
	}
	if ok {
		fmt.Printf("out of ephemeral ports\n")
		return 0, false
	}

	tc.ports[k] = 1

	return k.lport, true
}

func (tc *tcpcons_t) unreserve(lip Ip4_t, lport uint16) {
	tc.l.Lock()
	defer tc.l.Unlock()

	lk := tcplkey_t{lip: lip, lport: lport}
	// XXXPANIC
	if tc.ports[lk] == 0 {
		panic("must be reserved")
	}
	delete(tc.ports, lk)
}

// inserts the TCB into the TCP connection table. wasany is true if the tcb
// reserved a port on defs.INADDR_ANY.
func (tc *tcpcons_t) tcb_insert(tcb *Tcptcb_t, wasany bool) {
	tc.l.Lock()
	tc._tcb_insert(tcb, wasany, false)
	tc.l.Unlock()
}

// inserts the TCB, which was created via a passive connect, into the TCP
// connection table
func (tc *tcpcons_t) tcb_linsert(tcb *Tcptcb_t) {
	tc.l.Lock()
	tc._tcb_insert(tcb, false, true)
	tc.l.Unlock()
}

func (tc *tcpcons_t) _tcb_insert(tcb *Tcptcb_t, wasany, fromlisten bool) {
	if wasany && fromlisten {
		panic("impossible args")
	}
	lk := tcplkey_t{lip: tcb.lip, lport: tcb.lport}
	// if the tcb reserved on defs.INADDR_ANY, free up the used port on the
	// other local IPs
	anyk := tcplkey_t{lip: defs.INADDR_ANY, lport: tcb.lport}
	if wasany {
		// XXXPANIC
		if tc.ports[anyk] == 0 {
			panic("no such reservation")
		}
		delete(tc.ports, anyk)
		tc.ports[lk] = 1
	} else if fromlisten {
		n := tc.ports[anyk]
		// XXXPANIC
		if n == 0 {
			panic("listen reservation must exist")
		}
		tc.ports[anyk] = n + 1
	}

	// XXXPANIC
	if tc.ports[lk] == 0 && tc.ports[anyk] == 0 {
		panic("port must be reserved")
	}

	k := tcpkey_t{lip: tcb.lip, rip: tcb.rip, lport: tcb.lport,
		rport: tcb.rport}
	// XXXPANIC
	if _, ok := tc.econns[k]; ok {
		panic("entry exists")
	}
	tc.econns[k] = tcb
}

// if the first bool return is true, then only the tcb is valid. if the second
// bool return is true, then only the tcplistener is valid. otherwise, there is
// no such socket/connection.
func (tc *tcpcons_t) tcb_lookup(tk tcpkey_t) (*Tcptcb_t, bool,
	*tcplisten_t, bool) {
	if tk.lip == defs.INADDR_ANY {
		panic("localip must be known")
	}

	tc.l.Lock()
	tcb, istcb := tc.econns[tk]

	lk := tcplkey_t{lip: tk.lip, lport: tk.lport}
	l, islist := tc.listns[lk]
	if !islist {
		// check for any IP listener
		lk.lip = defs.INADDR_ANY
		l, islist = tc.listns[lk]
	}
	tc.l.Unlock()

	return tcb, istcb, l, islist
}

func (tc *tcpcons_t) tcb_del(tcb *Tcptcb_t) {
	tc.l.Lock()
	defer tc.l.Unlock()

	k := tcpkey_t{lip: tcb.lip, rip: tcb.rip, lport: tcb.lport,
		rport: tcb.rport}
	// XXXPANIC
	if _, ok := tc.econns[k]; !ok {
		panic("k doesn't exist")
	}
	delete(tc.econns, k)

	lk := tcplkey_t{lip: k.lip, lport: k.lport}
	n := tc.ports[lk]
	if n == 0 {
		// connection must be from listening
		anyk := lk
		anyk.lip = defs.INADDR_ANY
		ln := tc.ports[anyk]
		if ln == 0 {
			panic("lk doesn't exist")
		}
		if ln == 1 {
			delete(tc.ports, anyk)
		} else {
			tc.ports[anyk] = ln - 1
		}
	} else {
		if n != 1 {
			panic("must be active connect but not 1")
		}
		delete(tc.ports, lk)
	}
}

func (tc *tcpcons_t) listen_insert(tcl *tcplisten_t) {
	tc.l.Lock()
	defer tc.l.Unlock()

	if tcl.lport == 0 {
		panic("address must be known")
	}
	lk := tcplkey_t{lip: tcl.lip, lport: tcl.lport}
	// XXXPANIC
	if tc.ports[lk] == 0 {
		panic("must be reserved")
	}
	tc.listns[lk] = tcl
}

func (tc *tcpcons_t) listen_del(tcl *tcplisten_t) {
	tc.l.Lock()
	defer tc.l.Unlock()

	lk := tcplkey_t{lip: tcl.lip, lport: tcl.lport}
	n := tc.ports[lk]
	// XXXPANIC
	if n == 0 {
		panic("must be reserved")
	}
	if n == 1 {
		delete(tc.ports, lk)
	} else {
		tc.ports[lk] = n - 1
	}
	if _, ok := tc.listns[lk]; !ok {
		panic("no such listener")
	}
	delete(tc.listns, lk)
}

// tcpcons' mutex is a leaf lock
var tcpcons tcpcons_t

func send_rst(seq uint32, k tcpkey_t) {
	_send_rst(seq, 0, k, false)
}

func send_rstack(seq, ack uint32, k tcpkey_t) {
	_send_rst(seq, ack, k, true)
}

func _send_rst(seq, ack uint32, k tcpkey_t, useack bool) {
	select {
	case _rstchan <- rstmsg_t{k: k, seq: seq, ack: ack, useack: useack}:
	default:
		fmt.Printf("rst dropped\n")
	}
}

func net_tcp(pkt [][]uint8, tlen int) {
	hdr := pkt[0]
	if len(hdr) < ETHERLEN {
		return
	}
	rest := hdr[ETHERLEN:]
	ip4, rest, ok := Sl2iphdr(rest)
	if !ok {
		return
	}
	tcph, opts, rest, ok := Sl2tcphdr(rest)
	if !ok {
		return
	}

	sip := Sl2ip(ip4.Sip[:])
	dip := Sl2ip(ip4.Dip[:])
	//tcph.Dump(sip, dip, opts, len(rest))

	pkt[0] = rest

	localip := dip
	k := tcpkey_t{lip: localip, rip: sip, lport: Ntohs(tcph.Dport),
		rport: Ntohs(tcph.Sport)}
	tcb, istcb, listener, islistener := tcpcons.tcb_lookup(k)
	if istcb {
		// is the remote host reusing a port in TIMEWAIT? if so, allow
		// reuse.
		tcb.tcb_lock()
		if tcb.state == TIMEWAIT && islistener &&
			opts.Tsok && opts.Tsval >= tcb.tstamp.recent {
			tcb.kill()
			tcb.tcb_unlock()
			// fallthrough to islistener case
		} else {
			tcb.incoming(k, ip4, tcph, opts, pkt)
			tcb.tcb_unlock()
			return
		}
	}
	if islistener {
		smac := hdr[6:12]
		listener.l.Lock()
		listener.incoming(smac, k, ip4, tcph, opts, pkt)
		listener.l.Unlock()
	} else {
		_port_closed(tcph, k)
	}
}

func _port_closed(tcph *Tcphdr_t, k tcpkey_t) {
	rack, ok := tcph.Isack()
	if ok {
		send_rst(rack, k)
	} else {
		ack := Ntohl(tcph.Seq) + 1
		send_rstack(0, ack, k)
	}
}

// active connect fops
type Tcpfops_t struct {
	// tcb must always be non-nil
	tcb     *Tcptcb_t
	options defs.Fdopt_t
}

func (tf *Tcpfops_t) Set(tcb *Tcptcb_t, opt defs.Fdopt_t) {
	tf.tcb = tcb
	tf.options = opt
	tf.tcb.openc = 1

}

// to prevent an operation racing with a close
func (tf *Tcpfops_t) _closed() (defs.Err_t, bool) {
	if tf.tcb.openc == 0 {
		return -defs.EBADF, false
	}
	return 0, true
}

func (tf *Tcpfops_t) Close() defs.Err_t {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if err, ok := tf._closed(); !ok {
		return err
	}

	tf.tcb.openc--
	if tf.tcb.openc < 0 {
		panic("neg ref")
	}
	// XXX linger support
	if tf.tcb.openc == 0 {
		// XXX when to RST?
		tf.tcb.shutdown(true, true)
		if tf.tcb.state == TIMEWAIT {
			tf.tcb._bufrelease()
		}
	}

	return 0
}

func (tf *Tcpfops_t) Fstat(st *stat.Stat_t) defs.Err_t {
	sockmode := defs.Mkdev(2, 0)
	st.Wmode(sockmode)
	return 0
}

func (tf *Tcpfops_t) Lseek(int, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (tf *Tcpfops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	return nil, -defs.EINVAL
}

func (tf *Tcpfops_t) Pathi() defs.Inum_t {
	panic("tcp socket cwd")
}

func (tf *Tcpfops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	tf.tcb.tcb_lock()
	if err, ok := tf._closed(); !ok {
		tf.tcb.tcb_unlock()
		return 0, err
	}
	noblk := tf.options&defs.O_NONBLOCK != 0

	var read int
	var err defs.Err_t
	for {
		gimme := bounds.Bounds(bounds.B_TCPFOPS_T_READ)
		if !res.Resadd_noblock(gimme) {
			err = -defs.ENOHEAP
			break
		}
		read, err = tf.tcb.uread(dst)
		if err != 0 {
			break
		}
		if read != 0 || tf.tcb.rxdone {
			break
		}
		if noblk {
			err = -defs.EAGAIN
			break
		}
		if err = tf.tcb.rbufwait(); err != 0 {
			break
		}
	}
	tf.tcb.tcb_unlock()
	return read, err
}

func (tf *Tcpfops_t) Reopen() defs.Err_t {
	tf.tcb.tcb_lock()
	tf.tcb.openc++
	tf.tcb.tcb_unlock()
	return 0
}

func (tf *Tcpfops_t) Write(src fdops.Userio_i) (int, defs.Err_t) {
	tf.tcb.tcb_lock()
	if err, ok := tf._closed(); !ok {
		tf.tcb.tcb_unlock()
		return 0, err
	}
	noblk := tf.options&defs.O_NONBLOCK != 0

	var wrote int
	var err defs.Err_t
	for {
		gimme := bounds.Bounds(bounds.B_TCPFOPS_T_WRITE)
		if !res.Resadd_noblock(gimme) {
			err = -defs.ENOHEAP
			break
		}
		if tf.tcb.txdone {
			err = -defs.EPIPE
			break
		}
		var did int
		did, err = tf.tcb.uwrite(src)
		wrote += did
		if src.Remain() == 0 || err != 0 {
			break
		}
		if noblk {
			if did == 0 {
				err = -defs.EAGAIN
			}
			break
		}
		if err = tf.tcb.tbufwait(); err != 0 {
			break
		}
	}
	tf.tcb.tcb_unlock()
	return wrote, err
}

func (tf *Tcpfops_t) Truncate(newlen uint) defs.Err_t {
	return -defs.EINVAL
}

func (tf *Tcpfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (tf *Tcpfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (tf *Tcpfops_t) Accept(fdops.Userio_i) (fdops.Fdops_i, int, defs.Err_t) {
	panic("no imp")
}

func (tf *Tcpfops_t) Bind(saddr []uint8) defs.Err_t {
	fam := util.Readn(saddr, 1, 1)
	lport := Ntohs(Be16(util.Readn(saddr, 2, 2)))
	lip := Ip4_t(Ntohl(Be32(util.Readn(saddr, 4, 4))))

	// why verify saddr.family is defs.AF_INET?
	if fam != defs.AF_INET {
		return -defs.EAFNOSUPPORT
	}

	if lip != defs.INADDR_ANY {
		if _, ok := Nic_lookup(lip); !ok {
			return -defs.EADDRNOTAVAIL
		}
	}

	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if tf.tcb.state != TCPNEW {
		return -defs.EINVAL
	}

	if tf.tcb.bound {
		tcpcons.unreserve(tf.tcb.lip, tf.tcb.lport)
		tf.tcb.bound = false
	}

	eph := lport == 0
	var ok bool
	if eph {
		lport, ok = tcpcons.reserve_ephemeral(lip)
	} else {
		ok = tcpcons.reserve(lip, lport)
	}
	ret := -defs.EADDRINUSE
	if ok {
		tf.tcb.lip = lip
		tf.tcb.lport = lport
		tf.tcb.bound = true
		ret = 0
	}
	return ret
}

func (tf *Tcpfops_t) Connect(saddr []uint8) defs.Err_t {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if tf.tcb.state != TCPNEW {
		return -defs.EISCONN
	}
	if err, ok := tf._closed(); !ok {
		return err
	}

	blk := true
	if tf.options&defs.O_NONBLOCK != 0 {
		blk = false
	}
	dip := Ip4_t(Ntohl(Be32(util.Readn(saddr, 4, 4))))
	dport := Ntohs(Be16(util.Readn(saddr, 2, 2)))
	tcb := tf.tcb
	err := tcb._tcp_connect(dip, dport)
	if err != 0 {
		return err
	}
	var ret defs.Err_t
	if blk {
		for tcb.state == SYNSENT || tcb.state == SYNRCVD {
			if err := tcb.rbufwait(); err != 0 {
				return err
			}
		}
		if tcb.state != ESTAB && tcb.state != CLOSED {
			panic("unexpected state")
		}
		if tcb.state == CLOSED {
			ret = -defs.ECONNRESET
		}
	}
	return ret
}

func (tf *Tcpfops_t) Listen(backlog int) (fdops.Fdops_i, defs.Err_t) {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if tf.tcb.state != TCPNEW {
		return nil, -defs.EINVAL
	}

	if !tf.tcb.bound {
		lport, ok := tcpcons.reserve_ephemeral(defs.INADDR_ANY)
		if !ok {
			return nil, -defs.EADDRINUSE
		}
		tf.tcb.lip = defs.INADDR_ANY
		tf.tcb.lport = lport
		tf.tcb.bound = true
	}
	bl := 32
	if backlog > 0 && backlog < 512 {
		bl = backlog
	}

	tf.tcb._nstate(TCPNEW, LISTEN)

	ret := &tcplfops_t{options: tf.options}
	ret.tcl.tcl_init(tf.tcb.lip, tf.tcb.lport, bl)
	tcpcons.listen_insert(&ret.tcl)

	return ret, 0
}

// XXX read/write should be wrapper around recvmsg/sendmsg
func (tf *Tcpfops_t) Sendmsg(src fdops.Userio_i,
	toaddr []uint8, cmsg []uint8, flags int) (int, defs.Err_t) {
	if len(cmsg) != 0 {
		panic("no imp")
	}
	return tf.Write(src)
}

func (tf *Tcpfops_t) Recvmsg(dst fdops.Userio_i,
	fromsa fdops.Userio_i, cmsg fdops.Userio_i, flag int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	if cmsg.Totalsz() != 0 {
		panic("no imp")
	}
	wrote, err := tf.Read(dst)
	return wrote, 0, 0, 0, err
}

func (tf *Tcpfops_t) _pollchk(ev fdops.Ready_t) fdops.Ready_t {
	var ret fdops.Ready_t
	isdata := !tf.tcb.rxbuf.cbuf.Empty()
	if tf.tcb.dead && ev&fdops.R_ERROR != 0 {
		ret |= fdops.R_ERROR
	}
	if ev&fdops.R_READ != 0 && (isdata || tf.tcb.rxdone) {
		ret |= fdops.R_READ
	}
	if tf.tcb.txdone {
		if ev&fdops.R_HUP != 0 {
			ret |= fdops.R_HUP
		}
	} else if ev&fdops.R_WRITE != 0 && !tf.tcb.txbuf.cbuf.Full() {
		ret |= fdops.R_WRITE
	}
	return ret
}

func (tf *Tcpfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	ready := tf._pollchk(pm.Events)
	var err defs.Err_t
	if ready == 0 && pm.Dowait {
		tf.tcb.tcb_lock()
		ready = tf._pollchk(pm.Events)
		if ready == 0 {
			err = tf.tcb.pollers.Addpoller(&pm)
		}
		tf.tcb.tcb_unlock()
	}
	return ready, err
}

func (tf *Tcpfops_t) Fcntl(cmd, opt int) int {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	switch cmd {
	case defs.F_GETFL:
		return int(tf.options)
	case defs.F_SETFL:
		tf.options = defs.Fdopt_t(opt)
		return 0
	default:
		panic("weird cmd")
	}
}

func (tf *Tcpfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	switch opt {
	case defs.SO_NAME, defs.SO_PEER:
		if !tf.tcb.bound {
			return 0, -defs.EADDRNOTAVAIL
		}
		b := []uint8{8, defs.AF_INET, 0, 0, 0, 0, 0, 0}
		var port, ip int
		if opt == defs.SO_NAME {
			port = int(tf.tcb.lport)
			ip = int(tf.tcb.lip)
		} else {
			port = int(tf.tcb.rport)
			ip = int(tf.tcb.rip)
		}
		util.Writen(b, 2, 2, port)
		util.Writen(b, 4, 4, ip)
		did, err := bufarg.Uiowrite(b)
		return did, err
	default:
		return 0, -defs.EOPNOTSUPP
	}
}

func (tf *Tcpfops_t) Setsockopt(lev, opt int, src fdops.Userio_i,
	intarg int) defs.Err_t {
	tf.tcb.tcb_lock()
	defer tf.tcb.tcb_unlock()

	if lev != defs.SOL_SOCKET {
		return -defs.EOPNOTSUPP
	}
	ret := -defs.EOPNOTSUPP
	switch opt {
	case defs.SO_SNDBUF, defs.SO_RCVBUF:
		panic("fixme")
		n := uint(intarg)
		// circbuf requires that the buffer size is power-of-2. my
		// window handling assumes the buffer size > mss.
		// XXX
		mn := uint(1 << 12)
		mx := uint(1 << 12)
		if n < mn || n > mx || n&(n-1) != 0 {
			ret = -defs.EINVAL
			break
		}
		var cb *circbuf.Circbuf_t
		var sp *int
		if opt == defs.SO_SNDBUF {
			cb = &tf.tcb.txbuf.cbuf
			sp = &tf.tcb.sndsz
		} else {
			cb = &tf.tcb.rxbuf.cbuf
			sp = &tf.tcb.rcvsz
		}
		if cb.Used() > int(n) {
			ret = -defs.EINVAL
			break
		}
		*sp = int(n)
		ret = 0
		if cb.Bufsz() == 0 {
			break
		}
		nb := make([]uint8, n)
		fb := &vm.Fakeubuf_t{}
		fb.Fake_init(nb)
		did, err := cb.Copyout(fb)
		if err != 0 {
			panic("must succeed")
		}
		cb.Set(nb, did, pagemem)
	}
	return ret
}

func (tf *Tcpfops_t) Shutdown(read, write bool) defs.Err_t {
	tf.tcb.tcb_lock()
	ret := tf.tcb.shutdown(read, write)
	tf.tcb.tcb_unlock()
	return ret
}

// passive connect fops
type tcplfops_t struct {
	tcl     tcplisten_t
	options defs.Fdopt_t
}

// to prevent an operation racing with a close
func (tl *tcplfops_t) _closed() (defs.Err_t, bool) {
	if tl.tcl.openc == 0 {
		return -defs.EBADF, false
	}
	return 0, true
}

func (tl *tcplfops_t) Close() defs.Err_t {
	tl.tcl.l.Lock()
	defer tl.tcl.l.Unlock()

	if err, ok := tl._closed(); !ok {
		return err
	}

	tl.tcl.openc--
	if tl.tcl.openc < 0 {
		panic("neg ref")
	}
	if tl.tcl.openc == 0 {
		limits.Syslimit.Socks.Give()
		// close all unaccepted established connections
		for {
			tcb, ok := tl.tcl._contake()
			if !ok {
				break
			}
			tcb.tcb_lock()
			tcb._rst()
			tcb.kill()
			tcb.tcb_unlock()
		}
		// free all TCP buffers allocated for half-established
		// connections.
		for _, inc := range tl.tcl.seqs {
			// ...
			pagemem.Refup(inc.bufs.sp_pg)
			pagemem.Refdown(inc.bufs.sp_pg)
			pagemem.Refup(inc.bufs.rp_pg)
			pagemem.Refdown(inc.bufs.rp_pg)
		}
		tcpcons.listen_del(&tl.tcl)
	}

	return 0
}

func (tl *tcplfops_t) Fstat(*stat.Stat_t) defs.Err_t {
	panic("no imp")
}

func (tl *tcplfops_t) Lseek(int, int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (tl *tcplfops_t) Mmapi(int, int, bool) ([]mem.Mmapinfo_t, defs.Err_t) {
	return nil, -defs.EINVAL
}

func (tl *tcplfops_t) Pathi() defs.Inum_t {
	panic("tcp socket cwd")
}

func (tl *tcplfops_t) Read(dst fdops.Userio_i) (int, defs.Err_t) {
	return 0, -defs.ENOTCONN
}

func (tl *tcplfops_t) Reopen() defs.Err_t {
	tl.tcl.l.Lock()
	tl.tcl.openc++
	tl.tcl.l.Unlock()
	return 0
}

func (tl *tcplfops_t) Write(src fdops.Userio_i) (int, defs.Err_t) {
	return 0, -defs.EPIPE
}

func (tl *tcplfops_t) Truncate(newlen uint) defs.Err_t {
	return -defs.EINVAL
}

func (tl *tcplfops_t) Pread(dst fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (tl *tcplfops_t) Pwrite(src fdops.Userio_i, offset int) (int, defs.Err_t) {
	return 0, -defs.ESPIPE
}

func (tl *tcplfops_t) Accept(saddr fdops.Userio_i) (fdops.Fdops_i,
	int, defs.Err_t) {
	tl.tcl.l.Lock()
	defer tl.tcl.l.Unlock()

	var tcb *Tcptcb_t
	noblk := tl.options&defs.O_NONBLOCK != 0

	for {
		if tl.tcl.openc == 0 {
			return nil, 0, -defs.EBADF
		}
		ltcb, ok := tl.tcl._contake()
		if ok {
			tcb = ltcb
			break
		}
		if noblk {
			return nil, 0, -defs.EAGAIN
		}
		if err := proc.KillableWait(tl.tcl.rcons.cond); err != 0 {
			return nil, 0, err
		}
	}

	fops := &Tcpfops_t{tcb: tcb, options: tl.options}
	fops.tcb.openc = 1

	// write remote socket address to userspace
	sinsz := 8
	buf := make([]uint8, sinsz)
	util.Writen(buf, 1, 0, sinsz)
	util.Writen(buf, 1, 1, defs.AF_INET)
	util.Writen(buf, 2, 2, int(Htons(tcb.rport)))
	util.Writen(buf, 4, 4, int(Htonl(uint32(tcb.rip))))
	did, err := saddr.Uiowrite(buf)
	return fops, did, err
}

func (tl *tcplfops_t) Bind(saddr []uint8) defs.Err_t {
	return -defs.EINVAL
}

func (tl *tcplfops_t) Connect(saddr []uint8) defs.Err_t {
	return -defs.EADDRINUSE
}

func (tl *tcplfops_t) Listen(_backlog int) (fdops.Fdops_i, defs.Err_t) {
	backlog := uint(_backlog)
	if backlog > 512 {
		return nil, -defs.EINVAL
	}

	tl.tcl.l.Lock()
	defer tl.tcl.l.Unlock()

	// resize backlog so long as the unaccepted ready connections will
	// still fit
	rc := &tl.tcl.rcons
	nready := rc.inum - rc.cnum
	if nready > uint(backlog) {
		return nil, -defs.EBUSY
	}
	nsl := make([]*Tcptcb_t, backlog)
	olen := uint(len(rc.sl))
	for i := uint(0); rc.cnum+i < rc.inum; i++ {
		oi := (rc.cnum + i) % olen
		nsl[i] = rc.sl[oi]
	}
	rc.sl = nsl
	return tl, 0
}

func (tl *tcplfops_t) Sendmsg(src fdops.Userio_i,
	toaddr []uint8, cmsg []uint8, flags int) (int, defs.Err_t) {
	return 0, -defs.ENOTCONN
}

func (tl *tcplfops_t) Recvmsg(dst fdops.Userio_i,
	fromsa fdops.Userio_i, cmsg fdops.Userio_i, flags int) (int, int, int, defs.Msgfl_t, defs.Err_t) {
	return 0, 0, 0, 0, -defs.ENOTCONN
}

func (tl *tcplfops_t) Pollone(pm fdops.Pollmsg_t) (fdops.Ready_t, defs.Err_t) {
	tl.tcl.l.Lock()
	defer tl.tcl.l.Unlock()

	if _, ok := tl._closed(); !ok {
		return 0, 0
	}
	// why poll a listen socket for writing? don't allow it
	pm.Events &^= fdops.R_WRITE
	if pm.Events == 0 {
		return 0, 0
	}
	var ret fdops.Ready_t
	rc := &tl.tcl.rcons
	if pm.Events&fdops.R_READ != 0 && rc.inum != rc.cnum {
		ret |= fdops.R_READ
	}
	var err defs.Err_t
	if ret == 0 && pm.Dowait {
		err = tl.tcl.pollers.Addpoller(&pm)
	}
	return ret, err
}

func (tl *tcplfops_t) Fcntl(cmd, opt int) int {
	tl.tcl.l.Lock()
	defer tl.tcl.l.Unlock()

	switch cmd {
	case defs.F_GETFL:
		return int(tl.options)
	case defs.F_SETFL:
		tl.options = defs.Fdopt_t(opt)
		return 0
	default:
		panic("weird cmd")
	}
}

func (tl *tcplfops_t) Getsockopt(opt int, bufarg fdops.Userio_i,
	intarg int) (int, defs.Err_t) {
	switch opt {
	case defs.SO_ERROR:
		dur := [4]uint8{}
		util.Writen(dur[:], 4, 0, 0)
		did, err := bufarg.Uiowrite(dur[:])
		return did, err
	default:
		return 0, -defs.EOPNOTSUPP
	}
}

func (tl *tcplfops_t) Setsockopt(lev, opt int, bufarg fdops.Userio_i,
	intarg int) defs.Err_t {
	tl.tcl.l.Lock()
	defer tl.tcl.l.Unlock()

	if lev != defs.SOL_SOCKET {
		return -defs.EOPNOTSUPP
	}
	ret := -defs.EOPNOTSUPP
	switch opt {
	case defs.SO_SNDBUF, defs.SO_RCVBUF:
		n := uint(intarg)
		mn := uint(1 << 11)
		mx := uint(1 << 20)
		if n < mn || n > mx || n&(n-1) != 0 {
			ret = -defs.EINVAL
			break
		}
		var sp *int
		if opt == defs.SO_SNDBUF {
			sp = &tl.tcl.sndsz
		} else {
			sp = &tl.tcl.rcvsz
		}
		*sp = int(n)
		ret = 0
	}
	return ret
}

func (tl *tcplfops_t) Shutdown(read, write bool) defs.Err_t {
	return -defs.ENOTCONN
}

type nic_i interface {
	// the argument is scatter-gather buffer of the entire packet including
	// all headers. returns true if the packet was copied to the NIC's
	// transmit buffer (ie buf cannot be referenced once tx_* returns),
	// false otherwise.
	Tx_raw(buf [][]uint8) bool
	Tx_ipv4(buf [][]uint8) bool
	Tx_tcp(buf [][]uint8) bool
	Tx_tcp_tso(buf [][]uint8, tcphlen, mss int) bool
	Lmac() *Mac_t
}

var nics struct {
	l sync.Mutex
	m *map[Ip4_t]nic_i
}

func Nic_insert(ip Ip4_t, n nic_i) {
	nics.l.Lock()
	defer nics.l.Unlock()

	newm := make(map[Ip4_t]nic_i, len(*nics.m)+1)
	for k, v := range *nics.m {
		newm[k] = v
	}
	if _, ok := newm[ip]; ok {
		panic("two nics for same ip")
	}
	newm[ip] = n
	p := unsafe.Pointer(&newm)
	dst := (*unsafe.Pointer)(unsafe.Pointer(&nics.m))
	// store-release on x86
	atomic.StorePointer(dst, p)
}

func Nic_lookup(lip Ip4_t) (nic_i, bool) {
	pa := (*unsafe.Pointer)(unsafe.Pointer(&nics.m))
	mappy := *(*map[Ip4_t]nic_i)(atomic.LoadPointer(pa))
	nic, ok := mappy[lip]
	return nic, ok
}

// network stack processing begins here. pkt references DMA memory and will be
// clobbered once net_start returns to the caller.
func Net_start(pkt [][]uint8, tlen int) {
	// header should always be fully contained in the first slice
	buf := pkt[0]
	hlen := len(buf)
	if hlen < ETHERLEN {
		return
	}

	etype := Ntohs(Be16(util.Readn(buf, 2, 12)))
	ip4 := uint16(0x0800)
	arp := uint16(0x0806)
	switch etype {
	case arp:
		net_arp(pkt, tlen)
	case ip4:
		// strip ethernet header
		ippkt, _, ok := Sl2iphdr(buf[ETHERLEN:])
		if !ok {
			// short IP4 header
			return
		}
		if ippkt.Vers_hdr&0xf0 != 0x40 {
			// not IP4?
			return
		}
		// no IP options yet
		if ippkt.Vers_hdr&0xf != 0x5 {
			fmt.Printf("no imp\n")
			return
		}

		// the length of a packet received by my NIC is 4byte aligned.
		// thus prune bytes in excess of IP4 length.
		reallen := int(Ntohs(ippkt.Tlen)) + ETHERLEN
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

		proto := ippkt.Proto
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

func Net_init(pm mem.Page_i) {
	pagemem = pm
	netchk()

	bigtw._tcptimers_start()

	nics.m = new(map[Ip4_t]nic_i)
	*nics.m = make(map[Ip4_t]nic_i)
	arptbl.m = make(map[Ip4_t]*arprec_t)
	arptbl.waiters = make(map[Ip4_t][]chan bool)
	arptbl.enttimeout = 20 * time.Minute
	arptbl.restimeout = 5 * time.Second

	lo.lo_start()
	Nic_insert(lo.lip, lo)

	Routetbl.init()

	go icmp_daemon()

	tcpcons.init()

	_rstchan = make(chan rstmsg_t, 32)
	nrst := 4
	for i := 0; i < nrst; i++ {
		go rst_daemon()
	}

	net_test()
}

func net_test() {
	if false {
		me := Ip4_t(0x121a0531)
		netmask := Ip4_t(0xfffffe00)
		// 18.26.5.1
		gw := Ip4_t(0x121a0401)
		if Routetbl.Defaultgw(me, gw) != 0 {
			panic("no")
		}
		net := me & netmask
		if Routetbl.Insert_local(me, net, netmask) != 0 {
			panic("no")
		}

		net = Ip4_t(0x0a000000)
		netmask = Ip4_t(0xffffff00)
		gw1 := Ip4_t(0x0a000001)
		if Routetbl.insert_gateway(me, net, netmask, gw1) != 0 {
			panic("no")
		}

		net = Ip4_t(0x0a000000)
		netmask = Ip4_t(0xffff0000)
		gw2 := Ip4_t(0x0a000002)
		if Routetbl.insert_gateway(me, net, netmask, gw2) != 0 {
			panic("no")
		}

		Routetbl.routes.dump()

		dip := Ip4_t(0x0a000003)
		a, b, c := Routetbl.Lookup(dip)
		if c != 0 {
			panic("error")
		}
		if a != me {
			panic("bad local")
		}
		if b != gw1 {
			panic("exp gw1")
		}

		dip = Ip4_t(0x0a000103)
		a, b, c = Routetbl.Lookup(dip)
		if c != 0 {
			panic("error")
		}
		if a != me {
			panic("bad local")
		}
		if b != gw2 {
			fmt.Printf("** %x %x\n", b, gw1)
			fmt.Printf("** %v\n", Routetbl.routes.subnets)
			panic("exp gw2")
		}
	}

	// test ACK check wrapping
	tcb := &Tcptcb_t{}
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
	d(30, 0xfffffff0, 0x10+30)

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

	tr := tcprsegs_t{}
	winend := uint32(1000)
	if tr.recvd(0, winend, 0, 10) != 10 {
		panic("no")
	}
	if len(tr.segs) != 0 {
		panic("no")
	}
	if tr.recvd(10, winend, 10, 10) != 20 {
		panic("no")
	}
	if len(tr.segs) != 0 {
		panic("no")
	}
	if tr.recvd(20, winend, 30, 10) != 20 {
		panic("no")
	}
	if tr.recvd(20, winend, 50, 10) != 20 {
		panic("no")
	}
	if len(tr.segs) != 2 {
		panic("no")
	}
	if tr.recvd(20, winend, 40, 10) != 20 {
		panic("no")
	}
	if tr.recvd(20, winend, 20, 10) != 60 {
		panic("no")
	}
	if len(tr.segs) != 0 {
		panic("no")
	}
	winend = 10
	nxt := uint32(0xffffff00)
	if tr.recvd(nxt, winend, 0, 10) != nxt {
		panic("no")
	}
	if len(tr.segs) != 1 {
		panic("no")
	}
	if tr.recvd(nxt, winend, nxt, 0x100) != 10 {
		panic("no")
	}
	if len(tr.segs) != 0 {
		panic("no")
	}
}

type lomsg_t struct {
	buf     []uint8
	tcphlen int
	mss     int
	tso     bool
}

type lo_t struct {
	txc chan lomsg_t
	mac Mac_t
	lip Ip4_t
}

var lo = &lo_t{}

func (l *lo_t) lo_start() {
	l.lip = Ip4_t(0x7f000001)
	l.txc = make(chan lomsg_t, 128)
	go l._daemon()
}

func (l *lo_t) _daemon() {
	for {
		res.Kunresdebug()
		res.Kresdebug(res.Onek, "lo daemon")
		lm := <-l.txc
		if lm.tso {
			l._tso(lm.buf, lm.tcphlen, lm.mss)
		} else {
			sg := [][]uint8{lm.buf}
			Net_start(sg, len(lm.buf))
		}
	}
}

func (l *lo_t) _send(lm lomsg_t) bool {
	sent := true
	select {
	case l.txc <- lm:
	default:
		sent = false
	}
	return sent
}

func (l *lo_t) _copysend(buf [][]uint8, tcphl, mss int) bool {
	if (tcphl == 0) != (mss == 0) {
		panic("both or none")
	}
	tlen := 0
	for _, s := range buf {
		tlen += len(s)
	}
	b := make([]uint8, tlen)
	to := b
	for _, s := range buf {
		did := copy(to, s)
		if did < len(s) {
			panic("must fit")
		}
		to = to[did:]
	}
	return l._send(lomsg_t{buf: b, tcphlen: tcphl, mss: mss, tso: false})
}

func (l *lo_t) _tso(buf []uint8, tcphl, mss int) {
	if len(buf) < ETHERLEN+IP4LEN+TCPLEN {
		return
	}
	ip4, rest, ok := Sl2iphdr(buf[ETHERLEN:])
	if !ok {
		return
	}
	tcph, _, rest, ok := Sl2tcphdr(rest)
	if !ok {
		return
	}
	if tcph.Isrst() {
		panic("tcp tso reset")
	}
	doff := int(tcph.Dataoff>>4) * 4
	flen := ETHERLEN + IP4LEN + TCPLEN + doff
	if len(buf) < flen {
		return
	}
	lastfin := tcph.Isfin()
	lastpush := tcph.Ispush()
	fin := uint8(1 << 0)
	pu := uint8(1 << 3)
	tcph.Flags &^= (fin | pu)

	fullhdr := buf[ETHERLEN+IP4LEN+TCPLEN+doff:]
	sgbuf := [][]uint8{fullhdr, nil}
	for len(rest) != 0 {
		s := rest
		last := true
		if len(s) > mss {
			s = s[:mss]
			last = false
		}
		if last {
			if lastfin {
				tcph.Flags |= fin
			}
			if lastpush {
				tcph.Flags |= pu
			}
		}
		iplen := len(fullhdr[ETHERLEN:]) + len(s)
		if iplen > mss {
			panic("very naughty")
		}
		ip4.Tlen = Htons(uint16(iplen))
		sgbuf[1] = s
		Net_start(sgbuf, len(sgbuf[0])+len(sgbuf[1]))
		// only first packet gets syn
		if tcph.Issyn() {
			synf := uint8(1 << 1)
			tcph.Flags &^= synf
		}
		// increase sequence
		did := len(s)
		tcph.Seq = Htonl(uint32(did) + Ntohl(tcph.Seq))
		rest = rest[did:]
	}
}

func (l *lo_t) Tx_raw(buf [][]uint8) bool {
	return l._copysend(buf, 0, 0)
}

func (l *lo_t) Tx_ipv4(buf [][]uint8) bool {
	return l._copysend(buf, 0, 0)
}

func (l *lo_t) Tx_tcp(buf [][]uint8) bool {
	return l._copysend(buf, 0, 0)
}

func (l *lo_t) Tx_tcp_tso(buf [][]uint8, tcphlen, mss int) bool {
	return l._copysend(buf, tcphlen, mss)
}

func (l *lo_t) Lmac() *Mac_t {
	return &l.mac
}

func Netdump() {
	fmt.Printf("net dump\n")
	tcpcons.l.Lock()
	fmt.Printf("tcp table len: %v\n", len(tcpcons.econns))
	//for _, tcb := range tcpcons.econns {
	//	tcb.l.Lock()
	//	//if tcb.state == TIMEWAIT {
	//	//	tcb.l.Unlock()
	//	//	continue
	//	//}
	//	fmt.Printf("%v:%v -> %v:%v: %s\n",
	//	    ip2str(tcb.lip), tcb.lport,
	//	    ip2str(tcb.rip), tcb.rport,
	//	    statestr[tcb.state])
	//	tcb.l.Unlock()
	//}
	tcpcons.l.Unlock()
}
