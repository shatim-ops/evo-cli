// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/stack"
)

// stackHome points the job store (~/.evo/stack-jobs) at a temp dir.
func stackHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func planJob(t *testing.T, manifest string) *stack.Job {
	t.Helper()
	var m stack.Manifest
	if err := json.Unmarshal([]byte(manifest), &m); err != nil {
		t.Fatal(err)
	}
	j := stack.Plan(m)
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	return j
}

func fakeOK(v map[string]any) Handler {
	return func(context.Context, map[string]any) (string, bool) { return toJSON(v), false }
}

// Этап 3 поставил гейты на provision и s3_put, а шаги vm и s3 стека
// вызывали их без confirmed: каждый стек с ВМ или объектом S3 получал
// confirmation_required, 4 попытки и status=error — уже создав остальное.
func TestQA_StackGatedStepsAreConfirmed(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"s","vpc":{"name":"v"},"postgres":{"name":"p"},"kafka":{"name":"k"},"redis":{"name":"r"},"k8s":{"name":"k8"},"registry":{"name":"reg"},"snat":{"name":"sn"},"vm":{"name":"vm"},"nlb":{"name":"lb","port":80},"dns":{"zone_id":"z","name":"www"},"secrets":[{"path":"a","value":"b"}],"s3":[{"bucket":"b","key":"k","content":"c"}]}`)
	for _, st := range j.Steps {
		tl, ok := r.Lookup(st.Tool)
		if !ok {
			t.Fatalf("step %s: unknown tool %s", st.Key, st.Tool)
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for k := range st.Args {
			if _, ok := props[k]; !ok {
				t.Errorf("step %s: %s has no argument %q", st.Key, st.Tool, k)
			}
		}
		if _, gated := props["confirmed"]; gated && st.Args["confirmed"] != true {
			t.Errorf("step %s: %s is gated, the step does not pass confirmed=true", st.Key, st.Tool)
		}
	}

	// A job planned before the fix (persisted without the flag) still runs:
	// the real gates answer confirmation_required without it.
	old := planJob(t, `{"name":"old","vm":{"name":"vm1"},"s3":[{"bucket":"b","key":"k","content":"c"}]}`)
	for _, st := range old.Steps {
		delete(st.Args, "confirmed")
	}
	if err := old.Save(); err != nil {
		t.Fatal(err)
	}
	var gotVM, gotS3 map[string]any
	// The gate of provision, on a configuration it can create a VM with
	// (without one it refuses before the gate).
	const id = "00000000-0000-4000-8000-000000000001"
	inner := New(func(map[string]any) (*engine.Engine, error) {
		return &engine.Engine{Config: config.Config{ProjectID: id, Region: id, ImageID: id}}, nil
	}).handlers["provision"]
	r.handlers["provision"] = func(ctx context.Context, a map[string]any) (string, bool) {
		gotVM = a
		if text, isErr := inner(ctx, map[string]any{"name": a["name"]}); !strings.Contains(text, "confirmation_required") || !isErr {
			t.Errorf("provision without confirmed must ask: %s", text)
		}
		return toJSON(map[string]any{"status": "ok", "vm_id": "vm-1"}), false
	}
	r.handlers["s3_put"] = func(_ context.Context, a map[string]any) (string, bool) {
		gotS3 = a
		return toJSON(map[string]any{"status": "ok"}), false
	}
	text, isErr := r.handleStackApply(context.Background(), map[string]any{"job_id": old.ID, "confirmed": true})
	if isErr || gotVM["confirmed"] != true || gotS3["confirmed"] != true {
		t.Fatalf("apply: %s; provision args %v, s3_put args %v", text, gotVM, gotS3)
	}
}

// nlb.target, которого нет в стеке, или цикл (target "lb": tg ждёт lb, lb
// ждёт tg) — шаг не стартует никогда: стек создаёт остальное и навсегда
// остаётся running. DNS без nlb ждёт несуществующий lb.
func TestQA_StackPlanRejectsBrokenDependencies(t *testing.T) {
	home := stackHome(t)
	// ВМ стека проверяется на плане (образ и зона, release audit L-36):
	// нужен поддельный compute API.
	r := raRegistry(t, &raAPI{}, raCloud{subnets: raSubnets()}, nil)
	for _, m := range []string{
		`{"name":"a","postgres":{"name":"p"},"nlb":{"name":"lb","port":80,"target":"web"}}`,
		`{"name":"b","vm":{"name":"v","image":"Ubuntu-24.04","zone":"ru.AZ-1"},"nlb":{"name":"lb","port":80,"target":"lb"}}`,
		`{"name":"c","postgres":{"name":"p"},"nlb":{"name":"lb","port":80}}`,
		`{"name":"d","vm":{"name":"v","image":"Ubuntu-24.04","zone":"ru.AZ-1"},"dns":{"zone_id":"z","name":"www"}}`,
	} {
		var manifest map[string]any
		_ = json.Unmarshal([]byte(m), &manifest)
		got := callTool(t, r.handleStackPlan, map[string]any{"manifest": manifest})
		if got["status"] != "error" {
			t.Errorf("%s: planned a stack that can never finish: %v", m, got)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(home, ".evo", "stack-jobs")); len(entries) != 0 {
		t.Errorf("rejected plans were saved: %d jobs", len(entries))
	}

	var manifest map[string]any
	_ = json.Unmarshal([]byte(`{"name":"ok","vm":{"name":"v","image":"Ubuntu-24.04","zone":"ru.AZ-1"},"nlb":{"name":"lb","port":80},"dns":{"zone_id":"z","name":"www"}}`), &manifest)
	if got := callTool(t, r.handleStackPlan, map[string]any{"manifest": manifest}); got["status"] != "planned" {
		t.Errorf("valid stack: %v", got)
	}
}

// Два одновременных crank одного job (два HTTP-клиента; повтор опроса,
// пока первый ещё идёт) видели шаг pending и оба создавали ресурс.
func TestQA_StackConcurrentCranksCreateOnce(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"c","postgres":{"name":"p"}}`)
	var creates atomic.Int32
	r.handlers["pg_create"] = func(context.Context, map[string]any) (string, bool) {
		creates.Add(1)
		time.Sleep(50 * time.Millisecond)
		return toJSON(map[string]any{"status": "creating", "id": "pg-1", "operation_id": "op"}), false
	}
	r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{}})
	r.handlers["pg_get"] = fakeOK(map[string]any{"status": "CREATING"})

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
		}()
	}
	wg.Wait()
	if n := creates.Load(); n != 1 {
		t.Fatalf("pg_create called %d times, want 1 (duplicate billable cluster)", n)
	}
}

