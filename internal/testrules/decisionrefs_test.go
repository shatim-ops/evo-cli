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

// docsOutOfReach are the files under docs/ that are not for a reader of
// the product: the project's internals, and the release rules, which cite
// the decisions they enforce and only the owner changes.
var docsOutOfReach = map[string]bool{"docs/internal": true, "docs/RELEASING.md": true}

// The same for the user documentation under docs/: the pages a person or a
// model reads cite no internal decision (#2462; the owner's rule: clean the
// sources, not the published copy). Generated pages come from the code the
// test above walks; this one reads the hand-written ones too.
func TestDecisionRefs_NotInUserDocs(t *testing.T) {
	root := filepath.Join("..", "..")
	read := 0
	err := filepath.WalkDir(filepath.Join(root, "docs"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if docsOutOfReach[rel] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		read++
		for i, line := range strings.Split(string(data), "\n") {
			if ref := decisionRef.FindString(line); ref != "" {
				t.Errorf("%s:%d: %s: an internal decision in a page a person or a model reads; say the reason in words", rel, i+1, ref)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if read < 10 {
		t.Fatalf("only %d pages read: the walk lost docs/", read)
	}
}
