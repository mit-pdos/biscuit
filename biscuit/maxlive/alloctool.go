/*
 * TODO
 * - assume anything written via atomic.Store* is a root?
 * - Verify that Make{Interface, Closure} do not cause more heap
 *   allocations besides the ssa.Allocs already present for them.
 * - sends on buffered channel accumulate live data too
 * 	- I dumped all channel sends with pointer types and verified that all
 * 	  such sends are on unbuffered channels, thus the live data
 * 	  accumulation is already accounted for in the allocation of the object
 * 	  being sent. This check could be easily automated via more pointer
 * 	  analysis.
 * - recursive iteration loops
 *	- (replaced recursion with loops)
 * - don't forget that the tool ignores calls to package fmt.
 * - consider transient objects written to channels as permanent
 */

/*
 * TO USE
 * - make sure the looping calls that this tool ignores are actually prevented
 *   at runtime (this was the case at the time of writing)
 *
 * 1) mkdir -p ~/go/src/allocstudy && cd ~/go/src/allocstudy
 * 2) ~/biscuit/bin/go get -u "golang.org/x/tools/go/callgraph"
 * 3) ~/biscuit/bin/go run allocstudy.go
 */

/*
 * HOW IT WORKS
 *
 * XXX
 */
package main

import "bufio"
import "strconv"
import "os"

import "fmt"
import "time"
import "strings"
import "sort"

import "go/constant"
import "go/token"
import "go/types"
import "go/ast"

import "golang.org/x/tools/go/callgraph"
import "golang.org/x/tools/go/loader"
import "golang.org/x/tools/go/pointer"
import "golang.org/x/tools/go/ssa"
import "golang.org/x/tools/go/ssa/ssautil"

// DUMPER
var dumper = false

type halp_t struct {
	cg        *callgraph.Graph
	heads     map[*ssa.BasicBlock]*natl_t
	funcvisit map[*callgraph.Node]bool
	memos     map[*callgraph.Node]*calls_t
	fakefunc  map[string]bool
	chanfuncs map[*ssa.Function]bool
	// visited loop blocks
	lbvisit    map[*ssa.BasicBlock]bool
	usedbounds map[*ssa.BasicBlock]bool
	infloops   map[token.Position]bool
	recurses   map[string][]string
	nomemo     map[*callgraph.Node]bool
	ms         struct {
		P     lstack_t
		S     lstack_t
		nums  map[*callgraph.Node]int
		added map[*callgraph.Node]bool
		num   int
		comps []map[*callgraph.Node]bool
	}
	pkgs []*ssa.Package
}

func (h *halp_t) init(pkgs []*ssa.Package, cg *callgraph.Graph) {
	h.pkgs = pkgs
	h.cg = cg
	h.funcvisit = map[*callgraph.Node]bool{}
	h.fakefunc = map[string]bool{}
	h.infloops = map[token.Position]bool{}
	h.chanfuncs = map[*ssa.Function]bool{}
	h.recurses = map[string][]string{}
	h.nomemo = map[*callgraph.Node]bool{}
	h.reset()
}

func (h *halp_t) reset() {
	h.memos = map[*callgraph.Node]*calls_t{}
	h.heads = map[*ssa.BasicBlock]*natl_t{}
	h.lbvisit = map[*ssa.BasicBlock]bool{}
	h.usedbounds = map[*ssa.BasicBlock]bool{}
}

// size class code stolen from src/runtime/sizeclasses.go, which is
// automatically generated during build (but was identical between go1.8 and
// go1.10.1, so maybe the sizeclasses don't change)
const (
	_MaxSmallSize   = 32768
	smallSizeDiv    = 8
	smallSizeMax    = 1024
	largeSizeDiv    = 128
	_NumSizeClasses = 67
	_PageShift      = 13
)

var size_to_class8 = [smallSizeMax/smallSizeDiv + 1]uint8{0, 1, 2, 3, 3, 4, 4,
	5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13, 13, 14, 14,
	15, 15, 16, 16, 17, 17, 18, 18, 18, 18, 19, 19, 19, 19, 20, 20, 20, 20,
	21, 21, 21, 21, 22, 22, 22, 22, 23, 23, 23, 23, 24, 24, 24, 24, 25, 25,
	25, 25, 26, 26, 26, 26, 26, 26, 26, 26, 27, 27, 27, 27, 27, 27, 27, 27,
	28, 28, 28, 28, 28, 28, 28, 28, 29, 29, 29, 29, 29, 29, 29, 29, 30, 30,
	30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 31, 31, 31, 31,
	31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31, 31}

var size_to_class128 = [(_MaxSmallSize-smallSizeMax)/largeSizeDiv + 1]uint8{31,
	32, 33, 34, 35, 36, 36, 37, 37, 38, 38, 39, 39, 39, 40, 40, 40, 41, 42,
	42, 43, 43, 43, 43, 43, 44, 44, 44, 44, 44, 44, 45, 45, 45, 45, 46, 46,
	46, 46, 46, 46, 47, 47, 47, 48, 48, 49, 50, 50, 50, 50, 50, 50, 50, 50,
	50, 50, 51, 51, 51, 51, 51, 51, 51, 51, 51, 51, 52, 52, 53, 53, 53, 53,
	54, 54, 54, 54, 54, 55, 55, 55, 55, 55, 55, 55, 55, 55, 55, 55, 56, 56,
	56, 56, 56, 56, 56, 56, 56, 56, 57, 57, 57, 57, 57, 57, 58, 58, 58, 58,
	58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 58, 59, 59, 59, 59, 59, 59,
	59, 59, 59, 59, 59, 59, 59, 59, 59, 59, 60, 60, 60, 60, 60, 61, 61, 61,
	61, 61, 61, 61, 61, 61, 61, 61, 62, 62, 62, 62, 62, 62, 62, 62, 62, 62,
	63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63, 63,
	63, 63, 63, 63, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64, 64,
	64, 64, 64, 64, 64, 64, 64, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65, 65,
	66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66,
	66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66, 66}

func _sizetoclass(sz int) int {
	var sizeclass uint8
	if sz <= smallSizeMax-8 {
		sizeclass = size_to_class8[(sz+smallSizeDiv-1)/smallSizeDiv]
	} else {
		sizeclass = size_to_class128[(sz-smallSizeMax+largeSizeDiv-1)/largeSizeDiv]
	}
	return int(sizeclass)
}

var countsc struct {
	counts	[_NumSizeClasses]int
}

func classadd(bytes int) {
	countsc.counts[_sizetoclass(bytes)]++
}

func classdump() {
	fmt.Printf("Size class counts over all traversed allocations\n")
	for i, c := range countsc.counts {
		fmt.Printf("%3v: %9v\n", i, c)
	}
}

var _s = types.StdSizes{WordSize: 8, MaxAlign: 8}

func array_align(t types.Type) int {
	truesz := _s.Sizeof(t)
	align := _s.Alignof(t)
	if truesz%align != 0 {
		diff := align - (truesz % align)
		truesz += diff
	}
	//classadd(int(truesz))
	return int(truesz)
}

func slicesz(v ssa.Value, num int) int {
	// the element type could be types.Named...
	var typ *types.Slice
	if tt, ok := v.Type().(*types.Named); ok {
		typ = tt.Underlying().(*types.Slice)
	} else {
		typ = v.Type().(*types.Slice)
	}
	elmsz := array_align(typ.Elem())
	return num*elmsz + 3*8
}

func chansz(v *ssa.MakeChan) int {
	ue := v.Type().(*types.Chan).Elem()
	elmsz := array_align(ue)
	// all biscuit channel buffer sizes are constant
	num, ok := constant.Int64Val(v.Size.(*ssa.Const).Value)
	if !ok {
		panic("nuts")
	}
	es := int(num) * elmsz
	es = (es + 7) &^ 0x7
	return es + 3*8
}

// returns true if there is a successor path from cur to target
func blockpath1(cur, target *ssa.BasicBlock, v map[*ssa.BasicBlock]bool) bool {
	v[cur] = true
	if cur == target {
		return true
	}
	for _, bp := range cur.Succs {
		if v[bp] {
			continue
		}
		if blockpath1(bp, target, v) {
			return true
		}
	}
	return false
}

// returns true if cur == target
func blockpath(cur, target *ssa.BasicBlock) bool {
	visited := make(map[*ssa.BasicBlock]bool)
	return blockpath1(cur, target, visited)
}

// returns true if b has a successor which is also b's predecessor
func blockcycle(b *ssa.BasicBlock) bool {
	for _, bp := range b.Succs {
		if blockpath(bp, b) {
			return true
		}
	}
	return false
}

// remove "bound", "thunk", and anonymous function identifiers from the name.
// see ssa.Function.Relstring() docs.
func realname(f *ssa.Function) string {
	ret := f.String()
	if i := strings.Index(ret, "$"); i != -1 {
		ret = ret[:i]
	}
	if i := strings.Index(ret, "#"); i != -1 {
		ret = ret[:i]
	}
	return ret
}

type callstack_t struct {
	cs []string
}

func (cs *callstack_t) push(f *ssa.Function) {
	//cs.cs = append(cs.cs, realname(f))
	cs.cs = append(cs.cs, f.String())
}

func (cs *callstack_t) pop() {
	l := len(cs.cs)
	cs.cs = cs.cs[:l-1]
}

// returns true if the function imp is present in the current call stack.
func (cs *callstack_t) calledfrom(imp string) bool {
	for i := range cs.cs {
		if cs.cs[i] == imp {
			return true
		}
	}
	return false
}

func (cs *callstack_t) csdump(msg string) {
	fmt.Println(msg)
	for _, cs := range cs.cs {
		fmt.Printf("\t%v\n", cs)
	}
}

// a type to keep track of which loops may execute an allocation multiple times
type lmult_t struct {
	times int
	name  string
	isinf bool
	sym   string
}

type alloc_t struct {
	num    int
	inf    bool
	size   int
	lmults []lmult_t
}

type calls_t struct {
	me        *ssa.Function
	looper    bool
	calls     []*calls_t
	allallocs map[ssa.Value]alloc_t
	// total live bytes for this function and its callees
	totl     int
	symloops map[string]bool
}

func newcalls(me *ssa.Function) *calls_t {
	ret := &calls_t{me: me, calls: make([]*calls_t, 0)}
	ret.allallocs = make(map[ssa.Value]alloc_t)
	ret.symloops = make(map[string]bool)
	ret.totl = -1
	return ret
}

func (c *calls_t) givesym(p map[string]bool) {
	for s := range c.symloops {
		p[s] = true
	}
}

func (c *calls_t) addalloc(k ssa.Value, size int) {
	if k == nil {
		panic("no")
	}

	// make([]string, 0) results in ssa.Alloc for [0]string
	if size == 0 {
		return
	}
	// ignore allocations that the programmer claims are parity-evicted
	if _evicted[k] {
		return
	}
	if old, ok := c.allallocs[k]; ok {
		old.num += 1
		c.allallocs[k] = old
	} else {
		c.allallocs[k] = alloc_t{num: 1, inf: false, size: size}
	}
}

func (c *calls_t) loopmult(times int, name, sym string) {
	isinf := times == INF
	if isinf {
		times = 1000
	}
	if times < 0 {
		panic("no")
	}
	for k, ac := range c.allallocs {
		ac.num *= times
		ac.lmults = append(ac.lmults, lmult_t{times: times,
			name: name, isinf: isinf, sym: sym})
		c.allallocs[k] = ac
	}
}

func (c *calls_t) total(parptrs map[ssa.Value]bool) int {
	return c._mysum(parptrs)
}

