// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// everyStep is a manifest with a step of each kind a stack makes.
const everyStep = `{"name":"s","vpc":{"name":"v"},"postgres":{"name":"p"},"kafka":{"name":"k"},"redis":{"name":"r"},"k8s":{"name":"k8"},"registry":{"name":"reg"},"snat":{"name":"sn"},"vm":{"name":"vm"},"nlb":{"name":"lb","port":80},"dns":{"zone_id":"z","name":"www"},"secrets":[{"path":"a","value":"b"}],"s3":[{"bucket":"b","key":"k","content":"c"}]}`

// stepShape is a state a step of a job is saved in before stack_apply
// goes on with it: the states advance leaves and a cut-off call leaves.
type stepShape struct {
	name string
	set  func(st *stack.Step)
}

var stepShapes = []stepShape{
	{"pending", func(*stack.Step) {}},
	{"pending with an id", func(st *stack.Step) { st.ResourceID = "00000000-0000-4000-8000-0000000000a1" }},
	{"cut off with an id", func(st *stack.Step) {
		st.ResourceID, st.Interrupted = "00000000-0000-4000-8000-0000000000a2", true
	}},
	{"cut off without an id", func(st *stack.Step) { st.Interrupted = true }},
	{"waiting", func(st *stack.Step) {
		st.Status, st.ResourceID, st.OpID = "waiting", "00000000-0000-4000-8000-0000000000a3", "op-1"
	}},
	{"waiting without an id", func(st *stack.Step) { st.Status, st.OpID = "waiting", "op-2" }},
	{"error", func(st *stack.Step) {
		st.Status, st.Reason, st.ResourceID = "error", "API_ERROR", "00000000-0000-4000-8000-0000000000a4"
	}},
	{"done", func(st *stack.Step) { st.Status, st.ResourceID = "done", "00000000-0000-4000-8000-0000000000a5" }},
}

// stepMadeAnswer is a successful answer of a create tool: what it made, under
// the field its declaration names (Decl.Created), and an operation.
func stepMadeAnswer(tool string, n int) map[string]any {
	id := fmt.Sprintf("00000000-0000-4000-8000-%012d", 900+n)
	out := map[string]any{"status": "ok", "operation_id": fmt.Sprintf("op-made-%d", n)}
	if path := decls[tool].Created.id; path != "" {
		keys := strings.Split(path, ".")
		m := out
		for _, k := range keys[:len(keys)-1] {
			next := map[string]any{}
			m[k] = next
			m = next
		}
		m[keys[len(keys)-1]] = id
	}
	return out
}

// continuationRows reads the rows of the question of stack_apply: the
// steps it names under key.
func continuationRows(t *testing.T, text, key string) []map[string]any {
	t.Helper()
	var m struct {
		Preview struct {
			Params []struct {
				Key   string          `json:"key"`
				Value json.RawMessage `json:"value"`
			} `json:"params"`
		} `json:"preview"`
	}
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("the question is not JSON: %v\n%s", err, text)
	}
	for _, p := range m.Preview.Params {
		if p.Key == key {
			var rows []map[string]any
			if err := json.Unmarshal(p.Value, &rows); err != nil {
				t.Fatalf("%s is not a list of rows: %v", key, err)
			}
			return rows
		}
	}
	return nil
}

// What the consent to stack_apply does on a job it goes on with is what
// the question said (class of #1748, external review #1769; CLAUDE.md §6,
// axes 1 and 4): a step the consent creates is in will_create and so
// priced in steps_cost; a step made already (will_finish), in error
// (failed_steps) or done is not created again — the VM of a provision
// step is gone on with by its id (D102), not made anew. A row of
// will_create marked after_cut_off_create is an explicit conditional
// promise: the consent may find what the cut-off create made instead.
//
// Each step of every kind is saved in each state a crank or a cut-off
// call leaves, with the steps before it done, in two worlds: the reads
// of the platform find the step's resource, or answer NOT_FOUND.
func TestStackApply_ConsentCreatesWhatTheQuestionNamed(t *testing.T) {
	stackHome(t)
	plan := planJob(t, everyStep)
	for i := range plan.Steps {
		for _, shape := range stepShapes {
			for _, gone := range []bool{false, true} {
				name := fmt.Sprintf("%s %s, reads gone %v", plan.Steps[i].Key, shape.name, gone)
				t.Run(name, func(t *testing.T) {
					stackHome(t)
					job := planJob(t, everyStep)
					for j, st := range job.Steps {
						if j == i {
							continue
						}
						st.Status, st.ResourceID = "done", fmt.Sprintf("00000000-0000-4000-8000-%012d", 100+j)
						job.Outputs[st.Key] = st.ResourceID
					}
					step := job.Steps[i]
					shape.set(step)
					if step.Status == "done" {
						job.Outputs[step.Key] = step.ResourceID
					}
					if step.Status == "error" {
						job.Status = "error"
					}
					if err := job.Save(); err != nil {
						t.Fatal(err)
					}
					checkContinuation(t, job, step, gone)
				})
			}
		}
	}
}

