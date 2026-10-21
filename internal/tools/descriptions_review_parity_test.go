// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"os"
	"strings"
	"testing"
)

// TestReviewParity_RP_5_UnrecordedEnglishDescriptions: MCP tools/list
// English descriptions may differ from v1.1.0 only where CHANGELOG records
// a description change. deploy, status and org_projects grew sentences that
// v1.1.0 did not have: the unreleased section must have a bullet about the
// descriptions that names each of them.
func TestReviewParity_RP_5_UnrecordedEnglishDescriptions(t *testing.T) {
	b, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	_, unreleased, _ := strings.Cut(string(b), "## [Не выпущено]")
	unreleased, _, _ = strings.Cut(unreleased, "\n## [")
	var bullets []string
	for _, item := range strings.Split(unreleased, "\n- ") {
		if strings.HasPrefix(item, "Описания инструментов MCP") {
			bullets = append(bullets, item)
		}
	}
	recorded := func(name string) bool {
		for _, item := range bullets {
			if strings.Contains(item, "`"+name+"`") {
				return true
			}
		}
		return false
	}
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
		if strings.Contains(tl.Description, c.phrase) && !recorded(c.name) {
			t.Errorf("%s description differs from v1.1.0 (contains %q); no CHANGELOG bullet «Описания инструментов MCP …» of the unreleased section names `%s`",
				c.name, c.phrase, c.name)
		}
	}
}
