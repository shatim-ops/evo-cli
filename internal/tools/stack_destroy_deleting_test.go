// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// stack_destroy answered status=destroyed while the cloud was still
// deleting some of the stack (deleting not empty): "deleted" must mean the
// resource is gone (axis 3 of CLAUDE.md §6). Until a destroy finds them
// gone, the answer is status=deleting with the next step, exit code 0; the
// job holds resources — stack_plan warns of it, stack_apply refuses it.
// External review 11.11, item (1); Risk conclusion 98.
func TestStackDestroy_DeletingUntilTheCloudFinishes(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	key := j.Steps[0].Key
	j.Steps[0].Status, j.Status = "done", "done"
	j.Outputs[key], j.Steps[0].ResourceID = "ok", "a"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	answer := "deleting"
	var deletes int32
	r.handlers["secret_delete"] = func(context.Context, map[string]any) (string, bool) {
		atomic.AddInt32(&deletes, 1)
		return toJSON(map[string]any{"status": answer}), false
	}
	destroy := func(confirmed bool) (map[string]any, bool) {
		t.Helper()
		text, isErr := r.gated("stack_destroy", r.planStackDestroy)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": confirmed})
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("%v: %s", err, text)
		}
		return m, isErr
	}

	m, isErr := destroy(true)
	if isErr || m["status"] != "deleting" || m["next_tool"] != "stack_destroy" || stringArg(m, "next") == "" {
		t.Fatalf("call 1, the cloud still deleting: want status=deleting with the next step, code 0; got %v (error %v)", m, isErr)
	}
	saved, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Status != "deleting" {
		t.Errorf("the job must be saved as deleting, not %q", saved.Status)
	}
	if refs := stack.FindResourcefulJobs("g", ""); len(refs) != 1 {
		t.Errorf("a job the cloud is still deleting holds resources: stack_plan must warn of it, got %v", refs)
	}
	if _, err := r.appliableJob(j.ID); err == nil {
		t.Error("stack_apply must refuse a job being torn down")
	}
	if m, _ := destroy(false); m["status"] != "confirmation_required" {
		t.Errorf("the preview of a job still being deleted asks again (the CLI's --yes goes on), got %v", m)
	}

	answer = "already_deleted"
	if m, isErr := destroy(true); isErr || m["status"] != "destroyed" || m["deleting"] != nil {
		t.Fatalf("call 2, all gone: want destroyed, got %v", m)
	}
	if m, _ := destroy(true); m["status"] != "already_destroyed" {
		t.Fatalf("call 3: want already_destroyed, got %v", m)
	}
	if n := atomic.LoadInt32(&deletes); n != 2 {
		t.Errorf("deletes %d, want 2", n)
	}
}

// A job saved by 1.8.0-rc.1 or earlier as destroyed with deleting not
// empty is a teardown not finished: it reads as deleting everywhere — the
// preview asks, stack_plan warns, the confirmed call re-checks.
func TestStackDestroy_OldDestroyedWithDeletingIsUnfinished(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	key := j.Steps[0].Key
	j.Steps[0].Status = "done"
	j.Status, j.Deleting = "destroyed", []string{key}
	j.Outputs[key], j.Steps[0].ResourceID = "ok", "a"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	if refs := stack.FindResourcefulJobs("g", ""); len(refs) != 1 {
		t.Errorf("an old job with deletions in progress holds resources, got %v", refs)
	}
	text, _ := r.gated("stack_destroy", r.planStackDestroy)(context.Background(), map[string]any{"job_id": j.ID})
	if !strings.Contains(text, "confirmation_required") {
		t.Errorf("preview: %s", text)
	}
	r.handlers["secret_delete"] = fakeOK(map[string]any{"status": "deleting"})
	got := callTool(t, r.gated("stack_destroy", r.planStackDestroy), map[string]any{"job_id": j.ID, "confirmed": true})
	if got["status"] != "deleting" {
		t.Errorf("re-check still deleting: %v", got)
	}
}

// A job destroyed with nothing left: stack_status answers destroyed with no
// next step, the other side of DeletionsPending (Risk conclusion 101). The
// deleting side and the preview: TestStackStatus_DeletingJobSaysWhatIsLeft-
// AndTheNextStep, TestStackDestroy_PreviewOfDeletingJobDoesNotSayDestroyed.
// The description names the deleting outcome (FB180-15).
func TestStackStatus_DestroyedHasNoNextStep(t *testing.T) {
	// Destroyed, nothing left: no next step.
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	j.Steps[0].Status, j.Status = "done", "destroyed"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	if got := callTool(t, r.handleStackStatus, map[string]any{"job_id": j.ID, "wait_seconds": float64(1)}); got["status"] != "destroyed" || got["next_tool"] != nil || got["deleting"] != nil {
		t.Errorf("stack_status of a destroyed job: want destroyed without a next step, got %v", got)
	}
	tl, _ := newTestServer(t).Lookup("stack_status")
	if !strings.Contains(tl.Description, "status=deleting") {
		t.Errorf("the stack_status description does not name the deleting outcome: %s", tl.Description)
	}
}
