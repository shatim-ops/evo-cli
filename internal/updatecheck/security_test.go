// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package updatecheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
)

func TestSecurity_SlowlorisBoundedByTimeout(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer func() {
		close(done)
		srv.Close()
	}()
	latestURL = srv.URL

	start := time.Now()
	n := RefreshIfStale(context.Background(), "1.9.2", 250*time.Millisecond)
	elapsed := time.Since(start)
	if n.HasData || n.Available {
		t.Fatalf("slowloris must fail safely, got %+v", n)
	}
	// wall-clock: slowloris server waits 10*time.Second; 250ms client timeout must abort far earlier.
	if elapsed > 10*time.Second/2 {
		t.Fatalf("slowloris not bounded by timeout: elapsed=%v", elapsed)
	}
}

func TestSecurity_FIFONamedPipeDoesNotHang(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mkfifo test")
	}
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	if err := os.MkdirAll(config.CacheDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(cachePath(), 0o600); err != nil {
		t.Fatal(err)
	}

	finished := make(chan Notice, 1)
	go func() {
		finished <- ReadCached("1.9.2")
	}()
	select {
	case n := <-finished:
		if n.HasData || n.Available {
			t.Fatalf("FIFO must be refused, got %+v", n)
		}
	case <-time.After(time.Second):
		t.Fatal("ReadCached hung on FIFO named pipe")
	}
}

func TestSecurity_LocationSpoofingAndCookieIsolation(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)

	badLocs := []string{
		"https://github.com.evil.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3",
		"https://evilgithub.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3",
		"https://attacker:secret@github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3",
		"http://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3\x1b[31m",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3\u202e",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v١.٩.٣",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v1000.0.0",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3\x00",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3\r\nInject: 1",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3/../../other",
		"https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3 ",
	}
	for _, loc := range badLocs {
		if v, err := parseReleaseLocation(loc); err == nil {
			t.Fatalf("malicious Location %q accepted as %q", loc, v)
		}
	}

	t.Run("Set-Cookie from GitHub is never stored or sent on next request", func(t *testing.T) {
		setupTestEnv(t, now)
		var cookies []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookies = append(cookies, r.Header.Get("Cookie"))
			http.SetCookie(w, &http.Cookie{Name: "_gh_sess", Value: "tracking-cookie"})
			w.Header().Set("Location", "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3")
			w.WriteHeader(http.StatusFound)
		}))
		defer srv.Close()
		latestURL = srv.URL

		_ = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
		nowFunc = func() time.Time { return now.Add(25 * time.Hour) }
		_ = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
		if len(cookies) != 2 || cookies[0] != "" || cookies[1] != "" {
			t.Fatalf("unexpected cookies sent: %#v", cookies)
		}
	})
}

func TestSecurity_ZeroSecretLeakage(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	secrets := map[string]string{
		"CLOUDRU_KEY_ID":     "SUPER-SECRET-KEY-ID-999",
		"CLOUDRU_SECRET":     "SUPER-SECRET-VALUE-888",
		"MCP_BEARER_TOKEN":   "SUPER-SECRET-BEARER-777",
		"CLOUDRU_PROJECT_ID": "11111111-2222-3333-4444-555555555555",
	}
	for k, v := range secrets {
		t.Setenv(k, v)
	}

	var reqDump strings.Builder
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqDump.WriteString(r.URL.String())
		for k, vals := range r.Header {
			reqDump.WriteString(k + ":" + strings.Join(vals, ",") + "\n")
		}
		w.Header().Set("Location", "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	latestURL = srv.URL

	_ = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	cacheBytes, err := os.ReadFile(cachePath())
	if err != nil {
		t.Fatal(err)
	}
	combined := reqDump.String() + "\n" + string(cacheBytes)
	for k, v := range secrets {
		if strings.Contains(combined, v) {
			t.Fatalf("secret %s leaked into HTTP request or cache file", k)
		}
	}
	if strings.Contains(string(cacheBytes), "0001-01-01") {
		t.Fatalf("cache should omit zero timestamps cleanly: %s", string(cacheBytes))
	}
}

func TestConfig_UpdateCheckFalsePreservedAcrossSetJournal(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	dir := setupTestEnv(t, now)

	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("update_check: false\nprofiles: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !Disabled("1.9.2") {
		t.Fatal("expected update_check: false to disable checks")
	}
	if err := config.SetJournal(true); err != nil {
		t.Fatalf("SetJournal(true): %v", err)
	}
	if !config.UpdateCheckDisabled() {
		t.Fatal("SetJournal(true) erased update_check: false from config.yaml")
	}
	if err := config.SetJournal(false); err != nil {
		t.Fatalf("SetJournal(false): %v", err)
	}
	if !config.UpdateCheckDisabled() {
		t.Fatal("SetJournal(false) erased update_check: false from config.yaml")
	}
}

func TestSecurity_ConcurrentMCPNotificationsOnce(t *testing.T) {
	now := time.Date(2026, 11, 28, 12, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	saveCache(cacheFile{
		FetchedAt: now,
		NextTryAt: now.Add(24 * time.Hour),
		Latest:    "1.9.3",
	})
	n := ReadCached("1.9.2")

	var mu sync.Mutex
	var delivered atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu.Lock()
			defer mu.Unlock()
			if ShouldNotifyMCP(n) {
				MarkNotifiedMCP(n.Latest)
				delivered.Add(1)
			}
		}()
	}
	wg.Wait()
	if delivered.Load() != 1 {
		t.Fatalf("expected exactly 1 MCP notification delivered across 16 sessions, got %d", delivered.Load())
	}
}
