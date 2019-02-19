package ahci

import "fmt"
import "runtime"
import "sync"
import "sync/atomic"
import "unsafe"
import "container/list"

import "apic"

import "fs"
import "mem"
import "msi"
import "pci"
import "stats"

const ahci_debug = false

func dbg(x string, args...interface{}) {
	if ahci_debug {
		fmt.Printf(x, args...)
	}
}

//
// AHCI from sv6 from HiStar.
//
// Some useful docs:
// - http://wiki.osdev.org/AHCI
// - AHCI: https://www.intel.com/content/dam/www/public/us/en/documents/technical-specifications/serial-ata-ahci-spec-rev1-3-1.pdf
// - FIS: http://www.ece.umd.edu/courses/enee759h.S2003/references/serialata10a.pdf
// - CMD: http://www.t13.org/documents/uploadeddocuments/docs2007/d1699r4a-ata8-acs.pdf
//

var Ahci fs.Disk_i

type blockmem_t struct {
}

var Blockmem = &blockmem_t{}

func (bm *blockmem_t) Alloc() (mem.Pa_t, *mem.Bytepg_t, bool) {
	_, pa, ok := mem.Physmem.Refpg_new()
	if ok {
		d := (*mem.Bytepg_t)(unsafe.Pointer(mem.Physmem.Dmap(pa)))
		mem.Physmem.Refup(pa)
		return pa, d, ok
	} else {
		return pa, nil, ok
	}
}

func (bm *blockmem_t) Free(pa mem.Pa_t) {
	mem.Physmem.Refdown(pa)
}

func (bm *blockmem_t) Refup(pa mem.Pa_t) {
	mem.Physmem.Refup(pa)
}

// returns true if start is asynchronous
func (ahci *ahci_disk_t) Start(req *fs.Bdev_req_t) bool {
	if ahci.port == nil {
		panic("nil port")
	}
	ahci.port.start(req)
	return true
}

func (ahci *ahci_disk_t) Stats() string {
	if ahci == nil {
		panic("no adisk")
	}
	return ahci.port.stat.stat()
}

func attach_ahci(vid, did int, t pci.Pcitag_t) {
	if pci.Disk != nil {
		panic("adding two disks")
	}

	d := &ahci_disk_t{}
	d.tag = t
	barmask := ^uintptr((1 << 4) - 1)
	d.bara = uintptr(pci.Pci_read(t, pci.BAR5, 4)) & barmask
	ahci_base_len := 0x1100
	if uintptr(ahci_base_len) < unsafe.Sizeof(ahci_reg_t{}) {
		panic("struct larger than MMIO regs")
	}
	m := mem.Dmaplen32(d.bara, ahci_base_len)
	d.ahci = (*ahci_reg_t)(unsafe.Pointer(&(m[0])))
	if LD(&d.ahci.cap) & (1 << 31) == 0 {
		panic("64bit addressess not supported")
	}

	vec := msi.Msivec_t(0)
	msicap := 0x80
	cap_entry := pci.Pci_read(d.tag, msicap, 4)
	if cap_entry&0x1F != 0x5 {
		// this only works with bhw, but the driver uses MSI now; kill
		// this code?
		//pci.IRQ_DISK = 11 // XXX pci_disk_interrupt_wiring(t) returns 23, but 11 works
		//pci.INT_DISK = defs.IRQ_BASE + pci.IRQ_DISK
		panic("AHCI: no MSI\n")
	} else { // enable MSI interrupts
		vec = msi.Msi_alloc()

		dbg("AHCI: msicap %#x MSI to vec %#x\n", cap_entry, vec)

		var is_64bit = false
		if cap_entry&PCI_MSI_MCR_64BIT != 0 {
			is_64bit = true
		}

		// Disable multiple messages.  Since we specify only one message
		// and that is the default value in the message control
		// register, this simplifies configuration.
		log2_messages := uint32((cap_entry >> 17) & 0x7)
		if log2_messages != 0 {
			fmt.Printf("pci_map_msi_irq: requested messages %u, granted 1 message\n",
				1<<log2_messages)
			// Multiple Message Enable is bits 20-22.
			pci.Pci_write(d.tag, msicap, cap_entry & ^(0x7<<20))
		}

		// [PCI SA pg 253] Assign a dword-aligned memory address to the
		// device's Message Address Register.  (The Message Address
		// Register format is mandated by the x86 architecture.  See
		// 9.11.1 in the Vol. 3 of the Intel architecture manual.)

		// Non-remapped ("compatibility format") interrupts
		pci.Pci_write(d.tag, msicap+4*1,
			(0x0fee<<20)| // magic constant for northbridge
				(apic.Bsp_apic_id<<12)| // destination ID
				(1<<3)| // redirection hint
				(0<<2)) // destination mode

		if is_64bit {
			// Zero out the most-significant 32-bits of the Message Address Register,
			// which is at Dword 2 for 64-bit devices.
			pci.Pci_write(d.tag, msicap+4*2, 0)
		}

		//  Write base message data pattern into the device's Message
		//  Data Register.  (The Message Data Register format is
		//  mandated by the x86 architecture.  See 9.11.2 in the Vol. 3
		//  of the Intel architecture manual.  Message Data Register is
		//  at Dword 2 for 32-bit devices, and at Dword 3 for 64-bit
		//  devices.
		var offset = 2
		if is_64bit {
			offset = 3
		}
		pci.Pci_write(d.tag, msicap+4*offset,
			(0<<15)| // trigger mode (edge)
				//(0 << 14) |      // level for trigger mode (don't care)
				(0<<8)| // delivery mode (fixed)
				int(vec)) // vector

		// Set the MSI enable bit in the device's Message control
		// register.
		pci.Pci_write(d.tag, msicap, cap_entry|(1<<16))

		msimask := 0x60
		if pci.Pci_read(d.tag, msimask, 4)&1 != 0 {
			panic("msi pci masked")
		}

	}
	bus, dev, fnc := pci.Breakpcitag(t)
	fmt.Printf("AHCI %x %x (%v:%v:%v), bara %#x, MSI %v\n", vid, did, bus,
	    dev, fnc, d.bara, vec)

	SET(&d.ahci.ghc, AHCI_GHC_AE)

	d.ncs = ((LD(&d.ahci.cap) >> 8) & 0x1f) + 1
	dbg("AHCI: ncs %#x\n", d.ncs)

	for i := 0; i < 32; i++ {
		if LD(&d.ahci.pi)&(1<<uint32(i)) != 0x0 {
			if d.probe_port(i) {
				break
			}
		}
	}

	go d.int_handler(vec)
	Ahci = d
}

