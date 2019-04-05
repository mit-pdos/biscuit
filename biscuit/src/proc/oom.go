package proc

import "fmt"
import "runtime"
import "time"

import "oommsg"
import "res"

type oom_t struct {
	halp   chan oommsg.Oommsg_t
	evict  func() (int, int)
	lastpr time.Time
}

var Oom *oom_t = &oom_t{halp: oommsg.OomCh}

func Oom_init(evict func() (int, int)) {
	Oom.evict = evict
	go Oom.reign()
}

func (o *oom_t) gc() {
	now := time.Now()
	if now.Sub(o.lastpr) > time.Second {
		o.lastpr = now.Add(time.Second)
	}
	runtime.GCX()
}

func (o *oom_t) reign() {
outter:
	for msg := range o.halp {
		fmt.Printf("A need %v, rem %v\n", msg.Need, runtime.Remain())
		if msg.Need < runtime.Remain() {
			// there is apparently enough reservation available for
			// them now
			msg.Resume <- true
			continue
		}
		o.gc()
		fmt.Printf("B need %v, rem %v\n", msg.Need, runtime.Remain())
		//panic("OOM KILL\n")
		if msg.Need < runtime.Remain() {
			// there is apparently enough reservation available for
			// them now
			msg.Resume <- true
			continue
		}

		// XXX make sure msg.need is a satisfiable reservation size

		// XXX expand kernel heap with free pages; page if none
		// available

		last := 0
		for {
			a, b := o.evict()
			if a+b == last || a+b < 1000 {
				break
			}
			last = a + b
		}
		o.gc()
		if msg.Need < runtime.Remain() {
			msg.Resume <- true
			continue outter
		}
		for {
			// someone must die
			o.dispatch_peasant(msg.Need)
			o.gc()
			if msg.Need < runtime.Remain() {
				msg.Resume <- true
				continue outter
			}
		}
	}
}

func (o *oom_t) dispatch_peasant(need int) {
	// the oom killer's memory use should have a small bound
	var head *Proc_t
	Ptable.Iter(func (_ int32, p *Proc_t) bool {
		p.Oomlink = head
		head = p
		return false
	})

	var memmax int
	var vic *Proc_t
	for p := head; p != nil; p = p.Oomlink {
		mem := o.judge_peasant(p)
		if mem > memmax {
			memmax = mem
			vic = p
		}
	}

	// destroy the list so the Proc_ts and reachable objects become dead
	var next *Proc_t
	for p := head; p != nil; p = next {
		next = p.Oomlink
		p.Oomlink = nil
	}

	if vic == nil {
		panic("nothing to kill?")
	}

	fmt.Printf("Killing PID %d \"%v\" for (%v %v)...\n", vic.Pid, vic.Name,
		res.Human(need), vic.Vm.Vmregion.Novma)
	vic.Doomall()
	st := time.Now()
	dl := st.Add(time.Second)
	// wait for the victim to die
	sleept := 1 * time.Millisecond
	for {
		if _, ok := Proc_check(vic.Pid); !ok {
			break
		}
		now := time.Now()
		if now.After(dl) {
			fmt.Printf("oom killer: waiting for hog for %v...\n",
				now.Sub(st))
			o.gc()
			dl = dl.Add(1 * time.Second)
		}
		time.Sleep(sleept)
		sleept *= 2
		const maxs = 3 * time.Second
		if sleept > maxs {
			sleept = maxs
		}
	}
}

// acquires p's pmap and fd locks (separately)
func (o *oom_t) judge_peasant(p *Proc_t) int {
	// init(1) must never perish
	if p.Pid == 1 {
		return 0
	}

	p.Vm.Lock_pmap()
	novma := int(p.Vm.Vmregion.Novma)
	p.Vm.Unlock_pmap()

	var nofd int
	p.Fdl.Lock()
	for _, fd := range p.Fds {
		if fd != nil {
			nofd++
		}
	}
	p.Fdl.Unlock()

	// count per-thread and per-child process wait objects
	chalds := p.Mywait.Len()

	return novma + nofd + chalds
}
