package main

import "fmt"
import "math/rand"

type packet struct {
	ipaddr 		int
	payload		int
}

func main() {
	fmt.Printf("'network' test ")
	ch := make(chan packet)
	go genpackets(ch)

	process_packets(ch)
}

func genpackets(ch chan packet) {
	for {
		n := packet{rand.Intn(1000), 0}
		ch <- n
	}
}

func process_packets(in chan packet) {
	outbound := make(map[int]chan packet)

	for {
		p := <- in
		pri := p_priority(p)
		if ch, ok := outbound[pri]; ok {
			ch <- p
		} else {
			pch := make(chan packet)
			outbound[pri] = pch
			go ip_process(pch)
			pch <- p
		}
	}
}

func p_priority(p packet) int {
	return p.ipaddr / 100
}

func ip_process(ipchan chan packet) {
	for {
		p := <- ipchan
		if rand.Intn(1000) == 1 {
			fmt.Printf("%v ", p_priority(p))
		}
	}
}
