// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package certmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Цикл токенов A→B→A и пустая страница с тем же токеном не означают, что
// версии кончились. Сейчас Versions возвращает успех и повторы.
func TestBreakR4_R4_7_VersionTokenCycleIsError(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		next := "A"
		body := `{"versions":[{"id":"00000000-0000-4000-8000-000000000001"}],"nextPageToken":"A"}`
		switch r.URL.Query().Get("pageToken") {
		case "A":
			next = "B"
			body = `{"versions":[],"nextPageToken":"` + next + `"}`
		case "B":
			body = `{"versions":[{"id":"00000000-0000-4000-8000-000000000001"}],"nextPageToken":"A"}`
		}
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	out, err := New("tok", "00000000-0000-4000-8000-0000000000aa", srv.URL).Versions(context.Background(), "00000000-0000-4000-8000-000000000001")
	if err == nil {
		t.Fatalf("token cycle returned success: %v after %d requests", out, n)
	}
	if n > 5 {
		t.Fatalf("requests = %d, the walk is not bounded", n)
	}
}
