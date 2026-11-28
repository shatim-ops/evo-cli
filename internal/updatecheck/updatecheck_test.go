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
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/product"
)

func setupTestEnv(t *testing.T, now time.Time) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLOUDRU_CONFIG_DIR", dir)
	t.Setenv(EnvCI, "")
	t.Setenv(EnvNoUpdateCheck, "")
	t.Setenv(EnvForceUpdateCheck, "")

	origURL := latestURL
	origNow := nowFunc
	origBase := httpretry.DefaultBase

	nowFunc = func() time.Time { return now }
	memMu.Lock()
	memCache = cacheFile{}
	memCacheAt = ""
	memMu.Unlock()

	t.Cleanup(func() {
		latestURL = origURL
		nowFunc = origNow
		httpretry.DefaultBase = origBase
		memMu.Lock()
		memCache = cacheFile{}
		memCacheAt = ""
		memMu.Unlock()
	})
	return dir
}

func TestRefresh_HeadOnly_Headers_NoRedirectFollow_AndSingleAttemptOn500(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)
	product.SetVersion("1.9.2")

	var hits atomic.Int32
	var gotMethod string
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	latestURL = srv.URL + "/cloud-ru/evo-cmcp/releases/latest"

	n := RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	if n.HasData || n.Available {
		t.Fatalf("expected empty notice on 500, got %+v", n)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected exactly 1 attempt on 500 (no retries), got %d", hits.Load())
	}
	if gotMethod != http.MethodHead {
		t.Fatalf("method = %q, want HEAD", gotMethod)
	}
	if got := gotHeaders.Get("X-Request-Id"); got != "" {
		t.Fatalf("X-Request-Id must not be sent, got %q", got)
	}
	if got := gotHeaders.Get("Authorization"); got != "" {
		t.Fatalf("Authorization must not be sent, got %q", got)
	}
	if got := gotHeaders.Get("Cookie"); got != "" {
		t.Fatalf("Cookie must not be sent, got %q", got)
	}
	if got := gotHeaders.Get("User-Agent"); !strings.HasPrefix(got, "evo/") {
		t.Fatalf("User-Agent = %q, want prefix evo/", got)
	}

	// Next call within 24h must NOT hit network (24h retry interval after failure).
	nowFunc = func() time.Time { return now.Add(23 * time.Hour) }
	_ = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	if hits.Load() != 1 {
		t.Fatalf("expected 24h backoff after 500, got %d hits", hits.Load())
	}

	// After 24h, retry is allowed.
	nowFunc = func() time.Time { return now.Add(24 * time.Hour) }
	_ = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	if hits.Load() != 2 {
		t.Fatalf("expected retry after 24h, got %d hits", hits.Load())
	}
}

func TestRefresh_RedirectNotFollowedAndValidLocationParsed(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	latestURL = srv.URL

	n := RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	if !n.HasData || !n.Available || n.Latest != "1.9.3" {
		t.Fatalf("expected valid notice for 1.9.3, got %+v", n)
	}
	if hits.Load() != 1 {
		t.Fatalf("client must not follow redirects (expected 1 hit), got %d", hits.Load())
	}
}

func TestRefresh_InvalidResponsesRejected(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		status   int
		location string
	}{
		{"404", http.StatusNotFound, ""},
		{"200 no redirect", http.StatusOK, "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3"},
		{"empty location", http.StatusFound, ""},
		{"foreign host", http.StatusFound, "https://evil.example/cloud-ru/evo-cmcp/releases/tag/v1.9.3"},
		{"wrong repo", http.StatusFound, "https://github.com/other/repo/releases/tag/v1.9.3"},
		{"http scheme", http.StatusFound, "http://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3"},
		{"prerelease tag", http.StatusFound, "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3-rc.1"},
		{"missing v prefix", http.StatusFound, "https://github.com/cloud-ru/evo-cmcp/releases/tag/1.9.3"},
		{"query string", http.StatusFound, "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3?x=1"},
		{"fragment", http.StatusFound, "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3#top"},
		{"userinfo", http.StatusFound, "https://user:pass@github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3"},
		{"custom port", http.StatusFound, "https://github.com:8443/cloud-ru/evo-cmcp/releases/tag/v1.9.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupTestEnv(t, now)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.location != "" {
					w.Header().Set("Location", tc.location)
				}
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()
			latestURL = srv.URL

			n := RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
			if n.HasData || n.Available {
				t.Fatalf("case %s: expected empty notice, got %+v", tc.name, n)
			}
		})
	}
}

