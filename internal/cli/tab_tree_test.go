// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// tabFileDefault are the commands whose Tab on an empty word falls back to
// the shell's files (cobra's ShellCompDirectiveDefault, or a filter of
// extensions or directories) though the registry declares no path there: the
// command's path → true. It only shrinks. Empty: its deadline 1.7 is met,
// and a new line comes with a deadline of its own and its plan line (release
// plan 1.7, block B, Tab, T1, #718: one point — no file completion unless
// the registry declares the argument a path).
var tabFileDefault = map[string]bool{}

// tabPathDeclared reports whether the argument at the cursor of an empty
// word — the first positional — of the command at path is one the registry
// declares a file (CLISpec.Files of its tool): there Tab offers the files.
func tabPathDeclared(reg *tools.Registry, path []string) bool {
	if len(path) != 2 {
		return false
	}
	for _, tl := range reg.Tools() {
		if tl.CLI.Group != path[0] || tl.CLI.Verb != path[1] || len(tl.CLI.Args) == 0 {
			continue
		}
		for _, n := range strings.Split(strings.TrimRight(strings.TrimSuffix(tl.CLI.Args[0], "..."), "?"), "|") {
			if tl.CLI.Files[n] {
				return true
			}
		}
	}
	return false
}

// tabOffersFiles reports whether a directive of cobra's completion lets the
// shell offer files: the default, or a filter of extensions or of
// directories.
func tabOffersFiles(directive string) bool {
	d, err := strconv.Atoi(strings.TrimPrefix(directive, ":"))
	if err != nil {
		return false
	}
	return d == int(cobra.ShellCompDirectiveDefault) ||
		d&int(cobra.ShellCompDirectiveFilterFileExt) != 0 || d&int(cobra.ShellCompDirectiveFilterDirs) != 0
}

// T1 (#718, the owner's dogfood of 31.10; process plan 1.7 §5): Tab on an
// empty word of every command of the tree gives candidates or says there
// are none (ShellCompDirectiveNoFileComp); the shell's files only where the
// registry declares the argument a path (CLISpec.Files of the positional),
// and there any form of them (default, a filter of extensions or of
// directories). A command that falls back to the files is a line of
// tabFileDefault; a line whose command no longer does fails too.
func TestTab_EveryCommandCompletesOrSaysNone(t *testing.T) {
	_, _ = withJSONWorld(t)
	var paths [][]string
	var walk func(c *cobra.Command, p []string)
	walk = func(c *cobra.Command, p []string) {
		for _, s := range c.Commands() {
			if s.Hidden || s.Name() == "help" || strings.HasPrefix(s.Name(), "__") {
				continue
			}
			q := append(append([]string{}, p...), s.Name())
			paths = append(paths, q)
			walk(s, q)
		}
	}
	walk(newRootCmd(), nil)
	if len(paths) < 100 {
		t.Fatalf("%d commands in the tree: the walk sees nothing", len(paths))
	}
	reg := newRegistry()
	seen := map[string]bool{}
	var bad []string
	for _, p := range paths {
		root := newRootCmd()
		root.SetArgs(append(append([]string{"__complete"}, p...), ""))
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		if err := root.Execute(); err != nil {
			t.Fatalf("__complete %v: %v", p, err)
		}
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		directive := lines[len(lines)-1]
		name := strings.Join(p, " ")
		seen[name] = true
		fileDefault := tabOffersFiles(directive)
		declared := tabPathDeclared(reg, p)
		switch {
		case declared && tabFileDefault[name]:
			bad = append(bad, "evo "+name+": its argument is declared a path (CLISpec.Files): files on Tab are right, take it out of tabFileDefault")
		case declared:
			// A path: the shell's files, filtered or not, are the answer.
		case fileDefault && !tabFileDefault[name]:
			bad = append(bad, "evo "+name+": Tab on an empty word offers the shell's files — complete the argument, or say there is nothing (ShellCompDirectiveNoFileComp)")
		case !fileDefault && tabFileDefault[name]:
			bad = append(bad, "evo "+name+": in tabFileDefault, but Tab no longer offers files; take the line out (the list only shrinks)")
		}
	}
	for name := range tabFileDefault {
		if !seen[name] {
			bad = append(bad, "evo "+name+": in tabFileDefault, no such command")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	t.Logf("commands: %d, falling back to files: %d", len(paths), len(tabFileDefault))
}

// The directives that offer files: the default and the filters; NoFileComp
// and a list of candidates do not.
func TestTab_DirectivesThatOfferFiles(t *testing.T) {
	for d, want := range map[string]bool{":0": true, ":8": true, ":16": true, ":4": false, ":2": false, "x": false} {
		if got := tabOffersFiles(d); got != want {
			t.Errorf("tabOffersFiles(%q) = %v, want %v", d, got, want)
		}
	}
}
