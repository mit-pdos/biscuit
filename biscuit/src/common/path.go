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
func Canonicalize(path string) string {
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
