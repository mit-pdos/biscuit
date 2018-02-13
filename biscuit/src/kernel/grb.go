package main

type rbnode_i interface {
	less(interface{}) bool
	equal(interface{}) bool
}

type grbh_t struct {
	root  *grbn_t
	nodes int
}

type grbn_t struct {
	p    *grbn_t
	r    *grbn_t
	l    *grbn_t
	c    rbc_t
	priv rbnode_i
}

func (h *grbh_t) _rol(nn *grbn_t) {
	tmp := nn.r
	nn.r = tmp.l
	if nn.r != nil {
		tmp.l.p = nn
	}
	tmp.p = nn.p
	if tmp.p != nil {
		if nn == nn.p.l {
			nn.p.l = tmp
		} else {
			nn.p.r = tmp
		}
	} else {
		h.root = tmp
	}
	tmp.l = nn
	nn.p = tmp
}

func (h *grbh_t) _ror(nn *grbn_t) {
	tmp := nn.l
	nn.l = tmp.r
	if nn.l != nil {
		tmp.r.p = nn
	}
	tmp.p = nn.p
	if tmp.p != nil {
		if nn == nn.p.l {
			nn.p.l = tmp
		} else {
			nn.p.r = tmp
		}
	} else {
		h.root = tmp
	}
	tmp.r = nn
	nn.p = tmp
}

func (h *grbh_t) _balance(nn *grbn_t) {
	for par := nn.p; par != nil && par.c == RED; par = nn.p {
		gp := par.p
		if par == gp.l {
			tmp := gp.r
			if tmp != nil && tmp.c == RED {
				tmp.c = BLACK
				par.c = BLACK
				gp.c = RED
				nn = gp
				continue
			}
			if par.r == nn {
				h._rol(par)
				tmp = par
				par = nn
				nn = tmp
			}
			par.c = BLACK
			gp.c = RED
			h._ror(gp)
		} else {
			tmp := gp.l
			if tmp != nil && tmp.c == RED {
				tmp.c = BLACK
				par.c = BLACK
				gp.c = RED
				nn = gp
				continue
			}
			if par.l == nn {
				h._ror(par)
				tmp = par
				par = nn
				nn = tmp
			}
			par.c = BLACK
			gp.c = RED
			h._rol(gp)
		}
	}
	h.root.c = BLACK
}

func (h *grbh_t) insert(priv rbnode_i) *grbn_t {
	nn := &grbn_t{priv: priv, c: RED}
	if h.root == nil {
		h.root = nn
		h._balance(nn)
		return nn
	}

	n := h.root
	for {
		if !priv.less(n.priv) {
			if n.r == nil {
				n.r = nn
				nn.p = n
				break
			}
			n = n.r
		} else if priv.equal(n.priv) {
			return n
		} else {
			if n.l == nil {
				n.l = nn
				nn.p = n
				break
			}
			n = n.l
		}
	}
	h.nodes++
	h._balance(nn)
	return nn
}

func (h *grbh_t) _lookup(priv rbnode_i) *grbn_t {
	n := h.root
	for n != nil {
		if priv.equal(n.priv) {
			break
		} else if n.priv.less(priv) {
			n = n.r
		} else {
			n = n.l
		}
	}
	return n
}

func (h *grbh_t) lookup(priv rbnode_i) (interface{}, bool) {
	r := h._lookup(priv)
	if r == nil {
		return nil, false
	}
	return r.priv, true
}

func (h *grbh_t) _rembalance(par, nn *grbn_t) {
	for (nn == nil || nn.c == BLACK) && nn != h.root {
		if par.l == nn {
			tmp := par.r
			if tmp.c == RED {
				tmp.c = BLACK
				par.c = RED
				h._rol(par)
				tmp = par.r
			}
			if (tmp.l == nil || tmp.l.c == BLACK) &&
				(tmp.r == nil || tmp.r.c == BLACK) {
				tmp.c = RED
				nn = par
				par = nn.p
			} else {
				if tmp.r == nil || tmp.r.c == BLACK {
					oleft := tmp.l
					if oleft != nil {
						oleft.c = BLACK
					}
					tmp.c = RED
					h._ror(tmp)
					tmp = par.r
				}
				tmp.c = par.c
				par.c = BLACK
				if tmp.r != nil {
					tmp.r.c = BLACK
				}
				h._rol(par)
				nn = h.root
				break
			}
		} else {
			tmp := par.l
			if tmp.c == RED {
				tmp.c = BLACK
				par.c = RED
				h._ror(par)
				tmp = par.l
			}
			if (tmp.l == nil || tmp.l.c == BLACK) &&
				(tmp.r == nil || tmp.r.c == BLACK) {
				tmp.c = RED
				nn = par
				par = nn.p
			} else {
				if tmp.l == nil || tmp.l.c == BLACK {
					oright := tmp.r
					if oright != nil {
						oright.c = BLACK
					}
					tmp.c = RED
					h._rol(tmp)
					tmp = par.l
				}
				tmp.c = par.c
				par.c = BLACK
				if tmp.l != nil {
					tmp.l.c = BLACK
				}
				h._ror(par)
				nn = h.root
				break
			}
		}
	}
	if nn != nil {
		nn.c = BLACK
	}
}

func (h *grbh_t) remove(nn *grbn_t) *grbn_t {
	old := nn
	fast := true
	var child *grbn_t
	var par *grbn_t
	var col rbc_t
	if nn.l == nil {
		child = nn.r
	} else if nn.r == nil {
		child = nn.l
	} else {
		nn = nn.r
		left := nn.l
		for left != nil {
			nn = left
			left = nn.l
		}
		child = nn.r
		par = nn.p
		col = nn.c
		if child != nil {
			child.p = par
		}
		if par != nil {
			if par.l == nn {
				par.l = child
			} else {
				par.r = child
			}
		} else {
			h.root = child
		}
		if nn.p == old {
			par = nn
		}
		nn.p = old.p
		nn.l = old.l
		nn.r = old.r
		nn.c = old.c
		if old.p != nil {
			if old.p.l == old {
				old.p.l = nn
			} else {
				old.p.r = nn
			}
		} else {
			h.root = nn
		}
		old.l.p = nn
		if old.r != nil {
			old.r.p = nn
		}
		fast = false
	}
	if fast {
		par = nn.p
		col = nn.c
		if child != nil {
			child.p = par
		}
		if par != nil {
			if par.l == nn {
				par.l = child
			} else {
				par.r = child
			}
		} else {
			h.root = child
		}
	}
	if col == BLACK {
		h._rembalance(par, child)
	}
	h.nodes--
	return old
}

func (h *grbh_t) iter1(n *grbn_t, f func(interface{})) {
	if n == nil {
		return
	}
	h.iter1(n.l, f)
	f(n.priv)
	h.iter1(n.r, f)
}

func (h *grbh_t) iter(f func(interface{})) {
	h.iter1(h.root, f)
}

func (h *grbh_t) clear() {
	h.root = nil
	h.nodes = 0
}
