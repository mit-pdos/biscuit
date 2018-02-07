package common

import "sync"

// requirements for wait* syscalls (used for processes and threads):
// - wait for a pid that is not my child must fail
// - only one wait for a specific pid may succeed; others must fail
// - wait when there are no children must fail
// - wait for a process should not return thread info and vice versa
type Waitst_t struct {
	pid		int
	status		int
	atime		Accnt_t
	// true iff the exit status is valid
	valid		bool
}

type Wait_t struct {
	sync.Mutex
	pwait		whead_t
	twait		whead_t
	cond		*sync.Cond
	pid		int
}

type wlist_t struct {
	next	*wlist_t
	wst	Waitst_t
}

type whead_t struct {
	head	*wlist_t
	count	int
}
