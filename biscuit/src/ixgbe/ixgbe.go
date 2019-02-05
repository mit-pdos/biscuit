package ixgbe

import "fmt"
import "runtime"
import "sync"
import "sync/atomic"
import "time"
import "unsafe"

import "apic"
import "bnet"
import "bounds"
import . "inet"
import "mem"
import "msi"
import "pci"
import "res"
import "stats"

type ixgbereg_t uint

const (
	CTRL ixgbereg_t = 0x0
	// the x540 terminology is confusing regarding interrupts; an interrupt
	// is enabled when its bit is set in the mask set register (ims) and
	// disabled when cleared.
	CTRL_EXT    = 0x18
	EICR        = 0x800
	EIAC        = 0x810
	EICS        = 0x808
	EICS1       = 0xa90
	EICS2       = 0xa94
	EIMS        = 0x880
	EIMS1       = 0xaa0
	EIMS2       = 0xaa4
	EIMC        = 0x888
	EIMC1       = 0xab0
	EIMC2       = 0xab4
	EIAM        = 0x890
	GPIE        = 0x898
	EIAM1       = 0xad0
	EIAM2       = 0xad4
	PFVTCTL     = 0x51b0
	RTRPCS      = 0x2430
	RDRXCTL     = 0x2f00
	PFQDE       = 0x2f04
	RTRUP2TC    = 0x3020
	RTTUP2TC    = 0xc800
	DTXMXSZRQ   = 0x8100
	SECTXMINIFG = 0x8810
	HLREG0      = 0x4240
	MFLCN       = 0x4294
	RTTDQSEL    = 0x4904
	RTTDT1C     = 0x4908
	RTTDCS      = 0x4900
	RTTPCS      = 0xcd00
	MRQC        = 0xec80
	MTQC        = 0x8120
	MSCA        = 0x425c
	MSRWD       = 0x4260
	LINKS       = 0x42a4
	DMATXCTL    = 0x4a80
	DTXTCPFLGL  = 0x4a88
	DTXTCPFLGH  = 0x4a8c
	EEMNGCTL    = 0x10110
	SWSM        = 0x10140
	SW_FW_SYNC  = 0x10160
	RXFECCERR0  = 0x51b8
	FCTRL       = 0x5080
	RXCSUM      = 0x5000
	RXCTRL      = 0x3000
	// statistic reg4sters
	SSVPC   = 0x8780
	GPTC    = 0x4080
	TXDGPC  = 0x87a0
	TPT     = 0x40d4
	PTC64   = 0x40d8
	PTC127  = 0x40dc
	MSPDC   = 0x4010
	XEC     = 0x4120
	BPTC    = 0x40f4
	FCCRC   = 0x5118
	B2OSPC  = 0x41c0
	B2OGPRC = 0x2f90
	O2BGPTC = 0x41c4
	O2BSPC  = 0x87b0
	CRCERRS = 0x4000
	ILLERRC = 0x4004
	ERRBC   = 0x4008
	GPRC    = 0x4074
	PRC64   = 0x405c
	PRC127  = 0x4060

	FLA = 0x1001c
)

func _xreg(start, idx, max, step uint) ixgbereg_t {
	// XXX comment this out later so compiler can inline all these register
	// calculators
	if idx >= max {
		panic("bad x540 reg")
	}
	return ixgbereg_t(start + idx*step)
}

func template(n int) ixgbereg_t {
	return _xreg(0xa600, uint(n), 245, 4)
}

func FCRTH(n int) ixgbereg_t {
	return _xreg(0x3260, uint(n), 8, 4)
}

func RDBAL(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x1000, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd000, uint(n)-64, 128-64, 0x40)
	}
}

func RDBAH(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x1004, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd004, uint(n)-64, 128-64, 0x40)
	}
}

func RDLEN(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x1008, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd008, uint(n)-64, 128-64, 0x40)
	}
}

func SRRCTL(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x1014, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd014, uint(n)-64, 128-64, 0x40)
	}
}

func RXDCTL(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x1028, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd028, uint(n)-64, 128-64, 0x40)
	}
}

func RDT(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x1018, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd018, uint(n)-64, 128-64, 0x40)
	}
}

func RDH(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x1010, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd010, uint(n)-64, 128-64, 0x40)
	}
}

func QPRC(n int) ixgbereg_t {
	return _xreg(0x1030, uint(n), 16, 0x40)
}

func QPRDC(n int) ixgbereg_t {
	return _xreg(0x1430, uint(n), 16, 0x40)
}

func PFUTA(n int) ixgbereg_t {
	return _xreg(0xf400, uint(n), 128, 4)
}

func TDBAL(n int) ixgbereg_t {
	return _xreg(0x6000, uint(n), 128, 0x40)
}

func TDBAH(n int) ixgbereg_t {
	return _xreg(0x6004, uint(n), 128, 0x40)
}

func TDLEN(n int) ixgbereg_t {
	return _xreg(0x6008, uint(n), 128, 0x40)
}

func TDH(n int) ixgbereg_t {
	return _xreg(0x6010, uint(n), 128, 0x40)
}

func TDT(n int) ixgbereg_t {
	return _xreg(0x6018, uint(n), 128, 0x40)
}

func TXDCTL(n int) ixgbereg_t {
	return _xreg(0x6028, uint(n), 128, 0x40)
}

func TDWBAL(n int) ixgbereg_t {
	return _xreg(0x6038, uint(n), 128, 0x40)
}

func TDWBAH(n int) ixgbereg_t {
	return _xreg(0x603c, uint(n), 128, 0x40)
}

func RSCCTL(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x102c, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd02c, uint(n-64), 128-64, 0x40)
	}
}

func DCA_RXCTRL(n int) ixgbereg_t {
	if n < 64 {
		return _xreg(0x100c, uint(n), 64, 0x40)
	} else {
		return _xreg(0xd00c, uint(n-64), 128-64, 0x40)
	}
}

func RTRPT4C(n int) ixgbereg_t {
	return _xreg(0x2140, uint(n), 8, 4)
}

func RTTPT2C(n int) ixgbereg_t {
	return _xreg(0xdc20, uint(n), 8, 4)
}

func RTTDT2C(n int) ixgbereg_t {
	return _xreg(0x4910, uint(n), 8, 4)
}

func RXPBSIZE(n int) ixgbereg_t {
	return _xreg(0x3c00, uint(n), 8, 4)
}

func TXPBSIZE(n int) ixgbereg_t {
	return _xreg(0xcc00, uint(n), 8, 4)
}

func TXPBTHRESH(n int) ixgbereg_t {
	return _xreg(0x4950, uint(n), 8, 4)
}

func TQSM(n int) ixgbereg_t {
	return _xreg(0x8600, uint(n), 32, 4)
}

func IVAR(n int) ixgbereg_t {
	return _xreg(0x900, uint(n), 64, 4)
}

func EITR(n int) ixgbereg_t {
	if n < 24 {
		return _xreg(0x820, uint(n), 128, 4)
	} else {
		return _xreg(0x12300, uint(n-24), 128-24, 4)
	}
}

func PFVLVFB(n int) ixgbereg_t {
	return _xreg(0xf200, uint(n), 128, 4)
}

func VFTA(n int) ixgbereg_t {
	return _xreg(0xa000, uint(n), 128, 4)
}

func PFVFSPOOF(n int) ixgbereg_t {
	return _xreg(0x8200, uint(n), 8, 4)
}

func MPSAR(n int) ixgbereg_t {
	return _xreg(0xa600, uint(n), 256, 4)
}

func QPTC_L(n int) ixgbereg_t {
	return _xreg(0x8700, uint(n), 16, 8)
}

