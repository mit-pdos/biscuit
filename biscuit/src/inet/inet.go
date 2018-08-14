package inet

import "fmt"
import "unsafe"
import "util"
import "time"

type Ip4_t uint32

type Be16 uint16
type Be32 uint32

// convert little- to big-endian.
func Ip2sl(sl []uint8, ip Ip4_t) {
	sl[0] = uint8(ip >> 24)
	sl[1] = uint8(ip >> 16)
	sl[2] = uint8(ip >> 8)
	sl[3] = uint8(ip >> 0)
}

func Sl2ip(sl []uint8) Ip4_t {
	ret := Ip4_t(sl[0]) << 24
	ret |= Ip4_t(sl[1]) << 16
	ret |= Ip4_t(sl[2]) << 8
	ret |= Ip4_t(sl[3]) << 0
	return ret
}

func Ip2str(ip Ip4_t) string {
	return fmt.Sprintf("%d.%d.%d.%d", ip>>24, uint8(ip>>16),
		uint8(ip>>8), uint8(ip))
}

func Mac2str(m []uint8) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", m[0], m[1], m[2],
		m[3], m[4], m[5])
}

func Htons(v uint16) Be16 {
	return Be16(v>>8 | (v&0xff)<<8)
}

func Htonl(v uint32) Be32 {
	r := (v & 0x000000ff) << 24
	r |= (v & 0x0000ff00) << 8
	r |= (v & 0x00ff0000) >> 8
	r |= v >> 24
	return Be32(r)
}

func Ntohs(v Be16) uint16 {
	return uint16(v>>8 | (v&0xff)<<8)
}

func Ntohl(v Be32) uint32 {
	r := (v & 0x000000ff) << 24
	r |= (v & 0x0000ff00) << 8
	r |= (v & 0x00ff0000) >> 8
	r |= v >> 24
	return uint32(r)
}

const ARPLEN = int(unsafe.Sizeof(Arpv4_t{}))

// always big-endian
const macsz int = 6

type Mac_t [macsz]uint8

// arpv4_t gets padded if tpa is a uint32 instead of a byte array...
type Arpv4_t struct {
	Etherhdr_t
	htype Be16
	ptype Be16
	hlen  uint8
	plen  uint8
	oper  Be16
	sha   Mac_t
	spa   [4]uint8
	tha   Mac_t
	tpa   [4]uint8
}

func (ar *Arpv4_t) _init(smac *Mac_t) {
	arp := Htons(0x0806)
	ethernet := Htons(1)
	ipv4 := Htons(0x0800)
	macsz := uint8(6)
	ipv4sz := uint8(4)
	ar.etype = arp
	ar.htype = ethernet
	ar.ptype = ipv4
	ar.hlen = macsz
	ar.plen = ipv4sz
	copy(ar.smac[:], smac[:])
}

func (ar *Arpv4_t) Init_req(smac *Mac_t, sip, qip Ip4_t) {
	ar._init(smac)
	req := Htons(1)
	ar.oper = req
	copy(ar.sha[:], smac[:])
	Ip2sl(ar.spa[:], sip)
	Ip2sl(ar.tpa[:], qip)
	var dur Mac_t
	copy(ar.tha[:], dur[:])
	for i := range ar.dmac {
		ar.dmac[i] = 0xff
	}
}

func (ar *Arpv4_t) Init_reply(smac, dmac *Mac_t, sip, dip Ip4_t) {
	ar._init(smac)
	reply := Htons(2)
	ar.oper = reply
	copy(ar.sha[:], smac[:])
	copy(ar.tha[:], dmac[:])
	Ip2sl(ar.spa[:], sip)
	Ip2sl(ar.tpa[:], dip)
	copy(ar.dmac[:], dmac[:])
}

func (ar *Arpv4_t) Bytes() []uint8 {
	return (*[ARPLEN]uint8)(unsafe.Pointer(ar))[:]
}

const IP4LEN = int(unsafe.Sizeof(Ip4hdr_t{}))

// no options
type Ip4hdr_t struct {
	Vers_hdr uint8
	Dscp     uint8
	Tlen     Be16
	Ident    Be16
	Fl_frag  Be16
	Ttl      uint8
	Proto    uint8
	Cksum    Be16
	Sip      [4]uint8
	Dip      [4]uint8
}

func Sl2iphdr(buf []uint8) (*Ip4hdr_t, []uint8, bool) {
	if len(buf) < IP4LEN {
		return nil, nil, false
	}
	p := (*Ip4hdr_t)(unsafe.Pointer(&buf[0]))
	rest := buf[IP4LEN:]
	return p, rest, true
}

func (i4 *Ip4hdr_t) _init(l4len int, sip, dip Ip4_t, proto uint8) {
	var z Ip4hdr_t
	*i4 = z
	i4.Vers_hdr = 0x45
	i4.Tlen = Htons(uint16(l4len) + uint16(IP4LEN))
	// may want to toggle don't frag, specifically in TCP after not
	// receiving acks
	dontfrag := uint16(1 << 14)
	i4.Fl_frag = Htons(dontfrag)
	i4.Ttl = 0xff
	i4.Proto = proto
	Ip2sl(i4.Sip[:], sip)
	Ip2sl(i4.Dip[:], dip)
}

