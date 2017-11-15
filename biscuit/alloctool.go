/*
 * TODO
 * - Verify that Make{Interface, Closure} do not cause more heap
 *   allocations besides the ssa.Allocs already present for them.
 * - sends on buffered channel accumulate live data too
 * 	- I dumped all channel sends with pointer types and verified that all
 * 	  such sends are on unbuffered channels, thus the live data
 * 	  accumulation is already accounted for in the allocation of the object
 * 	  being sent. This check could be easily automated via more pointer
 * 	  analysis.
 * - account for append and any other builtins that may affect the analysis
 * - recursive iteration loops
 */

/*
 * TO USE
 * - make sure the looping calls that this tool ignores are actually prevented
 *   at runtime (this was the case at the time of writing)
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

type halp_t struct {
	cg *callgraph.Graph
}

func (h *halp_t) init(cg *callgraph.Graph) {
	h.cg = cg
}

var _s = types.StdSizes{WordSize: 8, MaxAlign: 8}

func array_align(t types.Type) int {
	truesz := _s.Sizeof(t)
	align := _s.Alignof(t)
	if truesz % align != 0 {
		diff := align - (truesz % align)
		truesz += diff
	}
	return int(truesz)
}

func slicesz(v *ssa.MakeSlice, num int) int {
	// the element type could be types.Named...
	elmsz := array_align(v.Type().(*types.Slice).Elem())
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
	return int(num)*elmsz + 3*8
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
	cs	[]string
}

func (cs *callstack_t) push(f *ssa.Function) {
	cs.cs = append(cs.cs, realname(f))
}

func (cs *callstack_t) pop() {
	l := len(cs.cs)
	cs.cs = cs.cs[:l - 1]
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

type calls_t struct {
	me	string
	csum	int
	looper	bool
	loopend	bool
	// cannot share nodes that have parent fields
	//par	*calls_t
	calls	[]*calls_t
}

func newcalls(me string) *calls_t {
	return &calls_t{me: me, calls: make([]*calls_t, 0)}
}

func newloopend() *calls_t {
	return &calls_t{loopend: true, calls: make([]*calls_t, 0)}
}

func (c *calls_t) addchild(child *calls_t) {
	if child != nil {
		//child.par = c
		c.calls = append(c.calls, child)
	}
}

func (c *calls_t) dump() {
	c.dump1(0)
}

func human(_bytes int) string {
	bytes := float64(_bytes)
	div := float64(1)
	order := 0
	for bytes / div > 1024 {
		div *= 1024
		order++
	}
	sufs := map[int]string{0: "B", 1: "kB", 2: "MB", 3: "GB", 4: "TB",
	    5: "PB"}
	return fmt.Sprintf("%.2f%s", float64(bytes) / div, sufs[order])
}

func (c *calls_t) dump1(depth int) {
	for i := 0; i < depth; i++ {
		fmt.Printf("  ")
	}
	lop := ""
	if c.looper {
		lop = "=L"
	}
	fmt.Printf("%c %s [%v]%s\n", 'A' + depth, c.me, human(c.csum), lop)
	for i := range c.calls {
		c.calls[i].dump1(depth + 1)
	}
}

// map of callees to list of impossible callers.
var _impossible = map[string][]string {
	//"(*main.pgcache_t).flush" : []string{"(*main.pgcache_t).evict"},
}

func ignore(f *ssa.Function, cs *callstack_t) bool {
	me := realname(f)
	//for _, t := range []string{"fmt.", "mbempty", "mbread", "memreclaim", ".ibread",
	//    "_ensureslot", "_dceadd", "idaemon_ensure", "fdelist_t).addhead", ".ibempty"} {
	for _, t := range []string{"fmt."} {
	//for _, t := range []string{"fmt.", "._mbensure", ".ibread", "frbh_t).insert", "_ensureslot",
	//    "dc_rbh_t).insert", "fdelist_t).addhead", "trymutex_t).tm_init"} {
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

var funcvisit = map[*callgraph.Node]bool{}

var memos = map[*callgraph.Node]*calls_t{}

func maxcall(tnode *callgraph.Node, target *ssa.CallCommon, cs *callstack_t) (*calls_t, bool) {
	var calls *calls_t
	found := false
	var max int
	for _, e := range tnode.Out {
		// XXX how to identify the particular call instruction from
		// ssa.CallInstruction provided by callgraph.Edge? is comparing
		// their ssa.CallCommons the correct way?
		tt := e.Site.Common()
		if tt != target {
			continue
		}
		if funcvisit[e.Callee] {
			fmt.Printf("LOOP AVOID %v %v\n", tnode.Func, e.Callee.Func)
			for _, e := range cs.cs {
				fmt.Printf("\t%v\n", e)
			}
			continue
		}
		if ignore(e.Callee.Func, cs) {
			me := e.Callee.Func.String()
			fmt.Printf("**** SKIP %s\n", me)
			continue
		}
		found = true
		var tc *calls_t
		if v, ok := memos[e.Callee]; ok {
			tc = v
		} else {
			tc = funcsum(e.Callee, cs)
			memos[e.Callee] = tc
		}
		if tc.csum >= max {
			max = tc.csum
			calls = tc
		}
	}
	if found && calls == nil {
		panic("wtf")
	}
	return calls, found
}

type enode_t struct {
	exitedge	*ssa.BasicBlock
	loopedge	*ssa.BasicBlock
	maxiter		int
	loopalloc	int
	loopcalls	*calls_t
}

func (e *enode_t) isexit(suc *ssa.BasicBlock) bool {
	return suc == e.exitedge
}

func (e *enode_t) cost() int {
	ret := e.maxiter * e.loopalloc
	if ret < 0 {
		panic("oh shit")
	}
	return ret
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
	dur := bufio.NewReader(os.Stdin)
	str, err := dur.ReadString('\n')
	if err != nil {
		goto again
	}
	ret, err = strconv.Atoi(str[:len(str) - 1])
	if err != nil {
		goto again
	}
	return ret
}

var _exits = map[*ssa.BasicBlock]*enode_t{}

// find the maximum allocation cost that starts and ends at cur
func calcexit(node *callgraph.Node, cur *ssa.BasicBlock, cs *callstack_t) *enode_t {
	ret := &enode_t{}
	exit, loop, ok := ammexit(cur)
	if !ok {
		panic("not exit node")
	}
	ret.exitedge = exit
	ret.loopedge = loop

	ret.loopcalls = _loopblocks(node, cur, cur, cs)
	if !ret.loopcalls.loopend {
		panic("nein!")
	}
	ret.loopalloc = ret.loopcalls.csum
	iterinput := -1
	if ret.loopalloc > 0 {
		if biter, ok := findboundcall(cur); ok {
			iterinput = biter
		} else {
			posi, ok := revblksrc(cur)
			if !ok {
				panic("no")
			}
			iterinput = loopbound(posi)
		}
	} else {
		iterinput = 0
	}
	ret.maxiter = iterinput
	return ret
}

func findboundcall(cur *ssa.BasicBlock) (int, bool) {
	if _, _, ok := ammexit(cur); !ok {
		panic("only call on exit nodes")
	}
	// BFS to find shortest loop back to this exit node, search all
	// instructions for special call
	type bnd_t struct {
		bb *ssa.BasicBlock
		next *bnd_t
	}
	// map block to parent block in this BFS search
	var final *ssa.BasicBlock
	par := make(map[*ssa.BasicBlock]*ssa.BasicBlock)
	head := &bnd_t{cur, nil}
	tail := head
outter:
	for head != nil {
		n := head.bb
		for _, suc := range n.Succs {
			// finish loop?
			if suc == cur {
				final = n
				break outter
			} else if _, ok := par[suc]; !ok {
				par[suc] = n
				nn := &bnd_t{suc, nil}
				tail.next = nn
				tail = nn
			}
		}
		head = head.next
	}
	if final == nil {
		panic("no loop; not exit")
	}
	BB, ok := cur.Parent().Pkg.Members["BOUND"]
	if !ok {
		panic("no BOUND() defined")
	}
	for final != nil {
		for _, in := range final.Instrs {
			rin, ok := in.(*ssa.Call)
			if !ok {
				continue
			}
			sc := rin.Common().StaticCallee()
			if sc == nil {
				continue
			}
			if sc == BB {
				fmt.Printf("BOUNDER %v\n", rin)
			}
		}
		final = par[final]
	}
	return 0, false
}

func mkexitnodes(node *callgraph.Node, cur *ssa.BasicBlock, cs *callstack_t) {
	visited := make(map[*ssa.BasicBlock]bool)
	mkexitnodes1(node, cur, visited, cs)
}

func mkexitnodes1(node *callgraph.Node, cur *ssa.BasicBlock,
    visited map[*ssa.BasicBlock]bool, cs *callstack_t) {
	if _, ok := visited[cur]; ok {
		return
	}
	visited[cur] = true
	// is cur an exit node?
	_, _, amexit := ammexit(cur)
	// calculate exit values in post-order since cost of outter loop
	// includes inner loops.
	for _, suc := range cur.Succs {
		mkexitnodes1(node, suc, visited, cs)
	}
	// we found all exit nodes among this block's children in this function
	if amexit {
		n := calcexit(node, cur, cs)
		_exits[cur] = n
	}
}

// returns exit edge, loop edge, whether exit node
func ammexit(cur *ssa.BasicBlock) (*ssa.BasicBlock, *ssa.BasicBlock, bool) {
	if len(cur.Succs) != 2 {
		return nil, nil, false
	}
	a := blockpath(cur.Succs[0], cur) && !blockpath(cur.Succs[1], cur)
	b := !blockpath(cur.Succs[0], cur) && blockpath(cur.Succs[1], cur)
	if a {
		return cur.Succs[1], cur.Succs[0], true
	}
	if b {
		return cur.Succs[0], cur.Succs[1], true
	}
	return nil, nil, false
}

// _loopblocks only considers loop iterations, i.e. paths that reach the given
// exit node.
func _loopblocks(node *callgraph.Node, blk, exitblk *ssa.BasicBlock, cs *callstack_t) *calls_t {
	visited := make(map[*ssa.BasicBlock]bool)
	return _loopblocks1(node, blk, exitblk, visited, cs)
}

func _loopblocks1(node *callgraph.Node, blk, exitblk *ssa.BasicBlock,
    visit map[*ssa.BasicBlock]bool, cs *callstack_t) *calls_t {
	if visit[blk] {
		if blk == exitblk {
			panic("should have terminated before")
		}
		//fmt.Printf("%v NOPPER\n", node.Func)
		return newcalls("NOLOOP")
	}
	visit[blk] = true
	defer delete(visit, blk)

	calls := newcalls(node.Func.String())
	var ret int
	for _, ip := range blk.Instrs {
		switch v := ip.(type) {
		//case *ssa.Go:
		//	sl := node.Func.Prog.Fset.Position(v.Pos())
		//	fmt.Printf("IGNORE GO SUM %v\n", sl)
		case ssa.CallInstruction:
			// see if this is a fake call with bound information
			comm := v.Common()
			// find which function for this call site allocates
			// most
			maxc, found := maxcall(node, comm, cs)
			if found {
				// ignore calls that don't allocate
				if maxc.csum > 0 {
					ret += maxc.csum
					calls.addchild(maxc)
				}
			} else {
				//fmt.Printf("failed for: %v\n", v)
			}
		case *ssa.MakeChan:
			if !reachable[v] {
				continue
			}
			ret += chansz(v)
		case *ssa.MakeSlice:
			if !reachable[v] {
				continue
			}
			num := slicebound(v)
			ret += slicesz(v, num)
		case *ssa.Alloc:
			if !v.Heap || !reachable[v] {
			//if !v.Heap {
				continue
			}
			tp := v.Type().Underlying().(*types.Pointer)
			ut := tp.Elem().Underlying()
			truesz := array_align(ut)
			ret += truesz
		}
	}
	// find maximum allocating block included in a loop
	exfound := false
	got := false
	var maxc calls_t
	for _, suc := range blk.Succs {
		var tc *calls_t
		if suc == exitblk {
			tc = newloopend()
		} else if !blockpath(suc, exitblk) {
			continue
		} else {
			tc = _loopblocks1(node, suc, exitblk, visit, cs)
		}
		if tc.loopend {
			exfound = true
		}
		if exfound && !tc.loopend {
			continue
		}
		got = true
		if tc.csum >= maxc.csum {
			maxc = *tc
		}
	}
	if !got {
		panic("loop succ must exist; called on non-loop node?")
	}
	ret += maxc.csum
	calls.csum = ret
	// merge sibling calls
	for _, tc := range maxc.calls {
		calls.addchild(tc)
	}
	// only consider loops
	calls.loopend = maxc.loopend
	if !calls.loopend {
		calls.csum = 0
	}
	return calls
}

func funcsum(node *callgraph.Node, cs *callstack_t) *calls_t {
	if funcvisit[node] {
		panic("should terminate in maxcall")
	}
	funcvisit[node] = true
	defer delete(funcvisit, node)

	if len(node.Func.Blocks) == 0 {
		fmt.Printf("**** NO BLOCKS %v\n", node.Func)
		return newcalls("NOB")
	}
	cs.push(node.Func)
	defer cs.pop()
	// the function entry block has Index == 0 and is the first block in
	// the slice
	entry := node.Func.Blocks[0]
	// find exit nodes, compute maximum iteration allocation
	mkexitnodes(node, entry, cs)

	visited := make(map[*ssa.BasicBlock]bool)
	return _sumblock(node, entry, visited, cs)
}

// _sumblock is called on a function's blocks once all of a function's exit
// nodes have been found and calculated.
func _sumblock(node *callgraph.Node, blk *ssa.BasicBlock,
    visit map[*ssa.BasicBlock]bool, cs *callstack_t) *calls_t {
	if visit[blk] {
		//fmt.Printf("BLOCK LOOP AVOID in %v\n", node.Func)
		return newcalls("LOOP")
	}
	visit[blk] = true
	defer delete(visit, blk)

	calls := newcalls(node.Func.String())
	var ret int
	for _, ip := range blk.Instrs {
		switch v := ip.(type) {
		//case *ssa.Go:
		//	sl := node.Func.Prog.Fset.Position(v.Pos())
		//	fmt.Printf("IGNORE GO SUM %v\n", sl)
		case ssa.CallInstruction:
			// find which function for this call site allocates
			// most
			maxc, found := maxcall(node, v.Common(), cs)
			if found {
				// ignore calls that don't allocate
				if maxc.csum > 0 {
					ret += maxc.csum
					calls.addchild(maxc)
				}
			} else {
				fmt.Printf("failed for: %v\n", v)
			}
		case *ssa.MakeChan:
			if !reachable[v] {
				continue
			}
			ret += chansz(v)
		case *ssa.MakeSlice:
			if !reachable[v] {
				continue
			}
			num := slicebound(v)
			ret += slicesz(v, num)
		case *ssa.Alloc:
			if !v.Heap || !reachable[v] {
			//if !v.Heap {
				continue
			}
			tp := v.Type().Underlying().(*types.Pointer)
			ut := tp.Elem().Underlying()
			truesz := array_align(ut)
			ret += truesz
		}
	}
	// find which successor block allocates most. if this is an exit node,
	// we must charge for the full loop allocation cost to the path that
	// takes the exit edge.
	enode, amexit := _exits[blk]
	var chosesuc *ssa.BasicBlock
	var max int
	var maxc *calls_t
	for _, suc := range blk.Succs {
		tc := _sumblock(node, suc, visit, cs)
		if amexit && enode.isexit(suc) {
			bonus := enode.cost()
			// add loop cost
			tc.csum += bonus
		}
		if tc.csum > max {
			chosesuc = suc
			max = tc.csum
			maxc = tc
		}
		if tc.looper {
			calls.looper = true
		}
	}
	if amexit {
		calls.looper = true
		// use exit node calls, which are a super set of this blocks
		// calls
		if enode.isexit(chosesuc) {
			calls.calls = enode.loopcalls.calls
		}
	}
	if maxc != nil {
		for _, tc := range maxc.calls {
			calls.addchild(tc)
		}
		ret += maxc.csum
	}
	calls.csum = ret
	return calls
}

func (h *halp_t) dumpcallees(root *ssa.Function) {
	rootnode, ok := h.cg.Nodes[root]
	if !ok {
		panic("no")
	}
	fmt.Printf("ROOT: %v\n", root)
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
		cees := callgraph.CalleesOf(f)
		for cnode := range cees {
			if d := did[cnode]; d {
				continue
			}
			did[cnode] = true
			left[cnode] = true
			s := fmt.Sprintf("%s --> %s", f.Func, cnode.Func)
			edges = append(edges, s)
		}
	}
	//for k := range did {
	//	fmt.Printf("%v\n", k.Func)
	//}

	calls := funcsum(rootnode, &callstack_t{})
	fmt.Printf("TOTAL ALLOCATIONS for %v: %v\n", root, human(calls.csum))
	fmt.Printf("calls:\n")
	calls.dump()

	// Print the edges in sorted order.
	//sort.Strings(edges)
	//for _, edge := range edges {
	//	fmt.Println(edge)
	//}
	//fmt.Println()
}

func crud() {
	c := loader.Config{}
	c.CreateFromFilenames("main", "/home/ccutler/dur.go")
	//c.Import("runtime")
	prog, err := c.Load()
	if err != nil {
		fmt.Printf("shite: %v\n", err)
		return
	}
	fmt.Printf("%v\n", prog.Package("main").Files)
	sprog := ssautil.CreateProgram(prog, ssa.NaiveForm)
	sprog.Build()
	fmt.Printf("**** %v\n", sprog)
}

func main() {
	c := loader.Config{}
	//c.CreateFromFilenames("main", "/home/ccutler/dur.go")
	c.CreateFromFilenames("main",
		"/home/ccutler/biscuit/biscuit/main.go",
		"/home/ccutler/biscuit/biscuit/syscall.go",
		"/home/ccutler/biscuit/biscuit/fs.go",
		"/home/ccutler/biscuit/biscuit/pmap.go",
		"/home/ccutler/biscuit/biscuit/hw.go",
		"/home/ccutler/biscuit/biscuit/fsrb.go",
		"/home/ccutler/biscuit/biscuit/bins.go",
		"/home/ccutler/biscuit/biscuit/net.go")
	iprog, err := c.Load()
	if err != nil {
		fmt.Println(err) // type error in some package
		return
	}

	// Create SSA-form program representation.
	prog := ssautil.CreateProgram(iprog, 0)
	mpkg := ssautil.MainPackages(prog.AllPackages())[0]
	//_sysfunc, ok := mpkg.Members["sys_recvmsg"]
	//_sysfunc, ok := mpkg.Members["proc_new"]
	//_sysfunc, ok := mpkg.Members["sys_socket"]
	//_sysfunc, ok := mpkg.Members["main"]
	//_sysfunc, ok := mpkg.Members["syscall"]
	_sysfunc, ok := mpkg.Members["flea"]
	if !ok {
		panic("none")
	}
	sysfunc := _sysfunc.(*ssa.Function)

	//T := mpkg.Type("imemnode_t").Type()
	//pT := types.NewPointer(T)
	//sysfunc := prog.LookupMethod(pT, mpkg.Pkg, "_deinsert")
	//T := mpkg.Type("proc_t").Type()
	//pT := types.NewPointer(T)
	//sysfunc := prog.LookupMethod(pT, mpkg.Pkg, "run")
	// Build SSA code for bodies of all functions in the whole program.
	mpkg.SetDebugMode(true)
	prog.Build()

	reachallocs(sysfunc)
	//natch(sysfunc)

	cg := mkcallgraph(sysfunc.Prog)
	var h halp_t
	h.init(cg)
	h.dumpcallees(sysfunc)
}

func mkcallgraph(prog *ssa.Program) *callgraph.Graph {
	pkgs := ssautil.MainPackages(prog.AllPackages())

	// Configure the pointer analysis to build a call-graph.
	config := &pointer.Config{
		Mains:          pkgs,
		BuildCallGraph: true,
	}
	result, err := pointer.Analyze(config)
	if err != nil {
		panic(err)
	}
	return result.CallGraph
}

func findmaplookup(cg *callgraph.Graph, glob *ssa.Global) ssa.Value {
	for _, node := range cg.Nodes {
		for _, blk := range node.Func.Blocks {
			for _, in := range blk.Instrs {
				lk, ok := in.(*ssa.Lookup)
				if !ok {
					continue
				}
				//fmt.Printf("%v %v %T\n", lk, lk.X.Type(), lk.X)
				uo, ok := lk.X.(*ssa.UnOp)
				if !ok {
					continue
				}
				//fmt.Printf("unop: %v, %v %T (%v)\n", uo, uo.Type(), uo, uo.Op)
				if uo.X == glob {
					if lk.CommaOk {
						// find the Extract instruction
						// to return the element value
						for _, ref := range *lk.Referrers() {
							ex, ok := ref.(*ssa.Extract)
							if ok && ex.Index == 0 {
								return ex
							}
						}
					} else {
						return lk
					}
				}
			}
		}
	}
	panic("no such thing")
}

func pdump(prog *ssa.Program, val ssa.Value, pp *pointer.Pointer) {
	for _, l := range pp.PointsTo().Labels() {
		fmt.Printf("  %T %v %s: %s\n", l.Value(), l.Value(),
		    prog.Fset.Position(l.Pos()), l)
	}
}

type stores_t struct {
	addrs	[]*pointer.Pointer
	vals	[]*pointer.Pointer
}

type qs_t struct {
	conf	*pointer.Config
	res	*pointer.Result
	pkg	*ssa.Package
	ins	map[ssa.Instruction]*stores_t
	numq	int
}

func (q *qs_t) qinit(pkg *ssa.Package) {
	q.pkg = pkg
	q.conf = &pointer.Config{
		Mains:          []*ssa.Package{pkg},
		BuildCallGraph: false,
	}
	q.ins = make(map[ssa.Instruction]*stores_t)
}

func (q *qs_t) addop(in ssa.Instruction) {
	var addrs []*pointer.Pointer
	var vals []*pointer.Pointer

	_add := func(v ssa.Value) *pointer.Pointer {
		wtfp, err := q.conf.AddExtendedQuery(v, "x")
		if err != nil {
			panic(err)
		}
		q.numq++
		return wtfp
	}
	adda := func(v ssa.Value) {
		addrs = append(addrs, _add(v))
	}
	addv := func(v ssa.Value) {
		vals = append(vals, _add(v))
	}

	switch rin := in.(type) {
	case *ssa.MapUpdate:
		adda(rin.Map)
		if pointer.CanPoint(rin.Key.Type()) {
			addv(rin.Key)
		}
		if pointer.CanPoint(rin.Value.Type()) {
			addv(rin.Value)
		}
	case *ssa.Store:
		adda(rin.Addr)
		addv(rin.Val)
	}
	st := &stores_t{addrs: addrs, vals: vals}
	if _, ok := q.ins[in]; ok {
		panic("oh noes")
	}
	q.ins[in] = st
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
			lv = append(la, pp.PointsTo().Labels()...)
		}
		fun(in, la, lv)
	}
}

var reachable = map[ssa.Value]bool{}

func reachallocs(sf *ssa.Function) {
	var roots []ssa.Value
	allfuncs := make([]*ssa.Function, 0)
	for _, mem := range sf.Pkg.Members {
		switch rt:= mem.(type) {
		case *ssa.Function:
			allfuncs = append(allfuncs, rt)
		case *ssa.Type:
			if nam, ok := rt.Type().(*types.Named); ok {
				for i := 0; i < nam.NumMethods(); i++ {
					meth := nam.Method(i)
					safunc := sf.Prog.FuncValue(meth)
					allfuncs = append(allfuncs, safunc)
				}
			}
		case *ssa.Global:
			roots = append(roots, rt)
		}
	}
	//boundfunc, ok := sf.Pkg.Members["BOUND"]
	//if !ok {
	//	panic("none globerton")
	//}

	//_glob, ok := sf.Pkg.Members["allprocs"]
	//_glob, ok := sf.Pkg.Members["flea"]
	//if !ok {
	//	panic("none globerton")
	//}
	//glob := _glob.(*ssa.Global)
	//fmt.Printf("%v %T %T\n", glob, glob, glob.Type().(*types.Pointer).Elem())
	//roots = []ssa.Value{glob}

	var stores qs_t
	stores.qinit(sf.Pkg)
	for _, fun := range allfuncs {
		{
			flop := funcloops(fun)
		}
		for _, blk := range fun.Blocks {
			for _, in := range blk.Instrs {
				//ops := in.Operands(nil)
				//for _, oo := range ops {
				//	if o, ok := (*oo).(*ssa.Global); ok && o == glob {
				//		fmt.Printf("FOUND %T %v\n", in, in)
				//	}
				//}
				switch rin := in.(type) {
				//case *ssa.Send:
				//	ue := rin.Chan.Type().(*types.Chan).Elem()
				//	if pointer.CanPoint(ue) {
				//		sl := sf.Prog.Fset.Position(rin.Pos())
				//		fmt.Printf("  SEND POINTER %v\n", sl)
				//	}
				//case *ssa.Call:
				//	dur := rin.Common().StaticCallee()
				//	if dur == boundfunc {
				//		fmt.Printf("BOUND %v\n", dur)
				//	}
				case *ssa.MapUpdate:
					if pointer.CanPoint(rin.Key.Type()) ||
					   pointer.CanPoint(rin.Value.Type()) {
						stores.addop(rin)
					}
				case *ssa.Store:
					ue := rin.Addr.Type().(*types.Pointer).Elem()
					if pointer.CanPoint(ue) {
						if !pointer.CanPoint(rin.Val.Type()) {
							panic("wtf")
						}
						stores.addop(rin)
					}
				}
			}
		}
	}

	stores.analyze()

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
					addval := false
					switch alloc := val.(type) {
					case *ssa.MakeChan:
						addval = true
					case *ssa.MakeSlice:
						addval = true
					case *ssa.Alloc:
						if alloc.Heap {
							addval = true
						}
					case *ssa.MakeMap:
						addval = true
					}
					if addval && !didvals[val] {
						didids[val] = didid
						didid++
						didvals[val] = true
						newroots = append(newroots, val)
						//sl := sf.Prog.Fset.Position(in.Pos())
						//fmt.Printf("   ROOT %v <- %v %v %T\n", didids[r], didids[val], sl, val)
					}
				}
			})
		}
		roots = newroots
	}
	fmt.Printf("dyune!\n")

	for val := range didvals {
		reachable[val] = true
	}
}

// types to save maximum loop bounds and slice sizes.
type nument_t struct {
	file	string
	sline	int
	eline	int
	bound	int
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

var BROOT = "/home/ccutler/biscuit/biscuit/"
var FS = BROOT + "fs.go"
var MAIN = BROOT + "main.go"
var SYSC = BROOT + "syscall.go"
var NET = BROOT + "net.go"

// INF is just a large constant for unbounded loops. the allocations for such
// loops need special handling, like evicting as much memory as they allocate
// when memory is tight
var INF = 10

var loopdb = numdb_t{
	ents: []nument_t{
		{FS, 2862, 2874, INF},	// unlink free blocks XXX
		{FS, 2126, 2140, 64},	// fill indirect with zero blocks
		{FS, 2180, 2190, INF},	// fill file with zero blocks XXX
		{FS, 2348, 2363, 8},	// flush dirty part of page
		{FS, 1369, 1382, 8},	// flush dirty part of page (again?)
		{FS, 3648, 3663, INF},	// iterate all inodes for evict
		{FS, 1285, 1294, INF},	// attempt pg cache evict
		{FS, 2310, 2337, 8},	// fill page from disk
		{FS, 2828, 2836, 1},	// get phys pages for file (no loop)
		{FS, 795, 805, INF},	// read raw dev
		{FS, 814, 827, INF},	// write raw dev
		{FS, 2216, 2226, INF},	// loop over file pages for read
		{FS, 2239, 2251, INF},	// loop over file pages for write
		{FS, 2271, 2286, INF},	// zero fill empty file blocks XXX
		{FS, 2448, 2466, INF},	// callback over file's pages
		{FS, 1567, 1575, INF},	// evict idaemon XXX
		{FS, 1187, 1204, INF},	// namei path
		{FS, 2403, 2406, 23},	// add free des for new dir page
		{FS, 1015, 1043, INF},	// O_CREAT deadlock avoidance XXX
		{FS, 1828, 1857, INF},	// loop inodes for getcwd
		{FS, 416, 433, INF},	// loop inodes for for ancestor check
		{FS, 228, 249, INF},	// unlink deadlock avoidance
		{MAIN, 2203, 2209, INF},// fds buffered in passfd XXX
		{MAIN, 1326, 1331, 512},// close all fds on exit
		{MAIN, 1799, 1819, INF},// translate/copy to/from usermem
		{MAIN, 1875, 1895, INF},// iovec translate/copy to/from usermem
		{MAIN, 1497, 1513, 2},  // pages for user string
		{MAIN, 1603, 1615, INF},// trans/copy to user mem
		{MAIN, 1630, 1638, INF},// trans/copy from user mem
		{MAIN, 1452, 1463, 2},  // user readn (8 bytes max)
		{MAIN, 1845, 1856, 10}, // user iovs
		{MAIN, 1474, 1482, 2},  // user writen (8 bytes max)
		//{MAIN, 701, 712, INF},  // copy parent's fds
		{MAIN, 700, 712, INF},  // copy parent's fds
		//{MAIN, 1567, 1586, 64}, // copy user exec args to kernel
		{MAIN, 1566, 1586, 64}, // copy user exec args to kernel
		{MAIN, 1136, 1182, INF},// user syscall/exception loop
		{NET, 3377, 3390, INF}, // read wait for tcp data
		{NET, 3412, 3430, INF}, // write wait for tcp buffer
		{NET, 280, 283, INF},   // arp evict race detection
		{NET, 228, 231, INF},   // arp evict race detection again
		{NET, 222, 226, INF},   // arp evict race detection again
		{NET, 258, 258, INF},   // arp evict race detection again
		{NET, 249, 249, INF},   // arp evict race detection again
		{SYSC, 1382, 1388, INF},// write wait for pipe buffer
		{SYSC, 1311, 1313, INF},// fd add wait for buffer
		{SYSC, 893, 937, 512},  // maximum poll fds
		{SYSC, 969, 993, INF},  // re-check fds? XXX
		{SYSC, 2986, 2988, 64},	// unix incoming sleep conds
		{SYSC, 3369, 3373, 512},// close child fds on fork fail
		{SYSC, 3752, 3761, 512},// close CLOEXEC fds
		{SYSC, 5027, 5044, INF},// copy TLS data XXX
		//{SYSC, 4973, 4986, INF},// all ELF headers
		{SYSC, 4969, 4986, INF},// all ELF headers
		{SYSC, 3814, 3825, 64},// copy user exec args to user
	},
}

var slicedb = numdb_t{
	ents: []nument_t{
		{FS, 1298, 1298, 8},		// dirty blocks slice
		{MAIN, 785, 785, 512},		// fd table expansion
		{MAIN, 699, 699, 512},		// copy fd table for fork
		{MAIN, 3084, 3084, 8},		// enable CPU PMCs
		{SYSC, 2409, 2409, 51},		// datagram sender addresses
		{NET, 1467, 1467, 512},		// listen tcp backlog
		{NET, 3883, 3883, 512},		// also listen tcp backlog
		{SYSC, 2985, 2985, 64},		// unix domain backlog
	},
}

func loopbound(posi token.Position) int {
	return 10
	if num, ok := loopdb.lookup(posi); ok {
		return num
	}
	return readnum(fmt.Sprintf("LOOP BOUND at %v: ", posi))
}
func slicebound(v ssa.Instruction) int {
	return 10
	posi := v.Parent().Prog.Fset.Position(v.Pos())
	if num, ok := slicedb.lookup(posi); ok {
		return num
	}
	return readnum(fmt.Sprintf("MAX SLICE LENGTH at %v: ", posi))
}

// a type for all natural loops in a single function
type funcloops_t struct {
	loops	[]*natl_t
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
	head	*ssa.BasicBlock
	lblock	map[*ssa.BasicBlock]bool
	nests	[]*natl_t
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
	fmt.Printf("\n")
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

func (nt *natl_t) iter(fun func(*natl_t)) {
	fun(nt)
	for _, nat := range nt.nests {
		fun(nat)
	}
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
		nats = nats[:len(nats) - 1]
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
