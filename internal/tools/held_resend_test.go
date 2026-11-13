// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// A start evo goes on with (its record held) whose VM fell back to stopped:
// the repeat with consent sends the command again — the resource is where
// the change started — and that send may be cut too. It left no trace: the
// held record of the first start stayed as it was, and the next repeat
// sent the command a third time with nobody asked (the blind repeat of the
// interim delta audit of 1.9, FB19I-2, through a held record; «Риск»,
// conclusion 177, У13). The cut send leaves its trace, and with the VM back
// where the change started the next repeat asks: two commands, not three.
func TestHeld_ACutSendOnAHeldOperationIsAskedAbout(t *testing.T) {
	r, w, dir := heldPowerWorld(t, "stopped")
	start := func(ctx context.Context) (map[string]any, string, bool) {
		tl, _ := r.Lookup("vm_start")
		text, isErr := tl.Handler(ctx, map[string]any{"name": "web", "confirmed": true})
		return parseAnswer(t, text), text, isErr
	}
	// The first start: the VM stays starting past the limit of the call,
	// and the record is held.
	if m, text, _ := start(context.Background()); m["status"] != "starting" {
		t.Fatalf("the first start: %s", text)
	}
	if held := intent.Pending(dir); len(held) != 1 || !held[0].Held {
		t.Fatalf("the record of the first start: %+v, want one held", held)
	}
	// The VM falls back to stopped; the repeat sends the command again and
	// is cut before the answer.
	w.setVMState(w.id, "stopped")
	ctx, cancel := context.WithCancel(context.Background())
	w.onPower = func() error {
		_ = w.faultCloud.SetVMPower(context.Background(), w.id, "start")
		w.setVMState(w.id, "stopped") // and falls back again
		cancel()
		return ctx.Err()
	}
	start(ctx)
	w.onPower = nil
	if n := w.powerCalls(); n != 2 {
		t.Fatalf("power calls after the cut repeat = %d, want 2", n)
	}
	// The next repeat with the consent of the first: the VM is where the
	// change started, and a command may have gone out unanswered — a
	// question, nothing sent.
	m, text, isErr := start(context.Background())
	if !isErr || m["status"] != "confirmation_required" || m["interrupted"] == nil {
		t.Errorf("the repeat after a cut send on a held operation: %s, want the question", text)
	}
	if n := w.powerCalls(); n != 2 {
		t.Errorf("power calls = %d, want 2: the repeat sent the command a third time with nobody asked", n)
	}
}

// A cut start or stop is told by the state of its VM (changeRule.settledBy):
// where the action leads — the record closes and the repeat answers as on
// any VM already there; in the transition — nobody is asked, the repeat
// waits for it; back where it started, or anywhere else — the question. A
// call cut before its command went out (markSent) left nothing: no record,
// no question («Риск», conclusion 177, У13; the conclusion on D107, У2).
func TestVMPower_ACutCallIsToldByTheStateOfItsVM(t *testing.T) {
	for _, c := range []struct {
		state, status string
		asked         bool
		records       int
	}{
		{"running", "already_running", false, 0},
		{"starting", "", false, -1},
		{"stopped", "confirmation_required", true, 0},
		{"error", "confirmation_required", true, 0},
	} {
		t.Run(c.state, func(t *testing.T) {
			r, w, dir := heldPowerWorld(t, "stopped")
			tl, _ := r.Lookup("vm_start")
			ctx, cancel := context.WithCancel(context.Background())
			w.onPower = func() error {
				_ = w.faultCloud.SetVMPower(context.Background(), w.id, "start")
				cancel()
				return ctx.Err()
			}
			tl.Handler(ctx, map[string]any{"vm_id": w.id, "confirmed": true})
			w.onPower = nil
			if left := intent.Pending(dir); len(left) != 1 || !left[0].Sent {
				t.Fatalf("the record of the cut start: %+v, want one, marked sent", left)
			}
			w.setVMState(w.id, c.state)
			text, _ := tl.Handler(context.Background(), map[string]any{"vm_id": w.id, "confirmed": true})
			m := parseAnswer(t, text)
			if _, asked := m["interrupted"]; asked != c.asked || (c.status != "" && m["status"] != c.status) {
				t.Errorf("the repeat with the VM %s: %s, want asked %v", c.state, text, c.asked)
			}
			if n := w.powerCalls(); n != 1 {
				t.Errorf("power calls = %d, want 1", n)
			}
			if c.records >= 0 && len(intent.Pending(dir)) != c.records && !c.asked {
				t.Errorf("records after the repeat: %+v, want %d", intent.Pending(dir), c.records)
			}
		})
	}
	t.Run("cut before the command", func(t *testing.T) {
		r, _, dir := heldPowerWorld(t, "stopped")
		tl, _ := r.Lookup("vm_start")
		ctx, cancel := context.WithCancel(context.Background())
		// The plan is cut at its read, before the command (no markSent).
		r.intended(tl, func(context.Context, map[string]any) (string, bool) {
			cancel()
			return `{"status":"error","reason":"INTERRUPTED"}`, true
		})(ctx, map[string]any{"vm_id": resMine, "confirmed": true})
		if left := intent.Pending(dir); len(left) != 0 {
			t.Errorf("a call that sent no command left a record: %+v", left)
		}
	})
}

// A repeat that finds the VM of a cut call in a transition waits for it —
// and the transition is no outcome: it may fall back. The trace of the cut
// send outlives the wait, and a VM back where the change started is asked
// about: one command before the question («Риск», the conclusion on
// #1652, У14).
func TestVMPower_ATransitionAfterACutCallKeepsItsTrace(t *testing.T) {
	for _, c := range []struct {
		tool, from, via, back string
	}{
		{"vm_start", "stopped", "stopping", "stopped"},
		{"vm_stop", "running", "stopping", "running"},
	} {
		t.Run(c.tool, func(t *testing.T) {
			r, w, dir := heldPowerWorld(t, c.from)
			tl, _ := r.Lookup(c.tool)
			ctx, cancel := context.WithCancel(context.Background())
			w.onPower = func() error {
				cmd := "start"
				if c.tool == "vm_stop" {
					cmd = "stop"
				}
				_ = w.faultCloud.SetVMPower(context.Background(), w.id, cmd)
				cancel()
				return ctx.Err()
			}
			tl.Handler(ctx, map[string]any{"vm_id": w.id, "confirmed": true})
			w.onPower = nil
			// The repeat finds a transition and waits for it.
			w.setVMState(w.id, c.via)
			tl.Handler(context.Background(), map[string]any{"vm_id": w.id, "confirmed": true})
			if n := w.powerCalls(); n != 1 {
				t.Fatalf("power calls after the repeat in the transition = %d, want 1", n)
			}
			if left := intent.Pending(dir); len(left) != 1 || left[0].CutSend.IsZero() {
				t.Fatalf("the trace of the cut send after the wait: %+v", left)
			}
			// The VM is back where the change started: the question.
			w.setVMState(w.id, c.back)
			text, _ := tl.Handler(context.Background(), map[string]any{"vm_id": w.id, "confirmed": true})
			if m := parseAnswer(t, text); m["interrupted"] == nil || w.powerCalls() != 1 {
				t.Errorf("the repeat with the VM back in %s: power calls %d, want 1 and the question\n%s", c.back, w.powerCalls(), text)
			}
		})
	}
}
