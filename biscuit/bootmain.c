#include <types.h>
#include <x86.h>

/**********************************************************************
 * XXX: this is wrong
 * This a dirt simple boot loader, whose sole job is to boot
 * an ELF kernel image from the first IDE hard disk.
 *
 * DISK LAYOUT
 *  * This program(boot.S and main.c) is the bootloader.  It should
 *    be stored in the first sector of the disk.
 *
 *  * The 2nd sector onward holds the kernel image.
 *
 *  * The kernel image must be in ELF format.
 *
 * BOOT UP STEPS
 *  * when the CPU boots it loads the BIOS into memory and executes it
 *
 *  * the BIOS intializes devices, sets of the interrupt routines, and
 *    reads the first sector of the boot device(e.g., hard-drive)
 *    into memory and jumps to it.
 *
 *  * Assuming this boot loader is stored in the first sector of the
 *    hard-drive, this code takes over...
 *
 *  * control starts in boot.S -- which sets up protected mode,
 *    and a stack so C code then run, then calls bootmain()
 *
 *  * bootmain() in this file takes over, reads in the kernel and jumps to it.
 **********************************************************************/

void waitdisk(void);
void readsect(void *, uint32_t);

static void *allocphys(uint64_t *, uint64_t);
static void checkmach(void);
static uint32_t getpg(void);
static void ensure_empty(uint64_t *, uint64_t);
static uint32_t ensure_pg(uint64_t *, int);
static void mapone(uint64_t *, uint64_t, uint64_t);
static void memset(void *, char, uint64_t);
static void pancake(char *msg, uint64_t addr);
static uint64_t *pgdir_walk(uint64_t *, uint64_t, int);
static void pmsg(char *);
static void pnum(uint64_t);
static void putch(char);
static void readseg(uint64_t *, uint64_t, uint64_t, uint64_t);

struct Elf {
	uint32_t 	e_magic;
#define ELF_MAGIC 0x464C457FU	/* "\x7FELF" in little endian */
	uint8_t		e_ident[12];		/* Id bytes */
	uint16_t	e_type;			/* file type */
	uint16_t	e_machine;		/* machine type */
	uint32_t	e_version;		/* version number */
	uint64_t	e_entry;		/* entry point */
	uint64_t	e_phoff;		/* Program hdr offset */
	uint64_t	e_shoff;		/* Section hdr offset */
	uint32_t	e_flags;		/* Processor flags */
	uint16_t	e_ehsize;		/* sizeof ehdr */
	uint16_t	e_phentsize;		/* Program header entry size */
	uint16_t	e_phnum;		/* Number of program headers */
	uint16_t	e_shentsize;		/* Section header entry size */
	uint16_t	e_shnum;		/* Number of section headers */
	uint16_t	e_shstrndx;		/* String table index */
};

struct Proghdr {
	uint32_t	p_type;		/* entry type */
	uint32_t	p_flags;	/* flags */
	uint64_t	p_offset;	/* offset */
	uint64_t	p_va;		/* virtual address */
	uint64_t	p_pa;		/* physical address */
	uint64_t	p_filesz;	/* file size */
	uint64_t	p_memsz;	/* memory size */
	uint64_t	p_align;	/* memory & file alignment */
};

// # of sectors this code takes up; i set this after compiling and observing
// the size of the text
#define BOOTBLOCKS     6

#define SECTSIZE	512
#define ELFHDR		((struct Elf *) 0x10000) // scratch space
#define ALLOCSTART      0x100000 // where to start grabbing pages; this must be
				 // 32-bit addressable
#define	NEWSTACK	0x80000000 // VA for new stack

void
bootmain(void)
{
	// read 1st page off disk
	int i;
	for (i = 0; i < 8; i++)
		readsect((char *)ELFHDR + i*SECTSIZE, BOOTBLOCKS+i);

	checkmach();

	// is this a valid ELF?
	if (ELFHDR->e_magic != ELF_MAGIC)
		goto bad;

	// pgdir is always at ALLOCSTART
	uint64_t *pgdir = (uint64_t *)(uint32_t)getpg();

	// load each program segment (ignores ph flags)
	struct Proghdr *ph, *eph;
	ph = (struct Proghdr *)((uint8_t *) ELFHDR + ELFHDR->e_phoff);
	eph = ph + ELFHDR->e_phnum;
	for (; ph < eph; ph++) {
		// p_pa is the load address of this segment (as well
		// as the physical address)
		if (ph->p_type != 1)	// PT_LOAD
			continue;

		readseg(pgdir, ph->p_va, ph->p_memsz, ph->p_offset);

		// zero bss
		if (ph->p_filesz != ph->p_memsz) {
			void *addr = allocphys(pgdir, ph->p_va);
			memset(addr + ph->p_filesz, 0,
			    ph->p_memsz - ph->p_filesz);
		}
	}

	// map the bootloader; this also maps our stack
	for (i = 0; i < BOOTBLOCKS; i++) {
		uint64_t addr = ROUNDDOWN(0x7c00 + i*SECTSIZE, PGSIZE);
		mapone(pgdir, addr, addr);
	}

	// give us VGA so we can print
	mapone(pgdir, 0xb8000, 0xb8000);
	mapone(pgdir, 0xb9000, 0xb9000);

	// get a new stack with guard page
	ensure_empty(pgdir, NEWSTACK - PGSIZE);
	ensure_empty(pgdir, NEWSTACK - 2*PGSIZE);
	allocphys(pgdir, NEWSTACK - 1);

	// XXX setup tramp if entry is 64bit address...
	if (ELFHDR->e_entry & ~((1ULL << 32) - 1))
		pancake("fixme: entry is 64 bit!", ELFHDR->e_entry);

	// goodbye, elf header
	uint32_t entry = (uint32_t)ELFHDR->e_entry;

	// goodbye, zeroing physical pages in getpg()
	uint32_t firstfree = getpg();

	// enter long mode
	enable_pae_wp();
	lcr3(pgdir);
	uint64_t efer = rdmsr(IA32_EFER);
	wrmsr(IA32_EFER, efer | IA32_EFER_LME);
	enable_paging();

	// goto ELF town
#define CODE64    3
	ljmp(CODE64, entry, (uint32_t)pgdir, firstfree, NEWSTACK);

bad:
	outw(0x8A00, 0x8A00);
	outw(0x8A00, 0x8E00);
	while (1)
		/* do nothing */;
}

