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
