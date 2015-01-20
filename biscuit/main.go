package main

import "fmt"
import "math/rand"
import "runtime"

type trapstore_t struct {
	trapno	int
	ucookie int
}
const ntrapst	int = 64
var trapstore [ntrapst]trapstore_t
var tshead	int
var tstail	int

func tsnext(c int) int {
	return (c + 1) % ntrapst
}

// trap cannot do anything that may have side-effects on the runtime (like
// fmt.Print, or use pancake!). the reason is that, by design, goroutines are
// scheduled cooperatively in the runtime. trap interrupts the runtime though,
// and then tries to execute more gocode, thus doing things the runtime did not
// expect.
//go:nosplit
func trapstub(tf *[23]uint64, ucookie int) {

	tfregs    := 16
	tf_trapno := tfregs
	//tf_rsp    := tfregs + 5
	//tf_rip    := tfregs + 2
	//tf_rflags   := tfregs + 4
	//fl_intf     := uint64(1 << 9)

	// add to trap circular buffer for actual trap handler
	if tsnext(tshead) == tstail {
		runtime.Pnum(4)
		for {
		}
	}
	trapno := tf[tf_trapno]
	trapstore[tshead].trapno = int(trapno)
	trapstore[tshead].ucookie = ucookie
	tshead = tsnext(tshead)

	SYSCALL   := uint64(64)
	TIMER     := uint64(32)
	//GPFAULT   := uint64(13)
	//PGFAULT   := uint64(14)

	if trapno == SYSCALL {
		// yield until the syscall is handled
		runtime.Useryield()
	} else if trapno == TIMER {
		// timer interrupts are not exposed to this traphandler yet
		runtime.Pnum(0x41)
		for {
		}
	} else {
		runtime.Pnum(trapno)
		runtime.Pnum(0x42)
		for {
		}
	}
}

func trap(handlers map[int]func()) {
	for {
		for tstail == tshead {
			// no work
			runtime.Gosched()
		}

		curtrap := trapstore[tstail].trapno
		uc := trapstore[tstail].ucookie
		tstail = tsnext(tstail)

		if h, ok := handlers[curtrap]; ok {
			fmt.Printf("[trap %v] ", curtrap)
			go h()
			continue
		}
		fmt.Printf("no handler for trap %v, ucookie %x ", curtrap, uc)
	}
}

func trap_timer() {
	fmt.Printf("Timer!")
}

func adduser() {
	fmt.Printf("add 'user' prog ")

	var tf [23]uint64
	tfregs    := 16
	tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	tf_rflags := tfregs + 4
	fl_intf     := uint64(1 << 9)
	tf_ss     := tfregs + 6
	tf_cs     := tfregs + 3

	tf[tf_rip] = runtime.Fnaddr(runtime.Turdyprog)
	tf[tf_rsp] = runtime.Newstack()
	tf[tf_rflags] = fl_intf
	tf[tf_ss] = 2 << 3
	tf[tf_cs] = 1 << 3

	runtime.Useradd(&tf, 0x31337)
}

func main() {
	// magic loop
	//if rand.Int() != 0 {
	//	for {
	//	}
	//}

	runtime.Install_traphandler(trapstub)

	trap_diex := func(c int) func() {
		return func() {
			fmt.Printf("[death on trap %v] ", c)
			pancake("perished")
		}
	}

	handlers := map[int]func() { 32: trap_timer, 14:trap_diex(14), 13:trap_diex(13)}
	go trap(handlers)

	adduser()

	fmt.Printf("'network' test ")
	ch := make(chan packet)
	go genpackets(ch)

	process_packets(ch)
}

func pancake(msg ...interface{}) {
	runtime.Cli()
	fmt.Print(msg)
	for {
	}
}

type packet struct {
	ipaddr 		int
	payload		int
}

func genpackets(ch chan packet) {
	for {
		n := packet{rand.Intn(1000), 0}
		ch <- n
	}
}

func spawnsend(p packet, outbound map[int]chan packet, f func(chan packet)) {
	pri := p_priority(p)
	if ch, ok := outbound[pri]; ok {
		ch <- p
	} else {
		pch := make(chan packet)
		outbound[pri] = pch
		go f(pch)
		pch <- p
	}
}

func p_priority(p packet) int {
	return p.ipaddr / 100
}

func process_packets(in chan packet) {
	outbound := make(map[int]chan packet)

	for {
		p := <- in
		spawnsend(p, outbound, ip_process)
	}
}

func ip_process(ipchan chan packet) {
	for {
		p := <- ipchan
		if rand.Intn(1000) == 1 {
			fmt.Printf("%v ", p_priority(p))
		}
	}
}

