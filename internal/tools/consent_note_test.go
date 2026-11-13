// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"sort"
	"strings"
	"testing"
)

// «Риск», conclusion 152 (release plan 1.9, the budget of tools/list): the
// rule of consent is one wording at every tool that asks — there were 15,
// a dozen of them without the person, and 11 tools that ask said nothing.
// A tool that takes «confirmed» ends its description with consentNote, the
// word of its class before it; the two tools that ask for a part of their
// calls say so in their own words, with confirmed=true and the user. What
// is approved — the preview — is named by the argument, at every one.
func TestConsentNote_OneWordingAtEveryToolThatAsks(t *testing.T) {
	words := map[string]bool{consentBillable: true, consentDeletes: true, consentInterrupts: true, consentReveals: true, consentOverwrites: true}
	var bad []string
	seen := 0
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		arg, asks := props["confirmed"].(map[string]any)
		word := consentWords[tl.Name]
		switch {
		case !asks:
			if word != "" || partialConsent[tl.Name] != nil || strings.Contains(tl.Description, "confirmed=true") {
				bad = append(bad, tl.Name+": takes no confirmed, and its description or a declaration speaks of consent")
			}
			continue
		case partialConsent[tl.Name] != nil:
			if !strings.Contains(tl.Description, "confirmed=true after the user approves") || strings.Contains(tl.Description, consentNote) || word != "" {
				bad = append(bad, tl.Name+": in partialConsent — its description names confirmed=true after the user approves in its own sentence, and carries no note of all")
			}
		default:
			want := consentText(tl.Name)
			// The note of the untrusted output, where a tool has one, is
			// the last (TestUntrustedOutputNote).
			if !strings.HasSuffix(strings.TrimSuffix(tl.Description, " "+untrustedOutputNote), want) || strings.Count(strings.ToLower(tl.Description), "requires confirmed=true") != 1 {
				bad = append(bad, tl.Name+": its description does not end with the consent note ("+want+"), or speaks of confirmed=true a second time in other words")
			}
			if word != "" && !words[word] {
				bad = append(bad, tl.Name+": the word of its class is not of the closed list")
			}
		}
		seen++
		// The preview is what the person approves: the argument says so.
		if desc, _ := arg["description"].(string); !strings.HasSuffix(desc, confirmedDesc) || !strings.Contains(confirmedDesc, "preview") || !strings.Contains(confirmedDesc, "person") {
			bad = append(bad, tl.Name+": the description of confirmed does not name the preview and the person")
		}
	}
	// A word that is true of the tool: what deletes data or interrupts the
	// service is declared destructive, and nothing else carries the word.
	for name, word := range consentWords {
		tl, _ := New(nil).Lookup(name)
		if (word == consentDeletes || word == consentInterrupts) && !IsDestructive(tl) {
			bad = append(bad, name+": «"+word+"» at a tool that is not destructive (IsDestructive)")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	if seen < 60 {
		t.Fatalf("%d tools ask for consent: the rule reads few", seen)
	}
}

// Every delete names the class of what the person approves, or declares
// why its word is not «Deletes data» (FB19I-29: the map was by hand, and
// pg_delete said it while disk_delete, pg_db_delete, secret_delete did
// not). A declared reason is not the word too.
func TestConsentWord_EveryDeleteNamesItsClassOrWhyNot(t *testing.T) {
	var bad []string
	for name := range deleteTargets {
		word, why := consentWords[name], consentWordless[name]
		switch {
		case word == "" && why == "":
			bad = append(bad, name+": a delete without the word of its class and without a reason in consentWordless")
		case word != "" && why != "":
			bad = append(bad, name+": the word of its class and a reason not to have one")
		}
	}
	for name := range consentWordless {
		if _, ok := deleteTargets[name]; !ok {
			bad = append(bad, name+": in consentWordless, but not a delete of deleteTargets")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
}