func QPTC(n int) ixgbereg_t {
	return _xreg(0x8680, uint(n), 16, 4)
}

func RAH(n uint) ixgbereg_t {
	return _xreg(0xa204, uint(n), 128, 8)
}

func RAL(n uint) ixgbereg_t {
	return _xreg(0xa200, uint(n), 128, 8)
}

// MDIO device is in bits [20:16] and MDIO reg is in [15:0]
type ixgbephyreg_t uint

const (
	// link status here (Auto-Negotiation Reserved Vendor Status 1)
	PHY_LINK ixgbephyreg_t = 0x07c810
	ALARMS1                = 0x1ecc00
)

type rxdesc_t struct {
	hwdesc *struct {
		p_data uint64
		p_hdr  uint64
	}
	// saved buffer physical addresses since hardware overwrites them once
	// a packet is received
	p_pbuf uint64
	p_hbuf uint64
}

func (rd *rxdesc_t) init(pbuf mem.Pa_t, hbuf uintptr, hw *int) {
	rd.p_pbuf = uint64(pbuf)
	rd.p_hbuf = uint64(hbuf)
	rd.hwdesc = (*struct {
		p_data uint64
		p_hdr  uint64
	})(unsafe.Pointer(hw))
}

func (rd *rxdesc_t) ready() {
	if (rd.p_pbuf|rd.p_hbuf)&1 != 0 {
		panic("rx buffers must be word aligned")
	}
	rd.hwdesc.p_data = rd.p_pbuf
	rd.hwdesc.p_hdr = rd.p_hbuf
}

func (rd *rxdesc_t) rxdone() bool {
	dd := uint64(1)
	// compiler barrier
	return atomic.LoadUint64(&rd.hwdesc.p_hdr)&dd != 0
}

func (rd *rxdesc_t) eop() bool {
	eop := uint64(1 << 1)
	return rd.hwdesc.p_hdr&eop != 0
}

func (rd *rxdesc_t) _fcoe() bool {
	t := atomic.LoadUint64(&rd.hwdesc.p_data)
	return t&(1<<15) != 0
}

// the NIC drops IP4/TCP packets that have a bad checksum when FCTRL.SBP is 0.
func (rd *rxdesc_t) ipcsumok() bool {
	if rd._fcoe() {
		return true
	}
	v := atomic.LoadUint64(&rd.hwdesc.p_hdr)
	ipcs := v&(1<<6) != 0
	ipe := v&(1<<31) != 0
	return !ipcs || (ipcs && !ipe)
}

func (rd *rxdesc_t) l4csumok() bool {
	if rd._fcoe() {
		return true
	}
	v := atomic.LoadUint64(&rd.hwdesc.p_hdr)
	l4i := v&(1<<5) != 0
	l4e := v&(1<<30) != 0
	return !l4i || (l4i && !l4e)
}

func (rd *rxdesc_t) pktlen() int {
	return int((rd.hwdesc.p_hdr >> 32) & 0xffff)
}

func (rd *rxdesc_t) hdrlen() int {
	return int((rd.hwdesc.p_data >> 21) & 0x3ff)
}

type txdesc_t struct {
	hwdesc *struct {
		p_addr uint64
		rest   uint64
	}
	p_buf uint64
	bufsz uint64
	ctxt  bool
	eop   bool
}

func (td *txdesc_t) init(p_addr mem.Pa_t, len uintptr, hw *int) {
	td.p_buf = uint64(p_addr)
	td.bufsz = uint64(len)
	td.hwdesc = (*struct {
		p_addr uint64
		rest   uint64
	})(unsafe.Pointer(hw))
}

func (td *txdesc_t) ctxt_ipv4(_maclen, _ip4len int) {
	td.ctxt = true
	td.eop = false
	maclen := uint64(_maclen)
	td.hwdesc.p_addr = maclen << 9
	hlen := uint64(_ip4len)
	td.hwdesc.p_addr |= hlen
	// DTYP = 0010b
	td.hwdesc.rest = 0x2 << 20
	// DEXT = 1
	td.hwdesc.rest |= 1 << 29
	// TUCMD.IPV4 = 1
	ipv4 := uint64(1 << 10)
	td.hwdesc.rest |= ipv4
	// leave IDX zero
}

func (td *txdesc_t) ctxt_tcp(_maclen, _ip4len int) {
	td.ctxt = true
	td.eop = false
	maclen := uint64(_maclen)
	td.hwdesc.p_addr = maclen << 9
	hlen := uint64(_ip4len)
	td.hwdesc.p_addr |= hlen
	// DTYP = 0010b
	td.hwdesc.rest = 0x2 << 20
	// DEXT = 1
	td.hwdesc.rest |= 1 << 29
	// TUCMD.L4T = 1 (TCP)
	tcp := uint64(1 << 11)
	td.hwdesc.rest |= tcp
}

func (td *txdesc_t) ctxt_tcp_tso(_maclen, _ip4len, _l4hdrlen, _mss int) {
	td.ctxt = true
	td.eop = false
	maclen := uint64(_maclen)
	td.hwdesc.p_addr = maclen << 9
	hlen := uint64(_ip4len)
	td.hwdesc.p_addr |= hlen
	// DTYP = 0010b
	td.hwdesc.rest = 0x2 << 20
	// DEXT = 1
	td.hwdesc.rest |= 1 << 29
	// TUCMD.L4T = 1 (TCP)
	tcp := uint64(1 << 11)
	// TUCMD.IPV4 = 1
	ipv4 := uint64(1 << 10)
	td.hwdesc.rest |= tcp | ipv4
	mss := uint64(_mss)
	if mss&^0xffff != 0 {
		panic("large mss")
	}
	if _ip4len+_l4hdrlen+_mss > 1500 {
		panic("packets > mtu")
	}
	td.hwdesc.rest |= mss << 48
	l4hdrlen := uint64(_l4hdrlen)
	if l4hdrlen&^0xff != 0 {
		panic("large l4hdrlen")
	}
	// XXXPANIC
	timeoptpadlen := 12
	msslen := 4
	if _l4hdrlen != TCPLEN && _l4hdrlen != TCPLEN+timeoptpadlen &&
		_l4hdrlen != TCPLEN+timeoptpadlen+msslen {
		panic("weird tcp header len (options?)")
	}
	td.hwdesc.rest |= l4hdrlen << 40
}

// returns remaining bytes
func (td *txdesc_t) data_continue(src [][]uint8) [][]uint8 {
	if len(src) == 0 {
		panic("empty buf")
	}

	td.ctxt = false
	td.eop = false
	dst := mem.Dmaplen(mem.Pa_t(td.p_buf), int(td.bufsz))
	paylen := uint64(0)
	for len(dst) != 0 && len(src) != 0 {
		cursrc := src[0]
		did := copy(dst, cursrc)
		paylen += uint64(did)
		dst = dst[did:]
		src[0] = cursrc[did:]
		if len(src[0]) == 0 {
			src = src[1:]
		}
	}

	// does this descriptor contain the end of the packet?
	last := false
	if len(src) == 0 {
		last = true
	}

	td.hwdesc.p_addr = td.p_buf
	// DTYP = 0011b
	td.hwdesc.rest = 0x3 << 20
	// DEXT = 1
	td.hwdesc.rest |= 1 << 29
	rs := uint64(1 << 27)
	eop := uint64(1 << 24)
	ifcs := uint64(1 << 25)
	td.hwdesc.rest |= ifcs
	if last {
		td.hwdesc.rest |= eop | rs
		td.eop = true
	}
	td._dtalen(paylen)
	return src
}

