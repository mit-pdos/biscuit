# Start the CPU: switch to 32-bit protected mode, jump into C.
# The BIOS loads this code from the first sector of the hard disk into
# memory at physical address 0x7c00 and starts executing in real mode
# with %cs=0 %ip=7c00.

.set PROT_MODE_CSEG, 0x8         # kernel code segment selector
.set PROT_MODE_DSEG, 0x10        # kernel data segment selector
.set CR0_PE_ON,      0x1         # protected mode enable flag
.set BOOTBLOCKS,     9
.set SECTSIZE,       512
.globl e820m
.set e820m,	0x6000

.globl start
start:
  .code16                     # Assemble for 16-bit mode
  cli                         # Disable interrupts
  cld                         # String operations increment

  # Set up the important data segment registers (DS, ES, SS).
  xorw    %ax,%ax             # Segment number zero
  movw    %ax,%ds             # -> Data Segment
  movw    %ax,%es             # -> Extra Segment
  movw    %ax,%ss             # -> Stack Segment

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

  # Switch from real to protected mode, using a bootstrap GDT
  # and segment translation that makes virtual addresses 
  # identical to their physical addresses, so that the 
  # effective memory map does not change during the switch.
  cli		// some bioses set IF in e820!
  lgdt    gdtdesc
  movl    %cr0, %eax
  orl     $CR0_PE_ON, %eax
  movl    %eax, %cr0
  
  # Jump to next instruction, but in 32-bit code segment.
  # Switches processor into 32-bit mode.
  ljmp    $PROT_MODE_CSEG, $protcseg

  .code32                     # Assemble for 32-bit mode
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

  # XXX: Use legacy IDE command block if this is Qemu; use hardcoded IDE
  # command block otherwise.
  xorl	%eax, %eax
  cpuid
  // "Genu", Intel's string; Qemu uses an AMD string
  cmpl	$0x756e6547, %ebx
  je	1f
  movw	$0x1f0, idereg
1:

  movl	 $0x1753, 0xb8000
  # Read the remaining sectors of the boot loader; the first was loaded for us.
  movl  $BOOTBLOCKS-1, %ecx
  movl  $0x7c00 + SECTSIZE, %eax
  movl  $1, %ebx

1:
  pushl %ebx
  pushl %eax
  call  readsect
  addl  $SECTSIZE, %eax
  incl  %ebx
  decl  %ecx
  jnz   1b

  call bootmain

  # If bootmain returns (it shouldn't), loop.
spin:
  jmp spin

# void waitdisk(void)
.globl waitdisk
waitdisk:
  movw	idereg, %dx
  addw	$7, %dx
1:
  inb   %dx, %al
  andb  $0xc0, %al
  cmpb  $0x40, %al
  jne   1b
  ret

# void readsect(void *dst, uint32_t offset)
.globl readsect
readsect:
  pushl %ebp
  movl  %esp, %ebp

  pushl %edi
  pushl %eax
  pushl %edx
  pushl %ecx
  pushl %ebx

  call waitdisk

  movw	idereg, %bx

  leaw	2(%bx), %dx
  movb	$1, %al
  outb	%al, %dx

  leaw	3(%bx), %dx
  movl  12(%ebp), %eax   // offset
  outb  %al, %dx

  leaw	4(%bx), %dx
  shr  $8, %eax
  outb  %al, %dx

  leaw	5(%bx), %dx
  shr  $8, %eax
  outb  %al, %dx

  leaw	6(%bx), %dx
  shr  $8, %eax
  orb   $0xe0, %al
  outb  %al, %dx

  leaw	7(%bx), %dx
  movb	$0x20, %al
  outb	%al, %dx

  call waitdisk

  movl  $(SECTSIZE/4), %ecx  // divide by 4 because we move one long at a time
  movl  8(%ebp), %edi
  leaw	0(%bx), %dx
  cld
  repnz insl

  popl  %ebx
  popl  %ecx
  popl  %edx
  popl  %eax
  popl  %edi

  movl  %ebp, %esp
  popl  %ebp

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
  SEG_NULL				# null seg
  SEG(STA_X|STA_R, 0x0, 0xffffffff)	# code seg
  SEG(STA_W, 0x0, 0xffffffff)	        # data seg
  SEG64(STA_X|STA_R, 0x0, 0xffffffff)	# 64bit code seg

gdtdesc:
  .word   (gdtdesc - gdt - 1)             # sizeof(gdt) - 1
  .long   gdt                             # address gdt

#define	E820		\
	.long 0,0;		\
	.long 0,0;		\
	.long 0;		\
	.long 0,0;		\
//.globl e820m
//e820m:
//E820
//E820
//E820
//E820
//E820
//E820
//E820

.globl e820entries
e820entries:
	.long 0
idereg:
	// hard coded PCI-native IDE command block for my test hardware's i3400
	// southbridge
	.word 0xeca0

.org 506
  // this location is special: mkbdisk.py puts the number of the first fs block
  // here
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
