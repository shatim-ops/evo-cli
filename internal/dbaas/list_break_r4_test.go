// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package dbaas

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Уникальные токены после maxListPages: список не полон, ошибки нет.
func TestBreakR4_R4_8_ListClustersMaxPagesIsError(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		k := n
		mu.Unlock()
		fmt.Fprintf(w, `{"clusters":[{"id":"00000000-0000-4000-8000-%012d"}],"next_page_token":"t%d"}`, k, k)
	}))
	defer srv.Close()

	all, err := New("tok", "00000000-0000-4000-8000-0000000000aa", srv.URL).ListClusters(context.Background(), nil)
	if err == nil {
		t.Fatalf("past the page cap: %d clusters and a nil error after %d requests", len(all), n)
	}
	if n != maxListPages {
		t.Fatalf("requests = %d, want %d", n, maxListPages)
	}
}
