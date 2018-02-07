#include <err.h> 
#include <stdio.h> 
#include <stdlib.h> 
#include <string.h> 
#include <unistd.h> 
#include <fcntl.h>

#include <elf.h> 

void
usage(char *me)
{
	printf( "%s <filename> <addr>\n"
		"\n"
		"Change the ELF entry point of <filename> to <addr>\n", me);

	exit(1);
}

void
chkelf(Elf64_Ehdr *eh)
{
	if (memcmp(&eh->e_ident, "\x7f""ELF", 4))
		errx(1, "not an elf");

	if (eh->e_ident[5] != ELFDATA2LSB)
		errx(1, "not little-endian?");

	if (eh->e_type != ET_EXEC)
		errx(1, "not an executable elf");

	if (eh->e_machine != EM_X86_64)
		errx(1, "not a 64 bit elf");
}

int
main(int argc, char **argv)
{
	Elf64_Ehdr eh;

	if (argc != 3)
		usage(argv[0]);

	const char *fn = argv[1];
	uint64_t addr = strtoul(argv[2], NULL, 0);

	if (addr & ~((1ULL << 32) -1))
		errx(1, "entry is 64bit pointer; bootloader will perish");

	int fd = open(fn, O_RDWR);

	if (fd < 0)
		err(1, "open");

	ssize_t ret = read(fd, &eh, sizeof(eh));
	if (ret < sizeof(eh))
		err(1, "short read? %zd", ret);

	chkelf(&eh);

	printf("using address 0x%lx\n", addr);
	eh.e_entry = addr;
	if ((ret = pwrite(fd, &eh, sizeof(eh), 0)) < sizeof(eh))
		err(1, "short write? %zd", ret);

	close(fd);

	return 0;
}
