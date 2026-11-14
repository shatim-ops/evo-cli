// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// External review of 11.11 (GPT on 71afe86), п. 2: stack_status advances
// the stack it shows — its next steps create resources (Decl.SideEffects,
// and its MCP annotation is not readOnlyHint), while «evo tools list» and
// «describe» gave it "mutating": false and its agent card «reads». An agent
// that picks tools by the catalog or the card took it for a read. Whether a
// tool is a read is one decision of the registry, and every place that
// tells it says the same: the card says «reads» only of a read, and the
// catalog shows no read-only face (mutating false and nothing else) for a
// tool that is not one.
func TestToolLabels_ReadOnlyWhereTheRegistryIsARead(t *testing.T) {
	setupCLI(t)
	reg := tools.New(nil)
	isRead := func(tl tools.Tool) bool {
		return !tl.Mutating && !tools.HasSideEffects(tl) && !tools.IsSensitive(tl)
	}

	code, stdout, stderr := runCLI(t, "", "tools", "list", "--json")
	if code != 0 {
		t.Fatalf("evo tools list --json: exit %d\n%s", code, stderr)
	}
	var list []map[string]any
	if err := json.Unmarshal([]byte(stdout), &list); err != nil {
		t.Fatalf("evo tools list --json: %v\n%.300s", err, stdout)
	}
	catalog := map[string]map[string]any{}
	for _, e := range list {
		name, _ := e["name"].(string)
		catalog[name] = e
	}
	// The catalog tells a tool that is not a read: mutating, or a key that
	// names its side effect.
	notReadInCatalog := func(e map[string]any) bool {
		return e["mutating"] == true || e["side_effects"] == true || e["read_only"] == false
	}

	cards := map[string]string{}
	row := regexp.MustCompile("^\\| `([a-z0-9_]+)` \\| [^|]*\\| ([^|]*) \\|")
	for _, g := range tools.Groups() {
		card, ok := tools.GroupCard(g)
		if !ok {
			continue
		}
		for _, line := range strings.Split(card, "\n") {
			if m := row.FindStringSubmatch(line); m != nil {
				cards[m[1]] = strings.TrimSpace(m[2])
			}
		}
	}
	if len(cards) == 0 || len(catalog) == 0 {
		t.Fatalf("cards %d, catalog %d: the scan sees nothing", len(cards), len(catalog))
	}

	for _, tl := range reg.Tools() {
		read := isRead(tl)
		if does, ok := cards[tl.Name]; ok && (does == "reads") != read {
			t.Errorf("%s: the card says %q, the registry: read %v", tl.Name, does, read)
		}
		e, ok := catalog[tl.Name]
		if !ok {
			t.Errorf("%s: not in evo tools list --json", tl.Name)
			continue
		}
		if !read && !tools.IsSensitive(tl) && !notReadInCatalog(e) {
			t.Errorf("%s: evo tools list shows it as a read (mutating %v), the registry: not a read (side effects)", tl.Name, e["mutating"])
		}
	}
}
