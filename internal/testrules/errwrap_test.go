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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// wrapWord is a word of prose in the format of a wrap: evo's words before
// the cloud's error, which the Russian CLI showed in English («create zone:
// dns API 500: …»; #319, D6). An identifier — a name with "_" or a
// dotted path — and a format verb are not prose.
var (
	wrapVerb  = regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z]`)
	wrapIdent = regexp.MustCompile(`[A-Za-z0-9]*[_.][A-Za-z0-9_.]*`)
	wrapWord  = regexp.MustCompile(`[A-Za-z]{2,}`)
	cyrillic  = regexp.MustCompile(`\p{Cyrillic}`)
)

func isSel(e ast.Expr, pkg, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != name {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// englishWrapsFile holds the English text of every historical wrap
// as it was before the fix (1.3.1): "package<TAB>format" per line. The fix
// moves a format into i18n.T(en, ru) and must keep en as it was — a script
// may match it, and EVO_LANG=en and the MCP server answer it as before.
const englishWrapsFile = "testdata/english-wraps.txt"

// wrapFormats counts the English formats of the wraps of production code
// by package: a literal format of fmt.Errorf, or the first argument of
// i18n.T or i18n.Tf that is the format of fmt.Errorf, with %w and prose.
func wrapFormats(t *testing.T) map[string]int {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]int{}
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			pkg := filepath.ToSlash(filepath.Dir(rel))
			ast.Inspect(f, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 || !isSel(call.Fun, "fmt", "Errorf") {
					return true
				}
				arg := call.Args[0]
				if tr, ok := arg.(*ast.CallExpr); ok && len(tr.Args) > 0 && (isSel(tr.Fun, "i18n", "T") || isSel(tr.Fun, "i18n", "Tf")) {
					arg = tr.Args[0]
				}
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				format, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.Contains(format, "%w") {
					return true
				}
				if wrapWord.MatchString(wrapIdent.ReplaceAllString(wrapVerb.ReplaceAllString(format, " "), " ")) {
					out[pkg+"\t"+format]++
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// Localizing the historical wraps changes the Russian CLI only: each English
// format of englishWrapsFile is still in its package, as a literal or as
// the en of i18n.T, as many times as before. A wrap removed or reworded on
// purpose takes its line out of the file in the same PR, for the reviewer
// to see.
func TestErrorWrapsKeepTheirEnglish(t *testing.T) {
	b, err := os.ReadFile(englishWrapsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{}
	for _, line := range strings.Split(strings.TrimRight(string(b), "\n"), "\n") {
		if line != "" {
			want[line]++
		}
	}
	if len(want) == 0 {
		t.Fatalf("%s is empty", englishWrapsFile)
	}
	got := wrapFormats(t)
	if len(got) == 0 {
		t.Fatal("no wrap found: the walk of the code is broken")
	}
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if got[k] < want[k] {
			pkg, format, _ := strings.Cut(k, "\t")
			t.Errorf("%s: the English wrap %q is there %d times, was %d — keep en of i18n.T as it was, or take the line out of %s on purpose",
				pkg, format, got[k], want[k], englishWrapsFile)
		}
	}
}