//func (c *calls_t) total(parptrs map[ssa.Value]bool) int {
//	ret := c._mysum(parptrs)
//	for _, ch := range c.calls {
//		if ch.totl == -1 {
//			panic("child without total?")
//		}
//		ret += ch.totl
//	}
//	c.totl = ret
//	return ret
//}

// calculate total reachable bytes from this function only (not callees)
func (c *calls_t) _mysum(myptrs map[ssa.Value]bool) int {
	ret := 0
	for k, ac := range c.allallocs {
		// XXX
		if ac.size < 0 || ac.num < 0 {
			panic("eh?")
		}
		if permanent[k] || (myptrs != nil && myptrs[k]) {
			ret += ac.size * ac.num
		}
	}
	return ret
}

func (c *calls_t) merge(sib *calls_t) {
	for _, tc := range sib.calls {
		c._addchild(tc)
	}
	c._mergeallocs(sib)
	sib.givesym(c.symloops)
}

func (c *calls_t) addchild(child *calls_t) {
	c._addchild(child)
	c._mergeallocs(child)
	child.givesym(c.symloops)
}

func (c *calls_t) _addchild(child *calls_t) {
	// XXX
	if child == nil {
		panic("eh?")
	}
	c.calls = append(c.calls, child)
}

func (c *calls_t) _mergeallocs(child *calls_t) {
	for k, ac := range child.allallocs {
		if old, ok := c.allallocs[k]; ok {
			old.num += ac.num
			old.inf = old.inf || ac.inf
			if old.size != ac.size {
				panic("crud")
			}
			old.lmults = append(old.lmults, ac.lmults...)
			c.allallocs[k] = old
		} else {
			c.allallocs[k] = ac
		}
	}
}

func (c *calls_t) dump(verbose bool) {
	c.dump1(verbose, 0, nil)
}

func (c *calls_t) dump1(v bool, depth int, parptrs map[ssa.Value]bool) {
	if c.me == &dummyfunc {
		return
	}
	//myptrs := ptrsetadd(parptrs, c.me)
	myptrs := funcreach[c.me]
	prdepth := func() {
		for i := 0; i < depth; i++ {
			fmt.Printf("  ")
		}
	}
	prdepth()
	lop := ""
	if c.looper {
		lop = "=L"
	}

outter:
	for k, ac := range c.allallocs {
		if !permanent[k] && !myptrs[k] {
			continue
		}
		for _, lm := range ac.lmults {
			if lm.isinf {
				lop = "=INFL"
				break outter
			}
		}
	}
	fmt.Printf("%c %s [%v]%s\n", 'A'+depth, c.me.String(),
		human(c.total(myptrs)), lop)

	if v {
		uniq := make(map[types.Type]bool)
		sizes := make(map[types.Type]int)
		exists := false
		for k, ac := range c.allallocs {
			uniq[k.Type()] = true
			sizes[k.Type()] = ac.size
			if permanent[k] || myptrs[k] {
				exists = true
			}
		}
		strs := make(map[types.Type]string)
		for t := range uniq {
			var s string
			for k, ac := range c.allallocs {
				if k.Type() != t {
					continue
				}
				if !permanent[k] && !myptrs[k] {
					continue
				}
				if s != "" {
					s += "+ "
				}
				if ac.num == 1 {
					s += "1 "
				} else {
					var t string
					for _, lm := range ac.lmults {
						if t != "" {
							t += "* "
						}
						var ts string
						if lm.isinf {
							ts = "INF"
						} else {
							ts = fmt.Sprintf("%v", lm.times)
						}
						t += fmt.Sprintf("%v(%v) ", lm.name, ts)
					}
					s += t
				}
				strs[t] = s
			}
		}
		if exists {
			//prdepth()
			//fmt.Printf(" |\n")
			//prdepth()
			//fmt.Printf(" |- LIMIT TYPE SUM----\n")
			//for t, s := range strs {
			//	prdepth()
			//	fmt.Printf(" |  %v(%v): %v\n", t, sizes[t], s)
			//}
			prdepth()
			fmt.Printf(" |\n")
			prdepth()
			fmt.Printf(" |--- EACH ALLOC\n")

			for k, ac := range c.allallocs {
				if !permanent[k] && !myptrs[k] {
					continue
				}
				in, ok := k.(ssa.Instruction)
				if !ok {
					// dumping code doesn't handle
					// *ssa.Function values created from
					// *ssa.{Defer,Go} (no corresponding
					// call instructions for defer? doesn't
					// Go have one though?)
					panic("fixme")
				}
				prdepth()
				let := ""
				if permanent[k] {
					let = " P"
				}
				fmt.Printf(" |-%v (%v*%v)%v\n", k, ac.size, ac.num, let)
				prdepth()
				fmt.Printf(" |       %T %v\n", k, in.Parent())
				prdepth()
				fmt.Printf(" |       %v\n", inpos(in))
				if ac.num > 1 {
					prdepth()
					fmt.Printf(" |       loops (%v):\n", len(ac.lmults))
					for _, lm := range ac.lmults {
						prdepth()
						tstr := fmt.Sprintf("%vx", lm.times)
						if lm.isinf {
							tstr = "INF"
						}
						fmt.Printf(" |         %v: %v\n", lm.name, tstr)
					}
				}
			}
		}
	}
	for i := range c.calls {
		c.calls[i].dump1(v, depth+1, myptrs)
	}
}

func (c *calls_t) dyump() {
	for k, v := range c.allallocs {
		fmt.Printf("  s%5v #%3v %T %v %v %v\n", v.size, v.num, k, k,
			k.(ssa.Instruction).Parent(), inpos(k.(ssa.Instruction)))
	}
}

// a type to create the symbolic limit equation from a calls_t
type summer_t struct {
	lastdid map[ssa.Value]bool
	// map of allocation size to count
	sizes map[int]int
	// map of symbolic loop names to map of allocation sizes to counts
	symsize map[string]map[int]int
	initted bool
}

func (sm *summer_t) _init() {
	if sm.initted {
		return
	}
	sm.initted = true
	sm.lastdid = make(map[ssa.Value]bool)
	sm.sizes = make(map[int]int)
	sm.symsize = make(map[string]map[int]int)
}

func (sm *summer_t) accumulate(c *calls_t, ptrs map[ssa.Value]bool) {
	sm._init()
	newdid := make(map[ssa.Value]bool)
	for k, ac := range c.allallocs {
		if !permanent[k] && !ptrs[k] {
			continue
		}
		newdid[k] = true
		if sm.lastdid[k] {
			continue
		}
		var syms []string
		for _, lm := range ac.lmults {
			if lm.sym != "" {
				syms = append(syms, lm.sym)
			}
		}
		if len(syms) > 0 {
			sort.Strings(syms)
			k := ""
			sep := ""
			for _, sym := range syms {
				k += sep + sym
				sep = " * "
			}
			oldm, ok := sm.symsize[k]
			if !ok {
				oldm = make(map[int]int)
			}
			old := oldm[ac.size]
			oldm[ac.size] = old + ac.num
			sm.symsize[k] = oldm
		} else {
			old := sm.sizes[ac.size]
			sm.sizes[ac.size] = old + ac.num
		}
	}
	sm.lastdid = newdid
}

func (sm *summer_t) equation() string {
	sm._init()

	ret := ""
	sep := ""
	for sym, sizes := range sm.symsize {
		ret += sep + sym
		sep2 := " "
		for size, count := range sizes {
			ret += sep2 + fmt.Sprintf("(%v) * %v", count, size)
			sep2 = " + "
		}
		sep = " + "
	}
	for size, count := range sm.sizes {
		ret += sep + fmt.Sprintf("%v * %v", count, size)
		sep = " + "
	}
	return ret
}

func human(_bytes int) string {
	bytes := float64(_bytes)
	div := float64(1)
	order := 0
	for bytes/div > 1024 {
		div *= 1024
		order++
	}
	sufs := map[int]string{0: "B", 1: "kB", 2: "MB", 3: "GB", 4: "TB",
		5: "PB"}
	return fmt.Sprintf("%.2f%s", bytes/div, sufs[order])
}

// map of callees to list of impossible callers.
var _impossible = map[string][]string{
//"(*main.pgcache_t).flush" : []string{"(*main.pgcache_t).evict"},
}

func ignore(f *ssa.Function, cs *callstack_t) bool {
	me := realname(f)
	for _, t := range []string{"fmt.", "strconv."} {
		if strings.Contains(me, t) {
			return true
		}
	}
	// prevent programmer-supplied impossible executions
	if imp, ok := _impossible[me]; ok {
		//fmt.Printf("IMPS %v\n", imp)
		//for i := range cs.cs {
		//	fmt.Printf("\t%v\n", cs.cs[i])
		//}
		for i := range imp {
			if cs.calledfrom(imp[i]) {
				fmt.Printf("FORBID %v from %v\n", f, imp[i])
				return true
			}
		}
	}
	return false
}

func callidests(node *callgraph.Node,
	in ssa.CallInstruction) []*callgraph.Node {
	target := in.Common()
	var ret []*callgraph.Node
	for _, e := range node.Out {
		// XXX how to identify the particular call instruction from
		// ssa.CallInstruction provided by callgraph.Edge? is comparing
		// their ssa.CallCommons the correct way?
		tt := e.Site.Common()
		if tt != target {
			continue
		}
		ret = append(ret, e.Callee)
	}
	return ret
}

// this function finds which callee function allocates most for a single
// callsite. first bool is whether this call may cause recursion, second is
// whether this callgraph node had an unvisited edge that should be visited for
// this call instruction.
func (ha *halp_t) maxcall(callees []*callgraph.Node, parptrs map[ssa.Value]bool,
	cs *callstack_t, iscall bool) (*calls_t, bool, bool) {
	var calls *calls_t
	found := false
	rec := false
	var max int
	syms := make(map[string]bool)

	for _, node := range callees {
		if ha.funcvisit[node] {
			rec = true
			// cycles in the call graph are problematic because our
			// tool doesn't know an upper bound on the number of
			// times the functions in the cycle are executed.
			// although this tool (conservatively) considers
			// channel sends as function calls, the receiver
			// doesn't actually restart the function call, thus it
			// isn't really a cycle in the call graph. therefore:
			// don't warn about them.
			if iscall {
				me := cs.cs[len(cs.cs)-1]
				k := fmt.Sprintf("%v -> %v\n", me, node.Func)
				var strs []string
				for _, e := range cs.cs {
					strs = append(strs, e)
				}
				ha.recurses[k] = strs
			}
			continue
		}
		if ignore(node.Func, cs) {
			//me := node.Func.String()
			//fmt.Printf("**** SKIP %s\n", me)
			continue
		}
		found = true
		var tc *calls_t
		if v, ok := ha.memos[node]; ok {
			tc = v
		} else {
			tc = ha.alloctree(node, parptrs, cs)
			if ha.nomemo[node] {
				// XXX alloctree() used to run once per
				// function, but can calculate recursive
				// functions multiple times now.
				for _, bb := range node.Func.Blocks {
					delete(ha.heads, bb)
					delete(ha.usedbounds, bb)
				}
			} else {
				ha.memos[node] = tc
			}
		}
		tc.givesym(syms)
		newmax := tc.total(parptrs)
		if newmax >= max {
			max = newmax
			calls = tc
		}
	}
	if found && calls == nil {
		panic("wtf")
	}
	if calls != nil {
		for s := range syms {
			calls.symloops[s] = true
		}
	}
	return calls, rec, found
}

