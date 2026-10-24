// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// stackCall runs a stack tool and decodes its answer.
func stackCall(t *testing.T, r *Registry, tool string, args map[string]any) (map[string]any, bool) {
	t.Helper()
	text, isErr := r.handlers[tool](context.Background(), args)
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		t.Fatalf("%s: not JSON: %s", tool, text)
	}
	return m, isErr
}

// G13-14 (Grok, wave 2): a synchronous step took the id of what it made
// from vm_id, id or path of the answer and wrote "ok" when none was there.
// snat_create answers nat_gateway.id: outputs.snat was "ok", stack_destroy
// refused "ok" as no id, and the billed SNAT gateway stayed. The steps run
// the real tools over the HTTP fault world here: the stack tests ran
// stand-ins answering a shape of their own (fakeOK), which hid it.
func TestStack_StepIDsAreReal(t *testing.T) {
	stackHome(t)
	a := &faultAPI{made: []faultResource{{host: faultS3Host, path: "bucket", ID: "evo-bucket", Name: "evo-bucket", before: true}}}
	r := faultAPIRegistry(t, a)
	j := planJob(t, `{"name":"st","snat":{"name":"evo-n","zone":"ru.AZ-1"},"secrets":[{"path":"evo-s","value":"x"}],"s3":[{"bucket":"evo-bucket","key":"site/k","content":"c"}]}`)

	if out, isErr := stackCall(t, r, "stack_apply", map[string]any{"job_id": j.ID, "confirmed": true}); isErr || out["status"] != "done" {
		t.Fatalf("stack_apply: %v", out)
	}
	job, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	billed := map[string]bool{}
	for _, b := range a.billed() {
		for _, n := range b.names {
			billed[n] = true
		}
	}
	want := map[string]func(string) bool{
		"snat":     func(v string) bool { return billed[v] }, // the gateway's id
		"secret:0": func(v string) bool { return v == "evo-s" },
		"s3:0":     func(v string) bool { return v == "site/k" },
	}
	for _, st := range job.Steps {
		if v := job.Outputs[st.Key]; !want[st.Key](v) {
			t.Errorf("step %s (%s): output %q names nothing it made", st.Key, st.Tool, v)
		}
	}

	if out, isErr := stackCall(t, r, "stack_destroy", map[string]any{"job_id": j.ID, "confirmed": true}); isErr || out["status"] != "destroyed" {
		t.Fatalf("stack_destroy: %v", out)
	}
	if left := a.billed(); len(left) > 0 {
		t.Errorf("stack_destroy left billed: %v", left)
	}
}

// A job saved before the fix holds "ok" for its SNAT gateway: destroy finds
// the gateway by the manifest's name and deletes it, and one that is gone
// already is deleted (D26) — the job ends destroyed, not destroy_failed for
// good.
func TestStack_DestroyOldOKOutput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		there bool
	}{{"found by name", true}, {"gone", false}} {
		t.Run(tc.name, func(t *testing.T) {
			stackHome(t)
			a := &faultAPI{}
			r := faultAPIRegistry(t, a)
			if tc.there {
				if out, _ := stackCall(t, r, "snat_create", map[string]any{"name": "evo-n", "zone": "ru.AZ-1", "confirmed": true}); out["status"] != "created" {
					t.Fatalf("snat_create: %v", out)
				}
			}
			j := planJob(t, `{"name":"old","snat":{"name":"evo-n","zone":"ru.AZ-1"}}`)
			j.Steps[0].Status = "done"
			j.Outputs["snat"] = "ok"
			if err := j.Save(); err != nil {
				t.Fatal(err)
			}
			if out, isErr := stackCall(t, r, "stack_destroy", map[string]any{"job_id": j.ID, "confirmed": true}); isErr || out["status"] != "destroyed" {
				t.Fatalf("stack_destroy: %v", out)
			}
			if left := a.billed(); len(left) > 0 {
				t.Errorf("stack_destroy left billed: %v", left)
			}
		})
	}
}

// stack_destroy deletes the resource of each step through the delete tool
// its create declares (createdIDs), naming it by the argument deleteTargets
// declares for that tool. It passed cluster_id to pg_delete, whose target is
// id: aliasArg took both, by luck.
func TestStack_DestroyCallsDeleteByItsTarget(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"s","vpc":{"name":"v"},"postgres":{"name":"p"},"kafka":{"name":"k"},"redis":{"name":"r"},"k8s":{"name":"k8"},"registry":{"name":"reg"},"snat":{"name":"sn"},"vm":{"name":"vm"},"nlb":{"name":"lb","port":80},"dns":{"zone_id":"z","name":"www"},"secrets":[{"path":"a","value":"b"}],"s3":[{"bucket":"b","key":"k","content":"c"}]}`)
	const id = "00000000-0000-4000-8000-0000000000aa"
	for _, st := range j.Steps {
		st.Status = "done"
		j.Outputs[st.Key] = id
	}
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	calls := map[string]map[string]any{}
	for tool := range deleteTargets {
		r.handlers[tool] = func(_ context.Context, args map[string]any) (string, bool) {
			calls[tool] = args
			return toJSON(map[string]any{"status": "deleted"}), false
		}
	}
	if out, isErr := stackCall(t, r, "stack_destroy", map[string]any{"job_id": j.ID, "confirmed": true}); isErr {
		t.Fatalf("stack_destroy: %v", out)
	}
	for _, st := range j.Steps {
		c, ok := createdIDs[st.Tool]
		if !ok || c.delete == "" {
			t.Errorf("step %s: %s declares no delete in createdIDs", st.Key, st.Tool)
			continue
		}
		args, ok := calls[c.delete]
		if !ok {
			t.Errorf("step %s: stack_destroy did not call %s", st.Key, c.delete)
			continue
		}
		dt := deleteTargets[c.delete]
		tl, _ := r.Lookup(c.delete)
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, need := range []string{dt.arg, dt.parent} {
			if v, _ := args[need].(string); need != "" && v == "" {
				t.Errorf("step %s: %s called without its target %q: %v", st.Key, c.delete, need, args)
			}
		}
		for k := range args {
			if _, ok := props[k]; !ok {
				t.Errorf("step %s: %s called with %q, which its schema has not", st.Key, c.delete, k)
			}
			if k != dt.arg && k != dt.parent && k != "confirmed" {
				t.Errorf("step %s: %s called with %q besides its target %q", st.Key, c.delete, k, dt.arg)
			}
		}
	}
}