// returns remaining bytes
func (td *txdesc_t) mkraw(src [][]uint8, tlen int) [][]uint8 {
	ret := td.data_continue(src)
	td._paylen(uint64(tlen))
	return ret
}

// offloads ipv4 checksum calculation to the NIC. returns the remaining bytes.
func (td *txdesc_t) mkipv4(src [][]uint8, tlen int) [][]uint8 {
	ret := td.data_continue(src)
	td._paylen(uint64(tlen))
	// POPTS.IXSM = 1
	ixsm := uint64(1 << 40)
	td.hwdesc.rest |= ixsm
	// no need for index or CC
	return ret
}

// offloads IPv4/TCP checksum to the NIC. returns the remaining bytes.
func (td *txdesc_t) mktcp(src [][]uint8, tlen int) [][]uint8 {
	ret := td.mkipv4(src, tlen)
	// POPTS.TXSM = 1
	txsm := uint64(1 << 41)
	td.hwdesc.rest |= txsm
	return ret
}

// offloads segmentation and checksums to the NIC. the TCP header's
// pseudo-header checksum must not include the length.
func (td *txdesc_t) mktcp_tso(src [][]uint8, tcphlen, tlen int) [][]uint8 {
	ret := td.mktcp(src, tlen)
	// DCMD.TSE = 1
	tse := uint64(1 << 31)
	td.hwdesc.rest |= tse
	if tlen <= ETHERLEN+IP4LEN+TCPLEN {
		panic("no payload")
	}
	// for TSO, PAYLEN is the # of bytes in the TCP payload (not the total
	// packet size, as usual).
	td._paylen(uint64(tlen - ETHERLEN - IP4LEN - tcphlen))
	return ret
}

func (td *txdesc_t) _dtalen(v uint64) {
	mask := uint64(0xffff)
	if v&^mask != 0 || v == 0 {
		panic("bad dtalen")
	}
	td.hwdesc.rest &^= mask
	td.hwdesc.rest |= v
}

func (td *txdesc_t) _paylen(v uint64) {
	mask := uint64(0x3ffff)
	if v&^mask != 0 || v == 0 {
		panic("bad paylen")
	}
	td.hwdesc.rest &^= mask << 46
	td.hwdesc.rest |= v << 46
}

// returns true when this tx descriptor is guaranteed to no longer be used by
// the hardware. the hardware does not set DD for context descriptors or
// descriptors without RS set (which can only be set on the last descriptor of
// the packet and thus, with this driver, always has EOP set). thus a context
// descriptor is available for reuse iff there is a data descriptor between
// this tx descriptor and TDH that has both DD and EOP set (EOP is reserved
// after writeback, thus we have to keep track of it ourselves).
func (td *txdesc_t) txdone() bool {
	if td.ctxt || !td.eop {
		return false
	}
	dd := uint64(1 << 32)
	return atomic.LoadUint64(&td.hwdesc.rest)&dd != 0
}

func (td *txdesc_t) _avail() {
	td.ctxt = false
	td.eop = true
	dd := uint64(1 << 32)
	td.hwdesc.rest |= dd
}

// if hw may writeback this descriptor, wait until the CPU observes the hw's
// write
func (td *txdesc_t) wbwait() {
	// the hardware does not report when a context descriptor or non-last
	// data descriptor is no long in use.
	if td.ctxt || !td.eop {
		return
	}
	for !td.txdone() {
		waits++
	}
}

type ixgbe_t struct {
	tag     pci.Pcitag_t
	bar0    []uint32
	_locked bool
	txs     []ixgbetx_t
	rx      struct {
		ndescs uint32
		descs  []rxdesc_t
		pkt    [][]uint8
		tailc  uint32
	}
	pgs    int
	linkup bool
	// big-endian
	mac Mac_t
	ip  Ip4_t
	mtu int
}

type ixgbetx_t struct {
	sync.Mutex
	qnum   int
	ndescs uint32
	descs  []txdesc_t
	// cache tail register in order to avoid reading NIC registers, which
	// apparently take 1-2us.
	tailc uint32
	// cache of most recent context descriptor parameters
	cc struct {
		istcp bool
		ethl  int
		ip4l  int
	}
}

func (x *ixgbe_t) init(t pci.Pcitag_t) {
	x.tag = t

	bar0, l := pci.Pci_bar_mem(t, 0)
	x.bar0 = mem.Dmaplen32(bar0, l)

	x.rx.pkt = make([][]uint8, 0, 30)

	x.mtu = 1500

	v := pci.Pci_read(t, 0x4, 2)
	memen := 1 << 1
	if v&memen == 0 {
		panic("memory access disabled")
	}
	busmaster := 1 << 2
	if v&busmaster == 0 {
		panic("busmaster disabled")
	}
	pciebmdis := uint32(1 << 2)
	y := x.rl(CTRL)
	if y&pciebmdis != 0 {
		panic("pcie bus master disable set")
	}
	nosnoop_en := 1 << 11
	v = pci.Pci_read(t, 0xa8, 2)
	if v&nosnoop_en == 0 {
		panic("pcie no snoop disabled")
	}
}

func (x *ixgbe_t) rs(reg ixgbereg_t, val uint32) {
	if reg%4 != 0 {
		panic("bad reg")
	}
	runtime.Store32(&x.bar0[reg/4], val)
}

func (x *ixgbe_t) rl(reg ixgbereg_t) uint32 {
	if reg%4 != 0 {
		panic("bad reg")
	}
	return atomic.LoadUint32(&x.bar0[reg/4])
}

func (x *ixgbe_t) log(fm string, args ...interface{}) {
	b, d, f := pci.Breakpcitag(x.tag)
	s := fmt.Sprintf("X540:(%v:%v:%v): %s\n", b, d, f, fm)
	fmt.Printf(s, args...)
}

func (x *ixgbe_t) _reset() {
	// if there is any chance that DMA may race with _reset, we must modify
	// _reset to execute the master disable protocol in (5.2.4.3.2)

	// link reset + device reset
	lrst := uint32(1 << 3)
	rst := uint32(1 << 26)
	v := x.rl(CTRL)
	v |= rst
	v |= lrst
	x.rs(CTRL, v)
	// 8.2.4.1.1: wait 1ms before checking reset bit after setting
	<-time.After(time.Millisecond)
	for x.rl(CTRL)&rst != 0 {
	}
	// x540 doc 4.6.3.2: wait for 10ms after reset "to enable a
	// smooth initialization flow"
	<-time.After(10 * time.Millisecond)
}

func (x *ixgbe_t) _int_disable() {
	maskall := ^uint32(0)
	x.rs(EIMC, maskall)
	x.rs(EIMC1, maskall)
	x.rs(EIMC2, maskall)
}

func (x *ixgbe_t) _phy_read(preg ixgbephyreg_t) uint16 {
	if preg&^((1<<21)-1) != 0 {
		panic("bad phy reg")
	}
	mdicmd := uint32(1 << 30)
	// wait for MDIO to be ready
	for x.rl(MSCA)&mdicmd != 0 {
	}
	opaddr := uint32(0)
	phyport := uint32(0)
	v := uint32(preg) | phyport<<21 | opaddr<<26 | mdicmd
	x.rs(MSCA, v)
	for x.rl(MSCA)&mdicmd != 0 {
	}
	opread := uint32(3)
	v = uint32(preg) | phyport<<21 | opread<<26 | mdicmd
	x.rs(MSCA, v)
	for x.rl(MSCA)&mdicmd != 0 {
	}
	ret := x.rl(MSRWD)
	return uint16(ret >> 16)
}

