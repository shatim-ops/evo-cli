// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The delta audit of 1.8.2 (docs/internal/audits/delta-1.8.2.md, FB182-1…3;
// release plan 1.8.2, «Правки дельта-аудита 1.8.2 до тега»): what the help
// of a list names as a filter is a flag of its command, a boolean filter
// says how to give «no», and an empty filter is USAGE on both surfaces.

// filterCommand is a list of listSpecs and its command.
type filterCommand struct {
	tool tools.Tool
	cmd  *cobra.Command
	path []string
}

// filterCommands are the lists with filters and their commands, with the
// positional arguments of the command filled.
func filterCommands(t *testing.T) []filterCommand {
	t.Helper()
	root := newRootCmd()
	var out []filterCommand
	for _, tl := range tools.New(nil).Tools() {
		if tools.ListFilters(tl) == nil {
			continue
		}
		path := []string{tl.CLI.Verb}
		if tl.CLI.Group != "" {
			path = []string{tl.CLI.Group, tl.CLI.Verb}
		}
		cmd, _, err := root.Find(path)
		if err != nil || cmd == nil || cmd == root {
			t.Errorf("%s: no command %v", tl.Name, path)
			continue
		}
		// The positional arguments the command takes: a value each.
		for _, a := range tl.CLI.Args {
			if !strings.HasSuffix(a, "?") && !strings.HasSuffix(a, "...") {
				path = append(path, "11111111-1111-4111-8111-111111111111")
			}
		}
		out = append(out, filterCommand{tool: tl, cmd: cmd, path: path})
	}
	if len(out) < 20 {
		t.Fatalf("%d lists with filters: the rule reads few", len(out))
	}
	return out
}

var filtersLine = regexp.MustCompile(`(?:Filters|Отбор): ([^.]*)\.`)

// FB182-1 (a): «Отбор: --status, --name, --cluster-id» named a flag
// k8s_nodepools has not (cluster_id is its positional argument).
func TestListFilters_HelpNamesOnlyFlags(t *testing.T) {
	for _, fc := range filterCommands(t) {
		m := filtersLine.FindStringSubmatch(fc.cmd.Long)
		if m == nil {
			t.Errorf("%s: the help names no filters", fc.tool.Name)
			continue
		}
		for _, f := range strings.Split(m[1], ", ") {
			name := strings.TrimPrefix(f, "--")
			if fc.cmd.Flags().Lookup(name) == nil {
				t.Errorf("%s: the help names %s, which is not a flag of %q", fc.tool.Name, f, fc.cmd.CommandPath())
			}
		}
	}
}

// FB182-2: `--attached false` is refused; the help of a boolean filter
// names the form that gives «no».
func TestListFilters_BooleanFilterHelpNamesFalse(t *testing.T) {
	seen := 0
	for _, fc := range filterCommands(t) {
		props, _ := fc.tool.InputSchema["properties"].(map[string]any)
		for _, a := range tools.ListFilters(fc.tool) {
			p, _ := props[a].(map[string]any)
			if p["type"] != "boolean" {
				continue
			}
			seen++
			fl := fc.cmd.Flags().Lookup(strings.ReplaceAll(a, "_", "-"))
			if fl == nil {
				t.Errorf("%s: no flag of the filter %s", fc.tool.Name, a)
				continue
			}
			if want := "--" + fl.Name + "=false"; !strings.Contains(fl.Usage, want) {
				t.Errorf("%s: the help of --%s does not name %s: %q", fc.tool.Name, fl.Name, want, fl.Usage)
			}
		}
	}
	if seen < 4 {
		t.Fatalf("%d boolean filters, want public_ip, attached, bootable, free at least", seen)
	}
}

// FB182-2: the row of boolean filters in the guide names the form too.
func TestListFilters_GuideNamesFalse(t *testing.T) {
	b, err := os.ReadFile("../../docs/guide.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "| `--public-ip`") {
			if !strings.Contains(line, "=false") {
				t.Errorf("the guide's row of boolean filters does not name --x=false: %s", line)
			}
			return
		}
	}
	t.Fatal("docs/guide.md: no row of the boolean filters («| `--public-ip`…»)")
}

