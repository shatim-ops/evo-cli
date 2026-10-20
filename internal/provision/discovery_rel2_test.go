// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
)

// samePage answers every request with the same page of size distinct ids,
// as a server that ignores offset does.
func samePage(size int) []map[string]any {
	items := make([]map[string]any, size)
	for i := range items {
		items[i] = map[string]any{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", i), "name": "n", "direction": "ingress"}
	}
	return items
}

// offsetPage answers the page at the request's offset: distinct ids.
func offsetPage(r *http.Request, size int) []map[string]any {
	start, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	items := make([]map[string]any, size)
	for i := range items {
		items[i] = map[string]any{"id": fmt.Sprintf("00000000-0000-4000-8000-%012d", start+i), "name": "n", "direction": "ingress"}
	}
	return items
}

// Сервер игнорирует offset, total — ровно две страницы: вторая страница —
// повтор первой, и offset после неё равен total. Половина списка не
// выдаётся за полный (релизный аудит, REL-2).
func TestRel2_IgnoredOffsetTotalTwoPagesIsError(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"items": samePage(listPageSize), "total": 2 * listPageSize})
	})
	defer srv.Close()

	vms, err := a.ListVMsInProject(context.Background(), "00000000-0000-4000-8000-0000000000aa")
	if err == nil {
		t.Fatalf("offset ignored, total=%d: got %d VMs and a nil error", 2*listPageSize, len(vms))
	}
	if calls != 2 {
		t.Fatalf("requests = %d, want 2", calls)
	}
}

// То же для правил группы безопасности: по ним deploy и destroy решают,
// какое правило добавить или удалить.
func TestRel2_SGRulesIgnoredOffsetTotalTwoPagesIsError(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"items": samePage(sgRulesPageSize), "total": 2 * sgRulesPageSize})
	})
	defer srv.Close()

	rules, err := a.ListSecurityGroupRules(context.Background(), "00000000-0000-4000-8000-0000000000bb")
	if err == nil {
		t.Fatalf("offset ignored, total=%d: got %d rules and a nil error", 2*sgRulesPageSize, len(rules))
	}
	if calls != 2 {
		t.Fatalf("requests = %d, want 2", calls)
	}
}

// Обычный двухстраничный список (100 + 100 разных при total=200) читается
// целиком: проверка повтора не ломает нормальную пагинацию.
func TestRel2_TwoDistinctPagesRead(t *testing.T) {
	calls := 0
	srv, a := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"items": offsetPage(r, listPageSize), "total": 2 * listPageSize})
	})
	defer srv.Close()

	vms, err := a.ListVMsInProject(context.Background(), "00000000-0000-4000-8000-0000000000aa")
	if err != nil {
		t.Fatalf("two distinct pages: %v", err)
	}
	if len(vms) != 2*listPageSize || calls != 2 {
		t.Fatalf("got %d VMs in %d requests, want %d in 2", len(vms), calls, 2*listPageSize)
	}

	calls = 0
	rules, err := a.ListSecurityGroupRules(context.Background(), "00000000-0000-4000-8000-0000000000bb")
	if err != nil {
		t.Fatalf("two distinct rule pages: %v", err)
	}
	if len(rules) != 2*sgRulesPageSize || calls != 2 {
		t.Fatalf("got %d rules in %d requests, want %d in 2", len(rules), calls, 2*sgRulesPageSize)
	}
}
