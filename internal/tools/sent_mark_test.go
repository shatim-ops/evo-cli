// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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

// A mark that cannot be written keeps the write back (httpretry), and the
// call says why — that its record could not be written and nothing was
// sent — not the error the handler made of a request that never went out
// («the request may have reached Cloud.ru»): Qwen on #1675, medium.
func TestIntent_AMarkNotWrittenIsTheAnswerOfTheCall(t *testing.T) {
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	dir := intent.Dir(eng.StateDir)
	tl, _ := r.Lookup("cert_renew")
	sent := 0
	text, isErr := r.intended(tl, func(ctx context.Context, _ map[string]any) (string, bool) {
		// The directory goes away between the record and the first write:
		// a file stands where it was, so nothing is written under it.
		if err := os.RemoveAll(dir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := httpretry.NoteWrite(ctx); err != nil {
			return `{"status":"error","reason":"API_UNREACHABLE","error":"cannot reach Cloud.ru API","hint":"The request may have reached Cloud.ru"}`, true
		}
		sent++
		return `{"status":"ok"}`, false
	})(context.Background(), map[string]any{"certificate_id": resMine, "confirmed": true})
	var a struct{ Reason, Error, Hint string }
	_ = json.Unmarshal([]byte(text), &a)
	if !isErr || sent != 0 || a.Reason != clierrors.CodeConfigInvalid || !strings.Contains(a.Error, "nothing was sent") || strings.Contains(a.Hint, "may have reached") {
		t.Errorf("sent %d, answer:\n%s", sent, text)
	}
}