func revblksrc(blk *ssa.BasicBlock) (token.Position, bool) {
	v := make(map[*ssa.BasicBlock]bool)
	return revblksrc1(blk, v)
}

func revblksrc1(blk *ssa.BasicBlock,
	v map[*ssa.BasicBlock]bool) (token.Position, bool) {
	var zt token.Position
	if v[blk] {
		return zt, false
	}
	v[blk] = true
	for i := len(blk.Instrs) - 1; i >= 0; i-- {
		pos := blk.Instrs[i].Pos()
		if pos != token.NoPos {
			ret := blk.Instrs[0].Parent().Prog.Fset.Position(pos)
			return ret, true
		}
	}
	for _, e := range blk.Preds {
		if ret, ok := revblksrc1(e, v); ok {
			return ret, true
		}
	}
	return zt, false
}

func readnum(msg string) int {
	var ret int
again:
	fmt.Print(msg)
	//fmt.Printf("\n");return 1
	dur := bufio.NewReader(os.Stdin)
	str, err := dur.ReadString('\n')
	if err != nil {
		goto again
	}
	ret, err = strconv.Atoi(str[:len(str)-1])
	if err != nil {
		goto again
	}
	return ret
}

func (ha *halp_t) suminstructions(node *callgraph.Node, blk *ssa.BasicBlock,
	parptrs map[ssa.Value]bool, calls *calls_t, cs *callstack_t) {
	for _, ip := range blk.Instrs {
		switch v := ip.(type) {
		case *ssa.Go:
			// sum of all stack frames in packages main, common,
			// and fs
			maxstack := 80 << 10
			calls.addalloc(v.Common().Value, maxstack)
		case *ssa.Defer:
			comm := v.Common()
			defsz := 64 + len(comm.Args) * 8
			// ssa.Defer.Value is an uninitialized *ssa.Call?
			calls.addalloc(comm.Value, defsz)
		case *ssa.Send, *ssa.Select:
			callees := chandests(ha.cg, v)
			for _, ce := range callees {
				ha.chanfuncs[ce.Func] = true
			}
			maxc, _, found := ha.maxcall(callees, parptrs, cs, false)
			if found {
				// ignore sends that don't allocate
				if maxc.total(parptrs) > 0 {
					calls.addchild(maxc)
				}
			}
		case ssa.CallInstruction:
			// find which function for this call site allocates
			// most
			callees := callidests(node, v)
			maxc, rec, found := ha.maxcall(callees, parptrs, cs, true)
			if found {
				// ignore calls that don't allocate
				if maxc.total(parptrs) > 0 {
					calls.addchild(maxc)
				}
			} else if !rec {
				//fmt.Printf("failed for: %v\n", v)
				var str string
				switch tt := v.Common().Value.(type) {
				case *ssa.Builtin:
					str = tt.Name()
					if strings.Contains(str, "append") {
						if str != "append" {
							panic("nein")
						}
						val := v.Value()
						num := slicebound(val, cs)
						sz := slicesz(val, num)
						calls.addalloc(val, sz)
					}
				case *ssa.Function:
					str = tt.String()
				default:
					//if strings.Contains(node.Func.String(),
					//	"brefcache_t") {
					//	continue
					//}
					sl := node.Func.Prog.Fset.Position(v.Pos())
					fmt.Printf("%v %T %v %v\n", v, v.Common().Value, v.Common().Method, sl)
					fmt.Printf("callees: %v\n", callees)
					fmt.Printf("callees2: %v\n", callgraph.CalleesOf(node))
					fmt.Printf("node.Out: %v\n", node.Out)
					cs.csdump("CS:")
					panic("which call?")
				}
				ha.fakefunc[str] = true
			}
		case *ssa.MakeChan:
			calls.addalloc(v, chansz(v))
		case *ssa.MakeMap:
			num := mapbound(v)
			elm := v.Type().(*types.Map).Elem()
			key := v.Type().(*types.Map).Key()
			sz := num * (array_align(elm) + array_align(key))
			calls.addalloc(v, sz)
		case *ssa.MakeSlice:
			num := slicebound(v, cs)
			calls.addalloc(v, slicesz(v, num))
		case *ssa.Alloc:
			if v.Heap {
				tp := v.Type().(*types.Pointer)
				ut := tp.Elem()
				calls.addalloc(v, array_align(ut))
			}
		}
	}
}

func _looppos(nat *natl_t) token.Position {
	for _, in := range nat.head.Instrs {
		p := inpos(in)
		if p.IsValid() {
			return p
		}
	}
	for blk := range nat.lblock {
		for _, in := range blk.Instrs {
			p := inpos(in)
			if p.IsValid() {
				return p
			}
		}
	}
	panic("no valid loop position?")
}

func (ha *halp_t) loopcalc(node *callgraph.Node, parptrs map[ssa.Value]bool,
	cs *callstack_t) {
	floops := funcloops(node.Func)
	// calculate each loop's allocations and iterations in post-order since
	// we must calculate the cost of a inner loops before we can calculate
	// the cost of the outer loops.
	floops.iter(func(nat *natl_t) {
		if ha.heads[nat.head] != nil {
			panic(">1 loops per head after loop merge")
		}
		ha.heads[nat.head] = nat
		lcalls := newcalls(node.Func)
		for bb := range nat.lblock {
			ha.suminstructions(node, bb, parptrs, lcalls, cs)
		}
		// include nested loops in cost
		for _, nest := range nat.nests {
			lcalls.merge(nest.lcalls)
		}
		nat.lcalls = lcalls
		// don't ask the programmer to provide a bound for loops that
		// create no reachable allocations from the containing
		// function.
		if nat.lcalls._mysum(parptrs) == 0 {
			//if nat.lcalls._mysum(funcreach[node.Func]) != 0 {
			//	panic("oh shit")
			//}
			// mark BOUND call as used, if it exists, to prevent
			// outer loop from getting confused
			nat.boundcall(ha.pkgs, ha.usedbounds)
			return
		}
		var iters int
		var name string
		it, tname, sym, ok := nat.boundcall(ha.pkgs, ha.usedbounds)
		if ok {
			iters = it
			name = tname
		} else {
			iters = loopbound(node.Func.String(),
				_looppos(nat))
			name = "MANUAL"
		}
		if iters == INF {
			pos := _looppos(nat)
			ha.infloops[pos] = true
		}
		if sym != "" {
			nat.lcalls.symloops[sym] = true
		}
		// this overestimates allocations since this loopmulti()
		// multiplies the number of allocations which aren't in any
		// parent's pointer set too, relying on calls_t.total() to
		// ignore such allocations.
		nat.lcalls.loopmult(iters, name, sym)
	})
	//floops.dump()
}

var dummyfunc ssa.Function

// XXX don't need to pass parent pointers down since a callee's ptr set must
// include all ptrs of parent that are stored to?
//func ptrsetadd(parptrs map[ssa.Value]bool,
//    me *ssa.Function) map[ssa.Value]bool {
//	// copy ptrs instead of adding to parptrs set to avoid the trouble of
//	// removing this functions pointers from the set once we are done. this
//	// function's pointer set may overlap with a parent's and such pointers
//	// should be removed iff it wasn't in the set at the beginning of this
//	// function.
//	myptrs := parptrs
//	if fptrs, ok := funcreach[me]; ok {
//		myptrs = make(map[ssa.Value]bool, len(parptrs))
//		for k, v := range parptrs {
//			myptrs[k] = v
//		}
//		for k, v := range fptrs {
//			myptrs[k] = v
//		}
//	} else {
//		//fmt.Printf("=== NO PTRS %v\n", me)
//	}
//	return myptrs
//}

func (ha *halp_t) alloctree(node *callgraph.Node, parptrs map[ssa.Value]bool,
	cs *callstack_t) *calls_t {
	//if node.Func.String() == "(*fs.bbitmap_t).Balloc" {
	//	return newcalls(node.Func)
	//}
	if ha.funcvisit[node] {
		panic("should terminate in maxcall")
	}
	ha.funcvisit[node] = true
	defer delete(ha.funcvisit, node)

	if len(node.Func.Blocks) == 0 {
		//fmt.Printf("**** NO BLOCKS %v\n", node.Func)
		return newcalls(&dummyfunc)
	}
	cs.push(node.Func)
	defer cs.pop()

	// the function entry block has Index == 0 and is the first block in
	// the slice
	entry := node.Func.Blocks[0]
	// find natural loops, compute maximum iteration allocation, and save
	// them for _sumblock
	ha.loopcalc(node, parptrs, cs)

	// we could prune all allocations that are not reachable from any of
	// this function's parents from the returned calls_t...
	visited := make(map[*ssa.BasicBlock]bool)
	return ha._sumblock(node, entry, parptrs, visited, cs)
}

// _sumblock is called on a function's blocks once all of a function's natural
// loops have been found and calculated.
func (ha *halp_t) _sumblock(node *callgraph.Node, blk *ssa.BasicBlock,
	parptrs map[ssa.Value]bool, visit map[*ssa.BasicBlock]bool,
	cs *callstack_t) *calls_t {
	if visit[blk] {
		//fmt.Printf("BLOCK LOOP AVOID in %v\n", node.Func)
		return newcalls(&dummyfunc)
	}
	visit[blk] = true
	defer delete(visit, blk)

	calls := newcalls(node.Func)
	// if this is a head loop block, simply use the pre-calculated loop
	// cost
	hnat, amhead := ha.heads[blk]
	// avoid double-counting loop blocks
	if amhead && !ha.lbvisit[blk] {
		calls.looper = true
		calls.merge(hnat.lcalls)
		for bb := range hnat.lblock {
			if ha.lbvisit[bb] {
				panic("double count loop block")
			}
			ha.lbvisit[bb] = true
		}
		defer func() {
			for bb := range hnat.lblock {
				if !ha.lbvisit[bb] {
					panic("shite")
				}
				delete(ha.lbvisit, bb)
			}
		}()
	} else if !amhead && !ha.lbvisit[blk] {
		ha.suminstructions(node, blk, parptrs, calls, cs)
	}
	var maxc calls_t
	maxsum := -1
	for _, suc := range blk.Succs {
		tc := ha._sumblock(node, suc, parptrs, visit, cs)
		newsum := tc.total(parptrs)
		if newsum >= maxsum {
			maxc = *tc
			maxsum = newsum
		}
		if tc.looper {
			calls.looper = true
		}
		tc.givesym(calls.symloops)
	}
	calls.merge(&maxc)
	return calls
}

func prettygraph(cg *callgraph.Graph, root *ssa.Function) {
	rootnode, ok := cg.Nodes[root]
	if !ok {
		panic("no")
	}

	funcname := func(f *callgraph.Node) string {
		name := f.Func.String()
		name = strings.Join(strings.Split(name, "("), "")
		name = strings.Join(strings.Split(name, ")"), "")
		name = strings.Join(strings.Split(name, "*"), "")
		name = strings.Join(strings.Split(name, "."), "_")
		name = strings.Join(strings.Split(name, "$"), "_")
		name = strings.Join(strings.Split(name, "/"), "_")
		return name
	}
	var edges []string
	did := map[*callgraph.Node]bool{rootnode: true}
	left := map[*callgraph.Node]bool{rootnode: true}
	for len(left) != 0 {
		var f *callgraph.Node
		for k := range left {
			f = k
			delete(left, k)
			break
		}
		name := funcname(f)
		fmt.Printf("node [name=%v]\n", name)
		cees := callgraph.CalleesOf(f)
		for cnode := range cees {
			s := fmt.Sprintf("%s -> %s", funcname(f),
				funcname(cnode))
			edges = append(edges, s)
			if d := did[cnode]; d {
				continue
			}
			did[cnode] = true
			left[cnode] = true
		}
	}
	for _, e := range edges {
		fmt.Printf("%v\n", e)
	}
}

