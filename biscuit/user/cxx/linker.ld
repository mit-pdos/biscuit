ENTRY(_entry)

SECTIONS
{
        . = 0x2c8000000000;
        .text : { *(.text) }
        .data : { *(.data) }
        .eh_frame : { *(.eh_frame) }
        .bss : { *(.bss) }

	/DISCARD/ : {
		*(.note.gnu.build-id)
	}
}
