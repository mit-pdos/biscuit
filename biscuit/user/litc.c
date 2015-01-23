#include <littypes.h>

static int px;
static int py;

void
putch(char c)
{
	ushort *vga = (ushort *)0xb8000;

	vga[py*80 + px] = (0x7 << 8) | c;
	px++;
	if (px >= 79) {
		px = 0;
		py++;
	}

	if (py >= 25)
		py = 0;
}

void
pmsg(char *msg)
{
	while(*msg) {
		putch(*msg);
		msg++;
	}
}
