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

// vm_stop is a line of the table of power actions on the planner of
// vm_start (D99, docs/internal/briefs/ops-wave-1.md §4). Its table «state
// of the VM → the answer» (§4.1) has a world per row here; what the two
// tools share — the planner, the next step, the refusals of the platform —
// is held by the tests of vm_start, and the rows below hold that the line
// of vm_stop is the right one.

// A running VM: the preview names the command, the VM, the downtime and
// that nothing says the VM starts again — and no price as a number; nothing
// is written before the yes (axes 1 and 4; §4.2).
func TestVMStop_RunningAsksWithAPreviewThatNamesTheDowntime(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	m, text, _ := callNamed(t, r, "vm_stop", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("vm_stop of a running VM: %s", text)
	}
	p, _ := m["preview"].(map[string]any)
	if p["command"] != "power_off" || p["vm_id"] != w.id || p["vm_state"] != "running" {
		t.Errorf("the preview does not name the command and the VM: %s", text)
	}
	interrupts, _ := p["interrupts"].(string)
	if !strings.Contains(interrupts, "stop answering") || !strings.Contains(interrupts, "memory") {
		t.Errorf("the preview does not name the downtime and the loss of what is in memory: %q", interrupts)
	}
	if msg, _ := m["message"].(string); !strings.Contains(msg, "vm_start") || !strings.Contains(msg, "not checked") {
		t.Errorf("the message does not say that the way back is not promised: %q", msg)
	}
	classes, _ := p["price_classes"].([]any)
	if len(classes) != 1 || p["tariff_url"] == "" || p["cost"] != nil {
		t.Fatalf("the preview prices a stopped VM, or names no tariff: %s", text)
	}
	// Saving is why a VM is stopped: the preview does not promise one
	// («Риск», conclusion 132, У1) — no «free», and a plain word that the
	// charges may go on.
	note, _ := classes[0].(map[string]any)["note"].(string)
	if !strings.Contains(note, "may not stop the charges") || !strings.Contains(note, "do not count on a saving") {
		t.Errorf("the price note does not say that stopping may not save: %q", note)
	}
	for _, promise := range []string{"free", "no charge", "not billed", "бесплатн"} {
		if strings.Contains(strings.ToLower(text), promise) {
			t.Errorf("the preview promises a saving (%q): %s", promise, text)
		}
	}
	if n := w.powerCalls(); n != 0 {
		t.Errorf("written before consent: %d power calls", n)
	}
}

// With the yes the command is sent once and the answer is the end of the
// transition.
func TestVMStop_ConfirmedStopsAndAnswersStopped(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	w.stepAfter = 3
	m, text, isErr := callNamed(t, r, "vm_stop", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "stopped" || m["state"] != "stopped" || m["vm_id"] != w.id {
		t.Fatalf("vm_stop with consent: %s", text)
	}
	if _, ok := m["elapsed_sec"]; !ok {
		t.Errorf("the answer has no elapsed_sec: %s", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want 1", n)
	}
}

// A repeat: the VM is stopped already — success with no question and no
// write (П3). And a stop in flight is waited for with no second command.
func TestVMStop_RepeatDoesNotWrite(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w := vmStartWorld(t, "stopped")
		m, text, isErr := callNamed(t, r, "vm_stop", map[string]any{"vm_id": w.id, "confirmed": confirmed})
		if isErr || m["status"] != "already_stopped" || w.powerCalls() != 0 {
			t.Errorf("stopped, confirmed=%v: %s (power calls %d)", confirmed, text, w.powerCalls())
		}

		r, w = vmStartWorld(t, "running")
		if err := w.SetVMPower(context.Background(), w.id, "power_off"); err != nil {
			t.Fatal(err)
		}
		w.stepAfter = 3
		m, text, isErr = callNamed(t, r, "vm_stop", map[string]any{"name": "web", "confirmed": confirmed})
		if isErr || m["status"] != "stopped" || w.powerCalls() != 1 {
			t.Errorf("stopping, confirmed=%v: %s (power calls %d, want the 1 of the world's own setup)", confirmed, text, w.powerCalls())
		}
	}
}

// The rest of the table: the states of another change settle or are
// refused, a VM with nothing to stop is refused with a read as the hint, a
// state evo does not know is asked about with a warning, and a VM that is
// not there is not found — each before the question and with no write.
func TestVMStop_TheRestOfTheTable(t *testing.T) {
	for _, state := range []string{"starting", "rebooting", "creating", "updating"} {
		r, w := vmStartWorld(t, state)
		m, text, isErr := callNamed(t, r, "vm_stop", map[string]any{"name": "web", "confirmed": true})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) || w.powerCalls() != 0 {
			t.Errorf("%s that does not settle: %s", state, text)
		}
	}
	r, w := vmStartWorld(t, "starting")
	if err := func() error {
		w.setVMState(w.id, "stopped")
		return w.SetVMPower(context.Background(), w.id, "power_on")
	}(); err != nil {
		t.Fatal(err)
	}
	w.stepAfter = 2
	if m, text, _ := callNamed(t, r, "vm_stop", map[string]any{"name": "web"}); m["status"] != "confirmation_required" {
		t.Errorf("starting that settled in running: %s", text)
	}
	for _, state := range []string{"deleting", "error_deleting", "error_creating"} {
		r, w := vmStartWorld(t, state)
		m, text, isErr := callNamed(t, r, "vm_stop", map[string]any{"name": "web", "confirmed": true})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) || w.powerCalls() != 0 {
			t.Errorf("%s: %s", state, text)
		}
		if strings.Contains(text, "destroy") || strings.Contains(text, "delete") {
			t.Errorf("%s: the refusal names a delete as a step: %s", state, text)
		}
	}
	for _, state := range []string{"error", "paused"} {
		r, w := vmStartWorld(t, state)
		m, text, _ := callNamed(t, r, "vm_stop", map[string]any{"name": "web"})
		if warning, _ := m["warning"].(string); m["status"] != "confirmation_required" || !strings.Contains(warning, state) || w.powerCalls() != 0 {
			t.Errorf("%s: %s", state, text)
		}
	}
	r, w = vmStartWorld(t, "running")
	if m, text, isErr := callNamed(t, r, "vm_stop", map[string]any{"vm_id": faultID(0x7f), "confirmed": true}); !isErr || m["reason"] != clerrors.CodeVMNotFound || w.powerCalls() != 0 {
		t.Errorf("no such VM: %s", text)
	}
}

