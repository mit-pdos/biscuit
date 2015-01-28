#include <types.h>
#include <x86.h>

/**********************************************************************
 * boot loader
 **********************************************************************/

void waitdisk(void);
void readsect(void *, uint32_t);

static void *allocphys(uint64_t *, uint64_t);
static uint32_t alloc_start(void);
static void checkmach(void);
static uint32_t getpg(void);
static uint64_t elfsize(void);
static void ensure_empty(uint64_t *, uint64_t);
static uint32_t ensure_pg(uint64_t *, int);
static int is32(uint64_t);
static int isect(uint64_t, uint64_t,uint64_t, uint64_t);
static void mapone(uint64_t *, uint64_t, uint64_t, int);
static void memset(void *, char, uint64_t);
static uint64_t mem_sizeadjust(uint64_t, uint64_t, uint64_t);
static uint64_t mem_bump(uint64_t);
static void pancake(char *msg, uint64_t addr);
static uint64_t *pgdir_walk(uint64_t *, uint64_t, int);
static void pmsg(char *);
static void pnum(uint64_t);
static void putch(char);
static void readseg(uint64_t *, uint64_t, uint64_t, uint64_t);

__attribute__((packed))
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

__attribute__((packed))
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

// size of e820_t is hardcoded in boot.S
__attribute__((packed))
struct e820_t {
	uint64_t base;
	uint64_t len;
	uint32_t type;
#define	MEM_AVAIL	1
	uint64_t extype;
};

// provided by boot.S
extern int e820entries;
extern struct e820_t e820m[];

// secret storage
struct __attribute__((packed)) ss_t {
	uint64_t e820map;
	uint64_t pgtbl;
	uint64_t first_free;
} *ss = (struct ss_t *)0x7c00;

// # of sectors this code takes up; i set this after compiling and observing
// the size of the text
#define BOOTBLOCKS     8
// boot.S has room for 7 e820 entries
#define	NE820          7

#define SECTSIZE	512
#define ELFHDR		((struct Elf *) 0x10000) // scratch space
#define	NEWSTACK	0x80000000 // VA for new stack

#define	VREC		0x42	// recursive mapping slot
#define	VTEMP		0x43

void
bootmain(void)
{
	// read 1st page of kernel
	int i;
	for (i = 0; i < 8; i++)
		readsect((char *)ELFHDR + i*SECTSIZE, BOOTBLOCKS+i);

	checkmach();

	// is this a valid ELF?
	if (ELFHDR->e_magic != ELF_MAGIC)
		goto bad;

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
		mapone(pgdir, addr, addr, 0);
	}

	// give us VGA so we can print
	mapone(pgdir, 0xb8000, 0xb8000, 1);
	mapone(pgdir, 0xb9000, 0xb9000, 1);

	// get a new stack with guard page
	ensure_empty(pgdir, NEWSTACK - PGSIZE);
	ensure_empty(pgdir, NEWSTACK - 2*PGSIZE);
	allocphys(pgdir, NEWSTACK - 1);

	// XXX setup tramp if entry is 64bit address...
	if (!is32(ELFHDR->e_entry))
		pancake("fixme: entry is 64 bit!", ELFHDR->e_entry);

	// goodbye, elf header
	uint32_t entry = (uint32_t)ELFHDR->e_entry;

	// goodbye, zeroing physical pages in getpg()
	uint32_t firstfree = getpg();

	// enter recursive mapping
	pgdir[VREC] = (uint64_t)(uint32_t)pgdir | PTE_P | PTE_W;
	// make sure VTEMP is empty
	if (pgdir[VTEMP] & PTE_P)
		pancake("VTEMP is present?", pgdir[VTEMP]);

	// enter long mode
	enable_pae();
	lcr3(pgdir);
	uint64_t efer = rdmsr(IA32_EFER);
	wrmsr(IA32_EFER, efer | IA32_EFER_LME);
	enable_paging_wp();

	// use secret structure
	ss->e820map = (uint64_t)(int)e820m;
	ss->pgtbl = (uint64_t)(int)pgdir;
	ss->first_free = firstfree;

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
	if (!is32(va))
		pancake("va too large for poor ol' 32-bit me", va);

	uint64_t *pte = pgdir_walk(pgdir, va, 1);
	uint32_t ma = ensure_pg(pte, 1);

	return (void *)(ma | ((uint32_t)va & PGOFFMASK));
}

