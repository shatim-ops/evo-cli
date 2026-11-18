// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// gateFile is the one place that reads confirmed: the gate of the
// registry, planner → gate → execution (release plan 1.3, W4).
const gateFile = "internal/tools/gate.go"

// inlineGates are the handlers that still read confirmed themselves,
// "file: function" → the work item of the release plan that moves the
// handler onto the gate. The list only shrinks: a read not here fails the
// test, so does a line with no read, and inlineGatesLeft is lowered with
// each line removed (G13-3, class K1).
// Empty since 1.9: every handler gates on its planner, and a new read of
// confirmed outside the gate fails the test (release plan 1.9, block 0в).
var inlineGates = map[string]string{}

// inlineGatesLeft is len(inlineGates): a line added back fails the test
// even with the read it names.
const inlineGatesLeft = 0

// notGates read the key "confirmed" without deciding on consent.
var notGates = map[string]string{
	"internal/cli/tools.go: buildToolArgs":                 "refuses --arg confirmed: the CLI confirms with --yes or the prompt",
	"internal/cli/tools.go: takesConfirmed":                "the schema of the tool: does it take confirmed",
	"internal/cli/tools.go: schemaParams":                  "leaves confirmed out of the flags of a command: the CLI confirms with --yes",
	"internal/confirm/confirm.go: Ask":                     "the status word of the answer of --yes, not the key of the call",
	"internal/tools/argcheck.go: checkArgs":                "confirmed is no unknown argument of a read-only tool",
	"internal/tools/intent.go: beginIntent":                "leaves confirmed out of the arguments the record names",
	"internal/tools/intent.go: canonArgs":                  "leaves confirmed out of the print of a call",
	"internal/tools/tools_stack.go: stackSteps":            "leaves confirmed out of the rows of the steps of a stack",
	"internal/tools/descriptions_ru.go: argDescriptionsRU": "the Russian description of the argument",
	"internal/tools/journal.go: journaled":                 "records consent in the session journal (D46); decides nothing",
	"internal/tools/registry.go: takesConsent":             "the schema of the tool: tools.NeedsConsent",
}

// CLAUDE.md §4: a call bound to fail is refused before the question of
// consent. Where each handler reads confirmed itself, the checks it makes
// after that read are made after the question (G13-3): the gate is one
// function of the registry, and production code reads confirmed — an index
// "confirmed" or "confirmed" passed to a call — only in gateFile, the
// handlers of inlineGates and the reads of notGates.
func TestConfirmedReadOnlyInTheGate(t *testing.T) {
	switch n := len(inlineGates); {
	case n > inlineGatesLeft:
		t.Errorf("inlineGates grew to %d lines from %d: the list only shrinks — put the handler on the gate instead", n, inlineGatesLeft)
	case n < inlineGatesLeft:
		t.Errorf("inlineGates has %d lines: lower inlineGatesLeft from %d to %d", n, inlineGatesLeft, n)
	}
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
	for _, dir := range []string{"internal", "cmd"} {
		// The files of a package are read together: a constant of the key
		// is declared in one file and read in another.
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			entries, err := os.ReadDir(path)
			if err != nil {
				return err
			}
			fset := token.NewFileSet()
			var files []*ast.File
			rels := map[*ast.File]string{}
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
					continue
				}
				f, err := parser.ParseFile(fset, filepath.Join(path, name), nil, 0)
				if err != nil {
					return err
				}
				rel, _ := filepath.Rel(root, filepath.Join(path, name))
				rels[f] = filepath.ToSlash(rel)
				files = append(files, f)
			}
			if len(files) == 0 {
				return nil
			}
			info := typeInfo(fset, files)
			for _, f := range files {
				if rels[f] == gateFile {
					continue
				}
				for _, decl := range f.Decls {
					key := rels[f] + ": " + declName(decl)
					for _, pos := range confirmedReads(decl, info) {
						seen[key] = true
						if inlineGates[key] == "" && notGates[key] == "" {
							t.Errorf("%s (%s): reads confirmed outside the gate (%s); move the check onto the gate",
								key, fset.Position(pos), gateFile)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, list := range []map[string]string{inlineGates, notGates} {
		for key := range list {
			if !seen[key] {
				t.Errorf("%s no longer reads confirmed: remove its line (and lower inlineGatesLeft)", key)
			}
		}
	}
}

// confirmedReads are the reads of confirmed in decl: any expression whose
// value is the text "confirmed" — a literal, a constant or a constant of a
// constant, a sum of literals, a literal a variable then holds (Астра
// #2203: a[key] with const key = "confirmed" went past a check of
// literals) — but the key of a write: the left side of an assignment (a
// call of a tool sets it) and the key of a literal of a map. info gives
// the values of the constants (typeInfo).
func confirmedReads(decl ast.Decl, info *types.Info) []token.Pos {
	write := map[ast.Expr]bool{}
	ast.Inspect(decl, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			for _, l := range x.Lhs {
				if ix, ok := l.(*ast.IndexExpr); ok {
					write[ix.Index] = true
				}
			}
		case *ast.KeyValueExpr:
			write[x.Key] = true
		}
		return true
	})
	var out []token.Pos
	ast.Inspect(decl, func(n ast.Node) bool {
		e, ok := n.(ast.Expr)
		if !ok || !isConfirmed(e, info) {
			return true
		}
		if !write[e] {
			out = append(out, e.Pos())
		}
		return false // a sum of literals is one read
	})
	return out
}

// isConfirmed: e is the text "confirmed", by its value as a constant.
func isConfirmed(e ast.Expr, info *types.Info) bool {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		s, err := strconv.Unquote(lit.Value)
		return err == nil && s == "confirmed"
	}
	if tv, ok := info.Types[e]; ok && tv.Value != nil && tv.Value.Kind() == constant.String {
		return constant.StringVal(tv.Value) == "confirmed"
	}
	return false
}

