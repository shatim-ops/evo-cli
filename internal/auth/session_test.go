// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
)

// newJWTIAMServer answers every exchange with a new access token made by
// mint (the n-th exchange, from 1) and expires_in.
func newJWTIAMServer(t *testing.T, expiresIn time.Duration, mint func(n int32) string) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": mint(n),
			"expires_in":   int(expiresIn.Seconds()),
		})
	}))
	t.Cleanup(srv.Close)
	old := IAMEndpoint
	IAMEndpoint = srv.URL
	t.Cleanup(func() { IAMEndpoint = old })
	return &calls
}

// fakeJWT is an unsigned token whose payload has exp.
func fakeJWT(exp time.Time) string {
	enc := base64.RawURLEncoding
	return enc.EncodeToString([]byte(`{"alg":"none"}`)) + "." +
		enc.EncodeToString([]byte(fmt.Sprintf(`{"exp":%d}`, exp.Unix()))) + "." +
		enc.EncodeToString([]byte("sig"))
}

// The token's own exp bounds its deadline when it is earlier than
// expires_in says: a token is not used past the moment the services
// reject it.
func TestToken_JWTExpBoundsTheDeadline(t *testing.T) {
	exp := time.Now().Add(30 * time.Minute).Truncate(time.Second)
	newJWTIAMServer(t, time.Hour, func(int32) string { return fakeJWT(exp) })
	tok, err := NewIAMProvider(testCreds, nil, nil).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !tok.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt %v, want the token's exp %v", tok.ExpiresAt, exp)
	}

	// exp later than expires_in, or a token that is no JWT: expires_in.
	for name, mint := range map[string]func(int32) string{
		"later exp": func(int32) string { return fakeJWT(time.Now().Add(3 * time.Hour)) },
		"not a JWT": func(int32) string { return "opaque-token" },
	} {
		newJWTIAMServer(t, time.Hour, mint)
		tok, err := NewIAMProvider(testCreds, nil, nil).Token(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if d := time.Until(tok.ExpiresAt); d < 59*time.Minute || d > time.Hour {
			t.Errorf("%s: deadline in %v, want expires_in (1h)", name, d)
		}
	}
}

// The pre-flight check is a deadline, so it is decided on the wall clock
// (IsValid), as the refresh is; the clock of answers (internal/clock) only
// prints the TTL (FB140-5). With that clock an hour ahead, a token of 11
// minutes passes.
func TestToken_PreFlightOnTheWallClock(t *testing.T) {
	old := clock.Now
	clock.Now = func() time.Time { return time.Now().Add(time.Hour) }
	t.Cleanup(func() { clock.Now = old })
	newJWTIAMServer(t, 11*time.Minute, func(int32) string { return "short-token" })
	if _, err := NewIAMProvider(testCreds, nil, nil).Token(context.Background()); err != nil {
		t.Errorf("a token of 11 minutes was refused: %v", err)
	}
}

// Forget drops the token a service rejected, from memory and from the
// file cache: the next call exchanges the key again instead of sending the
// same token until its deadline. Another token is left alone.
func TestIAMProvider_ForgetRejectedToken(t *testing.T) {
	calls := newJWTIAMServer(t, time.Hour, func(n int32) string { return fmt.Sprintf("tok-%d", n) })
	cache := FileCache{Path: filepath.Join(t.TempDir(), "token.json")}
	p := NewIAMProvider(testCreds, nil, cache)
	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	p.Forget("another-token")
	if again, _ := p.Token(context.Background()); again.Value != tok.Value || calls.Load() != 1 {
		t.Fatalf("a foreign token dropped the cached one: %q after %d exchanges", again.Value, calls.Load())
	}

	p.Forget(tok.Value)
	if _, ok := cache.Load(CacheKey(testCreds)); ok {
		t.Error("the rejected token is still in the file cache")
	}
	fresh, err := p.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Value == tok.Value || calls.Load() != 2 {
		t.Errorf("after Forget: token %q, %d exchanges; want a new token from a second exchange", fresh.Value, calls.Load())
	}

	// A new provider of the same key (the next CLI run) finds no rejected
	// token in the file either.
	p.Forget(fresh.Value)
	if _, err := NewIAMProvider(testCreds, nil, cache).Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("the next run reused the rejected token: %d exchanges, want 3", calls.Load())
	}
}
