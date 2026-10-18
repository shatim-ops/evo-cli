// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package verify

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serverPort(t *testing.T, s *httptest.Server) int {
	t.Helper()
	addr := s.Listener.Addr().(*net.TCPAddr)
	return addr.Port
}

func TestCheck_Healthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	port := serverPort(t, srv)
	results := Check(context.Background(), "127.0.0.1", []int{port}, 5*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if !r.Healthy {
		t.Errorf("expected Healthy=true, got false; error=%q", r.Error)
	}
	if r.StatusCode != 200 {
		t.Errorf("expected StatusCode=200, got %d", r.StatusCode)
	}
	if r.Port != port {
		t.Errorf("expected Port=%d, got %d", port, r.Port)
	}
	// Zero is fine: on Windows the clock is coarser than a loopback probe.
	if r.Latency < 0 {
		t.Errorf("expected non-negative Latency, got %v", r.Latency)
	}
	if r.Error != "" {
		t.Errorf("expected empty Error, got %q", r.Error)
	}
}

func TestCheck_Unhealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	port := serverPort(t, srv)
	results := Check(context.Background(), "127.0.0.1", []int{port}, 5*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Healthy {
		t.Errorf("expected Healthy=false for 500 response")
	}
	if r.StatusCode != 500 {
		t.Errorf("expected StatusCode=500, got %d", r.StatusCode)
	}
	if r.Error != "" {
		t.Errorf("expected empty Error for completed request, got %q", r.Error)
	}
}

func TestCheck_Timeout(t *testing.T) {
	// The handler holds the request until the client gives up (or the test
	// ends), so the probe hits its 100ms timeout without a real sleep.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	defer close(release)

	port := serverPort(t, srv)
	results := Check(context.Background(), "127.0.0.1", []int{port}, 100*time.Millisecond)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Healthy {
		t.Errorf("expected Healthy=false for timeout")
	}
	if r.Error == "" {
		t.Errorf("expected non-empty Error for timeout")
	}
}

func TestCheck_ConnectionRefused(t *testing.T) {
	// Find a port that is not listening by binding and immediately closing.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // port is now free and not listening

	results := Check(context.Background(), "127.0.0.1", []int{port}, 2*time.Second)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Healthy {
		t.Errorf("expected Healthy=false for connection refused")
	}
	if r.Error == "" {
		t.Errorf("expected non-empty Error for connection refused")
	}
}

func TestCheck_MultiplePorts(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthy.Close()

	hPort := serverPort(t, healthy)
	uPort := serverPort(t, unhealthy)

	results := Check(context.Background(), "127.0.0.1", []int{hPort, uPort}, 5*time.Second)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if !results[0].Healthy {
		t.Errorf("port %d: expected Healthy=true, got false; error=%q", hPort, results[0].Error)
	}
	if results[0].Port != hPort {
		t.Errorf("expected Port=%d, got %d", hPort, results[0].Port)
	}

	if results[1].Healthy {
		t.Errorf("port %d: expected Healthy=false, got true", uPort)
	}
	if results[1].StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected StatusCode=%d, got %d", http.StatusServiceUnavailable, results[1].StatusCode)
	}
	if results[1].Port != uPort {
		t.Errorf("expected Port=%d, got %d", uPort, results[1].Port)
	}
}

func TestCheck_DefaultTimeout(t *testing.T) {
	// Verify that passing 0 timeout does not panic and uses the default (10s).
	// We use a healthy server to ensure the probe succeeds within the default timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	port := serverPort(t, srv)
	results := Check(context.Background(), "127.0.0.1", []int{port}, 0)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Healthy {
		t.Errorf("expected Healthy=true with default timeout, got false; error=%q", results[0].Error)
	}
}
