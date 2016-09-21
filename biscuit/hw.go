package main

import "fmt"
import "runtime"
import "sync/atomic"
import "time"
import "unsafe"

const (
	VENDOR	int	= 0x0
	DEVICE		= 0x02
	STATUS		= 0x06
	CLASS		= 0x0b
	SUBCLASS	= 0x0a
	HEADER		= 0x0e
	_BAR0		= 0x10
	_BAR1		= 0x14
	_BAR2		= 0x18
	_BAR3		= 0x1c
	_BAR4		= 0x20
)

// width is width of the register in bytes
func pci_read(tag pcitag_t, reg, width int) int {
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
	m := ((1 << (8*uint(width))) - 1)
	return ret & m
}

func pci_write(tag pcitag_t, reg, val int) {
	if reg & 3 != 0 {
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
func pci_bar_pio(tag pcitag_t, barn int) uintptr {
	if barn < 0 || barn > 4 {
		panic("bad bar #")
	}
	ret := pci_read(tag, _BAR0 + 4*barn, 4)
	ispio := 1
	if ret & ispio == 0 {
		panic("is memory bar")
	}
	return uintptr(ret &^ 0x3)
}

// some memory bars include size in the low bits; this method doesn't mask such
// bits out.
func pci_bar_mem(tag pcitag_t, barn int) (uintptr, int) {
	if barn < 0 || barn > 4 {
		panic("bad bar #")
	}
	bari := _BAR0 + 4*barn
	ret := pci_read(tag, bari, 4)
	ispio := 1
	if ret & ispio != 0 {
		panic("is port io bar")
	}
	pci_write(tag, bari, -1)
	blen := uint32(pci_read(tag, bari, 4))
	blen &^= 0xf
	blen = ^blen + 1
	if blen == 0 {
		panic("bad bar length")
	}
	pci_write(tag, bari, ret)
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
	ret2 := pci_read(tag, bari + 4, 4)
	return uintptr((ret2 << 32) | ret &^ 0xf), int(blen)
}

func pci_dump() {
	pcipr := func(b, dev, f int, ind bool) (int, bool) {
		t := mkpcitag(b, dev, f)
		v  := pci_read(t, VENDOR, 2)
		if v == 0xffff {
			return 0, false
		}
		d   := pci_read(t, DEVICE, 2)
		mf  := pci_read(t, HEADER, 1)
		cl  := pci_read(t, CLASS, 1)
		scl := pci_read(t, SUBCLASS, 1)
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
			if mf & 0x80 != 0 {
				for f := 1; f < 8; f++ {
					pcipr(b, dev, f, true)
				}
			}
		}
	}
}

func pcibus_attach() {
	pciinfo := func(b, dev, f int) (int, int, bool, bool) {
		t := mkpcitag(b, dev, f)
		v  := pci_read(t, VENDOR, 2)
		if v == 0xffff {
			return 0, 0, false, false
		}
		d  := pci_read(t, DEVICE, 2)
		mf := pci_read(t, HEADER, 1)
		ismf := mf & 0x80 != 0
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

type pcitag_t uint

func mkpcitag(b, d, f int) pcitag_t {
	return pcitag_t(b << 16 | d << 11 | f << 8)
}

func breakpcitag(b pcitag_t) (int, int, int) {
	bus := int((b >> 16) & 0xff)
	d := int((b >> 11) & 0x1f)
	f := int((b >> 8) & 0x7)
	return bus, d, f
}

func pci_attach(vendorid, devid, bus, dev, fu int) {
	PCI_VEND_INTEL := 0x8086
	PCI_DEV_PIIX3 := 0x7000
	PCI_DEV_3400  := 0x3b20
	PCI_DEV_X540T := 0x1528

	// map from vendor ids to a map of device ids to attach functions
	alldevs := map[int]map[int]func(int, int, pcitag_t) {
		PCI_VEND_INTEL : {
			PCI_DEV_PIIX3 : attach_piix3,
			PCI_DEV_3400 : attach_3400,
			PCI_DEV_X540T: attach_x540t,
			},
		}

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

func attach_piix3(vendorid, devid int, tag pcitag_t) {
	if disk != nil {
		panic("adding two disks")
	}
	IRQ_DISK = 14
	INT_DISK = IRQ_BASE + IRQ_DISK

	d := &legacy_disk_t{}
	d.init(0x1f0, 0x3f6)
	disk = d
	fmt.Printf("legacy disk attached\n")
}

func attach_3400(vendorid, devid int, tag pcitag_t) {
	if disk != nil {
		panic("adding two disks")
	}

	intline := 0x3d
	pin := pci_read(tag, intline, 1)
	if pin < 1 || pin > 4 {
		panic("bad PCI pin")
	}

	// map PCI pin to IOAPIC pin number. the Intel PCH chipset exposes this
	// mapping through PCI registers and memory mapped IO so we don't need
	// to parse AML (thank you flying spaghetti monster!)
	taglpc := mkpcitag(0, 31, 0)
	rcba_p := pci_read(taglpc, 0xf0, 4)
	if rcba_p & 1 == 0 {
		panic("no root complex base")
	}
	rcba_p &^= ((1 << 14) - 1)
	// memory reads/writes to RCBA must be 32bit aligned
	rcba := dmaplen32(uintptr(rcba_p), 0x342c)
	// PCI dev 31 PIRQ routes
	routes := rcba[0x3140/4]
	pirq := (routes >> (4*(uint32(pin) - 1))) & 0x7
	// Intel PCH's IOAPIC has PIRQs on input pins 16-24
	gsi := int(16 + pirq)

	// make sure chipset isn't steering this PCI interrupt to the 8259
	// (which we have disabled)
	proutereg := 0x60
	if pirq >= 4 {
		proutereg = 0x68
	}
	v := pci_read(taglpc, proutereg, 4)
	disable := 0x80
	v |= disable << ((pirq % 4)*8)
	pci_write(taglpc, proutereg, v)

	IRQ_DISK = gsi
	INT_DISK = IRQ_BASE + IRQ_DISK

	d := &pciide_disk_t{}
	// 3400's PCI-native IDE command/control block
	rbase := pci_bar_pio(tag, 0)
	allstats := pci_bar_pio(tag, 1)
	busmaster := pci_bar_pio(tag, 4)

	d.init(rbase, allstats, busmaster)
	disk = d
	fmt.Printf("3400: base %#x, cntrl: %#x, bm: %#x, irq: %d\n", rbase,
	    allstats, busmaster, gsi)
}

type disk_t interface {
	start(*idebuf_t, bool)
	complete([]uint8, bool)
	intr() bool
	int_clear()
}

// use ata pio for fair comparisons against xv6, but i want to use ahci (or
// something) eventually. unlike xv6, we always use disk 0
const(
	ide_bsy = 0x80
	ide_drdy = 0x40
	ide_df = 0x20
	ide_err = 0x01

	ide_cmd_read = 0x20
	ide_cmd_write = 0x30
)

func ide_init(rbase uintptr) bool {
	ide_wait(rbase, false)

	found := false
	for i := 0; i < 1000; i++ {
		r := int(runtime.Inb(uint16(rbase + 7)))
		if r == 0xff {
			fmt.Printf("floating bus!\n")
			break
		} else if r != 0 {
			found = true
			break
		}
	}
	if found {
		fmt.Printf("IDE disk detected\n");
		return true
	}

	fmt.Printf("no IDE disk\n");
	return false
}

func ide_wait(base uintptr, chk bool) bool {
	var r int
	c := 0
	for {
		r = int(runtime.Inb(uint16(base + 7)))
		if r & (ide_bsy | ide_drdy) == ide_drdy {
			break
		}
		c++
		if c > 10000000 {
			fmt.Printf("waiting a very long time for disk...\n")
			c = 0
		}
	}
	if chk && r & (ide_df | ide_err) != 0 {
		return false
	}
	return true
}

func idedata_ready(base uintptr) {
	c := 0
	for {
		drq := 1 << 3
		st := int(runtime.Inb(uint16(base + 7)))
		if st & drq != 0 {
			return
		}
		c++
		if c > 10000000 {
			fmt.Printf("waiting a long time for DRQ...\n")
		}
	}
}

func ide_start(rbase, allstatus uintptr, ibuf *idebuf_t, writing bool) {
	ireg := func(n uintptr) uint16 {
		return uint16(rbase + n)
	}
	ide_wait(rbase, false)
	outb := runtime.Outb
	outb(uint16(allstatus), 0)
	outb(ireg(2), 1)
	bn := ibuf.block
	bd := ibuf.disk
	outb(ireg(3), uint8(bn & 0xff))
	outb(ireg(4), uint8((bn >> 8) & 0xff))
	outb(ireg(5), uint8((bn >> 16) & 0xff))
	outb(ireg(6), uint8(0xe0 | ((bd & 1) << 4) | (bn >> 24) & 0xf))
	if writing {
		outb(ireg(7), ide_cmd_write)
		idedata_ready(rbase)
		runtime.Outsl(int(ireg(0)), unsafe.Pointer(&ibuf.data[0]),
		    512/4)
	} else {
		outb(ireg(7), ide_cmd_read)
	}
}

func ide_complete(base uintptr, dst []uint8, writing bool) {
	if !writing {
		// read sector
		if ide_wait(base, true) {
			runtime.Insl(uint16(base + 0),
			    unsafe.Pointer(&dst[0]), 512/4)
		}
	} else {
		// cache flush; only needed for old disks?
		//runtime.Outb(base + 7, 0xe7)
	}
}

type legacy_disk_t struct {
	rbase	uintptr
	allstat	uintptr
}

func (d *legacy_disk_t) init(base, allst uintptr) {
	d.rbase = base
	d.allstat = allst
	ide_init(d.rbase)
}

func (d *legacy_disk_t) start(ibuf *idebuf_t, writing bool) {
	ide_start(d.rbase, d.allstat, ibuf, writing)
}

func (d *legacy_disk_t) complete(dst []uint8, writing bool) {
	ide_complete(d.rbase, dst, writing)
}

func (d *legacy_disk_t) intr() bool {
	return true
}

func (d *legacy_disk_t) int_clear() {
	// read status so disk clears int
	runtime.Inb(uint16(d.rbase + 7))
	runtime.Inb(uint16(d.rbase + 7))
	irq_eoi(IRQ_DISK)
}

type pciide_disk_t struct {
	rbase	uintptr
	allstat	uintptr
	bmaster uintptr
}

func (d *pciide_disk_t) init(base, allst, busmaster uintptr) {
	d.rbase = base
	d.allstat = allst
	d.bmaster = busmaster
	ide_init(d.rbase)
}

func (d *pciide_disk_t) start(ibuf *idebuf_t, writing bool) {
	ide_start(d.rbase, d.allstat, ibuf, writing)
}

func (d *pciide_disk_t) complete(dst []uint8, writing bool) {
	ide_complete(d.rbase, dst, writing)
}

func (d *pciide_disk_t) intr() bool {
	streg := uint16(d.bmaster + 0x02)
	bmintr := uint(1 << 2)
	st := runtime.Inb(streg)
	if st & bmintr == 0 {
		return false
	}
	return true
}

func (d *pciide_disk_t) int_clear() {
	// read status so disk clears int
	runtime.Inb(uint16(d.rbase + 7))
	runtime.Inb(uint16(d.rbase + 7))

	// in PCI-native mode, clear the interrupt via the legacy bus master
	// base, bar 4.
	streg := uint16(d.bmaster + 0x02)
	st := runtime.Inb(streg)
	er := uint(1 << 1)
	if st & er != 0 {
		panic("disk error")
	}
	runtime.Outb(streg, uint8(st))

	// and via apic
	irq_eoi(IRQ_DISK)
}

type _oride_t struct {
	src	int
	dst	int
	// trigger sense
	level	bool
	// polarity
	low	bool
}

type acpi_ioapic_t struct {
	base		uintptr
	overrides	map[int]_oride_t
}

func _acpi_cksum(tbl []uint8) {
	var cksum uint8
	for _, c := range tbl {
		cksum += c
	}
	if cksum != 0 {
		panic("bad ACPI table checksum")
	}
}

// returns a slice of the requested table and whether it was found
func _acpi_tbl(rsdt []uint8, sig string) ([]uint8, bool) {
	// RSDT contains 32bit pointers, XSDT contains 64bit pointers.
	hdrlen := 36
	ptrs := rsdt[hdrlen:]
	var tbl []uint8
	for len(ptrs) != 0 {
		tbln := readn(ptrs, 4, 0)
		ptrs = ptrs[4:]
		tbl = dmaplen(tbln, 8)
		if string(tbl[:4]) == sig {
			l := readn(tbl, 4, 4)
			tbl = dmaplen(tbln, l)
			return tbl, true
		}
	}
	return nil, false
}

// returns number of cpus, IO physcal address of IO APIC, and whether both the
// number of CPUs and an IO APIC were found.
func _acpi_madt(rsdt []uint8) (int, acpi_ioapic_t, bool) {
	// find MADT table
	tbl, found := _acpi_tbl(rsdt, "APIC")
	var apicret acpi_ioapic_t
	if !found {
		return 0, apicret, false
	}
	_acpi_cksum(tbl)
	apicret.overrides = make(map[int]_oride_t)
	marrayoff := 44
	ncpu := 0
	elen := 1
	// m is array of "interrupt controller structures" in MADT
	for m := tbl[marrayoff:]; len(m) != 0; m = m[m[elen]:] {
		// ACPI 5.2.12.2: each processor is required to have a LAPIC
		// entry
		tlapic    := uint8(0)
		tioapic   := uint8(1)
		toverride := uint8(2)

		tiosapic := uint8(6)
		tlsapic := uint8(7)
		tpint := uint8(8)
		if m[0] == tlapic {
			flags := readn(m, 4, 4)
			enabled := 1
			if flags & enabled != 0 {
				ncpu++
			}
		} else if m[0] == tioapic {
			apicret.base = uintptr(readn(m, 4, 4))
			//fmt.Printf("IO APIC addr: %x\n", apicret.base)
			//fmt.Printf("IO APIC IRQ start: %v\n", readn(m, 4, 8))
		} else if m[0] == toverride {
			src := readn(m, 1, 3)
			dst := readn(m, 4, 4)
			v := readn(m, 2, 8)
			var nover _oride_t
			nover.src = src
			nover.dst = dst
			//var active string
			switch (v & 0x3) {
			case 0:
				//active = "conforms"
				if dst < 16 {
					nover.low = true
				} else {
					nover.low = false
				}
			case 1:
				//active = "high"
				nover.low = false
			case 2:
				//active = "RESERVED?"
				panic("bad polarity")
			case 3:
				//active = "low"
				nover.low = true
			}
			//var trig string
			switch ((v & 0xc) >> 2) {
			case 0:
				//trig = "conforms"
				if dst < 16 {
					nover.level = false
				} else {
					nover.level = true
				}
			case 1:
				//trig = "edge"
				nover.level = false
			case 2:
				//trig = "RESERVED?"
				panic("bad trigger mode")
			case 3:
				//trig = "level"
				nover.level = true
			}
			apicret.overrides[dst] = nover
			//fmt.Printf("IRQ OVERRIDE: %v -> %v (%v, %v)\n", src,
			//    dst, trig, active)
		} else if m[0] == tiosapic {
			//fmt.Printf("*** IO SAPIC\n")
		} else if m[0] == tlsapic {
			//fmt.Printf("*** LOCAL SAPIC\n")
		} else if m[0] == tpint {
			//fmt.Printf("*** PLATFORM INT\n")
		}
	}
	return ncpu, apicret, ncpu != 0 && apicret.base != 0
}

// returns false if ACPI claims that MSI is broken
func _acpi_fadt(rsdt []uint8) bool {
	tbl, found := _acpi_tbl(rsdt, "FACP")
	if !found {
		return false
	}
	_acpi_cksum(tbl)
	flags := readn(tbl, 2, 109)
	nomsi      := 1 << 3
	return flags & nomsi == 0
}

func _acpi_scan() ([]uint8, bool) {
	// ACPI 5.2.5: search for RSDP in EBDA and BIOS read-only memory
	ebdap := (0x40 << 4) | 0xe
	p := dmap8(ebdap)
	ebda := readn(p, 2, 0)
	ebda <<= 4

	isrsdp := func(d []uint8) bool {
		s := string(d[:8])
		if s != "RSD PTR " {
			return false
		}
		var cksum uint8
		for i := 0; i < 20; i++ {
			cksum += d[i]
		}
		if cksum != 0 {
			return false
		}
		return true
	}
	rsdplen := 36
	for i := 0; i < 1 << 10; i += 16 {
		p = dmaplen(ebda + i, rsdplen)
		if isrsdp(p) {
			return p, true
		}
	}
	for bmem := 0xe0000; bmem < 0xfffff; bmem += 16 {
		p = dmaplen(bmem, rsdplen)
		if isrsdp(p) {
			return p, true
		}
	}
	return nil, false
}

func acpi_attach() int {
	rsdp, ok := _acpi_scan()
	if !ok {
		panic("no RSDP")
	}
	rsdtn := readn(rsdp, 4, 16)
	//xsdtn := readn(rsdp, 8, 24)
	rsdt := dmaplen(rsdtn, 8)
	if rsdtn == 0 || string(rsdt[:4]) != "RSDT" {
		panic("no RSDT")
	}
	rsdtlen := readn(rsdt, 4, 4)
	rsdt = dmaplen(rsdtn, rsdtlen)
	// verify RSDT checksum
	_acpi_cksum(rsdt)
	// may want to search XSDT, too
	ncpu, ioapic, ok := _acpi_madt(rsdt)
	if !ok {
		panic("no cpu count")
	}

	apic.apic_init(ioapic)

	msi := _acpi_fadt(rsdt)
	if !msi {
		panic("no MSI")
	}

	return ncpu
}

/* x86 interrupts -- a truly horrifying nightmare
 *
 * braindumping here to remind myself later.
 *
 * TERMINOLOGY:
 *
 * Polarity: the electical state of the wire which the device causes when said
 *     device wants an interrupt. active-high or active-low.
 * Trigger mode/sense: how long a device will put the wire to the interrupt
 *     polarity. edge-triggered or level-triggered. an interrupt controller
 *     must be told how the devices use each interrupt line i.e. the polarity
 *     and trigger mode of each line.
 * PIC/8259: legacy interrupt controllers from the original PC-AT from 1984. an
 *     8259 only has 8 input pins. thus, in order to handle all 16 of the
 *     legacy interrupts, two 8259s were setup "cascaded" (the output of the
 *     "slave" is connected to IRQ 2 of the "master"). thus both 8259s are
 *     programmed independently.
 * IRQ: interrupt line connected to an 8259 (not IOAPIC). on some modern
 *     southbridges, IRQs are actually implemented by a single wire using
 *     "serial IRQs". IRQs are edge-triggered, active-high. IRQs cannot be
 *     shared with PCI interrupts because PCI interrupts are level-triggered
 *     and active-low. however, some southbridges allow PCI interrupts to be
 *     "steered" (converted) to IRQs and take care of converting the
 *     polarity/trigger-mode of the wire.
 * PIRQ: PCI interrupt line. level-triggered and active-low.
 * PCI interrupt pins: each PCI device has four pins: INT[A-D]#. each pin can
 *     be connected to a different IOAPIC input pin or PCI link device. thus
 *     the OS must determine the mapping of the PCI interrupt pin used by a
 *     device to the IOAPIC input pin. this is the main challenge of x86
 *     interrupts.
 * PCI link device: programmable interrupt router. it routes PCI interrupt pins
 *     to different IOAPIC or 8259 input pins.
 * PIC mode: one of the two legacy modes stipulated by MP specification. in
 *     this mode, the 8259s are connected directly to the BSP, bypassing the
 *     BSPs LAPIC.
 * Virtual Wire mode: the other of the two legacy modes stipulated by the MP
 *     specification. in this mode the 8259s are connected to LINT0 (an
 *     interrupt pin) of the BSP (not bypassing the LAPIC as in PIC mode).
 * Symmetric mode: a mode stipulated by the MP specification where IOAPICs are
 *     used instead of 8259s which can deliver interrupts to any CPU, not just
 *     the BSP.
 * 8259 mode: ACPI's name for the mode that a PC starts in when booting (in MP
 *     spec terminology, either PIC mode or Virtual Wire mode).
 * APIC mode: ACPI's name for the mode where the PC uses IOAPICs instead of
 *     8259s ("Symmetric mode" in MP spec terminology). the OS tells ACPI that
 *     the system is switching to APIC mode by executing the _PIC method with
 *     argument 1 (one).
 * Global system interrupts: ACPI terminology for the distinct IDs assigned to
 *     each interrupt. ACPI provides a table (MADT) that contains the number of
 *     input pins and the global system interrupt start for each IOAPIC, thus
 *     the OS can figure out the mapping of global system interrupts to IOAPIC
 *     input pins.
 * Interrupt source override: information contained in ACPI's MADT table that
 *     instructs the OS how an IRQ maps to an IOAPIC input pin (the IOAPIC pin
 *     number, polarity, and trigger mode).
 *
 * HOW THE OS MAPS PCI INTERRUPT PINS TO IOAPIC INPUT PINS
 *
 * 1. the system boots in PIC or virtual wire mode. the BIOS has arbitrarily
 * routed interrupts from PCI devices to IRQs via PCI link devices. ACPI is
 * thinks the system is in 8259 mode.
 *
 * 2. OS decides to use IOAPICs instead of 8259s. the OS disables 8259s via
 * IMCR register (see MP spec).
 *
 * 3. OS executes ACPI _PIC method with argument value of "1" to ensure that
 * ACPI methods return information concerning IOAPICs and not 8259s.
 *
 * 4. for each PCI device's chosen PCI interrupt pin, the OS iterates through
 * the objects returned by the _PRT (PCI routing table) method on the
 * corresponding bus node in the ACPI device tree looking for the entry for
 * this (PCI dev number, PCI pin) pair. the resulting object describes either
 * the IOAPIC pin or the PCI link device to which the PCI pin connects. if the
 * former, the task is done.
 *
 * 5. the OS uses the ACPI _STA, _DIS, _CRS, and _SRS methods (status, disable,
 * current resources setting, set resource setting) on the PCI link device to
 * determine/configure the IOAPIC pin to which this PCI link device routes.
 *
 * steps 3-5 require an AML interpreter!
 *
 * luckily, my hardware exposes these PCI link devices through chipset PCI
 * device registers and memory mapped IO (in fact the ACPI methods in step 5
 * are implemented using these PCI device registers). thus i can avoid writing
 * an AML interpreter.
 *
 * if possible, i would like to use message signaled interrupts (MSI) -- the
 * documentation makes them seem much, much simpler. i think MSI avoids the
 * need for PCI interrupt pin to IOAPIC pin mapping entirely. however, i'll
 * need to upgrade my PIO disk driver to AHCI first since my SATA controller
 * won't generate MSI intterupts in IDE mode. also, we will still need the
 * IOAPIC to handle IRQs (COM1, keyboard, etc.).
*/

var apic apic_t

type apic_t struct {
	regs struct {
		sel	*uint32
		win	*uint32
		eoi	*uint32
	}
	npins	int
	// spinlock to protect access to the IOAPIC registers. because writing
	// an IOAPIC register requires two distinct memory writes, a single
	// IOAPIC register write cannot be atomic with respect to other memory
	// IOAPIC register writes. we must use a spinlock instead of a mutex
	// because we need to acquire this lock in interrupt context too.
	_mlock	runtime.Spinlock_t
}

func (ap *apic_t) apic_init(aioapic acpi_ioapic_t) {
	// enter "symmetric IO mode" (MP Conf 3.6.2); disable 8259 via IMCR
	runtime.Outb(0x22, 0x70)
	runtime.Outb(0x23, 1)

	base := aioapic.base
	va := dmaplen32(base, 4)
	ap.regs.sel = &va[0]

	va = dmaplen32(base + 0x10, 4)
	ap.regs.win = &va[0]

	va = dmaplen32(base + 0x40, 4)
	ap.regs.eoi = &va[0]

	pinlast := (apic.reg_read(1) >> 16) & 0xff
	ap.npins = int(pinlast + 1)

	bspid := uint32(bsp_apic_id)

	//fmt.Printf("APIC ID:  %#x\n", apic.reg_read(0))
	for i := 0; i < apic.npins; i++ {
		w1 := 0x10 + i*2
		r1 := apic.reg_read(w1)
		// vector: 32 + pin number
		r1 |= 32 + uint32(i)
		var islevel bool
		var islow bool
		if i < 16 {
			// ISA interrupts (IRQs) are edge-triggered, active
			// high
			islevel = false
			islow = false
		} else {
			// PIRQs are level-triggered, active-low (PCH 5.9.6 and
			// 5.10.2)
			islevel = true
			islow = true
		}
		// unless ACPI specifies differently via an "interrupt source
		// override"
		if ovr, ok := aioapic.overrides[i]; ok {
			islevel = ovr.level
			islow = ovr.low
		}
		level := uint32(1 << 15)
		activelow := uint32(1 << 13)
		if islevel {
			r1 |= level
		} else {
			r1 &^= level
		}
		if islow {
			r1 |= activelow
		} else {
			r1 &^= activelow
		}
		// delivery mode: fixed, destination mode: physical
		logical := uint32(1 << 11)
		r1 &^= logical
		dmode := uint32(7 << 8)
		r1 &^= dmode
		mask := uint32(1 << 16)
		r1 |= mask
		apic.reg_write(w1, r1)

		// route to BSP
		w2 := w1 + 1
		r2 := apic.reg_read(w2)
		r2 |= bspid << 24
		apic.reg_write(w2, r2)
	}
	//ap.dump()
}

func (ap *apic_t) reg_read(reg int) uint32 {
	if reg &^ 0xff != 0 {
		panic("bad IO APIC reg")
	}
	c := uint32(reg)
	runtime.Splock(&ap._mlock)
	runtime.Store32(ap.regs.sel, c)
	v := atomic.LoadUint32(ap.regs.win)
	runtime.Spunlock(&ap._mlock)
	return v
}

func (ap *apic_t) reg_write(reg int, v uint32) {
	if reg &^ 0xff != 0 {
		panic("bad IO APIC reg")
	}
	c := uint32(reg)
	runtime.Splock(&ap._mlock)
	runtime.Store32(ap.regs.sel, c)
	runtime.Store32(ap.regs.win, v)
	runtime.Spunlock(&ap._mlock)
}

func (ap *apic_t) irq_unmask(irq int) {
	if irq < 0 || irq > ap.npins {
		panic("bad irq")
	}

	mreg := 0x10 + irq*2
	v := ap.reg_read(mreg)
	maskbit := uint32(1 << 16)
	v &^= maskbit
	ap.reg_write(mreg, v)
}

// XXX nosplit because called from trapstub. this can go away when we have a
// LAPIC that supports EOI broadcast suppression.
//go:nosplit
func (ap *apic_t) irq_mask(irq int) {
	if irq < 0 || irq > ap.npins {
		runtime.Pnum(0xbad2)
		for {}
	}

	runtime.Splock(&ap._mlock)
	mreg := uint32(0x10 + irq*2)

	runtime.Store32(ap.regs.sel, mreg)
	v := atomic.LoadUint32(ap.regs.win)

	maskbit := uint32(1 << 16)
	v |= maskbit

	runtime.Store32(ap.regs.sel, mreg)
	runtime.Store32(ap.regs.win, v)
	runtime.Spunlock(&ap._mlock)
}

// LAPIC's are configured to broadcast EOI to IOAPICs for level-triggered
// interrupts automatically. newer CPUs let you disable EOI broadcast.
func (ap *apic_t) eoi(irq int) {
	if irq &^ 0xff != 0 {
		panic("bad irq")
	}
	runtime.Store32(ap.regs.eoi, uint32(irq + 32))
}

func (ap *apic_t) dump() {
	if ap.npins == 0 {
		return
	}
	for i := 0; i < ap.npins; i++ {
		r1 := ap.reg_read(0x10 + i*2)
		r2 := ap.reg_read(0x10 + i*2 + 1)
		intv := uint64(r2) << 32 | uint64(r1)
		vec := intv & 0xff
		m := intv & (1 << 16) != 0
		t := "edge"
		act := "high"
		if intv & (1 << 13) != 0 {
			act = "low"
		}
		if intv & (1 << 15) != 0 {
			t = "level"
		}
		delivs := map[uint64]string{0:"fixed", 1:"lowest priority",
		    2:"smi", 3:"reserved", 4:"NMI", 5:"INIT", 6:"reserved",
		    7:"ExtINIT"}
		deliv := delivs[((intv >> 8) & 3)]
		destmode := "physical"
		if intv & (1 << 11) != 0 {
			destmode = "logical"
		}
		dest := intv >> 56
		fmt.Printf("IRQ %v: vec: %v, mask: %v, mode: %v, " +
		    "act: %v, deliv: %v, destm: %v, dest: %#x\n", i, vec, m,
		    t, act, deliv, destmode, dest)
	}
}

func regn(r x540reg_t, i int) x540reg_t {
	return r + x540reg_t(i * 4)
}
func reg40(r x540reg_t, i int) x540reg_t {
	return r + x540reg_t(i * 40)
}

type x540reg_t uint
const (
	CTRL		x540reg_t	=    0x0
	// the x540 terminology is confusing regarding interrupts; an interrupt
	// is enabled when its bit is set in the mask set register (ims) and
	// disabled when cleared.
	CTRL_EXT			=    0x18
	EICR				=   0x800
	EIAC				=   0x810
	EITRi				=   0x820
	EITR1i				= 0x12300
	EICS				=   0x808
	IVARi				=   0x900
	EICS1				=   0xa90
	EICS2				=   0xa94
	EIMS				=   0x880
	EIMS1				=   0xaa0
	EIMS2				=   0xaa4
	EIMC				=   0x888
	EIMC1				=   0xab0
	EIMC2				=   0xab4
	EIAM				=   0x890
	GPIE				=   0x898
	EIAM1				=   0xad0
	EIAM2				=   0xad4
	PFVTCTL				=  0x51b0
	SRRCTLl				=  0x1014
	SRRCTL1l			=  0xd014
	RTRPCS				=  0x2430
	RDRXCTL				=  0x2f00
	RXPBSIZEi			=  0x3c00
	PFQDE				=  0x2f04
	RTRUP2TC			=  0x3020
	RTTUP2TC			=  0xc800
	DTXMXSZRQ			=  0x8100
	SECTXMINIFG			=  0x8810
	TXPBSIZEi			=  0xcc00
	TXPBTHRESHi			=  0x4950
	HLREG0				=  0x4240
	MFLCN				=  0x4294
	RTTDQSEL			=  0x4904
	RTTDT1C				=  0x4908
	RTTDT2Ci			=  0x4910
	RTTPT2Ci			=  0xcd20
	RTTDCS				=  0x4900
	RTTPCS				=  0xcd00
	RTRPT4Ci			=  0x2140
	MRQC				=  0xec80
	MTQC				=  0x8120
	MSCA				=  0x425c
	MSRWD				=  0x4260
	LINKS				=  0x42a4
	DMATXCTL			=  0x4a80
	DTXTCPFLGL			=  0x4a88
	DTXTCPFLGH			=  0x4a8c
	// element width is 0x40 bytes
	DCA_TXCTRLi			=  0x600c
	EEMNGCTL			= 0x10110
	SWSM				= 0x10140
	SW_FW_SYNC			= 0x10160
	// element width is 0x40 bytes
	RSCCTLl				=  0x102c
	RSCCTL1l			=  0xd02c

	FLA				= 0x1001c
)

// MDIO device is in bits [20:16] and MDIO reg is in [15:0]
type x540phyreg_t uint
const (
	// link status here (Auto-Negotiation Reserved Vendor Status 1)
	PHY_LINK	x540phyreg_t	= 0x07c810
	ALARMS1				= 0x1ecc00
)

type x540_t struct {
	tag	pcitag_t
	bar0	[]uint32
	_locked	bool
}

func (x *x540_t) init(t pcitag_t) {
	x.tag = t

	bar0, l := pci_bar_mem(t, 0)
	x.bar0 = dmaplen32(bar0, l)

	v := pci_read(t, 0x4, 2)
	memen := 1 << 1
	if v & memen == 0 {
		panic("memory access disabled")
	}
	busmaster := 1 << 2
	if v & busmaster == 0 {
		panic("busmaster disabled")
	}
	pciebmdis := uint32(1 << 2)
	y := x.rl(CTRL)
	if y & pciebmdis != 0 {
		panic("pcie bus master disable set")
	}
	nosnoop_en := 1 << 11
	v = pci_read(t, 0xa8, 2)
	if v & nosnoop_en == 0 {
		panic("pcie no snoop disabled")
	}
}

func (x *x540_t) rs(reg x540reg_t, val uint32) {
	if reg % 4 != 0 {
		panic("bad reg")
	}
	runtime.Store32(&x.bar0[reg/4], val)
}

func (x *x540_t) rl(reg x540reg_t) uint32 {
	if reg % 4 != 0 {
		panic("bad reg")
	}
	return atomic.LoadUint32(&x.bar0[reg/4])
}

func (x *x540_t) log(fm string, args ...interface{}) {
	b, d, f := breakpcitag(x.tag)
	s := fmt.Sprintf("X540:(%v:%v:%v): %s\n", b, d, f, fm)
	fmt.Printf(s, args...)
}

func (x *x540_t) _reset() {
	// if there is any chance that DMA may race with _reset, we must modify
	// _reset to execute the master disable protocol in (5.2.4.3.2)

	// link reset + device reset
	lrst := uint32(1 << 3)
	rst :=  uint32(1 << 26)
	v := x.rl(CTRL)
	v |= rst
	v |= lrst
	x.rs(CTRL, v)
	// 8.2.4.1.1: wait 1ms before checking reset bit after setting
	<- time.After(time.Millisecond)
	for x.rl(CTRL) & rst != 0 {
	}
	// x540 doc 4.6.3.2: wait for 10ms after reset "to enable a
	// smooth initialization flow"
	<- time.After(10*time.Millisecond)
}

func (x *x540_t) _int_disable() {
	maskall := ^uint32(0)
	x.rs(EIMC, maskall)
	x.rs(EIMC1, maskall)
	x.rs(EIMC2, maskall)
}

func (x *x540_t) _phy_read(preg x540phyreg_t) uint16 {
	if preg &^ ((1 << 21) - 1) != 0 {
		panic("bad phy reg")
	}
	mdicmd := uint32(1 << 30)
	// wait for MDIO to be ready
	for x.rl(MSCA) & mdicmd != 0 {
	}
	opaddr := uint32(0)
	phyport := uint32(0)
	v := uint32(preg) | phyport << 21 | opaddr << 26 | mdicmd
	x.rs(MSCA, v)
	for x.rl(MSCA) & mdicmd != 0 {
	}
	opread := uint32(3)
	v = uint32(preg) | phyport << 21 | opread << 26 | mdicmd
	x.rs(MSCA, v)
	for x.rl(MSCA) & mdicmd != 0 {
	}
	ret := x.rl(MSRWD)
	return uint16(ret >> 16)
}

var dur struct {
	sw	int
	hw	int
	fw	int
	nvmup	int
	swmng	int
}

// acquires the "lock" protecting the semaphores. returns whether fw timedout
func (x *x540_t) _reg_acquire() bool {
	to := 3*time.Second
	st := time.Now()
	smbi := uint32(1 << 0)
	for x.rl(SWSM) & smbi != 0 {
		if time.Since(st) > to {
			panic("SWSM timeout!")
		}
	}
	var fwdead bool
	st = time.Now()
	regsmp := uint32(1 << 31)
	for x.rl(SW_FW_SYNC) & regsmp != 0 {
		if time.Since(st) > to {
			x.log("SW_FW_SYNC timeout!")
			fwdead = true
			break
		}
	}
	return fwdead
}

func (x *x540_t) _reg_release() {
	regsmp := uint32(1 << 31)
	x.rs(SW_FW_SYNC, x.rl(SW_FW_SYNC) &^ regsmp)
	x.rs(SWSM, 0)
}

// takes the semaphore protecting a hardware resource
func (x *x540_t) hwlock() {
	if x._locked {
		panic("two hwlocks")
	}
	for i := 0; i < 100; i++ {
		if x._hwlock() {
			x._locked = true
			return
		}
		<- time.After(10*time.Millisecond)
	}
	fmt.Printf("lock stats: %v\n", dur)
	panic("hwlock timedout")
}

// returns true if the called acquired the software/firmware semaphore
func (x *x540_t) _hwlock() bool {
	// 11.7.5; this semaphore protects NVM, PHY[01], and MAC shared regs
	fwdead := x._reg_acquire()

	//sw_nvm  := uint32(1 << 0)
	//sw_phy0 := uint32(1 << 1)
	//sw_phy1 := uint32(1 << 2)
	//sw_mac  := uint32(1 << 3)
	hw_nvm  := uint32(1 << 4)
	fw_nvm  := uint32(1 << 5)
	fw_phy0 := uint32(1 << 6)
	fw_phy1 := uint32(1 << 7)
	fw_mac  := uint32(1 << 8)
	nvm_up  := uint32(1 << 9)
	sw_mng  := uint32(1 << 10)
	fwbits := fw_nvm | fw_phy0 | fw_phy1 | fw_mac

	ret := false
	v := x.rl(SW_FW_SYNC)
	if v & hw_nvm != 0 {
		dur.hw++
		goto out
	}
	if v & 0xf != 0 {
		dur.sw++
		goto out
	}
	if !fwdead && v & fwbits != 0 {
		dur.fw++
		goto out
	}
	if v & nvm_up != 0 {
		dur.nvmup++
	}
	if v & sw_mng != 0 {
		dur.swmng++
	}
	x.rs(SW_FW_SYNC, v | 0xf)
	ret = true
out:
	x._reg_release()
	return ret
}

func (x *x540_t) hwunlock() {
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

func attach_x540t(vid, did int, t pcitag_t) {
	b, d, f := breakpcitag(t)
	fmt.Printf("X540: %x %x (%d:%d:%d)\n", vid, did, b, d, f)
	if uint(f) > 1 {
		panic("virtual functions not supported")
	}

	var x x540_t
	x.init(t)

	// x540 doc 4.6.3 initialization sequence
	x._int_disable()
	x._reset()
	x._int_disable()

	// even though we disable flow control, we write 0 to FCTTV, FCRTL,
	// FCRTH, FCRTV, and  FCCFG
	n := 4
	fcttv := x540reg_t(0x3200)
	for i := 0; i < n; i++ {
		x.rs(regn(fcttv, i), 0)
	}
	n = 8
	fcrtl := x540reg_t(0x3220)
	fcrth := x540reg_t(0x3260)
	// XXX XXX set default values for FCRTH[n].RTH?
	for i := 0; i < n; i++ {
		x.rs(regn(fcrtl, i), 0)
		x.rs(regn(fcrth, i), 0)
	}

	fcrtv := x540reg_t(0x32a0)
	fccfg := x540reg_t(0x3d00)
	x.rs(fcrtv, 0)
	x.rs(fccfg, 0)

	mflcn := x540reg_t(0x4294)
	rfce := uint32(1 << 3)
	son := x.rl(mflcn) & rfce != 0
	if son {
		panic("receive flow control should be off?")
	}

	// enable no snooping
	nosnoop_dis := uint32(1 << 16)
	v := x.rl(CTRL_EXT)
	if v & nosnoop_dis != 0 {
		x.log("no snoop disabled. enabling.")
		x.rs(CTRL_EXT, v &^ nosnoop_dis)
	}

	x.hwlock()
	phyreset := uint16(1 << 6)
	for x._phy_read(ALARMS1) & phyreset == 0 {
	}
	x.log("phy reset complete")
	x.hwunlock()

	// 4.6.3 says to wait for CFG_DONE, but this bit never seems to set.
	// openbsd does not check this bit.

	//x.log("manage wait")
	//cfg_done := uint32(1 << 18 + f)
	//for x.rl(EEMNGCTL) & cfg_done == 0 {
	//}
	//x.log("manageability complete")

	dmadone := uint32(1 << 3)
	for x.rl(RDRXCTL) & dmadone == 0 {
	}
	x.log("dma engine initialized")

	msiaddrl := 0x54
	msidata := 0x5c

	maddr := 0xfee << 20
	pci_write(x.tag, msiaddrl, maddr)
	vec := 19
	mdata := vec | bsp_apic_id << 12
	pci_write(x.tag, msidata, mdata)

	// enable MSI interrupts
	msictrl := 0x50
	pv := pci_read(x.tag, msictrl, 4)
	pv |= 1 << 16
	pci_write(x.tag, msictrl, pv)

	msimask := 0x60
	if pci_read(x.tag, msimask, 4) & 1 != 0 {
		panic("msi pci masked")
	}

	// make sure legacy PCI interrupts are disabled
	pciintdis := 1 << 10
	pv = pci_read(x.tag, 0x4, 2)
	pci_write(x.tag, 0x4, pv | pciintdis)

	// disable autoclear/automask
	x.rs(EIAC, 0)
	x.rs(EIAM, 0)
	x.rs(EIAM1, 0)
	x.rs(EIAM2, 0)

	// disable interrupt throttling
	for n := 0; n < 24; n++ {
		x.rs(regn(EITRi, n), 0)
	}
	for n := 0; n < 104; n++ {
		x.rs(regn(EITR1i, n), 0)
	}

	// map all 128 rx/tx queues to interrupt 0
	for n := 0; n < 64; n++ {
		v := uint32(0x80808080)
		x.rs(regn(IVARi, n), v)
	}

	// disable RSC; docs say RSC is enabled by default, but it isn't on my
	// card...
	for n := 0; n < 128; n++ {
		var reg x540reg_t
		if n < 64 {
			reg = reg40(RSCCTLl, n)
		} else {
			reg = reg40(RSCCTL1l, n-64)
		}
		v := x.rl(reg)
		rscen := uint32(1 << 0)
		x.rs(reg, v &^ rscen)
	}

	// transmit init
	{
		txcrc      := uint32(1 <<  0)
		crcstrip   := uint32(1 <<  1)
		txpad      := uint32(1 << 10)
		rxlerr     := uint32(1 << 27)
		// HLREG0 contains non-zero reserved bits; read first to make
		// sure reserved bits stay set.
		v := x.rl(HLREG0)
		v |= txcrc | crcstrip | txpad | rxlerr
		x.rs(HLREG0, v)

		nosnoop_tso := uint32(1 << 1)
		v = x.rl(DMATXCTL)
		v |= nosnoop_tso
		x.rs(DMATXCTL, v)

		// XXX fill in mask bits for TCP segment flags
		//x.rs(DTXTCPFLGL, xxx)
		//x.rs(DTXTCPFLGH, xxx)

		// XXX may want to enable relaxed ordering or DCA for tx
		//for n := 0; n < 128; n++ {
		//	x.rs(reg40(DCA_TXCTRLi, n), xxx)
		//}

		// if necessary, setup IPG (inter-packet gap) here
		// ...

		arbdis := uint32(1 << 6)
		v = x.rl(RTTDCS)
		v |= arbdis
		x.rs(RTTDCS, v)

		x._dbc_init()

		v = x.rl(RTTDCS)
		v &^= arbdis
		x.rs(RTTDCS, v)

		// XXX setup tx queues...
	}

	x.rs(GPIE, 0)

	// clear all previous interrupts
	x.rs(EICR, ^uint32(0))
}

// _dbc_init() must only be called when RTTDCS.ARBDIS == 1 (7.2.1.2.1)
func (x *x540_t) _dbc_init() {
	// dbc=off, vt=off (section 4.6.11.3.4)
	rxpbsize := uint32(0x180 << 10)
	x.rs(regn(RXPBSIZEi, 0), rxpbsize)
	for n := 1; n < 8; n++ {
		x.rs(regn(RXPBSIZEi, n), 0)
	}
	txpbsize := uint32(0xa0 << 10)
	x.rs(regn(TXPBSIZEi, 0), txpbsize)
	for n := 1; n < 8; n++ {
		x.rs(regn(TXPBSIZEi, n), 0)
	}
	txpbthresh := uint32(0xa0)
	x.rs(regn(TXPBTHRESHi, 0), txpbthresh)
	for n := 1; n < 8; n++ {
		x.rs(regn(TXPBTHRESHi, n), 0)
	}

	v := x.rl(MRQC)
	mrqe := uint32(0xf)
	v &^= mrqe
	x.rs(MRQC, v)

	v = x.rl(MTQC)
	vtdbc := uint32(0xf)
	v &^= vtdbc
	x.rs(MTQC, v)

	vten := uint32(1 << 0)
	v = x.rl(PFVTCTL)
	v &^= vten
	x.rs(PFVTCTL, v)

	v = x.rl(PFQDE)
	dropen := uint32(1 << 0)
	v &^= dropen
	x.rs(PFQDE, v)

	// XXX per queue dropping?
	//for n := 0; n < 128; n++ {
	//	var reg x540reg_t
	//	if n < 64 {
	//		reg = reg40(SRRCTLl, n)
	//	} else {
	//		reg = reg40(SRRCTL1l, n-64)
	//	}
	//}

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
		x.rs(regn(RTTDT2Ci, n), 0)
	}
	for n := 0; n < 8; n++ {
		x.rs(regn(RTTPT2Ci, n), 0)
	}
	for n := 0; n < 8; n++ {
		x.rs(regn(RTRPT4Ci, n), 0)
	}

	v = x.rl(RTTDCS)
	tdpac  := uint32(1 <<  0)
	vmpac  := uint32(1 <<  1)
	tdrm   := uint32(1 <<  4)
	bdpm   := uint32(1 << 22)
	bpbfsm := uint32(1 << 23)
	v &^= tdpac | vmpac | tdrm
	v |= bdpm | bpbfsm
	x.rs(RTTDCS, v)

	v = x.rl(RTTPCS)
	tppac  := uint32(1 << 5)
	tprm   := uint32(1 << 8)
	arbd   := uint32(0x224 << 22)
	arbmask := uint32(((1 << 10) - 1) << 22)
	v &^= tppac | tprm | arbmask
	v |= arbd
	x.rs(RTTPCS, v)

	v = x.rl(RTRPCS)
	rrm  := uint32(1 << 1)
	rac  := uint32(1 << 2)
	v &^= rrm | rac
	x.rs(RTRPCS, v)
}
