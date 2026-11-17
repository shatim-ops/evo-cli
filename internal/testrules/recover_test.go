// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// recoversWithoutRepanic returns the lines of a file where recover() is
// called in a function that does not name runtime.Error: such a function
// survives a runtime fault and the test goes on.
func recoversWithoutRepanic(fset *token.FileSet, f *ast.File) []int {
	var lines []int
	var walk func(n ast.Node, body *ast.BlockStmt)
	walk = func(n ast.Node, body *ast.BlockStmt) {
		ast.Inspect(n, func(c ast.Node) bool {
			switch x := c.(type) {
			case *ast.FuncLit:
				if x.Body != body {
					walk(x.Body, x.Body)
					return false
				}
			case *ast.FuncDecl:
				if x.Body != nil && x.Body != body {
					walk(x.Body, x.Body)
				}
				return false
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "recover" && len(x.Args) == 0 && !namesRuntimeError(body) {
					lines = append(lines, fset.Position(x.Pos()).Line)
				}
			}
			return true
		})
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
			walk(fd.Body, fd.Body)
		}
	}
	return lines
}

// namesRuntimeError reports whether the function body refers to
// runtime.Error — the check that re-panics a runtime fault.
func namesRuntimeError(body *ast.BlockStmt) bool {
	if body == nil {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if s, ok := n.(*ast.SelectorExpr); ok {
			if id, ok := s.X.(*ast.Ident); ok && id.Name == "runtime" && s.Sel.Name == "Error" {
				found = true
			}
		}
		return !found
	})
	return found
}

// Go 1.26 on windows/amd64 corrupts the heap when a test recovers a
// hardware fault (a nil dereference) and goes on (golang/go#81238; the
// weekly race job and the Windows CI crashed with "fatal error: found bad
// pointer in Go heap", owner's decision on #2071, variant A). A test does
// not survive a runtime fault: a function of a test that calls recover()
// re-panics a runtime.Error, and a world that must not get far gives a
// stub that answers an error instead of a nil to dereference.
func TestRecoverInTestsRepanicsARuntimeFault(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, line := range recoversWithoutRepanic(fset, f) {
			t.Errorf("%s:%d: recover() in a test without re-panic of runtime.Error", filepath.ToSlash(rel), line)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The check finds a recover that swallows a fault and passes one that
// re-panics it, in a deferred literal and in a named function.
func TestRecoverCheck_TellsSwallowFromRepanic(t *testing.T) {
	src := `package p
import "runtime"
func swallow() { defer func() { _ = recover() }() }
func repanic() {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(runtime.Error); ok { panic(p) }
		}
	}()
}
func outer() {
	_ = runtime.Error(nil)
	defer func() { recover() }()
}
func named() { recover() }
`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	got := recoversWithoutRepanic(fset, f)
	if want := []int{3, 13, 15}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("lines = %v, want %v", got, want)
	}
}
