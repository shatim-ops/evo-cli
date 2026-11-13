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
	"sort"
	"strings"
	"testing"
)

// A tool reads the records of intents where the layer writes them (release
// plan 1.9, «backup_restore читает записи слоя намерений мимо объявленного
// каталога», a finding of «Риск» 13.11): the layer writes into the
// directory declared for the registry (Registry.SetStateDir, #1578), and
// restoreHeld read them from the engine's StateDir. While the two are one
// path the guard against a second restore works; with two paths three
// calls with consent sent three restores — three billed disks (fixed in
// #1633). The equality of the two has its invariant (#1578); the path a
// tool reads by had none.
//
// In internal/tools the directory given to intent.Dir and intent.Dirs is
// a name of the function — its parameter, or what Registry.stateDir
// returned to it — never a field read on the spot (eng.StateDir). A
// function that hands its parameter on to them is a reader of the
// directory itself, and its callers are held to the same rule, and so on
// up (pendingIntents(eng.StateDir) in doctor went by a parameter, a
// finding of «Риск» at #1633).

// intentDirUndeclared are the functions of internal/tools that give a
// reader of the directory something else today, "file: function" → what
// and whose it is. The list only shrinks. Closes by 1.10.
var intentDirUndeclared = map[string]string{
	"internal/tools/tools_auth.go: doctorChecks": "eng.StateDir, twice — the records doctor reads and settles (pendingIntents) and the foreign files it names (intent.Dirs); no money: a warning read, and a record closed, in another directory",
}

// intentDirCall is a call that hands a directory to a reader of it.
type intentDirCall struct {
	key, callee, at string
	arg             ast.Expr
	declared        map[string]bool
}

func TestTools_ReadIntentRecordsInTheDeclaredDirectory(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	type fn struct {
		key  string
		decl *ast.FuncDecl
	}
	var fns []fn
	err := filepath.WalkDir(filepath.Join(root, "internal", "tools"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			if d, ok := decl.(*ast.FuncDecl); ok && d.Body != nil {
				fns = append(fns, fn{filepath.ToSlash(rel) + ": " + d.Name.Name, d})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// readers: the name of a function → the index of the argument that is
	// the directory. intent.Dir and intent.Dirs first; then, to a fixed
	// point, every function that hands a parameter of its own to one.
	readers := map[string]int{"intent.Dir": 0, "intent.Dirs": 0}
	var calls []intentDirCall
	for grew := true; grew; {
		grew = false
		calls = calls[:0]
		for _, f := range fns {
			declared := declaredDirs(f.decl)
			params := paramIndex(f.decl)
			ast.Inspect(f.decl.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := calleeName(call)
				at, ok := readers[callee]
				if !ok || at >= len(call.Args) {
					return true
				}
				arg := call.Args[at]
				calls = append(calls, intentDirCall{f.key, callee, fset.Position(call.Pos()).String(), arg, declared})
				if id, ok := arg.(*ast.Ident); ok {
					if i, isParam := params[id.Name]; isParam {
						if _, known := readers[f.decl.Name.Name]; !known {
							readers[f.decl.Name.Name] = i
							grew = true
						}
					}
				}
				return true
			})
		}
	}
	if len(calls) < 8 || len(readers) < 4 {
		t.Fatalf("%d calls of %d readers of the directory found in internal/tools: the scan sees too little", len(calls), len(readers))
	}
	seen := map[string]bool{}
	for _, c := range calls {
		if id, ok := c.arg.(*ast.Ident); ok && c.declared[id.Name] {
			continue
		}
		seen[c.key] = true
		if intentDirUndeclared[c.key] == "" {
			t.Errorf("%s (%s): %s is given a directory that is neither a parameter of the function nor what Registry.stateDir returned — the records are read where the layer writes them, the declared directory (one point of decision, CLAUDE.md §2)",
				c.key, c.at, c.callee)
		}
	}
	var gone []string
	for key := range intentDirUndeclared {
		if !seen[key] {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		t.Errorf("intentDirUndeclared names %s, which reads the declared directory now: remove the line (the list only shrinks)", key)
	}
	t.Logf("readers of the directory: %d; calls of them: %d", len(readers), len(calls))
}

// calleeName is the name a call is made by: «intent.Dir» for a function of
// package intent, the bare name for a function or a method of
// internal/tools.
func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		if pkg, ok := f.X.(*ast.Ident); ok && pkg.Name == "intent" {
			return "intent." + f.Sel.Name
		}
		return f.Sel.Name
	}
	return ""
}

// paramIndex is the position of each parameter of fn.
func paramIndex(fn *ast.FuncDecl) map[string]int {
	out := map[string]int{}
	i := 0
	for _, p := range fn.Type.Params.List {
		for _, name := range p.Names {
			out[name.Name] = i
			i++
		}
		if len(p.Names) == 0 {
			i++
		}
	}
	return out
}

// declaredDirs are the names of fn that hold a declared directory: its
// parameters, and what a call of stateDir was assigned to first.
func declaredDirs(fn *ast.FuncDecl) map[string]bool {
	out := map[string]bool{}
	for _, p := range fn.Type.Params.List {
		for _, name := range p.Names {
			out[name.Name] = true
		}
	}
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "stateDir" {
			if name, ok := as.Lhs[0].(*ast.Ident); ok {
				out[name.Name] = true
			}
		}
		return true
	})
	return out
}
