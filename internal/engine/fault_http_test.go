// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func faultAdapter(t *testing.T, h http.HandlerFunc) *provision.CloudRuAdapter {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	a := provision.NewCloudRuAdapter(srv.Client(), srv.URL)
	a.SetToken("test-token")
	a.Backoff = time.Millisecond
	return a
}

func TestFault_HTTP_CreateVMObjectWithoutID(t *testing.T) {
	a := faultAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	})
	_, err := a.CreateVM(context.Background(), provision.CreateVMRequest{Name: "evo-app", ProjectID: faultProject})
	if err == nil || clerrors.Reason(err) == "" {
		t.Fatalf("object without id: %v", err)
	}
}

func TestFault_HTTP_CreateVMHTML(t *testing.T) {
	a := faultAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	})
	_, err := a.CreateVM(context.Background(), provision.CreateVMRequest{Name: "evo-app", ProjectID: faultProject})
	if err == nil || !strings.Contains(err.Error(), "unexpected") && clerrors.Reason(err) != clerrors.CodeAPIError {
		t.Fatalf("html: %v", err)
	}
}

func TestFault_HTTP_GetVM204(t *testing.T) {
	a := faultAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	_, err := a.GetVM(context.Background(), faultID(1))
	if err == nil {
		t.Fatal("204 decoded as a VM")
	}
}

func TestFault_HTTP_CreateVM429UsesBackoffNotRetryAfter(t *testing.T) {
	var n int
	a := faultAdapter(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	start := time.Now()
	_, err := a.CreateVM(context.Background(), provision.CreateVMRequest{Name: "evo-app", ProjectID: faultProject})
	// wall-clock: guards waiting out Retry-After (3600 s; httpretry caps it at 30 s).
	if time.Since(start) > 30*time.Second {
		t.Fatalf("waited %s for Retry-After 3600", time.Since(start))
	}
	if clerrors.Reason(err) != clerrors.CodeAPIRateLimited {
		t.Fatalf("reason %s (%v), attempts %d", clerrors.Reason(err), err, n)
	}
}

func TestFault_HTTP_RetryAfterCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "3600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	tr := &httpretry.Transport{Backoff: time.Hour, Base: srv.Client().Transport}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = tr.RoundTrip(req)
	// wall-clock: guards the hour of backoff and of Retry-After after the context's 40 ms.
	if time.Since(start) > 30*time.Second {
		t.Fatalf("Retry-After wait was not cut by the context: %s (%v)", time.Since(start), err)
	}
	if err == nil {
		t.Fatal("429 with a cancelled context returned a response")
	}
}