//
// Implementation
//

type ahci_reg_t struct {
	cap       uint32 // host capabilities
	ghc       uint32 // global host control
	is        uint32 // interrupt status
	pi        uint32 // ports implemented
	vs        uint32 // version
	ccc_ctl   uint32 // command completion coalescing control
	ccc_ports uint32 // command completion coalescing ports
	em_loc    uint32 // enclosure management location
	em_ctl    uint32 // enclosure management control
	cap2      uint32 // extended host capabilities
	bohc      uint32 // BIOS/OS handoff control and status
}

type port_reg_t struct {
	clb      uint64 // command list base address
	fb       uint64 // FIS base address
	is       uint32 // interrupt status
	ie       uint32 // interrupt enable
	cmd      uint32 // command and status
	reserved uint32
	tfd      uint32 // task file data
	sig      uint32 // signature
	ssts     uint32 // sata phy status: SStatus
	sctl     uint32 // sata phy control: SControl
	serr     uint32 // sata phy error: SError
	sact     uint32 // sata phy active: SActive
	ci       uint32 // command issue
	sntf     uint32 // sata phy notification: SNotify
	fbs      uint32 // FIS-based switching control
}

type ahci_disk_t struct {
	bara     uintptr
	model    string
	ahci     *ahci_reg_t
	tag      pci.Pcitag_t
	ncs      uint32
	nsectors uint64
	port     *ahci_port_t
	portid   int
}

type sata_fis_reg_h2d struct {
	fis_type uint8
	cflag    uint8
	command  uint8
	features uint8

	lba_0    uint8
	lba_1    uint8
	lba_2    uint8
	dev_head uint8

	lba_3       uint8
	lba_4       uint8
	lba_5       uint8
	features_ex uint8

	sector_count    uint8
	sector_count_ex uint8
	__pad1          uint8
	control         uint8

	__pad2 [4]uint8
}

type sata_fis_reg_d2h struct {
	fis_type uint8
	cflag    uint8
	status   uint8
	error    uint8

	lba_0    uint8
	lba_1    uint8
	lba_2    uint8
	dev_head uint8

	lba_3       uint8
	lba_4       uint8
	lba_5       uint8
	features_ex uint8

	sector_count    uint8
	sector_count_ex uint8
	__pad1          uint8
	control         uint8

	__pad2 [4]uint8
}

type ahci_recv_fis struct {
	dsfis    [0x20]uint8      // DMA setup FIS
	psfis    [0x20]uint8      // PIO setup FIS
	reg      sata_fis_reg_d2h // D2H register FIS
	_pad     [0x4]uint8
	sdbfis   [0x8]uint8  // set device bits FIS
	ufis     [0x40]uint8 // unknown FIS
	reserved [0x60]uint8
}

type ahci_cmd_header struct {
	flags     uint16
	prdtl     uint16
	prdbc     uint32
	ctba      uint64
	reserved0 uint64
	reserved1 uint64
}

type ahci_prd struct {
	dba      uint64
	reserved uint32
	dbc      uint32 // one less than #bytes
}

const (
	MAX_PRD_ENTRIES int = 65536
	MAX_PRD_SIZE    int = 4 * 1024 * 1024
)

type ahci_cmd_table struct {
	cfis     [0x10]uint32 // command FIS
	acmd     [0x10]uint8  // ATAPI command
	reserved [0x30]uint8
	prdt     [MAX_PRD_ENTRIES]ahci_prd
}

