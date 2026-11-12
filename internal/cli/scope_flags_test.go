// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// scopeFlagUnmarked are the flags of the command tree that name where a
// call works and take an empty value: "command --flag" → why. It only
// shrinks, and a new command does not join it. Closes by 1.9 (release plan
// 1.9, «Пустая строка у zone и аргументов того же класса»; D89, D100).
var scopeFlagUnmarked = map[string]string{}

// A flag that names where a call works — the project, the region, the zone
// (scopeFlags) — refuses an empty value wherever a command declares it: an
// empty one moved the write to the project or the zone of the profile
// (D89; «Риск», conclusion 53). The rule is declared per flag set
// (noEmptyScope), so a new command that declares --zone and forgets the
// call takes "" silently. Every such flag of the tree is refused empty one
// of two ways: it carries the mark, or it is the flag of a tool's argument
// whose schema refuses the empty string (minLength, D100: project_id,
// region, zone, availability_zone_id) — the command checks its arguments
// by the tool's schema (tools.CheckArgs).
func TestScopeFlags_EveryOneRefusesAnEmptyValue(t *testing.T) {
	scope := map[string]bool{"availability-zone-id": true}
	for _, n := range scopeFlags {
		scope[n] = true
	}
	// bySchema are the scope arguments the schema of a command's tool
	// refuses empty: "group command" → the flags.
	bySchema := map[string]map[string]bool{}
	registry := newRegistry()
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			tl, ok := registry.Lookup(c.Tool)
			if !ok {
				continue
			}
			props, _ := tl.InputSchema["properties"].(map[string]any)
			path := strings.TrimSpace(g.Name + " " + c.Verb)
			for arg, p := range props {
				pm, _ := p.(map[string]any)
				if fmt.Sprint(pm["minLength"]) != "1" {
					continue
				}
				if bySchema[path] == nil {
					bySchema[path] = map[string]bool{}
				}
				flag := strings.ReplaceAll(arg, "_", "-")
				if renamed := c.Rename[arg]; renamed != "" {
					flag = renamed
				}
				bySchema[path][flag] = true
			}
		}
	}
	found := map[string]bool{}
	seen := 0
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		check := func(f *pflag.Flag) {
			if !scope[f.Name] {
				return
			}
			seen++
			path := strings.TrimPrefix(c.CommandPath(), "evo ")
			if f.Annotations[noEmptyValueAnnotation] == nil && !bySchema[path][f.Name] {
				found[path+" --"+f.Name] = true
			}
		}
		c.LocalFlags().VisitAll(check)
		c.PersistentFlags().VisitAll(check)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRootCmd())
	if seen < 10 {
		t.Fatalf("%d scope flags in the command tree: the walk sees nothing", seen)
	}
	keys := make([]string, 0, len(found))
	for k := range found {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if scopeFlagUnmarked[k] == "" {
			t.Errorf("%s takes an empty value: mark the flags of the command (noEmptyScope), or give the argument of its tool the rule of the schema (noEmptyValue) — an empty one would move the call to the profile's project or zone", k)
		}
	}
	for k := range scopeFlagUnmarked {
		if !found[k] {
			t.Errorf("scopeFlagUnmarked names %q, which is marked now or is no flag of the tree: remove the line", k)
		}
	}
	t.Logf("scope flags in the command tree: %d", seen)
}