type lstack_t struct {
	q []*callgraph.Node
}

func (ls *lstack_t) push(node *callgraph.Node) {
	ls.q = append(ls.q, node)
}

func (ls *lstack_t) pop() *callgraph.Node {
	l := len(ls.q) - 1
	ret := ls.q[l]
	ls.q = ls.q[:l]
	return ret
}

func (ls *lstack_t) peek() *callgraph.Node {
	l := len(ls.q) - 1
	ret := ls.q[l]
	return ret
}

//  Our analysis assumes the call graph is acyclic since then each function's
//  maximum allocations is simply the total of the function and all its
//  children's maximum allocations. Recursive functions are not acyclic and
//  form a strongly connected component in the graph which our analysis detects
//  during the DFS analysis in order to terminate. However, this means that the
//  computed maximum allocations of the functions in the strongly-connected
//  component depend on traversal order (which are memoized after they are
//  computed) -- only the sum of the first visited function in the
//  strongly-connected component will include the sums of all the other
//  functions in the component. markstrong() sets a flag that prevents
//  memoization on all functions in strongly-connected components in the call
//  graph, ensuring that no incorrect memoizations are saved.
func (h *halp_t) markstrong(root *ssa.Function) {
	if h.cg == nil || len(h.cg.Nodes) == 0 {
		panic("make callgraph first")
	}
	h.ms.num = 1
	h.ms.nums = make(map[*callgraph.Node]int)
	h.ms.added = make(map[*callgraph.Node]bool)
	var P lstack_t
	var S lstack_t
	h.ms.P = P
	h.ms.S = S
	node, ok := h.cg.Nodes[root]
	if !ok {
		panic("no node")
	}
	h.markstrong1(node)
	for _, comp := range h.ms.comps {
		for n := range comp {
			h.nomemo[n] = true
		}
	}

	if len(h.ms.comps) > 0 {
		fmt.Printf("STRONGLY CONNECTED COMPONENTS\n")
		for i, comp := range h.ms.comps {
			fmt.Printf("%v:\n", i)
			for nn := range comp {
				fmt.Printf("\t%v\n", nn.Func.String())
			}
		}
	}
}

func (h *halp_t) markstrong1(node *callgraph.Node) {
	// https://en.wikipedia.org/wiki/Path-based_strong_component_algorithm
	mn := h.ms.nums[node]
	if mn != 0 {
		panic("visit twice")
	}
	h.ms.nums[node] = h.ms.num
	h.ms.num++
	h.ms.S.push(node)
	h.ms.P.push(node)

	for _, e := range node.Out {
		callee := e.Callee
		wnum := h.ms.nums[callee]
		if wnum == 0 {
			h.markstrong1(callee)
		} else if !h.ms.added[callee] {
			for h.ms.nums[h.ms.P.peek()] > wnum {
				h.ms.P.pop()
			}
		}
	}

	if h.ms.P.peek() == node {
		newcomp := make(map[*callgraph.Node]bool)
		for {
			nn := h.ms.S.pop()
			if newcomp[nn] {
				panic("double add")
			}
			newcomp[nn] = true
			if nn == node {
				break
			}
		}
		for nn := range newcomp {
			h.ms.added[nn] = true
		}
		if len(newcomp) > 1 {
			h.ms.comps = append(h.ms.comps, newcomp)
		}
		h.ms.P.pop()
	}
}

func (h *halp_t) maxlive(root *ssa.Function) string {
	rootnode, ok := h.cg.Nodes[root]
	if !ok {
		panic("no")
	}
	scan := map[*callgraph.Node]bool{rootnode: true}
	todo := map[*ssa.Function]bool{root: true}
	for len(scan) != 0 {
		var do *callgraph.Node
		for k := range scan {
			do = k
			break
		}
		delete(scan, do)
		for df := range callgraph.CalleesOf(do) {
			if !todo[df.Func] && /* XXX */ !ignore(df.Func, nil) {
				todo[df.Func] = true
				scan[df] = true
			}
		}
	}
	fmt.Printf("local max on %v functions...\n", len(todo))
	st := time.Now()

	localmax := make(map[*ssa.Function]*calls_t)
	for i := 0; len(todo) != 0; i++ {

		var next *ssa.Function
		for k := range todo {
			next = k
			break
		}
		delete(todo, next)

		node, ok := h.cg.Nodes[next]
		if !ok {
			panic("huh?")
		}
		h.reset()
		ptrs := funcreach[next]
		calls := h.alloctree(node, ptrs, &callstack_t{})

		if _, ok := localmax[next]; ok {
			panic("nyet")
		}
		localmax[next] = calls

		//fmt.Printf("calls:\n")
		//calls.dump(false)

	}

	if len(h.fakefunc) > 0 {
		fmt.Printf("FAILED TO FIND:\n")
		for fail := range h.fakefunc {
			fmt.Printf("  %v\n", fail)
		}
	}

	if len(h.chanfuncs) > 0 {
		fmt.Printf("ENSURE THESE THREADS HAVE LIMITS:\n")
		for ff := range h.chanfuncs {
			fmt.Printf("\t%v\n", ff)
			//fmt.Printf("\t%v\t(%v)\n", ff,
			//    inpos(ff.Blocks[0].Instrs[0]))
		}
	}

	if len(prunedsends) > 0 {
		fmt.Printf("PRUNED SENDS:\n")
		for in := range prunedsends {
			fmt.Printf("\t%v\n", inpos(in))
		}
	}

	if len(h.recurses) > 0 {
		fmt.Printf("VERIFY RECURSION CANNOT OCCUR\n")
		for k, v := range h.recurses {
			fmt.Printf("%v\n", k)
			for _, s := range v {
				fmt.Printf("\t%v\n", s)
			}
		}
	}

	{
		var dur []string
		for p := range h.infloops {
			dur = append(dur, p.String())
		}
		sort.Strings(dur)
		if len(dur) > 0 {
			fmt.Printf("****************************************" +
				"************************\n")
		}
		for _, s := range dur {
			fmt.Printf("INFINITE ALLOC at %v\n", s)
		}
		if len(dur) > 0 {
			fmt.Printf("****************************************" +
				"************************\n")
		}
	}

	fmt.Printf("local max took: %v\n", time.Now().Sub(st))

	rm := localmax[root]
	if len(rm.symloops) > 0 {
		fmt.Printf("ENSURE LIMIT INCLUDES FOLLOWING " +
			"SYMBOLIC BOUNDS:\n")
		for k := range rm.symloops {
			fmt.Printf("\t%v\n", k)
		}
	}

	fmt.Printf("MAXLIVE ROOT: %v\n", root)
	sum, maxpath := h.maxpath(root, localmax)

	var sm summer_t
	for i := len(maxpath); i > 0; i-- {
		ff := maxpath[i-1]
		lm := localmax[ff]
		sm.accumulate(lm, funcreach[ff])
	}
	leq := sm.equation()
	fmt.Printf("--- LIMIT: %v\n", leq)

	fmt.Printf("sum: %v\n", human(sum))
	for i := len(maxpath); i > 0; i-- {
		ff := maxpath[i-1]
		lm := localmax[ff]
		fmt.Printf("---FUNC %v\n", ff)
		lm.dump(dumper)
		//lm.dyump()
	}

	fmt.Printf("DUR %v %v %v\n", root, len(rm.symloops),
		rm.symloops)
	return leq
}

func (ha *halp_t) maxpath(root *ssa.Function,
	localmax map[*ssa.Function]*calls_t) (int, []*ssa.Function) {
	used := make(map[ssa.Value]bool)
	visit := make(map[*ssa.Function]bool)
	return ha.maxpath1(root, localmax, used, visit)
}

func (ha *halp_t) maxpath1(ff *ssa.Function, localmax map[*ssa.Function]*calls_t,
	parused map[ssa.Value]bool, visit map[*ssa.Function]bool) (int, []*ssa.Function) {
	if visit[ff] {
		return 0, nil
	}
	visit[ff] = true
	defer delete(visit, ff)

	mine := 0
	ptrs := funcreach[ff]
	myc := localmax[ff]
	myused := make(map[ssa.Value]bool)
	// Avoid recounting allocations by marking them used. the reason this
	// is OK is a little subtle. This tool considers all executed
	// allocations that are reachable from a parent pointer set to all be
	// reachable, even if really only one such object is actually
	// reachable. Thus if any allocation is reachable from the parent
	// pointer set, the parent will be charged for at least as many.
	//
	// Therefore: an allocation in a parent function that is also reachable
	// by an immediate child is the same allocation and should only be
	// counted in the parent (which is at least as large as the childs). On
	// the other hand, an allocation in an immediate child that is not
	// reachable from a parent has not been yet counted.
	for k, ac := range myc.allallocs {
		if !permanent[k] && !ptrs[k] {
			continue
		}
		myused[k] = true
		if !parused[k] {
			mine += ac.num * ac.size
		}
	}
	var maxchald []*ssa.Function
	var max int
	for cn := range callgraph.CalleesOf(ha.cg.Nodes[ff]) {
		if ignore(cn.Func, nil) {
			continue
		}
		tt, chald := ha.maxpath1(cn.Func, localmax, myused, visit)
		if tt >= max {
			max = tt
			maxchald = chald
		}
	}
	return max + mine, append(maxchald, ff)
}

func kernelcode() map[string][]string {
	ret := make(map[string][]string)
	mk := func(pref string, ns []string) []string {
		var ret []string
		for _, n := range ns {
			br := BROOT
			//br := "/home/ccutler/biscuit/biscuit/"
			ret = append(ret, br+"/"+pref+"/"+n+".go")
		}
		return ret
	}
	wtf := []string{"main", "bins", "syscall"}
	ret["main"] = mk("kernel", wtf)

	//wtf := []string{"flea"}
	//ret["main"] = mk("kernel", wtf)

	//wtf := []string{"hw", "main", "net", "syscall", "pmap", "fs", "fsrb",
	//    "bins"}
	//ret["main"] = mk("", wtf)

	return ret

	// BROOT is the biscuit/src folder i.e. root of packages
	//broot, err := os.Open(BROOT)
	//if err != nil {
	//	panic(err.Error())
	//}
	//dirs, err := broot.Readdir(0)
	//if err != nil {
	//	panic(err.Error())
	//}
	//ret := make(map[string][]string)
	//for _, _dir := range dirs {
	//	if !_dir.IsDir() {
	//		continue
	//	}
	//	if _dir.Name()[0] == '.' {
	//		continue
	//	}
	//	pkgpath := BROOT + "/" + _dir.Name()
	//	dir, err := os.Open(pkgpath)
	//	if err != nil {
	//		panic(err.Error())
	//	}
	//	more, err := dir.Readdir(0)
	//	if err != nil {
	//		panic(err.Error())
	//	}
	//	var srcs []string
	//	for _, file := range more {
	//		if file.IsDir() {
	//			fmt.Printf("WARNING: skipping directory in " +
	//			    "package dir: %v\n", file.Name)
	//		}
	//		name := file.Name()
	//		if name[0] == '.' || !strings.HasSuffix(name, ".go") {
	//			continue
	//		}
	//		srcs = append(srcs, pkgpath + "/" + name)
	//	}
	//	ret[dir.Name()] = srcs
	//}
	//return ret
}