var lockstat struct {
	sw    int
	hw    int
	fw    int
	nvmup int
	swmng int
}

// acquires the "lock" protecting the semaphores. returns whether fw timedout
func (x *ixgbe_t) _reg_acquire() bool {
	to := 3 * time.Second
	st := time.Now()
	smbi := uint32(1 << 0)
	for x.rl(SWSM)&smbi != 0 {
		if time.Since(st) > to {
			panic("SWSM timeout!")
		}
	}
	var fwdead bool
	st = time.Now()
	regsmp := uint32(1 << 31)
	for x.rl(SW_FW_SYNC)&regsmp != 0 {
		if time.Since(st) > to {
			x.log("SW_FW_SYNC timeout!")
			fwdead = true
			break
		}
	}
	return fwdead
}

func (x *ixgbe_t) _reg_release() {
	regsmp := uint32(1 << 31)
	x.rs(SW_FW_SYNC, x.rl(SW_FW_SYNC)&^regsmp)
	x.rs(SWSM, 0)
}

// takes all semaphores protecting NIC's NVM, PHY 0/1, and shared MAC registers
// from concurrent access by software and firmware
func (x *ixgbe_t) hwlock() {
	if x._locked {
		panic("two hwlocks")
	}
	for i := 0; i < 100; i++ {
		if x._hwlock() {
			x._locked = true
			return
		}
		<-time.After(10 * time.Millisecond)
	}
	fmt.Printf("lock stats: %v\n", lockstat)
	panic("hwlock timedout")
}

// returns true if the called acquired the software/firmware semaphore
func (x *ixgbe_t) _hwlock() bool {
	// 11.7.5; this semaphore protects NVM, PHY[01], and MAC shared regs
	fwdead := x._reg_acquire()

	//sw_nvm  := uint32(1 << 0)
	//sw_phy0 := uint32(1 << 1)
	//sw_phy1 := uint32(1 << 2)
	//sw_mac  := uint32(1 << 3)
	hw_nvm := uint32(1 << 4)
	fw_nvm := uint32(1 << 5)
	fw_phy0 := uint32(1 << 6)
	fw_phy1 := uint32(1 << 7)
	fw_mac := uint32(1 << 8)
	nvm_up := uint32(1 << 9)
	sw_mng := uint32(1 << 10)
	fwbits := fw_nvm | fw_phy0 | fw_phy1 | fw_mac

	ret := false
	v := x.rl(SW_FW_SYNC)
	if v&hw_nvm != 0 {
		lockstat.hw++
		goto out
	}
	if v&0xf != 0 {
		lockstat.sw++
		goto out
	}
	if !fwdead && v&fwbits != 0 {
		lockstat.fw++
		goto out
	}
	if v&nvm_up != 0 {
		lockstat.nvmup++
	}
	if v&sw_mng != 0 {
		lockstat.swmng++
	}
	x.rs(SW_FW_SYNC, v|0xf)
	ret = true
out:
	x._reg_release()
	return ret
}

func (x *ixgbe_t) hwunlock() {
	if !x._locked {
		panic("not locked")
	}
	x._locked = false
	x._reg_acquire()
	v := x.rl(SW_FW_SYNC)
	// clear sw bits
	v &^= 0xf
	x.rs(SW_FW_SYNC, v)
	x._reg_release()
}

// returns linkup and link speed
func (x *ixgbe_t) linkinfo() (bool, string) {
	link := uint32(1 << 30)
	v := x.rl(LINKS)
	speed := "unknown"
	switch (v >> 28) & 3 {
	case 1:
		speed = "100 Mb/s"
	case 2:
		speed = "1 Gb/s"
	case 3:
		speed = "10 Gb/s"
	}
	return v&link != 0, speed
}

func (x *ixgbe_t) wait_linkup(secs int) bool {
	link := uint32(1 << 30)
	st := time.Now()
	s := time.Duration(secs)
	for {
		v := x.rl(LINKS)
		if v&link != 0 {
			return true
		}
		if time.Since(st) > s*time.Second {
			return false
		}
		runtime.Gosched()
	}
}

func (x *ixgbe_t) pg_new() (*mem.Pg_t, mem.Pa_t) {
	x.pgs++
	a, b, ok := mem.Physmem.Refpg_new()
	if !ok {
		panic("oom during ixgbe init")
	}
	mem.Physmem.Refup(b)
	return a, b
}

func (x *ixgbe_t) Lmac() *Mac_t {
	return &x.mac
}

// returns after buf is enqueued to be trasmitted. buf's contents are copied to
// the DMA buffer, so buf's memory can be reused/freed
func (x *ixgbe_t) Tx_raw(buf [][]uint8) bool {
	return x._tx_nowait(buf, false, false, false, 0, 0)
}

func (x *ixgbe_t) Tx_ipv4(buf [][]uint8) bool {
	return x._tx_nowait(buf, true, false, false, 0, 0)
}

func (x *ixgbe_t) Tx_tcp(buf [][]uint8) bool {
	return x._tx_nowait(buf, true, true, false, 0, 0)
}

func (x *ixgbe_t) Tx_tcp_tso(buf [][]uint8, tcphlen, mss int) bool {
	return x._tx_nowait(buf, true, true, true, tcphlen, mss)
}

func (x *ixgbe_t) _tx_nowait(buf [][]uint8, ipv4, tcp, tso bool, tcphlen,
	mss int) bool {
	tq := runtime.CPUHint()
	myq := &x.txs[tq%len(x.txs)]
	myq.Lock()
	ok := x._tx_enqueue(myq, buf, ipv4, tcp, tso, tcphlen, mss)
	myq.Unlock()
	if !ok {
		fmt.Printf("tx packet(s) dropped!\n")
	}
	return ok
}

// returns true if the header sizes or context type have changed and thus a new
// context descriptor should be created. the TCP context descriptor includes
// IPV4 parameters. alternatively, we could simultaneously use both of the
// x540's context slots.
func (x *ixgbe_t) _ctxt_update(myq *ixgbetx_t, ipv4, tcp bool, ethl,
	ip4l int) bool {
	if !ipv4 && !tcp {
		return false
	}
	cc := &myq.cc
	wastcp := cc.istcp
	pdiffer := cc.ethl != ethl || cc.ip4l != ip4l
	if tcp == wastcp && !pdiffer {
		return false
	}
	cc.istcp = tcp
	cc.ethl = ethl
	cc.ip4l = ip4l
	return true
}

