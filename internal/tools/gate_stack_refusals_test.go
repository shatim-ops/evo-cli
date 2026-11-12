// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// What stack_apply and stack_destroy answer before the question — a
// refusal, or already_destroyed, which asks nothing — they answer the
// same with consent, and neither call runs a step tool or changes the job
// (the inventory of the gate, variant А: the planner refuses, the
// execution only checks again under the job's lock).
func TestGateStack_AnswerBeforeTheQuestionIsTheAnswerWithConsent(t *testing.T) {
	stackHome(t)
	const manifest = `{"name":"g","secrets":[{"path":"a","value":"b"}]}`
	foreign := planJob(t, manifest)
	foreign.ProjectID, foreign.Status = "p-A", "running"
	gone := planJob(t, manifest)
	gone.Steps[0].Status, gone.Status = "done", "destroyed"
	for _, j := range []*stack.Job{foreign, gone} {
		if err := j.Save(); err != nil {
			t.Fatal(err)
		}
	}
	r := projectServer("p-B")
	gone.ProjectID = "p-B"
	if err := gone.Save(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	for _, name := range []string{"secret_create", "secret_delete", "secret_get", "secret_list"} {
		r.handlers[name] = func(context.Context, map[string]any) (string, bool) {
			calls.Add(1)
			return toJSON(map[string]any{"status": "ok"}), false
		}
	}
	for _, c := range []struct {
		name, tool, id string
		status, reason string
	}{
		{"no job_id", "stack_apply", "", "error", "USAGE"},
		{"absent", "stack_apply", "stk-0123456789", "error", "NOT_FOUND"},
		{"of another project", "stack_apply", foreign.ID, "error", ""},
		{"destroyed", "stack_apply", gone.ID, "error", "USAGE"},
		{"no job_id", "stack_destroy", "", "error", "USAGE"},
		{"absent", "stack_destroy", "stk-0123456789", "error", "NOT_FOUND"},
		{"of another project", "stack_destroy", foreign.ID, "error", ""},
		{"destroyed", "stack_destroy", gone.ID, "already_destroyed", ""},
	} {
		var answers [2]string
		for i, confirmed := range []bool{false, true} {
			args := map[string]any{"confirmed": confirmed}
			if c.id != "" {
				args["job_id"] = c.id
			}
			text, isErr := r.handlers[c.tool](context.Background(), args)
			var m map[string]any
			if err := json.Unmarshal([]byte(text), &m); err != nil {
				t.Fatalf("%s, %s: %v: %s", c.tool, c.name, err, text)
			}
			if m["status"] != c.status || isErr != (c.status == "error") || (c.reason != "" && m["reason"] != c.reason) {
				t.Errorf("%s, %s (confirmed %v): %s, want %s %s", c.tool, c.name, confirmed, text, c.status, c.reason)
			}
			answers[i] = text
		}
		if answers[0] != answers[1] {
			t.Errorf("%s, %s: without consent\n%s\nwith confirmed=true\n%s", c.tool, c.name, answers[0], answers[1])
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("step tools called %d times, want 0", n)
	}
	for _, want := range []*stack.Job{foreign, gone} {
		got, err := stack.LoadJob(want.ID)
		if err != nil || got.Status != want.Status {
			t.Errorf("job %s after the calls: %+v %v, want status %s", want.ID, got, err, want.Status)
		}
	}
}
