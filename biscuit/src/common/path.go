package common

import "strings"

// allocation-less pathparts
type Pathparts_t struct {
	path string
	loc  int
}

func (pp *Pathparts_t) Pp_init(path string) {
	pp.path = path
	pp.loc = 0
}

func (pp *Pathparts_t) Next() (string, bool) {
	ret := ""
	for ret == "" {
		if pp.loc == len(pp.path) {
			return "", false
		}
		ret = pp.path[pp.loc:]
		nloc := strings.IndexByte(ret, '/')
		if nloc != -1 {
			ret = ret[:nloc]
			pp.loc += nloc + 1
		} else {
			pp.loc += len(ret)
		}
	}
	return ret, true
}

func IsAbsolute(path string) bool {
	return strings.HasPrefix(path, "/")
}

func Sdirname(path string) (string, string) {
	fn := path
	l := len(fn)
	// strip all trailing slashes
	for i := l - 1; i >= 0; i-- {
		if fn[i] != '/' {
			break
		}
		fn = fn[:i]
		l--
	}
	s := ""
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

// Wouldn't work for POSIX symbolic links and "..", but Biscuit doesn't support symbolic links
func CanonicalizeCopy(path string) string {
	pp := Pathparts_t{}
	pp.Pp_init(path)
	p := "."
	if IsAbsolute(path) {
		p = "/"
	}

	for cp, ok := pp.Next(); ok; cp, ok = pp.Next() {
		if cp == "." {
			continue
		} else if cp == ".." {
			l := strings.LastIndexAny(p, "/")
			if l == -1 {
			} else if l == 0 {
				p = p[:1]
			} else {
				p = p[:l]
			}
		} else {
			if p != "/" {
				p += "/"
			}
			p += cp
		}
	}
	return p
}

const MaxSlash = 60

type canonicalize_t struct {
	path  []rune
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

func (canon *canonicalize_t) add(r rune) {
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
func Canonicalize(path string) string {
	// fmt.Printf("canon: %s\n", path)
	canon := canonicalize_t{}
	canon.slash = make([]int, MaxSlash)
	canon.path = []rune(path)
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
	path = string(canon.path[:canon.index])
	// fmt.Printf("res: %s\n", path)
	return path
}
