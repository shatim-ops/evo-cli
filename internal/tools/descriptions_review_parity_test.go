// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// TestReviewParity_RP_5_UnrecordedEnglishDescriptions: MCP tools/list
// English descriptions may differ from v1.1.0 only where CHANGELOG records
// a description change (provision, marketplace_list, stack_plan,
// image_create.image_metadata). These three grew sentences that v1.1.0 did
// not have, and that bullet does not name them.
func TestReviewParity_RP_5_UnrecordedEnglishDescriptions(t *testing.T) {
	reg := New(nil)
	for _, c := range []struct{ name, phrase string }{
		{"deploy", "for a new VM and for an update of the existing one alike"},
		{"status", "When the cloud answered, vm_name, zone_name and flavor_name"},
		{"org_projects", "and the active project (active)"},
	} {
		tl, ok := reg.Lookup(c.name)
		if !ok {
			t.Fatalf("no tool %s", c.name)
		}
		if strings.Contains(tl.Description, c.phrase) {
			t.Errorf("%s description differs from v1.1.0 (contains %q); CHANGELOG records description-text changes only for provision, marketplace_list, stack_plan and image_create.image_metadata",
				c.name, c.phrase)
		}
	}
}

// TestReviewParity_RP_6_ConsentPhraseNotAtStart: every tool that needs
// consent on every call should say so at the start of its MCP description.
// Clients cut a description at 1024 characters; a phrase left at the end is
// what the provision shortening moved. The phrase is still not at the start
// for the tools listed.
func TestReviewParity_RP_6_ConsentPhraseNotAtStart(t *testing.T) {
	reg := New(nil)
	var bad []string
	for _, tl := range reg.Tools() {
		if !AlwaysNeedsConsent(tl) {
			continue
		}
		head := tl.Description
		if len(head) > 160 {
			head = head[:160]
		}
		low := strings.ToLower(head)
		if strings.Contains(low, "confirmed") || strings.Contains(low, "consent") || strings.Contains(low, "approv") {
			continue
		}
		bad = append(bad, tl.Name)
	}
	if len(bad) > 0 {
		t.Errorf("%d tools that always need consent do not say so in the first 160 characters: %s", len(bad), strings.Join(bad, ", "))
	}
}