static uint32_t
alloc_start(void)
{
	struct e820_t *ep;
	// sanity check e820 map
	for (ep = e820m; ep - e820m < e820entries; ep++) {
		struct e820_t *ep2;
		if (ep->type != MEM_AVAIL)
			continue;
		for (ep2 = e820m; ep2 - e820m < e820entries; ep2++) {
			if (ep == ep2 || ep2->type == MEM_AVAIL)
				continue;
			uint64_t e1 = ep->base + ep->len - 1;
			uint64_t e2 = ep2->base + ep2->len - 1;
			if (isect(ep->base, e1, ep2->base, e2)) {
				pnum(ep->base);
				pnum(e1);
				pnum(ep2->base);
				pnum(e2);
				pancake("usable intersects with unusable", 0);
			}
		}
	}

	// find memory to use in the e820 map
	uint64_t memsz = elfsize();
	uint32_t last = 0;
	int found = 0;

	for (ep = e820m; ep - e820m < e820entries; ep++) {
		if (ep->type != MEM_AVAIL)
			continue;
		// big enough?
		uint64_t regsz = ep->len;
		// we cannot use some regions of memory; subtract their size
		// from the available size
		regsz = mem_sizeadjust(ep->base, ep->base + ep->len, regsz);
		if (regsz >= memsz) {
			if (!is32(ep->base))
				continue;
			last = (uint32_t)ep->base;
			found = 1;
			break;
		}
	}
	if (!found)
		pancake("couldn't find memory", found);
	return last;
}

static void
checkmach(void)
{
	uint32_t eax, edx;
	cpuid(0x80000001, &eax, &edx);
	if ((edx & (1UL << 29)) == 0)
		pancake("not a 64 bit machine?", edx);

	// check e820 map
	if (e820entries > NE820)
		pancake("more than 7 e820 entries", e820entries);
}

static uint32_t
getpg(void)
{
	static uint32_t last;

	if (!last)
		last = alloc_start();

	last = mem_bump(last);

	uint32_t ret = last;
	last += PGSIZE;

	if (ret & PGOFFMASK)
		pancake("not aligned", ret);

	void *p = (void *)ret;
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

static int
is32(uint64_t in)
{
	return (in >> 32) == 0;
}

static int
isect(uint64_t a, uint64_t b,uint64_t x, uint64_t y)
{
	if (x >= a && x < b)
		return 1;
	if (y >= a && y < b)
		return 1;
	return 0;
}

static uint64_t
elfsize(void)
{
	struct Proghdr *ph, *eph;
	uint64_t ret = 0;

	ph = (struct Proghdr *)((uint8_t *) ELFHDR + ELFHDR->e_phoff);
	eph = ph + ELFHDR->e_phnum;
	for (; ph < eph; ph++)
		ret += ph->p_memsz;
	return ret;
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

// regions of memory not included in the e820 map, into which we cannot
// allocate
static struct {
	uint64_t start;
	uint64_t end;
} badregions[] = {
	// Elf header
	{0x10000, 0x11000},
	// VGA
	{0xa0000, 0x100000},
	// ourselves
	{ROUNDDOWN(0x7c00, PGSIZE),
	    ROUNDUP(0x7c00+BOOTBLOCKS*SECTSIZE, PGSIZE)},
};

__attribute__((unused))
static uint64_t
mem_bump(uint64_t s)
{
	int i;
	int num = sizeof(badregions)/sizeof(badregions[0]);

	for (i = 0; i < num; i++) {
		if (isect(badregions[i].start, badregions[i].end, s, s+PGSIZE))
			return badregions[i].end;
	}

	return s;
}

static uint64_t
mem_sizeadjust(uint64_t s, uint64_t e, uint64_t size)
{
	uint64_t ret = size;
	int i;
	int num = sizeof(badregions)/sizeof(badregions[0]);

	for (i = 0; i < num; i++)
		if (isect(s, e, badregions[i].start, badregions[i].end))
			ret -= badregions[i].end - badregions[i].start;

	return ret;
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
mapone(uint64_t *pgdir, uint64_t va, uint64_t pa, int cd)
{
	if (pa & PGOFFMASK)
		pancake("pa not aligned", pa);

	uint64_t *pte = pgdir_walk(pgdir, va, 1);

	if ((*pte & PTE_P) && PTE_ADDR(*pte) != pa)
		pancake("already mapped?", *pte);

	uint64_t perms = PTE_P | PTE_W | (cd ? PTE_PCD : 0);
	*pte = pa | perms;
}