func TestCache_TTL_FutureClock_MaxAge_CurrentNewer(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Location", "/cloud-ru/evo-cmcp/releases/tag/v1.9.3")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	latestURL = srv.URL

	n := RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	if !n.HasData || !n.Available || n.Latest != "1.9.3" {
		t.Fatalf("got %+v", n)
	}
	if hits.Load() != 1 {
		t.Fatalf("hits=%d, want 1", hits.Load())
	}

	// Within 24h: no network hit.
	nowFunc = func() time.Time { return now.Add(12 * time.Hour) }
	n = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	if !n.Available || hits.Load() != 1 {
		t.Fatalf("expected cached hit, n=%+v hits=%d", n, hits.Load())
	}

	// Current newer than latest (after rollback): silent.
	nNewer := ReadCached("1.9.4")
	if !nNewer.HasData || nNewer.Available {
		t.Fatalf("expected Available=false when current>latest, got %+v", nNewer)
	}

	// Cache older than 7 days: hidden from display.
	nowFunc = func() time.Time { return now.Add(MaxCacheAge + time.Minute) }
	if stale := ReadCached("1.9.2"); stale.HasData || stale.Available {
		t.Fatalf("expected cache >7d hidden, got %+v", stale)
	}

	// Future fetched_at (>5m): hidden on ReadCached and triggers refresh on RefreshIfStale.
	saveCache(cacheFile{
		FetchedAt: now.Add(time.Hour),
		NextTryAt: now.Add(24 * time.Hour),
		Latest:    "9.9.9",
	})
	nowFunc = func() time.Time { return now }
	if fut := ReadCached("1.9.2"); fut.HasData || fut.Available {
		t.Fatalf("expected future fetched_at ignored, got %+v", fut)
	}
	_ = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
	if hits.Load() != 2 {
		t.Fatalf("expected future cache to trigger refresh, hits=%d", hits.Load())
	}
}

