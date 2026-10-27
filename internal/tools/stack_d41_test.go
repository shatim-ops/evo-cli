// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// Grok live run on 1.3.0 (D41): a stack destroyed what it had not made. An
// object that was in the bucket before the stack was overwritten by its s3
// step and deleted by stack_destroy, without a word (G130-03; s3_put alone
// says «overwriting»); the secret of a path another stack held went with
// the second stack's destroy (G130-24). stack_plan refuses both before
// anything is created: what the stack would take over is not its own.
// The IDs are made up.

const stackSecretHost = "secretmanager.api.cloud.ru"

func planIn(t *testing.T, r *Registry, manifest string) (map[string]any, bool) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		t.Fatal(err)
	}
	text, isErr := r.handlers["stack_plan"](context.Background(), map[string]any{"manifest": m})
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("not JSON: %s", text)
	}
	return out, isErr
}

func TestStackPlan_RefusesObjectAlreadyInBucket(t *testing.T) {
	stackHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &faultAPI{cancel: cancel, made: []faultResource{
		{host: faultS3Host, path: "bucket", ID: "evo-bucket", Name: "evo-bucket", before: true},
		{host: faultS3Host, path: "object evo-bucket", ID: "site/index.html", Name: "site/index.html", before: true},
	}}
	r := faultAPIRegistry(t, a)
	_ = ctx
	out, isErr := planIn(t, r, `{"name":"web","s3":[{"bucket":"evo-bucket","key":"site/index.html","content":"new"}]}`)
	if !isErr || out["job_id"] != nil || out["reason"] != clerrors.CodeUsage ||
		!strings.Contains(out["error"].(string), "site/index.html") {
		t.Fatalf("an object already in the bucket must refuse the plan: %v", out)
	}
	// A key the bucket does not have plans as before.
	out, isErr = planIn(t, r, `{"name":"web","s3":[{"bucket":"evo-bucket","key":"site/new.html","content":"new"}]}`)
	if isErr || out["status"] != "planned" {
		t.Fatalf("a new key must plan: %v", out)
	}
}

