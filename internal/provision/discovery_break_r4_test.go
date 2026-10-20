// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

// Пустая страница при total больше уже прочитанного — список оборван.
// Ожидается ошибка и конечное число запросов, а не тихий успех.
func TestBreakR4_R4_2_EmptyPageWhileTotalRemains(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{"id": "00000000-0000-4000-8000-000000000001", "name": "a"}},
				"total": 2,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{},
			"total": 2,
		})
	})
	defer srv.Close()

	vms, err := a.ListVMsInProject(context.Background(), "00000000-0000-4000-8000-0000000000aa")
	if err == nil {
		t.Fatalf("empty page with total=2 returned %d VMs and a nil error", len(vms))
	}
	if calls > 3 {
		t.Fatalf("requests = %d, the walk is not bounded", calls)
	}
}

// Сервер игнорирует offset и повторяет первую страницу, total при этом
// больше числа различных id. Список молча короткий.
func TestBreakR4_R4_3_IgnoredOffsetIsError(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		items := make([]map[string]any, 100)
		for i := range items {
			items[i] = map[string]any{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", i), "name": "n"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": 300})
	})
	defer srv.Close()

	vms, err := a.ListVMsInProject(context.Background(), "00000000-0000-4000-8000-0000000000aa")
	if err == nil {
		t.Fatalf("offset ignored, total=300: got %d VMs and a nil error after %d requests", len(vms), calls)
	}
	if calls > 3 {
		t.Fatalf("requests = %d, the walk is not bounded", calls)
	}
}

// Пустая страница правил, пока offset меньше total.
func TestBreakR4_R4_4_SGRulesEmptyPageWhileTotalRemains(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{{"id": "00000000-0000-4000-8000-000000000001", "direction": "ingress", "port_range": "80:80"}},
				"total": 5,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{}, "total": 5})
	})
	defer srv.Close()

	rules, err := a.ListSecurityGroupRules(context.Background(), "00000000-0000-4000-8000-0000000000bb")
	if err == nil {
		t.Fatalf("empty rules page with total=5 returned %d rules and a nil error", len(rules))
	}
	if calls > 3 {
		t.Fatalf("requests = %d", calls)
	}
}

// После sgRulesMaxPages полный список не собран, а ошибка не возвращается.
func TestBreakR4_R4_5_SGRulesMaxPagesIsError(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", calls), "direction": "ingress"}},
			"total": 1 << 30,
		})
	})
	defer srv.Close()

	rules, err := a.ListSecurityGroupRules(context.Background(), "00000000-0000-4000-8000-0000000000bb")
	if err == nil {
		t.Fatalf("after %d pages got %d rules and a nil error; total was not reached", calls, len(rules))
	}
	if calls != sgRulesMaxPages {
		t.Fatalf("requests = %d, want %d", calls, sgRulesMaxPages)
	}
}