func ffunc(pkgs []*ssa.Package, str string) *ssa.Function {
	for _, pkg := range pkgs {
		if _sysfunc, ok := pkg.Members[str]; ok {
			ret := _sysfunc.(*ssa.Function)
			return ret
		}
	}
	fmt.Printf("for: %v\n", str)
	panic("no such function")
}

func fmeth(pkgs []*ssa.Package, tpe, meth string) *ssa.Function {
	for _, pkg := range pkgs {
		if pkg.Type(tpe) != nil {
			T := pkg.Type(tpe).Type()
			pT := types.NewPointer(T)
			ret := pkg.Prog.LookupMethod(pT, pkg.Pkg, meth)
			return ret
		}
	}
	fmt.Printf("for: %v %v\n", tpe, meth)
	panic("no such method")
}

func main() {
	c := loader.Config{}
	srcpkgs := kernelcode()
	for pkg, srcs := range srcpkgs {
		c.CreateFromFilenames(pkg, srcs...)
	}

	iprog, err := c.Load()
	if err != nil {
		fmt.Println(err) // type error in some package
		return
	}

	// Create SSA-form program representation.
	prog := ssautil.CreateProgram(iprog, 0)

	kernpkgs := map[string]bool{
		"package main": true, "package common": true, "package fs": true,
		"package accnt": true, "package ahci": true, "package apic":
		true, "package bnet": true, "package bounds": true,
		"package bpath": true, "package caller": true, "package circbuf": true,
		"package defs": true, "package fd": true, "package fdops":
		true, "package hashtable": true,
		"package inet": true, "package ixgbe": true,
		"package kernel": true, "package limits": true,
		"package mem": true, "package mkfs": true, "package msi": true,
		"package oommsg": true, "package pci": true, "package proc": true,
		"package res": true, "package stat": true, "package stats":
		true, "package tinfo": true,
		"package ustr": true, "package util": true,
		"package vm": true,
	}
	var pkgs []*ssa.Package
	for _, pkg := range prog.AllPackages() {
		if kernpkgs[pkg.String()] {
			pkgs = append(pkgs, pkg)
		}
	}

	// THINGY
	syscalls := []string{
		// testing
		//"sys_fork",
		//"sys_read",
		//"sys_write",
		//"sys_rename",

		// syscall with deep resevation
		//"sys_poll",

		// all syscalls
		"sys_read", "sys_write", "sys_open", "sys_stat", "sys_fstat",
		"sys_poll", "sys_lseek", "sys_mmap", "sys_munmap", "sys_readv",
		"sys_writev", "sys_sigaction", "sys_access", "sys_dup2",
		"sys_pause", "sys_getpid", "sys_getppid", "sys_socket",
		"sys_connect", "sys_accept", "sys_sendto", "sys_recvfrom",
		"sys_socketpair", "sys_shutdown", "sys_bind", "sys_listen",
		"sys_recvmsg", "sys_sendmsg", "sys_getsockopt",
		"sys_setsockopt", "sys_fork", "sys_execv", "sys_wait4",
		"sys_kill", "sys_fcntl", "sys_truncate", "sys_ftruncate",
		"sys_getcwd", "sys_chdir",
		"sys_rename",
		"sys_mkdir",
		"sys_link", "sys_unlink", "sys_gettimeofday", "sys_getrlimit",
		"sys_getrusage", "sys_mknod", "sys_setrlimit", "sys_sync",
		"sys_reboot", "sys_nanosleep", "sys_pipe2", "sys_prof",
		"sys_info", "sys_threxit", "sys_pread", "sys_pwrite",
		"sys_futex", "sys_gettid",
	}
	if syscalls == nil {
	}

	unboundfunc := []string{
		// kernel thread
		//"kbd_daemon",
		//"arp_resolve",
	}
	if unboundfunc == nil {
	}

	type tmp_t struct {
		t string
		m string
	}
	unboundmeth := []tmp_t{
		//// for testing/manual analysis
		////tmp_t{"oom_t", "reign"},
		////tmp_t{"syscall_t", "Sys_close"},

		// syscalls
		tmp_t{"syscall_t", "Sys_close"},
		tmp_t{"syscall_t", "Sys_exit"},

		//// run-only
		//tmp_t{"Proc_t", "run1"},

		//// deep reservations
		//tmp_t{"Proc_t", "Userargs"},
		//tmp_t{"Vm_t", "K2user_inner"},
		//tmp_t{"Vm_t", "User2k_inner"},
		//tmp_t{"Userbuf_t", "_tx"},
		//tmp_t{"Useriovec_t", "Iov_init"},
		//tmp_t{"Useriovec_t", "_tx"},
		//tmp_t{"imemnode_t", "_descan"},
		//tmp_t{"Fs_t", "Fs_op_rename"},
		//tmp_t{"Fs_t", "_isancestor"},
		//tmp_t{"rawdfops_t", "Read"},
		//tmp_t{"rawdfops_t", "Write"},
		//tmp_t{"Fs_t", "fs_namei"},
		//tmp_t{"imemnode_t", "do_write"},
		//tmp_t{"imemnode_t", "bmapfill"},
		//tmp_t{"imemnode_t", "iread"},
		//tmp_t{"imemnode_t", "iwrite"},
		//tmp_t{"imemnode_t", "immapinfo"},
		//tmp_t{"Tcpfops_t", "Read"},
		//tmp_t{"Tcpfops_t", "Write"},
		//tmp_t{"pipefops_t", "Write"},
		//tmp_t{"elf_t", "elf_load"},
		//tmp_t{"pipe_t", "op_fdadd"},

		//// manually evicted cached allocations
		//tmp_t{"imemnode_t", "ifree"},
		//tmp_t{"bitmap_t", "apply"},

		//// kernel threads
		//tmp_t{"ixgbe_t", "int_handler"},
		//tmp_t{"tcptimers_t", "_tcptimers_daemon"},
		//tmp_t{"log_t", "committer"},
		//tmp_t{"futex_t", "futex_start"},
	}
	if unboundmeth == nil {
	}

	_getmeth := func(t, m string) *ssa.Function {
		return fmeth(pkgs, t, m)
	}

	_getfunc := func(str string) *ssa.Function {
		return ffunc(pkgs, str)
	}
	if _getmeth == nil || _getfunc == nil {
	}

	var sysfuncs []*ssa.Function

	for _, str := range syscalls {
		sf := _getfunc(str)
		sysfuncs = append(sysfuncs, sf)
	}
	for _, str := range unboundfunc {
		sf := _getfunc(str)
		sysfuncs = append(sysfuncs, sf)
	}
	for _, tmp := range unboundmeth {
		sf := _getmeth(tmp.t, tmp.m)
		sysfuncs = append(sysfuncs, sf)
	}

	// Build SSA code for bodies of all functions in the whole program.
	//mpkg.SetDebugMode(true)
	prog.Build()

	cg := mkcallgraph(prog)
	//prettygraph(cg, sysfuncs[0])
	// XXX must precede maxlive()
	reachallocs(cg, prog, pkgs)
	leqs := make(map[string]string)

	for _, sysfunc := range sysfuncs {
		fmt.Printf("====================== %v =====================\n",
			sysfunc.String())

		var h halp_t
		h.init(pkgs, cg)
		h.markstrong(sysfunc)
		leq := h.maxlive(sysfunc)
		leqs[sysfunc.String()] = leq
	}
	fmt.Printf("\nSYS TABLE:\n\n")
	fmt.Printf("var _syslimits = map[int]int {\n")
	for sys, leq := range leqs {
		if leq == "" {
			leq = "0"
		}
		fmt.Printf("%v: %v,\n", tblname(kernpkgs, sys), leq)
	}
	fmt.Printf("}\n")
	//classdump()
}

func tblname(kpkgs map[string]bool, n string) string {
	for pkg := range kpkgs {
		pn := strings.Replace(pkg, "package ", "", -1) + "."
		n = strings.Replace(n, pn, "", -1)
	}
	n = strings.ToUpper(n)
	n = strings.Join(strings.Split(n, "("), "")
	n = strings.Join(strings.Split(n, ")"), "_")
	n = strings.Join(strings.Split(n, "*"), "")
	n = strings.Join(strings.Split(n, "."), "")
	return "B_" + n
}

func mkcallgraph(prog *ssa.Program) *callgraph.Graph {
	pkgs := ssautil.MainPackages(prog.AllPackages())

	// Configure the pointer analysis to build a call-graph.
	config := &pointer.Config{
		Mains: pkgs,
		//Reflection:	true,
		BuildCallGraph: true,
		//Log: os.Stdout,
	}
	result, err := pointer.Analyze(config)
	if err != nil {
		panic(err)
	}
	//fmt.Printf("WARNINGS\n")
	//for _, w := range result.Warnings {
	//	fmt.Printf("%v\n\t%v\n", w.Message,
	//	    prog.Fset.Position(w.Pos))
	//}
	return result.CallGraph
}

func pdump(prog *ssa.Program, val ssa.Value, pp *pointer.Pointer) {
	for _, l := range pp.PointsTo().Labels() {
		fmt.Printf("  %T %v %s: %s\n", l.Value(), l.Value(),
			prog.Fset.Position(l.Pos()), l)
	}
}

type inpairs_t struct {
	conf *pointer.Config
	res  *pointer.Result
	pkg  *ssa.Package
	ins  map[ssa.Instruction][]*pointer.Pointer
	numq int
}

func (ip *inpairs_t) ipinit(pkg *ssa.Package) {
	ip.pkg = pkg
	ip.conf = &pointer.Config{
		Mains: []*ssa.Package{pkg},
		//Reflection:	true,
		BuildCallGraph: false,
	}
	ip.ins = make(map[ssa.Instruction][]*pointer.Pointer)
}

func (ip *inpairs_t) add(in ssa.Instruction, v ssa.Value) {
	ptr := ip._query(v)
	old := ip.ins[in]
	ip.ins[in] = append(old, ptr)
}

func (ip *inpairs_t) _query(v ssa.Value) *pointer.Pointer {
	wtfp, err := ip.conf.AddExtendedQuery(v, "x")
	if err != nil {
		panic(err)
	}
	ip.numq++
	return wtfp
}

func (ip *inpairs_t) ipiter(f func(ssa.Instruction, ssa.Value)) {
	for in, ptrs := range ip.ins {
		uniq := make(map[ssa.Value]bool)
		for _, ptr := range ptrs {
			for _, l := range ptr.PointsTo().Labels() {
				uniq[l.Value()] = true
			}
		}
		for v := range uniq {
			f(in, v)
		}
	}
}

func (ip *inpairs_t) analyze() {
	fmt.Printf("IP analyzing %v queries...\n", ip.numq)
	st := time.Now()
	res, err := pointer.Analyze(ip.conf)
	if err != nil {
		panic(err)
	}
	ip.res = res
	fmt.Printf("took %v\n", time.Now().Sub(st))
}

type stores_t struct {
	addrs []*pointer.Pointer
	vals  []*pointer.Pointer
}

type qs_t struct {
	conf *pointer.Config
	res  *pointer.Result
	ins  map[ssa.Instruction]*stores_t
	apps map[ssa.Value]*pointer.Pointer
	numq int
}

func (q *qs_t) qinit(pkgs []*ssa.Package) {
	m := ssautil.MainPackages(pkgs[0].Prog.AllPackages())[0]
	q.conf = &pointer.Config{
		Mains: []*ssa.Package{m},
		//Reflection:	true,
		BuildCallGraph: false,
	}
	q.ins = make(map[ssa.Instruction]*stores_t)
	q.apps = make(map[ssa.Value]*pointer.Pointer)
}

