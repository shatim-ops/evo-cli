// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/stack"
)

// vmRow is one vm_list row; age is how long ago the VM was created.
func vmRow(id, name, state string, age time.Duration) map[string]any {
	return map[string]any{"id": id, "name": name, "state": state,
		"created_time": time.Now().Add(-age).UTC().Format(time.RFC3339)}
}

// Шаг vm стека: provision ответил ошибкой, но ВМ уже создана (error_creating,
// таймаут ожидания — ответ называет vm_id) или ответ потерян (ВМ видна в
// vm_list по имени и создана после старта шага). Раньше шаг повторял
// provision до 4 раз — до четырёх платных ВМ, которых stack_destroy не видит
// (матрица команд, 16.10.2026; QA r1 C2).
func TestStackVMStep_FailedProvisionIsNotRepeated(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
		answer   map[string]any // ответ-ошибка provision
		listed   []any          // что видит vm_list после вызова
		wantID   string
		wantStep string
	}{
		{"ответ называет ВМ", `{"name":"s","vm":{"name":"web"}}`,
			map[string]any{"status": "error", "vm_id": "vm-1", "error": "VM vm-1 entered state error_creating"},
			[]any{}, "vm-1", "error"},
		{"ответ потерян, ВМ видна по имени", `{"name":"s","vm":{"name":"web"}}`,
			map[string]any{"status": "error", "error": "API_UNREACHABLE: connection reset"},
			[]any{vmRow("vm-9", "web", "running", 0)}, "vm-9", "done"},
		// Без имени в манифесте ВМ называется "<стек>-vm", а не "evo-<время>"
		// на каждый вызов — иначе после потерянного ответа её не найти.
		{"ВМ без имени, ответ потерян", `{"name":"s","vm":{}}`,
			map[string]any{"status": "error", "error": "API_UNREACHABLE: connection reset"},
			[]any{vmRow("vm-7", "s-vm", "running", 0)}, "vm-7", "done"},
		// Найденная ВМ в error_creating не становится done (ревью Grok на #64):
		// она записывается в шаг для stack_destroy, job — в error.
		{"ответ потерян, ВМ в error_creating", `{"name":"s","vm":{"name":"web"}}`,
			map[string]any{"status": "error", "error": "API_UNREACHABLE: connection reset"},
			[]any{vmRow("vm-5", "web", "error_creating", 0)}, "vm-5", "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stackHome(t)
			r := newTestServer(t)
			j := planJob(t, tc.manifest)
			var creates atomic.Int32
			r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
				creates.Add(1)
				return toJSON(tc.answer), true
			}
			r.handlers["vm_list"] = fakeOK(map[string]any{"vms": tc.listed})
			r.gated("stack_apply", r.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
			for range 4 {
				r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
			}
			if n := creates.Load(); n != 1 {
				t.Fatalf("provision вызван %d раз, want 1 (лишние платные ВМ)", n)
			}
			loaded, err := stack.LoadJob(j.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got := loaded.Steps[0].ResourceID; got != tc.wantID {
				t.Fatalf("ResourceID шага vm = %q, want %q: stack_destroy не удалит ВМ", got, tc.wantID)
			}
			if got := loaded.Steps[0].Status; got != tc.wantStep {
				t.Fatalf("статус шага vm = %q, want %q", got, tc.wantStep)
			}
		})
	}
}

// В проекте уже есть ВМ пользователя с тем же именем, созданная до этого job;
// provision падает без ответа. Стек не должен её усыновить, а stack_destroy —
// удалить (ревью Grok на #64; класс QA R3/R8).
func TestStackVMStep_ForeignVMWithSameNameIsNotAdopted(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
	r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
		return toJSON(map[string]any{"status": "error", "error": "QUOTA_EXCEEDED: no vCPU quota"}), true
	}
	r.handlers["vm_list"] = fakeOK(map[string]any{"vms": []any{vmRow("users-vm", "web", "running", 3*time.Hour)}})
	var destroyed []string
	r.handlers["destroy"] = func(_ context.Context, args map[string]any) (string, bool) {
		id, _ := args["vm_id"].(string)
		destroyed = append(destroyed, id)
		return toJSON(map[string]any{"status": "deleted"}), false
	}
	r.gated("stack_apply", r.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	for range 4 {
		r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
	}
	loaded, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Steps[0].ResourceID; got == "users-vm" {
		t.Fatal("стек усыновил чужую ВМ с тем же именем")
	}
	r.gated("stack_destroy", r.planStackDestroy)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	for _, id := range destroyed {
		if id == "users-vm" {
			t.Fatal("stack_destroy удалил чужую ВМ с тем же именем")
		}
	}
}

