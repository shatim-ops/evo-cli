// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
)

// The rule of the layer, for every tool that takes consent — not a line
// per tool: a consented call that ends with no outcome and sent no write
// changed nothing, and leaves no record; one that sent a write leaves its
// record, marked «sent» before the write went out — a process killed
// after it leaves the mark («Риск», conclusion 187, У2; the owner's
// decision of 13.11).
func TestIntent_ACallThatSentNoWriteLeavesNoRecord(t *testing.T) {
	const cut = `{"status":"error","reason":"API_UNREACHABLE"}`
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	dir := intent.Dir(eng.StateDir)
	reached := 0
	for _, tl := range r.Tools() {
		if !takesConsent(tl) {
			continue
		}
		args := func() map[string]any {
			a := map[string]any{"confirmed": true, "name": "web"}
			for _, id := range idArgs[tl.Name] {
				a[id.name] = resMine
			}
			return a
		}
		if !NeedsConsent(tl, args()) {
			continue // these arguments ask nothing: no record
		}
		ran := false
		r.intended(tl, func(ctx context.Context, _ map[string]any) (string, bool) {
			ran = true
			return cut, true
		})(context.Background(), args())
		if !ran {
			continue // refused before the handler: nothing to tell
		}
		reached++
		if left := intent.Pending(dir); len(left) != 0 {
			t.Errorf("%s: a cut call that sent no write left %+v", tl.Name, left)
		}
		marked := false
		r.intended(tl, func(ctx context.Context, _ map[string]any) (string, bool) {
			if err := httpretry.NoteWrite(ctx); err != nil {
				t.Fatal(err)
			}
			// The write goes out now: its mark is in the file already.
			p := intent.Pending(dir)
			marked = len(p) == 1 && p[0].Sent && p[0].Counts
			return cut, true
		})(context.Background(), args())
		if left := intent.Pending(dir); len(left) != 1 || !marked {
			t.Errorf("%s: a cut call that sent a write: marked before it went %v, left %+v", tl.Name, marked, left)
		}
		for _, in := range intent.Pending(dir) {
			intent.Close(in)
		}
	}
	if reached < 40 {
		t.Fatalf("%d tools reached their handler: want most of those that take consent", reached)
	}
}
