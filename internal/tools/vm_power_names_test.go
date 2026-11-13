// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "testing"

// A power tool takes a VM by its name strictly (D99, П2; strictVMRef): a
// name two VMs have is refused with both ids before the question. The rule
// is one map and one resolver, and its world is vm_start's
// (TestVMStart_NameOfTwoVMsIsRefusedWithBothIDs); this holds that every
// tool of the table of power actions is in the map — a line dropped from
// it would send the command to the first VM the list gives, and no test of
// that tool would fail (review of #1461).
func TestVMPower_EveryToolTakesANameStrictly(t *testing.T) {
	for tool := range vmPowerActions {
		if !strictVMRef[tool] {
			t.Errorf("%s is a power tool (vmPowerActions) and not in strictVMRef: a name of two VMs would be resolved to the first", tool)
		}
	}
	for tool := range strictVMRef {
		if _, ok := vmPowerActions[tool]; !ok && strictNotPower[tool] == "" {
			t.Errorf("strictVMRef names %s, which is not a power tool: say in this test why it takes a name strictly, or remove the line", tool)
		}
	}
}

// strictNotPower are the tools of strictVMRef that are not power tools,
// each with why it takes a name strictly.
var strictNotPower = map[string]string{
	"vm_resize": "a change of flavor of the first VM of a name two VMs have would bill a VM the caller did not mean (D99, П2)",
}
