package main

import "fmt"
import "runtime"
import "sync"
import "sync/atomic"
import "unsafe"
import "strconv"
import "container/list"
import "reflect"

import "common"

const ahci_debug = false

//
// AHCI from sv6 from HiStar.
//
// Some useful docs:
// - http://wiki.osdev.org/AHCI
// - AHCI: https://www.intel.com/content/dam/www/public/us/en/documents/technical-specifications/serial-ata-ahci-spec-rev1-3-1.pdf
// - FIS: http://www.ece.umd.edu/courses/enee759h.S2003/references/serialata10a.pdf
// - CMD: http://www.t13.org/documents/uploadeddocuments/docs2007/d1699r4a-ata8-acs.pdf
//

var ahci common.Disk_i

type blockmem_t struct {
}

var blockmem = &blockmem_t{}

func (bm *blockmem_t) Alloc() (common.Pa_t, *common.Bytepg_t, bool) {
	_, pa, ok := physmem.Refpg_new()
	if ok {
		d := (*common.Bytepg_t)(unsafe.Pointer(physmem.Dmap(pa)))
		physmem.Refup(pa)
		return pa, d, ok
	} else {
		return pa, nil, ok
	}
}

func (bm *blockmem_t) Free(pa common.Pa_t) {
	physmem.Refdown(pa)
}

// returns true if start is asynchronous
func (ahci *ahci_disk_t) Start(req *common.Bdev_req_t) bool {
	ahci.port.start(req)
	return true
}

func (ahci *ahci_disk_t) Stats() string {
	if ahci == nil {
		panic("no adisk")
	}
	return ahci.port.stat()
}

