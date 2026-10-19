// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

func TestIAMProvider_RejectedCredentialsAreAuthError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "denied", status)
		}))
		oldEndpoint := IAMEndpoint
		IAMEndpoint = srv.URL
		_, err := NewIAMProvider(Credentials{KeyID: "k", Secret: "s"}, srv.Client(), nil).Token(context.Background())
		IAMEndpoint = oldEndpoint
		srv.Close()
		if got := clerrors.ExitCode(err); got != clerrors.ExitAuth {
			t.Errorf("status %d: exit code = %d (err %v), want %d", status, got, err, clerrors.ExitAuth)
		}
	}
}

func TestNewIAMProvider_DefaultClientHasTimeout(t *testing.T) {
	p := NewIAMProvider(Credentials{}, nil, nil)
	if p.client.Timeout == 0 {
		t.Error("default IAM client has no timeout")
	}
}

// flakyIAM resets the connection for the first `resets` requests, then
// answers with status for the next `fails` ones, then issues a token.
func flakyIAM(t *testing.T, resets, fails, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		switch {
		case n <= resets:
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close() // transport-level failure: no HTTP response at all
		case n <= resets+fails:
			http.Error(w, "unavailable", status)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"tok","expires_in":3600}`)
		}
	}))
	t.Cleanup(srv.Close)
	old := IAMEndpoint
	IAMEndpoint = srv.URL
	t.Cleanup(func() { IAMEndpoint = old })
	return srv, &calls
}

func recordSleep(delays *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, d time.Duration) error {
		*delays = append(*delays, d)
		return nil
	}
}

func TestIAMProvider_RetriesConnectionReset(t *testing.T) {
	srv, calls := flakyIAM(t, 1, 0, 0)
	p := NewIAMProvider(Credentials{KeyID: "k", Secret: "s"}, srv.Client(), nil)
	var delays []time.Duration
	p.sleep = recordSleep(&delays)

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("reset was not retried: %v", err)
	}
	if tok.Value != "tok" || calls.Load() != 2 {
		t.Errorf("token %q after %d calls, want tok after 2", tok.Value, calls.Load())
	}
	if len(delays) != 1 || delays[0] != exchangeBaseDelay {
		t.Errorf("delays = %v, want [%v]", delays, exchangeBaseDelay)
	}
}

func TestIAMProvider_Retries5xxWithBackoffThenGivesUp(t *testing.T) {
	srv, calls := flakyIAM(t, 0, 10, http.StatusBadGateway)
	p := NewIAMProvider(Credentials{KeyID: "k", Secret: "s"}, srv.Client(), nil)
	var delays []time.Duration
	p.sleep = recordSleep(&delays)

	_, err := p.Token(context.Background())
	if got := clerrors.ExitCode(err); got != clerrors.ExitAPI {
		t.Errorf("exit code = %d (err %v), want %d", got, err, clerrors.ExitAPI)
	}
	if int(calls.Load()) != exchangeAttempts {
		t.Errorf("calls = %d, want %d", calls.Load(), exchangeAttempts)
	}
	want := []time.Duration{exchangeBaseDelay, 2 * exchangeBaseDelay}
	if fmt.Sprint(delays) != fmt.Sprint(want) {
		t.Errorf("delays = %v, want %v", delays, want)
	}
}

func TestIAMProvider_DoesNotRetry4xx(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		srv, calls := flakyIAM(t, 0, 10, status)
		p := NewIAMProvider(Credentials{KeyID: "k", Secret: "s"}, srv.Client(), nil)
		var delays []time.Duration
		p.sleep = recordSleep(&delays)
		if _, err := p.Token(context.Background()); err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if calls.Load() != 1 || len(delays) != 0 {
			t.Errorf("status %d retried: %d calls, delays %v", status, calls.Load(), delays)
		}
	}
}

func TestIAMProvider_RetryStopsOnCancel(t *testing.T) {
	srv, calls := flakyIAM(t, 10, 0, 0)
	p := NewIAMProvider(Credentials{KeyID: "k", Secret: "s"}, srv.Client(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	p.sleep = func(ctx context.Context, _ time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if _, err := p.Token(ctx); err == nil {
		t.Fatal("expected error")
	}
	if calls.Load() != 1 {
		t.Errorf("calls after cancel = %d, want 1", calls.Load())
	}
}

// A certificate the system does not trust (a corporate proxy that inspects
// HTTPS) is not repeated, and the hint says what to do about it instead of
// "check your network connection".
func TestIAMProvider_UntrustedCertificateIsNotRetried(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the request passed the certificate check")
	}))
	defer srv.Close()
	old := IAMEndpoint
	IAMEndpoint = srv.URL
	defer func() { IAMEndpoint = old }()
	// The default roots, as evo's own client has: the test server's are unknown.
	client := &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()}
	p := NewIAMProvider(Credentials{KeyID: "k", Secret: "s"}, client, nil)
	var delays []time.Duration
	p.sleep = recordSleep(&delays)

	_, err := p.Token(context.Background())
	var ce *clerrors.CLIError
	if !errors.As(err, &ce) || ce.Code != clerrors.CodeAPIUnreachable || !strings.Contains(ce.Hint, "SSL_CERT_FILE") {
		t.Fatalf("err %v", err)
	}
	if len(delays) != 0 || p.Exchanges() != 1 {
		t.Errorf("retried: %d exchanges, delays %v", p.Exchanges(), delays)
	}
}