// caller must hold the ixgbetx_t's lock. returns true if buf was copied to the
// transmission queue.
func (x *ixgbe_t) _tx_enqueue(myq *ixgbetx_t, buf [][]uint8, ipv4, tcp,
	tso bool, tcphlen, mss int) bool {
	if len(buf) == 0 {
		panic("wut")
	}
	tail := myq.tailc
	tlen := 0
	for i := 0; i < len(buf); {
		if len(buf[i]) == 0 {
			copy(buf[i:], buf[i+1:])
			buf = buf[:len(buf)-1]
			continue
		}
		tlen += len(buf[i])
		i++
	}
	if tso && !tcp {
		panic("tso is only for tcp")
	}
	if tlen-ETHERLEN > 1500 && !tso {
		panic("should use tso")
	}
	if tlen == 0 {
		panic("wut")
	}
	need := tlen
	newtail := tail
	ctxtstale := (ipv4 || tcp) && x._ctxt_update(myq, ipv4, tcp, ETHERLEN,
		IP4LEN)
	if ctxtstale || tso {
		// segmentation offload requires a per-packet context
		// descriptor

		// first descriptor must be a context descriptor when using
		// checksum/segmentation offloading (only need 1 context for
		// all checksum offloads?). a successfull check for DD/eop
		// below implies that this context descriptor is free.
		newtail = (newtail + 1) % myq.ndescs
	}
	// starting from the last tail, look for a data descriptor that has DD
	// set and is the last descriptor in a packet, thus any previous
	// context descriptors (which may contain a packet's header for TSO)
	// are unused.
	for tt := newtail; ; {
		// find data descriptor with DD and eop
		for {
			fd := &myq.descs[tt]
			done := fd.txdone()
			if done {
				break
			} else if fd.eop && !done {
				// if this descriptor is the final data
				// descriptor of a packet and does not have DD
				// set, there aren't enough free tx descriptors
				return false
			}
			tt = (tt + 1) % myq.ndescs
		}
		// all tx descriptors between [tail, tt] are available. see if
		// these descriptors have buffers which are large enough to
		// hold buf. however, we cannot use tt even though it is free
		// since tt may be the last acceptable slot for TDT, but TDT
		// references the descriptor following the last usable
		// descriptor (and thus hardware will not process it).
		for newtail != tt && need != 0 {
			td := &myq.descs[newtail]
			bs := int(td.bufsz)
			if bs > need {
				need = 0
			} else {
				need -= bs
			}
			newtail = (newtail + 1) % myq.ndescs
		}
		if need == 0 {
			break
		} else {
			tt = (tt + 1) % myq.ndescs
		}
	}
	// the first descriptors are special
	fd := &myq.descs[tail]
	fd.wbwait()
	if tso {
		fd.ctxt_tcp_tso(ETHERLEN, IP4LEN, tcphlen, mss)
		tail = (tail + 1) % myq.ndescs
		fd = &myq.descs[tail]
		fd.wbwait()
		buf = fd.mktcp_tso(buf, tcphlen, tlen)
	} else if tcp {
		if ctxtstale {
			fd.ctxt_tcp(ETHERLEN, IP4LEN)
			tail = (tail + 1) % myq.ndescs
			fd = &myq.descs[tail]
			fd.wbwait()
		}
		buf = fd.mktcp(buf, tlen)
	} else if ipv4 {
		if ctxtstale {
			fd.ctxt_ipv4(ETHERLEN, IP4LEN)
			tail = (tail + 1) % myq.ndescs
			fd = &myq.descs[tail]
			fd.wbwait()
		}
		buf = fd.mkipv4(buf, tlen)
	} else {
		buf = fd.mkraw(buf, tlen)
	}
	tail = (tail + 1) % myq.ndescs
	for len(buf) != 0 {
		td := &myq.descs[tail]
		td.wbwait()
		buf = td.data_continue(buf)
		tail = (tail + 1) % myq.ndescs
	}
	if tail != newtail {
		panic("size miscalculated")
	}
	x.rs(TDT(myq.qnum), newtail)
	myq.tailc = newtail
	return true
}

func (x *ixgbe_t) rx_consume() {
	// tail itself must be empty
	tail := x.rx.tailc
	if x.rx.descs[tail].rxdone() {
		panic("tail must not have dd")
	}
	tailend := (tail + 1) % x.rx.ndescs
	if x.rx.descs[tailend].rxdone() {
		for {
			n := (tailend + 1) % x.rx.ndescs
			if !x.rx.descs[n].rxdone() {
				break
			}
			tailend = n
		}
	} else {
		// queue is still full
		spurs++
		return
	}
	// skip the empty tail
	tail = (tail + 1) % x.rx.ndescs
	otail := tail
	pkt := x.rx.pkt[0:1]
	for {
		rd := &x.rx.descs[tail]
		if !rd.rxdone() {
			panic("wut?")
		}
		// packet may span multiple descriptors (only for RSC when MTU
		// less than descriptor DMA buffer size?)
		buf := mem.Dmaplen(mem.Pa_t(rd.p_pbuf), rd.pktlen())
		pkt[0] = buf
		if !rd.eop() {
			panic("pkt > mtu?")
		}
		bnet.Net_start(pkt, len(buf))
		numpkts++
		if tail == tailend {
			break
		}
		tail++
		if tail == x.rx.ndescs {
			tail = 0
		}
	}
	// only reset descriptors for full packets
	for {
		rd := &x.rx.descs[otail]
		if !rd.rxdone() {
			panic("wut?")
		}
		rd.ready()
		if otail == tail {
			break
		}
		otail++
		if otail == x.rx.ndescs {
			otail = 0
		}
	}
	x.rs(RDT(0), tail)
	x.rx.tailc = tail
}

func (x *ixgbe_t) int_handler(vector msi.Msivec_t) {
	rantest := false
	r := bounds.Bounds(bounds.B_IXGBE_T_INT_HANDLER)
	res.Kreswait(r, "ixgbe int handler")
	for {
		res.Kunres()
		runtime.IRQsched(uint(vector))
		res.Kreswait(r, "ixgbe int handler")

		// interrupt status register clears on read
		st := x.rl(EICR)
		//x.log("*** NIC IRQ (%v) %#x", irqs, st)

		rx := uint32(1 << 0)
		tx := uint32(1 << 1)
		rxmiss := uint32(1 << 17)
		lsc := uint32(1 << 20)

		// XXX code for polling instead of using interrupts
		//runtime.Gosched()
		//var st uint32
		//if !rantest {
		//	x.log("link wait...\n")
		//	if !x.wait_linkup(10) {
		//		x.log("NO LINK\n")
		//		return
		//	}
		//	x.log("GOT LINK\n")
		//	st |= lsc
		//}

		//m := rx | tx | rxmiss | lsc
		//rhead := x.rl(RDH(0))
		//thead := x.rl(TDH(0))
		//for st & m == 0 {
		//	runtime.Gosched()
		//	st = x.rl(EICR)
		//	if x.rl(RDH(0)) != rhead {
		//		st |= rx
		//	}
		//	if x.rl(TDH(0)) != thead {
		//		st |= tx
		//	}
		//}

		if st&lsc != 0 {
			// link status change
			up, speed := x.linkinfo()
			x.linkup = up
			if up {
				x.log("link up @ %s", speed)
			} else {
				x.log("link down")
			}
			if up && !rantest {
				// 18.26.5.49 (bhw)
				me := Ip4_t(0x121a0531)
				if x.mac[3] == 0x51 {
					// 18.26.4.54 (bhw2)
					me = Ip4_t(0x121a0436)
				}
				x.ip = me
				bnet.Nic_insert(me, x)

				netmask := Ip4_t(0xfffffe00)
				// 18.26.5.1
				gw := Ip4_t(0x121a0401)
				bnet.Routetbl.Defaultgw(me, gw)
				net := me & netmask
				bnet.Routetbl.Insert_local(me, net, netmask)
				bnet.Routetbl.Dump()

				rantest = true
				//go x.tester1()
				//go x.tx_test2()
				go func() {
					for {
						//res.Kreswait(res.Onemeg,
						//	"stat printer")
						time.Sleep(10 * time.Second)
						v := x.rl(QPRDC(0))
						if v != 0 {
							fmt.Printf("rx drop:"+
								" %v\n", v)
						}
						if dropints != 0 {
							fmt.Printf("drop ints: %v\n", dropints)
							dropints = 0
						}
						res.Kunres()
					}
				}()
			}
		}
		if rxmiss&st != 0 {
			dropints++
		}
		if st&rx != 0 {
			// rearm rx descriptors
			x.rx_consume()
		}
		if st&tx != 0 {
			//x.txs[0].Lock()
			//x.txs[0].Unlock()
		}
	}
}