func (i4 *Ip4hdr_t) Init_icmp(icmplen int, sip, dip Ip4_t) {
	icmp := uint8(0x01)
	i4._init(icmplen, sip, dip, icmp)
}

func (i4 *Ip4hdr_t) Init_tcp(tcplen int, sip, dip Ip4_t) {
	tcp := uint8(0x06)
	i4._init(tcplen, sip, dip, tcp)
}

func (i4 *Ip4hdr_t) Bytes() []uint8 {
	return (*[IP4LEN]uint8)(unsafe.Pointer(i4))[:]
}

func (i4 *Ip4hdr_t) Hdrlen() int {
	return IP4LEN
}

const ETHERLEN = int(unsafe.Sizeof(Etherhdr_t{}))

type Etherhdr_t struct {
	dmac  Mac_t
	smac  Mac_t
	etype Be16
}

func (et *Etherhdr_t) _init(smac, dmac []uint8, etype uint16) {
	if len(smac) != 6 || len(dmac) != 6 {
		panic("weird mac len")
	}
	copy(et.smac[:], smac)
	copy(et.dmac[:], dmac)
	et.etype = Htons(etype)
}

func (et *Etherhdr_t) Init_ip4(smac, dmac []uint8) {
	etype := uint16(0x0800)
	et._init(smac, dmac, etype)
}

func (e *Etherhdr_t) Bytes() []uint8 {
	return (*[ETHERLEN]uint8)(unsafe.Pointer(e))[:]
}

type Tcphdr_t struct {
	Sport   Be16
	Dport   Be16
	Seq     Be32
	Ack     Be32
	Dataoff uint8
	Flags   uint8
	Win     Be16
	Cksum   Be16
	Urg     Be16
}

const TCPLEN = int(unsafe.Sizeof(Tcphdr_t{}))

func (t *Tcphdr_t) _init(sport, dport uint16, seq, ack uint32) {
	var z Tcphdr_t
	*t = z
	t.Sport = Htons(sport)
	t.Dport = Htons(dport)
	t.Seq = Htonl(seq)
	t.Ack = Htonl(ack)
	t.Dataoff = 0x50
}

func (t *Tcphdr_t) Init_syn(Sport, Dport uint16, seq uint32) {
	t._init(Sport, Dport, seq, 0)
	synf := uint8(1 << 1)
	t.Flags |= synf
}

func (t *Tcphdr_t) Init_synack(Sport, Dport uint16, seq, ack uint32) {
	t._init(Sport, Dport, seq, ack)
	synf := uint8(1 << 1)
	ackf := uint8(1 << 4)
	t.Flags |= synf | ackf
}

func (t *Tcphdr_t) Init_ack(Sport, Dport uint16, seq, ack uint32) {
	t._init(Sport, Dport, seq, ack)
	ackf := uint8(1 << 4)
	t.Flags |= ackf
}

func (t *Tcphdr_t) Init_rst(Sport, Dport uint16, seq uint32) {
	t._init(Sport, Dport, seq, 0)
	rst := uint8(1 << 2)
	t.Flags |= rst
}

func (t *Tcphdr_t) Set_opt(opt []uint8, tsopt []uint8, tsecr uint32) {
	if len(tsopt) < 10 {
		panic("not enough room for timestamps")
	}
	if len(opt)%4 != 0 {
		panic("options must be 32bit aligned")
	}
	words := len(opt) / 4
	t.Dataoff = uint8(TCPLEN/4+words) << 4
	tsval := Htonl(uint32(time.Now().UnixNano() >> 10))
	util.Writen(tsopt, 4, 2, int(tsval))
	util.Writen(tsopt, 4, 6, int(Htonl(tsecr)))
}

func (t *Tcphdr_t) Hdrlen() int {
	return int(t.Dataoff>>4) * 4
}

func (t *Tcphdr_t) Issyn() bool {
	synf := uint8(1 << 1)
	return t.Flags&synf != 0
}

func (t *Tcphdr_t) Isack() (uint32, bool) {
	ackf := uint8(1 << 4)
	return Ntohl(t.Ack), t.Flags&ackf != 0
}

func (t *Tcphdr_t) Isrst() bool {
	rst := uint8(1 << 2)
	return t.Flags&rst != 0
}

func (t *Tcphdr_t) Isfin() bool {
	fin := uint8(1 << 0)
	return t.Flags&fin != 0
}

func (t *Tcphdr_t) Ispush() bool {
	pu := uint8(1 << 3)
	return t.Flags&pu != 0
}

func (t *Tcphdr_t) Bytes() []uint8 {
	return (*[TCPLEN]uint8)(unsafe.Pointer(t))[:]
}

