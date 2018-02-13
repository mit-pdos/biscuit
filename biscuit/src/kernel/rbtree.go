package main

const (
	black = 0
	red   = 1
	dead  = 2
)

type rb_t struct {
	l     *rb_t
	r     *rb_t
	p     *rb_t
	color int
	k     int
	v     unsafe.Pointer
}

type rb_root struct {
	n *rb_t
}

func gp(n *rb_t) *rb_t {
	if n != nil && n.p != nil {
		return n.p.p
	}

	return nil
}

func uncle(n *rb_t) *rb_t {
	g := gp(n)
	if g != nil {
		if g.r == n.p {
			return g.l
		} else {
			return g.r
		}
	}
	return nil
}

func sibling(n *rb_t) *rb_t {
	if n != nil && n.p != nil {
		if n.p.l == n {
			return n.p.r
		} else {
			return n.p.l
		}
	}
	return nil
}

func mkleaf(p *rb_t) *rb_t {
	ret := &rb_t{}
	ret.color = black
	ret.p = p
	return ret
}

func rotatel(n *rb_t, root *rb_root) {
	p := n.p
	temp := n.r
	n.r = temp.l
	if n.r != nil {
		n.r.p = n
	}
	temp.l = n
	if temp.l != nil {
		temp.l.p = temp
	}
	temp.p = p
	if p != nil && p.r == n {
		p.r = temp
	} else if p != nil {
		p.l = temp
	}
	if temp.p == nil {
		root.n = temp
	}
}

func rotater(n *rb_t, root *rb_root) {
	p := n.p
	temp := n.l
	n.l = temp.r
	if n.l != nil {
		n.l.p = n
	}
	temp.r = n
	if temp.r != nil {
		temp.r.p = temp
	}
	temp.p = p
	if p != nil && p.r == n {
		p.r = temp
	} else if p != nil {
		p.l = temp
	}
	if temp.p == nil {
		root.n = temp
	}
}

func insert5(n *rb_t, root *rb_root) {
	g := gp(n)
	n.p.color = black
	g.color = red
	if n == n.p.l {
		rotater(g, root)
	} else {
		rotatel(g, root)
	}
}

func insert4(n *rb_t, root *rb_root) {
	g := gp(n)
	if n == n.p.r && n.p == g.l {
		rotatel(n.p, root)
		n = n.l
	} else if n == n.p.l && n.p == g.r {
		rotater(n.p, root)
		n = n.r
	}

	insert5(n, root)
}

func insert3(n *rb_t, root *rb_root) {
	u := uncle(n)
	if u != nil && u.color == red {
		n.p.color = black
		u.color = black
		g := gp(n)
		g.color = red
		insert1(g, root)
		return
	}
	insert4(n, root)
}

func insert2(n *rb_t, root *rb_root) {
	if n.p.color == black {
		return
	}
	insert3(n, root)
}

func insert1(n *rb_t, root *rb_root) {
	if n.p == nil {
		n.color = black
		return
	}
	insert2(n, root)
}

func binsert(root *rb_root, k int, v unsafe.Pointer) *rb_t {
	if root.n == nil {
		n := mkleaf(nil)
		n.color = red
		n.k = k
		n.v = v
		root.n = n
		return n
	}

	h := root.n
	prev := h
	for h != nil {
		prev = h
		if k < h.k {
			h = h.l
		} else if k > h.k {
			h = h.r
		} else {
			return h
		}
	}
	h = mkleaf(prev)
	if k < prev.k {
		prev.l = h
	} else {
		prev.r = h
	}
	if h.color != black {
		panic("weird color")
	}
	h.color = red
	h.v = v
	h.k = k
	return h
}

func (root *rb_root) rbinsert(k int, v unsafe.Pointer) {
	n := binsert(root, k, v)
	insert1(n, root)
}

func (root *rb_root) rblookup(k int) *rb_t {
	ret := root.n
	for ret != nil {
		if ret.k > k {
			ret = ret.l
		} else if ret.k < k {
			ret = ret.r
		} else {
			return ret
		}
	}
	return nil
}

func (root *rb_root) rbkill(k int) {
	n := root.rblookup(k)
	if n != nil {
		n.color = dead
		n.v = nil
	}
}

func rblen1(n *rb_t) int {
	if n == nil {
		return 0
	}
	ret := 1
	if n.color == dead {
		ret = 0
	}
	return rblen1(n.r) + rblen1(n.l) + ret
}

func (root *rb_root) rblen() int {
	return rblen1(root.n)
}

func prt(n *rb_t) {
	if n == nil {
		return
	}
	prt(n.l)
	fmt.Printf("k: %v, v: %v\n", n.k, n.v)
	prt(n.r)
}

func rb_test() {
	r := rb_root{}
	for i := 0; i < 30; i++ {
		d := rand.Intn(1000)
		v := unsafe.Pointer(uintptr(d))
		r.rbinsert(d, v)
	}
	prt(r.n)

	r = rb_root{}
	m := make(map[int]unsafe.Pointer)
	for i := 0; i < 100000; i++ {
		k := rand.Int()
		v := unsafe.Pointer(uintptr(rand.Int()))
		m[k] = v
		r.rbinsert(k, v)
	}
	dur := 0
	for k, v := range m {
		n := r.rblookup(k)
		if n.v != v {
			panic("bad lookup")
		}
		dur++
	}
	fmt.Printf("checked %v values\n", dur)
}
