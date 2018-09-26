# Start the CPU: switch to 32-bit protected mode, jump into C.
# The BIOS loads this code from the first sector of the hard disk into
# memory at physical address 0x7c00 and starts executing in real mode
# with %cs=0 %ip=7c00.

.set PROT_MODE_CSEG, 0x8         # kernel code segment selector
.set PROT_MODE_DSEG, 0x10        # kernel data segment selector
.set CR0_PE_ON,      0x1         # protected mode enable flag
.set BOOTBLOCKS,     10
.set SECTSIZE,       512
.globl e820m
.set e820m,          0x6000
// bounce buffer for real-mode int 13h
.set diskbounce,     0x5000

.globl start
.code16                     # Assemble for 16-bit mode
start:
	cld                         # String operations increment

	movb	%dl, diskid
	# Set up the important data segment registers (DS, ES, SS).
	xorw	%ax,%ax             # Segment number zero
	movw	%ax,%ds             # -> Data Segment
	movw	%ax,%es             # -> Extra Segment
	movw	%ax,%ss             # -> Stack Segment

	# Enable A20:
	#   For backwards compatibility with the earliest PCs, physical
	#   address line 20 is tied low, so that addresses higher than
	#   1MB wrap around to zero by default.  This code undoes this.
seta20.1:
	inb     $0x64,%al               # Wait for not busy
	testb   $0x2,%al
	jnz     seta20.1

	movb    $0xd1,%al               # 0xd1 -> port 0x64
	outb    %al,$0x64

seta20.2:
	inb     $0x64,%al               # Wait for not busy
	testb   $0x2,%al
	jnz     seta20.2

	movb    $0xdf,%al               # 0xdf -> port 0x60
	outb    %al,$0x60

// maybe use new a20?
//inb	$0x92, %al
//orb	$2, %al
//outb	%al, $0x92

// XXX move e820 code/buffer and diskbounce buffer to bootmain.c
	movw	$(4096/4), %cx
	movw	  $e820m, %di
	movl	$0, %eax
	repnz stosl

e820:
	movw    $start, %sp
	xorl    %ebx, %ebx
	pushl	  %ebx
	movw	  $e820m, %di

e820.1:
	movl	  $0xe820, %eax
	movl	  $20, %ecx
	movl    $0x534d4150, %edx
	int	  $0x15
	cmpl	  $0x534d4150, %eax
	je	  e820.4
e820.fail:
	movw	  $0xb000, %ax
	movw	  %ax, %ds
	xorl	  %eax, %eax
	movw	  $0x8000, %ax
	movw	  $0x1742, (%eax)
1:
	jmp	  1b
e820.4:
	popl	  %eax
	incl	  %eax
	pushl	  %eax
	# done?
	jc	  e820.2
	test	  %bx, %bx
	jz	  e820.2
	# 28 bytes
	addw	  $28, %di
	jmp	  e820.1
e820.2:
	popl	  %eax
	movl	  %eax, e820entries

	# read the rest of the boot loader
	movw  $0x7c00 + SECTSIZE, %ax
	movw	%ax, dap_dest_off
	movw	$0, dap_dest_seg
	movw	$1, dap_sectoff_lo

	movb	$0x42, %ah
	movb	diskid, %dl
	movw	$dap, %si
	int	$0x13

	jc	crud
	testb	%ah, %ah
	jnz	crud

	# Switch from real to protected mode, using a bootstrap GDT and segment
	# translation that makes virtual addresses identical to their physical
	# addresses, so that the effective memory map does not change during
	# the switch.
	cli
	lgdt    gdtdesc
	movl    %cr0, %eax
	orl     $CR0_PE_ON, %eax
	movl    %eax, %cr0

	# Jump to next instruction, but in 32-bit code segment.  Switches
	# processor into 32-bit mode.
	ljmp    $PROT_MODE_CSEG, $protcseg

# 16bit code
crud:
	movw	$0xb000, %ax
	movw	%ax, %ds
	movw	$0x1746, 0x8000
	jmp	crud

diskid:
	.byte	0
dap:
dap_size:
	.byte	0x10
	.byte	0
dap_nsect:
	.word	BOOTBLOCKS - 1
dap_dest_off:
	.word	0
dap_dest_seg:
	.word	0
dap_sectoff_lo:
	.long	0
dap_sectoff_hi:
	.long	0

.code32
protcseg:
	# Set up the protected-mode data segment registers
	movw    $PROT_MODE_DSEG, %ax    # Our data segment selector
	movw    %ax, %ds                # -> DS: Data Segment
	movw    %ax, %es                # -> ES: Extra Segment
	movw    %ax, %fs                # -> FS
	movw    %ax, %gs                # -> GS
	movw    %ax, %ss                # -> SS: Stack Segment

	# Set up the stack pointer
	movl    $start, %esp
	call bootmain

	# If bootmain returns (it shouldn't), loop.
spin:
	jmp spin

.code32
# void readsect(void *dst, uint32_t sectornum)
.globl readsect
readsect:
	movl	8(%esp), %eax		// arg2
	movl	%eax, dap_sectoff_lo
	movl	$0, dap_sectoff_hi
	movw	$8, dap_nsect
	movw	$diskbounce, dap_dest_off
	movw	$0, dap_dest_seg
	pushl	%esi
	pushl	%edi

.globl prot2real
prot2real:
	cli
	movl	$(5 << 3), %eax
	movl    %eax,%ds
	movl    %eax,%es
	movl    %eax,%ss

	ljmp	$(4 << 3), $fixseg
fixseg:
	movl	%cr0, %eax
	andl	$~CR0_PE_ON, %eax
	movl	%eax, %cr0
	ljmp	$0, $realseg
