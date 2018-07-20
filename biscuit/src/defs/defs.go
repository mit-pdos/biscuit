package defs

type Inum_t int

type Tid_t int

const (
	DIVZERO  = 0
	UD       = 6
	GPFAULT  = 13
	PGFAULT  = 14
	TIMER    = 32
	SYSCALL  = 64
	TLBSHOOT = 70
	PERFMASK = 72

	IRQ_BASE = 32

	IRQ_KBD  = 1
	IRQ_COM1 = 4
	INT_KBD  = IRQ_BASE + IRQ_KBD
	INT_COM1 = IRQ_BASE + IRQ_COM1

	INT_MSI0 = 56
	INT_MSI1 = 57
	INT_MSI2 = 58
	INT_MSI3 = 59
	INT_MSI4 = 60
	INT_MSI5 = 61
	INT_MSI6 = 62
	INT_MSI7 = 63
)

const (
	TFSIZE    = 24
	TFREGS    = 17
	TF_FSBASE = 1
	TF_R13    = 4
	TF_R12    = 5
	TF_R8     = 9
	TF_RBP    = 10
	TF_RSI    = 11
	TF_RDI    = 12
	TF_RDX    = 13
	TF_RCX    = 14
	TF_RBX    = 15
	TF_RAX    = 16
	TF_TRAP   = TFREGS
	TF_ERROR  = TFREGS + 1
	TF_RIP    = TFREGS + 2
	TF_CS     = TFREGS + 3
	TF_RSP    = TFREGS + 5
	TF_SS     = TFREGS + 6
	TF_RFLAGS = TFREGS + 4
	TF_FL_IF  = 1 << 9
)
