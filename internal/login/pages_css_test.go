// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"regexp"
	"strings"
	"testing"
)

// cssRule is a rule of the style of the pages whose selector is classes
// only (".alert", ".alert.warn") and which sets display.
type cssRule struct {
	classes []string
	display string
	order   int
	text    string
}

var (
	cssBlock    = regexp.MustCompile(`(?s)([^{}]+)\{([^{}]*)\}`)
	cssClasses  = regexp.MustCompile(`^(\.[A-Za-z][\w-]*)+$`)
	cssDisplay  = regexp.MustCompile(`(?:^|;)\s*display\s*:\s*([a-z-]+)`)
	htmlClasses = regexp.MustCompile(`class="([^"{}]+)"`)
)

// classRules are the rules of the static style of pages.html whose
// selector is classes only and which set display, in their order.
func classRules(t *testing.T) []cssRule {
	t.Helper()
	style := pagesHTML
	if i := strings.Index(style, "<style>"); i >= 0 {
		style = style[i+len("<style>"):]
	}
	if i := strings.Index(style, "</style>"); i >= 0 {
		style = style[:i]
	}
	var out []cssRule
	for i, m := range cssBlock.FindAllStringSubmatch(style, -1) {
		d := cssDisplay.FindStringSubmatch(m[2])
		if d == nil {
			continue
		}
		for _, sel := range strings.Split(m[1], ",") {
			sel = strings.TrimSpace(sel)
			if j := strings.LastIndex(sel, "*/"); j >= 0 {
				sel = strings.TrimSpace(sel[j+2:])
			}
			if !cssClasses.MatchString(sel) {
				continue
			}
			out = append(out, cssRule{classes: strings.Split(sel[1:], "."), display: d[1], order: i, text: sel + " { display: " + d[1] + " }"})
		}
	}
	return out
}

// Acceptance B-5 (06.11, ed95e3d6): with «Мне» chosen, the consent page
// showed «the agent will get project administrator rights» — the block
// .alert.warning.warn: «.warn { display: none }» hides it until the agent
// is chosen, and «.alert { display: flex }», of the same weight and later
// in the file, showed it always. A rule of classes that hides an element
// of the pages is not overridden by a rule of other classes of the same
// element that weighs as much and comes later, or weighs more: what the
// page hides until a choice stays hidden, and the consent says what evo
// does («Риск», the meanings of the consent, R-42).
func TestPages_HidingRuleIsNotOverriddenByAnotherClass(t *testing.T) {
	rules := classRules(t)
	hides := 0
	for _, r := range rules {
		if r.display == "none" {
			hides++
		}
	}
	if hides == 0 || len(rules) < 3 {
		t.Fatalf("%d rules of classes with display, %d of them hide: the scan sees nothing", len(rules), hides)
	}
	seen := map[string]bool{}
	for _, m := range htmlClasses.FindAllStringSubmatch(pagesHTML, -1) {
		el := strings.Fields(m[1])
		key := strings.Join(el, " ")
		if seen[key] {
			continue
		}
		seen[key] = true
		has := map[string]bool{}
		for _, c := range el {
			has[c] = true
		}
		var applies []cssRule
		for _, r := range rules {
			all := true
			for _, c := range r.classes {
				all = all && has[c]
			}
			if all {
				applies = append(applies, r)
			}
		}
		// The rule that wins the cascade: the most classes, then the last.
		var hide, win *cssRule
		for i := range applies {
			r := &applies[i]
			if r.display == "none" && hide == nil {
				hide = r
			}
			if win == nil || len(r.classes) > len(win.classes) || (len(r.classes) == len(win.classes) && r.order > win.order) {
				win = r
			}
		}
		if hide != nil && win.display != "none" {
			t.Errorf(`class="%s": %q hides it, and %q wins the cascade — the element is always visible`, key, hide.text, win.text)
		}
	}
}
