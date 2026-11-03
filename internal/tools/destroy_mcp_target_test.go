// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// P2 (agent runs of Grok on 1.6.0, G2-3; release plan 1.7, owner's
// decision 31.10): over MCP, destroy with confirmed and no target —
// neither vm_id nor name — deletes nothing, even when the folder of the
// server holds one record: it is USAGE «name the target from the preview».
// The preview without a target stays as it was: it names the record.
// A call with no live caller (MCP; `evo tools call destroy --yes`) is the
// one the rule holds for (H15 in docs/mcp.md).
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

	// A preview is confirmation_required, which answers isError like every
	// gate's preview (gate.go).
	preview, _ := r.handlers["destroy"](context.Background(), map[string]any{})
	if faultJSON(t, preview)["status"] != "confirmation_required" || !strings.Contains(preview, vmID) {
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

// The VM goes between the list and the DELETE (DELETE in the project is
// 404): already deleted, as the engine's destroy takes it, and the rest is
// cleaned up — the floating IP released, the record dropped (D26). P2 sends
// MCP to this path for the deployment 'evo destroy' deletes; it answered
// VM_NOT_FOUND there while 'evo destroy' answered success
// (TestParity_ErrorReasonEqualsTool, HTTP 404).
func TestDestroy_TargetGoneAtTheDeleteIsAlreadyDeleted(t *testing.T) {
	cloud := &targetCloud{deleteErr: clerrors.New(clerrors.CodeVMNotFound, "resource not found", "")}
	r, dir := targetRegistry(t, cloud)
	m, text, isErr := callNamed(t, r, "destroy", map[string]any{"vm_id": "0000a1a1-0000-4000-8000-000000000001", "confirmed": true})
	if isErr || m["status"] != "already_deleted" {
		t.Fatalf("want already_deleted: %s", text)
	}
	if cloud.releasedFIP != "fip-1" {
		t.Errorf("the floating IP stays billed: released %q", cloud.releasedFIP)
	}
	sf, err := state.Read(state.DefaultPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if a := sf.ActiveDeployment(); a != nil {
		t.Errorf("record left: %+v", a)
	}
}
