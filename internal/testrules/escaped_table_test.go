// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The table of escaped defects (owner's decision 12.11; release plan 1.9,
// «Сторож таблицы ускользнувших дефектов»): handoff/<version>-journal.md,
// «Ускользнувшие дефекты», is the account of a debt — a defect the tests
// did not find, the measure that would have, and who owes it. An account
// nobody checks drifts from the work, as the retro did before R1–R6. The
// check is narrow, by the form of a row: six cells; the measure names an
// outcome of the retro's closed list (retroOutcomes) and the role that owes
// it, in «»; the status begins with «открыта» or «закрыта»; a closed row
// names what closed it — a PR or a test — and a test it names is in the
// tree. Whether the cause is right and the measure enough is not held
// here: that is the release's review of the classes before phase 0.

// escapedSince is the first version whose journal has the table.
const escapedSince = "1.9"

var (
	escapedHeading = regexp.MustCompile(`^## Ускользнувшие дефекты`)
	escapedRow     = regexp.MustCompile(`^\|\s*(\d+)\s*\|`)
	escapedRole    = regexp.MustCompile(`«[^»]+»`)
	escapedStatus  = regexp.MustCompile(`^(открыта|закрыта)(?:$|[\s:,;.—])`)
	escapedRef     = regexp.MustCompile(`#\d+|\bTest[A-Z]\w*`)
)

// escapedRows are the rows of the table of a journal: the number of the
// row and its cells.
func escapedRows(text string) (rows map[string][]string, order []string) {
	rows = map[string][]string{}
	in := false
	for _, l := range strings.Split(text, "\n") {
		switch {
		case escapedHeading.MatchString(l):
			in = true
			continue
		case in && (strings.HasPrefix(l, "## ") || strings.HasPrefix(l, "### ")):
			return rows, order
		case !in:
			continue
		}
		m := escapedRow.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(l), "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		rows[m[1]] = cells
		order = append(order, m[1])
	}
	return rows, order
}

// escapedProblems is what is wrong with the form of a row.
func escapedProblems(cells []string, tests map[string]bool) []string {
	if len(cells) != 6 {
		return []string{fmt.Sprintf("%d cells, want 6: №, defect, how it was found, why the tests did not find it, the measure and who owes it, the status (a «|» inside a cell splits it)", len(cells))}
	}
	var out []string
	for i, name := range []string{"№", "the defect", "how it was found", "why the tests did not find it"} {
		if cells[i] == "" {
			out = append(out, name+" is empty")
		}
	}
	measure, status := cells[4], cells[5]
	named := false
	for _, o := range retroOutcomes {
		named = named || strings.Contains(strings.ToLower(measure), o)
	}
	if !named {
		out = append(out, "the measure names no outcome of the retro's list ("+strings.Join(retroOutcomes, ", ")+")")
	}
	if !escapedRole.MatchString(measure) {
		out = append(out, "the measure names no role that owes it, in «»")
	}
	m := escapedStatus.FindStringSubmatch(status)
	if m == nil {
		return append(out, "the status begins with neither «открыта» nor «закрыта»")
	}
	if m[1] != "закрыта" {
		return out
	}
	refs := escapedRef.FindAllString(status, -1)
	if len(refs) == 0 {
		out = append(out, "a closed row names nothing that closed it: a PR (#N) or a test (Test…)")
	}
	for _, ref := range refs {
		if strings.HasPrefix(ref, "Test") && !tests[ref] {
			out = append(out, "closed by "+ref+", which is no test of the tree")
		}
	}
	return out
}

func TestEscaped_TableRowsHaveAMeasureAnOwnerAndAStatus(t *testing.T) {
	root := filepath.Join("..", "..")
	tests := testNames(t, root)
	files, _ := filepath.Glob(filepath.Join(root, "docs", "internal", "handoff", "*-journal.md"))
	checked := 0
	for _, f := range files {
		version := strings.TrimSuffix(filepath.Base(f), "-journal.md")
		if versionLess(version, escapedSince) {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rows, order := escapedRows(string(b))
		if version == escapedSince && len(order) == 0 {
			t.Errorf("%s: no rows under «Ускользнувшие дефекты»: the heading or the form of the table was changed, and the check reads nothing", filepath.Base(f))
		}
		for _, n := range order {
			checked++
			for _, p := range escapedProblems(rows[n], tests) {
				t.Errorf("%s, «Ускользнувшие дефекты», row %s: %s", filepath.Base(f), n, p)
			}
		}
	}
	t.Logf("rows checked: %d", checked)
}

// The check reads what it is meant to refuse: a row of each wrong form.
func TestEscaped_ProblemsOfARow(t *testing.T) {
	tests := map[string]bool{"TestThere": true}
	ok := []string{"1", "defect", "found", "why", "Проверка: мир несёт состояние — «Процесс»", "закрыта: `TestThere` (#1)"}
	if p := escapedProblems(ok, tests); len(p) != 0 {
		t.Fatalf("a row of the right form: %v", p)
	}
	for name, edit := range map[string]func(c []string) []string{
		"five cells":            func(c []string) []string { return c[:5] },
		"no outcome":            func(c []string) []string { c[4] = "починить — «Процесс»"; return c },
		"no role":               func(c []string) []string { c[4] = "Проверка: мир несёт состояние"; return c },
		"another status":        func(c []string) []string { c[5] = "проверка слита; М5 открыта"; return c },
		"closed by nothing":     func(c []string) []string { c[5] = "закрыта"; return c },
		"closed by a lost test": func(c []string) []string { c[5] = "закрыта: `TestGone`"; return c },
		"an empty cause":        func(c []string) []string { c[3] = ""; return c },
	} {
		if p := escapedProblems(edit(append([]string(nil), ok...)), tests); len(p) == 0 {
			t.Errorf("%s: a row of this form passes", name)
		}
	}
	for _, status := range []string{"открыта", "открыта: правка у «разработки»", "закрыта по мере: `TestThere` (#2)"} {
		row := append([]string(nil), ok...)
		row[5] = status
		if p := escapedProblems(row, tests); len(p) != 0 {
			t.Errorf("status %q: %v", status, p)
		}
	}
}
