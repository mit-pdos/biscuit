package fs

import "common"

/*
 * red-black tree based on niels provos' red-black tree macros
 */

const (
	RED   common.Rbc_t = iota
	BLACK common.Rbc_t = iota
)

type frbh_t struct {
	root  *frbn_t
	nodes int
}

type frbn_t struct {
	p   *frbn_t
	r   *frbn_t
	l   *frbn_t
	c   common.Rbc_t
	pgi *common.Bdev_block_t
}

func (h *frbh_t) _rol(nn *frbn_t) {
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

func (h *frbh_t) _ror(nn *frbn_t) {
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

func (h *frbh_t) _balance(nn *frbn_t) {
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

func (h *frbh_t) insert(pgi *common.Bdev_block_t) *frbn_t {
	nn := &frbn_t{pgi: pgi, c: RED}
	if h.root == nil {
		h.root = nn
		h._balance(nn)
		return nn
	}

	n := h.root
	for {
		if pgi.Block > n.pgi.Block {
			if n.r == nil {
				n.r = nn
				nn.p = n
				break
			}
			n = n.r
		} else if pgi.Block < n.pgi.Block {
			if n.l == nil {
				n.l = nn
				nn.p = n
				break
			}
			n = n.l
		} else if n.pgi.Block == pgi.Block {
			return n
		}
	}
	h.nodes++
	h._balance(nn)
	return nn
}

func (h *frbh_t) _lookup(pgn int) *frbn_t {
	n := h.root
	for n != nil {
		if pgn == n.pgi.Block {
			break
		} else if n.pgi.Block < pgn {
			n = n.r
		} else {
			n = n.l
		}
	}
	return n
}

func (h *frbh_t) lookup(pgn int) (*common.Bdev_block_t, bool) {
	r := h._lookup(pgn)
	if r == nil {
		return nil, false
	}
	return r.pgi, true
}

func (h *frbh_t) _rembalance(par, nn *frbn_t) {
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

func (h *frbh_t) remove(nn *frbn_t) *frbn_t {
	old := nn
	fast := true
	var child *frbn_t
	var par *frbn_t
	var col common.Rbc_t
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

func (h *frbh_t) iter1(n *frbn_t, f func(*common.Bdev_block_t)) {
	if n == nil {
		return
	}
	h.iter1(n.l, f)
	f(n.pgi)
	h.iter1(n.r, f)
}

func (h *frbh_t) iter(f func(*common.Bdev_block_t)) {
	h.iter1(h.root, f)
}

func (h *frbh_t) clear() {
	h.root = nil
	h.nodes = 0
}

type dc_rbh_t struct {
	root  *dc_rbn_t
	nodes int
}

type dc_rbn_t struct {
	p    *dc_rbn_t
	r    *dc_rbn_t
	l    *dc_rbn_t
	c    common.Rbc_t
	name string
	icd  icdent_t
}

func (h *dc_rbh_t) _rol(nn *dc_rbn_t) {
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

func (h *dc_rbh_t) _ror(nn *dc_rbn_t) {
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

func (h *dc_rbh_t) _balance(nn *dc_rbn_t) {
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

func (h *dc_rbh_t) insert(name string, icd icdent_t) {
	nn := &dc_rbn_t{name: name, icd: icd, c: RED}
	if h.root == nil {
		h.root = nn
		h._balance(nn)
		return
	}

	n := h.root
	for {
		if name > n.name {
			if n.r == nil {
				n.r = nn
				nn.p = n
				break
			}
			n = n.r
		} else if name < n.name {
			if n.l == nil {
				n.l = nn
				nn.p = n
				break
			}
			n = n.l
		} else {
			return
		}
	}
	h.nodes++
	h._balance(nn)
	return
}

func (h *dc_rbh_t) _lookup(name string) *dc_rbn_t {
	n := h.root
	if n == nil {
		return nil
	}
	for n != nil {
		if name < n.name {
			n = n.l
		} else if name > n.name {
			n = n.r
		} else {
			break
		}
	}
	return n
}

func (h *dc_rbh_t) lookup(name string) (icdent_t, bool) {
	r := h._lookup(name)
	if r == nil {
		var zi icdent_t
		return zi, false
	}
	return r.icd, true
}

func (h *dc_rbh_t) _rembalance(par, nn *dc_rbn_t) {
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

func (h *dc_rbh_t) remove(name string) {
	n := h._lookup(name)
	if n == nil {
		panic("no such icd")
	}
	h._remove(n)
}

func (h *dc_rbh_t) _remove(nn *dc_rbn_t) *dc_rbn_t {
	old := nn
	fast := true
	var child *dc_rbn_t
	var par *dc_rbn_t
	var col common.Rbc_t
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

func (h *dc_rbh_t) iter1(n *dc_rbn_t, f func(string, icdent_t) bool) bool {
	if n == nil {
		return false
	}
	if h.iter1(n.l, f) || f(n.name, n.icd) || h.iter1(n.r, f) {
		return true
	}
	return false
}

// returns true if at least one call to f returned true. stops iterating once f
// returns true.
func (h *dc_rbh_t) iter(f func(string, icdent_t) bool) bool {
	return h.iter1(h.root, f)
}

func (h *dc_rbh_t) clear() {
	h.root = nil
	h.nodes = 0
}
