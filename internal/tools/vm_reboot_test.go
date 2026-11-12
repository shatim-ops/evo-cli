// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// vm_reboot is a line of the table of power actions whose from and to are
// one state (D99, docs/internal/briefs/ops-wave-1.md §5): every send is a
// change of its own. The tests below hold what follows from that — no
// «already», a next step that is a read, an end that counts only once the
// transition was seen; what the power tools share is held by the tests of
// vm_start.

// A running VM: the preview names the command, the VM and the downtime,
// says that a repeat is another reboot, and names no price — a reboot
// starts and stops no charge. Nothing is written before the yes.
func TestVMReboot_RunningAsksWithAPreviewThatNamesTheDowntime(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	m, text, _ := callNamed(t, r, "vm_reboot", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" {
		t.Fatalf("vm_reboot of a running VM: %s", text)
	}
	p, _ := m["preview"].(map[string]any)
	if p["command"] != "reboot" || p["vm_id"] != w.id || p["vm_state"] != "running" {
		t.Errorf("the preview does not name the command and the VM: %s", text)
	}
	if interrupts, _ := p["interrupts"].(string); !strings.Contains(interrupts, "do not answer") || !strings.Contains(interrupts, "memory") {
		t.Errorf("the preview does not name the downtime and the loss of what is in memory: %q", interrupts)
	}
	if msg, _ := m["message"].(string); !strings.Contains(msg, "every call is a new reboot") {
		t.Errorf("the message does not say that a repeat reboots again: %q", msg)
	}
	if p["price_classes"] != nil || p["tariff_url"] != nil || p["cost"] != nil {
		t.Errorf("the preview of a reboot names a price: %s", text)
	}
	if n := w.powerCalls(); n != 0 {
		t.Errorf("written before consent: %d power calls", n)
	}
}

// With the yes the command is sent once; the transition seen to its end
// is «rebooted». The same call again is a new reboot: it asks, and sends
// nothing without a yes of its own.
func TestVMReboot_ConfirmedRebootsAndARepeatAsksAgain(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	// The world ends the reboot at its sixth read: the reads before the
	// command count, and a reboot that ends before the first read after
	// it is one evo does not see (the test of reboot_accepted).
	w.stepAfter = 6
	m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "rebooted" || m["state"] != "running" || m["vm_id"] != w.id {
		t.Fatalf("vm_reboot with consent: %s", text)
	}
	if _, ok := m["elapsed_sec"]; !ok {
		t.Errorf("the answer has no elapsed_sec: %s", text)
	}
	m, text, _ = callNamed(t, r, "vm_reboot", map[string]any{"name": "web"})
	if m["status"] != "confirmation_required" {
		t.Errorf("a repeat of a reboot that ended: %s, want the question of a new one", text)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want 1", n)
	}
}

// A reboot in flight that this call did not send is waited for with no
// question and no command; the answer is the state, and says the reboot
// was not this call's.
func TestVMReboot_RebootInFlightIsWaitedForAndNotClaimed(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w := vmStartWorld(t, "running")
		if err := w.SetVMPower(context.Background(), w.id, "reboot"); err != nil {
			t.Fatal(err)
		}
		w.stepAfter = 6
		m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": confirmed})
		if isErr || m["status"] != "running" || m["state"] != "running" || w.powerCalls() != 1 {
			t.Errorf("rebooting, confirmed=%v: %s (power calls %d, want the 1 of the world's own setup)", confirmed, text, w.powerCalls())
		}
		if note, _ := m["note"].(string); !strings.Contains(note, "not this call") {
			t.Errorf("the answer claims a reboot the call did not send: %s", text)
		}
	}
}

// A stopped VM has nothing to reboot: refused before the question, with
// the start as the step — a reboot is not a way to start a VM past the
// preview of vm_start (§5.1).
func TestVMReboot_StoppedIsRefusedWithVMStartAsTheStep(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		r, w := vmStartWorld(t, "stopped")
		m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": confirmed})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, "stopped") || w.powerCalls() != 0 {
			t.Fatalf("stopped, confirmed=%v: %s (power calls %d)", confirmed, text, w.powerCalls())
		}
		next, _ := m["next_args"].(map[string]any)
		if m["next_tool"] != "vm_start" || next["vm_id"] != w.id || next["confirmed"] != nil {
			t.Errorf("the refusal does not name vm_start of the VM, without consent, as the step: %s", text)
		}
	}
}

// The rest of the table, as vm_stop's.
func TestVMReboot_TheRestOfTheTable(t *testing.T) {
	for _, state := range []string{"starting", "stopping", "creating", "updating", "deleting", "error_deleting", "error_creating"} {
		r, w := vmStartWorld(t, state)
		m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
		if !isErr || m["reason"] != clerrors.CodeUsage || !strings.Contains(text, state) || w.powerCalls() != 0 {
			t.Errorf("%s: %s", state, text)
		}
		if strings.Contains(text, "destroy") || strings.Contains(text, "delete") && !strings.Contains(state, "delet") {
			t.Errorf("%s: the refusal names a delete as a step: %s", state, text)
		}
	}
	for _, state := range []string{"error", "paused"} {
		r, w := vmStartWorld(t, state)
		m, text, _ := callNamed(t, r, "vm_reboot", map[string]any{"name": "web"})
		if warning, _ := m["warning"].(string); m["status"] != "confirmation_required" || !strings.Contains(warning, state) || w.powerCalls() != 0 {
			t.Errorf("%s: %s", state, text)
		}
	}
	r, _ := vmStartWorld(t, "running")
	m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"vm_id": faultID(0x7f), "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeVMNotFound {
		t.Errorf("no such VM: %s", text)
	}
}

