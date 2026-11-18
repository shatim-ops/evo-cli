// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A record of a call that has not returned — no Ended, its process alive —
// is a call going on, not a cut one: a second call of the same tool on the
// same resource is not told it was cut, and neither it nor its preview
// closes the record (A19-9, #2429; the rule of intent.Settle, FB19I-34).
// The consented one sends nothing: a client that timed out sends the call
// again while the first goes on («Риск», 138) — it is told the earlier
// call is going on. A record whose call returned (Ended) is still a cut
// call: asked about.
func TestIntent_ACallInFlightIsNotACutCall(t *testing.T) {
	const tool = "cert_renew" // stands for a tool of the list
	everySendIsAChange[tool] = changeRule{read: "cert_get"}
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	this := &state.Maker{KeyID: config.MaskKeyID("k")}
	for _, c := range []struct {
		name      string
		ended     bool
		confirmed bool
		asks      bool
	}{
		{"in flight, consented", false, true, false},
		{"in flight, preview", false, false, false},
		{"returned, consented", true, true, true},
		{"returned, preview", true, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := ownerRegistry(t, &ownerFake{}, false, ownProject)
			eng, _ := r.newEngine(nil)
			t.Chdir(eng.StateDir)
			dir := intent.Dir(eng.StateDir)
			if _, err := intent.Open(dir, intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this,
				PID: os.Getpid(), Sent: true, Ended: c.ended}); err != nil {
				t.Fatal(err)
			}
			sent := 0
			tl, _ := r.Lookup(tool)
			call := r.intended(tl, func(ctx context.Context, args map[string]any) (string, bool) {
				if confirmed, _ := args["confirmed"].(bool); !confirmed {
					return `{"status":"confirmation_required","message":"preview"}`, true
				}
				_ = httpretry.NoteWrite(ctx)
				sent++
				return `{"status":"ok"}`, false
			})
			text, _ := call(context.Background(), map[string]any{"certificate_id": resMine, "confirmed": c.confirmed})
			var m map[string]any
			_ = json.Unmarshal([]byte(text), &m)
			if _, asked := m["interrupted"]; asked != c.asks {
				t.Fatalf("asked about the record: %v, want %v\n%s", asked, c.asks, text)
			}
			if c.asks {
				return
			}
			if sent != 0 {
				t.Errorf("a call was sent while the earlier one goes on: %d\n%s", sent, text)
			}
			going, _ := m["going_on"].(map[string]any)
			if c.confirmed && (m["status"] != "confirmation_required" || going["calls"] != float64(1) || going["read"] != "cert_get") {
				t.Errorf("the consented call does not say the earlier one is going on: %s", text)
			}
			left := 0
			for _, in := range intent.Pending(dir) {
				if in.PID == os.Getpid() && !in.Ended && in.Sent {
					left++
				}
			}
			if left != 1 {
				t.Errorf("the record of the call going on was closed: %+v", intent.Pending(dir))
			}
		})
	}
}

// A record whose call cannot go on any more is no call going on: its
// question is that of a cut call, not going_on for as long as the pid
// lives («Риск», conclusion 401, У1) —
//   - of this pid, not opened by this process: a server in a container
//     killed mid-call and started again has the same pid;
//   - of a handler that panicked: the server recovers it, the call never
//     returns.
//
// And the note of pending operations says of a call going on what the
// answer says (У2); the answer that sends nothing has the reason of a
// question, so the CLI exits 3 (У3).
func TestIntent_ARecordThatCannotGoOnIsACutCall(t *testing.T) {
	const tool = "cert_renew"
	everySendIsAChange[tool] = changeRule{read: "cert_get"}
	t.Cleanup(func() { delete(everySendIsAChange, tool) })
	this := &state.Maker{KeyID: config.MaskKeyID("k")}
	args := func() map[string]any { return map[string]any{"certificate_id": resMine, "confirmed": true} }
	setup := func(t *testing.T) (*Registry, string, Handler) {
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		t.Chdir(eng.StateDir)
		tl, _ := r.Lookup(tool)
		return r, intent.Dir(eng.StateDir), r.intended(tl, func(ctx context.Context, args map[string]any) (string, bool) {
			if confirmed, _ := args["confirmed"].(bool); !confirmed {
				return `{"status":"confirmation_required","message":"preview"}`, true
			}
			return `{"status":"ok"}`, false
		})
	}
	asked := func(t *testing.T, call Handler) {
		t.Helper()
		text, _ := call(context.Background(), args())
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		if _, cut := m["interrupted"]; !cut || m["going_on"] != nil {
			t.Errorf("want the question of a cut call, not going_on: %s", text)
		}
	}
	t.Run("the same pid, another process", func(t *testing.T) {
		_, dir, call := setup(t)
		in := intent.Intent{ID: "0123456789abcdef", Tool: tool, Target: "certificate_id=" + resMine, Key: this,
			PID: os.Getpid(), Sent: true, Machine: "a-container-before", StartedAt: time.Now().Add(-30 * 24 * time.Hour)}
		b, _ := json.Marshal(in)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, in.ID+".json"), b, 0o600); err != nil {
			t.Fatal(err)
		}
		asked(t, call)
	})
	t.Run("a handler that panicked", func(t *testing.T) {
		r, _, call := setup(t)
		tl, _ := r.Lookup(tool)
		panicking := r.intended(tl, func(ctx context.Context, _ map[string]any) (string, bool) {
			_ = httpretry.NoteWrite(ctx)
			panic("the handler")
		})
		func() {
			defer func() {
				if p := recover(); p != nil {
					if err, ok := p.(runtime.Error); ok {
						panic(err)
					}
				}
			}()
			panicking(context.Background(), args())
		}()
		asked(t, call)
	})
	t.Run("the note and the reason of a call going on", func(t *testing.T) {
		r, dir, call := setup(t)
		if _, err := intent.Open(dir, intent.Intent{Tool: tool, Target: "certificate_id=" + resMine, Key: this,
			PID: os.Getpid(), Sent: true}); err != nil {
			t.Fatal(err)
		}
		text, _ := call(context.Background(), args())
		var m map[string]any
		_ = json.Unmarshal([]byte(text), &m)
		if m["going_on"] == nil || m["reason"] != "CONFIRMATION_REQUIRED" {
			t.Errorf("going_on without the reason of a question: %s", text)
		}
		root, _ := r.stateDir()
		notes, _ := r.pendingNotes(root, nil, callResource{})
		if len(notes) != 1 || !strings.Contains(notes[0], "has not returned yet") || strings.Contains(notes[0], "outcome is unknown") {
			t.Errorf("the note of a call going on: %v", notes)
		}
	})
}
