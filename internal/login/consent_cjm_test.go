// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

var (
	htmlTag  = regexp.MustCompile(`<(/?)([a-zA-Z][a-zA-Z0-9]*)\b([^>]*)>`)
	htmlAttr = regexp.MustCompile(`([a-zA-Z-]+)(?:="([^"]*)")?`)
)

// tagAt is a tag of a page: its name, whether it closes, its attributes.
type tagAt struct {
	name  string
	close bool
	attr  map[string]string
}

func pageTags(page string) []tagAt {
	var out []tagAt
	for _, m := range htmlTag.FindAllStringSubmatch(page, -1) {
		t := tagAt{name: strings.ToLower(m[2]), close: m[1] == "/", attr: map[string]string{}}
		for _, a := range htmlAttr.FindAllStringSubmatch(m[3], -1) {
			t.attr[strings.ToLower(a[1])] = html.UnescapeString(a[2])
		}
		out = append(out, t)
	}
	return out
}

// manyProjectsConsent is the consent page of a person with two
// organizations of five projects each, one of them without the right.
func manyProjectsConsent(t *testing.T) (string, []project) {
	t.Helper()
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })
	s := &session{cfg: Config{Now: time.Now}, choice: Choice{For: ForMe, Days: 30}}
	for o := 0; o < 2; o++ {
		for p := 0; p < 5; p++ {
			s.projects = append(s.projects, project{ID: fmt.Sprintf("p-%d-%d", o, p), Name: fmt.Sprintf("Проект %d-%d", o, p),
				Customer: fmt.Sprintf("c-%d", o), CustomerName: fmt.Sprintf("Организация %d", o), Allowed: p != 3})
		}
	}
	pg := consentPage(s)
	var b bytes.Buffer
	if err := pages.ExecuteTemplate(&b, pg.name, pg.data); err != nil {
		t.Fatal(err)
	}
	return b.String(), s.projects
}

// Release plan 1.8, «B-5, дефект CJM страницы согласия» (owner, 06.11;
// #1084): the consent page of evo login fits one screen of a laptop —
// «Организация» and «Проект» are drop-down lists, not long lists of radio
// buttons — and a click at the bottom of the form does not throw the
// screen to its top. Acceptance B-5 (06.11, Playwright on the page of two
// organizations of five projects, 1366×768 and 1440×900): the page is
// 1877 px high, and a click on a term below sends scrollY to 0 — the label
// focuses its radio, which stands at the top of the form, far from it.
// The test holds the structure; the height is measured in the browser
// (the brief of the acceptance). The meanings of the consent stay —
// TestConsent_Meanings (R-42, R-47).
func TestConsent_OneScreenStructure(t *testing.T) {
	page, projects := manyProjectsConsent(t)
	checkOneScreenStructure(t, page, projects)
}