type ahci_port_stat_t struct {
	Nbarrier  stats.Counter_t
	Nwrite    stats.Counter_t
	Niwrite   stats.Counter_t
	Nvwrite   stats.Counter_t
	Nread     stats.Counter_t
	Nnoslot   stats.Counter_t
	Ncoalesce stats.Counter_t
	Nintr     stats.Counter_t
}

type ahci_port_t struct {
	sync.Mutex
	cond_flush  *sync.Cond
	cond_queued *sync.Cond

	hba_id   int
	port      *port_reg_t
	nslot     uint32
	next_slot uint32
	inflight  []*fs.Bdev_req_t
	queued    *list.List
	nwaiting  int
	nflush    int

	block_pa [32]uintptr
	block    [32]*[512]uint8

	rfis_pa uintptr
	rfis    *ahci_recv_fis
	cmdh_pa uintptr
	cmdh    *[32]ahci_cmd_header
	cmdt_pa uintptr
	cmdt    *[32]ahci_cmd_table

	stat ahci_port_stat_t
}

type identify_device_t struct {
	_             [10]uint16 // Words 0-9
	serial        [20]uint8  // Words 10-19
	_             [3]uint16  // Words 20-22
	firmware      [8]uint8   // Words 23-26
	model         [40]uint8  // Words 27-46
	_             [13]uint16 // Words 47-59
	lba_sectors   uint32     // Words 60-61, assuming little-endian
	_             [13]uint16 // Words 62-74
	queue_depth   uint16     // Word 75
	sata_caps     uint16     // Word 76
	_             [6]uint16  // Words 77-82
	features83    uint16     // Words 83
	_             uint16     // Word 84
	features85    uint16     // Word 85
	features86    uint16     // Word 86
	features87    uint16     // Word 87
	udma_mode     uint16     // Word 88
	_             [4]uint16  // Words 89-92
	hwreset       uint16     // Word 93
	_             [6]uint16  // Words 94-99
	lba48_sectors uint64     // Words 100-103, assuming little-endian
	_             [15]uint16 // Words 104-118
	features119   uint16     // Word 119
}

const (
	PCI_MSI_MCR_64BIT = 0x00800000

	HBD_PORT_IPM_ACTIVE  uint32 = 1
	HBD_PORT_DET_PRESENT uint32 = 3

	SATA_SIG_ATA = 0x00000101 // SATA drive

	AHCI_GHC_AE uint32 = (1 << 31) // Use AHCI to communicate
	AHCI_GHC_IE uint32 = (1 << 1)  // Enable interrupts from AHCI

	AHCI_PORT_CMD_ST     uint32 = (1 << 0)  // start
	AHCI_PORT_CMD_SUD    uint32 = (1 << 1)  // spin-up device
	AHCI_PORT_CMD_POD    uint32 = (1 << 2)  // power on device
	AHCI_PORT_CMD_FRE    uint32 = (1 << 4)  // FIS receive enable
	AHCI_PORT_CMD_FR     uint32 = (1 << 14) // FIS receive running
	AHCI_PORT_CMD_CR     uint32 = (1 << 15) // command list running
	AHCI_PORT_CMD_ACTIVE uint32 = (1 << 28) // ICC active

	AHCI_PORT_INTR_DPE  = (1 << 5) // Descriptor (PRD) processed
	AHCI_PORT_INTR_SDBE = (1 << 3) // Set Device Bits FIS received
	AHCI_PORT_INTR_DSE  = (1 << 2) // DMA Setup FIS received
	AHCI_PORT_INTR_PSE  = (1 << 1) // PIO Setup FIS received
	AHCI_PORT_INTR_DHRE = (1 << 0) // D2H Register FIS received

	AHCI_PORT_INTR_DEFAULT = AHCI_PORT_INTR_DPE | AHCI_PORT_INTR_SDBE |
		AHCI_PORT_INTR_DSE | AHCI_PORT_INTR_PSE |
		AHCI_PORT_INTR_DHRE

	AHCI_CMD_FLAGS_WRITE uint16 = (1 << 6)

	SATA_FIS_TYPE_REG_H2D uint8 = 0x27
	SATA_FIS_TYPE_REG_D2H uint8 = 0x34
	SATA_FIS_REG_CFLAG    uint8 = (1 << 7) // issuing new command

	IDE_CMD_READ_DMA_EXT    uint8 = 0x25
	IDE_CMD_WRITE_DMA_EXT   uint8 = 0x35
	IDE_CMD_FLUSH_CACHE_EXT       = 0xea
	IDE_CMD_IDENTIFY        uint8 = 0xec
	IDE_CMD_SETFEATURES     uint8 = 0xef

	IDE_DEV_LBA   = 0x40
	IDE_CTL_LBA48 = 0x80

	IDE_FEATURE86_LBA48 uint16 = (1 << 10)
	IDE_STAT_BSY        uint32 = 0x80

	IDE_SATA_NCQ_SUPPORTED   = (1 << 8)
	IDE_SATA_NCQ_QUEUE_DEPTH = 0x1f

	IDE_FEATURE_WCACHE_ENA = 0x02
	IDE_FEATURE_RLA_ENA    = 0xAA
)

func LD(f *uint32) uint32 {
	return atomic.LoadUint32(f)
}

