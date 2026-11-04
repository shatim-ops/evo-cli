// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// Release plan 1.7, P3: a delete preview says whether this folder's evo
// made what goes (provenance), the same on every surface — the command's
// --dry-run --json, 'evo tools call', MCP over stdio and HTTP
// (comparePreviews, the mechanism of TestContract_PreviewParity) — and the
// person at the terminal reads the value in words (CLAUDE.md §6, axis 4).
func TestContract_DeletePreviewNamesProvenance(t *testing.T) {
	world := withParityWorld(t)
	srv := parityServers(t)
	reg := tools.New(nil)
	commands := commandsByTool()
	for _, c := range []struct {
		name, tool string
		args       map[string]any
		setup      func(*testing.T, parityWorld) (map[string]any, func())
		engine     []string
		want       string
	}{
		{name: "evo vm delete of a VM this folder does not record", tool: "destroy", args: parityArgs["destroy"], want: "unknown"},
		{name: "evo destroy of the recorded deployment", tool: "destroy", args: map[string]any{}, setup: recordDeployment,
			engine: []string{"destroy"}, want: "yes"},
		{name: "evo pg delete (a kind the state does not record)", tool: "pg_delete", args: parityArgs["pg_delete"], want: "unknown"},
		{name: "evo vpc fip-release of an address no record names", tool: "release_floating_ip", args: parityArgs["release_floating_ip"], want: "unknown"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tl, ok := reg.Lookup(c.tool)
			if !ok {
				t.Fatalf("no tool %s", c.tool)
			}
			done := func() {}
			if c.setup != nil {
				_, done = c.setup(t, world)
			}
			defer done()
			cmd := commands[c.tool]
			pc := parityCommandOf(t, cmd.group, cmd.cmd, tl, c.args)
			if c.engine != nil {
				pc.tree, pc.engine = c.engine, true
			}
			if why := comparePreviews(t, c.name, pc, srv, map[string]string{}); why != "" {
				t.Fatal(why)
			}
			newProcess()
			_, out, _ := runCLI(t, "", append([]string{"--dry-run", "--json"}, pc.tree...)...)
			details, ok := dryRunDetails(out)
			if !ok {
				t.Fatalf("no preview: %s", out)
			}
			prov, _ := details["provenance"].(map[string]any)
			if prov["created_by_evo"] != c.want || prov["note"] == "" {
				t.Fatalf("provenance %v, want created_by_evo %s with a note", details["provenance"], c.want)
			}
			newProcess()
			_, out, errOut := runCLI(t, "", append([]string{"--dry-run"}, pc.tree...)...)
			if human := out + errOut; !strings.Contains(human, "created by evo: "+c.want+" — "+prov["note"].(string)) {
				t.Errorf("the human preview does not say the provenance:\n%s", human)
			}
		})
	}
}
