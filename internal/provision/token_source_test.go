// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Fable on r3a: deploy took the token once and polled the VM with it for
// up to ten minutes; a token near its end expired mid-wait (401). With a
// token source every request carries the provider's current token.
func TestTokenSource_EachRequestGetsTheCurrentToken(t *testing.T) {
	var current atomic.Value
	current.Store("t1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+current.Load().(string) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":"vm-1","status":"running"}`))
	}))
	defer srv.Close()

	a := NewCloudRuAdapter(srv.Client(), srv.URL)
	a.SetToken("t1")
	a.SetTokenSource(func(context.Context) (string, error) { return current.Load().(string), nil })

	if _, err := a.GetVM(context.Background(), "vm-1"); err != nil {
		t.Fatalf("first token: %v", err)
	}
	current.Store("t2") // the provider renewed the token; t1 is refused now
	if _, err := a.GetVM(context.Background(), "vm-1"); err != nil {
		t.Fatalf("renewed token not used: %v", err)
	}
}

// A source that fails (IAM unreachable for a moment) falls back to the
// token set at the start rather than sending none.
func TestTokenSource_FailureFallsBackToSetToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"id":"vm-1","status":"running"}`))
	}))
	defer srv.Close()

	a := NewCloudRuAdapter(srv.Client(), srv.URL)
	a.SetToken("t1")
	a.SetTokenSource(func(context.Context) (string, error) { return "", errors.New("iam down") })
	if _, err := a.GetVM(context.Background(), "vm-1"); err != nil {
		t.Fatalf("fallback token not used: %v", err)
	}
}
