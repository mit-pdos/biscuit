#include <types.h>
#include <x86.h>

/**********************************************************************
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

struct Elf {
	uint32_t e_magic;
#define ELF_MAGIC 0x464C457FU	/* "\x7FELF" in little endian */
	uint8_t e_elf[12];
	uint16_t e_type;
	uint16_t e_machine;
	uint32_t e_version;
	uint32_t e_entry;
	uint32_t e_phoff;
	uint32_t e_shoff;
	uint32_t e_flags;
	uint16_t e_ehsize;
	uint16_t e_phentsize;
	uint16_t e_phnum;
	uint16_t e_shentsize;
	uint16_t e_shnum;
	uint16_t e_shstrndx;
};

struct Proghdr {
	uint32_t p_type;
	uint32_t p_offset;
	uint32_t p_va;
	uint32_t p_pa;
	uint32_t p_filesz;
	uint32_t p_memsz;
	uint32_t p_flags;
	uint32_t p_align;
};

struct Elf64 {
	uint32_t 	e_magic;
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

struct Proghdr64 {
	uint32_t	p_type;		/* entry type */
	uint32_t	p_flags;	/* flags */
	uint64_t	p_offset;	/* offset */
	uint64_t	p_va;		/* virtual address */
	uint64_t	p_pa;		/* physical address */
	uint64_t	p_filesz;	/* file size */
	uint64_t	p_memsz;	/* memory size */
	uint64_t	p_align;	/* memory & file alignment */
};


#define SECTSIZE	512
#define ELFHDR		((struct Elf *) 0x10000) // scratch space
#define ELFSTART	((uint32_t)0x10000)

void readsect(void*, uint32_t);
void readseg(uint32_t, uint32_t, uint32_t);
void waitdisk(void);
void readsect(void *, uint32_t);

static void memset(void *, char, uint32_t);
static void putch(uint32_t);
static void pnum(uint32_t);
static void pmsg(char *);

// # of sectors this code takes up; i set this after compiling and observing
// the size of the text
#define BOOTBLOCKS     4

void
bootmain(void)
{
	// XXX it would be better to have the bootloader load the segments
	// contiguously but setup a page table at the expected virtual address.
	// then it is easier to figure out which memory is free.

	// read 1st page off disk
	readseg((uint32_t) ELFHDR, SECTSIZE*8, 0);

	// is this a valid ELF?
	if (ELFHDR->e_magic != ELF_MAGIC)
		goto bad;

	uint32_t elfend = ELFSTART + ELFHDR->e_ehsize +
	    ELFHDR->e_phnum * ELFHDR->e_phentsize +
	    ELFHDR->e_shnum * ELFHDR->e_shentsize;

	// load each program segment (ignores ph flags)
	struct Proghdr *ph, *eph;
	ph = (struct Proghdr *) ((uint8_t *) ELFHDR + ELFHDR->e_phoff);
	eph = ph + ELFHDR->e_phnum;
	for (; ph < eph; ph++) {
		// p_pa is the load address of this segment (as well
		// as the physical address)
		if (ph->p_type != 1)	// PT_LOAD
			continue;

		// make sure the segment doesn't overwrite the ELF header that
		// we are reading
		uint32_t sstart = ph->p_pa;
		uint32_t send = ph->p_pa + ph->p_memsz;
		if ((sstart >= ELFSTART && sstart < elfend) ||
		    (send >= ELFSTART && send < elfend))
			goto bad;

		readseg(ph->p_pa, ph->p_memsz, ph->p_offset);
		// zero bss
		if (ph->p_filesz != ph->p_memsz)
			memset((void *)ph->p_pa + ph->p_filesz, 0,
			    ph->p_memsz - ph->p_filesz);
	}

	// call the entry point from the ELF header
	// note: does not return!
	((void (*)(void)) (ELFHDR->e_entry))();

bad:
	outw(0x8A00, 0x8A00);
	outw(0x8A00, 0x8E00);
	while (1)
		/* do nothing */;
}

// Read 'count' bytes at 'offset' from kernel into physical address 'pa'.
// Might copy more than asked
void
readseg(uint32_t pa, uint32_t count, uint32_t offset)
{
	uint32_t end_pa;

	end_pa = pa + count;

	// round down to sector boundary
	pa &= ~(SECTSIZE - 1);

	// translate from bytes to sectors, and kernel starts at sector
	// "BOOTBLOCKS"
	offset = (offset / SECTSIZE) + BOOTBLOCKS;

	// If this is too slow, we could read lots of sectors at a time.
	// We'd write more to memory than asked, but it doesn't matter --
	// we load in increasing order.
	while (pa < end_pa) {
		// Since we haven't enabled paging yet and we're using
		// an identity segment mapping (see boot.S), we can
		// use physical addresses directly.  This won't be the
		// case once JOS enables the MMU.
		readsect((uint8_t*) pa, offset);
		pa += SECTSIZE;
		offset++;
	}
}

static void
memset(void *p, char c, uint32_t sz)
{
	char *np = (char *)p;
	while (sz--)
		*np++ = c;
}

static void
putch(uint32_t mark)
{
        static uint8_t x;
        static uint8_t y;

        uint16_t *cons = (uint16_t *)0xb8000;

        cons[y*80 + x++] = (0x07 << 8) | (mark & 0xff);

        if (x >= 79) {
                x = 0;
                y++;
        }

	if (y >= 29)
		y = 0;
}

__attribute__((unused))
static void
pnum(uint32_t n)
{
	uint32_t nn = (uint32_t)n;
	int i;

	//for (i = 60; i >= 0; i -= 4) {
	for (i = 28; i >= 0; i -= 4) {
		uint32_t cn = (nn >> i) & 0xf;

		if (cn >= 0 && cn <= 9)
			putch('0' + cn);
		else
			putch('A' + cn - 10);
	}
}

__attribute__((unused))
static void
pmsg(char *msg)
{
	while (*msg)
		putch(*msg++);
}