// At the limit of an MCP call the answer is «rebooting» and the step is a
// read: the same call, come after the end of the reboot, would be the
// question of a second one (§5.2, 2).
func TestVMReboot_AtTheLimitTheNextStepIsARead(t *testing.T) {
	r, w := vmStartWorldWaiting(t, "running", 0)
	budgeted(r)
	m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "rebooting" || m["state"] != "rebooting" {
		t.Fatalf("vm_reboot whose VM stays rebooting past the limit: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	if m["next_tool"] != "vm_get" || next["vm_id"] != w.id {
		t.Errorf("the next step is %v, want the read vm_get of the VM: %s", m["next_tool"], text)
	}
	if say, _ := m["next"].(string); !strings.Contains(say, "new reboot") {
		t.Errorf("the answer does not say that a repeat of vm_reboot is a new reboot: %q", say)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want 1", n)
	}
}

// The platform took the command and the VM was never seen to leave
// running: evo does not know whether it rebooted faster than it reads or
// has not begun. Not «rebooted», and not an error a repeat would answer
// with a second reboot (§5.2, 3) — by the tool's own limit and by the
// budget of a call alike.
func TestVMReboot_AcceptedAndNotSeenIsSaidAsSuch(t *testing.T) {
	for name, world := range map[string]func() (*Registry, *powerWorld){
		"own limit": func() (*Registry, *powerWorld) { return vmStartWorld(t, "running") },
		"budget": func() (*Registry, *powerWorld) {
			r, w := vmStartWorldWaiting(t, "running", 0)
			budgeted(r)
			return r, w
		},
	} {
		r, w := world()
		w.powerIgnored = true
		m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
		if isErr || m["status"] != "reboot_accepted" || m["state"] != "running" {
			t.Fatalf("%s: a reboot taken and not seen: %s", name, text)
		}
		if warning, _ := m["warning"].(string); !strings.Contains(warning, "did not see") {
			t.Errorf("%s: the answer does not say the change of state was not seen: %s", name, text)
		}
		// Nowhere in the answer is «accepted» said as «rebooted» («Риск»,
		// conclusion 138, В1).
		if strings.Contains(text, "rebooted") {
			t.Errorf("%s: the answer of a reboot not seen says «rebooted»: %s", name, text)
		}
		if next, _ := m["next_args"].(map[string]any); m["next_tool"] != "vm_get" || next["vm_id"] != w.id {
			t.Errorf("%s: the next step is %v, want vm_get: %s", name, m["next_tool"], text)
		}
		if n := w.powerCalls(); n != 1 {
			t.Errorf("%s: power calls = %d, want 1", name, n)
		}
	}
}

// A VM that leaves the reboot for another state is an error with that
// state; a command the platform did not take is the error of the request —
// a VM that runs is not a reboot made.
func TestVMReboot_FailuresAreNotAReboot(t *testing.T) {
	r, w := vmStartWorld(t, "running")
	w.stepAfter, w.stepTo = 6, []string{"error"}
	m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
	if !isErr || m["reason"] != clerrors.CodeAPIError || m["state"] != "error" {
		t.Errorf("a reboot that ends in error: %s", text)
	}
	for _, kind := range []string{faultTransport, faultHTTP500} {
		r, w := vmStartWorld(t, "running")
		w.failNth("SetVMPower", 1, kind)
		m, text, isErr := callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
		if !isErr || m["status"] == "rebooted" || m["status"] == "reboot_accepted" {
			t.Errorf("%s on the command: %s, want the error of the request", kind, text)
		}
	}
	// A lost answer of a command taken: the VM is found rebooting, and the
	// call goes on to the end.
	r, w = vmStartWorld(t, "running")
	w.failNth("SetVMPower", 1, faultLost)
	w.stepAfter = 8
	m, text, isErr = callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "rebooted" || w.powerCalls() != 1 {
		t.Errorf("a lost answer of a reboot taken: %s (power calls %d)", text, w.powerCalls())
	}
	// 422 from running is the platform's own refusal, not the state's.
	r, w = vmStartWorld(t, "running")
	w.powerRefused = "zone_changes_prohibited"
	m, text, isErr = callNamed(t, r, "vm_reboot", map[string]any{"name": "web", "confirmed": true})
	details, _ := m["details"].(map[string]any)
	if !isErr || m["reason"] != clerrors.CodeAPIError || details["cause"] != "refused" || m["next_tool"] != nil {
		t.Errorf("a 422 for a running VM: %s", text)
	}
}
