// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Every tool says how the CLI shows it: a command (Group+Verb), no command
// with a reason (None), or not yet (CLIPending), exactly one of the three.
// A command is unique, and every name it uses is an argument of the tool's
// schema (docs/internal/cli-mcp-parity/spec.md, REQ-1).
func TestRegistry_EveryToolHasCLISpec(t *testing.T) {
	r := newTestServer()
	commands := map[string]string{} // "group verb" → tool
	described := 0
	for _, tl := range r.Tools() {
		spec := tl.CLI
		_, pending := CLIPending[tl.Name]
		kinds := 0
		if spec.Verb != "" {
			kinds++
		}
		if spec.None != "" {
			kinds++
		}
		if pending {
			kinds++
		}
		switch {
		case kinds == 0:
			t.Errorf("%s: no CLISpec (cliSpecs), no None reason and not in CLIPending: give it a command", tl.Name)
			continue
		case kinds > 1:
			t.Errorf("%s: more than one of a command (%q %q), None (%q) and CLIPending (%v); a tool with a command leaves CLIPending",
				tl.Name, spec.Group, spec.Verb, spec.None, pending)
			continue
		}
		if pending {
			if strings.TrimSpace(CLIPending[tl.Name]) == "" {
				t.Errorf("%s: CLIPending without the PR that gives it a command", tl.Name)
			}
			if spec.Group != "" || len(spec.Args) > 0 || len(spec.Rename) > 0 || len(spec.Omit) > 0 || len(spec.Pairs) > 0 ||
				len(spec.Files) > 0 || len(spec.Columns) > 0 || spec.Render != "" || spec.Raw {
				t.Errorf("%s: pending, but has a partial CLISpec %+v", tl.Name, spec)
			}
			continue
		}
		if spec.None != "" {
			if spec.Group != "" || len(spec.Args) > 0 {
				t.Errorf("%s: None (%q) with a command spec", tl.Name, spec.None)
			}
			continue
		}
		described++
		path := strings.TrimSpace(spec.Group + " " + spec.Verb)
		if other, dup := commands[path]; dup {
			t.Errorf("%s and %s: both are 'evo %s'", other, tl.Name, path)
		}
		commands[path] = tl.Name
		if strings.ContainsAny(spec.Verb, " _") || strings.ContainsAny(spec.Group, " _") {
			t.Errorf("%s: command 'evo %s': words are kebab-case", tl.Name, path)
		}

		props, _ := tl.InputSchema["properties"].(map[string]any)
		isArg := func(what, name string) {
			if _, ok := props[name]; !ok {
				t.Errorf("%s (evo %s): %s %q is not an argument of the schema", tl.Name, path, what, name)
			}
		}
		for i, a := range spec.Args {
			rest := strings.HasSuffix(a, "...")
			if rest && i != len(spec.Args)-1 {
				t.Errorf("%s: %q: only the last positional takes the rest", tl.Name, a)
			}
			a = strings.TrimSuffix(strings.TrimSuffix(a, "..."), "?")
			names := strings.Split(a, "|")
			if len(names) > 2 {
				t.Errorf("%s: %q: at most two names (id|name)", tl.Name, a)
			}
			for _, n := range names {
				isArg("positional", n)
			}
		}
		for n := range spec.Rename {
			isArg("Rename key", n)
		}
		for n := range spec.Omit {
			isArg("Omit key", n)
		}
		for n := range spec.Pairs {
			isArg("Pairs key", n)
		}
		for n := range spec.Files {
			isArg("Files key", n)
		}
	}
	for name := range cliSpecs {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("cliSpecs: no tool %s", name)
		}
	}
	for name := range CLIPending {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("CLIPending: no tool %s", name)
		}
	}
	if described+len(CLIPending) != len(r.Tools()) {
		t.Errorf("%d tools with a command + %d pending != %d tools", described, len(CLIPending), len(r.Tools()))
	}
}