// The rule sees a read however it is written, and not a write.
func TestConfirmedReadForms(t *testing.T) {
	for src, want := range map[string]int{
		`func f(a map[string]any) { c, _ := a["confirmed"].(bool); _ = c }`:    1,
		`func f(a map[string]any) bool { _, ok := a["confirmed"]; return ok }`: 1,
		`func f(a map[string]any) { g(a, "confirmed") }`:                       1,
		`func f(a map[string]any) { a["confirmed"] = true }`:                   0,
		`func f() map[string]any { return map[string]any{"confirmed": true} }`: 0,
		`func f(a map[string]any) { _ = a["name"] }`:                           0,
		// Астра #2203: the key by a constant, of a constant, as a sum.
		`func f(a map[string]any) (bool, bool) { const key = "confirmed"; v, ok := a[key].(bool); return !v, ok }`:                              2,
		`func f(a map[string]any) bool { const k1 = "confirmed"; const k2 = k1; _, ok := a[k2]; return ok }`:                                    3,
		`func f(a map[string]any) bool { _, ok := a["confir"+"med"]; return ok }`:                                                               1,
		`func f(a map[string]any) bool { k := "confirmed"; _, ok := a[k]; return ok }`:                                                          1,
		`func f(a map[string]any) bool { _, ok := a[string("confirmed")]; return ok }`:                                                          1,
		`func f(a map[string]any) bool { switch { case len(a) > 0: }; for k := range a { if k == "confirmed" { return true } }; return false }`: 1,
		// A write by a constant is a write.
		`func f(a map[string]any) { const key = "confirmed"; a[key] = true }`: 1,
		`func f(a map[string]any) { _ = a["confirmed_by"] }`:                  0,
	} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "x.go", "package x\nfunc g(map[string]any, string) {}\n"+src, 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(confirmedReads(f.Decls[1], typeInfo(fset, []*ast.File{f}))); got != want {
			t.Errorf("%s: %d reads, want %d", src, got, want)
		}
	}
	// A constant of another file of the package is seen where it is read.
	fset := token.NewFileSet()
	one, _ := parser.ParseFile(fset, "a.go", "package x\nconst key = \"confirmed\"\n", 0)
	two, _ := parser.ParseFile(fset, "b.go", "package x\nfunc f(a map[string]any) bool { _, ok := a[key]; return ok }\n", 0)
	if got := len(confirmedReads(two.Decls[0], typeInfo(fset, []*ast.File{one, two}))); got != 1 {
		t.Errorf("a key by a constant of another file: %d reads, want 1", got)
	}
}
