// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"testing"

	"github.com/shatim-ops/evo-cli/internal/journal"
)

// FB140-1: a dry run of an engine command only looks. Its record is a
// preview and consents to nothing, --yes or not, as the MCP call without
// confirmed is; before, it was a record of neither (preview: false,
// confirmed: false) and read as a call that ran.
func TestJournal_EngineDryRunIsAPreview(t *testing.T) {
	w, fake := withJSONWorld(t)
	if code, _, errOut := runCLI(t, "", "diag", "on"); code != 0 {
		t.Fatalf("diag on: %s", errOut)
	}
	path := journal.Path()
	cases, _, _ := jsonCases(t)
	checked := 0
	for _, c := range cases {
		if c.tool.Name != "deploy" && c.tool.Name != "destroy" {
			continue
		}
		done := func() {}
		if c.setup != nil {
			_, done = c.setup(t, w)
		}
		for _, yes := range []bool{false, true} {
			line := append([]string{"--json", "--dry-run"}, c.line...)
			if yes {
				line = append([]string{"--yes"}, line...)
			}
			before := journalSize(t, path)
			fake.reset()
			newProcess()
			runCLI(t, c.stdin, line...)
			for _, r := range journalSince(t, path, before) {
				if r.Surface != journal.SurfaceCLI || r.Depth != 0 || r.Tool != c.tool.Name {
					continue
				}
				checked++
				if !r.Preview || r.Confirmed {
					t.Errorf("%s (%s) %v: preview %v, confirmed %v; want a preview that consents to nothing",
						c.path, c.state, line, r.Preview, r.Confirmed)
				}
			}
		}
		done()
	}
	if checked == 0 {
		t.Fatal("no dry run of deploy or destroy reached the journal: the test proves nothing")
	}
}
