// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package billing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// consumptionServer answers /v2/consumption with two pages (the second by
// page_filter.page_token=p2), or with status when it is not 200, and
// records each query.
func consumptionServer(t *testing.T, status int) (*httptest.Server, func() []url.Values) {
	t.Helper()
	var mu sync.Mutex
	var calls []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/consumption" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "unexpected "+r.URL.String(), 500)
			return
		}
		mu.Lock()
		calls = append(calls, r.URL.Query())
		mu.Unlock()
		if status != 200 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"message":"denied"}`)
			return
		}
		if r.URL.Query().Get("page_filter.page_token") == "p2" {
			_, _ = io.WriteString(w, `{"consumptions":[{"amount":2}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"consumptions":[{"amount":1}],"next_page_token":"p2"}`)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []url.Values {
		mu.Lock()
		defer mu.Unlock()
		return append([]url.Values(nil), calls...)
	}
}

// Без договора — project_ids, agreement_id нет; страницы читаются все.
func TestConsumption_ByProject(t *testing.T) {
	srv, calls := consumptionServer(t, 200)
	c := New("tok", "", "", srv.URL).WithProject("p-1")
	items, err := c.Consumption(context.Background(), "2026-10-01T00:00:00Z", "2026-10-01T23:59:59Z")
	if err != nil || len(items) != 2 {
		t.Fatalf("items %v, err %v", items, err)
	}
	for _, q := range calls() {
		if q.Get("project_ids") != "p-1" || q.Has("agreement_id") || q.Get("page_filter.page_size") != "1000" {
			t.Errorf("query %v", q)
		}
	}
}

// С договором — как раньше: agreement_id, без project_ids.
func TestConsumption_ByAgreement(t *testing.T) {
	srv, calls := consumptionServer(t, 200)
	if _, err := New("tok", "agr-1", "", srv.URL).Consumption(context.Background(), "a", "b"); err != nil {
		t.Fatal(err)
	}
	for _, q := range calls() {
		if q.Get("agreement_id") != "agr-1" || q.Has("project_ids") {
			t.Errorf("query %v", q)
		}
	}
}

// Ни договора, ни проекта — CONFIG_INVALID, запроса нет.
func TestConsumption_NoFilter(t *testing.T) {
	srv, calls := consumptionServer(t, 200)
	_, err := New("tok", "", "", srv.URL).Consumption(context.Background(), "a", "b")
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeConfigInvalid || !strings.Contains(ce.Hint, "CLOUDRU_PROJECT_ID") {
		t.Errorf("err %v", err)
	}
	if n := len(calls()); n != 0 {
		t.Errorf("%d requests", n)
	}
}

// 403 по проекту — API_ERROR с ролью platform.project.admin и ссылкой на
// документацию; другой статус — прежняя ошибка «billing API N».
func TestConsumption_ProjectForbidden(t *testing.T) {
	srv, _ := consumptionServer(t, 403)
	_, err := New("tok", "", "", srv.URL).WithProject("p-1").Consumption(context.Background(), "a", "b")
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError || !strings.Contains(ce.Hint, "platform.project.admin") || !strings.Contains(ce.Hint, DocAPIStart) {
		t.Errorf("err %v", err)
	}
	srv, _ = consumptionServer(t, 400)
	_, err = New("tok", "", "", srv.URL).WithProject("p-1").Consumption(context.Background(), "a", "b")
	if err == nil || errors.As(err, &ce) || !strings.Contains(err.Error(), "billing API 400: denied") {
		t.Errorf("400: %v", err)
	}
}

// Баланс без договора — CONFIG_INVALID с подсказкой, запроса нет.
func TestBalance_NoAgreement(t *testing.T) {
	srv, calls := consumptionServer(t, 200)
	_, err := New("tok", "", srv.URL, srv.URL).WithProject("p-1").Balance(context.Background())
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeConfigInvalid || !strings.Contains(ce.Hint, "CLOUDRU_AGREEMENT_ID") {
		t.Errorf("err %v", err)
	}
	if n := len(calls()); n != 0 {
		t.Errorf("%d requests", n)
	}
}
