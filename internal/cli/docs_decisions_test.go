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

var (
	migrationSection = regexp.MustCompile(`(?ms)^### Миграция\n(.*?)(?:^##+ |\z)`)
	migrationItem    = regexp.MustCompile(`(?m)^- `)
	itemTitle        = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	decisionRowAny   = regexp.MustCompile(`(?m)^\| D(\d{1,3})\b[^|]*\|.*$`)
	stableHeading    = regexp.MustCompile(`(?m)^## \[\d+\.\d+\.\d+\]`)
	breakingWord     = regexp.MustCompile(`(?i)ломающ`)
)

// breakingDecisions are the numbers whose rows (any of them: D6, «D6
// (пересмотр)») say the change is breaking — «ломающее», not «не ломающее».
func breakingDecisions(table string) map[string]bool {
	out := map[string]bool{}
	for _, m := range decisionRowAny.FindAllStringSubmatch(table, -1) {
		row := strings.ToLower(m[0])
		for _, at := range breakingWord.FindAllStringIndex(row, -1) {
			if !strings.HasSuffix(row[:at[0]], "не ") {
				out[m[1]] = true
			}
		}
	}
	return out
}

// migrationItems are the items of the «Миграция» sections of text, each
// with its title (the first bold run, or its start).
func migrationItems(text string) map[string]string {
	out := map[string]string{}
	for _, sec := range migrationSection.FindAllStringSubmatch(text, -1) {
		for _, item := range migrationItem.Split(sec[1], -1)[1:] {
			title := item
			if m := itemTitle.FindStringSubmatch(item); m != nil {
				title = m[1]
			}
			out[title] = item
		}
	}
	return out
}

// migrationsWithoutBreakingDecision are the items of «Миграция» known to
// cite no decision marked breaking, title → what is missing; each is a
// line to «релиз» (#2472, 1–2). Only shrinks. Closes by 1.9, before the
// stable tag (#2472).
var migrationsWithoutBreakingDecision = map[string]string{
	"Базы данных без зоны при всех закрытых зонах — `USAGE` до вопроса": "no decision recorded (#2008)",
	"Целый аргумент вне 32 бит отвергается до обращения к облаку":       "D36 does not record the 32-bit bound as breaking",
	"`--json` CLI отвечает по-английски, как JSON инструмента MCP":      "no row of D6 is marked breaking",
}

// A breaking change of the coming version — an item of «Миграция» in a
// fragment of changelog.d or in a section of CHANGELOG.md above the last
// stable release — cites a decision whose row says it is breaking: the
// table of breaking changes of docs/RELEASING.md and the audit before the
// tag read the decisions, and a migration without one is a break nobody
// decided (audit of 1.9 rc.1, A19-17; #2472).
func TestDocs_MigrationCitesABreakingDecision(t *testing.T) {
	root := filepath.Join("..", "..")
	table, err := os.ReadFile(filepath.Join(root, "docs", "internal", "standalone", "DECISIONS.md"))
	if err != nil {
		t.Fatal(err)
	}
	breaking := breakingDecisions(string(table))
	if len(breaking) == 0 {
		t.Fatal("DECISIONS.md: no row marked breaking")
	}
	changelog, err := os.ReadFile(filepath.Join(root, "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	coming := string(changelog)
	if at := stableHeading.FindStringIndex(coming); at != nil {
		coming = coming[:at[0]]
	}
	sources := map[string]string{"CHANGELOG.md": coming}
	fragments, err := filepath.Glob(filepath.Join(root, "changelog.d", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fragments {
		if filepath.Base(f) == "README.md" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sources["changelog.d/"+filepath.Base(f)] = string(b)
	}
	found := map[string]bool{}
	for src, text := range sources {
		for title, item := range migrationItems(text) {
			ok := false
			for _, m := range decisionRef.FindAllStringSubmatch(item, -1) {
				ok = ok || breaking[m[1]]
			}
			if ok {
				continue
			}
			found[title] = true
			if migrationsWithoutBreakingDecision[title] == "" {
				t.Errorf("%s: «Миграция» %q cites no decision marked breaking (DECISIONS.md, «ломающее») — write the decision, or cite it", src, title)
			}
		}
	}
	for title := range migrationsWithoutBreakingDecision {
		if !found[title] {
			t.Errorf("migrationsWithoutBreakingDecision names %q, which cites a breaking decision now or is gone: remove the line (the list only shrinks)", title)
		}
	}

	for _, c := range []struct {
		name, table string
		want        bool
	}{
		{"breaking", "| D90 | x | y; ломающее, «Миграция» |\n", true},
		{"not breaking", "| D90 | x | y. Не ломающее: код тот же |\n", false},
		{"both", "| D96 | x | не ломающее для CLI; ломающее для MCP |\n", true},
		{"a refinement row", "| D6 (уточнение 14.11) | x | Ломающее по RELEASING |\n", true},
	} {
		if got := breakingDecisions(c.table)[decisionRowAny.FindStringSubmatch(c.table)[1]]; got != c.want {
			t.Errorf("%s: breaking %v, want %v", c.name, got, c.want)
		}
	}
}
