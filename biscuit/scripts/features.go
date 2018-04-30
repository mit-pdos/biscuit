package main

import (
 	"fmt"
	"go/ast"
 	"go/parser"
 	"go/token"
)

type info_t struct {
	name string
	pos string
}

var gostmt []string
var deferstmt []string
var appendstmt []string
var closures []string
var interfaces []string
var typeasserts []string
var maps []info_t
var slices []info_t
var channels []info_t
var strings []info_t
var nmaptypes int
var imports map[string][]string

func dotype(node ast.Expr, name string, pos string) {
	switch x := node.(type) {
	case *ast.MapType:
		i := info_t{name, pos}
		maps = append(maps, i)
	case *ast.ArrayType:
		i := info_t{name, pos}
		slices = append(slices, i)
	case *ast.ChanType:
		i := info_t{name, pos}
		channels = append(channels, i)
	case *ast.Ident:
		if x.Name == "string" {
			i := info_t{name, pos}
			strings = append(strings, i)
		}
	}
}

func doname(names []*ast.Ident) string {
	if len(names) > 0 {
		return names[0].String()
	} else {
		return ""
	}
}

func is_slice_expr(exprs []ast.Expr) bool {
	if len(exprs) == 0 {
		return false
	}
	return true
}

func is_append_call(exprs []ast.Expr) bool {
	if len(exprs) == 0 {
		return false
	}
	switch x := exprs[0].(type) {
	case *ast.CallExpr:
		switch y := x.Fun.(type) {
		case *ast.Ident:
			if y.Name == "append" {
				return true
			}
		}
	}
	return false
}

func donode(node ast.Node, fset *token.FileSet) bool {
	switch x := node.(type) {
	// case *ast.Ident:
	case *ast.Field:
		pos := fset.Position(node.Pos()).String()
		dotype(x.Type, doname(x.Names), pos)
	case *ast.MapType:
		// pos := fset.Position(node.Pos()).String()
		nmaptypes++
	case *ast.GenDecl:
		pos := fset.Position(node.Pos()).String()
		for _, spec := range x.Specs {
			switch y := spec.(type) {
			case *ast.ValueSpec:
				name := doname(y.Names)
				for _, val := range y.Values {
					switch z := val.(type) {
					case *ast.CompositeLit:
						dotype(z.Type, name, pos)
					}
				}
			}
		}
	case *ast.GoStmt:
		gostmt = append(gostmt, fset.Position(node.Pos()).String())
	case *ast.DeferStmt:
		deferstmt = append(deferstmt, fset.Position(node.Pos()).String())
	case *ast.AssignStmt:
		pos := fset.Position(node.Pos()).String()
		if is_slice_expr(x.Lhs) {
			if is_append_call(x.Rhs) {
				appendstmt = append(appendstmt, pos)
			}
		}
	case *ast.FuncLit:
		pos := fset.Position(node.Pos()).String()
		closures = append(closures, pos)
		// ast.Print(fset, x)
	case *ast.InterfaceType:
		pos := fset.Position(node.Pos()).String()
		interfaces = append(interfaces, pos)
	case *ast.TypeAssertExpr:
		pos := fset.Position(node.Pos()).String()
		typeasserts = append(typeasserts, pos)
	}
	return true
}

func addimport(f string, imp string) {
	s, ok := imports[imp]
	if ok {
		imports[imp] = append(s, f)
	} else {
		imports[imp] = []string{f}
	}
}
	
func dodir(name string) {
	fset := token.NewFileSet()
	asts, err := parser.ParseDir(fset, name, nil, 0)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, pkg := range asts {
		for _, f  := range pkg.Files {
			for _, s := range f.Imports {
				addimport(fset.Position(f.Package).String(), s.Path.Value)
			}
			ast.Inspect(f, func (node ast.Node) bool {
				return donode(node, fset)
			})
		}
	}
}

func printi(n string, x []info_t) {
	fmt.Printf("%s: %d:\n", n, len(x))
	for _, i := range x {
		fmt.Printf("\t%s (%s)\n", i.name, i.pos)
	}
}

func print(n string, x []string) {
	fmt.Printf("%s: %d:\n", n, len(x))
	for _, i := range x {
		fmt.Printf("\t%s\n", i)
	}
}

func printm(n string, m map[string][]string) {
	fmt.Printf("%s: %d:\n", n, len(m))
	for k, v := range(m) {
		fmt.Printf("\t%s (%d): %v\n", k, len(v), v)
	}
}

func main() {
	imports = make(map[string][]string)
	dodir("../src/fs")
	dodir("../src/common")
	dodir("../src/kernel")
	dodir("../src/ufs")
	printi("maps", maps)
	printi("arrays", slices)
	printi("channels", channels)
	printi("strings", strings)
	print("defer stmts", deferstmt)
	print("go stmts", gostmt)
	print("closures", closures)
	print("interfaces", interfaces)
	print("type asserts", typeasserts)
	printm("imports", imports)
}
