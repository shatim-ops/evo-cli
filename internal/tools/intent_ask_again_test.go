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
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A tool every call of which is one more change (everySendIsAChange): a
// client that timed out sends the same call again with the consent of the
// first one — and the VM would be rebooted twice on one consent («Риск»,
// conclusion 138). A consented call that finds the record of its own cut
// call is not sent: it answers the question again, saying what is known,
// and closes the record; the next consented call is sent as any other.
func TestIntent_ConsentedCallAsksAgainAfterItsCutCall(t *testing.T) {
	const tool = "cert_renew" // stands for a tool of the list: any tool that takes consent
	everySendIsAChange[tool] = "cert_get"
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	args := func() map[string]any { return map[string]any{"certificate_id": resMine, "confirmed": true} }
	this := &state.Maker{KeyID: config.MaskKeyID("k")}
	for _, c := range []struct {
		name   string
		record *intent.Intent
		asks   bool
	}{
		{"its own cut call", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this}, true},
		{"no record", nil, false},
		{"a cut call on another resource", &intent.Intent{Tool: tool, Target: "certificate_id=" + resForeign, Key: this}, false},
		{"a cut call of another key", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}}, false},
		{"a record with no key", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine}, false},
		{"an operation going on", &intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this, Held: true}, false},
		{"a cut call of another tool", &intent.Intent{Tool: "cert_delete", Target: "certificate_id=" + resMine, Key: this}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			eng, _ := r.newEngine(nil)
			t.Chdir(eng.StateDir) // evo runs in the directory of its state
			dir := intent.Dir(eng.StateDir)
			if c.record != nil {
				if _, err := intent.Open(dir, *c.record); err != nil {
					t.Fatal(err)
				}
			}
			sent := 0
			tl, _ := r.Lookup(tool)
			call := r.intended(tl, func(context.Context, map[string]any) (string, bool) {
				sent++
				return `{"status":"ok"}`, false
			})
			text, isErr := call(context.Background(), args())
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if asked := m["status"] == "confirmation_required"; asked != c.asks || (asked && (!isErr || sent != 0)) || (!asked && sent != 1) {
				t.Fatalf("asks %v, want %v; sent %d\n%s", asked, c.asks, sent, text)
			}
			if !c.asks {
				return
			}
			msg, _ := m["message"].(string)
			if !strings.Contains(msg, "its outcome is unknown") || !strings.Contains(msg, "cert_get") || m["interrupted"] == nil {
				t.Errorf("the question does not say what is known: %s", text)
			}
			if left := intent.Pending(dir); len(left) != 0 {
				t.Errorf("the record of the cut call is not closed: %+v", left)
			}
			// The person was asked: the next consented call is sent.
			if text, _ := call(context.Background(), args()); sent != 1 || strings.Contains(text, "confirmation_required") {
				t.Errorf("the call after the question: sent %d\n%s", sent, text)
			}
		})
	}
	// A tool not of the list is sent as before, its cut record settled by
	// the call that answered.
	t.Run("a tool not of the list", func(t *testing.T) {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		t.Chdir(eng.StateDir)
		if _, err := intent.Open(intent.Dir(eng.StateDir), intent.Intent{Tool: "pg_delete", Target: "id=" + resMine, Key: this}); err != nil {
			t.Fatal(err)
		}
		sent := 0
		tl, _ := r.Lookup("pg_delete")
		r.intended(tl, func(context.Context, map[string]any) (string, bool) {
			sent++
			return `{"status":"deleted"}`, false
		})(context.Background(), map[string]any{"id": resMine, "confirmed": true})
		if sent != 1 || len(intent.Pending(intent.Dir(eng.StateDir))) != 0 {
			t.Errorf("sent %d, records %+v", sent, intent.Pending(intent.Dir(eng.StateDir)))
		}
	})
	// The words of the record send to the read, not to the same call.
	t.Run("the words of the record", func(t *testing.T) {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		if _, err := intent.Open(intent.Dir(eng.StateDir), intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this}); err != nil {
			t.Fatal(err)
		}
		notes := r.PendingNotes(eng.StateDir)
		if len(notes) != 1 || !strings.Contains(notes[0], "one more change, not a repeat") || strings.Contains(notes[0], "settles it") || strings.Contains(notes[0], "doctor checks it") {
			t.Errorf("the note: %v", notes)
		}
	})
}
