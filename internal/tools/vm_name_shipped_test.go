// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// shippedVMRefSecond is the second VM of the world named as the first.
const shippedVMRefSecond = "00000000-0000-4000-8000-0000000000e7"

// shippedVMRefCalls are the released tools that take a VM by its name and
// resolve it at the one point (resolveVMFor): tool → its call with the VM
// named ref. A released tool that begins to call the point gets a line; a
// tool of strictVMRef has none — its answers are its own tests'.
var shippedVMRefCalls = map[string]func(ref string) map[string]any{
	"disk_attach": func(ref string) map[string]any { return map[string]any{"disk_id": faultDisk, "vm": ref} },
	"disk_detach": func(ref string) map[string]any { return map[string]any{"disk_id": faultDisk, "vm": ref} },
	"disk_create": func(ref string) map[string]any {
		return map[string]any{"name": "data2", "size_gb": float64(10), "attach_to": ref}
	},
	"fip_attach": func(ref string) map[string]any { return map[string]any{"fip": "203.0.113.10", "vm": ref} },
	"vm_get":     func(ref string) map[string]any { return map[string]any{"vm_id": ref} },
}

// shippedVMRefPreviewHasNoID are the released tools whose preview names
// the VM only as the call named it, not by the id it was resolved to: with
// two VMs of a name the preview does not tell which one the write goes to.
// For them the test sees only that the call is not refused. "tool" → what
// the preview has. A finding for «разработка» (CLAUDE.md §6, axis 4), not
// a miss of this test: the line goes when the preview names the id.
var shippedVMRefPreviewHasNoID = map[string]string{
	"disk_create": "the preview names attach_to as given",
	"fip_attach":  "will_attach.vm is the name as given",
}

// The reference to a VM is resolved at one point, with the strictness
// declared per tool (strictVMRef, D99): the tools of the first wave refuse
// a name two VMs have and answer VM_NOT_FOUND; the released ones keep the
// answer they shipped with — the first VM of the name, and NOT_FOUND with
// no hint for a name no VM has. Making the point strict for every caller
// changes the answer and the exit code of five released tools, which is
// breaking, and no test said so: the audit of the standard vm_start found
// that the mutation strict := true left the build green («1.9 — релиз»,
// 12.11). Whether the released tools become strict is the owner's
// decision; until it is made, their answers are these.
func TestVMRef_ReleasedToolsKeepTheirAnswer(t *testing.T) {
	for tool, args := range shippedVMRefCalls {
		if strictVMRef[tool] {
			t.Errorf("%s is strict (strictVMRef) and is listed as a released tool that keeps its answer: one of the two", tool)
			continue
		}
		world := func() *Registry {
			disk := map[string]any{"id": faultDisk, "name": "data", "size": float64(20), "state": "available"}
			if tool == "disk_detach" {
				disk["state"] = "in_use"
				disk["vms"] = []any{map[string]any{"id": agentDiskVM, "name": "web", "primary": false}}
			}
			r, c := agentDiskWorld(t, disk)
			c.faultCloud.mu.Lock()
			c.vms = append(c.vms, faultVM{ID: shippedVMRefSecond, Name: "web", Status: "running"})
			c.fips = append(c.fips, faultFIP{ID: faultID(0x71), IP: "203.0.113.10"})
			c.faultCloud.mu.Unlock()
			return r
		}

		// A name two VMs have: the first one, as before — no refusal, and
		// the second VM is not in the answer.
		m, text, _ := callNamed(t, world(), tool, args("web"))
		switch {
		case m["status"] == "error":
			t.Errorf("%s by a name two VMs have: refused (%v) — a released tool takes the first VM of the name; refusing is breaking\n%s", tool, m["reason"], text)
		case shippedVMRefPreviewHasNoID[tool] != "":
			if strings.Contains(text, agentDiskVM) {
				t.Errorf("shippedVMRefPreviewHasNoID names %s, whose answer names the VM's id now: remove the line", tool)
			}
		case !strings.Contains(text, agentDiskVM):
			t.Errorf("%s by a name two VMs have: the answer does not name the first VM %s\n%s", tool, agentDiskVM, text)
		case strings.Contains(text, shippedVMRefSecond):
			t.Errorf("%s by a name two VMs have: the answer names the second VM %s\n%s", tool, shippedVMRefSecond, text)
		}

		// A name no VM has: NOT_FOUND, as before — not VM_NOT_FOUND.
		m, text, isErr := callNamed(t, world(), tool, args("nosuch"))
		if !isErr || m["reason"] != clerrors.CodeNotFound || !strings.Contains(text, `nosuch`) {
			t.Errorf("%s by a name no VM has: want NOT_FOUND naming it, as released\n%s", tool, text)
		}
	}
}
