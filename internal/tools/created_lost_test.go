// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// lostCreate panics for a tool whose createdIDs declare no id field or no
// list, and only the fault matrix reached every caller (Qwen review of
// #306). Every call lostCreate(ctx, "<tool>", …) in production code names
// its tool by a literal, and that tool declares both.
func TestCreated_LostCreateCallersDeclareListAndField(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if fn, ok := call.Fun.(*ast.Ident); !ok || fn.Name != "lostCreate" || len(call.Args) < 2 {
				return true
			}
			calls++
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: lostCreate names its tool by no literal: the declaration cannot be checked", fset.Position(call.Pos()))
				return true
			}
			tool, _ := strconv.Unquote(lit.Value)
			c, ok := createdIDs[tool]
			switch {
			case !ok:
				t.Errorf("%s: lostCreate for %s, which is not in createdIDs", fset.Position(call.Pos()), tool)
			case c.lostField() == "" || c.list == "" || c.field == "":
				t.Errorf("%s: lostCreate for %s, whose createdIDs declare no id field (%q), list (%q) or field (%q)",
					fset.Position(call.Pos()), tool, c.lostField(), c.list, c.field)
			}
			return true
		})
	}
	if calls == 0 {
		t.Fatal("no call of lostCreate found: the rule checks nothing")
	}
}