// FB182-3: an empty filter MCP refuses as USAGE the CLI refuses so too —
// `--name ""` gave the whole list while --zone "" was USAGE. That MCP says
// USAGE for every filter of listSpecs is TestListFilters_EmptyFilterIsUsage
// (internal/tools); here the CLI follows it. The other way round is not
// held: a released argument of the handler («zone» of image_list, the
// specs) is USAGE in the CLI alone (D89) until release plan 1.9.
func TestListFilters_EmptyFilterRefusedByMCPIsRefusedByTheCLI(t *testing.T) {
	reg := newRegistry()
	for _, fc := range filterCommands(t) {
		props, _ := fc.tool.InputSchema["properties"].(map[string]any)
		tl, _ := reg.Lookup(fc.tool.Name)
		for _, a := range tools.ListFilters(fc.tool) {
			p, _ := props[a].(map[string]any)
			if p["type"] != "string" {
				continue
			}
			fl := fc.cmd.Flags().Lookup(strings.ReplaceAll(a, "_", "-"))
			if fl == nil {
				continue // TestListFilters_HelpNamesOnlyFlags names it
			}
			args := append(append([]string{}, fc.path...), "--"+fl.Name, "")
			_, stderr, _ := execCLI(t, args...)
			cli := reasonOf(stderr)

			mcpArgs := map[string]any{a: ""}
			for i, pos := range fc.tool.CLI.Args {
				if !strings.HasSuffix(pos, "?") && !strings.HasSuffix(pos, "...") {
					mcpArgs[strings.SplitN(pos, "|", 2)[0]] = fc.path[len(fc.path)-len(fc.tool.CLI.Args)+i]
				}
			}
			text, _ := tl.Handler(context.Background(), mcpArgs)
			if mcp := reasonOf(text); mcp == "USAGE" && cli != "USAGE" {
				t.Errorf("%s with %s \"\": MCP USAGE, CLI %q — one rule for both", fc.tool.Name, a, cli)
			}
		}
	}
}

// reasonOf is the reason of an answer of evo: of a tool, its JSON
// envelope (reason); of the CLI under execCLI, the error's text, which
// begins with it («USAGE: …»). "" for an answer without one.
func reasonOf(s string) string {
	var v struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal([]byte(s), &v) == nil && v.Reason != "" {
		return v.Reason
	}
	if m := regexp.MustCompile(`(?m)^([A-Z][A-Z_]+): `).FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

// Release plan 1.9, Вп-10: a filter by several values (--ids, --states)
// refuses an empty value as MCP does — `--ids a,,b` and `--ids ""` are
// USAGE naming the empty value, not the list of «a, b» and not the whole list —
// and takes its values repeated or between commas, as one list.
func TestListFilters_ListFlagsRefuseAnEmptyValue(t *testing.T) {
	reg := newRegistry()
	seen := 0
	for _, fc := range filterCommands(t) {
		props, _ := fc.tool.InputSchema["properties"].(map[string]any)
		tl, _ := reg.Lookup(fc.tool.Name)
		for _, a := range tools.ListFilters(fc.tool) {
			p, _ := props[a].(map[string]any)
			if p["type"] != "array" {
				continue
			}
			fl := fc.cmd.Flags().Lookup(strings.ReplaceAll(a, "_", "-"))
			if fl == nil {
				t.Errorf("%s: filter %s is no flag of the command", fc.tool.Name, a)
				continue
			}
			seen++
			for _, v := range []string{"", "x,,y", " , "} {
				args := append(append([]string{}, fc.path...), "--"+fl.Name, v)
				_, stderr, _ := execCLI(t, args...)
				if reasonOf(stderr) != "USAGE" || !strings.Contains(stderr, a+"[") {
					t.Errorf("%s --%s %q: want USAGE naming the empty value, got %.300s", fc.tool.Name, fl.Name, v, stderr)
				}
			}
			mcpArgs := map[string]any{a: []any{"x", ""}}
			for i, pos := range fc.tool.CLI.Args {
				if !strings.HasSuffix(pos, "?") && !strings.HasSuffix(pos, "...") {
					mcpArgs[strings.SplitN(pos, "|", 2)[0]] = fc.path[len(fc.path)-len(fc.tool.CLI.Args)+i]
				}
			}
			if text, _ := tl.Handler(context.Background(), mcpArgs); reasonOf(text) != "USAGE" {
				t.Errorf("%s with %s [x, \"\"] over MCP: want USAGE, got %.200s", fc.tool.Name, a, text)
			}
		}
	}
	if seen < 30 {
		t.Fatalf("%d list flags of the filters: the rule reads few", seen)
	}
}