// Шаг ВМ, у которого есть своя ВМ (D69: поднималась, когда кончился предел
// вызова), повторяет provision с её vm_id (D102). Её больше нет — и на месте
// появилась другая ВМ с тем же именем, созданная позже старта шага. Шаг не
// подхватывает чужую ВМ по имени и не повторяет provision: отказ ресурса
// шага окончательный с первого раза (Астра, остатки #1766; строка #1788).
func TestStackVMStep_TheStepsVMGoneIsFinalAndNotFoundByName(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
	j.Steps[0].ResourceID, j.Steps[0].StartedAt = "vm-1", time.Now().Add(-time.Minute)
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
		calls.Add(1)
		return toJSON(map[string]any{"status": "error", "reason": "VM_NOT_FOUND",
			"error": "the VM the call went on with is gone; no new one was made"}), true
	}
	r.handlers["vm_list"] = fakeOK(map[string]any{"vms": []any{vmRow("vm-2", "web", "running", 0)}})
	r.gated("stack_apply", r.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	for range 4 {
		r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("provision вызван %d раз, want 1: отказ ресурса шага окончательный", n)
	}
	loaded, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	st := loaded.Steps[0]
	if st.ResourceID != "vm-1" || st.Status != "error" || st.Reason != "VM_NOT_FOUND" || !loaded.Failed() {
		t.Errorf("шаг %+v, задание %s: want error VM_NOT_FOUND с vm-1, не vm-2", st, loaded.Status)
	}
}

// Отказ NOT_FOUND шага с ВМ — не о ВМ: образ недоступен, ВМ шага на месте.
// Отказ окончательный, но текст не говорит «ресурс шага пропал»: модель
// спланировала бы стек заново при живой платной ВМ («Риск», 222, У1).
func TestStackVMStep_ANotFoundAboutAnotherThingDoesNotSayTheVMIsGone(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"s","vm":{"name":"web"}}`)
	j.Steps[0].ResourceID, j.Steps[0].StartedAt = "vm-1", time.Now().Add(-time.Minute)
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
		return toJSON(map[string]any{"status": "error", "reason": "NOT_FOUND", "error": "image ubuntu is not available to the project"}), true
	}
	r.handlers["vm_list"] = fakeOK(map[string]any{"vms": []any{vmRow("vm-1", "web", "creating", 0)}})
	r.gated("stack_apply", r.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	loaded, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Failed() || strings.Contains(loaded.Error, "gone") || !strings.Contains(loaded.Error, "vm-1") {
		t.Errorf("задание %s: %q, want ошибку, которая называет vm-1 и не говорит «gone»", loaded.Status, loaded.Error)
	}
}

// Шаг, ждущий свою операцию, знает id своего ресурса: по концу операции
// имя его не заменяет, даже если список первым отдаёт другой ресурс с тем
// же именем («Риск», 222, У2): иначе следующие шаги строились бы в чужой
// сети, а stack_destroy удалил бы её.
func TestStackStep_AnOperationsEndKeepsTheStepsOwnID(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"s","vpc":{"name":"net"}}`)
	j.Steps[0].Status, j.Steps[0].ResourceID, j.Steps[0].OpID = "waiting", "vpc-A", "op-1"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r.handlers["vpc_operation"] = fakeOK(map[string]any{"done": true})
	r.handlers["vpc_list"] = fakeOK(map[string]any{"vpcs": []any{
		map[string]any{"id": "vpc-B", "name": "net"}, map[string]any{"id": "vpc-A", "name": "net"}}})
	r.gated("stack_apply", r.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	loaded, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st := loaded.Steps[0]; st.ResourceID != "vpc-A" || loaded.Outputs["vpc"] != "vpc-A" {
		t.Errorf("шаг %s с %s, outputs.vpc %s: want vpc-A, свою", st.Status, st.ResourceID, loaded.Outputs["vpc"])
	}
}

