// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// External review of 11.11 (GPT on 71afe86): the --help example
// «evo stack status <job-id> --wait-seconds 240» was refused by the command
// it shows — wait_seconds is at most 20. TestCommandExamples_AreValid
// checked that each flag exists, not its value. A value an example gives a
// flag of a tool's command passes the check the call makes (tools.CheckArgs:
// type, minimum and maximum, pattern, enum): a copied example is not
// refused. A placeholder (<job-id>) is a value to fill in and is not
// checked.
func TestCommandExamples_ValuesPassTheToolsCheck(t *testing.T) {
	reg := tools.New(nil)
	root := newRootCmd()
	checked := 0
	for _, g := range allTreeGroups() {
		for _, c := range g.Cmds {
			path := "evo " + g.Name + " " + c.Verb
			ex := commandExamples[path]
			if ex == "" {
				continue
			}
			tool, ok := reg.Lookup(c.Tool)
			if !ok {
				t.Errorf("%s: no tool %s", path, c.Tool)
				continue
			}
			props, _ := tool.InputSchema["properties"].(map[string]any)
			for _, line := range strings.Split(ex, "\n") {
				f := strings.Fields(line)
				i := len(f) - 1
				for i >= 0 && f[i] != "evo" {
					i--
				}
				if i < 0 {
					continue
				}
				cmd, rest, err := root.Find(f[i+1:])
				if err != nil || cmd.CommandPath() != path {
					continue
				}
				for j := 0; j < len(rest); j++ {
					a := rest[j]
					if !strings.HasPrefix(a, "--") {
						continue
					}
					name, val, hasVal := strings.Cut(strings.TrimPrefix(a, "--"), "=")
					fl := cmd.Flags().Lookup(name)
					if fl == nil || fl.Value.Type() == "bool" {
						continue
					}
					if !hasVal && j+1 < len(rest) {
						j++
						val = rest[j]
					}
					if examplePlaceholder.MatchString(val) || strings.HasPrefix(val, "$") {
						continue
					}
					key := strings.ReplaceAll(name, "-", "_")
					p, _ := props[key].(map[string]any)
					if p == nil {
						continue
					}
					v, ok := exampleValue(p, val)
					if !ok {
						t.Errorf("%q: --%s %q is not a %v", strings.TrimSpace(line), name, val, p["type"])
						continue
					}
					checked++
					if err := tools.CheckArgs(tool, map[string]any{key: v}); err != nil {
						t.Errorf("%q: --%s %s is refused by %s: %v", strings.TrimSpace(line), name, val, tool.Name, err)
					}
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no value of an example was checked: the scan sees nothing")
	}
}

// exampleValue is val as the tool's argument of schema p.
func exampleValue(p map[string]any, val string) (any, bool) {
	switch p["type"] {
	case "integer", "number":
		n, err := strconv.ParseFloat(val, 64)
		return n, err == nil
	case "boolean":
		b, err := strconv.ParseBool(val)
		return b, err == nil
	case "array":
		var out []any
		for _, s := range strings.Split(val, ",") {
			out = append(out, s)
		}
		return out, true
	}
	return val, true
}
