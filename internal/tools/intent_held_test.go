// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"path/filepath"
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
		{"the step was throttled", `{"status":"error","reason":"API_RATE_LIMITED"}`, false},
		{"the step was not let through", `{"status":"error","reason":"AUTH_EXPIRED"}`, false},
		{"the operation ended", `{"status":"started"}`, true},
		{"the operation was refused with a code", `{"status":"error","reason":"USAGE"}`, true},
		// evo goes on with it no more: the wait is handed to a read.
		{"the step hands the wait to a read", `{"status":"starting","next_tool":"vm_get","next_args":{"vm_id":"` + resMine + `"}}`, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r, dir, _ := start(t)
			r.settleHeld(context.Background(), "vm_start", step, c.answer)
			if got := len(intent.Pending(dir)) == 0; got != c.closed {
				t.Errorf("closed %v, want %v: %+v", got, c.closed, intent.Pending(dir))
			}
		})
	}
	// The call names the VM by its name, the record by its id: the answer
	// names the id («Риск», conclusion 137, У1).
	t.Run("a call by the name, whose answer names the id", func(t *testing.T) {
		r, dir, _ := start(t)
		r.settleHeld(context.Background(), "vm_start", map[string]any{"name": "web"}, `{"status":"already_running","vm_id":"`+resMine+`"}`)
		if left := intent.Pending(dir); len(left) != 0 {
			t.Errorf("left %+v", left)
		}
	})
	t.Run("a call by the name of another VM", func(t *testing.T) {
		r, dir, _ := start(t)
		r.settleHeld(context.Background(), "vm_start", map[string]any{"name": "db"}, `{"status":"already_running","vm_id":"`+resForeign+`"}`)
		if len(intent.Pending(dir)) != 1 {
			t.Error("a call on another VM closed the record")
		}
	})
	t.Run("a step on another resource", func(t *testing.T) {
		r, dir, _ := start(t)
		r.settleHeld(context.Background(), "vm_start", map[string]any{"vm_id": resForeign}, `{"status":"started"}`)
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
		r.settleHeld(context.Background(), "vm_start", step, `{"status":"started"}`)
		if len(intent.Pending(dir)) != 1 {
			t.Error("a call with this key closed the held record of another key")
		}
	})
	t.Run("the words of the record", func(t *testing.T) {
		r, dir, _ := start(t)
		notes := r.PendingNotes(filepath.Dir(filepath.Dir(dir)))
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

// The whole path of a call (intended): the consented call ends at its
// limit with the operation going on and its record is held; the step of
// the wait — the same tool, without consent — goes on waiting and keeps
// it, then ends the operation and closes it. A read of another tool in
// between closes nothing: only the tool's own step knows its outcome.
func TestIntent_HeldThroughTheCallsOfATool(t *testing.T) {
	r := ownerRegistry(t, &ownerFake{}, false, ownProject)
	eng, _ := r.newEngine(nil)
	t.Chdir(eng.StateDir)
	dir := intent.Dir(eng.StateDir)
	const goesOn = `{"status":"starting","next_tool":"vm_start","next_args":{"vm_id":"` + resMine + `"}}`
	answers := []string{goesOn, goesOn, `{"status":"started"}`}
	tl, _ := r.Lookup("vm_start")
	call := r.intended(tl, func(context.Context, map[string]any) (string, bool) {
		text := answers[0]
		answers = answers[1:]
		return text, false
	})
	held := func() int {
		n := 0
		for _, in := range intent.Pending(dir) {
			if in.Held {
				n++
			}
		}
		return n
	}
	call(context.Background(), map[string]any{"name": "web", "confirmed": true})
	if held() != 1 {
		t.Fatalf("after the consented call: %+v", intent.Pending(dir))
	}
	started := intent.Pending(dir)[0].StartedAt
	get, _ := r.Lookup("vm_get")
	r.intended(get, func(context.Context, map[string]any) (string, bool) { return `{"status":"ok"}`, false })(
		context.Background(), map[string]any{"vm_id": resMine})
	call(context.Background(), map[string]any{"vm_id": resMine})
	if left := intent.Pending(dir); held() != 1 || !left[0].StartedAt.Equal(started) {
		t.Fatalf("after a step that still waits: %+v", left)
	}
	call(context.Background(), map[string]any{"vm_id": resMine})
	if left := intent.Pending(dir); len(left) != 0 {
		t.Errorf("after the step that ended the operation: %+v", left)
	}
}

// A declared continuation with consent (provision at the limit of an MCP
// call): the record of the call that made the VM is held, the preview of
// the repeat keeps it, and the repeat — a consented call with an intent of
// its own — finishes the VM and leaves no record («Риск», conclusion 137).
func TestIntent_HeldThroughADeclaredContinuation(t *testing.T) {
	cloud, dir, r := provisionWorld(t)
	t.Chdir(dir)
	first := decode(t, mustCall(t, r, "provision", map[string]any{"name": "web", "ports": []any{"80"}, "confirmed": true}))
	next, _ := first["next_args"].(map[string]any)
	left := intent.Pending(intent.Dir(dir))
	if first["next_tool"] != "provision" || len(left) != 1 || !left[0].Held {
		t.Fatalf("after the first call: %v\n%+v", first, left)
	}
	mustCallErr(t, r, "provision", map[string]any{"name": "web", "ports": []any{"80"}})
	if left := intent.Pending(intent.Dir(dir)); len(left) != 1 {
		t.Fatalf("the preview of the repeat changed the records: %+v", left)
	}
	cloud.mu.Lock()
	cloud.slow = false
	for i := range cloud.vms {
		cloud.vms[i].Status = "active"
	}
	cloud.mu.Unlock()
	if m := decode(t, mustCall(t, r, "provision", next)); m["status"] != "ok" {
		t.Fatalf("the repeat: %v", m)
	}
	if left := intent.Pending(intent.Dir(dir)); len(left) != 0 {
		t.Errorf("after the repeat finished the VM: %+v", left)
	}
}
