// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// The note of a VM left billed names commands a person or an agent copies:
// the id goes into them only in the form of an id, and the state the cloud
// sent is quoted, never a part of the command («Риск», conclusion 291-2).
func TestVMLeftNote_CommandsTakeOnlyAnIDOfItsForm(t *testing.T) {
	const id = "00000000-0000-4000-8000-0000000000a1"
	note := vmLeftNote(id, "error; rm -rf ~\x1b[2J")
	if !strings.Contains(note, "evo vm delete "+id) || !strings.Contains(note, "evo vm get "+id) {
		t.Errorf("an id of its form goes into the commands: %s", note)
	}
	if !strings.Contains(note, `"error; rm -rf ~\x1b[2J"`) {
		t.Errorf("the state the cloud sent is quoted: %s", note)
	}
	for _, bad := range []string{"x; rm -rf ~", "a b", "$(id)", "x\ny"} {
		note := vmLeftNote(bad, "")
		if strings.Contains(note, "evo vm delete "+bad) || strings.Contains(note, "evo vm get "+bad) {
			t.Errorf("%q, not of the form of an id, goes into a command: %s", bad, note)
		}
		if !strings.Contains(note, "evo vm list") {
			t.Errorf("%q: the note names no way to find the VM: %s", bad, note)
		}
	}
}
