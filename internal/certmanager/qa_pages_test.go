// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package certmanager

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
)

// QA-2 (QA 20.10): цикл токенов A→B→A ловился только при повторе подряд
// и стоил maxVersionPages запросов. Теперь чтение обрывается на любом уже
// виденном токене за три запроса — и это ошибка «список не полон», а не
// успех с тем, что успели прочитать (break r4, R4-7: раньше тест ждал
// успех).
func TestVersions_TokenCycleIsError(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		next, id := "A", "1"
		switch r.URL.Query().Get("pageToken") {
		case "A":
			next, id = "B", "2"
		case "B":
			next, id = "A", "3"
		}
		_, _ = io.WriteString(w, `{"versions":[{"id":"`+id+`"}],"nextPageToken":"`+next+`"}`)
	}))
	defer srv.Close()
	out, err := New("tok", "p", srv.URL).Versions(context.Background(), "c-1")
	var ce *clierrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clierrors.CodeAPIError || !strings.Contains(err.Error(), `"A"`) || n != 3 {
		t.Fatalf("requests %d, err %v, out %v", n, err, out)
	}
}

// Версия, уже прочитанная на прошлой странице, не попадает в ответ
// второй раз; страница из одних повторов — сервер игнорирует токен,
// ошибка.
func TestVersions_RepeatedVersionsAreError(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = io.WriteString(w, `{"versions":[{"id":1},{"id":2}],"nextPageToken":"t`+strconv.Itoa(n)+`"}`)
	}))
	defer srv.Close()
	out, err := New("tok", "p", srv.URL).Versions(context.Background(), "c-1")
	if err == nil || n != 2 {
		t.Fatalf("requests %d, err %v, out %v", n, err, out)
	}
}

// Новые токены без конца — ошибка после maxVersionPages запросов.
func TestVersions_TooManyPages(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		_, _ = io.WriteString(w, `{"versions":[],"nextPageToken":"t`+strconv.Itoa(k)+`"}`)
	}))
	defer srv.Close()
	_, err := New("tok", "p", srv.URL).Versions(context.Background(), "c-1")
	if err == nil || n != maxVersionPages {
		t.Fatalf("requests %d, err %v", n, err)
	}
}
