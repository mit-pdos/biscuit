ENTRY(_entry)

SECTIONS
{
	. = 0x2c8000001000;
	.got : { *(.got*) }
	.eh_frame : { *(.eh_frame*) }
	.text : { *(.text*) }
	.data : { *(.data* .rodata*) }
	.tdata : { *(.tdata*) }
	.tbss : { *(.tbss*) }
	.bss : { *(.bss*) *(COMMON) }
	/*.got BLOCK(0x1000) : { *(.got*) }*/

	/DISCARD/ : {
		*(*)
	}
}
