// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The question about a call that was cut with its outcome unknown is
// answered by a person only (the interim delta audit of 1.9, high; «Риск»,
// conclusion 176, У1–У3). A command makes the call without consent itself
// and then answers the question: with --yes, with --json, without a
// terminal or under --dry-run nobody read it — nothing is sent, the exit
// is the one of «consent is needed», and the record of the cut call stays,
// however many times the command runs. A person at the terminal closes it
// with the answer: a yes sends one request, a no none. The tools are the
// ones whose every send is one more change — found by what a call over
// MCP answers after the cut, not by name.
func TestCutCall_OnlyAPersonAnswersTheQuestion(t *testing.T) {
	w, fake := withJSONWorld(t)
	cases, _, bad := jsonCases(t)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	world := w.rt.extra
	records := func() int {
		n := 0
		for _, dir := range intent.Dirs(w.dir) {
			n += len(intent.Pending(dir))
		}
		return n
	}
	oldTTY, oldIn := confirm.StdinIsTTY, confirm.Stdin
	t.Cleanup(func() { confirm.StdinIsTTY, confirm.Stdin = oldTTY, oldIn })
	asked := 0
	for _, c := range cases {
		if !c.tool.Mutating {
			continue
		}
		// prepare gives the arguments of the case for a run, and what
		// ends the run (a case that sets its world up: setup).
		prepare := func() (map[string]any, func()) {
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
		// byTool is a call over MCP with the consent; it returns the answer.
		byTool := func() string {
			args, done := prepare()
			defer done()
			args["confirmed"] = true
			fake.reset()
			newProcess()
			flagJSON, flagDryRun, flagYes = false, false, false
			tl, _ := newRegistry().Lookup(c.tool.Name)
			text, _ := tl.Handler(context.Background(), args)
			return text
		}
		// cut leaves the record of a call whose write got no answer.
		cut := func() bool {
			forgetRun(w)
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					return nil, errors.New("read: connection reset by peer")
				}
				return world(r)
			}
			defer func() { w.rt.extra = world }()
			byTool()
			return records() > 0
		}
		if !cut() {
			continue
		}
		interrupted, named := parseObject(byTool())["interrupted"].(map[string]any)
		if !named {
			continue
		}
		asked++
		// run runs the command and returns its exit code and its writes.
		run := func(flags ...string) (int, []string) {
			args, done := prepare()
			defer done()
			line := c.line
			if line == nil {
				cmd := commandsByTool()[c.tool.Name]
				line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
			}
			fake.reset()
			newProcess()
			code, _, _ := runCLI(t, c.stdin, append(flags, line...)...)
			return code, fake.takeWrites()
		}
		t.Run(c.path, func(t *testing.T) {
			if !cut() {
				t.Fatal("the second cut left no record")
			}
			confirm.StdinIsTTY = func() bool { return false }
			for _, flags := range [][]string{{"--yes"}, {"--yes"}, {"--json", "--yes"}, {"--json"}, {}, {"--yes"}} {
				code, writes := run(flags...)
				if code != 3 || len(writes) != 0 || records() != 1 {
					t.Errorf("%v: exit %d, writes %v, records %d; want exit 3, no write and the record kept", flags, code, writes, records())
				}
			}
			if code, writes := run("--dry-run"); code != 0 || len(writes) != 0 || records() != 1 {
				t.Errorf("--dry-run: exit %d, writes %v, records %d; want the record kept", code, writes, records())
			}
			// The refusal says why --yes did not answer, and sends to
			// the command that reads the resource, not to --yes.
			args, done := prepare()
			line := c.line
			if line == nil {
				cmd := commandsByTool()[c.tool.Name]
				line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, args).tree
			}
			fake.reset()
			newProcess()
			_, _, stderr := runCLI(t, c.stdin, append([]string{"--yes"}, line...)...)
			done()
			read, _ := newRegistry().Lookup(fmt.Sprint(interrupted["read_tool"]))
			if !strings.Contains(stderr, "Look at the resource with '"+toolCommandPath(read)+"'") || toolCommandPath(read) == "" ||
				!strings.Contains(stderr, "--yes does not answer") || !strings.Contains(stderr, "outcome is unknown") || strings.Contains(stderr, "Re-run with --yes") {
				t.Errorf("the refusal of --yes: %s", stderr)
			}

			if records() != 1 {
				t.Fatalf("%d records before the person answers, want the one of the cut call", records())
			}

			// A person says no: nothing is sent, and the cut call is
			// asked about no more.
			confirm.StdinIsTTY = func() bool { return true }
			confirm.Stdin = strings.NewReader("n\n")
			if code, writes := run(); code != 3 || len(writes) != 0 || records() != 0 {
				t.Errorf("a no at the terminal: exit %d, writes %v, records %d; want no write and the record closed", code, writes, records())
			}

			// A person says yes, --yes or not: one request, the record
			// closed.
			for _, flags := range [][]string{{}, {"--yes"}} {
				if !cut() {
					t.Fatal("the cut left no record")
				}
				confirm.Stdin = strings.NewReader("y\n")
				if code, writes := run(flags...); code != 0 || len(writes) != 1 || records() != 0 {
					t.Errorf("a yes at the terminal %v: exit %d, writes %v, records %d; want one write and the record closed", flags, code, writes, records())
				}
			}
		})
	}
	forgetRun(w)
	// Every tool whose send is one more change, as the registry declares
	// them (tools: everySendIsAChange): a tool that joins the list and is
	// in no subtest here — its world leaves no record, its case is not in
	// the world — is not held by this test. The number only grows.
	if asked < 9 {
		t.Errorf("%d tools ask again after a cut call, want the 9 of the registry or more", asked)
	}
}