func TestCache_SecurityAndResilience(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	// 1. Symlink on read is refused by OpenNoFollow.
	if runtime.GOOS != "windows" {
		target := filepath.Join(t.TempDir(), "victim.json")
		if err := os.WriteFile(target, []byte(`{"fetched_at":"2026-11-28T09:00:00Z","latest":"9.9.9"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(config.CacheDir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, cachePath()); err != nil {
			t.Fatal(err)
		}
		if n := ReadCached("1.9.2"); n.HasData || n.Available {
			t.Fatalf("symlink cache must not be read, got %+v", n)
		}
		_ = os.Remove(cachePath())
	}

	// 2. Oversized cache file (>4KB) is ignored.
	if err := os.MkdirAll(config.CacheDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	big := `{"fetched_at":"2026-11-28T09:00:00Z","latest":"9.9.9","pad":"` + strings.Repeat("x", MaxCacheBytes+50) + `"}`
	if err := os.WriteFile(cachePath(), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if n := ReadCached("1.9.2"); n.HasData {
		t.Fatalf("oversized cache must be ignored, got %+v", n)
	}

	// 3. Permissions 0600 on Unix and concurrent writes (-race).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.3")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	latestURL = srv.URL

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
			MarkNotifiedCLI("1.9.3")
			MarkNotifiedMCP("1.9.3")
		}()
	}
	wg.Wait()

	if runtime.GOOS != "windows" {
		st, err := os.Stat(cachePath())
		if err != nil {
			t.Fatal(err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Fatalf("cache perm = %o, want 0600", perm)
		}
	}

	// 4. Read-only cache dir falls back to in-memory without error.
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		roBase := t.TempDir()
		roCache := filepath.Join(roBase, "cache")
		if err := os.MkdirAll(roCache, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(roCache, 0o700) })
		t.Setenv("CLOUDRU_CONFIG_DIR", roBase)
		n := RefreshIfStale(context.Background(), "1.9.2", 2*time.Second)
		if !n.Available || n.Latest != "1.9.3" {
			t.Fatalf("expected in-memory notice in read-only cache dir, got %+v", n)
		}
		if n2 := ReadCached("1.9.2"); !n2.Available {
			t.Fatalf("expected ReadCached to return in-memory notice, got %+v", n2)
		}
	}
}

func TestDisabled_AllModes(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	dir := setupTestEnv(t, now)

	if Disabled("1.9.2") {
		t.Fatal("expected enabled for clean stable version")
	}
	// EVO_NO_UPDATE_CHECK=0 must NOT disable.
	t.Setenv(EnvNoUpdateCheck, "0")
	if Disabled("1.9.2") {
		t.Fatal("EVO_NO_UPDATE_CHECK=0 must not disable")
	}
	for _, val := range []string{"1", "true", "TRUE", "yes"} {
		t.Setenv(EnvNoUpdateCheck, val)
		if !Disabled("1.9.2") {
			t.Fatalf("EVO_NO_UPDATE_CHECK=%s must disable", val)
		}
	}
	t.Setenv(EnvNoUpdateCheck, "")

	// CI=true disables.
	t.Setenv(EnvCI, "true")
	if !Disabled("1.9.2") {
		t.Fatal("CI=true must disable")
	}
	t.Setenv(EnvCI, "")

	// Suffix / dev versions disabled by default, enabled by FORCE (with suffix stripped).
	if !Disabled("dev") || !Disabled("1.9.2-rc.1") || !Disabled("1.9.2+dirty") {
		t.Fatal("dev/suffix versions must be disabled by default")
	}
	t.Setenv(EnvForceUpdateCheck, "1")
	if Disabled("1.9.2-rc.1") || Disabled("dev") {
		t.Fatal("EVO_FORCE_UPDATE_CHECK=1 must enable suffix/dev versions")
	}
	// EVO_NO_UPDATE_CHECK=1 beats EVO_FORCE_UPDATE_CHECK=1.
	t.Setenv(EnvNoUpdateCheck, "1")
	if !Disabled("1.9.2-rc.1") {
		t.Fatal("EVO_NO_UPDATE_CHECK=1 must override EVO_FORCE_UPDATE_CHECK=1")
	}
	t.Setenv(EnvNoUpdateCheck, "")

	// rc comparison under FORCE: 1.9.2-rc.1 == 1.9.2, so 1.9.2 is not newer, 1.9.3 is newer.
	saveCache(cacheFile{FetchedAt: now, NextTryAt: now.Add(24 * time.Hour), Latest: "1.9.2"})
	if n := ReadCached("1.9.2-rc.1"); !n.HasData || n.Available {
		t.Fatalf("1.9.2-rc.1 vs 1.9.2 should not be Available, got %+v", n)
	}
	saveCache(cacheFile{FetchedAt: now, NextTryAt: now.Add(24 * time.Hour), Latest: "1.9.3"})
	if n := ReadCached("1.9.2-rc.1"); !n.HasData || !n.Available {
		t.Fatalf("1.9.2-rc.1 vs 1.9.3 should be Available, got %+v", n)
	}
	t.Setenv(EnvForceUpdateCheck, "")

	// config.yaml update_check: false disables.
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("update_check: false\nprofiles: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !Disabled("1.9.2") {
		t.Fatal("update_check: false in config.yaml must disable")
	}
}

func TestNotificationThrottle_OncePer24hAndPreservesCache(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	setupTestEnv(t, now)

	saveCache(cacheFile{
		FetchedAt: now,
		NextTryAt: now.Add(24 * time.Hour),
		Latest:    "1.9.3",
	})
	n := ReadCached("1.9.2")
	if !ShouldNotifyCLI(n) || !ShouldNotifyMCP(n) {
		t.Fatal("both channels should notify initially")
	}
	MarkNotifiedCLI("1.9.3")
	if ShouldNotifyCLI(n) {
		t.Fatal("CLI should be throttled within 24h")
	}
	if !ShouldNotifyMCP(n) {
		t.Fatal("MCP should still notify when only CLI notified")
	}
	MarkNotifiedMCP("1.9.3")
	if ShouldNotifyMCP(n) {
		t.Fatal("MCP should be throttled within 24h")
	}

	saveCache(cacheFile{
		FetchedAt: now.Add(time.Minute),
		NextTryAt: now.Add(24*time.Hour + time.Minute),
		Latest:    "1.9.4",
	})
	MarkNotifiedCLI("1.9.3")
	after := loadCache()
	if after.Latest != "1.9.4" {
		t.Fatalf("MarkNotifiedCLI overwrote newer latest: got %q, want 1.9.4", after.Latest)
	}

	n2 := ReadCached("1.9.2")
	if !ShouldNotifyCLI(n2) || !ShouldNotifyMCP(n2) {
		t.Fatal("new latest version must notify immediately")
	}

	MarkNotifiedCLI("1.9.4")
	nowFunc = func() time.Time { return now.Add(24*time.Hour + time.Second) }
	if !ShouldNotifyCLI(n2) {
		t.Fatal("CLI should notify again after 24h")
	}
}

func TestFormatters_SafeAndUniversal(t *testing.T) {
	if s := FormatCLI(Notice{HasData: true, Available: false, Current: "1.9.2", Latest: "1.9.2"}); s != "" {
		t.Fatalf("FormatCLI must be empty when not available, got %q", s)
	}
	if s := FormatMCPInstruction(Notice{HasData: true, Available: false, Current: "1.9.2", Latest: "1.9.2"}); s != "" {
		t.Fatalf("FormatMCPInstruction must be empty when not available, got %q", s)
	}

	cliMsg := FormatCLI(Notice{HasData: true, Available: true, Current: "1.9.2", Latest: "1.9.3"})
	if !strings.Contains(cliMsg, "1.9.3") || !strings.Contains(cliMsg, "1.9.2") || !strings.Contains(cliMsg, ReleaseURL()) {
		t.Fatalf("FormatCLI missing version or release URL: %q", cliMsg)
	}
	if strings.Contains(cliMsg, "\n") {
		t.Fatalf("FormatCLI must be single-line: %q", cliMsg)
	}

	// Even worst-case long versions (3-digit semver + dev suffix) stay <= 400 bytes.
	mcpMsg := FormatMCPInstruction(Notice{
		HasData:   true,
		Available: true,
		Current:   "999.999.999-rc.1+dev",
		Latest:    "999.999.999",
	})
	if len(mcpMsg) > 399 {
		t.Fatalf("FormatMCPInstruction too long (%d bytes > 399): %q", len(mcpMsg), mcpMsg)
	}
	for _, must := range []string{
		ReleaseURL(),
		"Never download, install, or run updates on your own.",
		"explicit user approval",
	} {
		if !strings.Contains(mcpMsg, must) {
			t.Fatalf("FormatMCPInstruction missing %q: %q", must, mcpMsg)
		}
	}
}

func FuzzParseReleaseLocation(f *testing.F) {
	f.Add("https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.2")
	f.Add("/cloud-ru/evo-cmcp/releases/tag/v1.9.2")
	f.Add("https://github.com/cloud-ru/evo-cmcp/releases/tag/v1.9.2-rc.1")
	f.Add("https://evil.example/cloud-ru/evo-cmcp/releases/tag/v1.9.2")
	f.Fuzz(func(t *testing.T, loc string) {
		v, err := parseReleaseLocation(loc)
		if err == nil && !semverRe.MatchString(v) {
			t.Fatalf("accepted non-semver %q from %q", v, loc)
		}
	})
}
