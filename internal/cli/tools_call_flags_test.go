// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

// toolsCallFlagBridges are the flags of 'evo tools call' (its own and the
// global ones) that share the name of an argument of a tool's schema, and
// how the flag reaches that argument: "tool.arg" → how. FB170-4 (#1034):
// the flag --wait of tools call was its own, and refused the tools whose
// schema has an argument wait — evo vm create --wait=false worked, evo
// tools call provision --wait=false did not.
var toolsCallFlagBridges = map[string]string{
	"provision.region": "--region is the profile's zone (Config.Region), the default the argument region takes",
	"deploy.region":    "--region is the profile's zone (Config.Region), the default the argument region takes",
}

// toolsCallFlagMisses are the names a flag of tools call shares with an
// argument the flag does not reach: "tool.arg" → what the flag does instead.
// It only shrinks. Closes by 1.10 (release plan 1.9, «Сроки храповиков «к
// 1.7–1.9»», #1590 question 19 (c)); release plan 1.8, «инвариант флагов
// tools call», Process · 1.8 (2); the fix is the development's).
var toolsCallFlagMisses = map[string]string{
	"s3_bucket_create.region": "the argument is the S3 region (ru-central-1); --region sets the profile's zone, and the bucket is made in the default S3 region",
}

// FB170-4, the class (release plan 1.8): an argument of a tool's schema
// that shares its name with a flag of 'evo tools call' either gets the
// flag's value — the wait of applyWait (a boolean wait), a line of
// toolsCallFlagBridges — or is a line of toolsCallFlagMisses. A new
// argument named like a flag (yes, json, dry-run, profile, project-id…)
// fails here: the caller who writes --<name> must reach the argument, or
// the collision is known.
func TestToolsCall_FlagsDoNotShadowSchemaArgs(t *testing.T) {
	root := newRootCmd()
	call, _, err := root.Find([]string{"tools", "call"})
	if err != nil || call.Name() != "call" {
		t.Fatalf("evo tools call: %v", err)
	}
	flags := map[string]bool{}
	call.InheritedFlags().VisitAll(func(f *pflag.Flag) { flags[f.Name] = true })
	call.LocalFlags().VisitAll(func(f *pflag.Flag) { flags[f.Name] = true })
	if !flags["wait"] || !flags["yes"] || !flags["region"] {
		t.Fatalf("flags of tools call: %v — the scan sees nothing", flags)
	}
	seen := map[string]bool{}
	var bad []string
	for _, tl := range newRegistry().Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name, p := range props {
			if !flags[name] {
				continue
			}
			key := tl.Name + "." + name
			seen[key] = true
			if pm, _ := p.(map[string]any); name == "wait" && pm["type"] == "boolean" {
				// applyWait: --wait=false is the tool's wait=false (#1034).
				args := map[string]any{}
				if err := applyWait(tl, false, args); err != nil || args["wait"] != false {
					bad = append(bad, fmt.Sprintf("%s: evo tools call %s --wait=false does not reach the argument wait: %v, args %v", key, tl.Name, err, args))
				}
				continue
			}
			if toolsCallFlagBridges[key] == "" && toolsCallFlagMisses[key] == "" {
				bad = append(bad, key+": the argument shares its name with the flag --"+name+" of evo tools call; make the flag reach it (as applyWait does for wait) and add it to toolsCallFlagBridges, or name it otherwise")
			}
		}
	}
	for _, m := range []map[string]string{toolsCallFlagBridges, toolsCallFlagMisses} {
		for key := range m {
			if !seen[key] {
				bad = append(bad, key+": no such argument named like a flag of tools call any more; take the line out")
			}
		}
	}
	for key := range toolsCallFlagBridges {
		if toolsCallFlagMisses[key] != "" {
			bad = append(bad, key+": both a bridge and a miss")
		}
	}
	sort.Strings(bad)
	if len(bad) > 0 {
		t.Error(strings.Join(bad, "\n"))
	}
	t.Logf("flags of tools call shared with schema arguments: %d (misses: %d)", len(seen), len(toolsCallFlagMisses))
}
