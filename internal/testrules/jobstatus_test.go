// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// jobStatusPackages are the packages that read stack.Job: the stack
// package and every package importing it.
var jobStatusPackages = []string{"stack", "tools"}

// A stack job's status is read through the predicates of stack.Job only:
// a method of Job that returns one bool is the one point that decides what
// a status means (DeletionsPending, Destroyed, TornDown). Comparing
// Job.Status with a constant anywhere else — == or != or a switch on it —
// fails. stack_status read the raw status: a job the cloud is still
// deleting answered without its next step, and a job saved by 1.8.0-rc.1
// as destroyed with deletions pending answered destroyed (Fable delta
// audit 1.8.0, FB180-15; D96).
func TestStackJobStatus_ReadThroughJobPredicates(t *testing.T) {
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	var bad []string
	for _, name := range jobStatusPackages {
		dir := filepath.Join("..", name)
		files := parsePackage(t, fset, dir)
		info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
		conf := types.Config{Importer: imp, Error: func(error) {}}
		if _, err := conf.Check("github.com/shatim-ops/evo-cli/internal/"+name, fset, files, info); err != nil {
			t.Fatalf("type-check internal/%s: %v", name, err)
		}
		for _, f := range files {
			for _, d := range f.Decls {
				fn, ok := d.(*ast.FuncDecl)
				if !ok || fn.Body == nil || isJobPredicate(name, fn, info) {
					continue
				}
				ast.Inspect(fn.Body, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.BinaryExpr:
						if n.Op != token.EQL && n.Op != token.NEQ {
							return true
						}
						if (isJobStatus(n.X, info) && isConst(n.Y, info)) || (isJobStatus(n.Y, info) && isConst(n.X, info)) {
							bad = append(bad, fset.Position(n.Pos()).String())
						}
					case *ast.SwitchStmt:
						if n.Tag != nil && isJobStatus(n.Tag, info) {
							bad = append(bad, fset.Position(n.Pos()).String())
						}
					}
					return true
				})
			}
		}
	}
	sort.Strings(bad)
	for _, p := range bad {
		t.Errorf("%s: stack.Job.Status compared with a constant; read it through a predicate of stack.Job (a method returning bool)", filepath.ToSlash(p))
	}
}

// parsePackage parses the non-test Go files of dir.
func parsePackage(t *testing.T, fset *token.FileSet, dir string) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	return files
}

// isJobPredicate is a method of stack.Job returning one bool.
func isJobPredicate(pkg string, fn *ast.FuncDecl, info *types.Info) bool {
	if pkg != "stack" || fn.Recv == nil || len(fn.Recv.List) != 1 {
		return false
	}
	if !isJob(info.Types[fn.Recv.List[0].Type].Type) {
		return false
	}
	res := fn.Type.Results
	if res == nil || len(res.List) != 1 || len(res.List[0].Names) > 1 {
		return false
	}
	b, ok := info.Types[res.List[0].Type].Type.(*types.Basic)
	return ok && b.Kind() == types.Bool
}

// isJobStatus is the selector x.Status with x a stack.Job or *stack.Job.
func isJobStatus(e ast.Expr, info *types.Info) bool {
	sel, ok := ast.Unparen(e).(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Status" {
		return false
	}
	return isJob(info.Types[sel.X].Type)
}

func isJob(t types.Type) bool {
	if t == nil {
		return false
	}
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Name() == "Job" && n.Obj().Pkg() != nil &&
		n.Obj().Pkg().Path() == "github.com/shatim-ops/evo-cli/internal/stack"
}

func isConst(e ast.Expr, info *types.Info) bool {
	return info.Types[e].Value != nil
}