// stack_apply на уже снесённом job отвечал status=done: все шаги остались
// «done» с первого apply, ресурсов же нет.
func TestQA_StackApplyRefusesDestroyedJob(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"d","secrets":[{"path":"a","value":"b"}]}`)
	r.handlers["secret_create"] = fakeOK(map[string]any{"status": "created", "id": "sec-1"})
	r.handlers["secret_delete"] = fakeOK(map[string]any{"status": "deleted"})
	if text, isErr := r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true}); isErr {
		t.Fatal(text)
	}
	if text, isErr := r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true}); isErr {
		t.Fatal(text)
	}
	got := callTool(t, r.handleStackApply, map[string]any{"job_id": j.ID, "confirmed": true})
	if got["status"] == "done" || got["error"] == nil {
		t.Fatalf("apply after destroy: %v", got)
	}
}

// Неудачное удаление помечало job «destroyed»: stack_plan переставал
// предупреждать о живых ресурсах, а ответ был без isError.
func TestQA_StackDestroyFailureKeepsJobVisible(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"f","vpc":{"name":"v"}}`)
	j.Steps[0].Status, j.Steps[0].ResourceID, j.Status = "done", "vpc-1", "done"
	j.Outputs["vpc"] = "vpc-1"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r.handlers["vpc_delete"] = func(context.Context, map[string]any) (string, bool) {
		return errResult(errors.New("500 internal error")), true
	}
	text, isErr := r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if !isErr {
		t.Errorf("a failed teardown is not an error: %s", text)
	}
	if orphans := stack.FindResourcefulJobs("f", ""); len(orphans) != 1 {
		t.Errorf("the job still holding vpc-1 is hidden from stack_plan: %v (%s)", orphans, text)
	}
}

