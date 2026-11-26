// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"regexp"
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
			// The history result cleanup_unconfirmed is a verdict, not a consent argument.
			if strings.Contains(strings.ReplaceAll(card, "`check_result: cleanup_unconfirmed`", ""), form) {
				t.Errorf("%s: the card carries %q — consent is the evo skill's rule, one place", g, form)
			}
		}
	}
	if _, ok := GroupCard("no-such-group"); ok {
		t.Error("a card for a group that is not there")
	}
}

func TestAuthCard_HandsLocalReceiptToTheOwner(t *testing.T) {
	for _, terminal := range []bool{false, true} {
		card, ok := groupCard("auth", terminal, nil, false)
		for _, want := range []string{"cleanup_unconfirmed", "floating IP", "port rule", "live", "cleanup hint", "person", "original state directory"} {
			if !ok || !strings.Contains(card, want) {
				t.Fatalf("terminal=%t: auth card omits %q", terminal, want)
			}
		}
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

// The operator and the agent both need to interpret a history row without
// consulting another document. Inspect the note containing the actual status.
func TestAuthCard_ExplainsUnconfirmedCleanup(t *testing.T) {
	// Test the positive relationship between the history row and the live
	// collection. Seeing either word elsewhere does not describe retention.
	row := `(?:(?:partial destroy|history) (?:row|entry)|partial destroy|row|entry)`
	retained := regexp.MustCompile(`^(?:(?:keeps|retains) (?:a|the) ` + row + `|(?:a|the) ` + row + ` (?:remains|stays)) in live\b`)
	action := regexp.MustCompile(`(?:^|[.;:])\s*(?:(?:retry|repeat|rerun|re-run|run|call|use)\b[^.;\n]{0,65}\b(?:destroy|vm_delete|receipts?|cleanup)\b|(?:check|verify|inspect|review)\b[^.;\n]{0,65}\b(?:address|ip|rules?|receipts?|billing|cleanup)\b)`)
	for _, terminal := range []bool{true, false} {
		card, ok := groupCard("auth", terminal, nil, false)
		if !ok {
			t.Fatal("missing auth card")
		}
		var note string
		for _, line := range strings.Split(card, "\n") {
			if _, clause, found := strings.Cut(strings.ToLower(line), "`check_result: cleanup_unconfirmed`"); found {
				// The surrounding PostgreSQL example and advice for other
				// statuses must not satisfy a pending destroy's requirements.
				note, _, _ = strings.Cut(clause, "other values:")
				note = strings.TrimSpace(strings.ReplaceAll(note, "`", ""))
				break
			}
		}
		if note == "" {
			t.Errorf("terminal=%t: missing cleanup_unconfirmed", terminal)
			continue
		}
		if !retained.MatchString(note) {
			t.Errorf("terminal=%t: cleanup_unconfirmed must say the partial destroy is kept in live: %s", terminal, note)
		}
		if !strings.Contains(note, "rule") {
			t.Errorf("terminal=%t: missing remaining rule: %s", terminal, note)
		}
		if !strings.Contains(note, "address") && !strings.Contains(note, "floating ip") {
			t.Errorf("terminal=%t: missing remaining address: %s", terminal, note)
		}
		if !action.MatchString(note) {
			t.Errorf("terminal=%t: missing recovery action and target for cleanup_unconfirmed: %s", terminal, note)
		}
	}
}
