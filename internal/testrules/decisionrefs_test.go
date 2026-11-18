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

// decisionRef is the number of an internal decision of the project (D17).
var decisionRef = regexp.MustCompile(`\bD[0-9]{1,3}\b`)

// The text evo shows a person or a model — a preview, a refusal, an
// answer, a description, the generated docs — does not cite the internal
// decisions of the project: the reader has no use for them (#2451, «Риск»,
// conclusion 380: the price preview printed «(D17)»). The reason is said in
// words. Every string literal of production code is walked; comments, where
// the decisions are cited, are not code.
func TestDecisionRefs_NotInProductText(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var bad []string
	walked := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "vendor" || d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return err
		}
		walked++
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err == nil && decisionRef.MatchString(s) {
				pos := fset.Position(lit.Pos())
				rel, _ := filepath.Rel(root, pos.Filename)
				bad = append(bad, filepath.ToSlash(rel)+":"+strconv.Itoa(pos.Line)+": "+decisionRef.FindString(s))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if walked < 100 {
		t.Fatalf("only %d files walked: the walk lost the tree", walked)
	}
	for _, b := range bad {
		t.Errorf("%s: an internal decision in the text a person or a model reads; say the reason in words", b)
	}
}
