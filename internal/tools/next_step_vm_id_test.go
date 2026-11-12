// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// D102: the next step of provision and deploy_apply names the VM it goes
// on with (vm_id), and a step whose VM is gone refuses — with consent and
// without it alike — saying no new VM was made and naming no next call.
// Nothing billable is made on the way to the refusal: no VM, no floating
// IP. TestNextStep_VMGoneBetweenTheCallsIsNotMadeAgain holds the count of
// creates; this one holds the answer.
func TestNextStep_VMGoneRefusesWithoutANextStep(t *testing.T) {
	for _, c := range []struct {
		tool   string
		args   map[string]any
		noDial bool
		plan   bool
	}{
		{tool: "provision", args: map[string]any{"name": "web", "confirmed": true}},
		{tool: "deploy_apply", args: map[string]any{"plan_id": "plan-0123456789ab", "confirmed": true}, noDial: true, plan: true},
	} {
		t.Run(c.tool, func(t *testing.T) {
			faultToolsIsolate(t)
			cloud := &faultCloud{noDial: c.noDial}
			dir := t.TempDir()
			if c.plan {
				if err := deployplan.Save(dir, faultPlan()); err != nil {
					t.Fatal(err)
				}
			}
			cloud.mu.Lock()
			cloud.slow = true
			cloud.mu.Unlock()
			r := progressed(budgetComputeRegistry(t, cloud, dir, engine.Timings{}))
			call := func(args map[string]any) (map[string]any, string, bool) {
				text, isErr := r.handlers[c.tool](context.Background(), args)
				var m map[string]any
				if err := json.Unmarshal([]byte(text), &m); err != nil {
					t.Fatalf("not JSON: %s", text)
				}
				return m, text, isErr
			}
			first, text, isErr := call(c.args)
			if isErr || first["status"] != "creating" {
				t.Fatalf("the first call: %s", text)
			}
			next, _ := first["next_args"].(map[string]any)
			if id, _ := first["vm_id"].(string); id == "" || next["vm_id"] != id {
				t.Fatalf("next_args do not name the VM of the answer (vm_id %v): %s", first["vm_id"], text)
			}
			count := func(op string) int {
				cloud.mu.Lock()
				defer cloud.mu.Unlock()
				return cloud.seen[op]
			}
			vms, fips := count("CreateVM"), count("CreateFloatingIP")

			cloud.mu.Lock()
			cloud.vms = nil
			cloud.mu.Unlock()

			for _, confirmed := range []bool{true, false} {
				args := map[string]any{}
				for k, v := range next {
					args[k] = v
				}
				args["confirmed"] = confirmed
				m, text, isErr := call(args)
				if !isErr || m["reason"] != clerrors.CodeVMNotFound {
					t.Fatalf("confirmed=%v: the step on a VM that is gone: %s", confirmed, text)
				}
				if !strings.Contains(text, "no new VM was created") && !strings.Contains(text, "новая не создана") {
					t.Errorf("confirmed=%v: the refusal does not say that nothing was made: %s", confirmed, text)
				}
				if _, has := m["next_tool"]; has {
					t.Errorf("confirmed=%v: the refusal names a next call: %s", confirmed, text)
				}
			}
			if a, b := count("CreateVM"), count("CreateFloatingIP"); a != vms || b != fips {
				t.Errorf("the refused steps made something: CreateVM %d→%d, CreateFloatingIP %d→%d", vms, a, fips, b)
			}
		})
	}
}

// The step whose VM is there goes on as before: the VM is adopted, no
// second one is made (the answer of the usual path does not change).
func TestNextStep_VMThereIsAdopted(t *testing.T) {
	faultToolsIsolate(t)
	cloud := &faultCloud{}
	dir := t.TempDir()
	cloud.mu.Lock()
	cloud.slow = true
	cloud.mu.Unlock()
	r := progressed(budgetComputeRegistry(t, cloud, dir, engine.Timings{}))
	text, isErr := r.handlers["provision"](context.Background(), map[string]any{"name": "web", "confirmed": true})
	var first map[string]any
	if err := json.Unmarshal([]byte(text), &first); err != nil || isErr {
		t.Fatalf("the first call: %s", text)
	}
	next, _ := first["next_args"].(map[string]any)
	if text, isErr = r.handlers["provision"](context.Background(), next); isErr {
		t.Fatalf("the step on a VM that is there: %s", text)
	}
	cloud.mu.Lock()
	n := cloud.seen["CreateVM"]
	cloud.mu.Unlock()
	if n != 1 {
		t.Errorf("CreateVM %d times, want 1: the step did not adopt the VM", n)
	}
}
