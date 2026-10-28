// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/journal"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// FB140-1 (Fable, audit of 1.4.0): consent is one fact whichever way it
// was given. A command run with --yes is the call over MCP with
// confirmed=true (TestParity_YesEqualsConfirmed), and its record in the
// journal (D46) that is not the preview says so — confirmed: true — as
// the MCP record does, whether or not the call found anything to ask
// about (an unchanged deployment, none to destroy). The engine commands
// ask at the engine's gate (Caller.Gate) and the call carries no confirmed
// argument, so the journal wrote confirmed: false for a consented deploy
// or destroy: 32 of 42 such records in the journals of the Grok run of
// 1.4.0. Every command of the JSON world that needs consent.
func TestJournal_ConsentGivenIsRecorded(t *testing.T) {
	w, fake := withJSONWorld(t)
	if code, _, errOut := runCLI(t, "", "diag", "on"); code != 0 {
		t.Fatalf("diag on: %s", errOut)
	}
	path := journal.Path()
	cases, _, _ := jsonCases(t)
	found := map[string]string{}
	checked := 0
	for _, c := range cases {
		if !tools.NeedsConsent(c.tool, c.args) {
			continue
		}
		args := copyArgs(c.args)
		done := func() {}
		if c.setup != nil {
			extra, teardown := c.setup(t, w)
			for k, v := range extra {
				args[k] = v
			}
			done = teardown
		}
		line := c.line
		if line == nil {
			cmd := commandsByTool()[c.tool.Name]
			line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
		}
		before := journalSize(t, path)
		fake.reset()
		newProcess()
		runCLI(t, c.stdin, append([]string{"--json", "--yes"}, line...)...)
		done()
		for _, r := range journalSince(t, path, before) {
			if r.Surface != journal.SurfaceCLI || r.Depth != 0 || r.Tool != c.tool.Name || r.Preview {
				continue
			}
			checked++
			if !r.Confirmed {
				key := c.path
				if c.state != "" {
					key += " (" + c.state + ")"
				}
				found[key] = "ran with --yes, the journal records confirmed: false"
			}
		}
	}
	t.Logf("%d records of consented calls checked", checked)
	if checked == 0 {
		t.Fatal("no consented call reached the journal: the test proves nothing")
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Errorf("%s: %s", k, found[k])
	}
}

func journalSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// journalSince reads the records written after the first n bytes.
func journalSince(t *testing.T, path string, n int64) []journal.Record {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []journal.Record
	sc := bufio.NewScanner(bytes.NewReader(data[n:]))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var r journal.Record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatalf("journal line is not a record: %v", err)
		}
		out = append(out, r)
	}
	return out
}
