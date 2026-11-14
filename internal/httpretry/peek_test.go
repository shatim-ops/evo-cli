// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// A layer of Transport that reads a body before its caller does and then
// gives it on reads it through peek, the one place that keeps the error
// the read ended with (#1746, #1769). io.ReadAll anywhere else in the
// package is a second place to lose it. Draining a body that is replaced
// or sent again (io.Copy to io.Discard) gives nothing on and is not a read
// of this kind.
func TestPeek_TheOnlyReadOfABodyGivenOn(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || (fn.Recv == nil && fn.Name.Name == "peek") {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "ReadAll" {
					if x, ok := sel.X.(*ast.Ident); ok && x.Name == "io" {
						t.Errorf("%s: %s reads a body with io.ReadAll; read it through peek", fset.Position(call.Pos()), fn.Name.Name)
					}
				}
				return true
			})
		}
	}
}
