// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
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

// Project inventory is used to decide which resources still exist. A failure
// on a promised page must discard the rows already read, even on service 404.
func TestProjectInventoryRequiresCompleteList(t *testing.T) {
	const empty = `{"items":[],"total":0}`
	const first = `{"items":[{"id":"one"}],"total":2}`
	full := empty + strings.Repeat(" ", maxListBody-len(empty))
	for _, service := range []struct {
		path string
		read func(*CloudRuAdapter) (int, error)
	}{
		{"/api/v1/vms", func(a *CloudRuAdapter) (int, error) {
			rows, err := a.ListVMsInProject(context.Background(), "p")
			return len(rows), err
		}},
		{"/api/v1/security-groups", func(a *CloudRuAdapter) (int, error) {
			rows, err := a.ListSecurityGroups(context.Background(), "p")
			return len(rows), err
		}},
		{"/api/v1/floating-ips", func(a *CloudRuAdapter) (int, error) {
			rows, err := a.ListFloatingIPs(context.Background(), "p")
			return len(rows), err
		}},
	} {
		for _, tc := range []struct {
			name             string
			pages            []string
			lastStatus, rows int // rows = -1 for an incomplete or malformed answer
		}{
			{"empty envelope", []string{empty}, 200, 0},
			{"legacy first-page array", []string{`[]`}, 200, 0},
			{"complete two pages", []string{first, `{"items":[{"id":"two"}],"total":2}`}, 200, 2},
			{"exact size limit", []string{full}, 200, 0},
			{"past size limit", []string{full + " "}, 200, -1},
			{"no content", []string{""}, 204, -1},
			{"partial content", []string{empty}, 206, -1},
			{"null root", []string{`null`}, 200, -1},
			{"null collection", []string{`{"items":null,"total":0}`}, 200, -1},
			{"null row", []string{`{"items":[null],"total":1}`}, 200, -1},
			{"null row in array", []string{`[null]`}, 200, -1},
			{"missing promised page", []string{first, `{"message":"not found"}`}, 404, -1},
			{"array after envelope", []string{first, `[]`}, 200, -1},
		} {
			t.Run(strings.TrimPrefix(service.path, "/api/v1/")+"/"+tc.name, func(t *testing.T) {
				var calls atomic.Int32
				srv, adapter := setupTestServer(func(w http.ResponseWriter, r *http.Request) {
					page := int(calls.Add(1))
					if page > len(tc.pages) || r.Method != http.MethodGet || r.URL.Path != service.path || r.URL.Query().Get("project_id") != "p" {
						t.Errorf("unexpected request %d: %s %s", page, r.Method, r.URL)
						http.Error(w, "unexpected request", 400)
						return
					}
					if page == 2 && r.URL.Query().Get("offset") != "1" {
						t.Errorf("second request offset = %q", r.URL.Query().Get("offset"))
					}
					if page == len(tc.pages) {
						w.WriteHeader(tc.lastStatus)
					}
					_, _ = io.WriteString(w, tc.pages[page-1])
				})
				defer srv.Close()
				n, err := service.read(adapter)
				if count := int(calls.Load()); count != len(tc.pages) {
					t.Errorf("read %d pages, want %d", count, len(tc.pages))
				}
				if tc.rows < 0 {
					if n != 0 || clierrors.Reason(err) != clierrors.CodeAPIError {
						t.Fatalf("invalid list: %d rows, reason %q, error %v", n, clierrors.Reason(err), err)
					}
					return
				}
				if err != nil || n != tc.rows {
					t.Fatalf("got %d rows, error %v; want %d rows", n, err, tc.rows)
				}
			})
		}
	}
}
