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
	"strconv"
	"strings"
	"sync"
	"testing"

	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/pager"
)

// QA-2 (QA 20.10): сервер, всегда отдающий тот же next_page_token,
// зацикливал Consumption до нехватки памяти. Теперь — ошибка API_ERROR
// за конечное число запросов; так же для цикла A→B→A.
func TestConsumption_RepeatedTokenIsError(t *testing.T) {
	for _, tc := range []struct {
		name  string
		next  func(token string) string
		calls int
	}{
		{"same token", func(string) string { return "same" }, 2},
		{"A-B-A", func(token string) string {
			switch token {
			case "":
				return "A"
			case "A":
				return "B"
			}
			return "A"
		}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			n := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				n++
				mu.Unlock()
				tok := r.URL.Query().Get("page_filter.page_token")
				_, _ = io.WriteString(w, `{"consumptions":[{"amount":1}],"next_page_token":"`+tc.next(tok)+`"}`)
			}))
			defer srv.Close()
			_, err := New("tok", "", "", srv.URL).WithProject("p-1").Consumption(context.Background(), "a", "b")
			var ce *clierrors.CLIError
			if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError || !strings.Contains(err.Error(), `"`+tc.next("x")+`"`) {
				t.Fatalf("err = %v", err)
			}
			if n != tc.calls {
				t.Fatalf("requests = %d, want %d", n, tc.calls)
			}
		})
	}
}

// Список длиннее pager.MaxPages — ошибка, а не обрезанная сумма.
func TestConsumption_TooManyPagesIsError(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		_, _ = io.WriteString(w, `{"consumptions":[],"next_page_token":"t`+strconv.Itoa(k)+`"}`)
	}))
	defer srv.Close()
	_, err := New("tok", "", "", srv.URL).WithProject("p-1").Consumption(context.Background(), "a", "b")
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError || !strings.Contains(err.Error(), strconv.Itoa(pager.MaxPages)) {
		t.Fatalf("err = %v", err)
	}
	if n != pager.MaxPages {
		t.Fatalf("requests = %d, want %d", n, pager.MaxPages)
	}
}
