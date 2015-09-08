ENTRY(_entry)

SECTIONS
{
        . = 0x2c8000001000;
        .text : { *(.text) }
        .data : { *(.data) }
        .bss : { *(.bss) }

	/DISCARD/ : {
		*(.eh_frame)
	}
}