func (x *ixgbe_t) tester1() {
	stirqs := stats.Irqs
	st := time.Now()
	for {
		<-time.After(30 * time.Second)
		nirqs := stats.Irqs - stirqs
		drops := x.rl(QPRDC(0))
		secs := time.Since(st).Seconds()
		pps := float64(numpkts) / secs
		ips := int(float64(nirqs) / secs)
		spursps := float64(spurs) / secs
		fmt.Printf("pkt %6v (%.4v/s), dr %v %v, ws %v, "+
			"irqs %v (%v/s), spurs %v (%.3v/s)\n", numpkts, pps,
			dropints, drops, waits, nirqs, ips, spurs, spursps)
	}
}

func attach_ixgbe(vid, did int, t pci.Pcitag_t) {
	if unsafe.Sizeof(*rxdesc_t{}.hwdesc) != 16 ||
		unsafe.Sizeof(*txdesc_t{}.hwdesc) != 16 {
		panic("unexpected padding")
	}

	b, d, f := pci.Breakpcitag(t)
	fmt.Printf("X540: %x %x (%d:%d:%d)\n", vid, did, b, d, f)
	if uint(f) > 1 {
		panic("virtual functions not supported")
	}

	var x ixgbe_t
	x.init(t)

	// x540 doc 4.6.3 initialization sequence
	x._int_disable()
	x._reset()
	x._int_disable()

	// even though we disable flow control, we write 0 to FCTTV, FCRTL,
	// FCRTH, FCRTV, and  FCCFG. we program FCRTH.RTH later.
	regn := func(r ixgbereg_t, i int) ixgbereg_t {
		return r + ixgbereg_t(i*4)
	}

	fcttv := ixgbereg_t(0x3200)
	for i := 0; i < 4; i++ {
		x.rs(regn(fcttv, i), 0)
	}
	fcrtl := ixgbereg_t(0x3220)
	for i := 0; i < 8; i++ {
		x.rs(regn(fcrtl, i), 0)
		x.rs(FCRTH(i), 0)
	}

	fcrtv := ixgbereg_t(0x32a0)
	fccfg := ixgbereg_t(0x3d00)
	x.rs(fcrtv, 0)
	x.rs(fccfg, 0)

	mflcn := ixgbereg_t(0x4294)
	rfce := uint32(1 << 3)
	son := x.rl(mflcn)&rfce != 0
	if son {
		panic("receive flow control should be off?")
	}

	// enable no snooping
	nosnoop_dis := uint32(1 << 16)
	v := x.rl(CTRL_EXT)
	if v&nosnoop_dis != 0 {
		x.log("no snoop disabled. enabling.")
		x.rs(CTRL_EXT, v&^nosnoop_dis)
	}
	// useful for testing whether no snoop/relaxed memory ordering affects
	// buge behavior
	//ro_dis := uint32(1 << 17)
	//x.rs(CTRL_EXT, v | nosnoop_dis | ro_dis)

	x.hwlock()
	phyreset := uint16(1 << 6)
	for x._phy_read(ALARMS1)&phyreset == 0 {
	}
	//x.log("phy reset complete")
	x.hwunlock()

	// 4.6.3 says to wait for CFG_DONE, but this bit never seems to set.
	// openbsd does not check this bit.

	//x.log("manage wait")
	//cfg_done := uint32(1 << 18 + f)
	//for x.rl(EEMNGCTL) & cfg_done == 0 {
	//}
	////x.log("manageability complete")

	dmadone := uint32(1 << 3)
	for x.rl(RDRXCTL)&dmadone == 0 {
	}
	//x.log("dma engine initialized")

	// hardware reset is complete

	// RAL/RAH are big-endian
	v = x.rl(RAH(0))
	av := uint32(1 << 31)
	if v&av == 0 {
		panic("RA 0 invalid?")
	}
	mac := (uint64(v) & 0xffff) << 32
	mac |= uint64(x.rl(RAL(0)))
	for i := 0; i < 6; i++ {
		b := uint8(mac >> (8 * uint(i)))
		x.mac[i] = b
	}

	// enable MSI interrupts
	msiaddrl := 0x54
	msidata := 0x5c

	maddr := 0xfee << 20
	pci.Pci_write(x.tag, msiaddrl, maddr)
	vec := msi.Msi_alloc()
	mdata := int(vec) | apic.Bsp_apic_id<<12
	pci.Pci_write(x.tag, msidata, mdata)

	msictrl := 0x50
	pv := pci.Pci_read(x.tag, msictrl, 4)
	msienable := 1 << 16
	pv |= msienable
	pci.Pci_write(x.tag, msictrl, pv)

	msimask := 0x60
	if pci.Pci_read(x.tag, msimask, 4)&1 != 0 {
		panic("msi pci masked")
	}

	// make sure legacy PCI interrupts are disabled
	pciintdis := 1 << 10
	pv = pci.Pci_read(x.tag, 0x4, 2)
	pci.Pci_write(x.tag, 0x4, pv|pciintdis)

	// disable autoclear/automask
	x.rs(EIAC, 0)
	x.rs(EIAM, 0)
	x.rs(EIAM1, 0)
	x.rs(EIAM2, 0)

	// disable interrupt throttling
	for n := 0; n < 128; n++ {
		x.rs(EITR(n), 0)
	}

	// map all 128 rx queues to EICR bit 0 and tx queues to EICR bit 1
	for n := 0; n < 64; n++ {
		v := uint32(0x81808180)
		x.rs(IVAR(n), v)
	}

	// disable RSC; docs say RSC is enabled by default, but it isn't on my
	// card...
	for n := 0; n < 128; n++ {
		v := x.rl(RSCCTL(n))
		rscen := uint32(1 << 0)
		x.rs(RSCCTL(n), v&^rscen)
	}

	// receive enable here
	{
		for i := 0; i < 8; i++ {
			x.rs(PFVFSPOOF(i), 0)
		}
		for i := 0; i < 256; i++ {
			x.rs(MPSAR(i), 0)
		}
		for i := 0; i < 128; i++ {
			x.rs(PFUTA(i), 0)
			x.rs(VFTA(i), 0)
			x.rs(PFVLVFB(i), 0)
		}
		// enable ethernet broadcast packets via FCTRL.BAM in order to
		// receive ARP requests.
		v := x.rl(FCTRL)
		// XXX debugging features: store bad packets and unicast
		// promiscuous
		//sbp := uint32(1 << 1)
		//upe := uint32(1 << 9)
		bam := uint32(1 << 10)
		v |= bam
		x.rs(FCTRL, v)

		v = x.rl(RXCSUM)
		ippcse := uint32(1 << 12)
		v |= ippcse
		x.rs(RXCSUM, v)

		v = x.rl(RDRXCTL)
		// HLREG0.RXCRCSTRP must match RDRXCTL.CRCSTRIP
		crcstrip := uint32(1 << 1)
		// for the two following bits, docs say: "reserved, software
		// should set this bit to 1" but the bit is initialized to 0??
		res1 := uint32(1 << 25)
		res2 := uint32(1 << 26)
		v |= crcstrip | res1 | res2
		x.rs(RDRXCTL, v)

		// bit 0 of the first word of an rx descriptor should disable
		// no snoop for packet write back when set, but my NIC seems to
		// ignore this bit! thus i disable no snoop for all rx packet
		// writebacks
		rxdatawritensen := uint32(1 << 12)
		v = x.rl(DCA_RXCTRL(0))
		v &^= rxdatawritensen
		x.rs(DCA_RXCTRL(0), v)

		// if we want to, enable jumbo frames here

		// setup rx queue 0
		pg, p_pg := x.pg_new()
		// RDBAL/TDBAL must be 128 byte aligned
		x.rs(RDBAL(0), uint32(p_pg))
		x.rs(RDBAH(0), uint32(p_pg>>32))
		x.rs(RDLEN(0), uint32(mem.PGSIZE))

		// packet buffers must be at least SRRCTL.BSIZEPACKET bytes,
		// header buffers must be at least SRRCTL.BSIZEHEADER bytes
		rdescsz := uint32(16)
		x.rx.ndescs = uint32(mem.PGSIZE) / rdescsz
		x.rx.descs = make([]rxdesc_t, x.rx.ndescs)
		for i := 0; i < len(pg); i += 4 {
			_, p_bpg := x.pg_new()
			// SRRCTL.BSIZEPACKET
			dn := i / 2
			ps := mem.Pa_t(2 << 10)
			x.rx.descs[dn].init(p_bpg, 0, &pg[i])
			x.rx.descs[dn+1].init(p_bpg+ps, 0, &pg[i+2])
		}

		v = x.rl(SRRCTL(0))
		// XXX how large should a single rx descriptor's packet buffer
		// be? leave them the default for now (2K)...

		// program receive descriptor minimum threshold here

		// section 7.1.10.1's DESCTYPE values contradict the register
		// description in section 8! section 7.1.10.1 is correct.
		desctype := uint32(1 << 25)
		v |= desctype
		x.rs(SRRCTL(0), v)

		for i := range x.rx.descs {
			x.rx.descs[i].ready()
		}
		x.rs(RDH(0), 0)

		// enable this rx queue
		v = x.rl(RXDCTL(0))
		qen := uint32(1 << 25)
		v |= qen
		x.rs(RXDCTL(0), v)
		for x.rl(RXDCTL(0))&qen == 0 {
		}
		// must enable queue via RXDCTL.ENABLE before setting RDT
		tailc := x.rx.ndescs - 1
		x.rs(RDT(0), tailc)
		x.rx.tailc = tailc

		// enable receive
		v = x.rl(RXCTRL)
		rxen := uint32(1 << 0)
		v |= rxen
		x.rs(RXCTRL, v)
		//x.log("RX enabled!")
	}

	// transmit init
	{
		// map all per-tx-queue statistics to counter 0
		for n := 0; n < 32; n++ {
			x.rs(TQSM(n), 0)
		}

		txcrc := uint32(1 << 0)
		crcstrip := uint32(1 << 1)
		txpad := uint32(1 << 10)
		rxlerr := uint32(1 << 27)
		// HLREG0.RXCRCSTRP must match RDRXCTL.CRCSTRIP
		v := x.rl(HLREG0)
		v |= txcrc | crcstrip | txpad | rxlerr
		x.rs(HLREG0, v)

		nosnoop_tso := uint32(1 << 1)
		v = x.rl(DMATXCTL)
		v |= nosnoop_tso
		x.rs(DMATXCTL, v)

		// XXX may want to enable relaxed ordering or DCA for tx
		//for n := 0; n < 128; n++ {
		//	x.rs(DCA_TXCTRL(n), xxx)
		//}

		// if necessary, setup IPG (inter-packet gap) here
		// ...

		x._dbc_init()

		// setup tx queues
		ntxqs := 4
		x.txs = make([]ixgbetx_t, ntxqs)
		for i := range x.txs {
			ttx := &x.txs[i]
			ttx.qnum = i
			pg, p_pg := x.pg_new()
			// RDBAL/TDBAL must be 128 byte aligned
			x.rs(TDBAL(i), uint32(p_pg))
			x.rs(TDBAH(i), uint32(p_pg>>32))
			x.rs(TDLEN(i), uint32(mem.PGSIZE))

			tdescsz := uint32(16)
			ndescs := uint32(mem.PGSIZE) / tdescsz
			ttx.ndescs = ndescs
			ttx.descs = make([]txdesc_t, ttx.ndescs)
			for j := 0; j < len(pg); j += 4 {
				_, p_bpg := x.pg_new()
				dn := j / 2
				ps := uintptr(mem.PGSIZE) / 2
				ttx.descs[dn].init(p_bpg, ps, &pg[j])
				ttx.descs[dn+1].init(p_bpg+mem.Pa_t(ps), ps,
					&pg[j+2])
				// set DD and eop so all tx descriptors appear
				// ready for use
				ttx.descs[dn]._avail()
				ttx.descs[dn+1]._avail()
			}

			// disable transmit descriptor head write-back. we may want
			// this later.
			x.rs(TDWBAL(i), 0)

			// number of transmit descriptors per cacheline, as per
			// 7.2.3.4.1.
			tdcl := uint32(64 / tdescsz)
			// number of internal NIC descriptor buffers 7.2.1.2
			nicdescs := uint32(40)
			pthresh := nicdescs - tdcl
			hthresh := tdcl
			wthresh := uint32(0)
			if pthresh&^0x7f != 0 || hthresh&^0x7f != 0 ||
				wthresh&^0x7f != 0 {
				panic("bad pre-fetcher thresholds")
			}
			v = uint32(pthresh | hthresh<<8 | wthresh<<16)
			x.rs(TXDCTL(i), v)

			x.rs(TDT(ttx.qnum), 0)
			x.rs(TDH(ttx.qnum), 0)
			ttx.tailc = 0
		}

		v = x.rl(DMATXCTL)
		dmatxenable := uint32(1 << 0)
		v |= dmatxenable
		x.rs(DMATXCTL, v)

		for i := 0; i < ntxqs; i++ {
			v = x.rl(TXDCTL(i))
			txenable := uint32(1 << 25)
			v |= txenable
			x.rs(TXDCTL(i), v)

			for x.rl(TXDCTL(i))&txenable == 0 {
			}
		}
		//x.log("TX enabled!")
	}

	// configure interrupts with throttling
	x.rs(GPIE, 0)
	// interrupt throttling significantly affects TCP bulk transfer
	// performance. openbsd's period is 125us where linux uses anything
	// from 80us to 672us.  EITR[1-128] are reserved for MSI-X mode.
	cnt_wdis := uint32(1 << 31)
	// maxitr's unit is 2.048 us
	//maxitr := uint32(0x1ff << 3)
	// 125us period
	smallitr := uint32(0x3c << 3)
	x.rs(EITR(0), cnt_wdis|smallitr)

	// clear all previous interrupts
	x.rs(EICR, ^uint32(0))

	go x.int_handler(vec)
	// unmask tx/rx queue interrupts and link change
	lsc := uint32(1 << 20)
	x.rs(EIMS, lsc|3)

	ntx := uint32(0)
	for i := range x.txs {
		ntx += x.txs[i].ndescs
	}
	macs := Mac2str(x.mac[:])
	x.log("attached: MAC %s, rxq %v, txq %v, MSI %v, %vKB", macs,
		x.rx.ndescs, ntx, vec, x.pgs<<2)
}

