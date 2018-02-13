package main

import "fmt"
import "math/rand"
import "sync"
import "runtime"

type packet struct {
	ipaddr  int
	payload int
}

func wtf(tf *uint64) {
	fmt.Printf("WTF!")
	for {
	}
}

func main() {
	runtime.Install_traphandler(wtf)
	for {
		fmt.Printf("hi ")
		for i := 0; i < 1000000; i++ {
		}
	}
}

func mainip() {
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
		p := <-in
		spawnsend(p, outbound, ip_process)
	}
}

func ip_process(ipchan chan packet) {
	for {
		p := <-ipchan
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

	for i := 0; i < cnt/PGSIZE; i++ {
		s := i * PGSIZE
		e := (i + 1) * PGSIZE
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

	for i := 0; i < cnt/PGSIZE; i++ {
		<-done
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

type bnode struct {
	fd   int
	data int
	l    *bnode
	r    *bnode
}

func binsert(root *bnode, nfd int, ndata int) {
	if root == nil {
		panic("nil root")
	}

	if nfd < root.fd {
		if root.l == nil {
			root.l = &bnode{nfd, ndata, nil, nil}
			return
		}
		binsert(root.l, nfd, ndata)
	} else if nfd > root.fd {
		if root.r == nil {
			root.r = &bnode{nfd, ndata, nil, nil}
			return
		}
		binsert(root.r, nfd, ndata)
	}
}

func blookup(root *bnode, fd int) *bnode {
	if root == nil {
		return nil
	} else if fd < root.fd {
		return blookup(root.l, fd)
	} else if fd > root.fd {
		return blookup(root.r, fd)
	}

	return root
}

func bprint(root *bnode) {
	if root == nil {
		return
	}

	bprint(root.l)
	fmt.Printf("%v ", root.fd)
	bprint(root.r)
}

func bcount(root *bnode) int {
	if root == nil {
		return 0
	}
	return 1 + bcount(root.l) + bcount(root.r)
}

func bcopy(root *bnode, par *bnode) *bnode {
	if root == nil {
		return nil
	}

	ret := bnode{root.fd, root.data, nil, nil}

	if par != nil {
		if par.fd > ret.fd {
			par.l = &ret
		} else {
			par.r = &ret
		}
	}

	bcopy(root.l, &ret)
	bcopy(root.r, &ret)
	return &ret
}

func reader(cnt int, ch chan int) {
	t := 0
	oldc := cnt

	for {
		nc := bcount(&root)
		if nc < oldc {
			panic("bad count")
		}
		if nc != oldc {
			oldc = nc
			fmt.Printf("%v ", oldc)
		}
		t++

		if t%100000 == 0 {
			ch <- 1
		}
	}
}

func rup() {
	wlock.Lock()
	defer wlock.Unlock()

	i := rand.Intn(100)
	for ; blookup(&root, i) != nil; i = rand.Intn(100) {
	}

	newroot := bcopy(&root, nil)
	binsert(newroot, i, 0)
	fmt.Printf("(inserted %v) ", i)
	root = *newroot
}

type addr int
type route int

var route_table *map[addr]*route

func route_get(dst addr) *route {
	if r, ok := (*route_table)[dst]; ok {
		return r
	}

	return nil
}

func route_insert(r *route, dst addr) {
	rtlock.Lock()
	defer rtlock.Unlock()

	newrt := copy_table()
	(*newrt)[dst] = r

	route_table = newrt
}

func copy_table() *map[addr]*route {
	ret := *route_table
	return &ret
}

var rtlock sync.Mutex

var wlock sync.Mutex
var root bnode

func main_rcu() {
	root.fd = 50
	for i := 0; i < 9; i++ {
		binsert(&root, rand.Intn(100), 0)
	}
	cnt := bcount(&root)
	ch := make(chan int)
	go reader(cnt, ch)

	for {
		<-ch
		rup()
	}
	fmt.Printf("done")
}
