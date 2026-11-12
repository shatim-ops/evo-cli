// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// stack_status read the raw job status: a job the cloud is still deleting
// answered without the steps being deleted and without the next step, and
// its description says to poll until done or error, which never come
// (axis 2 of CLAUDE.md §6). A job saved by 1.8.0-rc.1 as destroyed with
// deleting not empty answered destroyed, though the D96 migration says it
// reads as deleting. Fable delta audit 1.8.0, FB180-15.
func TestStackStatus_DeletingJobSaysWhatIsLeftAndTheNextStep(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
	}{
		{"deleting", stack.StatusDeleting},
		{"rc.1 destroyed with deleting", stack.StatusDestroyed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stackHome(t)
			r := newTestServer()
			j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
			key := j.Steps[0].Key
			j.Steps[0].Status = "done"
			j.Status, j.Deleting = tc.status, []string{key}
			j.Outputs[key], j.Steps[0].ResourceID = "ok", "a"
			if err := j.Save(); err != nil {
				t.Fatal(err)
			}
			text, isErr := r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": 1})
			var m map[string]any
			if err := json.Unmarshal([]byte(text), &m); err != nil {
				t.Fatalf("%v: %s", err, text)
			}
			if isErr || m["status"] != stack.StatusDeleting {
				t.Errorf("status: want deleting, got %v (error %v)", m["status"], isErr)
			}
			left, _ := m["deleting"].([]any)
			if !slices.Contains(left, any(key)) {
				t.Errorf("deleting: want the step still being deleted (%s), got %v", key, m["deleting"])
			}
			if m["next_tool"] != "stack_destroy" || stringArg(m, "next") == "" {
				t.Errorf("want next_tool stack_destroy and a next step, got next_tool=%v next=%v", m["next_tool"], m["next"])
			}
		})
	}
}

// The preview of a repeated stack_destroy said "stack ... was destroyed" of
// a job the cloud is still deleting. Fable delta audit 1.8.0, FB180-16.
func TestStackDestroy_PreviewOfDeletingJobDoesNotSayDestroyed(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	key := j.Steps[0].Key
	j.Steps[0].Status = "done"
	j.Status, j.Deleting = stack.StatusDeleting, []string{key}
	j.Outputs[key], j.Steps[0].ResourceID = "ok", "a"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	text, _ := r.gated("stack_destroy", r.planStackDestroy)(context.Background(), map[string]any{"job_id": j.ID})
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("%v: %s", err, text)
	}
	if m["status"] != "confirmation_required" {
		t.Fatalf("want the question again, got %v", m)
	}
	if msg := stringArg(m, "message"); strings.Contains(msg, "was destroyed") || strings.Contains(msg, "удалён") {
		t.Errorf("the preview of a job still being deleted must not say the stack was destroyed: %q", msg)
	}
}
