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
	"strconv"
	"strings"
	"testing"
)

// A path of evo's own directories is not taken apart as a string with "/"
// in it (main was red on Windows for six hours on 12.11: a test cut
// "/.evo/intents" off a path filepath.Join had made, and on Windows the
// path has backslashes — #1473, fixed by #1535). A pull request does not
// run internal/tools on Windows, so only main saw it. The rule reads the
// code on every runner: a call of package strings with a literal that
// names the state directory together with a slash is red — in tests and
// in the product alike; filepath (Dir, Base, Rel, Join) takes a path
// apart on every system.

// stateDirWithSlash are the forms of the literal the rule finds.
var stateDirWithSlash = []string{".evo/", "/.evo", ".cloudru-vm/", "/.cloudru-vm"}

func TestPaths_StateDirIsNotTakenApartAsAString(t *testing.T) {
	root := filepath.Join("..", "..")
	files := 0
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if rel == "internal/testrules/pathstrings_test.go" {
				return nil // the forms themselves
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			files++
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "strings" {
					return true
				}
				for _, arg := range call.Args {
					lit, ok := arg.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					s, _ := strconv.Unquote(lit.Value)
					for _, form := range stateDirWithSlash {
						if strings.Contains(s, form) {
							t.Errorf("%s: strings.%s with %q — a path of the state directory taken apart as a string: on Windows it has backslashes; use filepath (Dir, Base, Rel)",
								fset.Position(lit.Pos()), sel.Sel.Name, s)
							break
						}
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if files == 0 {
		t.Fatal("no file was read: the rule sees nothing")
	}
}
