package fd

import "sync"

import "bpath"
import "defs"
import "fdops"
import "ustr"

const (
	FD_READ    = 0x1
	FD_WRITE   = 0x2
	FD_CLOEXEC = 0x4
)

type Fd_t struct {
	// fops is an interface implemented via a "pointer receiver", thus fops
	// is a reference, not a value
	Fops  fdops.Fdops_i
	Perms int
}

func Copyfd(fd *Fd_t) (*Fd_t, defs.Err_t) {
	nfd := &Fd_t{}
	*nfd = *fd
	err := nfd.Fops.Reopen()
	if err != 0 {
		return nil, err
	}
	return nfd, 0
}

func Close_panic(f *Fd_t) {
	if f.Fops.Close() != 0 {
		panic("must succeed")
	}
}

type Cwd_t struct {
	sync.Mutex // to serialize chdirs
	Fd         *Fd_t
	Path       ustr.Ustr
}

func (cwd *Cwd_t) Fullpath(p ustr.Ustr) ustr.Ustr {
	if p.IsAbsolute() {
		return p
	} else {
		full := append(cwd.Path, '/')
		return append(full, p...)
	}
}

func (cwd *Cwd_t) Canonicalpath(p ustr.Ustr) ustr.Ustr {
	p1 := cwd.Fullpath(p)
	return bpath.Canonicalize(p1)
}

func MkRootCwd(fd *Fd_t) *Cwd_t {
	c := &Cwd_t{}
	c.Fd = fd
	c.Path = ustr.MkUstrRoot()
	return c
}
