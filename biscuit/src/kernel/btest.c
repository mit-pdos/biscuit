asm(".code16gcc");

typedef unsigned char uint8_t;
typedef unsigned short uint16_t;

static __inline uint8_t inb(int port);

static void pc(uint8_t, uint16_t);

int main()
{
	pc('A', 0);
	while (1)
		inb(0x80);
}

static __inline uint8_t
inb(int port)
{
	uint8_t data;
	__asm __volatile("inb %w1,%0" : "=a" (data) : "d" (port));
	return data;
}

static void
pc(uint8_t c, uint16_t x)
{
		asm volatile(
		    "movw	$0xb000, %%ax\n"
		    "movw	%%ax, %%ds\n"
		    "leaw	(0x8000)(, %%ecx, 2), %%ax\n"
		    "andw	$0xff, %%dx\n"
		    "orw	$0x1700, %%dx\n"
		    "movw	%%dx, (%%eax)\n"
		    "xorw	%%ax, %%ax\n"
		    "movw	%%ax, %%ds\n"
		    :
		    : "d"(c), "c"(x)
		    : "memory", "eax");
}

// why doesn't this work?
//asm(".org 510");
asm(".org 462");
asm(".byte 0x55, 0xaa");