static void *
allocphys(uint64_t *pgdir, uint64_t va)
{
	if (va & ~((1ULL << 32) - 1))
		pancake("va too large for poor ol' 32-bit me", va);

	uint64_t *pte = pgdir_walk(pgdir, va, 1);
	uint32_t ma = ensure_pg(pte, 1);

	return (void *)(ma | ((uint32_t)va & PGOFFMASK));
}

static void
checkmach(void)
{
	uint32_t eax, edx;
	cpuid(0x80000001, &eax, &edx);
	if ((edx & (1UL << 29)) == 0)
		pancake("not a 64 bit machine?", edx);
}

// XXX many assumptions about memory layout
static uint32_t
getpg(void)
{
	static uint64_t last = ALLOCSTART;

	uint64_t ret = last;
	last += PGSIZE;

	if (ret & PGOFFMASK)
		pancake("not aligned", ret);

	if (ret >= 0x00f00000 && ret < 0x01000000) {
		ret = 0x01000000;
		last = ret + PGSIZE;
	}

	if (ret >= 0xc0000000)
		pancake("oom?", ret);

	void *p = (void *)(uint32_t)ret;
	memset(p, 0, PGSIZE);

	return ret;
}

static uint32_t
ensure_pg(uint64_t *entry, int create)
{
	if (!(*entry & PTE_P)) {
		if (!create)
			return 0;

		*entry = getpg() | PTE_P | PTE_W;
	}

	return PTE_ADDR(*entry);
}

__attribute__((unused))
static void
ensure_empty(uint64_t *pgdir, uint64_t va)
{
	uint64_t *pte = pgdir_walk(pgdir, va, 0);

	if (pte == 0)
		return;

	if (*pte & PTE_P)
		pancake("page is mapped", va);
}

// Read 'count' bytes at 'offset' from kernel and map to virtual address 'va'.
// Might copy more than asked
static void
readseg(uint64_t *pgdir, uint64_t va, uint64_t count, uint64_t offset)
{
	uint64_t end_va;

	end_va = va + count;

	// round down to sector boundary
	va &= ~(SECTSIZE - 1);

	// translate from bytes to sectors, and kernel starts at sector
	// "BOOTBLOCKS"
	offset = (offset / SECTSIZE) + BOOTBLOCKS;

	// If this is too slow, we could read lots of sectors at a time.
	// We'd write more to memory than asked, but it doesn't matter --
	// we load in increasing order.
	while (va < end_va) {
		void *pa = allocphys(pgdir, va);
		readsect(pa, offset);
		va += SECTSIZE;
		offset++;
	}
}

static void
memset(void *p, char c, uint64_t sz)
{
	char *np = (char *)p;
	while (sz--)
		*np++ = c;
}

static void
putch(char mark)
{
        static uint8_t x;
        static uint8_t y;

        uint16_t *cons = (uint16_t *)0xb8000;

        cons[y*80 + x++] = (0x07 << 8) | mark;

        if (x >= 79) {
                x = 0;
                y++;
        }

	if (y >= 29)
		y = 0;
}

__attribute__((unused))
static void
pnum(uint64_t n)
{
	uint64_t nn = (uint64_t)n;
	int i;

	for (i = 60; i >= 0; i -= 4) {
		uint64_t cn = (nn >> i) & 0xf;

		if (cn >= 0 && cn <= 9)
			putch('0' + cn);
		else
			putch('A' + cn - 10);
	}
	putch(' ');
}

__attribute__((unused))
static void
pmsg(char *msg)
{
	while (*msg)
		putch(*msg++);
}

static void
pancake(char *msg, uint64_t addr)
{
	putch(' ');

	pmsg(msg);

	putch(' ');
	pnum(addr);
	pmsg(" PANCAKE");
	while (1);
}

static uint64_t *
pgdir_walk(uint64_t *pgdir, uint64_t va, int create)
{
	uint64_t *curpage;
	uint64_t *pml4e = &pgdir[PML4X(va)];
	curpage = (uint64_t *)ensure_pg(pml4e, create);
	uint64_t *pdpte = &curpage[PDPTX(va)];
	curpage = (uint64_t *)ensure_pg(pdpte, create);
	uint64_t *pde = &curpage[PDX(va)];
	curpage = (uint64_t *)ensure_pg(pde, create);

	return &curpage[PTX(va)];
}

static void
mapone(uint64_t *pgdir, uint64_t va, uint64_t pa)
{
	if (pa & PGOFFMASK)
		pancake("pa not aligned", pa);

	uint64_t *pte = pgdir_walk(pgdir, va, 1);

	if ((*pte & PTE_P) && PTE_ADDR(*pte) != pa)
		pancake("already mapped?", *pte);

	*pte = pa | PTE_P | PTE_W;
}