func LD64(f *uint64) uint64 {
	return atomic.LoadUint64(f)
}

func LD32(f *uint32) uint32 {
	return atomic.LoadUint32(f)
}

func ST(f *uint32, v uint32) {
	// Serial ATA AHCI 1.3.1 spec, section 3: "locked access are not
	// supported...indeterminite results may occur"
	//atomic.StoreUint32(f, v)
	runtime.Store32(f, v)
}

func ST16(f *uint16, v uint16) {
	a := (*uint32)(unsafe.Pointer(f))
	v32 := LD(a)
	ST(a, (v32&0xFFFF0000)|uint32(v))
}

func LD16(f *uint16) uint16 {
	a := (*uint32)(unsafe.Pointer(f))
	v := LD(a)
	return uint16(v & 0xFFFF)
}

func ST64(f *uint64, v uint64) {
	//atomic.StoreUint64(f, v)
	runtime.Store64(f, v)
}

func SET(f *uint32, v uint32) {
	runtime.Store32(f, LD(f)|v)
}

func SET16(f *uint16, v uint16) {
	ST16(f, LD16(f)|v)
}

func CLR16(f *uint16, v uint16) {
	n := LD16(f) & ^v
	ST16(f, n)
}

func CLR(f *uint32, v uint32) {
	v32 := LD(f)
	n := v32 & ^v
	runtime.Store32(f, n)
}

func (p *ahci_port_t) pg_new() (*mem.Pg_t, mem.Pa_t) {
	a, b, ok := mem.Physmem.Refpg_new()
	if !ok {
		panic("oom during port pg_new")
	}
	mem.Physmem.Refup(b)
	return a, b
}

func (p *ahci_port_t) pg_free(pa mem.Pa_t) {
	mem.Physmem.Refdown(pa)
}

func (p *ahci_port_t) init() bool {
	if LD(&p.port.ssts)&0x0F != HBD_PORT_DET_PRESENT {
		return false
	}
	if (LD(&p.port.ssts)>>8)&0x0F != HBD_PORT_IPM_ACTIVE {
		return false
	}

	// Only SATA drives
	if LD(&p.port.sig) != SATA_SIG_ATA {
		return false
	}

	// Wait for port to quiesce:
	if LD(&p.port.cmd)&(AHCI_PORT_CMD_ST|AHCI_PORT_CMD_CR|
		AHCI_PORT_CMD_FRE|AHCI_PORT_CMD_FR) != 0 {

		CLR(&p.port.cmd, AHCI_PORT_CMD_ST|AHCI_PORT_CMD_FRE)

		dbg("AHCI: port active, clearing ..\n")

		c := 0
		for LD(&p.port.cmd)&(AHCI_PORT_CMD_CR|AHCI_PORT_CMD_FR) != 0 {
			c++
			// XXX longer ...
			if c > 10000 {
				fmt.Printf("AHCI: port still active, giving up\n")
				return false
			}
		}
	}

	// Allocate memory for rfis
	_, pa := p.pg_new()
	if int(unsafe.Sizeof(*p.rfis)) > mem.PGSIZE {
		panic("not enough mem for rfis")
	}
	p.rfis_pa = uintptr(pa)

	// Allocate memory for cmdh
	_, pa = p.pg_new()
	if int(unsafe.Sizeof(*p.cmdh)) > mem.PGSIZE {
		panic("not enough mem for cmdh")
	}
	p.cmdh_pa = uintptr(pa)
	p.cmdh = (*[32]ahci_cmd_header)(unsafe.Pointer(mem.Physmem.Dmap(pa)))

	// Allocate memory for cmdt, which spans several physical pages that
	// must be consecutive. pg_new() returns physical pages during boot
	// consecutively (in increasing order).
	n := int(unsafe.Sizeof(*p.cmdt))/mem.PGSIZE + 1
	dbg("AHCI: size cmdt %v pages %v\n", unsafe.Sizeof(*p.cmdt), n)
	_, pa = p.pg_new()
	pa1 := pa
	for i := 1; i < n; i++ {
		_, pa1 = p.pg_new()
		if int(pa1-pa) != mem.PGSIZE*i {
			panic("AHCI: port init phys page not in order")
		}
	}
	p.cmdt_pa = uintptr(pa)
	p.cmdt = (*[32]ahci_cmd_table)(unsafe.Pointer(mem.Physmem.Dmap(pa)))

	// Initialize memory buffers
	for cmdslot, _ := range p.cmdh {
		v := &p.cmdt[cmdslot]
		pa := mem.Physmem.Dmap_v2p((*mem.Pg_t)(unsafe.Pointer(v)))
		if pa & ((1 << 7) - 1) != 0 {
			panic("not 128 byte aligned")
		}
		p.cmdh[cmdslot].ctba = (uint64)(pa)
	}

	ST64(&p.port.clb, uint64(p.cmdh_pa))
	ST64(&p.port.fb, uint64(p.rfis_pa))

	ST(&p.port.ci, 0)
	ST(&p.port.sact, 0)

	// Clear any errors first, otherwise the chip wedges
	CLR(&p.port.serr, 0xFFFFFFFF)
	ST(&p.port.serr, 0)

	SET(&p.port.cmd, AHCI_PORT_CMD_FRE|AHCI_PORT_CMD_ST|
		AHCI_PORT_CMD_SUD|AHCI_PORT_CMD_POD|
		AHCI_PORT_CMD_ACTIVE)

	phystat := LD(&p.port.ssts)
	if phystat == 0 {
		fmt.Printf("AHCI: port not connected\n")
		return false
	}

	// Allocate memory for holding a sector
	// XXX it would be much better if we can pass the pa of an Go object
	// allocated by the kernel
	for i, _ := range p.cmdh {
		_, pa = p.pg_new()
		p.block_pa[i] = uintptr(pa)
		p.block[i] = (*[512]uint8)(unsafe.Pointer(mem.Physmem.Dmap(pa)))
	}

	return true
}

