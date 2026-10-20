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

// pendingCommands are the commands that call no tool yet but will: the
// engine commands with their own code become commands of their tools in
// PR 5 (spec.md REQ-4), whoami and doctor become tools in PR 9 (REQ-11,
// owner's decision Q2). Path → the tool and the PR. The list only shrinks:
// an entry whose command is built from a CLISpec, or that is no command,
// fails TestParity_CommandsAndTools. Delete it once it is empty.
var pendingCommands = map[string]struct{ tool, until string }{
	"evo deploy":       {"deploy", "PR 5: свой код → engine.Deploy"},
	"evo status":       {"status", "PR 5: свой код → engine.Status (withNames)"},
	"evo logs":         {"logs", "PR 5: свой код → engine.Logs (--follow)"},
	"evo destroy":      {"destroy", "PR 5: свой код → engine.Destroy; destroy с vm_id|name — evo vm delete"},
	"evo list-zones":   {"list_zones", "PR 5: свой код → Cloud.ListAvailabilityZones; дубль evo vm zones с массивом в --json"},
	"evo project list": {"org_projects", "PR 5: свой код listProjects → Organization API"},
	"evo list-images":  {"list_images", "PR 5: устаревшая, скрытая; свой код → engine.ListImages"},
	"evo whoami":       {"", "PR 9: станет инструментом whoami (REQ-11)"},
	"evo doctor":       {"", "PR 9: станет инструментом doctor (REQ-11)"},
}

// No tool without a command and no command without a tool
// (docs/internal/cli-mcp-parity/spec.md, REQ-2): every command of the CLI,
// hidden ones included, is built from a tool's CLISpec, is a tree group, or
// is in localCommands (or, for now, pendingCommands); every tool with a
// CLISpec has exactly one command, a tool with None has none, and every
// other tool is in tools.CLIPending, which names no tool with a command.
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
				byTool[name] = append(byTool[name], path)
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
