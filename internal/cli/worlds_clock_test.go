// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every world of internal/cli runs on the stopped clock (stoppedClock): a
// world is a function of the tests that gives the tools an engine of its
// own (toolEngine = …). FB140-6 stopped the clock in withParityWorld, and
// withEmptyCloud, built apart, stayed on the moving one: the TTL of the
// token read 3599 in one answer and 3600 in the other, #490 red twice on
// Windows (whoami, then doctor; #492). The rule is the invariant, not a
// list of worlds (process backlog 1.6): a function that sets toolEngine
// stops the clock itself, or calls a function that does, or is called
// only by one that does.
func TestWorlds_RunOnTheStoppedClock(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	calls := map[string][]string{} // function → the functions it calls
	sets := map[string]token.Position{}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil || fd.Recv != nil {
				continue
			}
			name := fd.Name.Name
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					if id, ok := n.Fun.(*ast.Ident); ok {
						calls[name] = append(calls[name], id.Name)
					}
				case *ast.AssignStmt:
					for i, l := range n.Lhs {
						if id, ok := l.(*ast.Ident); !ok || id.Name != "toolEngine" || i >= len(n.Rhs) {
							continue
						}
						if installsWorld(n.Rhs[i]) {
							if _, seen := sets[name]; !seen {
								sets[name] = fset.Position(n.Pos())
							}
						}
					}
				}
				return true
			})
		}
	}
	if len(sets) == 0 {
		t.Fatal("no function sets toolEngine: the pattern moved, update the test")
	}
	callers := map[string][]string{}
	for f, cs := range calls {
		for _, c := range cs {
			callers[c] = append(callers[c], f)
		}
	}
	stops := map[string]bool{"stoppedClock": true}
	for changed := true; changed; {
		changed = false
		for f, cs := range calls {
			if stops[f] {
				continue
			}
			for _, c := range cs {
				if stops[c] {
					stops[f], changed = true, true
					break
				}
			}
		}
	}
	var bad []string
	for f, pos := range sets {
		if stops[f] || stoppedByCallers(f, callers, stops, map[string]bool{}) {
			continue
		}
		bad = append(bad, filepath.Base(pos.Filename)+":"+strconv.Itoa(pos.Line)+": "+f)
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("%s gives the tools a world of its own on the moving clock: call stoppedClock(t) in it (two answers that read the time differ by it, #492)", b)
	}
}

// installsWorld: the right side of toolEngine = … is a world's engine — a
// function literal or a named function — not the restore of a saved one
// (old, world, inner: a local variable).
func installsWorld(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.FuncLit:
		return true
	case *ast.Ident:
		return strings.HasSuffix(e.Name, "Engine") && e.Name != "toolEngine"
	}
	return false
}

// stoppedByCallers: every caller of f stops the clock, directly or through
// its own callers (a helper of the tests a world calls into).
func stoppedByCallers(f string, callers map[string][]string, stops, seen map[string]bool) bool {
	cs := callers[f]
	if len(cs) == 0 || seen[f] {
		return false
	}
	seen[f] = true
	for _, c := range cs {
		if !stops[c] && !stoppedByCallers(c, callers, stops, seen) {
			return false
		}
	}
	return true
}