func (q *qs_t) _query(v ssa.Value) *pointer.Pointer {
	wtfp, err := q.conf.AddExtendedQuery(v, "x")
	if err != nil {
		panic(err)
	}
	q.numq++
	return wtfp
}

// Creates pointer analysis queries on the operands of the store-like
// instruction "in".
func (q *qs_t) addop(in ssa.Instruction) {
	var addrs []*pointer.Pointer
	var vals []*pointer.Pointer

	added := false
	adda := func(v ssa.Value) {
		added = true
		addrs = append(addrs, q._query(v))
	}
	addv := func(v ssa.Value) {
		added = true
		if st, ok := typetostruct(v.Type()); ok {
			vals = append(vals, q._structquery(v, st)...)
		} else {
			vals = append(vals, q._query(v))
		}
	}

	switch rin := in.(type) {
	case *ssa.MakeInterface:
		if canpoint(rin.X.Type()) {
			adda(rin)
			addv(rin.X)
		}
	case *ssa.MapUpdate:
		adda(rin.Map)
		if canpoint(rin.Key.Type()) {
			addv(rin.Key)
		}
		if canpoint(rin.Value.Type()) {
			addv(rin.Value)
		}
	case *ssa.Store:
		adda(rin.Addr)
		addv(rin.Val)
	case *ssa.Call:
		// handle append()
		av := rin.Common().Args[1]
		adda(rin)
		addv(av)
	}
	if !added {
		return
	}
	st := &stores_t{addrs: addrs, vals: vals}
	if _, ok := q.ins[in]; ok {
		panic("oh noes")
	}
	q.ins[in] = st
}

// XXX merge _structquery and _query
func (q *qs_t) _structquery(v ssa.Value, st *types.Struct) []*pointer.Pointer {
	var ret []*pointer.Pointer
	for _, qstr := range stqensure(st) {
		ret = append(ret, q._queryext(v, qstr))
	}
	return ret
}

func (q *qs_t) _queryext(v ssa.Value, qstr string) *pointer.Pointer {
	wtfp, err := q.conf.AddExtendedQuery(v, qstr)
	if err != nil {
		panic(err)
	}
	q.numq++
	return wtfp
}

func (q *qs_t) analyze() {
	fmt.Printf("analyzing %v queries...\n", q.numq)
	st := time.Now()
	res, err := pointer.Analyze(q.conf)
	if err != nil {
		panic(err)
	}
	q.res = res
	fmt.Printf("took %v\n", time.Now().Sub(st))
}

func (q *qs_t) dump() {
	fmt.Printf("WARNINGS: %v\n", len(q.res.Warnings))
	//for _, w := range q.res.Warnings {
	//	fmt.Printf("%v\n\t%v\n", w.Message,
	//	    sf.Prog.Fset.Position(w.Pos))
	//}
}

func (q *qs_t) iiter(fun func(ssa.Instruction, []*pointer.Label,
	[]*pointer.Label)) {
	for in, st := range q.ins {
		var la []*pointer.Label
		var lv []*pointer.Label
		for _, pp := range st.addrs {
			la = append(la, pp.PointsTo().Labels()...)
		}
		for _, pp := range st.vals {
			lv = append(lv, pp.PointsTo().Labels()...)
		}
		fun(in, la, lv)
	}
}

func funcdump(f *ssa.Function) {
	f.WriteTo(os.Stdout)
	fmt.Printf("-----\n")
	for bi, b := range f.Blocks {
		fmt.Printf("BLOCK %v\n", bi)
		for _, in := range b.Instrs {
			fmt.Printf("\t%-30s %T\n", in.String(), in)
		}
	}
}

func inpos(in ssa.Instruction) token.Position {
	p := in.Parent().Prog
	ret := p.Fset.Position(in.Pos())
	return ret
}

var permanent = map[ssa.Value]bool{}
var funcreach = map[*ssa.Function]map[ssa.Value]bool{}

// bounds for maps and slices via SBOUND(); keys must be *ssa.MakeMap or
// *ssa.MakeSlice.
var sboundcalls = map[ssa.Value]int{}

type sbound_t struct {
	ptr *pointer.Pointer
	max int
}

func mkboundcalls(pkgs []*ssa.Package, allfuncs []*ssa.Function) {
	sbound := ffunc(pkgs, "SBOUND")

	// map of calls of SBOUND to MakeSlice/MakeMap
	sptrs := make(map[*ssa.Call]sbound_t)

	var sbounds qs_t
	sbounds.qinit(pkgs)
	// find all max slice size annotations
	for _, fun := range allfuncs {
		for _, blk := range fun.Blocks {
			for _, in := range blk.Instrs {
				switch rin := in.(type) {
				case *ssa.Call:
					com := rin.Common()
					sc := com.StaticCallee()
					if sc != sbound {
						continue
					}
					if _, ok := sptrs[rin]; ok {
						panic("eh?")
					}
					// first arg is value, second arg is
					// bound. for convenience, first arg is
					// an interface and must thus be
					// converted.
					var n int
					switch ra := com.Args[1].(type) {
					case *ssa.Const:
						n = int(ra.Int64())
					case *ssa.Convert:
						n = int(ra.X.(*ssa.Const).Int64())
					default:
						fmt.Printf("%T %v %v\n", ra, ra,
							inpos(in))
						panic("weird SBOUND max")
					}
					a1 := com.Args[0]
					a1 = a1.(*ssa.MakeInterface).X
					ptr := sbounds._query(a1)
					sb := sbound_t{ptr, n}
					sptrs[rin] = sb
				}
			}
		}
	}
	lastadd := make(map[ssa.Value]ssa.Instruction)
	sbounds.analyze()
	for in, sb := range sptrs {
		for _, l := range sb.ptr.PointsTo().Labels() {
			v := l.Value()
			if old, ok := sboundcalls[v]; ok && old != sb.max {
				opos := inpos(lastadd[v])
				fmt.Printf("first: %v at %v\n", old, opos)
				fmt.Printf("second: %v at %v\n", sb.max,
					inpos(in))
				panic("different maxes for same value")
			}
			sboundcalls[v] = sb.max
			lastadd[v] = in
		}
	}
}

// the keys are *ssa.Send, the values are functions which may be executed due
// to this *ssa.Send.
var chancallees = map[ssa.Instruction][]*ssa.Function{}
var prunedsends = map[ssa.Instruction]bool{}

func chandests(cg *callgraph.Graph, in ssa.Instruction) []*callgraph.Node {
	// ignore receive only selects
	if rin, ok := in.(*ssa.Select); ok {
		recvonly := true
		for _, sel := range rin.States {
			if sel.Dir == types.SendOnly {
				recvonly = false
				break
			}
		}
		if recvonly {
			return nil
		}
	}
	var ret []*callgraph.Node
	ffs, ok := chancallees[in]
	if !ok || len(ffs) == 0 {
		if prunedsends[in] {
			return nil
		}
		fmt.Printf("%v\n", inpos(in))
		panic("send on nil chan or no receivers?")
	}
	for _, ff := range ffs {
		ret = append(ret, cg.Nodes[ff])
	}
	return ret
}

func mkchancalls(pkgs []*ssa.Package, allfuncs []*ssa.Function) {
	var qs qs_t
	qs.qinit(pkgs)
	sptrs := make(map[*pointer.Pointer]ssa.Instruction)
	rfuncs := make(map[*pointer.Pointer]*ssa.Function)
	sadd := func(v ssa.Value, rin ssa.Instruction) {
		ptr := qs._query(v)
		if _, ok := sptrs[ptr]; ok {
			panic("nein")
		}
		sptrs[ptr] = rin
	}
	radd := func(v ssa.Value, fun *ssa.Function) {
		ptr := qs._query(v)
		if _, ok := rfuncs[ptr]; ok {
			panic("no")
		}
		rfuncs[ptr] = fun
	}

	prunef := ffunc(pkgs, "PRUNE")
	var prptrs []*pointer.Pointer

	for _, fun := range allfuncs {
		for _, blk := range fun.Blocks {
			for _, in := range blk.Instrs {
				switch rin := in.(type) {
				case *ssa.UnOp:
					// only consider channel receives
					if rin.Op != token.ARROW {
						continue
					}
					radd(rin.X, fun)
				case *ssa.Send:
					//fmt.Printf("CHAN SEND: %v\n", inpos(rin))
					sadd(rin.Chan, in)
				case *ssa.Select:
					for _, sel := range rin.States {
						if sel.Dir != types.SendOnly &&
							sel.Dir != types.RecvOnly {
							panic("wut")
						}
						if sel.Dir == types.SendOnly {
							sadd(sel.Chan, in)
						} else {
							radd(sel.Chan, fun)
						}
					}
				case *ssa.Call:
					com := rin.Common()
					sc := com.StaticCallee()
					if sc != prunef {
						continue
					}
					a1 := com.Args[0]
					a1 = a1.(*ssa.MakeInterface).X
					prptrs = append(prptrs, qs._query(a1))
				}
			}
		}
	}
	qs.analyze()

	// map of channel allocations, readers of which should not have their
	// allocations counted against the sender. such readers will either
	// have limit checks of their own or obviously bounded memory use.
	prunes := make(map[ssa.Value]bool)
	for _, ptr := range prptrs {
		for _, l := range ptr.PointsTo().Labels() {
			v := l.Value()
			prunes[v] = true
		}
	}

	// map channel allocations to send instructions
	sctoinst := make(map[ssa.Value][]ssa.Instruction)
	for ptr, in := range sptrs {
		for _, l := range ptr.PointsTo().Labels() {
			v := l.Value()
			if prunes[v] {
				prunedsends[in] = true
				continue
			}
			old := sctoinst[v]
			sctoinst[v] = append(old, in)
		}
	}
	for ptr, ff := range rfuncs {
		for _, l := range ptr.PointsTo().Labels() {
			v := l.Value()
			if _, ok := sctoinst[v]; !ok {
				fmt.Printf("receive only channel? %v %v\n", v,
					inpos(v.(ssa.Instruction)))
			}
			for _, in := range sctoinst[v] {
				old := chancallees[in]
				chancallees[in] = append(old, ff)
			}
		}
	}
}

// map of allocations which the programmer asserts are only allocated after
// another such object is evicted, thus such allocations do not increase live
// memory
var _evicted = map[ssa.Value]bool{}

func mkevictcalls(pkgs []*ssa.Package, allfuncs []*ssa.Function) {
	pev := ffunc(pkgs, "PEVICT")

	var evs qs_t
	evs.qinit(pkgs)

	var ptrs []*pointer.Pointer
	for _, fun := range allfuncs {
		for _, blk := range fun.Blocks {
			for _, in := range blk.Instrs {
				switch rin := in.(type) {
				case *ssa.Call:
					com := rin.Common()
					sc := com.StaticCallee()
					if sc != pev {
						continue
					}
					a1 := com.Args[0]
					a1 = a1.(*ssa.MakeInterface).X
					ptrs = append(ptrs, evs._query(a1))
				}
			}
		}
	}
	evs.analyze()
	for _, ptr := range ptrs {
		//if len(ptr.PointsTo().Labels()) > 1 {
		//	for _, l := range ptr.PointsTo().Labels() {
		//		fmt.Printf("\t%v\n", inpos(l.Value().(ssa.Instruction)))
		//	}
		//	panic("PEVICT with more than one allocation?")
		//}
		for _, l := range ptr.PointsTo().Labels() {
			v := l.Value()
			_evicted[v] = true
		}
	}
}