func attach_ahci(vid, did int, t pcitag_t) {
	if disk != nil {
		panic("adding two disks")
	}

	d := &ahci_disk_t{}
	d.tag = t
	d.bara = pci_read(t, _BAR5, 4)
	fmt.Printf("attach AHCI disk %#x tag %#x\n", did, d.tag)
	m := common.Dmaplen32(uintptr(d.bara), int(unsafe.Sizeof(*d)))
	d.ahci = (*ahci_reg_t)(unsafe.Pointer(&(m[0])))

	vec := msivec_t(0)
	msicap := 0x80
	cap_entry := pci_read(d.tag, msicap, 4)
	if cap_entry&0x1F != 0x5 {
		fmt.Printf("AHCI: no MSI\n")
		IRQ_DISK = 11 // XXX pci_disk_interrupt_wiring(t) returns 23, but 11 works
		INT_DISK = common.IRQ_BASE + IRQ_DISK
	} else { // enable MSI interrupts
		vec = msi_alloc()

		fmt.Printf("AHCI: msicap %#x MSI to vec %#x\n", cap_entry, vec)

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
			pci_write(d.tag, msicap, cap_entry & ^(0x7<<20))
		}

		// [PCI SA pg 253] Assign a dword-aligned memory address to the
		// device's Message Address Register.  (The Message Address
		// Register format is mandated by the x86 architecture.  See
		// 9.11.1 in the Vol. 3 of the Intel architecture manual.)

		// Non-remapped ("compatibility format") interrupts
		pci_write(d.tag, msicap+4*1,
			(0x0fee<<20)| // magic constant for northbridge
				(bsp_apic_id<<12)| // destination ID
				(1<<3)| // redirection hint
				(0<<2)) // destination mode

		if is_64bit {
			// Zero out the most-significant 32-bits of the Message Address Register,
			// which is at Dword 2 for 64-bit devices.
			pci_write(d.tag, msicap+4*2, 0)
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
		pci_write(d.tag, msicap+4*offset,
			(0<<15)| // trigger mode (edge)
				//(0 << 14) |      // level for trigger mode (don't care)
				(0<<8)| // delivery mode (fixed)
				int(vec)) // vector

		// Set the MSI enable bit in the device's Message control
		// register.
		pci_write(d.tag, msicap, cap_entry|(1<<16))

		msimask := 0x60
		if pci_read(d.tag, msimask, 4)&1 != 0 {
			panic("msi pci masked")
		}

	}

	SET(&d.ahci.ghc, AHCI_GHC_AE)

	d.ncs = ((LD(&d.ahci.cap) >> 8) & 0x1f) + 1
	fmt.Printf("AHCI: ahci %#x ncs %#x\n", d.ahci, d.ncs)

	for i := 0; i < 32; i++ {
		if LD(&d.ahci.pi)&(1<<uint32(i)) != 0x0 {
			d.probe_port(i)
		}
	}

	go d.int_handler(vec)
	ahci = d
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
	bara     int
	model    string
	ahci     *ahci_reg_t
	tag      pcitag_t
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

type ahci_port_t struct {
	sync.Mutex
	cond_flush  *sync.Cond
	cond_queued *sync.Cond

	port      *port_reg_t
	nslot     uint32
	next_slot uint32
	inflight  []*common.Bdev_req_t
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

	// stats
	nbarrier  int
	nwrite    int
	nvwrite   int
	nread     int
	nnoslot   int
	ncoalesce int
}

type identify_device struct {
	pad0          [10]uint16 // Words 0-9
	serial        [20]uint8  // Words 10-19
	pad1          [3]uint16  // Words 20-22
	firmware      [8]uint8   // Words 23-26
	model         [40]uint8  // Words 27-46
	pad2          [13]uint16 // Words 47-59
	lba_sectors   uint32     // Words 60-61, assuming little-endian
	pad3          [13]uint16 // Words 62-74
	queue_depth   uint16     // Word 75
	sata_caps     uint16     // Word 76
	pad4          [8]uint16  // Words 77-84
	features85    uint16     // Word 85
	features86    uint16     // Word 86
	features87    uint16     // Word 87
	udma_mode     uint16     // Word 88
	pad5          [4]uint16  // Words 89-92
	hwreset       uint16     // Word 93
	pad6          [6]uint16  // Words 94-99
	lba48_sectors uint64     // Words 100-104, assuming little-endian
}

const (
	PCI_MSI_MCR_64BIT = 0x00800000

	HBD_PORT_IPM_ACTIVE  uint32 = 1
	HBD_PORT_DET_PRESENT uint32 = 3

	SATA_SIG_ATA = 0x00000101 // SATA drive

	AHCI_GHC_AE uint32 = (1 << 31) // Use AHCI to communicat
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
	atomic.StoreUint32(f, v)
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
	atomic.StoreUint64(f, v)
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

func (p *ahci_port_t) pg_new() (*common.Pg_t, common.Pa_t) {
	a, b, ok := physmem.Refpg_new()
	if !ok {
		panic("oom during port pg_new")
	}
	physmem.Refup(b)
	return a, b
}

func (p *ahci_port_t) pg_free(pa common.Pa_t) {
	physmem.Refdown(pa)
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

		fmt.Printf("AHCI: port active, clearing ..\n")

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
	if int(unsafe.Sizeof(*p.rfis)) > common.PGSIZE {
		panic("not enough mem for rfis")
	}
	p.rfis_pa = uintptr(pa)

	// Allocate memory for cmdh
	_, pa = p.pg_new()
	if int(unsafe.Sizeof(*p.cmdh)) > common.PGSIZE {
		panic("not enough mem for cmdh")
	}
	p.cmdh_pa = uintptr(pa)
	p.cmdh = (*[32]ahci_cmd_header)(unsafe.Pointer(physmem.Dmap(pa)))

	// Allocate memory for cmdt, which spans several physical pages that
	// must be consecutive. pg_new() returns physical pages during boot
	// consecutively (in increasing order).
	n := int(unsafe.Sizeof(*p.cmdt))/common.PGSIZE + 1
	fmt.Printf("AHCI: size cmdt %v pages %v\n", unsafe.Sizeof(*p.cmdt), n)
	_, pa = p.pg_new()
	pa1 := pa
	for i := 1; i < n; i++ {
		_, pa1 = p.pg_new()
		if int(pa1-pa) != common.PGSIZE*i {
			panic("AHCI: port init phys page not in order")
		}
	}
	p.cmdt_pa = uintptr(pa)
	p.cmdt = (*[32]ahci_cmd_table)(unsafe.Pointer(physmem.Dmap(pa)))

	// Initialize memory buffers
	for cmdslot, _ := range p.cmdh {
		v := &p.cmdt[cmdslot]
		pa := common.Dmap_v2p((*common.Pg_t)(unsafe.Pointer(v)))
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
		p.block[i] = (*[512]uint8)(unsafe.Pointer(physmem.Dmap(pa)))
	}

	return true
}

func (p *ahci_port_t) stat() string {
	s := "ahci:"
	s += " #flush "
	s += strconv.Itoa(p.nbarrier)
	s += " #read "
	s += strconv.Itoa(p.nread)
	s += " #write "
	s += strconv.Itoa(p.nwrite)
	s += " #vwrite "
	s += strconv.Itoa(p.nvwrite)
	s += " #noslot "
	s += strconv.Itoa(p.nnoslot)
	s += " #ncoalesce "
	s += strconv.Itoa(p.ncoalesce)
	s += "\n"
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

func (p *ahci_port_t) identify() (*identify_device, *string, bool) {
	fis := &sata_fis_reg_h2d{}
	fis.fis_type = SATA_FIS_TYPE_REG_H2D
	fis.cflag = SATA_FIS_REG_CFLAG
	fis.command = IDE_CMD_IDENTIFY
	fis.sector_count = 1

	// To receive the identity
	b := common.MkBlock_newpage(-1, "identify", blockmem, ahci, nil)
	p.fill_prd(0, b)
	p.fill_fis(0, fis)

	ST(&p.port.ci, uint32(1))

	if !p.wait(0) {
		fmt.Printf("AHCI: timeout waiting for identity\n")
		return nil, nil, false
	}

	id := (*identify_device)(unsafe.Pointer(physmem.Dmap(b.Pa)))
	if LD16(&id.features86)&IDE_FEATURE86_LBA48 == 0 {
		fmt.Printf("AHCI: disk too small, driver requires LBA48\n")
		return nil, nil, false
	}

	ret_id := &identify_device{}
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
	// fmt.Printf("AHCI: fis %#x\n", fis)
}

func (p *ahci_port_t) fill_prd_v(cmdslot int, blks *list.List) uint64 {
	nbytes := uint64(0)
	cmd := &p.cmdt[cmdslot]
	slot := 0
	for e := blks.Front(); e != nil; e = e.Next() {
		blk := e.Value.(*common.Bdev_block_t)
		ST64(&cmd.prdt[slot].dba, uint64(blk.Pa))
		l := len(blk.Data)
		if l != BSIZE {
			panic("fill_prd_v")
		}
		ST(&cmd.prdt[slot].dbc, uint32(l-1))
		SET(&cmd.prdt[slot].dbc, 1<<31)
		nbytes += uint64(l)
		slot++
	}
	ST16(&p.cmdh[cmdslot].prdtl, uint16(blks.Len()))
	return nbytes
}

func (p *ahci_port_t) fill_prd(cmdslot int, b *common.Bdev_block_t) {
	l := list.New()
	if b != nil {
		l.PushBack(b)
	}
	p.fill_prd_v(cmdslot, l)
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
				req := e.Value.(*common.Bdev_req_t)
				p.startslot(req, s)
			}
		}
		if p.queued.Len() == 0 || !ok {
			if ahci_debug {
				fmt.Printf("queuemgr: go to sleep: %v %v\n", p.queued.Len(), ok)
			}
			p.cond_queued.Wait()
		}
	}
}

