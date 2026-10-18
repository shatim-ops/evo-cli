// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// Fake credentials: tests never see real ones.
var testCreds = Credentials{KeyID: "test-key-123", Secret: "test-secret"}

// newMockIAMServer mimics the Cloud.ru IAM token exchange endpoint and
// counts the requests it serves.
func newMockIAMServer(t *testing.T, tokenTTL time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			KeyID  string `json:"keyId"`
			Secret string `json:"secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if body.KeyID == "" || body.Secret == "" {
			http.Error(w, "missing keyId or secret", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-iam-token-" + body.KeyID,
			"expires_in":   int(tokenTTL.Seconds()),
		})
	}))
	t.Cleanup(srv.Close)
	old := IAMEndpoint
	IAMEndpoint = srv.URL
	t.Cleanup(func() { IAMEndpoint = old })
	return srv, &calls
}

func TestIAMProvider_Token(t *testing.T) {
	srv, _ := newMockIAMServer(t, time.Hour)

	tok, err := NewIAMProvider(testCreds, srv.Client(), nil).Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Value != "test-iam-token-test-key-123" {
		t.Errorf("unexpected token value: %s", tok.Value)
	}
	if tok.TTL() < 50*time.Minute {
		t.Errorf("expected TTL > 50min, got %s", tok.TTL())
	}
}

func TestIAMProvider_IgnoresEnvironment(t *testing.T) {
	// The provider uses the credentials it was given, whatever the env says.
	srv, _ := newMockIAMServer(t, time.Hour)
	t.Setenv("CLOUDRU_KEY_ID", "env-key")
	t.Setenv("CLOUDRU_SECRET", "env-secret")

	tok, err := NewIAMProvider(Credentials{KeyID: "file-key", Secret: "file-secret"}, srv.Client(), nil).
		Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value != "test-iam-token-file-key" {
		t.Errorf("token issued for the wrong key: %s", tok.Value)
	}
}

func TestIAMProvider_CachesTokenInMemory(t *testing.T) {
	srv, calls := newMockIAMServer(t, time.Hour)
	p := NewIAMProvider(testCreds, srv.Client(), nil)
	for i := 0; i < 2; i++ {
		if _, err := p.Token(context.Background()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if calls.Load() != 1 || p.Exchanges() != 1 {
		t.Errorf("expected 1 server call, got %d (exchanges %d)", calls.Load(), p.Exchanges())
	}
}

func TestIAMProvider_RefreshesExpired(t *testing.T) {
	srv, calls := newMockIAMServer(t, time.Hour)
	p := NewIAMProvider(testCreds, srv.Client(), nil)
	p.cached = &Token{Value: "old-token", ExpiresAt: time.Now().Add(time.Minute)} // < 5min threshold

	tok, err := p.Token(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok.Value == "old-token" || calls.Load() != 1 {
		t.Errorf("expected a refresh, got %q after %d calls", tok.Value, calls.Load())
	}
}

func TestIAMProvider_NoCredentials(t *testing.T) {
	for _, c := range []Credentials{{}, {KeyID: "only-id"}, {Secret: "only-secret"}} {
		_, err := NewIAMProvider(c, http.DefaultClient, nil).Token(context.Background())
		var cliErr *clerrors.CLIError
		if !errors.As(err, &cliErr) || cliErr.Code != clerrors.CodeAuthRequired {
			t.Errorf("%+v: expected %s, got %v", c, clerrors.CodeAuthRequired, err)
		}
	}
}

func TestIAMProvider_PreFlightTTL(t *testing.T) {
	// Server returns a token that expires in 5 minutes (< 10min threshold).
	srv, _ := newMockIAMServer(t, 5*time.Minute)
	_, err := NewIAMProvider(testCreds, srv.Client(), nil).Token(context.Background())
	var cliErr *clerrors.CLIError
	if !errors.As(err, &cliErr) || cliErr.Code != clerrors.CodeAuthExpired {
		t.Fatalf("expected %s, got %v", clerrors.CodeAuthExpired, err)
	}
}

func TestFileCache_FreshTokenNeedsNoNetwork(t *testing.T) {
	srv, calls := newMockIAMServer(t, time.Hour)
	cache := FileCache{Path: filepath.Join(t.TempDir(), "cache", "token-default.json")}

	// First run: exchange and store.
	first, err := NewIAMProvider(testCreds, srv.Client(), cache).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Second run (a new process): the token comes from disk.
	p2 := NewIAMProvider(testCreds, srv.Client(), cache)
	second, err := p2.Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || p2.Exchanges() != 0 {
		t.Errorf("second run went to IAM: %d calls, %d exchanges", calls.Load(), p2.Exchanges())
	}
	if second.Value != first.Value {
		t.Errorf("cached token %q != issued %q", second.Value, first.Value)
	}
	if runtime.GOOS != "windows" {
		st, err := os.Stat(cache.Path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Errorf("token cache mode = %04o, want 0600", st.Mode().Perm())
		}
	}
}

func TestFileCache_ExpiredTokenIsRefreshed(t *testing.T) {
	srv, calls := newMockIAMServer(t, time.Hour)
	cache := FileCache{Path: filepath.Join(t.TempDir(), "token-default.json")}
	if err := cache.Store(CacheKey(testCreds), Token{Value: "stale", ExpiresAt: time.Now().Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}

	tok, err := NewIAMProvider(testCreds, srv.Client(), cache).Token(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok.Value == "stale" || calls.Load() != 1 {
		t.Fatalf("expected refresh, got %q after %d calls", tok.Value, calls.Load())
	}
	if stored, ok := cache.Load(CacheKey(testCreds)); !ok || stored.Value != tok.Value {
		t.Errorf("refreshed token not written back: %+v %v", stored, ok)
	}
}

func TestFileCache_OtherKeyIsAMiss(t *testing.T) {
	cache := FileCache{Path: filepath.Join(t.TempDir(), "token-default.json")}
	if err := cache.Store("key-a", Token{Value: "tok-a", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Load("key-b"); ok {
		t.Error("token of another key returned from cache")
	}
	if err := os.WriteFile(cache.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Load("key-a"); ok {
		t.Error("corrupt cache file returned a token")
	}
}

func TestToken_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		ttl    time.Duration
		minTTL time.Duration
		want   bool
	}{
		{"valid with plenty of time", 1 * time.Hour, 5 * time.Minute, true},
		{"valid at boundary", 6 * time.Minute, 5 * time.Minute, true},
		{"invalid below threshold", 4 * time.Minute, 5 * time.Minute, false},
		{"invalid expired", -1 * time.Minute, 5 * time.Minute, false},
		{"zero TTL threshold", 1 * time.Second, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tok := Token{Value: "test", ExpiresAt: time.Now().Add(tt.ttl)}
			if got := tok.IsValid(tt.minTTL); got != tt.want {
				t.Errorf("IsValid(%v) = %v, want %v", tt.minTTL, got, tt.want)
			}
		})
	}
}

// A live cached token of the same key ID does not vouch for another secret:
// the provider goes to IAM, which rejects a wrong secret.
func TestCache_WrongSecretIsAMiss(t *testing.T) {
	srv, calls := newMockIAMServer(t, time.Hour)
	cache := FileCache{Path: filepath.Join(t.TempDir(), "token-default.json")}
	if _, err := NewIAMProvider(testCreds, srv.Client(), cache).Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	wrong := Credentials{KeyID: testCreds.KeyID, Secret: "wrong-secret"}
	if _, err := NewIAMProvider(wrong, srv.Client(), cache).Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("IAM called %d times, want 2: the cached token of the right secret was reused for a wrong one", calls.Load())
	}
	if CacheKey(testCreds) == CacheKey(wrong) {
		t.Fatal("cache key does not depend on the secret")
	}
}
