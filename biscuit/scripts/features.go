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
var maps []info_t
var slices []info_t
var nmaptypes int

func dotype(node ast.Expr, name string, pos string) {
	switch node.(type) {
	case *ast.MapType:
		i := info_t{name, pos}
		maps = append(maps, i)
	case *ast.ArrayType:
		i := info_t{name, pos}
		slices = append(slices, i)
	}
}

func doname(names []*ast.Ident) string {
	if len(names) > 0 {
		return names[0].String()
	} else {
		return ""
	}
}
	
func donode(node ast.Node, fset *token.FileSet) bool {
	switch x := node.(type) {
	// case *ast.Ident:
	case *ast.Field:
		pos := fset.Position(node.Pos()).String()
		// fmt.Printf("field: %s %s\n", pos, x.Names)
		dotype(x.Type, doname(x.Names), pos)
	case *ast.MapType:
		// pos := fset.Position(node.Pos()).String()
		nmaptypes++
	case *ast.GenDecl:
		pos := fset.Position(node.Pos()).String()
		for _, spec := range x.Specs {
			// ast.Print(fset, spec)
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
	}
	return true
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
			ast.Inspect(f, func (node ast.Node) bool {
				return donode(node, fset)
			})
		}
	}
}

func main() {
	dodir("../src/fs")
	dodir("../src/common")
	dodir("../src/kernel")
	dodir("../src/ufs")
	fmt.Printf("maps: %d (%d):\n", len(maps), nmaptypes)
	for _, i := range maps {
		fmt.Printf("\t%s (%s)\n", i.name, i.pos)
	}
	fmt.Printf("arrays: %d:\n", len(slices))
	for _, i := range slices {
		fmt.Printf("\t%s (%s)\n", i.name, i.pos)
	}
	fmt.Printf("defer stmts: %d %v\n", len(deferstmt), deferstmt)
	fmt.Printf("go stmts: %d %v\n", len(gostmt), gostmt)
}
