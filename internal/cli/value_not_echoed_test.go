// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// evo help <topic>, evo mcp --http <address> and evo tools call|describe
// <tool> do not repeat what was typed: a secret in the wrong place (#2353).
// help still prints the usage and exits 0, as cobra's did.
func TestHelpAndMCP_DoNotRepeatTheValue(t *testing.T) {
	setupCLI(t)
	const secret = "s3cr3tCLI27183650904512"
	for _, c := range []struct {
		args []string
		code int
	}{
		{[]string{"help", secret}, 0},
		{[]string{"help", "vm", secret}, 0},
		{[]string{"mcp", "--http", secret}, 1},
		{[]string{"--json", "mcp", "--http", secret}, 1},
		{[]string{"tools", "call", secret}, 1},
		{[]string{"--json", "tools", "call", secret}, 1},
		{[]string{"tools", "describe", secret}, 1},
	} {
		newProcess()
		code, out, errOut := runCLI(t, "", c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d: %s%s", c.args, code, c.code, out, errOut)
		}
		if strings.Contains(out+errOut, "s3cr3t") {
			t.Errorf("%v: repeats the value: %s%s", c.args, out, errOut)
		}
	}
}
