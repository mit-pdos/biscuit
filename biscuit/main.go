package main

import "fmt"
import "math/rand"
import "runtime"

type thread_t struct {
	valid	int
	tf	[23]uint64
	user	int
}

const nthreads	int = 10

var threads	[nthreads]thread_t
var th_cur	int

type trapstore_t struct {
	trapno	int
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
func trapstub(tf *[23]uint64) {

	tfregs    := 16
	tf_trapno := tfregs
	//tf_rsp    := tfregs + 5
	//tf_rip    := tfregs + 2
	//tf_rflags   := tfregs + 4
	//fl_intf     := uint64(1 << 9)

	phack := func(c uint64) {
		runtime.Pnum(c)
		for {
		}
	}

	// add to trap circular buffer for actual trap handler
	if tsnext(tshead) == tstail {
		phack(4)
	}
	trapno := tf[tf_trapno]
	trapstore[tshead].trapno = int(trapno)
	tshead = tsnext(tshead)

	//TIMER   := uint64(32)
	//GPFAULT := uint64(13)
	//PGFAULT := uint64(14)

	// if kernel go code causes an exception and cannot be restarted, our
	// trap handler will never get to run, thus such a condition is a
	// critical error
	//if trapno != TIMER && threads[th_cur].user == 0 {
	//	runtime.Pnum(trapno)
	//	runtime.Pnum(tf[tf_rip])
	//	phack(3)
	//}

	runtime.Yieldy()
}

func trap(handlers map[int]func()) {
	for {
		for tstail == tshead {
			// no work
			runtime.Gosched()
		}

		curtrap := trapstore[tstail].trapno
		tstail = tsnext(tstail)

		if h, ok := handlers[curtrap]; ok {
			go h()
			continue
		}
		fmt.Print("no handler for trap", curtrap)
	}
}

func trap_timer() {
	fmt.Printf("Timer!")
}

func trap_die() {
	pancake("trap die")
}

func main() {
	// install trap handler. have to setup already existing threads to run
	// too
	runtime.Cli()

	runtime.Install_traphandler(trapstub)

	ctf := 0
	for i, _ := range threads {
		if runtime.Tf_get(i, &threads[ctf].tf) == 0 {
			threads[ctf].valid = 1
			ctf++
		}
	}
	fmt.Printf("grandfathered %v threads", ctf)

	th_cur = runtime.Current_thread()
	if threads[th_cur].valid != 1 {
		pancake("current thread invalid")
	}
	runtime.Sti()

	handlers := map[int]func() { 32: trap_timer, 14:trap_die, 13:trap_die}
	go trap(handlers)

	fmt.Printf("'network' test ")
	ch := make(chan packet)
	go genpackets(ch)

	process_packets(ch)

	//for {
	//	fmt.Printf("hi ")
	//	for i := 0; i < 100000; i++ {
	//	}
	//}
}

func pancake(msg ...interface{}) {
	runtime.Cli()
	fmt.Print(msg)
	for {
	}
}

func tdump(t thread_t) {
	tfregs := 16
	tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	fmt.Printf("RIP %x", t.tf[tf_rip])
	fmt.Printf("RSP %x", t.tf[tf_rsp])
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
			var p *int
			*p = 0
		}
	}
}

