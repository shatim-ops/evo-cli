// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package certmanager

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// QA-2 (QA 20.10): цикл токенов A→B→A ловился только при повторе подряд
// и стоил maxVersionPages запросов. Теперь чтение останавливается на
// любом уже виденном токене: три запроса.
func TestVersions_TokenCycleStops(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		mu.Unlock()
		next := "A"
		switch r.URL.Query().Get("pageToken") {
		case "A":
			next = "B"
		case "B":
			next = "A"
		}
		_, _ = io.WriteString(w, `{"versions":[{"id":"1"}],"nextPageToken":"`+next+`"}`)
	}))
	defer srv.Close()
	out, err := New("tok", "p", srv.URL).Versions(context.Background(), "c-1")
	if err != nil || n != 3 {
		t.Fatalf("requests %d, err %v, out %v", n, err, out)
	}
	if vs, _ := out["versions"].([]any); len(vs) != 3 {
		t.Fatalf("versions %v", out)
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
