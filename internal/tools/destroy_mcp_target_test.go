// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"
)

// P2 (agent runs of Grok on 1.6.0, G2-3; release plan 1.7, owner's
// decision 31.10): over MCP, destroy with confirmed and no target —
// neither vm_id nor name — deletes nothing, even when the folder of the
// server holds one record: it is USAGE «name the target from the preview».
// The preview without a target stays as it was: it names the record.
// A call with no live caller (MCP; `evo tools call destroy --yes`) is the
// one the rule holds for.
func TestDestroy_ConfirmedWithoutTargetOverMCPIsUsage(t *testing.T) {
	cloud := &faultCloud{}
	r, _ := faultToolsRegistry(t, cloud)
	if text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true}); isErr {
		t.Fatalf("provision: %s", text)
	}
	if n := len(cloud.liveVMs()); n != 1 {
		t.Fatalf("the world holds %d VMs before destroy, want 1", n)
	}
	vmID := cloud.liveVMs()[0].ID

	preview, isErr := r.handlers["destroy"](context.Background(), map[string]any{})
	if isErr || !strings.Contains(preview, vmID) {
		t.Errorf("the preview without a target names the record's VM %s, as before:\n%s", vmID, preview)
	}

	text, isErr := r.handlers["destroy"](context.Background(), map[string]any{"confirmed": true})
	m := faultJSON(t, text)
	if !isErr || m["reason"] != "USAGE" {
		t.Errorf("destroy confirmed without vm_id or name over MCP: want an error with reason USAGE, got isErr=%v reason=%v:\n%s", isErr, m["reason"], text)
	}
	if n := len(cloud.liveVMs()); n != 1 {
		t.Errorf("destroy without a target deleted the VM: %d live VMs, want 1", n)
	}
	for _, c := range cloud.callLog() {
		if strings.HasPrefix(c, "DeleteVM") || strings.HasPrefix(c, "DeleteFloatingIP") {
			t.Errorf("destroy without a target reached the cloud's delete: %s", c)
		}
	}
	if !strings.Contains(text, vmID) {
		t.Errorf("the refusal names the target to pass (vm_id %s from the preview):\n%s", vmID, text)
	}

	// With the target named, the same call deletes.
	text, isErr = r.handlers["destroy"](context.Background(), map[string]any{"vm_id": vmID, "confirmed": true})
	if isErr {
		t.Errorf("destroy with vm_id: %s", text)
	}
}
