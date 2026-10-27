// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

func projectServer(project string) *Registry {
	return New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: config.Config{ProjectID: project}}, nil
	})
}

// stack_plan records the server's project in the job. A VPC plans without
// a read; a secret path is checked in the Secret Manager (D41).
func TestStackPlan_RecordsProject(t *testing.T) {
	stackHome(t)
	r := projectServer("p-A")
	got := callTool(t, r.handleStackPlan, map[string]any{"manifest": map[string]any{
		"name": "g", "vpc": map[string]any{"name": "v"},
	}})
	if got["project_id"] != "p-A" {
		t.Fatalf("plan answer = %v, want project_id p-A", got)
	}
	j, err := stack.LoadJob(stringArg(got, "job_id"))
	if err != nil || j.ProjectID != "p-A" {
		t.Fatalf("job = %+v %v, want project p-A", j, err)
	}
}

// A job planned in project A on a server restarted for project B: apply,
// status and destroy refuse, calling no step tool. A destroy in B used to
// read every 404 as "already gone" and mark the job destroyed while its
// resources in A stayed billed (QA P2).
func TestStackTools_RefuseJobOfAnotherProject(t *testing.T) {
	stackHome(t)
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	j.ProjectID = "p-A"
	j.Status = "running"
	j.Outputs["secret:0"] = "a"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r := projectServer("p-B")
	var calls atomic.Int32
	for _, name := range []string{"secret_create", "secret_delete", "secret_get", "secret_list"} {
		r.handlers[name] = func(context.Context, map[string]any) (string, bool) {
			calls.Add(1)
			return toJSON(map[string]any{"status": "ok"}), false
		}
	}
	for name, h := range map[string]Handler{
		"stack_apply":   r.handleStackApply,
		"stack_status":  r.handleStackStatus,
		"stack_destroy": r.handleStackDestroy,
	} {
		text, isErr := h(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true, "wait_seconds": float64(1)})
		if !isErr || !strings.Contains(text, "p-A") {
			t.Errorf("%s = %s (error %v), want an error naming project p-A", name, text, isErr)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Errorf("step tools called %d times, want 0", n)
	}
	again, err := stack.LoadJob(j.ID)
	if err != nil || again.Status != "running" {
		t.Errorf("job after the refusals = %+v %v, want running", again, err)
	}
}