realseg:
.code16
	xorw    %ax,%ax
	movw    %ax,%ds
	movw    %ax,%es
	movw    %ax,%ss
	sti

	movb	$0x42, %ah
	movb	diskid, %dl
	movw	$dap, %si
	int	$0x13

	jc	crud
	testb	%ah, %ah
	jnz	crud

	cli
	movl	%cr0, %eax
	orl	$CR0_PE_ON, %eax
	movl	%eax, %cr0
	ljmp    $PROT_MODE_CSEG, $protseg
.code32
protseg:
	movw    $PROT_MODE_DSEG, %ax
	movw    %ax, %ds
	movw    %ax, %es
	movw    %ax, %fs
	movw    %ax, %gs
	movw    %ax, %ss

	// copy sector from diskbounce to destination. we cannot directly read
	// the sector to the destination address because int 13h is a real mode
	// BIOS call and thus only has 1MB of addressable memory.
	movl	$diskbounce, %esi
	movl	12(%esp), %edi		// arg1
	movl	$4096/4, %ecx
	cld
	repnz	movsl

	popl	%edi
	popl	%esi
	ret

# Bootstrap GDT
.p2align 2                                # force 4 byte alignment

#define SEG_NULL						\
	.word 0, 0;						\
	.byte 0, 0, 0, 0
#define SEG(type,base,lim)					\
	.word (((lim) >> 12) & 0xffff), ((base) & 0xffff);	\
	.byte (((base) >> 16) & 0xff), (0x90 | (type)),		\
		(0xC0 | (((lim) >> 28) & 0xf)), (((base) >> 24) & 0xff)
// real-mode segment: has G and D/B clear (since G is clear RSEG doesn't shift
// limit by page size)
#define RSEG(type,base,lim)					\
	.word ((lim) & 0xffff), ((base) & 0xffff);	\
	.byte (((base) >> 16) & 0xff), (0x90 | (type)),		\
		(0x00 | (((lim) >> 16) & 0xf)), (((base) >> 24) & 0xff)
#define SEG64(type,base,lim)					\
	.word (((lim) >> 12) & 0xffff), ((base) & 0xffff);	\
	.byte (((base) >> 16) & 0xff), (0x90 | (type)),		\
		(0xA0 | (((lim) >> 28) & 0xf)), (((base) >> 24) & 0xff)

// Application segment type bits
#define STA_X		0x8	    // Executable segment
#define STA_E		0x4	    // Expand down (non-executable segments)
#define STA_C		0x4	    // Conforming code segment (executable only)
#define STA_W		0x2	    // Writeable (non-executable segments)
#define STA_R		0x2	    // Readable (executable segments)
#define STA_A		0x1	    // Accessed

gdt:
	SEG_NULL				# 0 - null seg
	SEG(STA_X|STA_R, 0x0, 0xffffffff)	# 1 - code seg
	SEG(STA_W, 0x0, 0xffffffff)	        # 2 - data seg
	SEG64(STA_X|STA_R, 0x0, 0xffffffff)	# 3 - 64bit code seg
	RSEG(STA_X|STA_R, 0, 0xffff)		# 4 - real mode code
	RSEG(STA_W, 0, 0xffff)			# 5 - real mode data

gdtdesc:
  .word   (gdtdesc - gdt - 1)             # sizeof(gdt) - 1
  .long   gdt                             # address gdt

.globl e820entries
e820entries:
	.long 0
.org 0x1de
       .byte 0, 0, 0, 0, 0xb1, 0, 0, 0
       .long 0, 0
.org 0x1fa
	// this location is special: mkbdisk.py puts the number of the first fs
	// block here
	.long 0
	.byte 0x55, 0xaa

//endy:
// useful debugging code
//cksum:
//  xorl	%ebp, %ebp
//  #movl	$((endy - .)/4), %ecx
//  movl	$(4096/4), %ecx
//  xorl	%ebx, %ebx
//1:
//  leal	0(, %ebx, 4), %edx
//  movl	(%edx), %eax
//  addl	%eax, %ebp
//  incl	%ebx
//  cmp	%ebx, %ecx
//  jne	1b
//
//  movl	%ebp, %eax
//  call	pnum
//  ret

//dump:
//  movl	$((endy - .)/4), %ecx
//  xorl	%ebx, %ebx
//1:
//  leal	wtf(, %ebx, 4), %edx
//  movl	(%edx), %eax
//  call	pnum
//  incl	%ebx
//  cmp	%ebx, %ecx
//  jne	1b

// num in eax
//pnum:
//	pushl	%eax
//	pushl	%edx
//	pushl	%ecx
//	movl	%eax, %edx
//	movl	$8, %ecx
//1:
//	xorl	%eax, %eax
//	movl	%edx, %eax
//	andl	$0xf0000000, %eax
//	shr	$28, %eax
//	call	pdig
//	shl	$4, %edx
//	loop	1b
//	popl	%ecx
//	popl	%edx
//	popl	%eax
//	ret
//
//pdig:
//	pushl	%eax
//	pushl	%edx
//	pushl	%ecx
//	cmpl	$9, %eax
//	ja	hex
//	addl	$'0', %eax
//print:
//	orl	$0x1700, %eax
//	movl	biddle, %edx
//	movw	$0xb000, %cx
//	movw	%cx, %ds
//	leal	(0x8000)(, %edx, 2), %ecx
//	movw	%ax, (%ecx)
//	xorl	%ecx, %ecx
//	movw	%cx, %ds
//	incl	%edx
//	movl	%edx, biddle
//
//	popl	%ecx
//	popl	%edx
//	popl	%eax
//	ret
//hex:
//	addl	$('A' - 10), %eax
//	jmp print
