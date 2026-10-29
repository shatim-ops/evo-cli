// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package testrules

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The plan review (owner's decision 29.10, docs/internal/handoff/README.md,
// «Сводка плана»): a release session that closes writes, next to its
// retro, what of the plan is done, what lags behind and what to do with
// it; the owner decides the lagging rows in one issue with a default for
// each. These checks hold what the template asks: a review for each retro
// of «релиз» from planReviewSince on (S1); the counts of the version and of
// the next one, what the session added and the owner's issue (S2); a row
// that names its plan line and ends in a default from the closed list
// (S3); at most planReviewMaxRows rows, so that the owner reads it (S4).

const (
	planReviewSince   = "1.5"
	planReviewMaxRows = 12
)

// planReviewKnown are retros of «релиз» written before the review: the
// version and the parentheses of the heading → why there is no review.
// The list only shrinks.
var planReviewKnown = map[string]string{
	"1.5 (29.10.2026)": "session 1 of 1.5 closed before the owner's decision of 29.10",
}

var (
	planReviewHeading = regexp.MustCompile(`^### Сводка плана «релиз · ([^»]+)» (\(.*\))$`)
	releaseRetro      = regexp.MustCompile(`^### Ретро «релиз · ([^»]+)» (\(.*\))$`)
	planReviewCount   = regexp.MustCompile(`^- \d+\.\d+(?:\.\d+)?: строк \d+ — сделано \d+, в PR \d+, не начато \d+`)
	planReviewNext    = regexp.MustCompile(`^- \d+\.\d+(?:\.\d+)?, следующая: строк \d+ — в PR \d+, не начато \d+; первым блоком — \S`)
	planReviewNew     = regexp.MustCompile(`^- Новое за сессию: строк \d+`)
	planReviewIssue   = regexp.MustCompile(`^- Решения владельца: (?:#\d+|нет отстающих)`)
	planReviewRef     = regexp.MustCompile("`release-plan\\.md:\\d+`|#\\d+")
	planReviewDefault = regexp.MustCompile(`^(?:держим|в \d+\.\d+(?:\.\d+)?|снять)\b`)
)

type planReview struct {
	key   string // version and the parentheses of the heading
	lines []string
}

// planReviews reads the reviews and the retros of «релиз» of each journal.
func planReviews(t *testing.T, dir string) (reviews map[string]planReview, retros []string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*-journal.md"))
	reviews = map[string]planReview{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cur string
		for _, l := range strings.Split(string(b), "\n") {
			if m := releaseRetro.FindStringSubmatch(l); m != nil {
				retros = append(retros, m[1]+" "+m[2])
			}
			if strings.HasPrefix(l, "#") {
				cur = ""
			}
			if m := planReviewHeading.FindStringSubmatch(l); m != nil {
				cur = m[1] + " " + m[2]
				reviews[cur] = planReview{key: cur}
				continue
			}
			if cur != "" {
				r := reviews[cur]
				r.lines = append(r.lines, l)
				reviews[cur] = r
			}
		}
	}
	return reviews, retros
}

func TestPlanReview_Rules(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "internal", "handoff")
	reviews, retros := planReviews(t, dir)
	found := map[string]bool{}
	for _, key := range retros {
		version, _, _ := strings.Cut(key, " ")
		if versionLess(version, planReviewSince) {
			continue
		}
		if _, ok := reviews[key]; ok {
			continue
		}
		found[key] = true
		if planReviewKnown[key] == "" {
			t.Errorf("S1: the retro «релиз · %s» has no «### Сводка плана «релиз · %s» %s» next to it (handoff/README.md, «Сводка плана»)",
				version, version, strings.TrimPrefix(key, version+" "))
		}
	}
	for k := range planReviewKnown {
		if !found[k] {
			t.Errorf("planReviewKnown names %q, which has a review now or no retro: remove the line", k)
		}
	}

	for key, r := range reviews {
		need := map[string]*regexp.Regexp{
			"the counts of the version": planReviewCount,
			"the counts of the next":    planReviewNext,
			"what the session added":    planReviewNew,
			"the owner's issue":         planReviewIssue,
		}
		rows := 0
		for _, l := range r.lines {
			for what, re := range need {
				if re.MatchString(l) {
					delete(need, what)
				}
			}
			cells, ok := planReviewRow(l)
			if !ok {
				continue
			}
			rows++
			if !planReviewRef.MatchString(cells[0]) {
				t.Errorf("S3 %s: a row names no plan line (`release-plan.md:N` or #N): %s", key, l)
			}
			if !planReviewDefault.MatchString(cells[len(cells)-1]) {
				t.Errorf("S3 %s: a row ends in no default (держим, в <версия>, снять): %s", key, l)
			}
		}
		for what := range need {
			t.Errorf("S2 %s: no line with %s (handoff/README.md, «Сводка плана»)", key, what)
		}
		if rows > planReviewMaxRows {
			t.Errorf("S4 %s: %d lagging rows, at most %d — group them", key, rows, planReviewMaxRows)
		}
	}
}

// planReviewRow are the cells of a body row of the table, false for the
// header, the rule and any other line.
func planReviewRow(l string) ([]string, bool) {
	if !strings.HasPrefix(l, "| ") || strings.HasPrefix(l, "|---") || strings.HasPrefix(l, "| Отстаёт") {
		return nil, false
	}
	parts := strings.Split(strings.Trim(l, "| "), " | ")
	if len(parts) < 2 {
		return nil, false
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts, true
}

// The template of the review is the one the journals follow.
func TestPlanReview_TemplateFollowsTheRules(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "internal", "handoff", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(b), "### Сводка плана «релиз · <версия>» (<дата>)\n")
	if !ok {
		t.Fatal("handoff/README.md: no template of the plan review")
	}
	body, _, _ := strings.Cut(rest, "```")
	filled := strings.NewReplacer(
		"<версия>", "1.5", "<следующая>", "1.6", "<N>", "10", "<A>", "1", "<B>", "2", "<C>", "3",
		"<K>", "4", "<откуда>", "аудит", "<блок>", "слепые зоны", "<строка>", "12", "<суть>", "что",
		"<роль>", "разработка", "<почему стоит>", "ждёт пробы", "<умолчание>", "в 1.6", "#<issue>", "#1",
	).Replace(body)
	for what, re := range map[string]*regexp.Regexp{
		"the counts of the version": planReviewCount, "the counts of the next": planReviewNext,
		"what the session added": planReviewNew, "the owner's issue": planReviewIssue,
	} {
		if !anyLine(filled, re) {
			t.Errorf("the template has no line with %s:\n%s", what, filled)
		}
	}
	for _, l := range strings.Split(filled, "\n") {
		if cells, ok := planReviewRow(l); ok && (!planReviewRef.MatchString(cells[0]) || !planReviewDefault.MatchString(cells[len(cells)-1])) {
			t.Errorf("the row of the template breaks S3: %s", l)
		}
	}
}

// anyLine: a line of the text matches.
func anyLine(text string, re *regexp.Regexp) bool {
	for _, l := range strings.Split(text, "\n") {
		if re.MatchString(l) {
			return true
		}
	}
	return false
}
