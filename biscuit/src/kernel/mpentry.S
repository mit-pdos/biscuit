.globl _start

/*
 * this code needs to go somewhere within the first 1MB of mem (because APs
 * start in real mode) and must also have an identity mapping in the kernel
 * pmap. thus the old bootloader location is a good choice.
 */

.set CR0_PE,             (1 << 0)
.set CR0_PG,             (1 << 31)
.set CR4_PAE,            (1 << 5)
.set IA32_EFER,          0xc0000080
.set IA32_EFER_LME,      (1 << 8)
.set PGSIZE,             4096
.set PROT_MODE_CSEG,     (1 << 3)
.set PROT_MODE_CSEG64,   (3 << 3)
.set PROT_MODE_DSEG,     (2 << 3)
.set REAL_CSEG64,        (1 << 3)
// provided by boot loader and kernel. holds pmap, idt, gdt, and the next
// address to jump to
.set SECRET,             0x7c00

#define SPLOCK(x)		\
	movq	$x, %rdi;	\
1:				\
	movq	$1, %rax;	\
	lock			\
	xchgq	%rax, (%rdi);	\
	cmpq	$0, %rax;	\
	je	3f;		\
2:				\
	pause;			\
	movq	(%rdi), %rax;	\
	cmpq	$0, %rax;	\
	je	1b;		\
	jmp	2b;		\
3:

#define SPUNLOCK(x)		\
	movq	$0, x

.align 4096
.code16
_start:

	cli

	# start our journey into long mode
	# enter protected mode
	movw	%ax, %ds
	lgdt	gdtdesc
	movl	%cr0, %eax
	orl	$CR0_PE, %eax
	movl	%eax, %cr0

	ljmp	$PROT_MODE_CSEG, $protcseg

.code32
protcseg:
	movw	$PROT_MODE_DSEG, %ax
	movw	%ax, %ds
	movw	%ax, %ss
	movw	%ax, %es
	movw	%ax, %fs
	movw	%ax, %gs

	# set pae
	movl	%cr4, %eax
	orl	$CR4_PAE, %eax
	movl	%eax, %cr4

	# load pmap
	movl	$SECRET, %ebp
	movl	8(%ebp), %ebx	// ebx = pmap
	mov	%ebx, %cr3

	# set IA32_EFER_LME
	movl	$IA32_EFER, %ecx
	rdmsr
	orl	$IA32_EFER_LME, %eax
	wrmsr

	# set paging
	movl	%cr0, %eax
	orl	$CR0_PG, %eax
	movl	%eax, %cr0

	# ljmp
	ljmp	$PROT_MODE_CSEG64, $protocseg64

.code64
protocseg64:
	SPLOCK(lock1)

	// increment ap count
	movq	$SECRET, %rdx
	movq	64(%rdx), %rbx
	incq	%rbx
	movq	%rbx, 64(%rdx)
	// store id in %rbp
	movq	%rbx, %rbp

	// get our own stack
	movq	72(%rdx), %rax
	movq	%rax, %rsp
	// rsp = bottom of stack page
	addq	$PGSIZE, %rsp
	addq	$(PGSIZE * 4), %rax
	movq	%rax, 72(%rdx)

	// load idt and gdt
	lgdt	32(%rdx)
	lidt	48(%rdx)

	SPUNLOCK(lock1)

	// wait for BSP to map our stack. APs that joined too late (hopefuly
	// that doesn't happen) loop here.
1:
	pause
	movq	80(%rdx), %rax
	cmpq	%rbp, %rax
	jb	1b

	// stack is now valid, update CS
	pushq	$REAL_CSEG64
	pushq	$realcseg
	lretq

realcseg:
	movq	24(%rdx), %rax
	pushq	%rbp
	movq	$0, %rbp
	call	*%rax

lock1:
	.quad	0

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
  .word   31                              # sizeof(gdt) - 1
  .long   gdt                             # address gdt
