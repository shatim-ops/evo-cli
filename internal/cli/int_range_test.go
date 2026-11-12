// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// An integer out of 32 bits is refused over the CLI as over MCP: USAGE that
// names the argument, in the words of the tool's own check — by a flag of a
// command and by --arg of evo tools call, whose digits too long for a
// 64-bit parser are an integer out of range as well («Риск», conclusion
// 171, У4). A flag of a command takes digits a 64-bit parser reads; what it
// cannot read is the flag's own USAGE, with no argument named.
func TestIntegerFlag_OutOfRangeRefusedAsTheToolDoes(t *testing.T) {
	outOfRange := func(args ...string) bool {
		t.Helper()
		_, errOut, code := execCLI(t, args...)
		return code != 0 && strings.Contains(errOut, "USAGE") &&
			(strings.Contains(errOut, "cpu") && (strings.Contains(errOut, "is out of range") || strings.Contains(errOut, "вне диапазона")))
	}
	for _, v := range []string{"2147483648", "99999999999"} {
		if !outOfRange("vm", "flavors", "--cpu", v) {
			t.Errorf("--cpu %s: not the tool's USAGE «out of range» naming cpu", v)
		}
	}
	for _, v := range []string{"99999999999", "99999999999999999999999"} {
		if !outOfRange("tools", "call", "list_flavors", "--arg", "cpu="+v) {
			t.Errorf("--arg cpu=%s: not the tool's USAGE «out of range» naming cpu", v)
		}
	}
	for _, v := range []string{"99999999999999999999999", "1e30"} {
		if _, errOut, code := execCLI(t, "vm", "flavors", "--cpu", v); code == 0 || !strings.Contains(errOut, v) {
			t.Errorf("--cpu %s: exit %d, %s; want the flag's refusal naming the value", v, code, errOut)
		}
	}
}
