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

// englishWraps are the packages whose wraps are still English only, each
// with its part of the fix (1.3.1, owner's decision 25.10: internal/tools
// first, then the clients of the services, then the rest) or why it stays.
// The list only shrinks: a wrap elsewhere fails the test, and so does a
// line whose package has none left.
var englishWraps = map[string]string{
	"internal/agentconf":  "1.3.1, part 3",
	"internal/auth":       "1.3.1, part 3",
	"internal/cli":        "1.3.1, part 3",
	"internal/config":     "1.3.1, part 3",
	"internal/deployplan": "1.3.1, part 3",
	"internal/kubetools":  "1.3.1, part 3",
	"internal/mcp":        "stays: the MCP server speaks English (internal/cli/mcp.go sets i18n.EN; D6)",
	"internal/stack":      "1.3.1, with D41 («1.3 — разработка») or after it",
}

// copiedWraps are the wraps whose ru in i18n.T is the en copied —
// "package<TAB>format" → the PR that fixes it. The list only shrinks.
var copiedWraps = map[string]string{}

// CLAUDE.md §2, D6: what evo writes in an error is in the CLI's language;
// the cloud's own text stays the cloud's. An error that wraps another
// (fmt.Errorf with %w) puts evo's words before the cloud's, and a literal
// format is English in both languages: the format goes through i18n.T or
// i18n.Tf instead — the English text stays as it is. In production code of
// internal and cmd, a fmt.Errorf whose format is a string literal with %w
// and a word of prose fails the test, outside englishWraps.
func TestErrorWrapsSpeakCLILanguage(t *testing.T) {
	root := filepath.Join("..", "..")
	n := 0
	left := map[string]int{}
	copied := map[string]bool{}
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
			ast.Inspect(f, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || len(call.Args) == 0 || !isSel(call.Fun, "fmt", "Errorf") {
					return true
				}
				arg := call.Args[0]
				translated := false
				if tr, ok := arg.(*ast.CallExpr); ok && len(tr.Args) > 1 && (isSel(tr.Fun, "i18n", "T") || isSel(tr.Fun, "i18n", "Tf")) {
					// i18n.T(en, ru): the wrap is in the CLI's language only
					// when ru is Russian — an en copied into ru passes the
					// form, not the rule.
					ru, ok := tr.Args[1].(*ast.BasicLit)
					if !ok || ru.Kind != token.STRING {
						return true
					}
					arg, translated = ru, true
				}
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				format, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.Contains(format, "%w") {
					return true
				}
				if translated && cyrillic.MatchString(format) {
					return true
				}
				prose := wrapIdent.ReplaceAllString(wrapVerb.ReplaceAllString(format, " "), " ")
				if wrapWord.MatchString(prose) {
					n++
					pkg := filepath.ToSlash(filepath.Dir(rel))
					left[pkg]++
					if englishWraps[pkg] != "" {
						return true
					}
					if translated && copiedWraps[pkg+"\t"+format] != "" {
						copied[pkg+"\t"+format] = true
						return true
					}
					t.Errorf("%s:%d: fmt.Errorf(%s): evo's words before a wrapped error — the format through i18n.T(en, ru)",
						filepath.ToSlash(rel), fset.Position(lit.Pos()).Line, lit.Value)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for k := range copiedWraps {
		if !copied[k] {
			t.Errorf("copiedWraps names %q, whose ru is Russian now: remove the line", k)
		}
	}
	for pkg := range englishWraps {
		if left[pkg] == 0 {
			t.Errorf("englishWraps names %s, which has no English wrap left: remove the line", pkg)
		}
	}
	t.Logf("%d wraps in English only, in %d packages", n, len(left))
}

func isSel(e ast.Expr, pkg, name string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != name {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == pkg
}

// englishWrapsFile holds the English text of every wrap of englishWraps
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

// The fix of englishWraps changes the Russian CLI only: each English
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
