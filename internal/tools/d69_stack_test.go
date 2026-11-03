// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// D69, PR 3: the VM step of a stack takes provision's creating as a VM in
// progress — not made, not failed — and the next crank repeats provision,
// which adopts the VM and finishes it. The password of the VM goes out
// once, in the answer of the crank that got it, and nowhere is it kept.
func TestD69_StackVMStepCreatingIsInProgress(t *testing.T) {
	home := stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
	var calls atomic.Int32
	var waitArgs []any
	r.handlers["provision"] = func(_ context.Context, args map[string]any) (string, bool) {
		waitArgs = append(waitArgs, args["wait"])
		if calls.Add(1) == 1 {
			return toJSON(map[string]any{"status": "creating", "vm_id": "vm-1", "vm_status": "creating",
				"password": "pw-made-once", "next_tool": "provision"}), false
		}
		return toJSON(map[string]any{"status": "ok", "vm_id": "vm-1", "vm_ip": "203.0.113.4"}), false
	}

	text, isErr := r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	m := decode(t, text)
	if isErr || m["status"] != "running" {
		t.Fatalf("a VM creating is not an error of the stack: %s", text)
	}
	if pw, _ := m["passwords"].(map[string]any); pw["vm"] != "pw-made-once" {
		t.Errorf("the password of the step's VM is not in the answer that got it: %s", text)
	}
	loaded, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	st := loaded.Steps[0]
	if st.Status != "pending" || st.ResourceID != "vm-1" || st.Attempts != 0 || !strings.Contains(st.Detail, "creating") {
		t.Errorf("the step is not a VM in progress (pending, its VM for stack_destroy, no attempt used, the state said): %+v", st)
	}
	_ = filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if raw, _ := os.ReadFile(path); strings.Contains(string(raw), "pw-made-once") {
				t.Errorf("the password is kept in %s", path)
			}
		}
		return nil
	})

	text, _ = r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
	m = decode(t, text)
	if m["status"] != "done" || calls.Load() != 2 {
		t.Fatalf("the next crank does not finish the VM by repeating provision (%d calls): %s", calls.Load(), text)
	}
	if _, ok := m["passwords"]; ok {
		t.Errorf("the password goes out again: %s", text)
	}
	for i, w := range waitArgs {
		if w != nil {
			t.Errorf("call %d of provision passed wait=%v: the step waits within the call's budget", i+1, w)
		}
	}
}

// provision's wait for the boot floating IP after the VM is active ends
// with the budget too: creating, the VM active, the repeat finishes it.
func TestD69_ProvisionFIPWaitIsInTheBudget(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{fipDown: true}
	timings := provisionTimings()
	timings.FIPPollInterval = time.Hour
	r := budgeted(budgetComputeRegistry(t, cloud, t.TempDir(), timings))
	done := make(chan string, 1)
	go func() {
		text, _ := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
		done <- text
	}()
	select {
	case text := <-done:
		m := decode(t, text)
		if m["status"] != "creating" || m["vm_status"] != "active" || m["next_tool"] != "provision" {
			t.Errorf("want creating with the VM active and the next step: %s", text)
		}
	// wall-clock: four times the world's budget (budgetWatch is 1/45 of
	// D69's 45 s): a wait for the floating IP the budget does not end
	// runs for hours.
	case <-time.After(budgetWatch):
		t.Fatal("the wait for the floating IP outlives the budget")
	}
}
