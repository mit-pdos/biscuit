package apic

import "fmt"
import "runtime"
import "sync/atomic"
import "unsafe"

import "defs"
import "mem"
import "util"

var Bsp_apic_id int

const apic_debug bool = false

func dbg(f string, args...interface{}) {
	if apic_debug {
		fmt.Printf(f, args...)
	}
}

// these functions can only be used when interrupts are cleared
//go:nosplit
func lap_id() int {
	a := unsafe.Pointer(uintptr(0xfee00000))
	lapaddr := (*[1024]uint32)(a)
	return int(lapaddr[0x20/4] >> 24)
}

func Bsp_init() {
	Bsp_apic_id = lap_id()
	dbg("BSP APIC ID: %#x\n", Bsp_apic_id)
}

var Apic apic_t

type apic_t struct {
	regs struct {
		sel *uint32
		win *uint32
		eoi *uint32
	}
	npins int
	// spinlock to protect access to the IOAPIC registers. because writing
	// an IOAPIC register requires two distinct memory writes, a single
	// IOAPIC register write cannot be atomic with respect to other memory
	// IOAPIC register writes. we must use a spinlock instead of a mutex
	// because we need to acquire this lock in interrupt context too.
	_mlock runtime.Spinlock_t
}

func (ap *apic_t) apic_init(aioapic acpi_ioapic_t) {
	// enter "symmetric IO mode" (MP Conf 3.6.2); disable 8259 via IMCR
	runtime.Outb(0x22, 0x70)
	runtime.Outb(0x23, 1)

	base := aioapic.base
	va := mem.Dmaplen32(base, 4)
	ap.regs.sel = &va[0]

	va = mem.Dmaplen32(base+0x10, 4)
	ap.regs.win = &va[0]

	va = mem.Dmaplen32(base+0x40, 4)
	ap.regs.eoi = &va[0]

	pinlast := (Apic.reg_read(1) >> 16) & 0xff
	ap.npins = int(pinlast + 1)
	if ap.npins < 24 {
		panic("unexpectedly few pins on first IO APIC")
	}

	bspid := uint32(Bsp_apic_id)

	dbg("APIC ID:  %#x\n", Apic.reg_read(0))
	for i := 0; i < Apic.npins; i++ {
		w1 := 0x10 + i*2
		r1 := Apic.reg_read(w1)
		// vector: 32 + pin number
		r1 = defs.IRQ_BASE + uint32(i)
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
		Apic.reg_write(w1, r1)

		// route to BSP
		w2 := w1 + 1
		r2 := Apic.reg_read(w2)
		r2 = bspid << 24
		Apic.reg_write(w2, r2)
	}
	//ap.dump()
}

func (ap *apic_t) reg_read(reg int) uint32 {
	if reg&^0xff != 0 {
		panic("bad IO APIC reg")
	}
	c := uint32(reg)
	fl := runtime.Pushcli()
	// XXX don't take this lock in uninterruptible and interruptible
	// contexts
	runtime.Splock(&ap._mlock)
	runtime.Store32(ap.regs.sel, c)
	v := atomic.LoadUint32(ap.regs.win)
	runtime.Spunlock(&ap._mlock)
	runtime.Popcli(fl)
	return v
}

func (ap *apic_t) reg_write(reg int, v uint32) {
	if reg&^0xff != 0 {
		panic("bad IO APIC reg")
	}
	c := uint32(reg)
	fl := runtime.Pushcli()
	runtime.Splock(&ap._mlock)
	runtime.Store32(ap.regs.sel, c)
	runtime.Store32(ap.regs.win, v)
	runtime.Spunlock(&ap._mlock)
	runtime.Popcli(fl)
}

func (ap *apic_t) Irq_unmask(irq int) {
	if irq < 0 || irq > ap.npins {
		panic("irq_unmask: bad irq")
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
func (ap *apic_t) Irq_mask(irq int) {
	if irq < 0 || irq > ap.npins {
		runtime.Pnum(0xbad2)
		for {
		}
	}

	fl := runtime.Pushcli()
	runtime.Splock(&ap._mlock)
	mreg := uint32(0x10 + irq*2)

	runtime.Store32(ap.regs.sel, mreg)
	v := atomic.LoadUint32(ap.regs.win)

	maskbit := uint32(1 << 16)
	v |= maskbit

	runtime.Store32(ap.regs.sel, mreg)
	runtime.Store32(ap.regs.win, v)
	runtime.Spunlock(&ap._mlock)
	runtime.Popcli(fl)
}

// LAPIC's are configured to broadcast EOI to IOAPICs for level-triggered
// interrupts automatically. newer CPUs let you disable EOI broadcast.
func (ap *apic_t) eoi(irq int) {
	if irq&^0xff != 0 {
		panic("eio bad irq")
	}
	runtime.Store32(ap.regs.eoi, uint32(irq+defs.IRQ_BASE))
}

func (ap *apic_t) dump() {
	if ap.npins == 0 {
		return
	}
	fmt.Printf("APIC BASE: %p\n", ap.regs.sel)
	for i := 0; i < ap.npins; i++ {
		r1 := ap.reg_read(0x10 + i*2)
		r2 := ap.reg_read(0x10 + i*2 + 1)
		intv := uint64(r2)<<32 | uint64(r1)
		vec := intv & 0xff
		m := intv&(1<<16) != 0
		t := "edge"
		act := "high"
		if intv&(1<<13) != 0 {
			act = "low"
		}
		if intv&(1<<15) != 0 {
			t = "level"
		}
		delivs := map[uint64]string{0: "fixed", 1: "lowest priority",
			2: "smi", 3: "reserved", 4: "NMI", 5: "INIT", 6: "reserved",
			7: "ExtINIT"}
		deliv := delivs[((intv >> 8) & 3)]
		destmode := "physical"
		if intv&(1<<11) != 0 {
			destmode = "logical"
		}
		dest := intv >> 56
		fmt.Printf("IRQ %v: vec: %v, mask: %v, mode: %v, "+
			"act: %v, deliv: %v, destm: %v, dest: %#x\n", i, vec, m,
			t, act, deliv, destmode, dest)
	}
}

func Acpi_attach() int {
	rsdp, ok := _acpi_scan()
	if !ok {
		panic("no RSDP")
	}
	rsdtn := mem.Pa_t(util.Readn(rsdp, 4, 16))
	//xsdtn := Readn(rsdp, 8, 24)
	rsdt := mem.Dmaplen(rsdtn, 8)
	if rsdtn == 0 || string(rsdt[:4]) != "RSDT" {
		panic("no RSDT")
	}
	rsdtlen := util.Readn(rsdt, 4, 4)
	rsdt = mem.Dmaplen(rsdtn, rsdtlen)
	// verify RSDT checksum
	_acpi_cksum(rsdt)
	// may want to search XSDT, too
	ncpu, ioapic, ok := _acpi_madt(rsdt)
	if !ok {
		panic("no cpu count")
	}

	Apic.apic_init(ioapic)

	msi := _acpi_fadt(rsdt)
	if !msi {
		panic("no MSI")
	}

	return ncpu
}
