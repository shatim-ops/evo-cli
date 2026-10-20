// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Every example names a real command, and every --flag in it exists on the
// command it runs: a copied example must not fail (the guide copies them).
func TestCommandExamples_AreValid(t *testing.T) {
	root := newRootCmd()
	for path, ex := range commandExamples {
		cmd, _, err := root.Find(strings.Fields(path)[1:])
		if err != nil || cmd.CommandPath() != path {
			t.Errorf("example for unknown command %q", path)
			continue
		}
		if cmd.Example != ex {
			t.Errorf("%s: example not applied", path)
		}
		for _, line := range strings.Split(ex, "\n") {
			checkExampleLine(t, root, line)
		}
	}
}

// parityTrees are the trees the CLI ↔ MCP parity plan added (spec.md
// REQ-3, PR 3), parityVerbs the commands it added to the trees of v1.1
// ("group verb"): each has an example, and parity_newcmds_test.go runs it
// against its tool.
var (
	parityTrees = map[string]bool{"dns": true, "cert": true, "lb": true, "tg": true, "snat": true,
		"kafka": true, "redis": true, "ar": true, "ca": true}
	parityVerbs = map[string]bool{}
)

// Every command the parity plan added has an example in its --help
// (spec.md REQ-3): the commands of its trees and those it added to the
// trees of v1.1.
func TestCommandExamples_NewCommands(t *testing.T) {
	n := 0
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if !parityTrees[g.Name] && !parityVerbs[g.Name+" "+c.Verb] {
				continue
			}
			n++
			path := "evo " + g.Name + " " + c.Verb
			if strings.TrimSpace(commandExamples[path]) == "" {
				t.Errorf("%s (%s): no example in commandExamples", path, c.Tool)
			}
		}
	}
	if n == 0 {
		t.Fatal("no command of the parity trees")
	}
}

// examplePlaceholder is a value to fill in, <cluster-id>: its ">" is no
// redirection, and the flags after it are checked too.
var examplePlaceholder = regexp.MustCompile(`<[A-Za-z0-9_.-]+>`)

func checkExampleLine(t *testing.T, root *cobra.Command, line string) {
	t.Helper()
	line = examplePlaceholder.ReplaceAllString(line, "VALUE")
	// Only the evo part of a pipeline or a chain: "evo deploy --yes && evo verify".
	for _, part := range strings.FieldsFunc(line, func(r rune) bool { return r == '|' || r == '&' || r == '>' }) {
		f := strings.Fields(part)
		// The last "evo": in "claude mcp add … evo -- evo mcp" the first one
		// is the server's name.
		i := len(f) - 1
		for i >= 0 && f[i] != "evo" {
			i--
		}
		if i < 0 {
			continue
		}
		cmd, rest, err := root.Find(f[i+1:])
		if err != nil {
			t.Errorf("%q: %v", line, err)
			continue
		}
		for _, a := range rest {
			if !strings.HasPrefix(a, "--") {
				continue
			}
			name := strings.TrimPrefix(strings.SplitN(a, "=", 2)[0], "--")
			if cmd.Flags().Lookup(name) == nil && cmd.InheritedFlags().Lookup(name) == nil && root.PersistentFlags().Lookup(name) == nil {
				t.Errorf("%q: %s has no flag --%s", line, cmd.CommandPath(), name)
			}
		}
	}
}

// The jq example of vm list reads the fields the vm list rows carry: the
// tree's columns name them (they are the keys of vm_list answers).
func TestCommandExamples_JQFieldsExist(t *testing.T) {
	cols := map[string]bool{}
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			if g.Name == "vm" && c.Verb == "list" {
				for _, col := range c.Columns {
					cols[col] = true
				}
			}
		}
	}
	for _, f := range []string{"name", "state"} {
		if !strings.Contains(commandExamples["evo vm list"], "."+f) || !cols[f] {
			t.Errorf("vm list example reads .%s, columns %v", f, cols)
		}
	}
}