func checkOneScreenStructure(t testing.TB, page string, projects []project) {
	t.Helper()
	tags := pageTags(page)

	// 1. The project is chosen in a drop-down list, the organizations are
	// in it (or in one of their own): no radio button per project.
	var inSelect, selects int
	options, groups := map[string]bool{}, map[string]bool{}
	disabled := map[string]bool{}
	for _, tg := range tags {
		switch {
		case tg.name == "select" && !tg.close:
			selects++
			inSelect++
		case tg.name == "select" && tg.close:
			inSelect--
		case tg.name == "optgroup" && !tg.close && inSelect > 0:
			groups[tg.attr["label"]] = true
		case tg.name == "option" && !tg.close && inSelect > 0:
			options[tg.attr["value"]] = true
			if _, ok := tg.attr["disabled"]; ok {
				disabled[tg.attr["value"]] = true
			}
		case tg.name == "input" && tg.attr["name"] == "project" && tg.attr["type"] == "radio":
			t.Errorf("a radio button per project (%s): the project is chosen in a drop-down list", tg.attr["value"])
		}
	}
	if selects == 0 {
		t.Errorf("no <select> on the page: «Организация» and «Проект» are drop-down lists")
	}
	orgs := map[string]bool{}
	for _, p := range projects {
		orgs[p.CustomerName] = true
		if !options[p.ID] {
			t.Errorf("project %q is no option of a drop-down list", p.Name)
		}
		if !p.Allowed && !disabled[p.ID] {
			t.Errorf("project %q without the right is an option one can choose: disabled, as the radio was", p.Name)
		}
	}
	for o := range orgs {
		if !groups[o] && !strings.Contains(selectTexts(page), o) {
			t.Errorf("organization %q is in no drop-down list (an optgroup or an option of its own)", o)
		}
	}

	// 2. Nothing throws the screen up: no link to «#», and an input a label
	// points to stands in that label or in its fieldset, or is fixed in
	// the viewport — a radio at the top of the form gets the focus of a
	// click below and scrolls to it.
	type place struct {
		fieldset, label int
		fixed           bool
	}
	fixed := fixedInputTypes(page)
	inputs, labels := map[string]place{}, map[string]int{}
	var fieldsets, labelStack []int
	n := 0
	for _, tg := range tags {
		fs := -1
		if len(fieldsets) > 0 {
			fs = fieldsets[len(fieldsets)-1]
		}
		switch {
		case tg.name == "a" && !tg.close && strings.HasPrefix(tg.attr["href"], "#"):
			t.Errorf("a link to %q: it moves the screen", tg.attr["href"])
		case tg.name == "fieldset" && !tg.close:
			n++
			fieldsets = append(fieldsets, n)
		case tg.name == "fieldset" && tg.close && len(fieldsets) > 0:
			fieldsets = fieldsets[:len(fieldsets)-1]
		case tg.name == "label" && !tg.close:
			n++
			labelStack = append(labelStack, n)
			if f := tg.attr["for"]; f != "" {
				labels[f] = fs
			}
		case tg.name == "label" && tg.close && len(labelStack) > 0:
			labelStack = labelStack[:len(labelStack)-1]
		case (tg.name == "input" || tg.name == "select") && tg.attr["id"] != "":
			p := place{fieldset: fs, label: -1, fixed: fixed[tg.attr["type"]]}
			if len(labelStack) > 0 {
				p.label = labelStack[len(labelStack)-1]
			}
			inputs[tg.attr["id"]] = p
		}
	}
	for id, fs := range labels {
		in, ok := inputs[id]
		if !ok {
			continue
		}
		if in.label < 0 && (in.fieldset < 0 || in.fieldset != fs) && !in.fixed {
			t.Errorf("label for=%q stands in a fieldset, and its input outside it, not position: fixed: a click on the label scrolls to the input", id)
		}
	}
}

// fixedInputTypes are the types of input the style of the page fixes in
// the viewport (input[type=radio] { position: fixed }): a focused input
// that is always in view scrolls nothing (#1085 keeps the radios at the
// top of the form so).
func fixedInputTypes(page string) map[string]bool {
	out := map[string]bool{}
	sel := regexp.MustCompile(`input\[type=([a-z]+)\]$`)
	pos := regexp.MustCompile(`(?:^|;)\s*position\s*:\s*fixed`)
	for _, m := range cssBlock.FindAllStringSubmatch(page, -1) {
		if !pos.MatchString(m[2]) {
			continue
		}
		for _, s := range strings.Split(m[1], ",") {
			if t := sel.FindStringSubmatch(strings.TrimSpace(s)); t != nil {
				out[t[1]] = true
			}
		}
	}
	return out
}

// selectTexts is the text of the drop-down lists of a page.
func selectTexts(page string) string {
	var b strings.Builder
	for _, m := range regexp.MustCompile(`(?s)<select\b.*?</select>`).FindAllString(page, -1) {
		b.WriteString(html.UnescapeString(m))
	}
	return b.String()
}