func typetostruct(t types.Type) (*types.Struct, bool) {
	var ret *types.Struct
	switch rt := t.(type) {
	default:
		return nil, false
	case *types.Struct:
		ret = rt
	case *types.Named:
		if ru, ok := rt.Underlying().(*types.Struct); ok {
			ret = ru
		} else {
			return nil, false
		}
	}
	return ret, true
}

// returns a slice of strings which can be passed to AddExtendedQuery() to run
// pointer analysis on all the pointer fields of a struct value.
func structqueries(wtf *types.Struct) []string {
	return structqueries1(wtf, "x")
}

func structqueries1(st *types.Struct, pref string) []string {
	var ret []string
	add := func(s []string) {
		ret = append(ret, s...)
	}

	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		ft := f.Type()
		memb := fmt.Sprintf("%s.%s", pref, f.Name())
		if pointer.CanPoint(ft) {
			add([]string{memb})
		} else {
			nextst, recurse := typetostruct(ft)
			if recurse {
				add(structqueries1(nextst, memb))
			}
		}
	}
	return ret
}

// memos of all pointers in a struct
var stqueries = map[*types.Struct][]string{}

func stqensure(st *types.Struct) []string {
	if ret, ok := stqueries[st]; ok {
		return ret
	}
	ret := structqueries(st)
	stqueries[st] = ret
	return ret
}

// returns true for pointer types, structs which contain pointers, and other
// types considered special by pointer.CanPoint (reflect types).
func canpoint(t types.Type) bool {
	if pointer.CanPoint(t) {
		return true
	}
	st, ok := typetostruct(t)
	if !ok {
		return false
	}
	qs := stqensure(st)
	return len(qs) != 0
}

func reachallocs(cg *callgraph.Graph, prog *ssa.Program, pkgs []*ssa.Package) {
	var roots []ssa.Value
	allfuncs := make([]*ssa.Function, 0)
	//skip := map[string]bool {
	//	"package sort": true,
	//	"package fmt": true,
	//	"package reflect": true,
	//}
	// XXX is builtin append replaced with per-type appends which are
	// actually defined in some of these packages? (i.e. special append
	// handling below can go away?)
	//allow := map[string]bool {
	//	"package main": true,
	//	"package time": true,
	//	//"package sort": true,
	//}
	for _, pkg := range prog.AllPackages() {
		for _, mem := range pkg.Members {
			switch rt := mem.(type) {
			case *ssa.Function:
				allfuncs = append(allfuncs, rt)
				allfuncs = append(allfuncs, rt.AnonFuncs...)
			case *ssa.Type:
				if nam, ok := rt.Type().(*types.Named); ok {
					for i := 0; i < nam.NumMethods(); i++ {
						meth := nam.Method(i)
						safunc := prog.FuncValue(meth)
						allfuncs = append(allfuncs, safunc)
						allfuncs = append(allfuncs, safunc.AnonFuncs...)
					}
				}
			case *ssa.Global:
				roots = append(roots, rt)
			}
		}
	}
	var keep []*ssa.Function
	// XXX special case atomic.Store* calls in store analysis?
	warns := make(map[*callgraph.Node]bool)
	for _, ff := range allfuncs {
		// XXX to make analysis faster
		//oks := map[string]bool{"package main": true,
		//    "package time": true}
		//if !oks[ff.Pkg.String()] {
		//	continue
		//}
		nn := cg.Nodes[ff]
		if nn == nil || len(nn.In) == 0 {
			continue
		}
		keep = append(keep, ff)
		if strings.HasPrefix(ff.String(), "sync/atomic.Store") {
			warns[nn] = true
		}
	}
	allfuncs = keep

	{
		uniq := make(map[string]bool)
		for ff := range warns {
			for _, c := range ff.In {
				s := fmt.Sprintf("%v -> %v",
					c.Caller.Func.String(),
					c.Callee.Func.String())
				uniq[s] = true
			}
		}
		var t []string
		for s := range uniq {
			t = append(t, s)
		}
		sort.Strings(t)
		fmt.Printf("ATOMIC STORE calls\n")
		for _, s := range t {
			fmt.Printf("\t%v\n", s)
		}
	}

	mkboundcalls(pkgs, allfuncs)

	mkchancalls(pkgs, allfuncs)

	mkevictcalls(pkgs, allfuncs)

	// find all stores and store-like instructions
	var stores qs_t
	stores.qinit(pkgs)
	for _, fun := range allfuncs {
		for _, blk := range fun.Blocks {
			for _, in := range blk.Instrs {
				switch rin := in.(type) {
				case *ssa.MakeInterface:
					stores.addop(rin)
				// append() hides assignments since it is
				// builtin and thus this tool never observes
				// the store to a slice element. if the element
				// can point, manually add the element argument
				// of append to the list of possible values of
				// this store.
				case *ssa.Call:
					wtf, ok := rin.Call.Value.(*ssa.Builtin)
					if !ok {
						break
					}
					if wtf.Name() == "append" {
						// the second argument to append has slice type
						av := rin.Common().Args[1]
						// language spec allows append
						// string to slice of bytes,
						// which means the element
						// argument has type basic...
						if _, ok := av.Type().(*types.Basic); ok {
							break
						}
						sav := av.Type().(*types.Slice)
						if canpoint(sav.Elem()) {
							stores.addop(rin)
						}
					}

				case *ssa.MapUpdate:
					if canpoint(rin.Key.Type()) ||
						canpoint(rin.Value.Type()) {
						stores.addop(rin)
					}
				case *ssa.Store:
					ue := rin.Addr.Type().(*types.Pointer).Elem()
					if canpoint(ue) {
						if !canpoint(rin.Val.Type()) {
							panic("wtf")
						}
						stores.addop(rin)
					}
				}
			}
		}
	}
	stores.analyze()

	// XXX merge both store analyses?
	didid := 0
	didids := make(map[ssa.Value]int)
	for _, r := range roots {
		didids[r] = didid
		didid++
		//fmt.Printf("SOOT %v %v\n", didids[r], r)
	}
	didvals := make(map[ssa.Value]bool)
	rnd := 0
	for len(roots) != 0 {
		fmt.Printf("round %v\n", rnd)
		rnd++
		var newroots []ssa.Value
		for _, r := range roots {
			stores.iiter(func(in ssa.Instruction, ap []*pointer.Label, vp []*pointer.Label) {
				addallocs := false
				for _, l := range ap {
					if l.Value() == r {
						addallocs = true
						break
					}
				}
				// check for direct store to static storage
				if !addallocs {
					var addr ssa.Value
					switch rin := in.(type) {
					case *ssa.MapUpdate:
						addr = rin.Map
					case *ssa.Store:
						addr = rin.Addr
					}
					if addr == r {
						addallocs = true
					}
				}
				if !addallocs {
					return
				}

				// the address pointer may point to a root, add
				// any allocations that may be written by this
				// store to the root set
				for _, l := range vp {
					val := l.Value()
					if !didvals[val] {
						didids[val] = didid
						didid++
						didvals[val] = true
						newroots = append(newroots, val)
						//sl := inpos(in)
						//fmt.Printf("   ROOT %v <- %v %v %T %v %v\n", didids[r], didids[val], sl, val, val.Type(), val.Parent())
					}
				}
			})
		}
		roots = newroots
	}
	fmt.Printf("dyune!\n")

	for val := range didvals {
		permanent[val] = true
	}

	alldefs := make(map[*ssa.Function]map[ssa.Value]bool)
	// make sure go stack allocations are marked permanent and that defer
	// allocations are in each functions pointer set.
	for _, ff := range allfuncs {
		defs := make(map[ssa.Value]bool)
		for _, bb := range ff.Blocks {
			for _, in := range bb.Instrs {
				switch rin := in.(type) {
				default:
					continue
				case *ssa.Go:
					val := rin.Common().Value
					permanent[val] = true
				case *ssa.Defer:
					val := rin.Common().Value
					defs[val] = true
				}
			}
		}
		if len(defs) > 0 {
			alldefs[ff] = defs
		}
	}

	var funq qs_t
	funq.qinit(pkgs)
	funcptrs := make(map[*ssa.Function][]*pointer.Pointer)
	for _, ff := range allfuncs {
		var ptrs []*pointer.Pointer
		rds := make([]*ssa.Value, 10)
		for _, bb := range ff.Blocks {
			for _, in := range bb.Instrs {
				rds = rds[0:0]
				if _, ok := in.(ssa.Instruction); !ok {
					continue
				}
				rds = in.Operands(rds)
				for _, rd := range rds {
					if *rd == nil {
						continue
					}
					rt := (*rd).Type()
					if !canpoint(rt) {
						continue
					}
					//sl := inpos(in)
					//fmt.Printf("%v %T %v\n", *rd, *rd, sl)
					if st, ok := typetostruct(rt); ok {
						ptrs = append(ptrs, funq._structquery(*rd, st)...)
					} else {
						ptrs = append(ptrs, funq._query(*rd))
					}
				}
			}
		}
		funcptrs[ff] = ptrs
	}
	funq.analyze()

	for ff, ptrs := range funcptrs {
		uniq := make(map[ssa.Value]bool)
		for _, ptr := range ptrs {
			for _, l := range ptr.PointsTo().Labels() {
				v := l.Value()
				uniq[v] = true
			}
		}
		funcreach[ff] = uniq
		//fmt.Printf("FUNC %v\n", ff)
		//for _, v := range us {
		//	fmt.Printf("    %v %T %v\n", v, v, v.Parent())
		//}
	}

	for ff, defs := range alldefs {
		old := funcreach[ff]
		for defval := range defs {
			old[defval] = true
		}
		funcreach[ff] = old
	}

	// values have stores to the keys
	transitp := make(map[ssa.Value]map[ssa.Value]bool)
	stores.iiter(func(in ssa.Instruction, ap []*pointer.Label, vp []*pointer.Label) {
		vvals := make(map[ssa.Value]bool)
		for _, l := range vp {
			vvals[l.Value()] = true
		}
		for _, l := range ap {
			av := l.Value()
			if old, ok := transitp[av]; ok {
				for nv := range vvals {
					old[nv] = true
				}
			} else {
				transitp[av] = vvals
			}
		}
	})

	// add all allocations that are transitively reachable from each
	// function's allocations
	for _, vals := range funcreach {
		for {
			changed := false
			for v := range vals {
				tran, ok := transitp[v]
				if !ok {
					continue
				}
				for nt := range tran {
					if vals[nt] {
						continue
					}
					vals[nt] = true
					changed = true
				}
			}
			if !changed {
				break
			}
		}
	}

	// ignore permanent allocs since they are handled separately
	for _, vals := range funcreach {
		for v := range vals {
			del := false
			if permanent[v] {
				del = true
			}
			switch rv := v.(type) {
			case *ssa.MakeInterface, *ssa.Convert, *ssa.Function,
				*ssa.Global:
				del = true
			case *ssa.Alloc:
				if !rv.Heap {
					del = true
				}
			}
			if del {
				delete(vals, v)
			}
		}
	}

	//for ff, vals := range funcreach {
	//	if len(vals) == 0 {
	//		continue
	//	}
	//	fmt.Printf("FUNC %v\n", ff)
	//	for v := range vals {
	//		var sl token.Position
	//		if in, ok := v.(ssa.Instruction); ok {
	//			sl = inpos(in)
	//		}
	//		fmt.Printf("    %v %T %v %v\n", v, v, v.Parent(), sl)
	//	}
	//}
}