func (p *ahci_port_stat_t) stat() string {
	s := "ahci:" + stats.Stats2String(*p)
	*p = ahci_port_stat_t{}
	return s
}

func swap(info []uint8) []uint8 {
	for i := 0; i < len(info); i += 2 {
		c := info[i]
		info[i] = info[i+1]
		info[i+1] = c

	}
	return info
}

func (p *ahci_port_t) identify() (*identify_device_t, *string, bool) {
	fis := &sata_fis_reg_h2d{}
	fis.fis_type = SATA_FIS_TYPE_REG_H2D
	fis.cflag = SATA_FIS_REG_CFLAG
	fis.command = IDE_CMD_IDENTIFY
	fis.sector_count = 1

	// To receive the identity
	b := fs.MkBlock_newpage(-1, "identify", Blockmem, Ahci, nil)
	p.fill_prd(0, b)
	p.fill_fis(0, fis)

	ST(&p.port.ci, uint32(1))

	if !p.wait(0) {
		fmt.Printf("AHCI: timeout waiting for identity\n")
		return nil, nil, false
	}

	id := (*identify_device_t)(unsafe.Pointer(mem.Physmem.Dmap(b.Pa)))
	if (id.features87 >> 14) != 1 {
		panic("ATA features87 invalid")
	}
	dbg("words 82-83 valid: %v\n", (id.features83 >> 14) == 1)
	dbg("words 85-87 valid: %v\n", (id.features87 >> 14) == 1)
	dbg("words 119 valid:   %v\n", (id.features119 >> 14) == 1)
	dbg("features 83 : %#x, 48-bit lba: %v\n", id.features83,
	    (id.features83 & (1 << 10)) != 0)
	if LD16(&id.features86)&IDE_FEATURE86_LBA48 == 0 {
		fmt.Printf("AHCI: disk too small, driver requires LBA48\n")
		return nil, nil, false
	}

	ret_id := &identify_device_t{}
	*ret_id = *id

	p.pg_free(b.Pa)

	m := swap(id.model[:])
	s := string(m)

	return ret_id, &s, true
}

func (p *ahci_port_t) enable_write_cache() bool {
	fis := &sata_fis_reg_h2d{}
	fis.fis_type = SATA_FIS_TYPE_REG_H2D
	fis.cflag = SATA_FIS_REG_CFLAG
	fis.command = IDE_CMD_SETFEATURES
	fis.features = IDE_FEATURE_WCACHE_ENA

	p.fill_prd(0, nil)
	p.fill_fis(0, fis)

	ST(&p.port.ci, uint32(1))

	if !p.wait(0) {
		fmt.Printf("AHCI: timeout waiting for write_cache\n")
		return false
	}
	return true
}

func (p *ahci_port_t) enable_read_ahead() bool {
	fis := &sata_fis_reg_h2d{}
	fis.fis_type = SATA_FIS_TYPE_REG_H2D
	fis.cflag = SATA_FIS_REG_CFLAG
	fis.command = IDE_CMD_SETFEATURES
	fis.features = IDE_FEATURE_RLA_ENA

	p.fill_prd(0, nil)
	p.fill_fis(0, fis)

	ST(&p.port.ci, uint32(1))

	if !p.wait(0) {
		fmt.Printf("AHCI: timeout waiting for read_ahead\n")
		return false
	}
	return true
}

func (p *ahci_port_t) wait(s uint32) bool {
	for c := 0; c < 100000; c++ {
		stat := LD(&p.port.tfd) & 0xff
		ci := LD(&p.port.ci) & (1 << s)
		sact := LD(&p.port.sact) & (1 << s)
		serr := LD(&p.port.serr)
		is := LD(&p.port.is)
		if stat&IDE_STAT_BSY == 0 && ci == 0 {
			return true
		}
		if c%10000 == 0 {
			fmt.Printf("AHCI: wait %v: stat %#x ci %#x sact %#x error %#x is %#x\n", s, stat&IDE_STAT_BSY, ci, sact, serr, is)
		}

	}
	return false
}

