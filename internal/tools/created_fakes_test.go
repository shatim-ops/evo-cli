// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// foreignFakes are the tests whose fake of a create answers without the id
// field createdIDs declares, on purpose: "file: test" → why.
var foreignFakes = map[string]string{}

// The stack tests replaced the handlers of steps with fakes of their own
// shape (fakeOK(map{"id": …})), so the "ok" written where snat_create
// answers nat_gateway.id went unseen (G13-14, class K2). A test that
// replaces the handler of a create that answers an id (createdIDs) answers
// with the field declared there — or runs the real handler over a fake
// API — unless foreignFakes says why not.
func TestCreatedFakes_AnswerTheDeclaredID(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var bad []string
	for _, file := range files {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			key := file + ": " + fd.Name.Name
			ast.Inspect(fd, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok || len(as.Lhs) != 1 || len(as.Rhs) != 1 {
					return true
				}
				tool := replacedHandler(as.Lhs[0])
				if createdIDs[tool].id == "" {
					return true
				}
				if answersKey(as.Rhs[0], idKeys(tool)) {
					return true
				}
				seen[key] = true
				if foreignFakes[key] == "" {
					bad = append(bad, fmt.Sprintf("%s (%s): the fake of %s answers without %q (createdIDs)", key, fset.Position(as.Pos()), tool, createdIDs[tool].id))
				}
				return true
			})
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Error(b + " — answer the declared field, run the real handler over a fake API, or say why in foreignFakes")
	}
	for key := range foreignFakes {
		if !seen[key] {
			t.Errorf("foreignFakes names %s, which fakes no create without its id: remove the line", key)
		}
	}
}

// replacedHandler is the tool whose handler e names: x.handlers["tool"].
func replacedHandler(e ast.Expr) string {
	ix, ok := e.(*ast.IndexExpr)
	if !ok {
		return ""
	}
	sel, ok := ix.X.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "handlers" {
		return ""
	}
	lit, ok := ix.Index.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	s, _ := strconv.Unquote(lit.Value)
	return s
}

// idKeys are the top-level keys of the answer of tool that hold its id.
func idKeys(tool string) map[string]bool {
	out := map[string]bool{}
	c := createdIDs[tool]
	for _, p := range []string{c.id, c.lostField()} {
		k, _, _ := strings.Cut(p, ".")
		out[k] = true
	}
	return out
}

// answersKey reports whether every success answer of the fake e carries
// one of keys: the map of fakeOK, and in a function literal the map of each
// return whose second value is false (an error answer needs no id).
func answersKey(e ast.Expr, keys map[string]bool) bool {
	if call, ok := e.(*ast.CallExpr); ok {
		if fn, ok := call.Fun.(*ast.Ident); ok && fn.Name == "fakeOK" {
			return hasKey(call, keys)
		}
	}
	fl, ok := e.(*ast.FuncLit)
	if !ok {
		return true // another form: not a fake of an answer
	}
	all := true
	ast.Inspect(fl.Body, func(n ast.Node) bool {
		if _, nested := n.(*ast.FuncLit); nested {
			return false
		}
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 2 {
			return true
		}
		if isErr, ok := ret.Results[1].(*ast.Ident); ok && isErr.Name == "false" && !hasKey(ret.Results[0], keys) {
			all = false
		}
		return true
	})
	return all
}

// hasKey reports whether a map literal in e has one of keys.
func hasKey(e ast.Node, keys map[string]bool) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, el := range cl.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if lit, ok := kv.Key.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, _ := strconv.Unquote(lit.Value); keys[s] {
					found = true
				}
			}
		}
		return true
	})
	return found
}
