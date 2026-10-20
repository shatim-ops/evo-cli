// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// No tool without a command and no command without a tool
// (docs/internal/cli-mcp-parity/spec.md, REQ-2): every command of the CLI,
// hidden ones included, is built from a tool's CLISpec or is an engine
// command calling its tool (engineCommands), is a tree group, or is in
// localCommands; every tool has a CLISpec and exactly one command, at the
// path its CLISpec names, plus the aliases engineCommands lists with the
// reason (a tool with None would have none).
func TestParity_CommandsAndTools(t *testing.T) {
	reg := tools.New(nil)
	root := newRootCmd()
	root.InitDefaultHelpCmd()

	local := map[string]string{}
	for _, lc := range localCommands {
		if _, dup := local[lc.Path]; dup {
			t.Errorf("localCommands: %s twice", lc.Path)
		}
		if strings.TrimSpace(lc.Reason) == "" {
			t.Errorf("localCommands: %s without a reason", lc.Path)
		}
		local[lc.Path] = lc.Reason
	}

	aliases := map[string]string{} // path → tool
	for _, ec := range engineCommands {
		if ec.Alias != "" {
			aliases[ec.Path] = ec.Tool
		}
	}
	aliasSeen := map[string]bool{}

	groups := map[string]bool{}
	wantPath := map[string]string{} // tool → "evo group verb"
	for _, tl := range reg.Tools() {
		spec := tl.CLI
		if spec.Verb == "" {
			continue
		}
		groups[spec.Group] = true
		wantPath[tl.Name] = strings.Join(strings.Fields("evo "+spec.Group+" "+spec.Verb), " ") // Group "": a command at the root
		if spec.Render != "" && treeRenders[spec.Render] == nil {
			t.Errorf("%s: CLISpec.Render %q is no view of treeRenders", tl.Name, spec.Render)
		}
	}

	byTool := map[string][]string{}
	seen := map[string]bool{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			path := sub.CommandPath()
			seen[path] = true
			_, isLocal := local[path]
			if name, ok := sub.Annotations[toolAnnotation]; ok {
				if tool, alias := aliases[path]; alias {
					aliasSeen[path] = true
					if tool != name {
						t.Errorf("%s: alias of %s in engineCommands, but calls %s", path, tool, name)
					}
				} else {
					byTool[name] = append(byTool[name], path)
				}
				if isLocal {
					t.Errorf("%s calls the tool %s, but is listed as a command without one (localCommands): drop the entry", path, name)
				}
				if sub.HasSubCommands() {
					// evo deploy: an engine command with the commands of
					// other tools under it (spec.md R7); each of them is a
					// tool's command too.
					if _, engine := engineCommandOf(name); !engine || aliases[path] != "" {
						t.Errorf("%s: a tool's command has subcommands", path)
					}
					for _, c := range sub.Commands() {
						if _, ok := c.Annotations[toolAnnotation]; !ok && c.Name() != "help" {
							t.Errorf("%s: a subcommand of the engine command %s calls no tool", c.CommandPath(), path)
						}
					}
					walk(sub)
				}
				continue
			}
			isGroup := c == root && groups[sub.Name()]
			switch {
			case isGroup && isLocal:
				t.Errorf("%s is a tree group and listed in localCommands", path)
			case isGroup, isLocal:
			default:
				t.Errorf("%s calls no tool and is not in localCommands: build it from a tool's CLISpec, or list it with the reason", path)
			}
			walk(sub)
		}
	}
	walk(root)

	for path := range local {
		if !seen[path] {
			t.Errorf("localCommands: no command %s", path)
		}
	}
	for path := range aliases {
		if !aliasSeen[path] {
			t.Errorf("engineCommands: no alias %s calling its tool", path)
		}
	}

	withCommand := 0
	for _, tl := range reg.Tools() {
		paths := byTool[tl.Name]
		switch {
		case tl.CLI.Verb != "":
			withCommand++
			if len(paths) != 1 || paths[0] != wantPath[tl.Name] {
				t.Errorf("%s: commands %v, want exactly [%s]", tl.Name, paths, wantPath[tl.Name])
			}
		case tl.CLI.None != "":
			if len(paths) > 0 {
				t.Errorf("%s: CLISpec.None (%q), but commands %v", tl.Name, tl.CLI.None, paths)
			}
		default:
			t.Errorf("%s: no CLISpec: give it a command", tl.Name)
		}
		delete(byTool, tl.Name)
	}
	for name, paths := range byTool {
		t.Errorf("%v: annotated with %s, which is no tool", paths, name)
	}
	t.Logf("tools: %d with a command; local commands: %d; engine commands: %d", withCommand, len(localCommands), len(engineCommands))
}