var numpkts int
var dropints int
var waits int
var spurs int

func (x *ixgbe_t) rx_test() {
	prstat := func(v bool) {
		a := x.rl(SSVPC)
		b := x.rl(GPRC)
		c := x.rl(XEC)
		d := x.rl(FCCRC)
		e := x.rl(CRCERRS)
		f := x.rl(ILLERRC)
		g := x.rl(ERRBC)
		h := x.rl(PRC64)
		i := x.rl(PRC127)
		j := x.rl(QPRC(0))
		k := x.rl(QPRDC(0))
		if v {
			fmt.Println("  RX stats: ", a, b, c, d, e, f, g,
				h, i, j, k)
		}
	}
	prstat(false)

	i := 0
	for {
		i++
		//for i := 0; i < 3; i++ {
		<-time.After(time.Second)
		tail := x.rl(RDT(0))
		rdesc := &x.rx.descs[tail]
		rdesc.ready()
		tail++
		if tail == x.rx.ndescs {
			tail = 0
		}

		x.rs(RDT(0), tail)
		for !rdesc.rxdone() {
		}
		eop := uint64(1 << 1)
		if rdesc.hwdesc.p_hdr&eop == 0 {
			panic("EOP not set")
		}
		if x.rl(RDH(0)) != tail {
			panic("wtf")
		}

		hl := rdesc.hdrlen()
		pl := rdesc.pktlen()
		if pl > 1<<11 {
			panic("expected packet len")
		}
		fmt.Printf("packet %v: plen: %v, hdrlen: %v, hdr: ", i, pl, hl)
		b := mem.Dmaplen(mem.Pa_t(rdesc.p_pbuf), int(rdesc.pktlen()))
		for _, c := range b[:hl] {
			fmt.Printf("%0.2x ", c)
		}
		fmt.Printf("\n")
	}
}

