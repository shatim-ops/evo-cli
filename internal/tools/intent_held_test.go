// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// A consented call that ends at its time limit with the operation still
// going — its answer names the same tool as the next step — keeps its
// record: the step of a wait carries no consent and writes no intent, so
// the start of the operation would be known to no one, and the limit of
// its repeats could not count from it (release plan 1.9, «Намерение D92
// держит запись, пока операция идёт»; «Риск», conclusion 111, У1). The
// record is named as the step names the resource, keeps its StartedAt, and
// closes when a call of the step ends the operation.
func TestIntent_HeldWhileTheOperationGoesOn(t *testing.T) {
	const goesOn = `{"status":"starting","next_tool":"vm_start","next_args":{"vm_id":"` + resMine + `"}}`
	step := map[string]any{"vm_id": resMine}
	start := func(t *testing.T) (*Registry, string, intent.Intent) {
		t.Helper()
		r := ownerRegistry(t, &ownerFake{}, false, ownProject)
		eng, _ := r.newEngine(nil)
		t.Chdir(eng.StateDir) // evo runs in the directory of its state
		dir := intent.Dir(eng.StateDir)
		end, err := r.beginIntent(context.Background(), "vm_start", map[string]any{"name": "web", "confirmed": true})
		if err != nil {
			t.Fatal(err)
		}
		was := intent.Pending(dir)
		end(goesOn)
		left := intent.Pending(dir)
		if len(was) != 1 || len(left) != 1 {
			t.Fatalf("records before the answer %+v, after %+v", was, left)
		}
		in := left[0]
		if !in.Held || in.Target != "vm_id="+resMine || in.Name != "" || !in.StartedAt.Equal(was[0].StartedAt) || in.ID != was[0].ID {
			t.Fatalf("the held record: %+v, of %+v", in, was[0])
		}
		return r, dir, in
	}
	for _, c := range []struct {
		name, answer string
		closed       bool
	}{
		{"the step still waits", goesOn, false},
		{"the step asks", `{"status":"confirmation_required"}`, false},
		{"the step was cut", `{"status":"error","reason":"INTERRUPTED"}`, false},
		{"the step did not reach the cloud", `{"status":"error","reason":"API_UNREACHABLE"}`, false},
		{"the operation ended", `{"status":"started"}`, true},
		{"the operation was refused with a code", `{"status":"error","reason":"USAGE"}`, true},
		{"the next step is another tool", `{"status":"starting","next_tool":"vm_get","next_args":{"vm_id":"` + resMine + `"}}`, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, dir, _ := start(t)
			r.settleHeld("vm_start", step, c.answer)
			if got := len(intent.Pending(dir)) == 0; got != c.closed {
				t.Errorf("closed %v, want %v: %+v", got, c.closed, intent.Pending(dir))
			}
		})
	}
	t.Run("a step on another resource", func(t *testing.T) {
		r, dir, _ := start(t)
		r.settleHeld("vm_start", map[string]any{"vm_id": resForeign}, `{"status":"started"}`)
		if len(intent.Pending(dir)) != 1 {
			t.Error("a call on another VM closed the record")
		}
	})
	t.Run("a record of another key", func(t *testing.T) {
		r, dir, in := start(t)
		intent.Close(in)
		in.Key = &state.Maker{ServiceAccountID: "sa-other", KeyID: "ot…er"}
		if _, err := intent.Open(dir, in); err != nil {
			t.Fatal(err)
		}
		r.settleHeld("vm_start", step, `{"status":"started"}`)
		if len(intent.Pending(dir)) != 1 {
			t.Error("a call with this key closed the held record of another key")
		}
	})
	t.Run("the words of the record", func(t *testing.T) {
		r, dir, _ := start(t)
		notes := r.PendingNotes(strings.TrimSuffix(dir, "/.evo/intents"))
		if len(notes) != 1 || !strings.Contains(notes[0], "the operation was still going") || strings.Contains(notes[0], "the outcome is unknown") {
			t.Errorf("the note of a held record: %v", notes)
		}
	})
	t.Run("a new consented call ends the operation", func(t *testing.T) {
		r, dir, _ := start(t)
		end, err := r.beginIntent(context.Background(), "vm_start", map[string]any{"vm_id": resMine, "confirmed": true})
		if err != nil {
			t.Fatal(err)
		}
		end(`{"status":"started"}`)
		if left := intent.Pending(dir); len(left) != 0 {
			t.Errorf("left %+v", left)
		}
	})
	t.Run("a new consented call that goes on is the one held", func(t *testing.T) {
		r, dir, first := start(t)
		end, err := r.beginIntent(context.Background(), "vm_start", map[string]any{"vm_id": resMine, "confirmed": true})
		if err != nil {
			t.Fatal(err)
		}
		end(goesOn)
		left := intent.Pending(dir)
		if len(left) != 1 || left[0].ID == first.ID || !left[0].Held {
			t.Errorf("left %+v, want the record of the new call alone", left)
		}
	})
}
