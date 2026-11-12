// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
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

// rebootWorldInADir is vmStartWorld in a directory of evo: the calls write
// their intents there, as a run of evo does.
func rebootWorldInADir(t *testing.T) (*Registry, *powerWorld, string) {
	t.Helper()
	c := &faultCloud{}
	vm, err := c.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	c.setVMState(vm.ID, "running")
	w := &powerWorld{faultCloud: c, id: vm.ID}
	dir := t.TempDir()
	t.Chdir(dir) // evo runs in the directory of its state
	r := New(func(map[string]any) (*engine.Engine, error) {
		var buf bytes.Buffer
		return &engine.Engine{
			Config:   config.Config{ProjectID: faultProject, KeyID: "k", Secret: "s"},
			Auth:     staticToken{},
			Cloud:    w,
			Printer:  output.NewWithWriter(&buf, output.ModeText, false),
			StateDir: dir,
			Timings:  engine.Timings{VMPollInterval: time.Millisecond, VMPollTimeout: 50 * time.Millisecond},
		}, nil
	})
	return r, w, intent.Dir(dir)
}

// The blind repeat («Риск», conclusion 138, У1): the client cuts the call
// after the command went out — its time limit, a Ctrl-C — and sends the
// same call again with the same consent. The VM runs again by then, and
// the call would reboot it a second time on a consent given once. The
// record of the cut call makes the repeat ask instead: one command, and
// the next consented call is a reboot of its own.
func TestVMReboot_RepeatAfterACutCallAsksAndSendsNothing(t *testing.T) {
	r, w, dir := rebootWorldInADir(t)
	tl, _ := r.Lookup("vm_reboot")
	args := func() map[string]any { return map[string]any{"name": "web", "confirmed": true} }

	// The first call: the platform takes the command, the client is gone
	// before the answer.
	ctx, cancel := context.WithCancel(context.Background())
	w.onPower = func() error {
		err := w.faultCloud.SetVMPower(context.Background(), w.id, "reboot")
		cancel()
		if err != nil {
			return err
		}
		return ctx.Err()
	}
	tl.Handler(ctx, args())
	w.onPower = nil
	if n := w.powerCalls(); n != 1 {
		t.Fatalf("power calls after the cut call = %d, want 1", n)
	}
	if left := intent.Pending(dir); len(left) != 1 || left[0].Tool != "vm_reboot" || left[0].Held {
		t.Fatalf("the record of the cut call: %+v, want one, not held", left)
	}
	// The reboot ends; the VM runs as it did before the call.
	w.stepVMPower(w.id)

	// The repeat with the consent of the first call: a question, nothing
	// sent, the record closed.
	text, isErr := tl.Handler(context.Background(), args())
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	if !isErr || m["status"] != "confirmation_required" || m["interrupted"] == nil {
		t.Fatalf("the repeat after a cut call: %s, want the question again with what was cut", text)
	}
	if msg, _ := m["message"].(string); !strings.Contains(msg, "outcome is unknown") || !strings.Contains(msg, "vm_get") {
		t.Errorf("the question does not say that the outcome is unknown and where to look: %q", msg)
	}
	if n := w.powerCalls(); n != 1 {
		t.Fatalf("power calls after the repeat = %d, want 1: the repeat rebooted the VM again", n)
	}
	if left := intent.Pending(dir); len(left) != 0 {
		t.Errorf("the record after the question: %+v, want none", left)
	}

	// The person was asked: the next consented call is a reboot of its own.
	w.stepAfter = w.reads + 6
	text, isErr = tl.Handler(context.Background(), args())
	_ = json.Unmarshal([]byte(text), &m)
	if isErr || m["status"] != "rebooted" || w.powerCalls() != 2 {
		t.Errorf("the consented call after the question: %s (power calls %d, want 2)", text, w.powerCalls())
	}
}

// The declaration of the layer and the table of power actions say one
// thing: an action whose start and target are one state is a tool every
// send of which is a change, and no other power tool is.
func TestVMPower_EverySendIsAChangeIsDeclaredToTheIntentLayer(t *testing.T) {
	for tool, act := range vmPowerActions {
		if _, declared := everySendIsAChange[tool]; declared != act.everySendIsAChange() {
			t.Errorf("%s: from %s, to %s, declared to the intent layer as «every send is a change»: %v", tool, act.from, act.to, declared)
		}
	}
}
