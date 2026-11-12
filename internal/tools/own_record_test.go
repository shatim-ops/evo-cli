// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The answer of a call over MCP does not name the record the call itself
// left as what an earlier run left: it says how the call ended, and the
// CLI's answer has no such note. evo stands where its state is — the
// engine's directory is the working one, as in the product. The record is
// left out by its id, in that one answer only: the next answer of the
// server names it, and the same call again, with the consent of the first,
// is asked about and not sent («Риск», conclusion 172, У7–У9).
func TestIntent_ACallsOwnRecordIsNotInItsOwnAnswer(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	r := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: config.Config{ProjectID: "p", KeyID: "k", Secret: "s"}, StateDir: dir}, nil
	})
	reboot, _ := r.Lookup("vm_reboot")
	args := func() map[string]any { return map[string]any{"vm_id": resMine, "confirmed": true} }
	sent := 0
	call := r.budgeted(r.intended(reboot, func(context.Context, map[string]any) (string, bool) {
		sent++
		return `{"status":"error","error":"reboot: API_ERROR: 502","reason":"API_ERROR"}`, true
	}))

	text, _ := call(context.Background(), args())
	var cut map[string]any
	_ = json.Unmarshal([]byte(text), &cut)
	if _, named := cut["pending_operations"]; named || cut["reason"] != "API_ERROR" || sent != 1 {
		t.Fatalf("the answer of the cut call names its own record, or lost its reason: sent %d\n%s", sent, text)
	}
	if n := len(intent.Pending(intent.Dir(dir))); n != 1 {
		t.Fatalf("%d records after the cut call, want 1", n)
	}

	// Another call of the same server: its answer names the record.
	skill, _ := r.Lookup("agent_skill")
	other, _ := skill.Handler(context.Background(), map[string]any{"group": "vm"})
	if !strings.Contains(other, "pending_operations") || !strings.Contains(other, "vm_reboot") {
		t.Fatalf("the next answer does not name the record the cut call left:\n%s", other)
	}

	// The same call again, with the consent of the first: asked, not sent.
	again, _ := call(context.Background(), args())
	if !strings.Contains(again, `"confirmation_required"`) || sent != 1 {
		t.Fatalf("the same call after its cut one: sent %d\n%s", sent, again)
	}
}
