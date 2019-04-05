package res

import "fmt"
import "runtime"

import "caller"
import "oommsg"
import "tinfo"

// XXX argument should be Boundkey_t so caller doesn't have to

// blocks until memory is available or returns false if this process has been
// killed and should terminate.
func Resbegin(want *Res_t) bool {
	if !Kernel {
		return true
	}
	r := _reswait(want, false, true)
	//if !r {
	//	fmt.Printf("Slain!\n")
	//}
	return r
}

var Kernel bool

var Resfail = caller.Distinct_caller_t{
	Enabled: true,
	Whitel: map[string]bool{
		// XXX these need to be fixed to handle ENOHEAP
		"fs.(*Fs_t).Fs_rename":      true,
		"main.(*susfops_t)._fdrecv": true,
	},
}

const resfail = false

// blocks until memory is available or returns false if this process has been
// killed and should terminate.
func Resadd(want *Res_t) bool {
	if !Kernel {
		return true
	}
	if resfail {
		if ok, path := Resfail.Distinct(); ok {
			fmt.Printf("failing: %s\n", path)
			return false
		}
	}
	r := _reswait(want, true, true)
	//if !r {
	//	fmt.Printf("Slain!\n")
	//}
	return r
}

// for reservations when locks may be held; the caller should abort and retry.
func Resadd_noblock(want *Res_t) bool {
	if !Kernel {
		return true
	}
	if resfail {
		if ok, path := Resfail.Distinct(); ok {
			fmt.Printf("failing: %s\n", path)
			return false
		}
	}
	return _reswait(want, true, false)
}

func Resend() {
	if !Kernel {
		return
	}
	if !Lims {
		return
	}
	runtime.Gresrelease()
}

func Human(_bytes int) string {
	bytes := float64(_bytes)
	div := float64(1)
	order := 0
	for bytes/div > 1024 {
		div *= 1024
		order++
	}
	sufs := map[int]string{0: "B", 1: "kB", 2: "MB", 3: "GB", 4: "TB",
		5: "PB"}
	return fmt.Sprintf("%.2f%s", float64(bytes)/div, sufs[order])
}

//var lastp time.Time

//func _reswait(c int, incremental, block bool) bool {
func _reswait(want *Res_t, incremental, block bool) bool {
	if !Lims {
		return true
	}
	f := runtime.Greserve
	for !f(want) {
		//if time.Since(lastp) > time.Second {
		//	fmt.Printf("RES failed %v\n", c)
		//	Callerdump(2)
		//}
		t := tinfo.Current()
		if t.Doomed() {
			return false
		}
		if !block {
			return false
		}
		//fmt.Printf("%v: Wait for memory hog to die...\n", p.Name)
		var omsg oommsg.Oommsg_t
		omsg.Need = 2 << 20
		omsg.Resume = make(chan bool, 1)
		select {
		case oommsg.OomCh <- omsg:
		case <-tinfo.Current().Killnaps.Killch:
			return false
		}
		select {
		case <-omsg.Resume:
		case <-tinfo.Current().Killnaps.Killch:
			return false
		}
	}
	return true
}

// a type to make it easier for code that allocates cached objects to determine
// when we must try to evict them.
type Cacheallocs_t struct {
	initted bool
}

// returns true if the caller must try to evict their recent cache allocations.
func (ca *Cacheallocs_t) Shouldevict(want *Res_t) bool {
	if !Kernel {
		return false
	}
	if !Lims {
		return false
	}
	//init := !ca.initted
	//ca.initted = true
	//return !runtime.Cacheres(res, init)
	return !runtime.Greserve(want)
}

var Kwaits int

const Lims = true

func Kreswait(want *Res_t, name string) {
	if !Kernel {
		return
	}
	if !Lims {
		return
	}
	for !runtime.Greserve(want) {
		fmt.Printf("kernel thread \"%v\" waiting for hog to die...\n", name)
		Kwaits++
		var omsg oommsg.Oommsg_t
		//omsg.Need = 100 << 20
		omsg.Need = int(want.Objs[1])
		omsg.Resume = make(chan bool, 1)
		oommsg.OomCh <- omsg
		<-omsg.Resume
	}
}

func Kunres() int {
	if !Kernel {
		return 0
	}
	if !Lims {
		return 0
	}
	runtime.Gresrelease()
	return 0
}

func Kresdebug(want *Res_t, name string) {
	//Kreswait(c, name)
}

func Kunresdebug() int {
	//return Kunres()
	return 0
}

type Res_t = runtime.Res_t

var Onemeg = &Res_t{Objs: runtime.Resobjs_t{1: 1 << 20}}
var Onek = &Res_t{Objs: runtime.Resobjs_t{1: 1 << 10}}
var Afewk = &Res_t{Objs: runtime.Resobjs_t{1: 1 << 13}}
