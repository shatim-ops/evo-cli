// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

// finishedDeleteStatus are the statuses of a delete that say the resource
// is gone (axis 3, CLAUDE.md §6: «удалено» значит, что ресурса нет).
var finishedDeleteStatus = map[string]bool{"destroyed": true, "deleted": true, "already_deleted": true, "already_destroyed": true}

// goneButDeleting is what an answer of a delete says against itself: a
// finished status with resources the cloud is still deleting, or a
// resource in both "deleted" and "deleting".
func goneButDeleting(m map[string]any) string {
	var deleting []string
	if d, ok := m["deleting"].([]any); ok {
		for _, x := range d {
			deleting = append(deleting, fmt.Sprint(x))
		}
	}
	if len(deleting) == 0 {
		return ""
	}
	if st, _ := m["status"].(string); finishedDeleteStatus[st] {
		return fmt.Sprintf("status %q with %v still deleting", st, deleting)
	}
	if done, ok := m["deleted"].([]any); ok {
		for _, x := range done {
			for _, d := range deleting {
				if fmt.Sprint(x) == d {
					return fmt.Sprintf("%s is both deleted and deleting", d)
				}
			}
		}
	}
	return ""
}

// External review of 11.11 (GPT on 71afe86), п. 1: stack_destroy answered
// status "destroyed" while the cloud was still deleting a resource of the
// stack (its "deleting" not empty), and listed that resource in "deleted"
// too. An agent that reads «destroyed» takes the stack for gone, reuses
// its names and stops paying attention. «Destroyed» is said only when
// nothing is left: with a deletion in progress the answer is not finished
// (a status of its own and the step that checks it again — the next_tool
// it already gives), and a resource still deleting is not «deleted».
// TestStackDestroy_PendingDeletionsAreRechecked held the old answer
// (status destroyed with deleting): the fix changes it with this test.
func TestStackDestroy_DestroyedOnlyWhenNothingIsDeleting(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	key := j.Steps[0].Key
	j.Steps[0].Status = "done"
	j.Outputs[key] = "ok"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	answer := "deleting"
	r.handlers["secret_delete"] = func(context.Context, map[string]any) (string, bool) {
		return toJSON(map[string]any{"status": answer}), false
	}
	destroy := func() map[string]any {
		t.Helper()
		text, _ := r.gated("stack_destroy", r.planStackDestroy)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
		var m map[string]any
		if err := json.Unmarshal([]byte(text), &m); err != nil {
			t.Fatalf("%v: %s", err, text)
		}
		return m
	}
	m := destroy()
	if why := goneButDeleting(m); why != "" {
		t.Errorf("stack_destroy with a resource the cloud is still deleting: %s — want a status that is not finished, the resource only in deleting: %v", why, m)
	}
	if m["next_tool"] != "stack_destroy" {
		t.Errorf("a destroy in progress names no next step: %v", m)
	}
	// The cloud done: now it is destroyed, and nothing is deleting.
	answer = "already_deleted"
	if m := destroy(); m["status"] != "destroyed" || goneButDeleting(m) != "" {
		t.Errorf("the re-check found it gone: want destroyed, got %v", m)
	}
}
