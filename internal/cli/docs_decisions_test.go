// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
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

var (
	reserveRow     = regexp.MustCompile(`(?m)^\| D(\d{1,3}) \| ([^|]*)\| \*\*Зарезервирован\*\*`)
	reserveVersion = regexp.MustCompile(`\((\d+\.\d+(?:\.\d+)?)\)`)
)

// releasedRow is the CHANGELOG heading that releases version v: a minor
// («1.3») is released by any of its tags, a patch («1.3.1») only by its
// own — a reserve for a patch of a released minor stays until that patch.
func releasedRow(v string) *regexp.Regexp {
	if strings.Count(v, ".") == 2 {
		return regexp.MustCompile(`(?m)^## \[` + regexp.QuoteMeta(v) + `\]`)
	}
	return regexp.MustCompile(`(?m)^## \[` + regexp.QuoteMeta(v) + `\.\d+\]`)
}

// reserveProblems checks the rows that hold an issued decision number
// until its PR writes the decision (CLAUDE.md §0, rule 4): a reserve names
// its version, gives way to the decision row instead of standing beside
// it, and does not outlive the release of its version.
func reserveProblems(table, changelog string) []string {
	rows := map[string]int{}
	for _, m := range decisionRow.FindAllStringSubmatch(table, -1) {
		rows[m[1]]++
	}
	var problems []string
	for _, m := range reserveRow.FindAllStringSubmatch(table, -1) {
		n := m[1]
		if rows[n] > 1 {
			problems = append(problems, fmt.Sprintf("D%s: reserve row beside the decision row — replace the reserve, do not add a second row", n))
		}
		v := reserveVersion.FindStringSubmatch(m[2])
		if v == nil {
			problems = append(problems, fmt.Sprintf("D%s: reserve row names no version, e.g. «(1.3)»", n))
			continue
		}
		if releasedRow(v[1]).MatchString(changelog) {
			problems = append(problems, fmt.Sprintf("D%s: still reserved, but CHANGELOG releases %s — write the decision or mark it «**Снят**»", n, v[1]))
		}
	}
	return problems
}

func TestDocs_DecisionReserves(t *testing.T) {
	root := filepath.Join("..", "..")
	table, err := os.ReadFile(filepath.Join(root, "docs", "internal", "standalone", "DECISIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range reserveProblems(string(table), string(changelog)) {
		t.Error("DECISIONS.md: " + p)
	}

	const reserve = "| D26 | Повторный delete (1.3) | **Зарезервирован** «1.3 — релизом»; решение пишет PR | правило 4 |\n"
	for _, c := range []struct {
		name, table, changelog string
		want                   int
	}{
		{"open version", reserve, "## [Не выпущено]\n## [1.2.3] — 2026-10-23\n", 0},
		{"rc is not a release", reserve, "## [Не выпущено]\n", 0},
		{"released version", reserve, "## [1.3.0] — 2026-11-01\n", 1},
		{"released patch", reserve, "## [1.3.1] — 2026-11-05\n", 1},
		{"patch reserve, minor released", "| D39 | Образ до вопроса (1.3.1) | **Зарезервирован** «1.3 — релизом» | правило 4 |\n", "## [Не выпущено]\n## [1.3.0] — 2026-10-24\n", 0},
		{"patch reserve, patch released", "| D39 | Образ до вопроса (1.3.1) | **Зарезервирован** «1.3 — релизом» | правило 4 |\n", "## [1.3.1] — 2026-11-05\n## [1.3.0] — 2026-10-24\n", 1},
		{"no version", "| D26 | Повторный delete | **Зарезервирован** «1.3 — релизом» | правило 4 |\n", "", 1},
		{"beside the decision", reserve + "| D26 | Повторный delete (1.3) | **Отказ не нужен** | … |\n", "", 1},
		{"written", "| D26 | Повторный delete (1.3) | **Отказ не нужен** | … |\n", "## [1.3.0] — 2026-11-01\n", 0},
		{"withdrawn", "| D26 | Повторный delete (1.3) | **Снят** | … |\n", "## [1.3.0] — 2026-11-01\n", 0},
	} {
		if got := reserveProblems(c.table, c.changelog); len(got) != c.want {
			t.Errorf("%s: %d problems %q, want %d", c.name, len(got), got, c.want)
		}
	}
}
