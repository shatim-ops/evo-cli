// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Every tool says how the CLI shows it: a command (Group+Verb) or no
// command with a reason (None), exactly one of the two.
// A command is unique, and every name it uses is an argument of the tool's
// schema (docs/internal/cli-mcp-parity/spec.md, REQ-1).
func TestRegistry_EveryToolHasCLISpec(t *testing.T) {
	r := newTestServer()
	commands := map[string]string{} // "group verb" → tool
	described := 0
	for _, tl := range r.Tools() {
		spec := tl.CLI
		switch {
		case spec.Verb == "" && spec.None == "":
			t.Errorf("%s: no CLISpec (cliSpecs) and no None reason: give it a command", tl.Name)
			continue
		case spec.Verb != "" && spec.None != "":
			t.Errorf("%s: both a command (%q %q) and None (%q)", tl.Name, spec.Group, spec.Verb, spec.None)
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
		positional := map[string]bool{}
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
				positional[n] = true
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
		// A secret has its value flag and the flag of its file: a string
		// argument with a flag of its own.
		for n := range spec.SecretFiles {
			isArg("SecretFiles key", n)
			if p, _ := props[n].(map[string]any); p != nil && p["type"] != "string" {
				t.Errorf("%s (evo %s): SecretFiles key %q is a %v, not a string", tl.Name, path, n, p["type"])
			}
			if positional[n] || spec.Omit[n] || spec.Pairs[n] || spec.Files[n] {
				t.Errorf("%s (evo %s): SecretFiles key %q is positional, in Omit, Pairs or Files: it needs a flag of its own", tl.Name, path, n)
			}
		}
	}
	for name := range cliSpecs {
		if _, ok := r.Lookup(name); !ok {
			t.Errorf("cliSpecs: no tool %s", name)
		}
	}
	if described != len(r.Tools()) {
		t.Errorf("%d tools with a command != %d tools", described, len(r.Tools()))
	}
}
