package pci

import "fmt"
import "runtime"

import "defs"
import "mem"

var IRQ_DISK int = -1
var INT_DISK int = -1

// our actual disk
var Disk Disk_i

const (
	VENDOR   int = 0x0
	DEVICE       = 0x02
	STATUS       = 0x06
	CLASS        = 0x0b
	SUBCLASS     = 0x0a
	HEADER       = 0x0e
	_BAR0        = 0x10
	_BAR1        = 0x14
	_BAR2        = 0x18
	_BAR3        = 0x1c
	_BAR4        = 0x20
	BAR5         = 0x24
)

// width is width of the register in bytes
func Pci_read(tag Pcitag_t, reg, width int) int {
	if width <= 0 || (reg / 4 != (reg + width - 1) / 4) {
		panic("read spans more than one reg")
	}
	enable := 1 << 31
	rsh := reg % 4
	r := reg - rsh
	t := enable | int(tag) | r

	pci_addr := 0xcf8
	pci_data := 0xcfc
	runtime.Outl(pci_addr, t)
	d := runtime.Inl(pci_data)
	runtime.Outl(pci_addr, 0)

	ret := int(uint(d) >> uint(rsh*8))
	m := ((1 << (8 * uint(width))) - 1)
	return ret & m
}

func Pci_write(tag Pcitag_t, reg, val int) {
	if reg&3 != 0 {
		panic("reg must be 32bit aligned")
	}
	enable := 1 << 31
	t := enable | int(tag) | reg

	pci_addr := 0xcf8
	pci_data := 0xcfc
	runtime.Outl(pci_addr, t)
	runtime.Outl(pci_data, val)
	runtime.Outl(pci_addr, 0)
}

// don't forget to enable busmaster in pci command reg before attaching
func pci_bar_pio(tag Pcitag_t, barn int) uintptr {
	if barn < 0 || barn > 4 {
		panic("bad bar #")
	}
	ret := Pci_read(tag, _BAR0+4*barn, 4)
	ispio := 1
	if ret&ispio == 0 {
		panic("is memory bar")
	}
	return uintptr(ret &^ 0x3)
}

// some memory bars include size in the low bits; this method doesn't mask such
// bits out.
func Pci_bar_mem(tag Pcitag_t, barn int) (uintptr, int) {
	if barn < 0 || barn > 4 {
		panic("bad bar #")
	}
	bari := _BAR0 + 4*barn
	ret := Pci_read(tag, bari, 4)
	ispio := 1
	if ret&ispio != 0 {
		panic("is port io bar")
	}
	Pci_write(tag, bari, -1)
	blen := uint32(Pci_read(tag, bari, 4))
	blen &^= 0xf
	blen = ^blen + 1
	if blen == 0 {
		panic("bad bar length")
	}
	Pci_write(tag, bari, ret)
	mtype := (uint32(ret) >> 1) & 0x3
	if mtype == 1 {
		// 32bit memory bar
		return uintptr(ret &^ 0xf), int(blen)
	}
	if mtype != 2 {
		panic("weird memory bar type")
	}
	if barn > 4 {
		panic("64bit memory bar requires 2 bars")
	}
	ret2 := Pci_read(tag, bari+4, 4)
	return uintptr((ret2 << 32) | ret&^0xf), int(blen)
}

func Pci_dump() {
	pcipr := func(b, dev, f int, ind bool) (int, bool) {
		t := mkpcitag(b, dev, f)
		v := Pci_read(t, VENDOR, 2)
		if v == 0xffff {
			return 0, false
		}
		d := Pci_read(t, DEVICE, 2)
		mf := Pci_read(t, HEADER, 1)
		cl := Pci_read(t, CLASS, 1)
		scl := Pci_read(t, SUBCLASS, 1)
		if ind {
			fmt.Printf("    ")
		}
		fmt.Printf("%d: %d: %d: %#x %#x (%#x %#x)\n", b, dev, f, v, d,
			cl, scl)
		return mf, true
	}
	fmt.Printf("PCI dump:\n")
	for b := 0; b < 256; b++ {
		for dev := 0; dev < 32; dev++ {
			mf, ok := pcipr(b, dev, 0, false)
			if !ok {
				continue
			}
			if mf&0x80 != 0 {
				for f := 1; f < 8; f++ {
					pcipr(b, dev, f, true)
				}
			}
		}
	}
}