func (p *ahci_port_t) fill_fis(cmdslot int, fis *sata_fis_reg_h2d) {
	if unsafe.Sizeof(*fis) != 20 {
		panic("fill_fis: fis wrong length")
	}
	f := (*[5]uint32)(unsafe.Pointer(fis))
	for i := 0; i < len(f); i++ {
		ST(&p.cmdt[cmdslot].cfis[i], f[i])
	}
	ST16(&p.cmdh[cmdslot].flags, uint16(5))
	// dbg("AHCI: fis %#x\n", fis)
}

func (p *ahci_port_t) fill_prd_v(cmdslot int, blks *fs.BlkList_t) uint64 {
	nbytes := uint64(0)
	cmd := &p.cmdt[cmdslot]
	slot := 0
	for blk := blks.FrontBlock(); blk != nil; blk = blks.NextBlock() {
		if uint64(blk.Pa) & 1 != 0 {
			panic("whut")
		}
		ST64(&cmd.prdt[slot].dba, uint64(blk.Pa))
		l := len(blk.Data)
		if l != fs.BSIZE {
			panic("fill_prd_v")
		}
		ST(&cmd.prdt[slot].dbc, uint32(l-1))

		// 4.2.3.3: Setting 1<<31 will generate an interrupt for when
		// the data in slot slot has been transferred, which results in
		// a large number of interrupts for big transfers.
		// SET(&cmd.prdt[slot].dbc, 1<<31)

		nbytes += uint64(l)
		slot++
	}
	ST16(&p.cmdh[cmdslot].prdtl, uint16(blks.Len()))
	ST(&p.cmdh[cmdslot].prdbc, 0)
	return nbytes
}

func (p *ahci_port_t) fill_prd(cmdslot int, b *fs.Bdev_block_t) {
	bl := fs.MkBlkList()
	if b != nil {
		bl.PushBack(b)
	}
	p.fill_prd_v(cmdslot, bl)
}

func (p *ahci_port_t) find_slot() (int, bool) {
	all_scanned := false
	for s := p.next_slot; s < p.nslot; {
		ci := LD(&p.port.ci)
		if p.inflight[s] == nil &&
			ci&uint32(1<<s) == uint32(0) {
			p.next_slot = (p.next_slot + 1) % p.nslot
			return int(s), true
		}
		if s == p.nslot-1 && !all_scanned {
			s = 0
			all_scanned = true
		} else {
			s++
		}
	}
	return 0, false
}

func (p *ahci_port_t) queuemgr() {
	defer p.Unlock()
	p.Lock()
	for {
		ok := false
		if p.queued.Len() > 0 {
			s, ok := p.find_slot()
			if ok {
				e := p.queued.Front()
				p.queued.Remove(e)
				req := e.Value.(*fs.Bdev_req_t)
				p.startslot(req, s)
			}
		}
		if p.queued.Len() == 0 || !ok {
			dbg("queuemgr: go to sleep: %v %v\n", p.queued.Len(), ok)
			p.cond_queued.Wait()
		}
	}
}

func (p *ahci_port_t) queue_coalesce(req *fs.Bdev_req_t) {
	ok := false
	for e := p.queued.Front(); e != nil; e = e.Next() {
		r := e.Value.(*fs.Bdev_req_t)
		if r.Cmd == fs.BDEV_FLUSH || req.Cmd == fs.BDEV_FLUSH {
			break
		}
		if r.Blks.Len() == 0 {
			panic("queue_coalesce")
		}
		if r.Cmd == req.Cmd { // combine reads with reads, and writes with writes
			last := r.Blks.BackBlock()
			first := req.Blks.FrontBlock()
			if first.Block == last.Block+1 {
				dbg("collapse %d %d %d\n", first.Block, last.Block, r.Blks.Len())
				p.stat.Ncoalesce++
				r.Blks.Append(req.Blks)
				ok = true
				break
			}
		}
	}

	if !ok {
		p.queued.PushBack(req)
	}
}

func (p *ahci_port_t) start(req *fs.Bdev_req_t) {
	defer p.Unlock()
	p.Lock()

	// Flush waits until outstanding commands have finished and then flushes
	// the non-volatile cache of the storage device.  XXX It might be better
	// to have a just a barrier operation (i.e., not flushing non-volatile
	// cache), and tag writes with FUA for writes that need to persist
	// immediately (instead of flusing the complete on-disk cache).
	for req.Cmd == fs.BDEV_FLUSH {
		ci := LD(&p.port.ci)
		sact := LD(&p.port.sact)
		if ci == 0 { // && sact == 0 {
			break
		} else {
			dbg("flush: slots in progress %#x %#x\n", ci, sact)
			p.nflush++
			p.cond_flush.Wait()
			p.nflush--
		}
	}

	if req.Cmd == fs.BDEV_WRITE {
		p.stat.Nwrite++
	}

	if p.queued.Len() > 0 {
		p.queue_coalesce(req)
		p.stat.Nnoslot++
		return
	}

	// Find slot; if none is available, return
	s, ok := p.find_slot()
	if !ok {
		dbg("AHCI start: queue for slot\n")
		p.queued.PushBack(req)
		p.stat.Nnoslot++
		return
	}
	p.startslot(req, s)
}

