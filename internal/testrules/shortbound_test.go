// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// minTestBound is the shortest bound a test may give the code under test
// from now: the steps before the point the test holds take milliseconds,
// and on a loaded runner under -race a few tens of them (#789).
const minTestBound = 500 * time.Millisecond

// durationUnits are the units of package time a test writes a duration in.
var durationUnits = map[string]time.Duration{
	"Nanosecond": time.Nanosecond, "Microsecond": time.Microsecond, "Millisecond": time.Millisecond,
	"Second": time.Second, "Minute": time.Minute, "Hour": time.Hour,
}

// A bound a test sets from the wall clock — time.Now().Add(d), as the Until
// of a deploy or a deadline — is a guess about the runner when it is short:
// the code under test must reach the point the test holds before it, and a
// loaded runner under -race may not (#789: a bound of 50 ms came in vm or
// docker, not where the test held, on a PR that did not touch deploy). A
// positive d under minTestBound fails unless the line or the one above it
// says why with "wall-clock:" — the same rule as TestNoUnguardedWallClockBounds,
// for a bound the code reads instead of one the test compares. The budget
// of an MCP call a test gives a registry (SetCallBudget) is the same bound
// (#793: a world of 250 ms did not reach its stage on a slow Windows
// runner); a named constant passes when its declaration says why.
func TestTests_NoShortBoundFromNow(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		bad = append(bad, shortBoundsIn(t, root, path)...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b)
	}
}

// shortBoundsIn reads the test files of dir: the package's constants first
// (a bound may be a named one, testBound), then each time.Now().Add.
func shortBoundsIn(t *testing.T, root, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		f, err := parser.ParseFile(fset, p, nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		files, names = append(files, f), append(names, p)
	}
	consts := map[string]time.Duration{}
	declared := map[string]bool{} // constants whose declaration says why, wall-clock:
	for range 2 {                 // a constant may be made of one declared after it
		for _, f := range files {
			for _, decl := range f.Decls {
				gd, ok := decl.(*ast.GenDecl)
				if !ok || gd.Tok != token.CONST {
					continue
				}
				for _, spec := range gd.Specs {
					vs := spec.(*ast.ValueSpec)
					why := strings.Contains(gd.Doc.Text()+vs.Doc.Text()+vs.Comment.Text(), "wall-clock:")
					for i, n := range vs.Names {
						if i < len(vs.Values) {
							if v, ok := durationOf(vs.Values[i], consts); ok {
								consts[n.Name], declared[n.Name] = v, why
							}
						}
					}
				}
			}
		}
	}
	var bad []string
	for i, f := range files {
		src, err := os.ReadFile(names[i])
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(string(src), "\n")
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			what := ""
			switch {
			case isNowAdd(call):
				what = "from time.Now()"
			case isCallBudget(call):
				what = "as the budget of an MCP call (SetCallBudget)"
			default:
				return true
			}
			v, ok := durationOf(call.Args[0], consts)
			if !ok || v <= 0 || v >= minTestBound {
				return true
			}
			if id, isConst := call.Args[0].(*ast.Ident); isConst && declared[id.Name] {
				return true
			}
			line := fset.Position(call.Pos()).Line
			if strings.Contains(lines[line-1], "wall-clock:") || (line > 1 && strings.Contains(lines[line-2], "wall-clock:")) {
				return true
			}
			rel, _ := filepath.Rel(root, names[i])
			bad = append(bad, filepath.ToSlash(rel)+":"+strconv.Itoa(line)+": a bound of "+v.String()+" "+what+": under "+minTestBound.String()+
				" the code under test may not reach the point the test holds on a loaded runner (#789); give it minTestBound or more, or say why with \"wall-clock:\"")
			return true
		})
	}
	return bad
}

// isCallBudget reports whether call is r.SetCallBudget(…): the bound of
// every call of a registry, from the call's start.
func isCallBudget(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "SetCallBudget"
}

// isNowAdd reports whether call is time.Now().Add(…).
func isNowAdd(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Add" {
		return false
	}
	inner, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return false
	}
	isel, ok := inner.Fun.(*ast.SelectorExpr)
	if !ok || isel.Sel.Name != "Now" {
		return false
	}
	pkg, ok := isel.X.(*ast.Ident)
	return ok && pkg.Name == "time"
}

// durationOf evaluates a duration a test writes as a constant: a unit of
// time, a number times a unit, a sum or quotient of them, or a named
// constant of the package.
func durationOf(e ast.Expr, consts map[string]time.Duration) (time.Duration, bool) {
	switch x := e.(type) {
	case *ast.ParenExpr:
		return durationOf(x.X, consts)
	case *ast.Ident:
		v, ok := consts[x.Name]
		return v, ok
	case *ast.SelectorExpr:
		if pkg, ok := x.X.(*ast.Ident); ok && pkg.Name == "time" {
			v, ok := durationUnits[x.Sel.Name]
			return v, ok
		}
	case *ast.BasicLit:
		if x.Kind == token.INT {
			n, err := strconv.ParseInt(x.Value, 0, 64)
			return time.Duration(n), err == nil
		}
	case *ast.UnaryExpr:
		if x.Op == token.SUB {
			v, ok := durationOf(x.X, consts)
			return -v, ok
		}
	case *ast.BinaryExpr:
		a, okA := durationOf(x.X, consts)
		b, okB := durationOf(x.Y, consts)
		if !okA || !okB {
			return 0, false
		}
		switch x.Op {
		case token.MUL:
			return a * b, true
		case token.ADD:
			return a + b, true
		case token.SUB:
			return a - b, true
		case token.QUO:
			if b != 0 {
				return a / b, true
			}
		}
	}
	return 0, false
}
