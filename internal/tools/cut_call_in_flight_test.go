// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

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