func TestStackPlan_RefusesSecretPathTaken(t *testing.T) {
	stackHome(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	a := &faultAPI{cancel: cancel, made: []faultResource{
		{host: stackSecretHost, path: "/v2/secret", ID: "00000000-0000-4000-8000-0000000000a1", Name: "app/db", before: true},
	}}
	r := faultAPIRegistry(t, a)
	_ = ctx
	out, isErr := planIn(t, r, `{"name":"two","secrets":[{"path":"app/db","value":"x"}]}`)
	if !isErr || out["job_id"] != nil || !strings.Contains(out["error"].(string), "app/db") {
		t.Fatalf("a secret path that exists must refuse the plan: %v", out)
	}

	// Another stack's job holds a path it created (the cloud may not show
	// it yet, or a destroy of it is pending): the path is not free either.
	held := planJob(t, `{"name":"one","secrets":[{"path":"app/cache","value":"y"}]}`)
	held.Steps[0].Status, held.Status = "done", "done"
	held.Outputs[held.Steps[0].Key] = "ok"
	if err := held.Save(); err != nil {
		t.Fatal(err)
	}
	out, isErr = planIn(t, r, `{"name":"two","secrets":[{"path":"app/cache","value":"x"}]}`)
	if !isErr || out["job_id"] != nil || !strings.Contains(out["error"].(string), held.ID) {
		t.Fatalf("a secret path held by another stack must refuse the plan and name the job: %v", out)
	}
	// A destroyed job holds nothing.
	held.Status = "destroyed"
	if err := held.Save(); err != nil {
		t.Fatal(err)
	}
	if out, isErr = planIn(t, r, `{"name":"two","secrets":[{"path":"app/cache","value":"x"}]}`); isErr {
		t.Fatalf("a path of a destroyed job is free: %v", out)
	}
}

// A step whose create the cloud refused on purpose (NOT_FOUND of a zone,
// USAGE, a 4xx) was retried up to maxStepAttempts on every stack_status,
// and stack_status showed it pending without the reason (G130-04). It is
// a terminal error now, named with its reason; and a refused create is not
// reconciled by name: the resource of that name is not the step's (G130-24).
func TestStackAdvance_RefusedStepIsTerminal(t *testing.T) {
	for name, answer := range map[string]string{
		"NOT_FOUND": errResult(clerrors.New(clerrors.CodeNotFound, "DNS zone z-1 is in neither the public nor the private zone list", "")),
		"USAGE":     usageT("record_type %q is not one of …", "record_type %q не из …", "x"),
		"409":       errResult(errors.New("create record: dns API 409: already exists")),
	} {
		t.Run(name, func(t *testing.T) {
			stackHome(t)
			r := newTestServer()
			j := planJob(t, `{"name":"d","dns":{"zone_id":"z-1","name":"www","target":"203.0.113.10"}}`)
			var calls int32
			r.handlers["dns_record_create"] = func(context.Context, map[string]any) (string, bool) {
				atomic.AddInt32(&calls, 1)
				return answer, true
			}
			// The name is free before the step, and taken after its refusal:
			// a reconcile by name would adopt this record.
			var lists int32
			r.handlers["dns_records"] = func(context.Context, map[string]any) (string, bool) {
				if atomic.AddInt32(&lists, 1) == 1 {
					return toJSON(map[string]any{"records": []any{}}), false
				}
				return toJSON(map[string]any{"records": []any{map[string]any{"id": "rec-foreign", "name": "www", "type": "a"}}}), false
			}
			got := callTool(t, r.handleStackApply, map[string]any{"job_id": j.ID, "confirmed": true})
			if got["status"] != "error" {
				t.Fatalf("a refused step must end the job: %v", got)
			}
			if n := atomic.LoadInt32(&calls); n != 1 {
				t.Errorf("the refused create ran %d times, want 1", n)
			}
			job, err := stack.LoadJob(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if out := job.Outputs["dns"]; out != "" {
				t.Errorf("a refused step adopted %q by name", out)
			}
			details, _ := got["details"].(map[string]any)
			if d, _ := details["dns"].(string); d == "" {
				t.Errorf("stack_status/apply must name the step's reason in details: %v", got)
			}
		})
	}
}

// A lost answer (5xx, no connection) still retries and reconciles by name:
// the create may have gone through.
func TestStackAdvance_LostAnswerStillRetries(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"d","dns":{"zone_id":"z-1","name":"www","target":"203.0.113.10"}}`)
	var calls int32
	r.handlers["dns_record_create"] = func(context.Context, map[string]any) (string, bool) {
		atomic.AddInt32(&calls, 1)
		return errResult(errors.New("create record: dns API 502: bad gateway")), true
	}
	r.handlers["dns_records"] = fakeOK(map[string]any{"records": []any{}})
	got := callTool(t, r.handleStackApply, map[string]any{"job_id": j.ID, "confirmed": true})
	if got["status"] == "error" {
		t.Fatalf("a lost answer must stay retryable: %v", got)
	}
	if attempts, _ := got["attempts"].(map[string]any); attempts["dns"] != float64(1) {
		t.Errorf("stack_status must count the attempts of a retried step: %v", got)
	}
}

// stack_destroy of a destroyed job asked for consent again (G130-31). Its
// resources are gone: the answer is already_destroyed, without the
// question and without a call (D26). With confirmed=true it runs the
// teardown again, to re-check deletions still in progress.
func TestStackDestroy_DestroyedJobAnswersAlreadyDestroyed(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"g","secrets":[{"path":"a","value":"b"}]}`)
	j.Steps[0].Status, j.Status = "done", "destroyed"
	j.Outputs[j.Steps[0].Key] = "ok"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	var deletes int32
	r.handlers["secret_delete"] = func(context.Context, map[string]any) (string, bool) {
		atomic.AddInt32(&deletes, 1)
		return toJSON(map[string]any{"status": "already_deleted"}), false
	}
	text, isErr := r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID})
	if isErr || !strings.Contains(text, `"already_destroyed"`) || strings.Contains(text, "confirmation_required") {
		t.Fatalf("a destroyed job: want already_destroyed without the question, got %s", text)
	}
	if n := atomic.LoadInt32(&deletes); n != 0 {
		t.Errorf("already_destroyed called delete %d times", n)
	}
}