func (p *ahci_port_t) startslot(req *fs.Bdev_req_t, s int) {
	switch req.Cmd {
	case fs.BDEV_WRITE:
		if req.Blks.Len() > 1 {
			p.stat.Nvwrite++
		} else {
			p.stat.Niwrite++
		}
		p.issue(s, req.Blks, IDE_CMD_WRITE_DMA_EXT)
	case fs.BDEV_READ:
		p.stat.Nread++
		p.issue(s, req.Blks, IDE_CMD_READ_DMA_EXT)
	case fs.BDEV_FLUSH:
		p.stat.Nbarrier++
		p.issue(s, nil, IDE_CMD_FLUSH_CACHE_EXT)
	}
	p.inflight[s] = req
	dbg("AHCI start: issued slot %v req %v sync %v ci %#x\n",
	    s, req.Cmd, req.Sync, LD(&p.port.ci))
}

// blks must be contiguous on disk (but not necessarily in memory)
func (p *ahci_port_t) issue(s int, blks *fs.BlkList_t, cmd uint8) {
	fis := &sata_fis_reg_h2d{}
	fis.fis_type = SATA_FIS_TYPE_REG_H2D
	fis.cflag = SATA_FIS_REG_CFLAG
	fis.command = cmd

	len := uint64(0)
	if blks != nil {
		len = p.fill_prd_v(s, blks)
	}

	if len%512 != 0 {
		panic("ACHI: issue len not multiple of 512 ")
	}
	if len >= uint64(MAX_PRD_SIZE)*(uint64)(MAX_PRD_ENTRIES) {
		panic("ACHI: issue len too large")
	}
	nsector := len / 512
	ST(&p.cmdh[s].prdbc, 0)

	fis.dev_head = IDE_DEV_LBA
	fis.control = IDE_CTL_LBA48
	var bn uint64
	if blks == nil {
		bn = uint64(0)
	} else {
		bn = uint64(blks.FrontBlock().Block)
	}
	sector_offset := bn * uint64(fs.BSIZE/512)
	fis.lba_0 = uint8((sector_offset >> 0) & 0xff)
	fis.lba_1 = uint8((sector_offset >> 8) & 0xff)
	fis.lba_2 = uint8((sector_offset >> 16) & 0xff)
	fis.lba_3 = uint8((sector_offset >> 24) & 0xff)
	fis.lba_4 = uint8((sector_offset >> 32) & 0xff)
	fis.lba_5 = uint8((sector_offset >> 40) & 0xff)

	fis.sector_count = uint8(nsector & 0xff)
	fis.sector_count_ex = uint8((nsector >> 8) & 0xff)

	p.fill_fis(s, fis) // sets flags to length fis
	if cmd == IDE_CMD_WRITE_DMA_EXT {
		SET16(&p.cmdh[s].flags, AHCI_CMD_FLAGS_WRITE)
	}
	dbg("cmdh: prdtl %#x flags %#x bc %v\n", LD16(&p.cmdh[s].prdtl),
	    LD16(&p.cmdh[s].flags), LD(&p.cmdh[s].prdbc))

	// issue command
	ST(&p.port.ci, (1 << uint(s)))
}

// Clear interrupt status
func (ahci *ahci_disk_t) clear_is() {
	// AHCI 1.3, section 10.7.2.1 says we need to first clear the
	// port interrupt status and then clear the host interrupt
	// status.  It's fine to do this even after we've processed the
	// port interrupt: if any port interrupts happened in the mean
	// time, the host interrupt bit will just get set again. */
	SET(&ahci.ahci.is, (1 << uint32(ahci.portid)))
	dbg("clear_is: %v is %#x sact %#x gis %#x\n", ahci.portid,
	    LD(&ahci.port.port.is), LD(&ahci.port.port.sact), LD(&ahci.ahci.is))
}

func (ahci *ahci_disk_t) enable_interrupt() {
	ST(&ahci.port.port.ie, AHCI_PORT_INTR_DEFAULT)
	SET(&ahci.ahci.ghc, AHCI_GHC_IE)
	dbg("AHCI: interrupts enabled ghc %#x ie %#x\n",
		LD(&ahci.ahci.ghc)&0x2, LD(&ahci.port.port.ie))
}

