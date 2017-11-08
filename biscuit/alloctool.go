/*
 * TODO
 */
package main

import "fmt"
import "bufio"
import "strconv"
import _"time"
import "strings"
import "os"
import "sort"
import "go/token"
import "go/types"

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

func (c *calls_t) dump1(depth int) {
	for i := 0; i < depth; i++ {
		fmt.Printf("  ")
	}
	lop := ""
	if c.looper {
		lop = "=L"
	}
	fmt.Printf("%c %s [%v]%s\n", 'A' + depth, c.me, c.csum, lop)
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
	//for _, t := range []string{".memreclaim", "fmt.", ".mbempty", ".mbread", ".ibread"} {
	//for _, t := range []string{"fmt.", ".evict", "pgcache_t).release"} {
	for _, t := range []string{"fmt.", "mbempty", "mbread", "memreclaim"} {
	//for _, t := range []string{"fmt."} {
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

var memos = map[*callgraph.Node]*calls_t{}

func maxcall(tnode *callgraph.Node, target *ssa.CallCommon, cs *callstack_t) (*calls_t, bool) {
	found := false
	var max int
	var calls *calls_t
	for _, e := range tnode.Out {
		// XXX how to identify the particular call instruction from
		// ssa.CallInstruction provided by callgraph.Edge? is comparing
		// their ssa.CallCommons the correct way?
		tt := e.Site.Common()
		if tt != target {
			continue
		}
		if tedge := callgraph.PathSearch(e.Callee,
		    func(n *callgraph.Node) bool {
			return n.ID == tnode.ID
		    }); tedge != nil {
			fmt.Printf("LOOP AVOID %v %v\n", tnode.Func,
			    e.Callee.Func)
			for _, dur := range tedge {
				fmt.Printf("\t%v -> %v\n", dur.Caller.Func,
				    dur. Callee.Func)
			}
			continue
		}
		if ignore(tnode.Func, cs) {
			me := tnode.Func.String()
			fmt.Printf("**** SKIP %s\n", me)
			continue
		}
		//choopy := cs.calledfrom("(*main.pgcache_t).evict")
		choopy := false
		found = true
		var tc *calls_t
		if v, ok := memos[e.Callee]; ok && !choopy {
			tc = v
		} else {
			tc = funcsum(e.Callee, cs)
			if !choopy {
				memos[e.Callee] = tc
			}
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

func revblksrc(blk *ssa.BasicBlock) (string, bool) {
	v := make(map[*ssa.BasicBlock]bool)
	return revblksrc1(blk, v)
}

func revblksrc1(blk *ssa.BasicBlock, v map[*ssa.BasicBlock]bool) (string, bool) {
	if v[blk] {
		return "", false
	}
	v[blk] = true
	for i := len(blk.Instrs) - 1; i >= 0; i-- {
		pos := blk.Instrs[i].Pos()
		if pos != token.NoPos {
			ret := blk.Instrs[0].Parent().Prog.Fset.Position(pos).String()
			return ret, true
		}
	}
	for _, e := range blk.Preds {
		if ret, ok := revblksrc1(e, v); ok {
			return ret, true
		}
	}
	return "", false
}

var _exits = map[*ssa.BasicBlock]*enode_t{}

// find the maximum allocation cost that starts and ends at cur
func calcexit(node *callgraph.Node, cur *ssa.BasicBlock, cs *callstack_t) *enode_t {
	ret := &enode_t{}
	if blockpath(cur.Succs[0], cur) {
		ret.exitedge = cur.Succs[1]
		ret.loopedge = cur.Succs[0]
	} else if blockpath(cur.Succs[1], cur) {
		ret.exitedge = cur.Succs[0]
		ret.loopedge = cur.Succs[1]
	} else {
		panic("not exit node")
	}
	ret.loopcalls = _loopblocks(node, cur, cur, cs)
	ret.loopalloc = ret.loopcalls.csum
	iterinput := -1
	if ret.loopalloc > 0 {
		sline, ok := revblksrc(cur)
		if !ok {
			panic("no")
		}
again:
		fmt.Printf("WHAT IS BOUND at %v (%vk)?\n", sline, ret.loopalloc >> 10)
		dur := bufio.NewReader(os.Stdin)
		str, err := dur.ReadString('\n')
		if err != nil {
			goto again
		}
		iterinput, err = strconv.Atoi(str[:len(str) - 1])
		if err != nil {
			goto again
		}
	} else {
		iterinput = 0
	}
	ret.maxiter = iterinput
	fmt.Printf("USING %v\n", ret.maxiter)
	return ret
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
	amexit := false
	if len(cur.Succs) == 2 {
		a := blockpath(cur.Succs[0], cur) && !blockpath(cur.Succs[1], cur)
		b := !blockpath(cur.Succs[0], cur) && blockpath(cur.Succs[1], cur)
		amexit = a || b
	}
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
		fmt.Printf("%v NOPPER\n", node.Func)
		return newcalls("NOLOOP")
	}
	visit[blk] = true

	calls := newcalls(node.Func.String())
	var ret int
	for _, ip := range blk.Instrs {
		switch v := ip.(type) {
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
				//fmt.Printf("failed for: %v\n", v)
			}
		case *ssa.Alloc:
			if !v.Heap {
				continue
			}
			tp := v.Type().Underlying().(*types.Pointer)
			ut := tp.Elem().Underlying()
			truesz := array_align(ut)
			ret += truesz
		}
	}
	// find maximum allocating block included in a loop
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
	// only consider loops
	calls.loopend = maxc.loopend
	if !calls.loopend {
		calls.csum = 0
	}
	delete(visit, blk)
	return calls
}

func funcsum(node *callgraph.Node, cs *callstack_t) *calls_t {
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

	calls := newcalls(node.Func.String())
	var ret int
	for _, ip := range blk.Instrs {
		switch v := ip.(type) {
		//case *ssa.Store:
		//	//fmt.Printf("== STORE TO %T\n", v.Addr)
		//	sl := node.Func.Prog.Fset.Position(v.Pos())
		//	if alloc, ok := v.Addr.(*ssa.Alloc); ok {
		//		if !alloc.Heap {
		//			//fmt.Printf("HEAPY WRITE %v %v\n", alloc.Type().String(), sl)
		//			continue
		//		}
		//	}
		//	switch vp := v.Addr.Type().(type) {
		//	case *types.Pointer:
		//		var name string
		//		switch ue := vp.Elem().(type) {
		//		default:
		//			name = fmt.Sprintf("wtf %T", ue)
		//		// XXX all interface stores are from
		//		// fmt.Printf/Sprintf?
		//		case *types.Basic, *types.Interface:
		//			continue
		//		case *types.Slice:
		//			name = "SLICE"
		//		case *types.Chan:
		//			name = "CHAN"
		//		case *types.Pointer:
		//			name = "PTR"
		//		}
		//		fmt.Printf("== %s STORE %v %v\n", name, vp.Elem().String(), sl)
		//	}
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
		case *ssa.Alloc:
			if !v.Heap {
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
	var max int
	var maxc *calls_t
	for _, suc := range blk.Succs {
		var bonus int
		if amexit && enode.isexit(suc) {
			bonus = enode.cost()
		}
		tc := _sumblock(node, suc, visit, cs)
		tc.csum += bonus
		if tc.csum > max {
			max = tc.csum
			maxc = tc
		}
		if tc.looper {
			calls.looper = true
		}
	}
	if maxc != nil {
		for _, tc := range maxc.calls {
			calls.addchild(tc)
		}
		ret += maxc.csum
	}
	if amexit {
		calls.looper = true
	}
	calls.csum = ret
	delete(visit, blk)
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
	fmt.Printf("TOTAL ALLOCATIONS for %v: %v\n", root, calls.csum)
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
	_sysfunc, ok := mpkg.Members["syscall"]
	//_sysfunc, ok := mpkg.Members["sys_recvmsg"]
	//_sysfunc, ok := mpkg.Members["proc_new"]
	//_sysfunc, ok := mpkg.Members["sys_socket"]
	//_sysfunc, ok := mpkg.Members["main"]

	//T := mpkg.Type("imemnode_t").Type()
	//pT := types.NewPointer(T)
	////sysfunc := prog.LookupMethod(pT, mpkg.Pkg, "offsetblk")
	//sysfunc := prog.LookupMethod(pT, mpkg.Pkg, "_deinsert")
	sysfunc := _sysfunc.(*ssa.Function)
	if !ok {
		panic("no")
	}
	// Build SSA code for bodies of all functions in the whole program.
	prog.Build()

	cg := mkcallgraph(prog)

	allocio(cg, sysfunc)

	//var h halp_t
	//h.init(cg)
	//h.dumpcallees(sysfunc)
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

func pdump(prog *ssa.Program, pp *pointer.Pointer) {
	for _, l := range pp.PointsTo().Labels() {
		fmt.Printf("  %s: %s\n", prog.Fset.Position(l.Pos()), l)
	}
}

// XXX direct and indirect pointer queries on ssa.Global maps don't seem to
// work... surely there is a better way to find an ssa.Value to query the
// contents of a map given its ssa.Global?
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

func allocio(cg *callgraph.Graph, sf *ssa.Function) {
	pkg := sf.Pkg

	config := &pointer.Config{
		Mains:          []*ssa.Package{pkg},
		//Mains:          ssautil.MainPackages(sf.Prog.AllPackages()),
		BuildCallGraph: false,
	}

	//arg := pkg.Members["flea"].(*ssa.Function).Params[0]
	//config.AddQuery(arg)
	//arg = pkg.Members["flea"].(*ssa.Function).Params[1]
	//config.AddQuery(arg)

	allprocs := pkg.Members["allprocs"].(*ssa.Global)
	//config.AddQuery(allprocs)
	config.AddQuery(findmaplookup(cg, allprocs))
	//wtfp, err := config.AddExtendedQuery(allprocs, "(*x)[0]")
	//if err != nil {
	//	panic(err)
	//}

	result, err := pointer.Analyze(config)
	if err != nil {
		panic(err)
	}

	//fmt.Printf("wtf:\n")
	//pdump(sf.Prog, wtfp)
	fmt.Printf("points to (%v):\n", len(result.Queries))
	for _, q := range result.Queries {
		fmt.Printf("query: %v\n", q)
		pdump(sf.Prog, &q)
	}
}

func ptrstores(f *ssa.Function) []*ssa.Store {
	var ret []*ssa.Store
	for _, b := range f.Blocks {
		for _, is := range b.Instrs {
			st, ok := is.(*ssa.Store)
			if !ok {
				continue
			}
			sline := f.Prog.Fset.Position(is.Pos())
			selm := st.Addr.Type().(*types.Pointer).Elem()
			switch selm.(type) {
			case *types.Basic, *types.Slice:
				continue
			}
			if selm.String() == "main.pollmsg_t" {
				fmt.Printf("%v %v\n", selm.String(), sline)
				ret = append(ret, st)
			}
		}
	}
	return ret
}

func analysis(prog *ssa.Program) {
	pkg := ssautil.MainPackages(prog.AllPackages())[0]
	pt := pkg.Type("pollers_t").Type()
	ppt := types.NewPointer(pt)
	addpt := prog.LookupMethod(ppt, pkg.Pkg, "addpoller")
	pstores := ptrstores(addpt)

	// Configure the pointer analysis to build a call-graph.
	config := &pointer.Config{
		//Mains:          prog.AllPackages(),
		Mains:          ssautil.MainPackages(prog.AllPackages()),
		BuildCallGraph: false,
	}

	for _, st := range pstores {
		//config.AddIndirectQuery(st.Addr)
		config.AddQuery(st.Addr)
	}
	//config.AddQuery(addpt.Params[1])

	arg := prog.LookupMethod(ppt, pkg.Pkg, "addpoller").Params[1]
	wtfp, err := config.AddExtendedQuery(arg, "x.notif")
	if err != nil {
		panic(err)
	}

	result, err := pointer.Analyze(config)
	if err != nil {
		panic(err)
	}

	fmt.Printf("wtfp:\n")
	for _, l := range wtfp.PointsTo().Labels() {
		fmt.Printf("  %s: %s\n", prog.Fset.Position(l.Pos()), l)
	}

	fmt.Printf("points to (%v):\n", len(result.Queries))
	for _, q := range result.Queries {
		var labels []string
		fmt.Printf("query: %v\n", q)
		for _, l := range q.PointsTo().Labels() {
		    label := fmt.Sprintf("  %s: %s", prog.Fset.Position(l.Pos()), l)
		    labels = append(labels, label)
		}
		sort.Strings(labels)
		for _, label := range labels {
		    fmt.Println(label)
		}
	}
}

//c.CreateFromFilenames("runtime",
//    "/home/ccutler/biscuit/src/runtime/runtime.go",
//    "/home/ccutler/biscuit/src/runtime/runtime1.go",
//    "/home/ccutler/biscuit/src/runtime/runtime2.go",
//    "/home/ccutler/biscuit/src/runtime/type.go",
//    "/home/ccutler/biscuit/src/runtime/alg.go",
//    "/home/ccutler/biscuit/src/runtime/os_linux_generic.go",
//    "/home/ccutler/biscuit/src/runtime/mcache.go",
//    "/home/ccutler/biscuit/src/runtime/sizeclasses.go",
//    "/home/ccutler/biscuit/src/runtime/mheap.go",
//    "/home/ccutler/biscuit/src/runtime/malloc.go",
//    "/home/ccutler/biscuit/src/runtime/chan.go",
//    "/home/ccutler/biscuit/src/runtime/trace.go",
//    "/home/ccutler/biscuit/src/runtime/mgc.go",
//    "/home/ccutler/biscuit/src/runtime/mgcwork.go",
//    "/home/ccutler/biscuit/src/runtime/cgocall.go",
//    "/home/ccutler/biscuit/src/runtime/stack.go",
//    "/home/ccutler/biscuit/src/runtime/mgcsweepbuf.go",
//    "/home/ccutler/biscuit/src/runtime/mcentral.go",
//    "/home/ccutler/biscuit/src/runtime/mfixalloc.go",
//    "/home/ccutler/biscuit/src/runtime/mprof.go",
//    "/home/ccutler/biscuit/src/runtime/symtab.go",
//    "/home/ccutler/biscuit/src/runtime/plugin.go",
//    "/home/ccutler/biscuit/src/runtime/defs_linux_amd64.go",
//    "/home/ccutler/biscuit/src/runtime/signal_linux_amd64.go",
//    "/home/ccutler/biscuit/src/runtime/typekind.go",
//    "/home/ccutler/biscuit/src/runtime/stubs.go",
//    "/home/ccutler/biscuit/src/runtime/stubs2.go",
//    "/home/ccutler/biscuit/src/runtime/hashmap.go",
//    "/home/ccutler/biscuit/src/runtime/string.go",
//    "/home/ccutler/biscuit/src/runtime/print.go",
//    "/home/ccutler/biscuit/src/runtime/panic.go",
//    "/home/ccutler/biscuit/src/runtime/error.go",
//    "/home/ccutler/biscuit/src/runtime/mbitmap.go",
//    "/home/ccutler/biscuit/src/runtime/hash64.go",
//    "/home/ccutler/biscuit/src/runtime/lock_futex.go",
//    "/home/ccutler/biscuit/src/runtime/race.go",
//    "/home/ccutler/biscuit/src/runtime/slice.go",
//    "/home/ccutler/biscuit/src/runtime/extern.go",
//    "/home/ccutler/biscuit/src/runtime/mstats.go",
//    "/home/ccutler/biscuit/src/runtime/mgcsweep.go",
//    "/home/ccutler/biscuit/src/runtime/atomic_pointer.go",
//    "/home/ccutler/biscuit/src/runtime/env_posix.go",
//    "/home/ccutler/biscuit/src/runtime/mstkbar.go",
//    "/home/ccutler/biscuit/src/runtime/msan.go",
//    "/home/ccutler/biscuit/src/runtime/mem_linux.go",
//    "/home/ccutler/biscuit/src/runtime/mfinal.go",
//    "/home/ccutler/biscuit/src/runtime/mgcmark.go",
//    "/home/ccutler/biscuit/src/runtime/fastlog2.go",
//    "/home/ccutler/biscuit/src/runtime/proc.go",
//    "/home/ccutler/biscuit/src/runtime/mbarrier.go",
//    "/home/ccutler/biscuit/src/runtime/sema.go",
//    "/home/ccutler/biscuit/src/runtime/time.go",
//    "/home/ccutler/biscuit/src/runtime/traceback.go",
//    "/home/ccutler/biscuit/src/runtime/lfstack.go",
//    "/home/ccutler/biscuit/src/runtime/cgo.go",
//    "/home/ccutler/biscuit/src/runtime/sys_x86.go",
//    "/home/ccutler/biscuit/src/runtime/iface.go",
//    "/home/ccutler/biscuit/src/runtime/utf8.go",
//    "/home/ccutler/biscuit/src/runtime/msize.go",
//    "/home/ccutler/biscuit/src/runtime/write_err.go",
//    "/home/ccutler/biscuit/src/runtime/signal_unix.go",
//    "/home/ccutler/biscuit/src/runtime/signal_amd64x.go",
//    "/home/ccutler/biscuit/src/runtime/unaligned1.go",
//    "/home/ccutler/biscuit/src/runtime/mmap.go",
//    "/home/ccutler/biscuit/src/runtime/fastlog2table.go",
//    "/home/ccutler/biscuit/src/runtime/netpoll_stub.go",
//    "/home/ccutler/biscuit/src/runtime/sys_nonppc64x.go",
//    "/home/ccutler/biscuit/src/runtime/cpuprof.go",
//    "/home/ccutler/biscuit/src/runtime/cgocheck.go",
//    "/home/ccutler/biscuit/src/runtime/lfstack_64bit.go",
//    "/home/ccutler/biscuit/src/runtime/sigtab_linux_generic.go",
//    "/home/ccutler/biscuit/src/runtime/signal_sighandler.go",
//    "/home/ccutler/biscuit/src/runtime/sigqueue.go",
//    "/home/ccutler/biscuit/src/runtime/vdso_linux_amd64.go",
//    "/home/ccutler/biscuit/src/runtime/sigaction_linux.go",
//    "/home/ccutler/biscuit/src/runtime/cputicks.go",
//    "/home/ccutler/biscuit/src/runtime/os_linux.go")
