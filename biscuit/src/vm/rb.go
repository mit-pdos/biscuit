package vm

type Rbc_t int

const (
	RED   Rbc_t = iota
	BLACK Rbc_t = iota
)

type Rbh_t struct {
	root *Rbn_t
}

type Rbn_t struct {
	p   *Rbn_t
	r   *Rbn_t
	l   *Rbn_t
	c   Rbc_t
	vmi Vminfo_t
}

func (h *Rbh_t) _rol(nn *Rbn_t) {
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

func (h *Rbh_t) _ror(nn *Rbn_t) {
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

func (h *Rbh_t) _balance(nn *Rbn_t) {
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

func (h *Rbh_t) _insert(vmi *Vminfo_t) *Rbn_t {
	nn := &Rbn_t{vmi: *vmi, c: RED}
	if h.root == nil {
		h.root = nn
		h._balance(nn)
		return nn
	}

	n := h.root
	for {
		if vmi.Pgn > n.vmi.Pgn {
			if n.r == nil {
				n.r = nn
				nn.p = n
				break
			}
			n = n.r
		} else if vmi.Pgn < n.vmi.Pgn {
			if n.l == nil {
				n.l = nn
				nn.p = n
				break
			}
			n = n.l
		} else if n.vmi.Pgn == vmi.Pgn {
			return n
		}
	}
	h._balance(nn)
	return nn
}

func (h *Rbh_t) lookup(pgn uintptr) *Rbn_t {
	n := h.root
	for n != nil {
		pgend := n.vmi.Pgn + uintptr(n.vmi.Pglen)
		if pgn >= n.vmi.Pgn && pgn < pgend {
			break
		} else if n.vmi.Pgn < pgn {
			n = n.r
		} else {
			n = n.l
		}
	}
	return n
}

func (h *Rbh_t) _rembalance(par, nn *Rbn_t) {
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

func (h *Rbh_t) remove(nn *Rbn_t) *Rbn_t {
	old := nn
	fast := true
	var child *Rbn_t
	var par *Rbn_t
	var col Rbc_t
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
	return old
}