func (t *Tcphdr_t) Dump(sip, dip Ip4_t, opt Tcpopt_t, dlen int) {
	s := fmt.Sprintf("%s:%d -> %s:%d", Ip2str(sip), Ntohs(t.Sport),
		Ip2str(dip), Ntohs(t.Dport))
	if t.Issyn() {
		s += fmt.Sprintf(", S")
	}
	s += fmt.Sprintf(" [%v+%v]", Ntohl(t.Seq), dlen)
	if ack, ok := t.Isack(); ok {
		s += fmt.Sprintf(", A [%v]", ack)
	}
	if t.Isrst() {
		s += fmt.Sprintf(", R")
	}
	if t.Isfin() {
		s += fmt.Sprintf(", F")
	}
	if t.Ispush() {
		s += fmt.Sprintf(", P")
	}
	s += fmt.Sprintf(", win=%v", Ntohs(t.Win))
	if opt.Sackok {
		s += fmt.Sprintf(", SACKok")
	}
	if opt.Wshift != 0 {
		s += fmt.Sprintf(", wshift=%v", opt.Wshift)
	}
	if opt.Tsval != 0 {
		s += fmt.Sprintf(", timestamp=%v", opt.Tsval)
	}
	if opt.Mss != 0 {
		s += fmt.Sprintf(", MSS=%v", opt.Mss)
	}
	fmt.Printf("%s\n", s)
}

type Tcpopt_t struct {
	Wshift uint
	Tsval  uint32
	Tsecr  uint32
	Mss    uint16
	Tsok   bool
	Sackok bool
}

func _sl2tcpopt(buf []uint8) Tcpopt_t {
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

	var ret Tcpopt_t
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
			ret.Mss = Ntohs(Be16(util.Readn(buf, 2, 2)))
			buf = buf[4:]
		case owsopt:
			if len(buf) < 3 {
				break outer
			}
			ret.Wshift = uint(buf[2])
			buf = buf[3:]
		case osackok:
			ret.Sackok = true
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
			ret.Tsok = true
			ret.Tsval = Ntohl(Be32(util.Readn(buf, 4, 2)))
			ret.Tsecr = Ntohl(Be32(util.Readn(buf, 4, 6)))
			buf = buf[10:]
		}
	}
	return ret
}

func Sl2tcphdr(buf []uint8) (*Tcphdr_t, Tcpopt_t, []uint8, bool) {
	if len(buf) < TCPLEN {
		var z Tcpopt_t
		return nil, z, nil, false
	}
	p := (*Tcphdr_t)(unsafe.Pointer(&buf[0]))
	rest := buf[TCPLEN:]
	var opt Tcpopt_t
	doff := int(p.Dataoff>>4) * 4
	if doff > TCPLEN && doff <= len(buf) {
		opt = _sl2tcpopt(buf[TCPLEN:doff])
		rest = buf[doff:]
	}
	return p, opt, rest, true
}

type Tcppkt_t struct {
	Ether  Etherhdr_t
	Iphdr  Ip4hdr_t
	Tcphdr Tcphdr_t
}

// writes pseudo header partial cksum to the TCP header cksum field. the sum is
// not complemented so that the partial pseudo header cksum can be summed with
// the rest of the TCP cksum (offloaded to the NIC).
func (tp *Tcppkt_t) Crc(l4len int, sip, dip Ip4_t) {
	sum := uint32(uint16(sip))
	sum += uint32(uint16(sip >> 16))
	sum += uint32(uint16(dip))
	sum += uint32(uint16(dip >> 16))
	sum += uint32(tp.Iphdr.Proto)
	sum += uint32(l4len)
	lm := uint32(^uint16(0))
	for sum&^0xffff != 0 {
		sum = (sum >> 16) + (sum & lm)
	}
	tp.Tcphdr.Cksum = Htons(uint16(sum))
}

func (tp *Tcppkt_t) Hdrbytes() ([]uint8, []uint8, []uint8) {
	return tp.Ether.Bytes(), tp.Iphdr.Bytes(), tp.Tcphdr.Bytes()
}

type Icmppkt_t struct {
	Ether Etherhdr_t
	Iphdr Ip4hdr_t
	Typ   uint8
	Code  uint8
	// cksum is endian agnostic, so long as the used endianness is
	// consistent throughout the checksum computation
	Cksum uint16
	Ident Be16
	Seq   Be16
	Data  []uint8
}

func (ic *Icmppkt_t) Init(smac, dmac *Mac_t, sip, dip Ip4_t, typ uint8,
	data []uint8) {
	var z Icmppkt_t
	*ic = z
	l4len := len(data) + 2*1 + 3*2
	ic.Ether.Init_ip4(smac[:], dmac[:])
	ic.Iphdr.Init_icmp(l4len, sip, dip)
	ic.Typ = typ
	ic.Data = data
}

func (ic *Icmppkt_t) Crc() {
	sum := uint32(ic.Typ) + uint32(ic.Code)<<8
	sum += uint32(ic.Cksum)
	sum += uint32(ic.Ident)
	sum += uint32(ic.Seq)
	buf := ic.Data
	for len(buf) > 1 {
		sum += uint32(buf[0]) + uint32(buf[1])<<8
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
	ic.Cksum = ret
}

func (ic *Icmppkt_t) Hdrbytes() []uint8 {
	const hdrsz = ETHERLEN + IP4LEN + 2*1 + 3*2
	return (*[hdrsz]uint8)(unsafe.Pointer(ic))[:]
}
