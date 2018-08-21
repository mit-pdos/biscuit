package ustr

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

var DotDot = Ustr{'.', '.'}

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
