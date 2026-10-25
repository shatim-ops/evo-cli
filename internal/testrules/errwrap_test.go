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
	"regexp"
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
)

// englishWraps are the packages whose wraps are still English only, each
// with its part of the fix (1.3.1, owner's decision 25.10: internal/tools
// first, then the clients of the services, then the rest). The list only
// shrinks: a wrap elsewhere fails the test, and so does a line whose
// package has none left.
var englishWraps = map[string]string{
	"internal/tools": "1.3.1, part 1",

	"internal/artifact":     "1.3.1, part 2",
	"internal/audit":        "1.3.1, part 2",
	"internal/billing":      "1.3.1, part 2",
	"internal/certmanager":  "1.3.1, part 2",
	"internal/containerapp": "1.3.1, part 2",
	"internal/dbaas":        "1.3.1, part 2",
	"internal/dns":          "1.3.1, part 2",
	"internal/mk8s":         "1.3.1, part 2",
	"internal/monitoring":   "1.3.1, part 2",
	"internal/nlb":          "1.3.1, part 2",
	"internal/postgresql":   "1.3.1, part 2",
	"internal/s3":           "1.3.1, part 2",
	"internal/secret":       "1.3.1, part 2",
	"internal/vpc":          "1.3.1, part 2",

	"internal/agentconf":  "1.3.1, part 3",
	"internal/auth":       "1.3.1, part 3",
	"internal/cli":        "1.3.1, part 3",
	"internal/config":     "1.3.1, part 3",
	"internal/deployplan": "1.3.1, part 3",
	"internal/kubetools":  "1.3.1, part 3",
	"internal/mcp":        "1.3.1, part 3",
	"internal/stack":      "1.3.1, part 3",
}

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
				lit, ok := call.Args[0].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				format, err := strconv.Unquote(lit.Value)
				if err != nil || !strings.Contains(format, "%w") {
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
