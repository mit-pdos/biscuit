package proc

import "sync"

import "accnt"
import "defs"

// requirements for wait* syscalls (used for processes and threads):
// - wait for a pid that is not my child must fail
// - only one wait for a specific pid may succeed; others must fail
// - wait when there are no children must fail
// - wait for a process should not return thread info and vice versa
type Waitst_t struct {
	Pid    int
	Status int
	Atime  accnt.Accnt_t
	// true iff the exit status is valid
	Valid bool
}

type Wait_t struct {
	sync.Mutex
	pwait whead_t
	twait whead_t
	cond  *sync.Cond
	Pid   int
}

func (w *Wait_t) Wait_init(mypid int) {
	w.cond = sync.NewCond(w)
	w.Pid = mypid
}

type wlist_t struct {
	next *wlist_t
	wst  Waitst_t
}

type whead_t struct {
	head  *wlist_t
	count int
}

func (wh *whead_t) wpush(id int) {
	n := &wlist_t{}
	n.wst.Pid = id
	n.next = wh.head
	wh.head = n
	wh.count++
}

func (wh *whead_t) wpopvalid() (Waitst_t, bool) {
	var prev *wlist_t
	n := wh.head
	for n != nil {
		if n.wst.Valid {
			wh.wremove(prev, n)
			return n.wst, true
		}
		prev = n
		n = n.next
	}
	var zw Waitst_t
	return zw, false
}

// returns the previous element in the wait status singly-linked list (in order
// to remove the requested element), the requested element, and whether the
// requested element was found.
func (wh *whead_t) wfind(id int) (*wlist_t, *wlist_t, bool) {
	var prev *wlist_t
	ret := wh.head
	for ret != nil {
		if ret.wst.Pid == id {
			return prev, ret, true
		}
		prev = ret
		ret = ret.next
	}
	return nil, nil, false
}

func (wh *whead_t) wremove(prev, h *wlist_t) {
	if prev != nil {
		prev.next = h.next
	} else {
		wh.head = h.next
	}
	h.next = nil
	wh.count--
}

// returns the number of nodes in the linked list
func (w *Wait_t) Len() int {
	w.Lock()
	defer w.Unlock()
	ret := 0
	for p := w.pwait.head; p != nil; p = p.next {
		ret++
	}
	for p := w.twait.head; p != nil; p = p.next {
		ret++
	}
	return ret
}

// if there are more unreaped child statuses (procs or threads) than noproc,
// _start() returns false and id is not added to the status map.
func (w *Wait_t) _start(id int, isproc bool, noproc uint) bool {
	w.Lock()
	defer w.Unlock()
	if uint(w.pwait.count+w.twait.count) > noproc {
		return false
	}
	var wh *whead_t
	if isproc {
		wh = &w.pwait
	} else {
		wh = &w.twait
	}
	wh.wpush(id)
	return true
}

func (w *Wait_t) putpid(pid, status int, atime *accnt.Accnt_t) {
	w._put(pid, status, true, atime)
}

func (w *Wait_t) puttid(tid, status int, atime *accnt.Accnt_t) {
	w._put(tid, status, false, atime)
}

func (w *Wait_t) _put(id, status int, isproc bool, atime *accnt.Accnt_t) {
	w.Lock()
	defer w.Unlock()
	var wh *whead_t
	if isproc {
		wh = &w.pwait
	} else {
		wh = &w.twait
	}
	_, wn, ok := wh.wfind(id)
	if !ok {
		panic("id must exist")
	}
	wn.wst.Valid = true
	wn.wst.Status = status
	if atime != nil {
		wn.wst.Atime.Userns += atime.Userns
		wn.wst.Atime.Sysns += atime.Sysns
	}
	w.cond.Broadcast()
}

func (w *Wait_t) Reappid(pid int, noblk bool) (Waitst_t, defs.Err_t) {
	return w._reap(pid, true, noblk)
}

func (w *Wait_t) Reaptid(tid int, noblk bool) (Waitst_t, defs.Err_t) {
	return w._reap(tid, false, noblk)
}

func (w *Wait_t) _reap(id int, isproc bool, noblk bool) (Waitst_t, defs.Err_t) {
	if id == defs.WAIT_MYPGRP {
		panic("no imp")
	}
	var wh *whead_t
	if isproc {
		wh = &w.pwait
	} else {
		wh = &w.twait
	}

	w.Lock()
	defer w.Unlock()
	var zw Waitst_t
	for {
		if id == defs.WAIT_ANY {
			// XXXPANIC
			if wh.count < 0 {
				panic("neg childs")
			}
			if wh.count == 0 {
				return zw, -defs.ECHILD
			}
			if ret, ok := wh.wpopvalid(); ok {
				return ret, 0
			}
		} else {
			wp, wn, ok := wh.wfind(id)
			if !ok {
				return zw, -defs.ECHILD
			}
			if wn.wst.Valid {
				wh.wremove(wp, wn)
				return wn.wst, 0
			}
		}
		if noblk {
			return zw, 0
		}
		// wait for someone to exit
		if err := KillableWait(w.cond); err != 0 {
			return zw, err
		}
	}
}
