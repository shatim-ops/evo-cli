// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// After a call whose outcome is not known, the repeat sends a request only
// after a person answers — the same in the CLI and over MCP (release plan
// 1.9, «Повтор после оборванного вызова: --yes в CLI отправляет второй
// запрос там, где MCP спрашивает заново»; the interim delta audit of
// 13.11, high). The CLI calls a tool without the consent first and then
// with it; with --yes nobody answers the question in between. The parity of
// the repeat was held by a call over MCP with confirmed, not by the
// sequence the CLI makes: here each tool that asks for consent is cut — its
// first write gets no answer — and then repeated both ways, and the two
// send the same number of writes.
func TestParityWorld_YesAfterACutCallSendsWhatMCPSends(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, bad := jsonCases(t)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	world := w.rt.extra
	lost := errors.New("read: connection reset by peer")
	// prepare gives the arguments of c for a run, and what ends the run.
	prepare := func(c jsonCase) (map[string]any, func()) {
		args := copyArgs(c.args)
		if c.setup == nil {
			return args, func() {}
		}
		extra, done := c.setup(t, w)
		for k, v := range extra {
			args[k] = v
		}
		return args, done
	}
	// byTool calls the tool of c with the consent, as a call over MCP.
	byTool := func(c jsonCase) {
		args, done := prepare(c)
		defer done()
		args["confirmed"] = true
		fake.reset()
		newProcess()
		flagJSON, flagDryRun, flagYes = false, false, false
		tl, _ := newRegistry().Lookup(c.tool.Name)
		tl.Handler(context.Background(), args)
	}
	// cut calls the tool of c with the consent in a world whose every
	// write gets no answer, and says whether a record of the call stays.
	cut := func(c jsonCase) bool {
		forgetRun(w)
		w.rt.extra = func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				return nil, lost
			}
			return world(r)
		}
		defer func() { w.rt.extra = world }()
		byTool(c)
		left := 0
		for _, dir := range intent.Dirs(w.dir) {
			left += len(intent.Pending(dir))
		}
		return left > 0
	}
	ran, uncut := 0, []string{}
	var differ []string
	for _, c := range cases {
		if !c.tool.Mutating {
			continue
		}
		name := c.path
		if c.state != "" {
			name += " (" + c.state + ")"
		}
		if !cut(c) {
			uncut = append(uncut, name)
			continue
		}
		ran++
		// The repeat over MCP: one call with the consent.
		byTool(c)
		mcp := fake.takeWrites()

		// The repeat by the CLI: the command with --yes.
		if !cut(c) {
			t.Errorf("%s: the second cut left no record: the world is not the same twice", name)
			continue
		}
		args, done := prepare(c)
		line := c.line
		if line == nil {
			cmd := commandsByTool()[c.tool.Name]
			line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
		}
		fake.reset()
		newProcess()
		runCLI(t, c.stdin, append([]string{"--json", "--yes"}, line...)...)
		cli := fake.takeWrites()
		done()
		if len(cli) != len(mcp) {
			differ = append(differ, name+": --yes sent "+countOf(cli)+", MCP with the consent sent "+countOf(mcp))
		}
	}
	forgetRun(w)
	sort.Strings(differ)
	for _, d := range differ {
		t.Errorf("after a cut call, %s — the repeat sends a request only after a person answers, the same on both surfaces", d)
	}
	if ran < 20 {
		t.Errorf("%d tools were cut, want most of those that ask for consent: the cut no longer leaves a record (not cut: %v)", ran, uncut)
	}
	t.Logf("cut and repeated both ways: %d; differ: %d; not cut by a lost write (%d): %s", ran, len(differ), len(uncut), strings.Join(uncut, ", "))
}

// countOf is the number of writes, with the writes when there are any.
func countOf(writes []string) string {
	if len(writes) == 0 {
		return "0 writes"
	}
	return fmt.Sprintf("%d (%s)", len(writes), strings.Join(writes, "; "))
}
