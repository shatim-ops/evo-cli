// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package vpc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// The client repeats a read the server answered 503 and a write it refused
// with 429; a write answered 503 is not repeated (BACKLOG [02]: only compute
// used to retry).
func TestClientRetriesTransientAnswers(t *testing.T) {
	var gets, posts, puts atomic.Int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if gets.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(`{"vpcs":[{"id":"v1"}]}`))
		case http.MethodPost:
			b, _ := io.ReadAll(r.Body)
			bodies = append(bodies, string(b))
			if posts.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			puts.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()
	c := New("tok", "p1", srv.URL)
	ctx := context.Background()

	if _, err := c.ListVPCs(ctx); err != nil {
		t.Fatalf("ListVPCs after one 503: %v", err)
	}
	if gets.Load() != 2 {
		t.Fatalf("GET calls = %d, want 2", gets.Load())
	}

	// bytes.NewReader in do(): http.NewRequest sets GetBody, so the body
	// is replayed after 429.
	_, status, err := c.do(ctx, http.MethodPost, "/x", nil, map[string]string{"a": "b"})
	if err != nil || status != http.StatusOK || posts.Load() != 2 {
		t.Fatalf("POST after 429: status %d err %v calls %d, want 200 after 2", status, err, posts.Load())
	}
	if bodies[0] != `{"a":"b"}` || bodies[1] != bodies[0] {
		t.Fatalf("replayed bodies = %q", bodies)
	}

	_, status, err = c.do(ctx, http.MethodPut, "/x", nil, map[string]string{"a": "b"})
	if err != nil || status != http.StatusServiceUnavailable || puts.Load() != 1 {
		t.Fatalf("PUT answered 503: status %d err %v calls %d, want 503 once", status, err, puts.Load())
	}
}
