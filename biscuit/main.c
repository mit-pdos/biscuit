#include <types.h>
#include <x86.h>

static volatile uint16_t *vga = (uint16_t *)0xb8000;

static void
clear()
{
	int r, i;
	for (r = 0; r < 32; r++)
		for (i = 0; i < 80; i++)
			vga[r*80+i] = 0;
}

static void
delay(int times)
{
	int i;
	for (i = 0; i < times; i++)
		outb(0x80, 0);
}

static void
put(char c)
{
	const int row = 2;
	const int col = 32;

	vga[row*80+col] = 0x07 << 8 | c;
}

int
main(void)
{
	uint16_t b[] = {'B', 'i', 's', 'c', 'u', 'i', 't'};
	char bar[] = {'/', '-', '\\', '|'};
	uint32_t i;

	clear();

	for (i = 0; i < sizeof(b)/sizeof(b[0]); i++)
		vga[80+ 30+i] = b[i] + (((i+1) % 10) << 8);

	while (1) {
		delay(100000);
		put(bar[i++ % sizeof(bar)]);
	}
}