func checkContinuation(t *testing.T, job *stack.Job, step *stack.Step, gone bool) {
	r := newTestServer(t)
	apply := r.handlers["stack_apply"]
	// The platform: the step's tool records its calls and makes what it is
	// asked; every other tool answers as a read that finds nothing new.
	var creates []map[string]any
	n := 0
	// A list answers its array (Decl.Created), empty: the names are free,
	// and nothing is found by name, in both worlds.
	lists := map[string]string{}
	for _, d := range decls {
		if d.Created.list != "" {
			lists[d.Created.list] = d.Created.field
		}
	}
	for name := range r.handlers {
		switch field, isList := lists[name]; {
		case isList && name != step.Tool:
			r.handlers[name] = fakeOK(map[string]any{field: []any{}})
		case name == step.Tool:
			r.handlers[name] = func(_ context.Context, args map[string]any) (string, bool) {
				creates = append(creates, args)
				n++
				if n > 3 {
					// A crank that creates the step again and again would
					// not end: it is stopped here and fails below.
					return toJSON(map[string]any{"status": "error", "reason": "USAGE", "error": "the world refuses a fourth create"}), true
				}
				return toJSON(stepMadeAnswer(name, n)), false
			}
		case strings.HasPrefix(name, "stack_"):
		case gone:
			r.handlers[name] = func(context.Context, map[string]any) (string, bool) {
				return toJSON(map[string]any{"status": "error", "reason": "NOT_FOUND", "error": "not found"}), true
			}
		default:
			r.handlers[name] = fakeOK(map[string]any{})
		}
	}

	text, _ := apply(context.Background(), map[string]any{"job_id": job.ID})
	inRows := func(key string) (map[string]any, bool) {
		for _, row := range continuationRows(t, text, key) {
			if row["step"] == step.Key {
				return row, true
			}
		}
		return nil, false
	}
	create, toCreate := inRows("will_create")
	_, toFinish := inRows("will_finish")
	_, failed := inRows("failed_steps")
	if priced := strings.Contains(text, `"component": "`+step.Key+`"`); toCreate != priced {
		t.Errorf("%s: in will_create %v, priced in steps_cost %v; want both or neither:\n%s", step.Key, toCreate, priced, text)
	}
	if n := btoi(toCreate) + btoi(toFinish) + btoi(failed); n > 1 || (n == 0 && step.Status != "done") {
		t.Fatalf("the question names %s %s in %d of will_create, will_finish, failed_steps; want one:\n%s", step.Key, step.Status, n, text)
	}

	out, _ := apply(context.Background(), map[string]any{"job_id": job.ID, "confirmed": true})
	made := 0
	for _, args := range creates {
		// A repeat of provision with the step's VM goes on with it: no new VM.
		if step.Tool == "provision" && step.ResourceID != "" && args["vm_id"] == step.ResourceID {
			continue
		}
		made++
	}
	switch {
	case !toCreate && made > 0:
		t.Errorf("the consent created %s %d times, and the question did not name it in will_create:\n%s\nthe answer: %s", step.Key, made, text, out)
	case toCreate && made > 1:
		t.Errorf("the consent created %s %d times, the question named it once", step.Key, made)
	case toCreate && made == 0 && create["after_cut_off_create"] != true:
		t.Errorf("the question named %s in will_create, and the consent created nothing: the promise of a create is unconditional only without after_cut_off_create\nthe answer: %s", step.Key, out)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
