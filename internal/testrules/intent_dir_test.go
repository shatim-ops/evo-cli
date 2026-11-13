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
// returned to it — never a field read on the spot (eng.StateDir). The
// rule reads one function: a caller that hands the engine's directory to
// a parameter is not seen by it.

// intentDirUndeclared are the functions of internal/tools that give
// intent.Dir or intent.Dirs something else today, "file: function" → what
// and whose it is. The list only shrinks. Closes by 1.10.
var intentDirUndeclared = map[string]string{
	"internal/tools/tools_auth.go: doctorChecks": "eng.StateDir — the foreign files doctor names; no money, a warning read in another directory",
}

func TestTools_ReadIntentRecordsInTheDeclaredDirectory(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	calls := 0
	err := filepath.WalkDir(filepath.Join(root, "internal", "tools"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			key := rel + ": " + fn.Name.Name
			declared := declaredDirs(fn)
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 1 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || (sel.Sel.Name != "Dir" && sel.Sel.Name != "Dirs") {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "intent" {
					return true
				}
				calls++
				if arg, ok := call.Args[0].(*ast.Ident); ok && declared[arg.Name] {
					return true
				}
				seen[key] = true
				if intentDirUndeclared[key] == "" {
					t.Errorf("%s (%s): intent.%s is given a directory that is neither a parameter of the function nor what Registry.stateDir returned — the records are read where the layer writes them, the declared directory (one point of decision, CLAUDE.md §2)",
						key, fset.Position(call.Pos()), sel.Sel.Name)
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls < 5 {
		t.Fatalf("%d calls of intent.Dir and intent.Dirs found in internal/tools: the scan sees nothing", calls)
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
