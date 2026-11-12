// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// A declared continuation keeps the consent in its next step
// (nextStepKeepsConsent), so the step must send nothing but what the
// person approved (owner's decision 12.11 on R-85; release plan 1.9,
// «Согласие не переезжает в следующий шаг»). stack_destroy: the destroy of
// a stack whose VM the cloud deletes slowly is cut by the budget and names
// stack_destroy job_id confirmed as the next step. The step, called as the
// answer names it, creates nothing and deletes nothing the first call did
// not ask to delete: the only writes of the two calls are deletes of the
// job's own resources.
func TestNextStep_StackDestroyStepSendsNothingButTheApprovedDelete(t *testing.T) {
	faultToolsIsolate(t)
	stackHome(t)
	cloud := &faultCloud{}
	dir := t.TempDir()
	j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
	setup := budgetComputeRegistry(t, cloud, dir, faultToolsTimings())
	if text, isErr := setup.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true}); isErr {
		t.Fatalf("stack_apply before stack_destroy: %s", text)
	}
	cloud.mu.Lock()
	cloud.slow = true
	made := len(cloud.vms)
	applied := len(cloud.log)
	cloud.mu.Unlock()
	if made != 1 {
		t.Fatalf("the stack made %d VMs, want 1", made)
	}

	r := budgeted(budgetComputeRegistry(t, cloud, dir, engine.Timings{}))
	text, isErr := r.handlers["stack_destroy"](context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if isErr {
		t.Fatalf("stack_destroy: %s", text)
	}
	var first map[string]any
	if err := json.Unmarshal([]byte(text), &first); err != nil {
		t.Fatal(err)
	}
	next, _ := first["next_tool"].(string)
	args, _ := first["next_args"].(map[string]any)
	if next != "stack_destroy" || args["job_id"] != j.ID || args["confirmed"] != true {
		t.Fatalf("the answer cut by the budget names %s %v, want stack_destroy with job_id %s and confirmed: the world of the test changed\n%s", next, args, j.ID, text)
	}

	// The step, as the agent sends it: the tool and the arguments of the
	// answer, nothing added.
	if text, isErr = r.handlers[next](context.Background(), args); isErr {
		t.Fatalf("the next step: %s", text)
	}

	cloud.mu.Lock()
	defer cloud.mu.Unlock()
	deletes := 0
	for _, op := range cloud.log[applied:] {
		switch {
		case strings.HasPrefix(op, "Get"), strings.HasPrefix(op, "List"), strings.HasPrefix(op, "Find"), op == "SetToken":
		case strings.HasPrefix(op, "Delete"), strings.HasPrefix(op, "Release"), strings.HasPrefix(op, "Detach"):
			deletes++
		default:
			t.Errorf("the destroy and its next step sent %s: a step that keeps the consent sends only the deletes the person approved", op)
		}
	}
	if deletes == 0 {
		t.Fatalf("the world recorded no delete of the destroy (%v): the check sees nothing", cloud.log[applied:])
	}
	if len(cloud.vms) > made {
		t.Errorf("the world has %d VMs after the destroy and its step, %d before: the step made one", len(cloud.vms), made)
	}
}
