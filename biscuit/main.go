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

func parcopy(to []byte, from []byte, done chan int) {
        for i, c := range from {
                to[i] = c
        }
        done <- 1
}

func sys_write(to []byte, p []byte) int {
        PGSIZE := 4096
        done := make(chan int)
        cnt := len(p)
        fmt.Printf("len is %v\n", cnt)

        for i := 0; i < cnt / PGSIZE; i++ {
                s := i*PGSIZE
                e := (i+1)*PGSIZE
                if cnt < e {
                        e = cnt
                }
                go parcopy(to[s:e], p[s:e], done)
        }

        left := cnt % PGSIZE
        if left != 0 {
                t := cnt - left
                for i := t; i < cnt; i++ {
                        to[i] = p[i]
                }
        }

        for i := 0; i < cnt / PGSIZE; i++ {
                <- done
        }

        return cnt
}

func ver(a []byte, b []byte) {
        for i, c := range b {
                if a[i] != c {
                        panic("bad")
                }
        }
}

func main_write() {
        to := make([]byte, 4096*1)
        from := make([]byte, 4096*1)

        for i, _ := range from {
                from[i] = byte(rand.Int())
        }

        sys_write(to, from)

        ver(to, from)
        fmt.Printf("done\n")
}
