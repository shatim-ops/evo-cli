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

// pendingCommands are the commands that call no tool yet but will: whoami
// and doctor become tools in PR 9 (REQ-11, owner's decision Q2). Path → the
// tool and the PR. The list only shrinks: an entry whose command is built
// from a CLISpec, or that is no command, fails TestParity_CommandsAndTools.
// Delete it once it is empty.
var pendingCommands = map[string]struct{ tool, until string }{
	"evo whoami": {"", "PR 9: станет инструментом whoami (REQ-11)"},
	"evo doctor": {"", "PR 9: станет инструментом doctor (REQ-11)"},
}

// No tool without a command and no command without a tool
// (docs/internal/cli-mcp-parity/spec.md, REQ-2): every command of the CLI,
// hidden ones included, is built from a tool's CLISpec or is an engine
// command calling its tool (engineCommands), is a tree group, or is in
// localCommands (or, for now, pendingCommands); every tool with a CLISpec
// has exactly one command, at the path its CLISpec names, plus the aliases
// engineCommands lists with the reason; a tool with None has none, and
// every other tool is in tools.CLIPending, which names no tool with a
// command.
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
	for path, p := range pendingCommands {
		if _, both := local[path]; both {
			t.Errorf("%s is in both localCommands and pendingCommands", path)
		}
		if p.tool != "" {
			if _, ok := reg.Lookup(p.tool); !ok {
				t.Errorf("pendingCommands: %s names no tool %s", path, p.tool)
			}
		}
		if strings.TrimSpace(p.until) == "" {
			t.Errorf("pendingCommands: %s without the PR", path)
		}
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
			_, isPending := pendingCommands[path]
			if name, ok := sub.Annotations[toolAnnotation]; ok {
				if tool, alias := aliases[path]; alias {
					aliasSeen[path] = true
					if tool != name {
						t.Errorf("%s: alias of %s in engineCommands, but calls %s", path, tool, name)
					}
				} else {
					byTool[name] = append(byTool[name], path)
				}
				if isLocal || isPending {
					t.Errorf("%s calls the tool %s, but is listed as a command without one (localCommands/pendingCommands): drop the entry", path, name)
				}
				if sub.HasSubCommands() {
					t.Errorf("%s: a tool's command has subcommands", path)
				}
				continue
			}
			isGroup := c == root && groups[sub.Name()]
			switch {
			case isGroup && (isLocal || isPending):
				t.Errorf("%s is a tree group and listed in localCommands/pendingCommands", path)
			case isGroup, isLocal, isPending:
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
	for path := range pendingCommands {
		if !seen[path] {
			t.Errorf("pendingCommands: no command %s", path)
		}
	}
	for path := range aliases {
		if !aliasSeen[path] {
			t.Errorf("engineCommands: no alias %s calling its tool", path)
		}
	}

	withCommand, pending := 0, 0
	for _, tl := range reg.Tools() {
		paths := byTool[tl.Name]
		_, isPending := tools.CLIPending[tl.Name]
		switch {
		case tl.CLI.Verb != "":
			withCommand++
			if isPending {
				t.Errorf("%s has the command %s and is in tools.CLIPending: drop it from the list", tl.Name, wantPath[tl.Name])
			}
			if len(paths) != 1 || paths[0] != wantPath[tl.Name] {
				t.Errorf("%s: commands %v, want exactly [%s]", tl.Name, paths, wantPath[tl.Name])
			}
		case tl.CLI.None != "":
			if len(paths) > 0 {
				t.Errorf("%s: CLISpec.None (%q), but commands %v", tl.Name, tl.CLI.None, paths)
			}
		case isPending:
			pending++
			if len(paths) > 0 {
				t.Errorf("%s is in tools.CLIPending, but has the commands %v: drop it from the list", tl.Name, paths)
			}
		default:
			t.Errorf("%s: no CLISpec and not in tools.CLIPending: give it a command", tl.Name)
		}
		delete(byTool, tl.Name)
	}
	for name, paths := range byTool {
		t.Errorf("%v: annotated with %s, which is no tool", paths, name)
	}
	t.Logf("tools: %d with a command, %d pending; local commands: %d, pending commands: %d",
		withCommand, pending, len(localCommands), len(pendingCommands))
}