// Шаг PostgreSQL ждёт готовности своего кластера A; чтение A отвечает
// NOT_FOUND, в списке только чужой B того же имени (Астра, #1829). Один
// промах — не отказ: чтение после создания может отставать. Промахи подряд
// до предела — окончательный исход: шаг и задание в ошибке, причина
// названа, B не подхвачен, create не зовётся.
func TestStackStep_TheWaitedResourceNotFoundEndsTheStep(t *testing.T) {
	stackHome(t)
	r := newTestServer(t)
	j := planJob(t, `{"name":"probe","postgres":{"name":"same"}}`)
	j.Steps[0].Status, j.Steps[0].ResourceID = "waiting", "cl-A"
	if err := j.Save(); err != nil {
		t.Fatal(err)
	}
	r.handlers["pg_get"] = func(context.Context, map[string]any) (string, bool) {
		return toJSON(map[string]any{"status": "error", "reason": "NOT_FOUND", "error": "cluster not found"}), true
	}
	r.handlers["pg_list"] = fakeOK(map[string]any{"clusters": []any{map[string]any{"id": "cl-B", "name": "same"}}})
	r.handlers["pg_create"] = func(context.Context, map[string]any) (string, bool) {
		t.Fatal("pg_create вызван: шаг создаёт заново")
		return "", true
	}
	// Частый опрос сразу после создания: промахов больше предела по счёту,
	// но время от первого промаха не вышло — задание не падает («Риск»,
	// 230, У2).
	r.gated("stack_apply", r.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	for range maxStepMissing + 1 {
		r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
	}
	loaded, err := stack.LoadJob(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Failed() || loaded.Steps[0].Missing < maxStepMissing {
		t.Fatalf("частый опрос до срока провалил задание или не считал промахи: %s %+v", loaded.Status, loaded.Steps[0])
	}
	// Срок от первого промаха вышел: следующий прогон кончает шаг.
	loaded.Steps[0].MissingSince = loaded.Steps[0].MissingSince.Add(-stepMissingFor)
	if err := loaded.Save(); err != nil {
		t.Fatal(err)
	}
	r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
	if loaded, err = stack.LoadJob(j.ID); err != nil {
		t.Fatal(err)
	}
	st := loaded.Steps[0]
	if !loaded.Failed() || st.Status != "error" || st.Reason != "NOT_FOUND" || st.ResourceID != "cl-A" || !strings.Contains(loaded.Error, "cl-A") {
		t.Errorf("шаг %s %s %s, задание %s %q: want ошибку NOT_FOUND, названную по cl-A", st.Status, st.Reason, st.ResourceID, loaded.Status, loaded.Error)
	}
	// Текст не утверждает, что ресурса нет: он говорит, что установлено,
	// чем проверить и что убирает stack_destroy («Риск», 230, У1).
	for _, want := range []string{"not found by its id", "pg_get", "stack_destroy"} {
		if !strings.Contains(loaded.Error, want) {
			t.Errorf("текст ошибки не называет %q: %s", want, loaded.Error)
		}
	}
	if strings.Contains(loaded.Error, "gone") {
		t.Errorf("текст утверждает, что ресурс пропал: %s", loaded.Error)
	}
}

// Шаг VPC ждёт свою операцию; она кончилась, а сети A шага в списке нет —
// только чужая B того же имени (Астра, #1830). Шаг не закрывается done с
// отсутствующим ресурсом: промахи подряд до предела — ошибка; A в списке —
// done с A.
func TestStackStep_AnOperationsEndChecksTheStepsResourceIsThere(t *testing.T) {
	for _, present := range []bool{false, true} {
		stackHome(t)
		r := newTestServer(t)
		j := planJob(t, `{"name":"probe","vpc":{"name":"same"}}`)
		j.Steps[0].Status, j.Steps[0].ResourceID, j.Steps[0].OpID = "waiting", "vpc-A", "op-old"
		if err := j.Save(); err != nil {
			t.Fatal(err)
		}
		listed := []any{map[string]any{"id": "vpc-B", "name": "same"}}
		if present {
			listed = append(listed, map[string]any{"id": "vpc-A", "name": "same"})
		}
		r.handlers["vpc_operation"] = fakeOK(map[string]any{"done": true})
		r.handlers["vpc_list"] = fakeOK(map[string]any{"vpcs": listed})
		r.gated("stack_apply", r.planStackApply)(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
		for range maxStepMissing {
			r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
		}
		loaded, err := stack.LoadJob(j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !present {
			// The time since the first miss has run out.
			loaded.Steps[0].MissingSince = loaded.Steps[0].MissingSince.Add(-stepMissingFor)
			if err := loaded.Save(); err != nil {
				t.Fatal(err)
			}
			r.handleStackStatus(context.Background(), map[string]any{"job_id": j.ID, "wait_seconds": float64(1)})
			if loaded, err = stack.LoadJob(j.ID); err != nil {
				t.Fatal(err)
			}
		}
		st := loaded.Steps[0]
		switch {
		case present && (st.Status != "done" || loaded.Outputs["vpc"] != "vpc-A"):
			t.Errorf("A в списке: шаг %s, outputs.vpc %s; want done с vpc-A", st.Status, loaded.Outputs["vpc"])
		case !present && (st.Status != "error" || st.Reason != "NOT_FOUND" || loaded.Outputs["vpc"] != ""):
			t.Errorf("A нет: шаг %s %s, outputs.vpc %q; want ошибку NOT_FOUND без выхода", st.Status, st.Reason, loaded.Outputs["vpc"])
		}
	}
}
