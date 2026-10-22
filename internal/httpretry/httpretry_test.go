// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package httpretry

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// flaky fails the first n calls with err (or status), then answers 200.
type flaky struct {
	n      int32
	calls  atomic.Int32
	err    error
	status int
	bodies []string
}

func (f *flaky) RoundTrip(r *http.Request) (*http.Response, error) {
	c := f.calls.Add(1)
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		f.bodies = append(f.bodies, string(b))
	}
	if c <= f.n {
		if f.err != nil {
			return nil, f.err
		}
		return &http.Response{StatusCode: f.status, Body: io.NopCloser(strings.NewReader("busy")), Header: http.Header{}}, nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}}, nil
}

func do(t *testing.T, f *flaky, method, body string) (*http.Response, error) {
	t.Helper()
	tr := &Transport{Base: f, Backoff: time.Millisecond}
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, _ := http.NewRequest(method, "https://example.invalid/x", rdr)
	return tr.RoundTrip(req)
}

func TestTransportErrorRetriedOnGet(t *testing.T) {
	f := &flaky{n: 2, err: errors.New("connection reset by peer")}
	resp, err := do(t, f, http.MethodGet, "")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if f.calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", f.calls.Load())
	}
}

func TestTransportErrorNotRetriedOnWrites(t *testing.T) {
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		f := &flaky{n: 1, err: errors.New("EOF")}
		if _, err := do(t, f, m, `{"a":1}`); err == nil {
			t.Fatalf("%s: want the transport error", m)
		}
		if f.calls.Load() != 1 {
			t.Fatalf("%s: calls = %d, want 1", m, f.calls.Load())
		}
	}
}

// A certificate the system does not trust (a corporate proxy that inspects
// HTTPS) stays untrusted: no three more attempts with 1+2+4 s of waiting.
func TestUntrustedCertificateNotRetried(t *testing.T) {
	f := &flaky{n: 100, err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}}
	if _, err := do(t, f, http.MethodGet, ""); err == nil {
		t.Fatal("want the certificate error")
	}
	if got := f.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
}

