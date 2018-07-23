package pci

import "fmt"
import "runtime"
import "unsafe"

import "apic"

type legacy_disk_t struct {
	rbase   uintptr
	allstat uintptr
}

func (d *legacy_disk_t) init(base, allst uintptr) {
	d.rbase = base
	d.allstat = allst
	ide_init(d.rbase)
}

func (d *legacy_disk_t) Start(ibuf *Idebuf_t, writing bool) {
	ide_start(d.rbase, d.allstat, ibuf, writing)
}

func (d *legacy_disk_t) Complete(dst []uint8, writing bool) {
	ide_complete(d.rbase, dst, writing)
}

func (d *legacy_disk_t) Intr() bool {
	return true
}

func (d *legacy_disk_t) Int_clear() {
	// read status so disk clears int
	runtime.Inb(uint16(d.rbase + 7))
	runtime.Inb(uint16(d.rbase + 7))
	apic.Apic.Irq_unmask(IRQ_DISK)
	// irq_eoi(IRQ_DISK)
}

// use ata pio for fair comparisons against xv6, but i want to use ahci (or
// something) eventually. unlike xv6, we always use disk 0
const (
	ide_bsy  = 0x80
	ide_drdy = 0x40
	ide_df   = 0x20
	ide_err  = 0x01

	ide_cmd_read  = 0x20
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
		fmt.Printf("IDE disk detected\n")
		return true
	}

	fmt.Printf("no IDE disk\n")
	return false
}

func ide_wait(base uintptr, chk bool) bool {
	var r int
	c := 0
	for {
		r = int(runtime.Inb(uint16(base + 7)))
		if r&(ide_bsy|ide_drdy) == ide_drdy {
			break
		}
		c++
		if c > 10000000 {
			fmt.Printf("waiting a very long time for disk...\n")
			c = 0
		}
	}
	if chk && r&(ide_df|ide_err) != 0 {
		return false
	}
	return true
}

func idedata_ready(base uintptr) {
	c := 0
	for {
		drq := 1 << 3
		st := int(runtime.Inb(uint16(base + 7)))
		if st&drq != 0 {
			return
		}
		c++
		if c > 10000000 {
			fmt.Printf("waiting a long time for DRQ...\n")
		}
	}
}

func ide_start(rbase, allstatus uintptr, ibuf *Idebuf_t, writing bool) {
	ireg := func(n uintptr) uint16 {
		return uint16(rbase + n)
	}
	ide_wait(rbase, false)
	outb := runtime.Outb
	outb(uint16(allstatus), 0)
	outb(ireg(2), 1)
	bn := ibuf.Block
	bd := ibuf.Disk
	outb(ireg(3), uint8(bn&0xff))
	outb(ireg(4), uint8((bn>>8)&0xff))
	outb(ireg(5), uint8((bn>>16)&0xff))
	outb(ireg(6), uint8(0xe0|((bd&1)<<4)|(bn>>24)&0xf))
	if writing {
		outb(ireg(7), ide_cmd_write)
		idedata_ready(rbase)
		runtime.Outsl(int(ireg(0)), unsafe.Pointer(&ibuf.Data[0]),
			512/4)
	} else {
		outb(ireg(7), ide_cmd_read)
	}
}

func ide_complete(base uintptr, dst []uint8, writing bool) {
	if !writing {
		// read sector
		if ide_wait(base, true) {
			runtime.Insl(uint16(base+0),
				unsafe.Pointer(&dst[0]), 512/4)
		}
	} else {
		// cache flush; only needed for old disks?
		//runtime.Outb(base + 7, 0xe7)
	}
}
