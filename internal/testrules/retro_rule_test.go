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

// retroRuleKnown are items of the retros with the outcome «правило» whose
// rule did not reach CLAUDE.md or docs/RELEASING.md: "<version> «<role>»:
// <the item's first words>" → what is to be done. The list only shrinks; a
// line the journals no longer give fails the test. Empty: its deadline 1.9
// is met, and a new line comes with a deadline of its own and its plan line:
// the owner writes the rule into the rules or the retro drops the outcome.
var retroRuleKnown = map[string]string{}

var (
	// retroRuleRef names the rules themselves: the file, a section (§N)
	// or a numbered rule of CLAUDE.md §0 (правило N).
	retroRuleRef = regexp.MustCompile(`CLAUDE\.md|RELEASING\.md|§\s*\d|правил[оау]\s+\d`)
	retroPR      = regexp.MustCompile(`#\d+`)
)

// The owner's decision 10.11 (release plan 1.8, «Проверка: правило из ретро
// дошло до правил»): the release's check of 10.11 found three of 33 items
// of the retros with the outcome «правило» whose rule CLAUDE.md and
// docs/RELEASING.md did not have. An item with the outcome «правило» names
// where the rule is: the rules themselves (CLAUDE.md, RELEASING.md, §N,
// правило N), or a PR the text of the rules names — the rules name the
// decision that brought each of them. A clone of CI has one commit, so the
// PR is checked by the text of the rules, not by git.
func TestRetro_RuleReachedTheRules(t *testing.T) {
	root := filepath.Join("..", "..")
	var rules strings.Builder
	for _, f := range []string{"CLAUDE.md", filepath.Join("docs", "RELEASING.md")} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		rules.Write(b)
	}
	inRules := map[string]bool{}
	for _, pr := range retroPR.FindAllString(rules.String(), -1) {
		inRules[pr] = true
	}
	found := map[string]bool{}
	n := 0
	for v, rs := range retros(t, filepath.Join(root, "docs", "internal", "handoff")) {
		for _, r := range rs {
			for _, it := range r.items {
				if it.repeat() || it.outcome() != "правило" {
					continue
				}
				n++
				if reached(it.text, inRules) {
					continue
				}
				key := v + " «" + roleName(r.role) + "»: " + firstWords(it.text, 8)
				found[key] = true
				if retroRuleKnown[key] == "" {
					t.Errorf("%s — the outcome «правило» names neither the rules (CLAUDE.md, RELEASING.md, §N, правило N) nor a PR the rules name:\n%s", key, it.text)
				}
			}
		}
	}
	for k := range retroRuleKnown {
		if !found[k] {
			t.Errorf("retroRuleKnown names %q, which the journals no longer give unreached: remove the line", k)
		}
	}
	if n < 20 {
		t.Fatalf("%d items with the outcome «правило»: the scan sees too few", n)
	}
}

// reached tells an item that names the rules or a PR their text names.
func reached(text string, inRules map[string]bool) bool {
	if retroRuleRef.MatchString(text) {
		return true
	}
	for _, pr := range retroPR.FindAllString(text, -1) {
		if inRules[pr] {
			return true
		}
	}
	return false
}

// firstWords is the first n words of s.
func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}
