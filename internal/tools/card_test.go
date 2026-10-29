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
// it and what deletes it (createdIDs); a delete says a repeat is safe
// (deleteTargets); an id argument says where the id comes from (idArgs); a
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
			if c, ok := createdIDs[tl.Name]; ok && c.delete != "" && !strings.Contains(card, "`"+c.delete+"`") {
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
