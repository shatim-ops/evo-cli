// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/engine"
)

// The next step of provision and deploy_apply carries confirmed, and until
// D102 was bound to nothing but a name or a plan (R-85; «Риск», conclusion
// 125); it names the VM by its id now (nextStepKeepsConsent). The person approved one VM. When that VM
// is gone between the call cut by the budget and the step — deleted in the
// console, by another agent — the step, called as the answer names it,
// makes a second VM with the consent of the first: a billed resource the
// person was not asked about. The step must not create: it answers the
// state it found, or asks again.
func TestNextStep_VMGoneBetweenTheCallsIsNotMadeAgain(t *testing.T) {
	cases := []struct {
		tool   string
		args   map[string]any
		noDial bool
		setup  func(t *testing.T, dir string)
	}{
		{tool: "provision", args: map[string]any{"name": "web", "confirmed": true}},
		{tool: "deploy_apply", args: map[string]any{"plan_id": "plan-0123456789ab", "confirmed": true}, noDial: true,
			setup: func(t *testing.T, dir string) {
				if err := deployplan.Save(dir, faultPlan()); err != nil {
					t.Fatal(err)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			faultToolsIsolate(t)
			cloud := &faultCloud{noDial: c.noDial}
			dir := t.TempDir()
			if c.setup != nil {
				c.setup(t, dir)
			}
			cloud.mu.Lock()
			cloud.slow = true
			cloud.mu.Unlock()
			r := progressed(budgetComputeRegistry(t, cloud, dir, engine.Timings{}))

			text, isErr := r.handlers[c.tool](context.Background(), c.args)
			if isErr {
				t.Fatalf("%s: %s", c.tool, text)
			}
			var first map[string]any
			if err := json.Unmarshal([]byte(text), &first); err != nil {
				t.Fatal(err)
			}
			next, _ := first["next_tool"].(string)
			args, _ := first["next_args"].(map[string]any)
			if next == "" || len(args) == 0 {
				t.Fatalf("the call cut by the budget names no next step: the world of the test changed\n%s", text)
			}
			created := func() int {
				cloud.mu.Lock()
				defer cloud.mu.Unlock()
				return cloud.seen["CreateVM"]
			}
			if n := created(); n != 1 {
				t.Fatalf("the first call created %d VMs, want 1: the world of the test changed\n%s", n, text)
			}

			// The VM is gone: the cloud has none.
			cloud.mu.Lock()
			cloud.vms = nil
			cloud.mu.Unlock()

			// The step, as the agent sends it.
			stepText, _ := r.handlers[next](context.Background(), args)
			if n := created(); n != 1 {
				t.Errorf("the next step of %s (%s %v) created a VM again: CreateVM %d times — the consent was for one VM; the step must be bound to the id of the VM the person approved (D102)\n%s",
					c.tool, next, args, n, stepText)
			}
		})
	}
}
