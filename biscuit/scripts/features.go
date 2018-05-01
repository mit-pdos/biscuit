package main

import (
	"bytes"
 	"fmt"
	"log"
	"go/ast"
 	"go/parser"
 	"go/token"
	"io"
	"path/filepath"
	"strings"
	"os"
)

type info_t struct {
	name string
	pos string
}

var allocs []string
var gostmt []string
var deferstmt []string
var appendstmt []string
var closures []string
var interfaces []string
var typeasserts []string
var multiret []string
var finalizers []string
var maps []info_t
var slices []info_t
var channels []info_t
var stringuse []info_t
var nmaptypes int
var imports map[string][]string
var lcount int

var verbose = false

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
			stringuse = append(stringuse, i)
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

func is_make_call(exprs []ast.Expr) bool {
	if len(exprs) == 0 {
		return false
	}
	switch x := exprs[0].(type) {
	case *ast.CallExpr:
		switch y := x.Fun.(type) {
		case *ast.Ident:
			if y.Name == "make" {
				return true
			}
		}
	}
	return false
}

func is_new_call(exprs []ast.Expr) bool {
	if len(exprs) == 0 {
		return false
	}
	switch x := exprs[0].(type) {
	case *ast.CallExpr:
		switch y := x.Fun.(type) {
		case *ast.Ident:
			if y.Name == "new" {
				return true
			}
		}
	}
	return false
}

func is_alloc_call(exprs []ast.Expr) bool {
	if len(exprs) == 0 {
		return false
	}
	switch x := exprs[0].(type) {
	case *ast.UnaryExpr:
		if x.Op == token.AND {
			switch x.X.(type) {
			case *ast.CompositeLit:
				return true
			}
		}
	}
	return false
}

func is_set_finalizer(c *ast.CallExpr) bool {
	switch x := c.Fun.(type) {
	case *ast.SelectorExpr:
		if x.Sel.Name == "SetFinalizer" {
			return true
		}
	}
	return false
}

func donode(node ast.Node, fset *token.FileSet) bool {
	// ast.Print(fset,node)
	switch x := node.(type) {
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
		if is_append_call(x.Rhs) {
			appendstmt = append(appendstmt, pos)
		}
		if is_make_call(x.Rhs) {
			allocs = append(allocs, pos)
		}
		if is_new_call(x.Rhs) {
			allocs = append(allocs, pos)
		}
		if is_alloc_call(x.Rhs) {
			// ast.Print(fset, x)
			// fmt.Printf("pos %s\n", pos)
			allocs = append(allocs, pos)
		}
		
	case *ast.FuncLit:
		pos := fset.Position(node.Pos()).String()
		closures = append(closures, pos)
	case *ast.InterfaceType:
		pos := fset.Position(node.Pos()).String()
		interfaces = append(interfaces, pos)
	case *ast.TypeAssertExpr:
		pos := fset.Position(node.Pos()).String()
		typeasserts = append(typeasserts, pos)
	case *ast.FuncDecl:
		pos := fset.Position(node.Pos()).String()
		// ast.Print(fset, x)
		t := x.Type
		if t.Results != nil && len(t.Results.List) > 1 {
			multiret = append(multiret, pos)
		}
	case *ast.ExprStmt:
		pos := fset.Position(node.Pos()).String()
		switch y := x.X.(type) {
	        case *ast.CallExpr:
			// ast.Print(fset, x)
			if is_set_finalizer(y) {
				finalizers = append(finalizers, pos)
			}
		}
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

func lineCounter(r io.Reader) (int, error) {
    buf := make([]byte, 32*1024)
    count := 0
    lineSep := []byte{'\n'}

    for {
        c, err := r.Read(buf)
        count += bytes.Count(buf[:c], lineSep)

        switch {
        case err == io.EOF:
            return count, nil

        case err != nil:
            return count, err
        }
    }
}

func dofile(path string) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, s := range f.Imports {
		addimport(fset.Position(f.Package).String(), s.Path.Value)
	}
	ast.Inspect(f, func (node ast.Node) bool {
		return donode(node, fset)
	})

	file, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	l, err := lineCounter(file)
	if err != nil {
		log.Fatal(err)
	}
	lcount += l
}

func frac(x int) float64 {
	return (float64(x)/float64(lcount))*1000
}

func printi(n string, x []info_t) {
	fmt.Printf("%s & %.2f \\\\ \n", n, frac(len(x)))
	if verbose {
		for _, i := range x {
			fmt.Printf("\t%s (%s)\n", i.name, i.pos)
		}
	}
}

func print(n string, x []string) {
	fmt.Printf("%s & %.2f \\\\ \n", n, frac(len(x)))
	if verbose {
		for _, i := range x {
			fmt.Printf("\t%s\n", i)
		}
	}
}

func printm(n string, m map[string][]string) {
	fmt.Printf("%s & %.2f \\\\ \n", n, frac(len(m)))
	if verbose {
		for k, v := range(m) {
			fmt.Printf("\t%s (%d): %v\n", k, len(v), v)
		}
	}
}


func main() {
	if len(os.Args) != 2 {
		fmt.Println("features.go <path>")
		return
	}
	imports = make(map[string][]string)
	dir := os.Args[1]
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(strings.TrimSpace(path)) == ".go"  {
			dofile(path)
		}
		return nil
	})
	if err != nil {
		fmt.Printf("error %v\n", err)
		
	}

	fmt.Printf("Line count %d\n", lcount)

	// printi("Maps", maps)
	// printi("Slices", slices)
	// printi("Channels", channels)
	// printi("Strings", stringuse)
	// print("Multi-value return", multiret)
	// print("Closures", closures)
	// print("Finalizers", finalizers)
	// print("Defer stmts", deferstmt)
	// print("Go stmts", gostmt)
	// print("Interfaces", interfaces)
	// print("Type asserts", typeasserts)
	// printm("Imports", imports)
	print("Allocs", allocs)

}
