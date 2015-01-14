package main

import "fmt"
import "math/rand"
import "runtime"

type thread_t struct {
	valid	int
	tf	[23]uint64
}

const nthreads	int = 10

var threads	[nthreads]thread_t
var th_cur	int

func pancake(msg ...interface{}) {
	runtime.Cli()
	fmt.Print(msg)
	for {
	}
}

// trap cannot do anything that may have side-effects on the runtime (like
// fmt.Print, or use pancake!). the reason is that, by design, goroutines are
// scheduled cooperatively in the runtime. trap interrupts the runtime though,
// and then tries to execute more gocode, thus doing things the runtime did not
// expect.
//go:nosplit
func trap(tf *[23]uint64) {
	tfregs    := 16
	tf_trapno := tfregs
	//tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2

	TIMER := uint64(32)

	switch tf[tf_trapno] {
	case TIMER:
		// save current thread context
		threads[th_cur].tf = *tf

		tnext := func(t int) int {
			return (t + 1) % nthreads
		}
		next := tnext(th_cur)
		for ;threads[next].valid == 0; next = tnext(next) {
		}
		if threads[next].valid != 1 {
			runtime.Pnum(2)
			for {
			}
		}
		th_cur = next
		runtime.Lapic_eoi()
		runtime.Trapret(&threads[th_cur].tf)

	default:
		runtime.Pnum(3)
		runtime.Pnum(tf[tf_trapno])
		runtime.Pnum(tf[tf_rip])
		for {
		}
	}
}

func tdump(t thread_t) {
	tfregs := 16
	tf_rsp    := tfregs + 5
	tf_rip    := tfregs + 2
	fmt.Printf("RIP %x", t.tf[tf_rip])
	fmt.Printf("RSP %x", t.tf[tf_rsp])
}

func main() {
	// install trap handler. have to setup already existing threads to run
	// too
	runtime.Cli()

	runtime.Install_traphandler(trap)

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

	fmt.Printf("'network' test ")
	ch := make(chan packet)
	go genpackets(ch)

	process_packets(ch)
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

