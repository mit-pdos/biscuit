package common

type Ustr []uint8

func (us Ustr) Isdot() bool {
	return len(us) == 1 && us[0] == '.'
}

func (us Ustr) Isdotdot() bool {
	return len(us) == 2 && us[0] == '.' && us[1] == '.'
}

func (us Ustr) Eq(s Ustr) bool {
	if len(us) != len(s) {
		return false
	}
	for i, v := range us {
		if v != s[i] {
			return false
		}
	}
	return true
}

func MkUstr() Ustr {
	us := Ustr{}
	return us
}

func MkUstrDot() Ustr {
	us := Ustr(".")
	return us
}

func MkUstrRoot() Ustr {
	us := Ustr("/")
	return us
}

func MkUstrDotDot() Ustr {
	us := Ustr{'.', '.'}
	return us
}

func MkUstrSlice(buf []uint8) Ustr {
	for i := 0; i < len(buf); i++ {
		if buf[i] == uint8(0) {
			return buf[:i]
		}
	}
	return buf
}

func (us Ustr) Extend(p Ustr) Ustr {
	tmp := make(Ustr, len(us))
	copy(tmp, us)
	r := append(tmp, '/')
	return append(r, p...)
}

func (us Ustr) ExtendStr(p string) Ustr {
	return us.Extend(Ustr(p))
}

func (us Ustr) IsAbsolute() bool {
	if len(us) == 0 {
		return false
	}
	return us[0] == '/'
}

func (us Ustr) IndexByte(b uint8) int {
	for i, v := range us {
		if v == b {
			return i
		}
	}
	return -1
}

func (us Ustr) String() string {
	return string(us)
}

// allocation-less pathparts
type Pathparts_t struct {
	path Ustr
	loc  int
}

func (pp *Pathparts_t) Pp_init(path Ustr) {
	pp.path = path
	pp.loc = 0
}

func (pp *Pathparts_t) Next() (Ustr, bool) {
	ret := MkUstr()
	for len(ret) == 0 {
		if pp.loc == len(pp.path) {
			return MkUstr(), false
		}
		ret = pp.path[pp.loc:]
		nloc := Ustr.IndexByte(ret, '/')
		if nloc != -1 {
			ret = ret[:nloc]
			pp.loc += nloc + 1
		} else {
			pp.loc += len(ret)
		}
	}
	return ret, true
}

func Sdirname(path Ustr) (Ustr, Ustr) {
	fn := path
	l := len(fn)
	// strip all trailing slashes
	for i := l - 1; i >= 0; i-- {
		if fn[i] != uint8('/') {
			break
		}
		fn = fn[:i]
		l--
	}
	var s Ustr
	for i := l - 1; i >= 0; i-- {
		if fn[i] == '/' {
			// remove the rightmost slash only if it is not the
			// first char (the root).
			if i == 0 {
				s = fn[0:1]
			} else {
				s = fn[:i]
			}
			fn = fn[i+1:]
			break
		}
	}

	return s, fn
}

const MaxSlash = 60

type canonicalize_t struct {
	path  Ustr
	slash []int
	d     int
	index int
}

func (canon *canonicalize_t) reset(d int) {
	if d >= 0 {
		canon.index = canon.slash[d] + 1
		canon.d = d
	} else {
		if canon.path[0] == '/' {
			canon.index = 1
			canon.d = 1
		} else {
			canon.d = 0
			canon.index = 0
		}
	}
	// fmt.Printf("reset index to %d %d\n", d, canon.index)
}

func (canon *canonicalize_t) add(r uint8) {
	canon.path[canon.index] = r
	canon.index++
}

func (canon *canonicalize_t) addslash() {
	if canon.index > 0 && canon.path[canon.index-1] == '/' {
		// skip /
	} else {
		canon.slash[canon.d] = canon.index
		canon.d++
		canon.add('/')
	}
}

func (canon *canonicalize_t) deltrailingslash() {
	if canon.index > 1 && canon.path[canon.index-1] == '/' {
		canon.index--
	}
}

// Assume utf encoding of characters
func Canonicalize(path Ustr) Ustr {
	// fmt.Printf("canon: %s\n", path)
	canon := canonicalize_t{}
	canon.slash = make([]int, MaxSlash)
	canon.path = path
	lastisdot := false
	lastisdotdot := false
	for _, u := range path {
		// fmt.Printf("%d %#U d %d %v\n", i, u, canon.d, canon.slash)
		if u == '/' {
			if lastisdot {
				canon.reset(canon.d - 1)
			}
			if lastisdotdot {
				canon.reset(canon.d - 2)
			}
			canon.addslash()
			lastisdot = false
			lastisdotdot = false
		} else if u == '.' {
			if lastisdot {
				lastisdotdot = true
				lastisdot = false
			} else {
				lastisdot = true
			}
			continue
		} else {
			if lastisdot {
				canon.add('.')
				lastisdot = false
			}
			if lastisdotdot {
				canon.add('.')
				canon.add('.')
				lastisdotdot = false
			}
			canon.add(u)
		}
	}
	if lastisdotdot {
		canon.reset(canon.d - 2)
	}
	canon.deltrailingslash()
	path = canon.path[:canon.index]
	// fmt.Printf("res: %s\n", path)
	return path
}
