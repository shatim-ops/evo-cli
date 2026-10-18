// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
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
			r := newTestServer()
			j := planJob(t, tc.manifest)
			var creates atomic.Int32
			r.handlers["provision"] = func(context.Context, map[string]any) (string, bool) {
				creates.Add(1)
				return toJSON(tc.answer), true
			}
			r.handlers["vm_list"] = fakeOK(map[string]any{"vms": tc.listed})
			r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
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
	r := newTestServer()
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
	r.handleStackApply(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
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
	r.handleStackDestroy(context.Background(), map[string]any{"job_id": j.ID, "confirmed": true})
	for _, id := range destroyed {
		if id == "users-vm" {
			t.Fatal("stack_destroy удалил чужую ВМ с тем же именем")
		}
	}
}
