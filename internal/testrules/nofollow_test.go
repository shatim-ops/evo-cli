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

// directWrites are the places of production code that open a file for
// writing by its path — os.WriteFile, os.Create, os.OpenFile with a flag of
// writing: "file<TAB>function" → how many such calls it has and why it may. Such a call follows a symlink
// at the path and writes to its target. A write to evo's own files goes
// through the helper that refuses a link (O_NOFOLLOW) or through
// config.WriteFileAtomic (rename replaces a link). The places of R-67 (а)
// are here until «разработка» moves them to the helper: the list only
// shrinks.
type directWrite struct {
	n   int
	why string
}

var directWrites = map[string]directWrite{
	"internal/config/nofollow.go\tOpenNoFollow": {1, "the helper itself: O_NOFOLLOW and a regular file only (ledger 1.8, № 29)"},
	"internal/cli/main.go\tignoreInGit":         {1, "O_CREATE|O_EXCL: an existing file or symlink fails the open, nothing is written through it"},
}

// writeFlags are the flags of os.OpenFile that make it a write.
var writeFlags = map[string]bool{"O_WRONLY": true, "O_RDWR": true, "O_CREATE": true, "O_APPEND": true, "O_TRUNC": true}

// R-67 («Риск», conclusions 60–61; release plan 1.8, «R-67: запись evo
// через symlink из клонированного репозитория», (б)): evo's data directory
// lives in the project, and a symlink committed to a repository comes with
// git clone — deploy wrote known_hosts through it to its target. A write by
// path in production code is declared in directWrites with its reason; a
// new one fails the build, and so does a line whose place is gone.
func TestWrites_NoNewWriteThroughALink(t *testing.T) {
	root := filepath.Join("..", "..")
	found := map[string][]string{}
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			local := importNames(f)
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok || fd.Body == nil {
					continue
				}
				key := rel + "\t" + funcName(fd)
				ast.Inspect(fd.Body, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						return true
					}
					id, ok := sel.X.(*ast.Ident)
					if !ok {
						return true
					}
					pkg, name := local[id.Name], sel.Sel.Name
					write := false
					switch {
					case pkg == "os" && (name == "WriteFile" || name == "Create"):
						write = true
					case pkg == "io/ioutil" && name == "WriteFile":
						write = true
					case pkg == "os" && name == "OpenFile" && len(call.Args) > 1:
						write = hasWriteFlag(call.Args[1])
					}
					if write {
						found[key] = append(found[key], fset.Position(call.Pos()).String()+": "+pkg+"."+name)
					}
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		switch w := directWrites[k]; {
		case w.why == "":
			t.Errorf("%s: a write by path follows a symlink to its target (R-67) — write through the helper with O_NOFOLLOW or config.WriteFileAtomic, or declare it in directWrites with the reason", strings.Join(found[k], "; "))
		case len(found[k]) != w.n:
			t.Errorf("%s: %d writes by path, directWrites declares %d — a new one goes through the helper: %s", k, len(found[k]), w.n, strings.Join(found[k], "; "))
		}
	}
	for k := range directWrites {
		if found[k] == nil {
			t.Errorf("directWrites names %q, which writes by path no more: remove the line", k)
		}
	}
	if len(found) == 0 {
		t.Fatal("no write by path found: the scan sees nothing")
	}
}

// hasWriteFlag tells a flag expression of os.OpenFile that names a flag of
// writing (os.O_CREATE|os.O_RDWR…), or one the scan cannot read — a
// variable, which is taken for a write.
func hasWriteFlag(e ast.Expr) bool {
	found, unknown := false, false
	ast.Inspect(e, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if writeFlags[x.Sel.Name] {
				found = true
			}
			return false
		case *ast.Ident:
			unknown = true
		}
		return true
	})
	return found || unknown
}
