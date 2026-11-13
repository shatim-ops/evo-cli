// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// The end of the repeats of a power operation (release plan 1.9,
// «Намерение D92 держит запись, пока операция идёт»; «Риск», conclusion
// 111, У1). A start cut by the limit of an MCP call names the same call as
// the next step, and its record stays (held). The step tells its own start
// from somebody else's by that record: its own it goes on with — the same
// call again — until the limit of the operation, counted from the start of
// the operation and not from each repeat; past it the answer is the state
// and a read, evo waits no more, and the record closes.

// heldPowerWorld is vmStartWorldWaiting in a directory of evo: the calls
// write their intents there, as a run of evo does.
func heldPowerWorld(t *testing.T, state string) (*Registry, *powerWorld, string) {
	t.Helper()
	c := &faultCloud{}
	vm, err := c.CreateVM(context.Background(), provision.CreateVMRequest{Name: "web"})
	if err != nil {
		t.Fatal(err)
	}
	c.setVMState(vm.ID, state)
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
			Timings:  engine.Timings{VMPollInterval: time.Millisecond},
		}, nil
	})
	r.SetStateDir(dir)
	return budgeted(r), w, intent.Dir(dir)
}

func TestVMStart_OwnStartGoesOnUntilTheLimitOfTheOperation(t *testing.T) {
	r, w, dir := heldPowerWorld(t, "stopped")
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if isErr || m["status"] != "starting" || m["next_tool"] != "vm_start" {
		t.Fatalf("vm_start whose VM stays starting past the limit of the call: %s", text)
	}
	held := intent.Pending(dir)
	if len(held) != 1 || !held[0].Held || held[0].Target != "vm_id="+w.id {
		t.Fatalf("the record of the start: %+v, want one held record of the VM", held)
	}
	next, _ := m["next_args"].(map[string]any)

	// The step of its own start: the VM still starting, the same call
	// again — the operation is this key's to go on with.
	m, text, isErr = callNamed(t, r, "vm_start", next)
	if isErr || m["status"] != "starting" || m["next_tool"] != "vm_start" {
		t.Fatalf("the step of evo's own start within the limit of the operation: %s, want the same call as the step", text)
	}
	if args, _ := m["next_args"].(map[string]any); args["vm_id"] != w.id || args["confirmed"] != nil {
		t.Errorf("the step's own next step: %s, want the VM by its id and no consent", text)
	}
	if left := intent.Pending(dir); len(left) != 1 || !left[0].StartedAt.Equal(held[0].StartedAt) {
		t.Fatalf("the record after a step that goes on: %+v, want the one of the start, with its time", left)
	}

	// Past the limit of the operation, counted from its start: the state
	// and a read. evo waits no more, and the record closes.
	old := clock.Now
	t.Cleanup(func() { clock.Now = old })
	clock.Now = func() time.Time { return held[0].StartedAt.Add(vmPowerTimeout + time.Second) }
	m, text, isErr = callNamed(t, r, "vm_start", next)
	if isErr || m["status"] != "starting" || m["next_tool"] != "vm_get" {
		t.Fatalf("the step past the limit of the operation: %s, want the state and vm_get", text)
	}
	if _, ok := m["since_start_sec"]; !ok {
		t.Errorf("the answer past the limit does not say how long the operation has gone on: %s", text)
	}
	if left := intent.Pending(dir); len(left) != 0 {
		t.Errorf("the record after evo stopped waiting: %+v, want none", left)
	}
	if n := w.powerCalls(); n != 1 {
		t.Errorf("power calls = %d, want the 1 of the start", n)
	}
}

// The end of the operation within the limit: the step answers the outcome
// and the record closes.
func TestVMStart_OwnStartEndsAndClosesItsRecord(t *testing.T) {
	r, w, dir := heldPowerWorld(t, "stopped")
	m, text, _ := callNamed(t, r, "vm_start", map[string]any{"name": "web", "confirmed": true})
	if m["next_tool"] != "vm_start" {
		t.Fatalf("the start: %s", text)
	}
	next, _ := m["next_args"].(map[string]any)
	w.stepVMPower(w.id)
	m, text, isErr := callNamed(t, r, "vm_start", next)
	if isErr || m["status"] != "already_running" {
		t.Fatalf("the step once the VM runs: %s", text)
	}
	if left := intent.Pending(dir); len(left) != 0 {
		t.Errorf("the record after the operation ended: %+v", left)
	}
}

// A start with no record of this key — somebody else's, or another key's —
// is not evo's to go on with: the read, as before.
func TestVMStart_ForeignStartIsNotGoneOnWith(t *testing.T) {
	r, w, dir := heldPowerWorld(t, "stopped")
	if err := w.SetVMPower(context.Background(), w.id, "power_on"); err != nil {
		t.Fatal(err)
	}
	m, text, isErr := callNamed(t, r, "vm_start", map[string]any{"vm_id": w.id})
	if isErr || m["status"] != "starting" || m["next_tool"] != "vm_get" {
		t.Fatalf("vm_start of a VM somebody else is starting: %s", text)
	}
	if left := intent.Pending(dir); len(left) != 0 {
		t.Errorf("a call that sent nothing left a record: %+v", left)
	}
}