func Pcibus_attach() {
	pciinfo := func(b, dev, f int) (int, int, bool, bool) {
		t := mkpcitag(b, dev, f)
		v := Pci_read(t, VENDOR, 2)
		if v == 0xffff {
			return 0, 0, false, false
		}
		d := Pci_read(t, DEVICE, 2)
		mf := Pci_read(t, HEADER, 1)
		ismf := mf&0x80 != 0
		return v, d, ismf, true
	}
	devattach := func(b, dev int) {
		vid, did, mf, ok := pciinfo(b, dev, 0)
		if !ok {
			return
		}
		pci_attach(vid, did, b, dev, 0)
		if !mf {
			return
		}
		// attach multi functions too
		for f := 1; f < 8; f++ {
			vid, did, _, ok := pciinfo(b, dev, f)
			if !ok {
				continue
			}
			pci_attach(vid, did, b, dev, f)
		}
	}
	for b := 0; b < 256; b++ {
		for dev := 0; dev < 32; dev++ {
			devattach(b, dev)
		}
	}
}

const (
	PCI_VEND_INTEL = 0x8086
	// PCI_DEV_PIIX3 = 0x7000
	// PCI_DEV_3400  = 0x3b20
	PCI_DEV_X540T     = 0x1528
	PCI_DEV_AHCI_QEMU = 0x2922
	PCI_DEV_AHCI_BHW  = 0x3b22
	PCI_DEV_AHCI_BHWX = 0xa102
	//PCI_DEV_AHCI_BHW2 = 0x8d02 // (0:31:2)
	PCI_DEV_AHCI_BHW2 = 0x8d62 // (0:17:4)
)

// map from vendor ids to a map of device ids to attach functions
var alldevs = map[int]map[int]func(int, int, Pcitag_t){
}

func Pci_register(vendor, dev int, attach func(int, int, Pcitag_t)) {
	if _, ok := alldevs[vendor]; !ok {
		alldevs[vendor] = make(map[int]func(int, int, Pcitag_t))
	}
	alldevs[vendor][dev] = attach
}

func Pci_register_intel(dev int, attach func(int, int, Pcitag_t)) {
	Pci_register(PCI_VEND_INTEL, dev, attach)
}

func pci_attach(vendorid, devid, bus, dev, fu int) {
	tag := mkpcitag(bus, dev, fu)
	devs, ok := alldevs[vendorid]
	if !ok {
		return
	}
	attach, ok := devs[devid]
	if !ok {
		return
	}
	attach(vendorid, devid, tag)
}

type Pcitag_t uint

func mkpcitag(b, d, f int) Pcitag_t {
	return Pcitag_t(b<<16 | d<<11 | f<<8)
}

func Breakpcitag(b Pcitag_t) (int, int, int) {
	bus := int((b >> 16) & 0xff)
	d := int((b >> 11) & 0x1f)
	f := int((b >> 8) & 0x7)
	return bus, d, f
}

func attach_piix3(vendorid, devid int, tag Pcitag_t) {
	if Disk != nil {
		panic("adding two disks")
	}
	IRQ_DISK = 14
	INT_DISK = defs.IRQ_BASE + IRQ_DISK

	d := &legacy_disk_t{}
	d.init(0x1f0, 0x3f6)
	Disk = d
	fmt.Printf("legacy disk attached\n")
}

// this code determines the mapping of a PCI interrupt pin to an IRQ number for
// a specific type of Intel southbridge (the one in bhw).
func pci_disk_interrupt_wiring(t Pcitag_t) int {
	intline := 0x3d
	pin := Pci_read(t, intline, 1)
	if pin < 1 || pin > 4 {
		panic("bad PCI pin")
	}

	fmt.Printf("pin %v\n", pin)

	// map PCI pin to IOAPIC pin number. the Intel PCH chipset exposes this
	// mapping through PCI registers and memory mapped IO so we don't need
	// to parse AML (thank you flying spaghetti monster!)
	taglpc := mkpcitag(0, 31, 0)
	rcba_p := Pci_read(taglpc, 0xf0, 4)
	if rcba_p&1 == 0 {
		panic("no root complex base")
	}
	rcba_p &^= ((1 << 14) - 1)
	// memory reads/writes to RCBA must be 32bit aligned
	rcba := mem.Dmaplen32(uintptr(rcba_p), 0x342c)
	// PCI dev 31 PIRQ routes
	routes := rcba[0x3140/4]
	pirq := (routes >> (4 * (uint32(pin) - 1))) & 0x7
	// Intel PCH's IOAPIC has PIRQs on input pins 16-24
	gsi := int(16 + pirq)

	// make sure chipset isn't steering this PCI interrupt to the 8259
	// (which we have disabled)
	proutereg := 0x60
	if pirq >= 4 {
		proutereg = 0x68
	}
	v := Pci_read(taglpc, proutereg, 4)
	disable := 0x80
	v |= disable << ((pirq % 4) * 8)
	Pci_write(taglpc, proutereg, v)
	return gsi
}