func (p *ahci_port_t) queue_coalesce(req *common.Bdev_req_t) {
	ok := false
	for e := p.queued.Front(); e != nil; e = e.Next() {
		xx := &common.Bdev_req_t{}
		if reflect.TypeOf(e.Value) != reflect.TypeOf(xx) {
			fmt.Printf("v %v\n", reflect.TypeOf(e.Value))
		}
		r := e.Value.(*common.Bdev_req_t)
		if r.Blks.Len() == 0 {
			continue
		}
		elast := r.Blks.Back()
		last := elast.Value.(*common.Bdev_block_t)
		if req.Blks != nil {
			efirst := req.Blks.Front()
			first := efirst.Value.(*common.Bdev_block_t)
			if first.Block == last.Block+1 {
				if ahci_debug {
					fmt.Printf("collapse %d %d %d\n", first.Block, last.Block, r.Blks.Len())
				}
				p.ncoalesce++
				for f := req.Blks.Front(); f != nil; f = f.Next() {
					r.Blks.PushBack(f.Value)
				}
				ok = true
				break
			}
		}
	}
	if !ok {
		p.queued.PushBack(req)
	}
}

func (p *ahci_port_t) start(req *common.Bdev_req_t) {
	defer p.Unlock()
	p.Lock()

	// Flush must wait until outstanding commands have finished
	// XXX should support FUA in writes?
	for req.Cmd == common.BDEV_FLUSH {
		ci := LD(&p.port.ci)
		sact := LD(&p.port.sact)
		if ci == 0 { // && sact == 0 {
			break
		} else {
			if ahci_debug {
				fmt.Printf("flush: slots in progress %#x %#x\n", ci, sact)
			}
			p.nflush++
			p.cond_flush.Wait()
			p.nflush--
		}
	}

	if p.queued.Len() > 0 {
		p.queue_coalesce(req)
		p.nnoslot++
		return
	}

	// Find slot; if none is available, return
	s, ok := p.find_slot()
	if !ok {
		if ahci_debug {
			fmt.Printf("AHCI start: queue for slot\n")
		}
		p.queued.PushBack(req)
		p.nnoslot++
		return
	}
	p.startslot(req, s)
}