// Живой прогон 18.10 (run 39167011818): первый stack_destroy — destroyed
// (балансировщик и группа ещё удаляются), повторный, как советует note, —
// destroy_failed навсегда: destroy уже удалённой ВМ отвечает VM_NOT_FOUND
// «no VM … in project …; nothing was deleted», а «уже удалено» узнавалось
// только по тексту (404, NOT_FOUND, not found). Код ответа — тоже признак.
func TestQA_StackDestroyAgainVMGoneIsDeleted(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"g","vm":{"name":"g-vm","private":true}}`)
	for _, st := range j.Steps {
		st.Status = "done"
	}
	j.Status = "destroyed"
	j.Outputs["vm"] = "vm-1"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r.handlers["destroy"] = func(context.Context, map[string]any) (string, bool) {
		return errResult(clerrors.New(clerrors.CodeVMNotFound,
			"no VM vm-1 in project p; nothing was deleted", "List the project's VMs with vm_list (evo vm list).")), true
	}
	text, isErr := r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if isErr || !strings.Contains(text, `"destroyed"`) || !strings.Contains(text, "vm (already gone)") {
		t.Fatalf("a VM that is already gone failed the teardown: %s", text)
	}

	// Другая ошибка шага vm (облако не ответило) — по-прежнему сбой: признак
	// «уже удалено» — код ответа, а не любая ошибка destroy.
	r.handlers["destroy"] = func(context.Context, map[string]any) (string, bool) {
		return errResult(clerrors.New(clerrors.CodeAPIError, "destroy VM vm-1: API returned status 500", "")), true
	}
	text, isErr = r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	if !isErr || !strings.Contains(text, `"destroy_failed"`) {
		t.Fatalf("a failed VM deletion was taken for a deleted VM: %s", text)
	}
}

// job_id становится именем файла: "../x" читал JSON вне каталога заданий,
// и stack_apply исполнял его шаги.
func TestQA_StackJobIDCannotLeaveJobsDir(t *testing.T) {
	home := stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"t","secrets":[{"path":"a","value":"b"}]}`)
	data, _ := os.ReadFile(filepath.Join(home, ".evo", "stack-jobs", j.ID+".json"))
	if err := os.WriteFile(filepath.Join(home, "evil.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	r.handlers["secret_create"] = func(context.Context, map[string]any) (string, bool) {
		called = true
		return toJSON(map[string]any{"status": "created"}), false
	}
	for _, h := range []Handler{r.handleStackApply, r.handleStackStatus, r.handleStackDestroy} {
		got := callTool(t, h, map[string]any{"job_id": "../../evil", "confirmed": true, "wait_seconds": float64(1)})
		if got["status"] != "error" || !strings.Contains(got["error"].(string), "invalid job_id") {
			t.Errorf("traversal job_id accepted: %v", got)
		}
	}
	if called {
		t.Fatal("a job outside the jobs directory was executed")
	}
}

// stack_status с wait_seconds=1 спал между crank'ами 8 с без учёта бюджета:
// ответ через ~9 с вместо ~1 с.
func TestQA_StackStatusHonoursShortBudget(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"w","postgres":{"name":"p"}}`)
	j.Status = "running"
	j.Steps[0].Status, j.Steps[0].ResourceID = "waiting", "pg-1"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r.handlers["pg_get"] = fakeOK(map[string]any{"status": "CREATING"})
	start := time.Now()
	got := callTool(t, r.handleStackStatus, map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
	// wall-clock: guards a full crank pause (stackCrankInterval, 8 s) past wait_seconds=1: half of it.
	if d := time.Since(start); d > stackCrankInterval/2 {
		t.Fatalf("wait_seconds=1 took %v", d)
	}
	if got["status"] != "running" {
		t.Fatalf("status: %v", got)
	}
}

// stack_status is a read-only poll: on a planned job it must not start any
// step. It used to advance "planned" jobs, creating every billable resource
// of a stack_plan without stack_apply/confirmed=true (QA 15.10.2026, C1).
func TestQA_StackStatusDoesNotStartPlannedJob(t *testing.T) {
	stackHome(t)
	r := newTestServer()
	j := planJob(t, `{"name":"g","postgres":{"name":"p"},"secrets":[{"path":"a","value":"b"}]}`)
	var calls atomic.Int32
	for _, name := range []string{"pg_create", "secret_create", "pg_list", "pg_get"} {
		r.handlers[name] = func(context.Context, map[string]any) (string, bool) {
			calls.Add(1)
			return toJSON(map[string]any{"status": "created", "id": "x"}), false
		}
	}
	got := callTool(t, r.handleStackStatus, map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
	if n := calls.Load(); n != 0 {
		t.Fatalf("stack_status on a planned job called %d handlers, want 0", n)
	}
	if got["status"] != "planned" || !strings.Contains(stringArg(got, "note"), "stack_apply") {
		t.Fatalf("answer = %v, want planned with a note naming stack_apply", got)
	}
	again, err := stack.LoadJob(j.ID)
	if err != nil || again.Status != "planned" {
		t.Fatalf("job after stack_status = %+v %v, want planned", again, err)
	}
}
