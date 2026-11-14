// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The paths that write and clean up read lists by themselves: a stack step
// finds what it made by name (stepFinder, findIDByName, takenByName), an
// intent record finds its resource (intent.go), the completion and the
// reference of a filter read a catalogue (completion.go, listed). The
// filters of 1.8.2 narrow what the list answers; such a path under a filter
// would not see a namesake in another zone or state, and would create a
// duplicate or leave a billed resource unfound. These paths pass only the
// list's own arguments (as before 1.8.2) or nothing.

// internalListArgs are the arguments the in-process finds pass to a list
// beside the create's listArgs: intent.go names the project.
var internalListArgs = []string{"project_id"}

func TestListFilters_InternalFindsPassNoFilter(t *testing.T) {
	for create, c := range allCreated() {
		if c.list == "" {
			continue
		}
		filters := map[string]bool{}
		for _, f := range listSpecs[c.list].filters {
			filters[f.arg] = true
		}
		for _, a := range append(append([]string{}, c.listArgs...), internalListArgs...) {
			if filters[a] {
				t.Errorf("%s finds what it made in %s with %q, a filter of that list: a namesake outside the selection is not found; make it an own argument of the list (listSpecs.own) or drop it from listArgs", create, c.list, a)
			}
		}
	}
}

// TestListFilters_UncheckedCallsPassNoArguments: an in-process call of a
// handler through r.unchecked — under the check of the arguments, so under
// the filters too — passes an empty map. Such a call reads a whole list to
// resolve a name; an argument there would be a filter the caller did not
// mean.
func TestListFilters_UncheckedCallsPassNoArguments(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			idx, ok := call.Fun.(*ast.IndexExpr)
			if !ok {
				return true
			}
			sel, ok := idx.X.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "unchecked" {
				return true
			}
			seen++
			if len(call.Args) != 2 || !emptyMapLit(call.Args[1]) {
				t.Errorf("%s: a call through unchecked passes arguments; it reads a whole list, so pass map[string]any{}", fset.Position(call.Pos()))
			}
			return true
		})
	}
	// completion.go and listed (list_filters.go) call this way: none
	// found means the rule reads nothing.
	if seen < 2 {
		t.Fatalf("found %d calls through unchecked, want at least 2: the rule no longer sees them", seen)
	}
}

func emptyMapLit(e ast.Expr) bool {
	lit, ok := e.(*ast.CompositeLit)
	if !ok || len(lit.Elts) != 0 {
		return false
	}
	_, ok = lit.Type.(*ast.MapType)
	return ok
}