func (p *ahci_port_t) startslot(req *common.Bdev_req_t, s int) {
	switch req.Cmd {
	case common.BDEV_WRITE:
		p.nwrite++
		p.issue(s, req.Blks, IDE_CMD_WRITE_DMA_EXT)
	case common.BDEV_READ:
		p.nread++
		p.issue(s, req.Blks, IDE_CMD_READ_DMA_EXT)
	case common.BDEV_FLUSH:
		p.nbarrier++
		p.issue(s, nil, IDE_CMD_FLUSH_CACHE_EXT)
	}
	p.inflight[s] = req
	if ahci_debug {
		fmt.Printf("AHCI start: issued slot %v req %v sync %v ci %#x\n",
			s, req.Cmd, req.Sync, LD(&p.port.ci))
	}
}

// blks must be contiguous on disk (but not necessarily in memory)
func (p *ahci_port_t) issue(s int, blks *list.List, cmd uint8) {
	fis := &sata_fis_reg_h2d{}
	fis.fis_type = SATA_FIS_TYPE_REG_H2D
	fis.cflag = SATA_FIS_REG_CFLAG
	fis.command = cmd

	len := uint64(0)
	if blks != nil {
		if blks.Len() > 1 {
			p.nvwrite++
		}
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
		e := blks.Front()
		blk := e.Value.(*common.Bdev_block_t)
		bn = uint64(blk.Block)
	}
	sector_offset := bn * uint64(BSIZE/512)
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
	if ahci_debug {
		fmt.Printf("cmdh: prdtl %#x flags %#x bc %v\n", LD16(&p.cmdh[s].prdtl),
			LD16(&p.cmdh[s].flags), LD(&p.cmdh[s].prdbc))
	}

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
	if ahci_debug {
		fmt.Printf("clear_is: %v is %#x sact %#x gis %#x\n", ahci.portid,
			LD(&ahci.port.port.is), LD(&ahci.port.port.sact),
			LD(&ahci.ahci.is))
	}
}

func (ahci *ahci_disk_t) enable_interrupt() {
	ST(&ahci.port.port.ie, AHCI_PORT_INTR_DEFAULT)
	SET(&ahci.ahci.ghc, AHCI_GHC_IE)
	fmt.Printf("AHCI: interrupts enabled ghc %#x ie %#x\n",
		LD(&ahci.ahci.ghc)&0x2, LD(&ahci.port.port.ie))
}

