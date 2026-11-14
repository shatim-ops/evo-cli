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
	r := projectServer(t, "p-B")
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

// A repeat of stack_apply on a job that is done asks as it did — its exit
// code does not change before the major — and the question says that the
// consent creates nothing (unchanged: true, as deploy's of an unchanged
// compose file); with consent no step tool is called and the job stays
// done (release plan 1.9, «Повтор у выпущенных инструментов», п. 4).
func TestStackApply_JobDoneAsksAndSaysItCreatesNothing(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	j.Steps[0].Status, j.Status = "done", "done"
	j.Outputs[j.Steps[0].Key] = "ok"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r.handlers["secret_create"] = func(context.Context, map[string]any) (string, bool) {
		calls.Add(1)
		return toJSON(map[string]any{"status": "ok"}), false
	}
	text, isErr := r.handlers["stack_apply"](context.Background(), map[string]any{"job_id": j.ID})
	var m map[string]any
	_ = json.Unmarshal([]byte(text), &m)
	msg, _ := m["message"].(string)
	if !isErr || m["status"] != "confirmation_required" || m["unchanged"] != true || !strings.Contains(msg, "creates nothing") || strings.Contains(msg, "BILLABLE") {
		t.Errorf("the question of a job done: %s", text)
	}
	text, isErr = r.handlers["stack_apply"](context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	_ = json.Unmarshal([]byte(text), &m)
	if m["unchanged"] != true {
		t.Errorf("the answer with consent does not say the call created nothing: %s", text)
	}
	if after, err := stack.LoadJob(j.ID); isErr || err != nil || !after.Done() || calls.Load() != 0 {
		t.Errorf("with consent: %s; job %+v, %d step call(s)", text, after, calls.Load())
	}
	// A job not done is asked about as before.
	fresh := planJob(t, `{"name":"h","secrets":[{"path":"c","value":"d"}]}`)
	text, _ = r.handlers["stack_apply"](context.Background(), map[string]any{"job_id": fresh.ID})
	if strings.Contains(text, "unchanged") || !strings.Contains(text, "BILLABLE") {
		t.Errorf("the question of a job planned: %s", text)
	}
}

// The question of stack_apply names what the consent creates: every step
// not done yet, its tool and its arguments — the names of the resources
// among them (CLAUDE.md §6, axis 4); a step done is not asked about again,
// and the value of a secret is not in the question (CLAUDE.md §4). The
// question prices the steps to run.
func TestStackApply_TheQuestionNamesEveryStep(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","vpc":{"name":"net-g"},"secrets":[{"path":"app/db","value":"s3cr3t-value"}]}`)
	for _, st := range j.Steps {
		if st.Tool == "vpc_create" {
			st.Status = "done"
		}
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	text, _ := r.handlers["stack_apply"](context.Background(), map[string]any{"job_id": j.ID})
	if !strings.Contains(text, "app/db") || !strings.Contains(text, "secret_create") || !strings.Contains(text, secretNotShown) {
		t.Errorf("the question does not name the step to run: %s", text)
	}
	if strings.Contains(text, "s3cr3t-value") {
		t.Errorf("the value of a secret is in the question: %s", text)
	}
	if strings.Contains(text, "net-g") {
		t.Errorf("the question names a step done: %s", text)
	}
	// The price of what the consent pays for: the class of each step to
	// run, as stack_plan prices the job; a step done is paid already.
	var m struct {
		Preview struct {
			Params []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"params"`
		} `json:"preview"`
	}
	_ = json.Unmarshal([]byte(text), &m)
	var cost map[string]any
	for _, p := range m.Preview.Params {
		if p.Key == "steps_cost" {
			cost, _ = p.Value.(map[string]any)
		}
	}
	byComponent, _ := cost["by_component"].([]any)
	components := map[string]bool{}
	for _, l := range byComponent {
		if line, _ := l.(map[string]any); line != nil && line["class"] != nil {
			c, _ := line["component"].(string)
			components[c] = true
		}
	}
	if !components["secret:0"] || components["vpc"] {
		t.Errorf("the question's cost, want the class of secret:0 and not of vpc, done: %v", cost)
	}
}

// The body of an S3 object a stack puts is not in the question of
// stack_apply either («Риск», conclusion 213, У1): evo keeps it off the job
// file once the step is done, as a secret's value, and an object may be a
// .env with a key. The question names its size. The human preview is
// made from the same answer, so a value not in it is not shown there.
func TestStackApply_TheQuestionHidesAnObjectsBody(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","s3":[{"bucket":"b-g","key":".env","content":"API_KEY=s3cr3t-body"}]}`)
	text, _ := r.handlers["stack_apply"](context.Background(), map[string]any{"job_id": j.ID})
	if strings.Contains(text, "s3cr3t-body") {
		t.Errorf("the body of the object is in the question: %s", text)
	}
	if !strings.Contains(text, ".env") || !strings.Contains(text, "not shown: 19 bytes") {
		t.Errorf("the question does not name the object and the size of its body: %s", text)
	}
}

// A step whose resource is made already — waiting for it to be ready, or
// pending with its id (a VM the next crank finishes) — is not one the
// consent creates: the question names it apart, with its id, and its price
// is not in steps_cost (Астра, #1748). The consent pays for what it makes.
func TestStackApply_TheQuestionTellsAMadeResourceFromACreate(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"g","vpc":{"name":"net-g"},"secrets":[{"path":"app/db","value":"v"}]}`)
	for _, st := range j.Steps {
		if st.Tool == "vpc_create" {
			st.Status, st.ResourceID, st.OpID = "waiting", "00000000-0000-4000-8000-0000000000aa", "op-1"
		}
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	text, _ := r.handlers["stack_apply"](context.Background(), map[string]any{"job_id": j.ID})
	var m struct {
		Preview struct {
			Params []struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			} `json:"params"`
		} `json:"preview"`
	}
	_ = json.Unmarshal([]byte(text), &m)
	params := map[string]string{}
	for _, p := range m.Preview.Params {
		b, _ := json.Marshal(p.Value)
		params[p.Key] = string(b)
	}
	if strings.Contains(params["will_create"], "net-g") || !strings.Contains(params["will_create"], "app/db") {
		t.Errorf("will_create, want the secret and not the VPC made already: %s", params["will_create"])
	}
	if !strings.Contains(params["will_finish"], "vpc") || !strings.Contains(params["will_finish"], "00000000-0000-4000-8000-0000000000aa") {
		t.Errorf("will_finish, want the VPC made already with its id: %s", params["will_finish"])
	}
	if strings.Contains(params["steps_cost"], `"component":"vpc"`) || !strings.Contains(params["steps_cost"], `"component":"secret:0"`) {
		t.Errorf("steps_cost, want the secret's and not the VPC's: %s", params["steps_cost"])
	}
	if !strings.Contains(text, "will_finish are made already") {
		t.Errorf("the question does not say what will_finish is: %s", text)
	}
	// A pending step that holds its resource's id (a VM the next crank
	// finishes, a cut-off create whose resource was found) is made too.
	j2 := planJob(t, `{"name":"h","vpc":{"name":"net-h"}}`)
	j2.Steps[0].ResourceID = "00000000-0000-4000-8000-0000000000bb"
	if err := j2.Save(); err != nil {
		t.Fatal(err)
	}
	text, _ = r.handlers["stack_apply"](context.Background(), map[string]any{"job_id": j2.ID})
	if strings.Contains(text, "net-h") || !strings.Contains(text, "0000000000bb") {
		t.Errorf("a pending step with its resource's id, want it in will_finish: %s", text)
	}
}