var symdb = map[string]int{
	// the quotation marks are included in the argument values read out of
	// the annotation calls
	// raising fds to 1024 only increases fork's bound by ~200kB
	"\"fds\"":  1 << 9,
	"\"vmas\"": 1 << 10,

	// these bounds changes depending on which reservation size we are
	// computing: the log daemon or syscalls. these can go away once
	// combining block linked lists for ahci requests are allocation-less
	"\"amlogdaemon1\"": 1, // syscall
	//"\"amlogdaemon1\"": 256, // log daemon

	//"\"amlogdaemon2\"": 1, // syscall
	//"\"amlogdaemon2\"": 3000, // log daemon
}

// types to save maximum loop bounds and slice sizes.
type nument_t struct {
	file  string
	sline int
	eline int
	bound int
}

type numdb_t struct {
	ents []nument_t
}

func (n *numdb_t) lookup(posi token.Position) (int, bool) {
	for _, e := range n.ents {
		//fmt.Printf("%v %v\n", posi.Filename, e.file)
		if posi.Filename != e.file {
			continue
		}
		if posi.Line >= e.sline && posi.Line <= e.eline {
			return e.bound, true
		}
	}
	return 0, false
}

var BROOT = "/home/ccutler/biscuit/biscuit/src/"

var RUNT = "/home/ccutler/biscuit/src/"
var MUT = RUNT + "sync/mutex.go"

// INF is just a large constant for unbounded loops. the allocations for such
// loops need special handling, like evicting as much memory as they allocate
// when memory is tight
var INF = -1

var loopdb = numdb_t{
	ents: []nument_t{
		{MUT, 55, 91, INF},   // Mutex.Lock()
		{MUT, 116, 130, INF}, // Mutex.Unlock()
	},
}

func loopbound(fun string, posi token.Position) int {
	if num, ok := loopdb.lookup(posi); ok {
		return num
	}
	return readnum(fmt.Sprintf("LOOP BOUND at %v %v: ", fun, posi))
}

var slicedb = numdb_t{
	ents: []nument_t{
		//  sync.Pool caches are unbounded...
		{RUNT + "sync/pool.go", 101, 101, 1024},
		{RUNT + "sync/pool.go", 201, 201, 10}, // total sync.Pools
		{RUNT + "sync/pool.go", 205, 205, 64}, // per-cpu caches
	},
}

// v should be an *ssa.MakeSlice of an *ssa.Call (for append)
func slicebound(in ssa.Instruction, cs *callstack_t) int {
	var v ssa.Value
	switch rin := in.(type) {
	default:
		panic("no")
	case *ssa.Call:
		v = rin.Value()
	case *ssa.MakeSlice:
		v = rin
	}
	if n, ok := sboundcalls[v]; ok {
		return n
	}
	posi := inpos(in)
	if num, ok := slicedb.lookup(posi); ok {
		return num
	}
	return readnum(fmt.Sprintf("MAX SLICE LENGTH at %v: ", posi))
}

var mapdb = numdb_t{
	ents: []nument_t{},
}

func mapbound(v ssa.Instruction) int {
	if n, ok := sboundcalls[v.(*ssa.MakeMap)]; ok {
		return n
	}
	posi := inpos(v)
	if num, ok := mapdb.lookup(posi); ok {
		return num
	}
	return readnum(fmt.Sprintf("MAX MAP BUCKETS at %v: ", posi))
}

// a type for all natural loops in a single function
type funcloops_t struct {
	loops []*natl_t
}

func (fl *funcloops_t) distinctloop(n *natl_t) {
	fl.loops = append(fl.loops, n)
}

func (fl *funcloops_t) dump() {
	for _, l := range fl.loops {
		l.dump(0)
	}
}

func (fl *funcloops_t) maxdepth() int {
	max := 0
	for _, l := range fl.loops {
		got := l.maxdepth1(1)
		if got > max {
			max = got
		}
	}
	return max
}

func (fl *funcloops_t) iter(fun func(*natl_t)) {
	for _, l := range fl.loops {
		l.iter(fun)
	}
}

// a type for a node in the natural loop tree
type natl_t struct {
	head   *ssa.BasicBlock
	lblock map[*ssa.BasicBlock]bool
	nests  []*natl_t
	lcalls *calls_t
}

func newnatl(head *ssa.BasicBlock) *natl_t {
	return &natl_t{head: head, lblock: map[*ssa.BasicBlock]bool{head: true}}
}

func (nt *natl_t) loopblock(lb *ssa.BasicBlock) {
	nt.lblock[lb] = true
}

func (nt *natl_t) loopnest(n *natl_t) {
	nt.nests = append(nt.nests, n)
}

func (nt *natl_t) dump(depth int) {
	for i := 0; i < depth; i++ {
		fmt.Printf("  ")
	}
	fmt.Printf("| ")
	for bn := range nt.lblock {
		fmt.Printf(" %v", bn)
	}
	fmt.Printf(" [ %v %v ]\n" /*nt.liters*/, "?", nt.lcalls.total(nil))
	for _, nest := range nt.nests {
		nest.dump(depth + 1)
	}
}

func (nt *natl_t) maxdepth1(d int) int {
	max := d
	for _, l := range nt.nests {
		got := l.maxdepth1(d + 1)
		if got > max {
			max = got
		}
	}
	return max
}

// iterates in post-order
func (nt *natl_t) iter(fun func(*natl_t)) {
	for _, nat := range nt.nests {
		nat.iter(fun)
	}
	fun(nt)
}

func (nt *natl_t) boundcall(pkgs []*ssa.Package,
	usedbounds map[*ssa.BasicBlock]bool) (int, string,
	string, bool) {

	BB := ffunc(pkgs, "BOUND")
	NB := ffunc(pkgs, "NBOUND")
	YB := ffunc(pkgs, "SYMBOUND")

	ret := -1
	found := false
	var sym string
	var name string
	for bb := range nt.lblock {
		for _, in := range bb.Instrs {
			rin, ok := in.(*ssa.Call)
			if !ok {
				continue
			}
			comm := rin.Common()
			sc := comm.StaticCallee()
			if sc == nil {
				continue
			}
			if usedbounds[bb] {
				// this is a BOUND call for a nested loop
				continue
			}
			isbb := sc == BB
			isnb := sc == NB
			isyb := sc == YB
			if isbb || isnb || isyb {
				if found {
					panic("two BOUND calls in one loop")
				}
				found = true
				usedbounds[bb] = true
				name = inpos(rin).String()
				if isbb || isnb {
					ret = int(comm.Args[0].(*ssa.Const).Int64())
				}
				if isnb {
					name = comm.Args[1].(*ssa.Const).Value.String()
				} else if isyb {
					name = comm.Args[0].(*ssa.Const).Value.String()
					sym = comm.Args[1].(*ssa.Const).Value.String()
					if tret, ok := symdb[sym]; ok {
						ret = tret
					} else {
						fmt.Printf("(%v)\n", sym)
						panic("no such sym")
					}
				}
			}
		}
	}
	return ret, name, sym, found
}

// yahoooo...
type natsort_t struct {
	nats []*natl_t
}

func (ns *natsort_t) Len() int {
	return len(ns.nats)
}

func (ns *natsort_t) Less(i, j int) bool {
	return len(ns.nats[i].lblock) < len(ns.nats[j].lblock)
}

func (ns *natsort_t) Swap(i, j int) {
	ns.nats[i], ns.nats[j] = ns.nats[j], ns.nats[i]
}

func funcloops(sf *ssa.Function) *funcloops_t {
	// identify back edges
	nats := make([]*natl_t, 0)
	for _, bb := range sf.Blocks {
		for _, suc := range bb.Succs {
			if suc.Dominates(bb) {
				nats = append(nats, nloop(suc, bb))
			}
		}
	}
	// merge loops that share a head block
	remove := func(i int) {
		copy(nats[i:], nats[i+1:])
		nats = nats[:len(nats)-1]
	}
	for changed := true; changed; {
		changed = false
		for i, out := range nats {
			for j, in := range nats {
				if i == j {
					continue
				}
				if out.head == in.head {
					changed = true
					for bn := range in.lblock {
						out.loopblock(bn)
					}
					remove(j)
					break
				}
			}
			if changed {
				break
			}
		}
	}
	// all loops either distinct or nested
	for i, out := range nats {
		for j, in := range nats {
			if i == j {
				continue
			}
			sm := len(out.lblock)
			if len(in.lblock) < sm {
				sm = len(in.lblock)
			}
			same := 0
			for bn := range out.lblock {
				if in.lblock[bn] {
					same++
				}
			}
			if same != 0 && same != sm {
				panic("partial still")
			}
		}
	}
	nsort := &natsort_t{nats}
	// sort loops by number of blocks. thus a nested loop's containing loop
	// will have a higher index.
	sort.Sort(nsort)
	pars := make(map[*natl_t]*natl_t)
	for i, nat := range nats {
		if pars[nat] != nil {
			panic("wut")
		}
		// the parent for this natural loop, if any, will be the
		// natural loop whose blocks a superset of this one's and that
		// has the smallest index (but larger than this loop's index)
		for j := i + 1; j < len(nats); j++ {
			tnat := nats[j]
			issuper := true
			for bn := range nat.lblock {
				if !tnat.lblock[bn] {
					issuper = false
					break
				}
			}
			if issuper {
				pars[nat] = tnat
				break
			}
		}
	}
	// build loop tree
	fl := &funcloops_t{}
	for _, nat := range nats {
		if par, ok := pars[nat]; ok {
			par.loopnest(nat)
		} else {
			fl.distinctloop(nat)
		}
	}
	return fl
}

func nloop(h, t *ssa.BasicBlock) *natl_t {
	ret := newnatl(h)
	for _, bb := range h.Parent().Blocks {
		if bb == h || !h.Dominates(bb) {
			continue
		}
		v := map[*ssa.BasicBlock]bool{h: true}
		if blockpath1(bb, t, v) {
			ret.loopblock(bb)
		}
	}
	return ret
}

// calculates for loop depth using ast tree instead of ssa to make sure my loop
// detection code is correct. this function requires Pkg.SetDebugMode(true).
func fordepth(fast ast.Node) int {
	if fast == nil {
		return -1
	}
	fd := fast.(*ast.FuncDecl)
	return fordepth1(fd.Body.List)
}

func fordepth1(fast []ast.Stmt) int {
	max := 0
	found := false
	for _, _st := range fast {
		var tries [][]ast.Stmt
		addt := func(s []ast.Stmt) {
			tries = append(tries, s)
		}
		switch st := _st.(type) {
		default:
			//fmt.Printf("HANDLE %T\n", st)
		case *ast.ExprStmt, *ast.AssignStmt:
			// nothing
		case *ast.ForStmt:
			addt(st.Body.List)
			found = true
		case *ast.IfStmt:
			addt(st.Body.List)
			if st.Else == nil {
				break
			}
			switch bb := st.Else.(type) {
			default:
				//fmt.Printf("ELSE HANDLE: %T\n", bb)
			case *ast.BlockStmt:
				addt(bb.List)
			}
		case *ast.RangeStmt:
			found = true
			addt(st.Body.List)
		case *ast.SelectStmt:
			addt(st.Body.List)
		case *ast.SwitchStmt:
			addt(st.Body.List)
		case *ast.TypeSwitchStmt:
			addt(st.Body.List)
		case *ast.CaseClause:
			addt(st.Body)
		case *ast.CommClause:
			addt(st.Body)
		}
		for _, try := range tries {
			got := fordepth1(try)
			if got > max {
				max = got
			}
		}
	}
	if found {
		max += 1
	}
	return max
}
