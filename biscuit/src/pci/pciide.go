package pci

import "fmt"
import "runtime"

import "apic"
import "defs"

type pciide_disk_t struct {
	rbase   uintptr
	allstat uintptr
	bmaster uintptr
}

func attach_3400(vendorid, devid int, tag Pcitag_t) {
	if Disk != nil {
		panic("adding two disks")
	}

	gsi := pci_disk_interrupt_wiring(tag)
	IRQ_DISK = gsi
	INT_DISK = defs.IRQ_BASE + IRQ_DISK

	d := &pciide_disk_t{}
	// 3400's PCI-native IDE command/control block
	rbase := pci_bar_pio(tag, 0)
	allstats := pci_bar_pio(tag, 1)
	busmaster := pci_bar_pio(tag, 4)

	d.init(rbase, allstats, busmaster)
	Disk = d
	fmt.Printf("3400: base %#x, cntrl: %#x, bm: %#x, irq: %d\n", rbase,
		allstats, busmaster, gsi)
}

func (d *pciide_disk_t) init(base, allst, busmaster uintptr) {
	d.rbase = base
	d.allstat = allst
	d.bmaster = busmaster
	ide_init(d.rbase)
}

func (d *pciide_disk_t) Start(ibuf *Idebuf_t, writing bool) {
	ide_start(d.rbase, d.allstat, ibuf, writing)
}

func (d *pciide_disk_t) Complete(dst []uint8, writing bool) {
	ide_complete(d.rbase, dst, writing)
}

func (d *pciide_disk_t) Intr() bool {
	streg := uint16(d.bmaster + 0x02)
	bmintr := uint(1 << 2)
	st := runtime.Inb(streg)
	if st&bmintr == 0 {
		return false
	}
	return true
}

func (d *pciide_disk_t) Int_clear() {
	// read status so disk clears int
	runtime.Inb(uint16(d.rbase + 7))
	runtime.Inb(uint16(d.rbase + 7))

	// in PCI-native mode, clear the interrupt via the legacy bus master
	// base, bar 4.
	streg := uint16(d.bmaster + 0x02)
	st := runtime.Inb(streg)
	er := uint(1 << 1)
	if st&er != 0 {
		panic("disk error")
	}
	runtime.Outb(streg, uint8(st))

	// and via apic
	apic.Apic.Irq_unmask(IRQ_DISK)
	// irq_eoi(IRQ_DISK)
}
