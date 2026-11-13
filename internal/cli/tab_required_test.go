// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"sort"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// tabRequiredMisses are the commands whose Tab on "-" does not put the
// required flags first: the tool → true. It only shrinks. Empty: its
// deadline 1.7 is met, and a new line comes with a deadline of its own and
// its plan line (release plan 1.7, block B, Tab, T2, #718).
var tabRequiredMisses = map[string]bool{}

// requiredFlags are the flags of the arguments a call of tl must have —
// the schema's required and the sets of RequiredOneOf — that are flags of
// its command, not positionals; confirmed is the consent (--yes).
func requiredFlags(tl tools.Tool) map[string]bool {
	pos := map[string]bool{}
	for _, a := range tl.CLI.Args {
		for _, n := range strings.Split(strings.TrimRight(strings.TrimSuffix(a, "..."), "?"), "|") {
			pos[n] = true
		}
	}
	args := map[string]bool{}
	if req, ok := tl.InputSchema["required"].([]string); ok {
		for _, a := range req {
			args[a] = true
		}
	}
	if req, ok := tl.InputSchema["required"].([]any); ok {
		for _, a := range req {
			if s, ok := a.(string); ok {
				args[s] = true
			}
		}
	}
	for _, set := range tl.RequiredOneOf {
		for _, a := range set {
			args[a] = true
		}
	}
	out := map[string]bool{}
	for a := range args {
		if pos[a] || a == "confirmed" || tl.CLI.Omit[a] {
			continue
		}
		flag := strings.ReplaceAll(a, "_", "-")
		if r, ok := tl.CLI.Rename[a]; ok {
			flag = r
		}
		out["--"+flag] = true
	}
	return out
}

// T2 (#718, the owner's dogfood of 31.10; process plan 1.7 §5): on a
// command of a tool, Tab on "-" names the flags the call needs first — the
// schema's required and the pairs of RequiredOneOf, as the registry
// declares them — before the flags of the root and the optional ones; a
// flag already given is not offered again.
func TestTab_RequiredFlagsFirst(t *testing.T) {
	_, _ = withJSONWorld(t)
	reg := newRegistry()
	checked := 0
	var bad []string
	for _, tl := range reg.Tools() {
		if tl.CLI.Group == "" || tl.CLI.Verb == "" {
			continue
		}
		req := requiredFlags(tl)
		if len(req) == 0 {
			continue
		}
		checked++
		root := newRootCmd()
		root.SetArgs([]string{"__complete", tl.CLI.Group, tl.CLI.Verb, "-"})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		if err := root.Execute(); err != nil {
			t.Fatalf("__complete %s %s -: %v", tl.CLI.Group, tl.CLI.Verb, err)
		}
		var flags []string
		for _, line := range strings.Split(out.String(), "\n") {
			name, _, _ := strings.Cut(line, "\t")
			if strings.HasPrefix(name, "--") {
				flags = append(flags, name)
			}
		}
		first := map[string]bool{}
		for i := 0; i < len(req) && i < len(flags); i++ {
			first[flags[i]] = true
		}
		ok := len(first) == len(req)
		for f := range req {
			ok = ok && first[f]
		}
		switch {
		case !ok && !tabRequiredMisses[tl.Name]:
			var want []string
			for f := range req {
				want = append(want, f)
			}
			sort.Strings(want)
			bad = append(bad, "evo "+tl.CLI.Group+" "+tl.CLI.Verb+" -<Tab>: the required "+strings.Join(want, ", ")+" are not first: "+strings.Join(flags[:min(len(flags), len(req)+2)], " "))
		case ok && tabRequiredMisses[tl.Name]:
			bad = append(bad, tl.Name+": in tabRequiredMisses, but its required flags come first now; take the line out (the list only shrinks)")
		}
	}
	if checked < 20 {
		t.Fatalf("%d commands with required flags: the test sees nothing", checked)
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Fatal(strings.Join(bad, "\n"))
	}
	t.Logf("commands with required flags: %d, not first: %d", checked, len(tabRequiredMisses))
}
