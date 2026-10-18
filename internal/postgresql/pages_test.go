// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package postgresql

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// openapi-postgresql.yaml: GET /v1/clusters answers clusters and
// next_page_token; the next page is asked for by page_token (#98).
func TestListClusters_FollowsNextPage(t *testing.T) {
	var mu sync.Mutex
	var gotTokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("page_token")
		mu.Lock()
		gotTokens = append(gotTokens, tok)
		mu.Unlock()
		if r.URL.Query().Has("pageToken") {
			t.Errorf("pageToken sent to PostgreSQL: %s", r.URL.RawQuery)
		}
		if tok == "" {
			_, _ = io.WriteString(w, `{"clusters":[{"id":"c1","name":"one"}],"next_page_token":"p2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"clusters":[{"id":"c2","name":"two"}]}`)
	}))
	defer srv.Close()
	got, err := New("t", "proj", srv.URL).ListClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1]["id"] != "c2" {
		t.Fatalf("clusters = %v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gotTokens) != 2 || gotTokens[1] != "p2" {
		t.Fatalf("page tokens sent: %q", gotTokens)
	}
}

// A server that keeps answering a token already followed ends the read
// instead of looping forever.
func TestListClusters_StopsOnRepeatedToken(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"clusters":[{"id":"c","name":"same"}],"next_page_token":"same-token"}`)
	}))
	defer srv.Close()
	got, err := New("t", "proj", srv.URL).ListClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 || len(got) != 2 {
		t.Fatalf("calls = %d, clusters = %d; want 2 and 2", calls.Load(), len(got))
	}
}

// A list that never ends fails past pager.MaxPages instead of returning a
// list cut short without saying so.
func TestListClusters_EndlessPagesFail(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		_, _ = io.WriteString(w, `{"clusters":[],"next_page_token":"t`+strconv.Itoa(int(n))+`"}`)
	}))
	defer srv.Close()
	if _, err := New("t", "proj", srv.URL).ListClusters(context.Background()); err == nil {
		t.Fatalf("endless list read without an error after %d calls", calls.Load())
	}
}
