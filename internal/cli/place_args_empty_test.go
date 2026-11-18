// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// A place argument (Decl.Place) given empty to the CLI is USAGE naming it,
// as the tool answers by MCP: `--subnet-id "$VAR"` with an empty variable
// made the resource in the subnet of the default (D100; FB19I-37, #1597).
func TestPlaceArgs_EmptyFlagIsUsage(t *testing.T) {
	root := newRootCmd()
	checked := 0
	for _, tl := range tools.New(nil).Tools() {
		if len(tl.Decl.Place) == 0 {
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
		for _, a := range tl.CLI.Args {
			if !strings.HasSuffix(a, "?") && !strings.HasSuffix(a, "...") {
				path = append(path, "11111111-1111-4111-8111-111111111111")
			}
		}
		// The required arguments, given: the refusal is of the place
		// argument, not of one missing.
		props, _ := tl.InputSchema["properties"].(map[string]any)
		required, _ := tl.InputSchema["required"].([]string)
		for _, r := range required {
			if fl := cmd.Flags().Lookup(strings.ReplaceAll(r, "_", "-")); fl != nil {
				p, _ := props[r].(map[string]any)
				v := "11111111-1111-4111-8111-111111111111"
				switch {
				case p["type"] == "integer" || p["type"] == "number":
					v = "1"
				case r == "cidr" || r == "subnet":
					v = "10.0.0.0/16"
				}
				path = append(path, "--"+fl.Name, v)
			}
		}
		for _, arg := range tl.Decl.Place {
			flag := strings.ReplaceAll(arg, "_", "-")
			if cmd.Flags().Lookup(flag) == nil {
				t.Errorf("%s: no flag --%s for place argument %s", tl.Name, flag, arg)
				continue
			}
			checked++
			_, stderr, _ := execCLI(t, append(append([]string{}, path...), "--"+flag, "")...)
			if reasonOf(stderr) != "USAGE" || !(strings.Contains(stderr, arg) || strings.Contains(stderr, "--"+flag)) {
				t.Errorf("%s --%s \"\": want USAGE naming it: %s", tl.Name, flag, stderr)
			}
		}
	}
	if checked < 30 {
		t.Fatalf("checked %d place flags", checked)
	}
}