func (ahci *ahci_disk_t) probe_port(pid int) {
	p := &ahci_port_t{}
	p.cond_flush = sync.NewCond(p)
	p.cond_queued = sync.NewCond(p)
	a := ahci.bara + 0x100 + 0x80*pid
	m := common.Dmaplen32(uintptr(a), int(unsafe.Sizeof(*p)))
	p.port = (*port_reg_t)(unsafe.Pointer(&(m[0])))
	if p.init() {
		fmt.Printf("AHCI SATA ATA port %v %#x\n", pid, p.port)
		ahci.port = p
		ahci.portid = pid
		id, m, ok := p.identify()
		if ok {
			ahci.model = *m
			ahci.nsectors = LD64(&id.lba48_sectors)
			fmt.Printf("AHCI: model %v sectors %#x\n", ahci.model, ahci.nsectors)
			if id.sata_caps&IDE_SATA_NCQ_SUPPORTED == 0 {
				fmt.Printf("AHCI: SATA Native Command Queuing not supported\n")
				return
			}
			p.nslot = uint32(1 + (id.queue_depth & IDE_SATA_NCQ_QUEUE_DEPTH))
			fmt.Printf("AHCI: slots %v\n", p.nslot)
			if p.nslot < ahci.ncs {
				fmt.Printf("AHCI: NCQ queue depth limited to %d (out of %d)\n",
					p.nslot, ahci.ncs)
			}
			p.inflight = make([]*common.Bdev_req_t, p.nslot)
			p.queued = list.New()
			_ = p.enable_write_cache()
			_ = p.enable_read_ahead()
			id, _, _ = p.identify()
			fmt.Printf("AHCI: write cache %v read ahead %v\n",
				LD16(&id.features85)&(1<<5) != 0,
				LD16(&id.features85)&(1<<4) != 0)
			ahci.clear_is()
			ahci.enable_interrupt()
			go p.queuemgr()
			return // only one port
		}
	}
}

// Called by int_handler(), which holds lock through intr()
func (p *ahci_port_t) port_intr(ahci *ahci_disk_t) {
	defer p.Unlock()
	p.Lock()

	ci := LD(&p.port.ci)
	int := false
	for s := uint(0); s < 32; s++ {
		if p.inflight[s] != nil && ci&(1<<s) == 0 {
			int = true
			if ahci_debug {
				fmt.Printf("port_intr: slot %v interrupt\n", s)
			}
			if p.inflight[s].Cmd == common.BDEV_WRITE {
				// page has been written, don't need a reference to it
				// and can be removed from cache.
				for e := p.inflight[s].Blks.Front(); e != nil; e = e.Next() {
					blk := e.Value.(*common.Bdev_block_t)
					blk.Done("interrupt")
				}
			}
			if p.inflight[s].Sync {
				if ahci_debug {
					fmt.Printf("port_intr: ack inflight %v\n", s)
				}
				// writing to channel while holding ahci lock, but should be ok
				p.inflight[s].AckCh <- true

			}
			p.inflight[s] = nil
			if p.queued.Len() > 0 {
				p.cond_queued.Signal()

			}
			if p.nflush > 0 && p.queued.Len() == 0 {
				if ahci_debug {
					fmt.Printf("port_intr: wakeup sync %v\n", s)
				}
				p.cond_flush.Signal()
			}
		}
	}
	if !int && ahci_debug {
		fmt.Printf("?")
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
			SET(&ahci.port.port.is, 0xFFFFFFFF)
			ahci.port.port_intr(ahci)
			// ahci.clear_is()
		}
	}
	if !int && ahci_debug {
		fmt.Printf("!")
	}
}

// Go routing for handling interrupts
func (ahci *ahci_disk_t) int_handler(vec msivec_t) {
	fmt.Printf("AHCI: interrupt handler running\n")
	for {
		runtime.IRQsched(uint(vec))
		ahci.intr()
	}
}
