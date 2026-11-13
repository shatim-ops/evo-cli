// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
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
// ones whose every send is one more change — everySendIsAChange, read from
// its source; each must have a case in the world, with its setup or
// without, that is cut and asked again.
func TestCutCall_OnlyAPersonAnswersTheQuestion(t *testing.T) {
	declared := everySendDeclared(t) // before the world moves the process
	w, fake := withJSONWorld(t)
	cases, _, bad := jsonCases(t)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	base := w.rt.extra
	records := func() int {
		n := 0
		for _, dir := range intent.Dirs(w.dir) {
			n += len(intent.Pending(dir))
		}
		return n
	}
	oldTTY, oldIn := confirm.StdinIsTTY, confirm.Stdin
	t.Cleanup(func() { confirm.StdinIsTTY, confirm.Stdin = oldTTY, oldIn })
	asked, probed := 0, 0
	ran, declaredTool := map[string]bool{}, map[string]bool{}
	for _, name := range declared {
		declaredTool[name] = true
	}
	for _, c := range cases {
		// Only a declared tool asks again (askAgainAfterACut reads the
		// list), so only they are cut: a cut of every consented call of
		// the world cost 16 of the test's 20 seconds in the retries of
		// the S3 client, to find the same three.
		if !c.tool.Mutating || !declaredTool[c.tool.Name] {
			continue
		}
		probed++
		// A case that prepares its world (setup) is a case as any other:
		// left out, sg_rule_create and ten more went unread («Риск», the
		// probe on #1602; release plan 1.9).
		caseArgs, world, done := copyArgs(c.args), base, func() {}
		if c.setup != nil {
			extra, teardown := c.setup(t, w)
			for k, v := range extra {
				caseArgs[k] = v
			}
			world, done = w.rt.extra, teardown
		}
		// byTool is a call over MCP with the consent; it returns the answer.
		byTool := func() string {
			args := copyArgs(caseArgs)
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
				if world == nil {
					return nil, nil
				}
				return world(r)
			}
			defer func() { w.rt.extra = world }()
			byTool()
			return records() > 0
		}
		if !cut() {
			t.Errorf("%s: declared in everySendIsAChange, and the cut of its call in the world left no record — the world does not bring it to its write", c.path)
			forgetRun(w)
			done()
			continue
		}
		interrupted, named := parseObject(byTool())["interrupted"].(map[string]any)
		if !named {
			t.Errorf("%s: declared in everySendIsAChange, and its consented call after the cut does not ask again", c.path)
			forgetRun(w)
			done()
			continue
		}
		asked++
		ran[c.tool.Name] = true
		cmd := commandsByTool()[c.tool.Name]
		line := c.line
		if line == nil {
			line = parityCommandOf(t, cmd.group, cmd.cmd, c.tool, copyArgs(caseArgs)).tree
		}
		// run runs the command and returns its exit code and its writes.
		run := func(flags ...string) (int, []string) {
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
			fake.reset()
			newProcess()
			_, _, stderr := runCLI(t, c.stdin, append([]string{"--yes"}, line...)...)
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
		forgetRun(w)
		done()
	}
	forgetRun(w)
	if asked < 3 {
		t.Errorf("%d tools ask again after a cut call, want every tool whose send is one more change", asked)
	}
	// A declared tool with no case in the world would go unread in
	// silence: every tool of everySendIsAChange ran above.
	for _, name := range declared {
		if !ran[name] {
			t.Errorf("%s is declared in everySendIsAChange and no case of the world cut its call and was asked again: the rule of --yes is not held for it", name)
		}
	}
	t.Logf("consented calls probed: %d; asked again after a cut: %d", probed, asked)
}

// everySendDeclared are the tools of everySendIsAChange (internal/tools),
// read from its source: the list is not exported, and this test must not
// miss a tool the world does not reach.
func everySendDeclared(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join("..", "tools", "intent.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != "everySendIsAChange" || len(vs.Values) != 1 {
			return true
		}
		if lit, ok := vs.Values[0].(*ast.CompositeLit); ok {
			for _, e := range lit.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.BasicLit); ok {
						if name, err := strconv.Unquote(key.Value); err == nil {
							names = append(names, name)
						}
					}
				}
			}
		}
		return false
	})
	if len(names) < 3 {
		t.Fatalf("%d tools read from everySendIsAChange in internal/tools/intent.go: the list moved, update everySendDeclared", len(names))
	}
	sort.Strings(names)
	return names
}
