package main

import (
 	"fmt"
	"go/ast"
 	"go/parser"
 	"go/token"
)

var gostmt []string
var deferstmt []string

func dir(name string) {
	fset := token.NewFileSet() // positions are relative to fset

	asts, err := parser.ParseDir(fset, name, nil, 0)
	if err != nil {
		fmt.Println(err)
		return
	}

	for _, pkg := range asts {
		for _, f  := range pkg.Files {
			ast.Inspect(f, func(node ast.Node) bool {
				var s string
				var s1 string
				switch x:= node.(type) {
				case *ast.Ident:
					// s = x.Name
					if x.Obj != nil {
						s1 = x.Obj.Name
					}
				case *ast.MapType:
					// s = "MAP"
				case *ast.GoStmt:
					gostmt = append(gostmt, fset.Position(node.Pos()).String())
				case *ast.DeferStmt:
					deferstmt = append(deferstmt, fset.Position(node.Pos()).String())
				}
				if s != "" {
					fmt.Printf("%s:\t%s (%s)\n", fset.Position(node.Pos()), s, s1)
				}
				return true
			})
		}
	}
}

func main() {
	dir("../src/fs")
	dir("../src/common")
	dir("../src/kernel")
	dir("../src/ufs")
	fmt.Printf("defer stmts: %d %v\n", len(deferstmt), deferstmt)
	fmt.Printf("go stmts: %d %v\n", len(gostmt), gostmt)
}