// Every client of evo goes through http.DefaultTransport (the service
// clients through Transport and UserAgent with a nil base, verify through
// its Clone), which takes HTTPS_PROXY, HTTP_PROXY and NO_PROXY from the
// environment. A transport built by hand has no Proxy unless it sets one:
// it would bypass the corporate proxy, and the person would see a timeout.
func TestNoHandBuiltTransport(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && path != root {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(b), "\n") {
			if strings.Contains(line, "http.Transport{") && !strings.Contains(line, "Proxy:") {
				t.Errorf("%s:%d: an http.Transport without Proxy (clone http.DefaultTransport instead): %s", path, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGiveUpAfterMaxRetries(t *testing.T) {
	f := &flaky{n: 100, err: errors.New("EOF")}
	if _, err := do(t, f, http.MethodGet, ""); err == nil {
		t.Fatal("want error")
	}
	if got := f.calls.Load(); got != MaxRetries+1 {
		t.Fatalf("calls = %d, want %d", got, MaxRetries+1)
	}
}

func TestTooManyRequestsReplaysBody(t *testing.T) {
	f := &flaky{n: 1, status: http.StatusTooManyRequests}
	resp, err := do(t, f, http.MethodPost, `{"name":"x"}`)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if len(f.bodies) != 2 || f.bodies[0] != f.bodies[1] || f.bodies[1] != `{"name":"x"}` {
		t.Fatalf("bodies = %q", f.bodies)
	}
}

func TestServerErrorsOnlyForReads(t *testing.T) {
	f := &flaky{n: 1, status: http.StatusServiceUnavailable}
	if resp, _ := do(t, f, http.MethodGet, ""); resp.StatusCode != 200 {
		t.Fatalf("GET status = %d, want 200 after a retry", resp.StatusCode)
	}
	f = &flaky{n: 1, status: http.StatusServiceUnavailable}
	if resp, _ := do(t, f, http.MethodPost, "{}"); resp.StatusCode != 503 || f.calls.Load() != 1 {
		t.Fatalf("POST status = %d calls = %d, want 503 once", resp.StatusCode, f.calls.Load())
	}
	f = &flaky{n: 1, status: http.StatusInternalServerError}
	if resp, _ := do(t, f, http.MethodGet, ""); resp.StatusCode != 500 || f.calls.Load() != 1 {
		t.Fatalf("500 must not be retried: status = %d calls = %d", resp.StatusCode, f.calls.Load())
	}
}

func TestRetryAfter(t *testing.T) {
	cases := map[string]time.Duration{"2": 2 * time.Second, "0": 0, "600": maxRetryAfter}
	for v, want := range cases {
		if got, ok := retryAfter(v); !ok || got != want {
			t.Fatalf("retryAfter(%q) = %v %v, want %v", v, got, ok, want)
		}
	}
	for _, v := range []string{"", "-1", "Wed, 21 Oct 2015 07:28:00 GMT"} {
		if _, ok := retryAfter(v); ok {
			t.Fatalf("retryAfter(%q) accepted", v)
		}
	}
}

func TestContextStopsBackoff(t *testing.T) {
	f := &flaky{n: 100, err: errors.New("EOF")}
	tr := &Transport{Base: f, Backoff: time.Hour}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid/x", nil)
	start := time.Now()
	if _, err := tr.RoundTrip(req); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline", err)
	}
	// wall-clock: guards the hour of backoff: half of it.
	if time.Since(start) > tr.Backoff/2 {
		t.Fatal("backoff ignored the context")
	}
}

// http.Client.Timeout must cover the waits too: a client with a short
// timeout does not sit through a long backoff.
func TestClientTimeoutStopsBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	cl := &http.Client{Timeout: 200 * time.Millisecond, Transport: &Transport{Backoff: time.Hour}}
	start := time.Now()
	if _, err := cl.Get(srv.URL); err == nil {
		t.Fatal("want timeout")
	}
	// wall-clock: guards the hour of backoff: half of it.
	if time.Since(start) > time.Hour/2 {
		t.Fatal("client timeout ignored during backoff")
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestRealServerRecovers(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	resp, err := NewClient(5 * time.Second).Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || calls.Load() != 2 {
		t.Fatalf("status = %d calls = %d", resp.StatusCode, calls.Load())
	}
}

// TestUserAgent: a request without a User-Agent gets evo's, on both the
// retrying transport and the bare wrapper; one with its own keeps it.
func TestUserAgent(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("User-Agent"))
	}))
	defer srv.Close()
	for _, c := range []*http.Client{NewClient(5 * time.Second), {Transport: UserAgent(nil)}} {
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
		req.Header.Set("User-Agent", "mine")
		if resp, err = c.Do(req); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if req.Header.Get("User-Agent") != "mine" {
			t.Error("the caller's request was changed")
		}
	}
	if len(got) != 4 || !strings.HasPrefix(got[0], "evo/") || got[0] != got[2] || got[1] != "mine" || got[3] != "mine" {
		t.Errorf("User-Agent seen: %q", got)
	}
}

// An HTML page with a 2xx is a gateway's, not the API's answer (break r3b,
// R3B-1): the client sees a 502. JSON and XML pass as they are.
func TestHTMLAsBadGateway(t *testing.T) {
	for _, tc := range []struct {
		ctype string
		want  int
	}{
		{"text/html; charset=utf-8", http.StatusBadGateway},
		{"application/json", http.StatusOK},
		{"application/xml", http.StatusOK},
		{"", http.StatusOK},
	} {
		resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {tc.ctype}}, Body: io.NopCloser(strings.NewReader("<html></html>"))}
		if got := HTMLAsBadGateway(resp).StatusCode; got != tc.want {
			t.Errorf("%q: status %d, want %d", tc.ctype, got, tc.want)
		}
	}
	notFound := &http.Response{StatusCode: 404, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(""))}
	if got := HTMLAsBadGateway(notFound).StatusCode; got != 404 {
		t.Errorf("a 404 HTML page became %d", got)
	}
}
