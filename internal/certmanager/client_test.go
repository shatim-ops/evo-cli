// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package certmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// L-17 (аудит релиза): API отказывает в списке версий без pageSize —
// 400 «PageSize: value must be inside range [1, 1000]», хотя в swagger
// параметр необязателен. Клиент шлёт pageSize=1000 и читает все страницы
// по nextPageToken.
func TestVersions_PageSizeAndPages(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/certificate/c-1/versions" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		got = append(got, q.Encode())
		if q.Get("pageSize") != "1000" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"code":3,"message":"PageSize: value must be inside range [1, 1000]"}`)
			return
		}
		switch q.Get("pageToken") {
		case "":
			_, _ = io.WriteString(w, `{"versions":[{"id":"1"},{"id":"2"}],"nextPageToken":"t2"}`)
		case "t2":
			_, _ = io.WriteString(w, `{"versions":[{"id":"3"}]}`)
		default:
			w.WriteHeader(400)
		}
	}))
	defer srv.Close()

	c := New("tok", "p", srv.URL)
	out, err := c.Versions(context.Background(), "c-1")
	if err != nil {
		t.Fatal(err)
	}
	vs, _ := out["versions"].([]any)
	if len(vs) != 3 {
		t.Errorf("versions %v, want 3 from two pages", out)
	}
	if len(got) != 2 || got[0] != "pageSize=1000" || got[1] != "pageSize=1000&pageToken=t2" {
		t.Errorf("queries %v", got)
	}
}

// Сервер, повторяющий тот же токен, не зацикливает клиент: два запроса и
// ошибка «список не полон» (break r4, R4-7; раньше — успех).
func TestVersions_RepeatedTokenIsError(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		_, _ = io.WriteString(w, `{"versions":[{"id":"`+string(rune('0'+n))+`"}],"nextPageToken":"same"}`)
	}))
	defer srv.Close()
	out, err := New("tok", "p", srv.URL).Versions(context.Background(), "c-1")
	if err == nil || n != 2 {
		t.Fatalf("requests %d, err %v, out %v", n, err, out)
	}
}
