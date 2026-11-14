// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// #515, option A, layer B: the card of a group is generated from the
// registry's declarations, so 155 tools are covered by construction, not
// by prose. Every group has a card; the card names every tool of the group
// with its command; what a create makes says where its id is, how to find
// it and what deletes it (Decl.Created); a delete says a repeat is safe
// (Decl.Delete); an id argument says where the id comes from (Decl.IDArgs); a
// group with an operation tool says to wait with it. Consent is said in one
// place, the rules of the evo skill: no card carries its form.
func TestGroupCards_CoverEveryToolFromTheDeclarations(t *testing.T) {
	r := New(nil)
	for _, g := range Groups() {
		card, ok := GroupCard(g)
		if !ok {
			t.Errorf("group %s: no card", g)
			continue
		}
		for _, tl := range r.Tools() {
			if tl.Group != g {
				continue
			}
			if !strings.Contains(card, "`"+tl.Name+"`") {
				t.Errorf("%s: the card does not name %s", g, tl.Name)
			}
			if tl.CLI.Group != "" && !strings.Contains(card, "`evo "+tl.CLI.Group+" "+tl.CLI.Verb+"`") {
				t.Errorf("%s: the card does not give the command of %s", g, tl.Name)
			}
			if c, ok := createdOf(tl.Name); ok && c.delete != "" && !strings.Contains(card, "`"+c.delete+"`") {
				t.Errorf("%s: the card does not say %s deletes what %s makes", g, c.delete, tl.Name)
			}
		}
		if _, ok := r.Lookup(g + "_operation"); ok && !strings.Contains(card, "`"+g+"_operation`") {
			t.Errorf("%s: the card does not say to wait with %s_operation", g, g)
		}
		for _, form := range []string{"confirmed", "--yes"} {
			if strings.Contains(card, form) {
				t.Errorf("%s: the card carries %q — consent is the evo skill's rule, one place", g, form)
			}
		}
	}
	if _, ok := GroupCard("no-such-group"); ok {
		t.Error("a card for a group that is not there")
	}
}

// Every id argument with a list says where its id comes from.
func TestGroupCards_SayWhereAnIDComesFrom(t *testing.T) {
	card, _ := GroupCard("pg")
	if !strings.Contains(card, "`cluster_id` ← `pg_list`") {
		t.Errorf("pg: the card does not say cluster_id comes from pg_list:\n%s", card)
	}
}

// D68: the notes of a group (cardNotes) — the facts of the scenario skills
// a declaration cannot say — are in its card, under Notes; a note of a
// group that is not would be read by nobody. A note carries no form of
// consent either (TestGroupCards_CoverEveryToolFromTheDeclarations).
func TestGroupCards_PrintTheirNotes(t *testing.T) {
	if len(cardNotes) == 0 {
		t.Fatal("no notes in cardNotes: the test sees nothing")
	}
	for g, notes := range cardNotes {
		card, ok := GroupCard(g)
		if !ok {
			t.Errorf("cardNotes: %s is not a group", g)
			continue
		}
		if !strings.Contains(card, "\n## Notes\n") {
			t.Errorf("%s: the card has no Notes section:\n%s", g, card)
		}
		for _, n := range notes {
			if !strings.Contains(card, "\n- "+n+"\n") {
				t.Errorf("%s: the card does not print the note %q", g, n)
			}
		}
	}
	if card, _ := GroupCard("vm"); strings.Contains(card, "## Notes") {
		t.Errorf("vm: a Notes section with no notes:\n%s", card)
	}
}
