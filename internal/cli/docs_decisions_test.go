// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	decisionRef = regexp.MustCompile(`\bD(\d{1,3})\b`)
	decisionRow = regexp.MustCompile(`(?m)^\| D(\d{1,3}) \|`)
)

// A decision cited in the documents or the code (D17, D19) has its row in
// DECISIONS.md: the plan, a CHANGELOG entry or a comment that names one
// the table lacks sends the next session to nothing (D17 was cited from
// 1.2.1 until #188 wrote it; process audit 1.2.1, PR 5). The audits keep
// the history as it was and are not checked.
func TestDocs_DecisionsCitedAreWritten(t *testing.T) {
	root := filepath.Join("..", "..")
	table, err := os.ReadFile(filepath.Join(root, "docs", "internal", "standalone", "DECISIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	written := map[string]bool{}
	for _, m := range decisionRow.FindAllStringSubmatch(string(table), -1) {
		written[m[1]] = true
	}
	if len(written) == 0 {
		t.Fatal("DECISIONS.md: no rows | Dn |")
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, filepath.ToSlash(path))
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" || rel == "docs/internal/audits" || d.Name() == "archive" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".md") && !strings.HasSuffix(rel, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			for _, m := range decisionRef.FindAllStringSubmatch(line, -1) {
				if !written[m[1]] {
					t.Errorf("%s:%d: D%s is cited, DECISIONS.md has no row | D%s |", rel, i+1, m[1], m[1])
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
