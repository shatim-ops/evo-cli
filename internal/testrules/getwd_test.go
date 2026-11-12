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

// A tool does not ask the process where it is (release plan 1.9, «Каталог
// записей намерений берётся из двух источников»): the directory a tool
// works in is the engine's StateDir, set in one place. A handler that
// reads os.Getwd is a second point of the same decision — today the two
// are one path, and the day they are not (a server started elsewhere, a
// test, a directory passed by flag) the tool reads and writes in two
// places. internal/tools does not call os.Getwd.

// getwdInTools are the functions of internal/tools that call os.Getwd
// today, "file: function" → what they take from it. They move to one
// function of the path from StateDir. Only shrinks. Closes by 1.9.
var getwdInTools = map[string]string{
	"internal/tools/intent.go: cutCalls":    "the directory of the intent records",
	"internal/tools/intent.go: settleHeld":  "the directory of the intent records",
	"internal/tools/intent.go: namedKept":   "the directory of the intent records",
	"internal/tools/intent.go: withPending": "the directory of the intent records",
}

func TestTools_DoNotAskTheProcessForItsDirectory(t *testing.T) {
	root := filepath.Join("..", "..")
	seen := map[string]bool{}
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
			key := rel + ": " + declName(decl)
			ast.Inspect(decl, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Getwd" {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "os" {
					return true
				}
				seen[key] = true
				if getwdInTools[key] == "" {
					t.Errorf("%s (%s): calls os.Getwd — the directory of a tool is the engine's StateDir, one point of decision (CLAUDE.md §2); take the path from it", key, fset.Position(sel.Pos()))
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var gone []string
	for key := range getwdInTools {
		if !seen[key] {
			gone = append(gone, key)
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		t.Errorf("getwdInTools names %s, which calls os.Getwd no longer: remove the line (the list only shrinks)", key)
	}
}
