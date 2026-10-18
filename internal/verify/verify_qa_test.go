// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package verify

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// A redirect is the service answering. Following it probed another host
// (an external domain, or https:// on 443 with a certificate for a name,
// not for the IP) and reported a working service as UNHEALTHY.
func TestCheck_RedirectIsAnAnswerNotFollowed(t *testing.T) {
	var elsewhere atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		elsewhere.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/login", http.StatusFound)
	}))
	defer srv.Close()

	r := Check(context.Background(), "127.0.0.1", []int{serverPort(t, srv)}, 5*time.Second)[0]
	if !r.Healthy || r.StatusCode != http.StatusFound {
		t.Fatalf("result = %+v, want healthy 302", r)
	}
	if elsewhere.Load() != 0 {
		t.Fatal("the probe followed the redirect to another server")
	}
}

// An HTTPS-only service (nginx on 443) answered plain HTTP with a TLS
// alert: "malformed HTTP response", UNHEALTHY, verify exit 1. The probe
// retries over TLS; the certificate is not checked (it names a domain, the
// probe dials an IP) — this is a liveness check, not trust.
func TestCheck_TLSServiceIsProbedOverHTTPS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := Check(context.Background(), "127.0.0.1", []int{serverPort(t, srv)}, 5*time.Second)[0]
	if !r.Healthy || r.StatusCode != http.StatusOK {
		t.Fatalf("result = %+v, want healthy 200 over TLS", r)
	}
}

func TestCheck_SlowServiceTimesOut(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	start := time.Now()
	r := Check(context.Background(), "127.0.0.1", []int{serverPort(t, srv)}, 100*time.Millisecond)[0]
	if r.Healthy || r.Error == "" {
		t.Fatalf("result = %+v, want a timeout", r)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("probe took %s with a 100ms timeout", d)
	}
}

// A database or cache port answers in its own protocol (or just closes):
// it is up, not broken — verify must not fail a working deployment.
func TestCheck_NonHTTPServiceIsHealthyTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("-ERR unknown command 'GET'\r\n")) // like Redis
			_ = c.Close()
		}
	}()
	r := Check(context.Background(), "127.0.0.1", []int{ln.Addr().(*net.TCPAddr).Port}, time.Second)[0]
	if !r.Healthy || r.Protocol != "tcp" {
		t.Fatalf("result = %+v, want healthy tcp", r)
	}
}

// An HTTP answer keeps its meaning: 5xx is unhealthy, and the protocol says http.
func TestCheck_HTTPProtocolReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	r := Check(context.Background(), "127.0.0.1", []int{serverPort(t, srv)}, time.Second)[0]
	if r.Healthy || r.Protocol != "http" || r.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("result = %+v, want unhealthy http 503", r)
	}
}