func (ahci *ahci_disk_t) probe_port(pid int) bool {
	p := &ahci_port_t{}
	p.hba_id = pid
	p.cond_flush = sync.NewCond(p)
	p.cond_queued = sync.NewCond(p)
	port_mmio_len := 0x80
	a := ahci.bara + 0x100 + uintptr(pid*port_mmio_len)
	if unsafe.Sizeof(port_reg_t{}) > uintptr(port_mmio_len) {
		panic("port_reg_t larger than MMIO regs")
	}
	m := mem.Dmaplen32(uintptr(a), port_mmio_len)
	p.port = (*port_reg_t)(unsafe.Pointer(&(m[0])))
	if p.init() {
		dbg("AHCI SATA ATA port %v %#x\n", pid, p.port)
		ahci.port = p
		ahci.portid = pid
		id, m, ok := p.identify()
		if ok {
			ahci.model = *m
			ahci.nsectors = LD64(&id.lba48_sectors)
			dbg("AHCI: model %v sectors %#x\n", ahci.model, ahci.nsectors)
			if id.sata_caps&IDE_SATA_NCQ_SUPPORTED == 0 {
				fmt.Printf("AHCI: SATA Native Command Queuing not supported\n")
				return false
			}
			p.nslot = uint32(1 + (id.queue_depth & IDE_SATA_NCQ_QUEUE_DEPTH))
			dbg("AHCI: slots %v\n", p.nslot)
			if p.nslot < ahci.ncs {
				fmt.Printf("AHCI: NCQ queue depth limited to %d (out of %d)\n",
					p.nslot, ahci.ncs)
			}
			p.inflight = make([]*fs.Bdev_req_t, p.nslot)
			p.queued = list.New()
			_ = p.enable_write_cache()
			_ = p.enable_read_ahead()
			id, _, _ = p.identify()
			dbg("AHCI: write cache %v read ahead %v\n",
				LD16(&id.features85)&(1<<5) != 0,
				LD16(&id.features85)&(1<<4) != 0)
			ahci.clear_is()
			ahci.enable_interrupt()
			go p.queuemgr()
			return true// only one port
		}
	}
	return false
}

func (p *ahci_port_t) port_intr(ahci *ahci_disk_t) {
	defer p.Unlock()
	p.Lock()

	ci := LD(&p.port.ci)
	int := false
	p.stat.Nintr++
	for s := uint(0); s < 32; s++ {
		if p.inflight[s] != nil && ci&(1<<s) == 0 {
			int = true
			dbg("port_intr: slot %v interrupt\n", s)
			if p.inflight[s].Cmd == fs.BDEV_WRITE {
				// page has been written, don't need a reference to it
				// and can be removed from cache.
				p.inflight[s].Blks.Apply(func(b *fs.Bdev_block_t) {
					b.Done("interrupt")
				})
			}
			if p.inflight[s].Sync {
				dbg("port_intr: ack inflight %v\n", s)
				// writing to channel while holding ahci lock, but should be ok
				p.inflight[s].AckCh <- true

			}
			p.inflight[s] = nil
			if p.queued.Len() > 0 {
				p.cond_queued.Signal()

			}
			if p.nflush > 0 && p.queued.Len() == 0 {
				dbg("port_intr: wakeup sync %v\n", s)
				p.cond_flush.Signal()
			}
		}
	}
	if !int {
		dbg("?")
	}
	ahci.clear_is()
}

func (ahci *ahci_disk_t) intr() {
	int := false
	is := LD(&ahci.ahci.is)
	for i := uint32(0); i < 32; i++ {
		if is&(1<<i) != 0 {
			if i != uint32(ahci.portid) {
				panic("intr: wrong port\n")
			}
			int = true

			// clear port interrupt. interrupts coming in while we are
			// processing will be deliver after clear_is().
			SET(&ahci.port.port.is, 0x1<<i)
			ahci.port.port_intr(ahci)
		}
	}
	if !int {
		dbg("!")
	}
}

// Go routine for handling interrupts
func (ahci *ahci_disk_t) int_handler(vec msi.Msivec_t) {
	dbg("AHCI: interrupt handler running\n")
	for {
		runtime.IRQsched(uint(vec))
		ahci.intr()
	}
}

func _chk(_p interface{}, off, base uintptr) {
	var p uintptr
	switch v := _p.(type) {
	case *uint16:
		p = uintptr(unsafe.Pointer(v))
	case *uint32:
		p = uintptr(unsafe.Pointer(v))
	case *uint64:
		p = uintptr(unsafe.Pointer(v))
	default:
		panic("bad ptr")
	}
	diff := p - base
	if diff != off {
		fmt.Printf("%#x - %#x (%d) != %d\n", p, base, diff, off)
		panic("struct padded?")
	}
}

func ahci_verify() {
	a := &ahci_reg_t{}
	chk := func(p interface{}, off uintptr) {
		base := uintptr(unsafe.Pointer(a))
		_chk(p, off, base)
	}
	chk(&a.cap2, 0x24)
}

func port_verify() {
	nport := &port_reg_t{}
	chk := func(p interface{}, off uintptr) {
		base := uintptr(unsafe.Pointer(nport))
		_chk(p, off, base)
	}
	chk(&nport.cmd, 0x18)
	chk(&nport.ssts, 0x28)
	chk(&nport.sctl, 0x2c)
	chk(&nport.serr, 0x30)
}

func ata_verify() {
	f := &identify_device_t{}
	chk := func(p interface{}, off uintptr) {
		base := uintptr(unsafe.Pointer(f))
		_chk(p, off, base)
	}
	chk(&f.features83, 83*2)
	chk(&f.features86, 86*2)
	chk(&f.features119, 119*2)
}

func Ahci_init() {
	ahci_verify()
	port_verify()
	ata_verify()
	pci.Pci_register_intel(pci.PCI_DEV_AHCI_BHW, attach_ahci)
	pci.Pci_register_intel(pci.PCI_DEV_AHCI_BHW2, attach_ahci)
	pci.Pci_register_intel(pci.PCI_DEV_AHCI_QEMU, attach_ahci)
}