func (x *ixgbe_t) tx_test() {
	x.txs[0].Lock()
	defer x.txs[0].Unlock()

	fmt.Printf("test tx start\n")
	pkt := &Tcppkt_t{}
	pkt.Tcphdr.Init_ack(8080, 8081, 31337, 31338)
	pkt.Tcphdr.Win = 1 << 14
	l4len := 0
	pkt.Iphdr.Init_tcp(l4len, 0x01010101, 0x02020202)
	bmac := []uint8{0x00, 0x13, 0x72, 0xb6, 0x7b, 0x42}
	pkt.Ether.Init_ip4(x.mac[:], bmac)
	pkt.Crc(l4len, 0x01010101, 0x121a0530)

	eth, ip, thdr := pkt.Hdrbytes()
	durdata := new([3 * 1460]uint8)
	sgbuf := [][]uint8{eth, ip, thdr, durdata[:]}
	tlen := 0
	for _, s := range sgbuf {
		tlen += len(s)
	}

	h := x.rl(TDH(0))
	t := x.rl(TDT(0))
	if h != t {
		panic("no")
	}
	fd := &x.txs[0].descs[t]
	fd.wbwait()
	fd.ctxt_tcp_tso(ETHERLEN, IP4LEN, len(thdr), 1460)
	t = (t + 1) % x.txs[0].ndescs
	fd = &x.txs[0].descs[t]
	fd.wbwait()
	sgbuf = fd.mktcp_tso(sgbuf, len(thdr), tlen)
	t = (t + 1) % x.txs[0].ndescs
	for len(sgbuf) != 0 {
		fd = &x.txs[0].descs[t]
		fd.wbwait()
		sgbuf = fd.data_continue(sgbuf)
		t = (t + 1) % x.txs[0].ndescs
	}
	x.rs(TDT(0), t)

	for ot := h; ot != t; ot = (ot + 1) % x.txs[0].ndescs {
		fd := &x.txs[0].descs[ot]
		if fd.eop {
			fmt.Printf("wait for %v...\n", ot)
			fd.wbwait()
			fmt.Printf("%v is ready\n", ot)
		} else {
			fmt.Printf("skip %v\n", ot)
		}
	}
	if x.rl(TDT(0)) != x.rl(TDH(0)) {
		panic("htf?")
	}
	fmt.Printf("test tx done\n")
}

func (x *ixgbe_t) _dbc_init() {
	// dbc=off, vt=off (section 4.6.11.3.4)
	rxpbsize := uint32(0x180 << 10)
	x.rs(RXPBSIZE(0), rxpbsize)
	for n := 1; n < 8; n++ {
		x.rs(RXPBSIZE(n), 0)
	}
	// 4.6.3.2 "FCRTH.RTH must be set even if flow control is disabled"
	x.rs(FCRTH(0), x.rl(RXPBSIZE(0))-0x6000)

	txpbsize := uint32(0xa0 << 10)
	x.rs(TXPBSIZE(0), txpbsize)
	for n := 1; n < 8; n++ {
		x.rs(TXPBSIZE(n), 0)
	}
	txpbthresh := uint32(0xa0)
	x.rs(TXPBTHRESH(0), txpbthresh)
	for n := 1; n < 8; n++ {
		x.rs(TXPBTHRESH(n), 0)
	}

	v := x.rl(MRQC)
	mrqe := uint32(0xf)
	v &^= mrqe
	x.rs(MRQC, v)

	// RTTDCS.ARBDIS must be 0 before programming MTQC
	arbdis := uint32(1 << 6)
	v = x.rl(RTTDCS)
	v |= arbdis
	x.rs(RTTDCS, v)

	v = x.rl(MTQC)
	vtdbc := uint32(0xf)
	v &^= vtdbc
	x.rs(MTQC, v)

	v = x.rl(RTTDCS)
	v &^= arbdis
	x.rs(RTTDCS, v)

	vten := uint32(1 << 0)
	v = x.rl(PFVTCTL)
	v &^= vten
	x.rs(PFVTCTL, v)

	v = x.rl(PFQDE)
	dropen := uint32(1 << 0)
	v &^= dropen
	x.rs(PFQDE, v)

	x.rs(RTRUP2TC, 0)
	x.rs(RTTUP2TC, 0)

	x.rs(DTXMXSZRQ, 0xfff)

	v = x.rl(SECTXMINIFG)
	// docs contradict: says use both 0x10 and 0x1f when in non DBC
	// mode?
	mrkrinstert := uint32(0x10 << 8)
	mrkmask := uint32(((1 << 11) - 1) << 8)
	v &^= mrkmask
	v |= mrkrinstert
	x.rs(SECTXMINIFG, v)
	v = x.rl(MFLCN)
	rpfcm := uint32(1 << 2)
	rpfcemask := uint32(0xff << 4)
	v &^= rpfcm | rpfcemask
	// XXX do we really need to enable flow control as 4.6.11.3.4
	// claims?
	//rfce := uint32(1 << 3)
	//v |= rfce
	x.rs(MFLCN, v)
	//x.rs(FCCFG,

	for n := 0; n < 128; n++ {
		x.rs(RTTDQSEL, uint32(n))
		x.rs(RTTDT1C, 0)
	}
	for n := 0; n < 8; n++ {
		x.rs(RTTDT2C(n), 0)
	}
	for n := 0; n < 8; n++ {
		x.rs(RTTPT2C(n), 0)
	}
	for n := 0; n < 8; n++ {
		x.rs(RTRPT4C(n), 0)
	}

	v = x.rl(RTTDCS)
	tdpac := uint32(1 << 0)
	vmpac := uint32(1 << 1)
	tdrm := uint32(1 << 4)
	bdpm := uint32(1 << 22)
	bpbfsm := uint32(1 << 23)
	v &^= tdpac | vmpac | tdrm
	v |= bdpm | bpbfsm
	x.rs(RTTDCS, v)

	v = x.rl(RTTPCS)
	tppac := uint32(1 << 5)
	tprm := uint32(1 << 8)
	arbd := uint32(0x224 << 22)
	arbmask := uint32(((1 << 10) - 1) << 22)
	v &^= tppac | tprm | arbmask
	v |= arbd
	x.rs(RTTPCS, v)

	v = x.rl(RTRPCS)
	rrm := uint32(1 << 1)
	rac := uint32(1 << 2)
	v &^= rrm | rac
	x.rs(RTRPCS, v)
}

func Ixgbe_init() {
	pci.Pci_register_intel(pci.PCI_DEV_X540T, attach_ixgbe)
}
