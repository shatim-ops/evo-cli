// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// D81, the second half (#897, variant A; the probe of shared disks, 2 of 2,
// CMP-27): Cloud.ru attaches a disk to one VM at a time — the attach to a
// second VM answered 422 «Disk can not be attached from current state»
// after consent. disk_attach of a disk another VM has is refused before
// the question, USAGE with that VM's id; disk_detach from a VM the disk is
// not attached to, the same («Риск», conclusion 9: one decision for attach
// and detach). Nothing is written.
func TestDiskAttachDetach_AnotherVMsDiskRefusedBeforeTheQuestion(t *testing.T) {
	other := faultID(0x52)
	disk := func() map[string]any {
		return map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "in_use",
			"vms": []any{map[string]any{"id": other, "name": "db", "primary": false}}}
	}
	for _, tool := range []string{"disk_attach", "disk_detach"} {
		for _, confirmed := range []bool{false, true} {
			r, c := agentDiskWorld(t, disk())
			m, text, isErr := callNamed(t, r, tool, map[string]any{"disk_id": faultDisk, "vm": "web", "confirmed": confirmed})
			if !isErr || m["reason"] != clerrors.CodeUsage || m["status"] == "confirmation_required" {
				t.Errorf("%s confirmed=%v: want USAGE before the question: %s", tool, confirmed, text)
			}
			if !strings.Contains(text, other) {
				t.Errorf("%s confirmed=%v: the refusal does not name the other VM's id: %s", tool, confirmed, text)
			}
			if len(c.writes) != 0 {
				t.Errorf("%s confirmed=%v: written %v", tool, confirmed, c.writes)
			}
		}
	}
}
