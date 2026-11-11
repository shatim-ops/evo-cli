// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// stack_status advances the stack it shows, but 'tools list' and 'tools
// describe' said mutating: false with nothing else, and its card said
// "reads" (external review 11.11, item (2)). A tool with side effects
// (tools.HasSideEffects) says so in the catalog: side_effects: true in
// 'tools list --json' and 'tools describe', in words in describe. The card
// and the "reads" label: TestToolLabels_ReadOnlyWhereTheRegistryIsARead.
func TestToolsCatalog_SideEffectsFromTheRegistry(t *testing.T) {
	out, _, code := execCLI(t, "tools", "list", "--json")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(out), &list); err != nil {
		t.Fatal(err)
	}
	reg := newRegistry()
	seen := 0
	for _, e := range list {
		name, _ := e["name"].(string)
		tl, _ := reg.Lookup(name)
		got, _ := e["side_effects"].(bool)
		if got != tools.HasSideEffects(tl) {
			t.Errorf("%s: side_effects %v, registry %v", name, got, tools.HasSideEffects(tl))
		}
		if !tools.HasSideEffects(tl) {
			continue
		}
		seen++
		desc, _, _ := execCLI(t, "tools", "describe", name)
		if !strings.Contains(desc, "side effects") && !strings.Contains(desc, "побочн") {
			t.Errorf("tools describe %s does not name its side effects:\n%s", name, desc)
		}
		var d map[string]any
		dj, _, _ := execCLI(t, "tools", "describe", name, "--json")
		if err := json.Unmarshal([]byte(dj), &d); err != nil || d["side_effects"] != true {
			t.Errorf("tools describe %s --json: side_effects %v", name, d["side_effects"])
		}
	}
	if seen == 0 {
		t.Fatal("no tool with side effects in the catalog: stack_status is one")
	}
}