// At the limit of an MCP call the answer is the transient state with the
// same call, without the consent, as the next step; the step writes
// nothing, and a stop that fell back between the calls — the VM runs
// again — asks again instead of stopping it on the old consent (R-85).
func TestVMStop_NextStepDoesNotWriteAndAFallBackAsksAgain(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "running", 0)
	budgeted(r)
	m, text, isErr := callNamed(t, r, "vm_stop", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "stopping" || m["next_tool"] != "vm_stop" {
		t.Fatalf("vm_stop whose VM stays stopping past the limit: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	if next["vm_id"] != w.id || next["confirmed"] != nil {
		t.Fatalf("next_args: %v, want the VM by its id and no consent", next)
	}
	if _, text, _ = callNamed(t, r, "vm_stop", next); w.powerCalls() != 1 {
		t.Errorf("the next step on a VM still stopping wrote: %s", text)
	}
	w.stepVMPower(w.id, "running")
	m, text, _ = callNamed(t, r, "vm_stop", next)
	if m["status"] != "confirmation_required" || w.powerCalls() != 1 {
		t.Errorf("the step on a VM that runs again does not ask: %s (power calls %d)", text, w.powerCalls())
	}
}

// A stop that began and fell back within the call is an error with no next
// step, as a start's (conclusion 123, К2).
func TestVMStop_StopThatFallsBackIsAnError(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	w.stepAfter, w.stepTo = 3, []string{"running"}
	m, text, isErr := callNamed(t, r, "vm_stop", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "running" || m["next_tool"] != nil {
		t.Fatalf("vm_stop whose VM fell back to running: %s", text)
	}
}

// The VM of this folder's deployment: the preview says the deployment
// goes down with it (§4.3, В2); a VM the folder has no record of gets no
// such line.
func TestVMStop_PreviewNamesTheFoldersDeployment(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	m, _, _ := callNamed(t, r, "vm_stop", map[string]any{"name": "web"})
	if p, _ := m["preview"].(map[string]any); p["folder_deployment"] != nil {
		t.Fatalf("a VM the folder has no record of is called the folder's deployment: %v", p["folder_deployment"])
	}
	dir := t.TempDir()
	if err := state.Update(state.DefaultPath(dir), stateLockWait, func(sf *state.File) error {
		sf.UpsertDeployment(state.DeploymentRecord{ID: "d-1", VMID: w.id, VMName: "web", Status: "running"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !folderDeploymentOn(dir, w.id) || folderDeploymentOn(dir, faultID(0x7f)) {
		t.Fatal("folderDeploymentOn does not tell the folder's VM from another")
	}
}

// vm_stop takes the service down: a client asks the person every time
// (Decl.Destructive; §4.3, В1).
func TestVMStop_IsDestructiveForTheClient(t *testing.T) {
	r, _ := vmStartWorld(t, "running")
	tl, _ := r.Lookup("vm_stop")
	if !IsDestructive(tl) {
		t.Error("vm_stop is not in Decl.Destructive")
	}
	if start, _ := r.Lookup("vm_start"); IsDestructive(start) {
		t.Error("vm_start is in Decl.Destructive: it stops nothing")
	}
}
