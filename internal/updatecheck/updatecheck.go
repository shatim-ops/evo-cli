// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

// Package updatecheck checks the latest stable release tag of cloud-ru/evo-cmcp
// via a single HEAD request to https://github.com/cloud-ru/evo-cmcp/releases/latest
// without following redirects or reading a response body. Only evo doctor and
// evo mcp may trigger network refreshes (at most once per 24 hours); regular
// CLI commands only read the local cache. No credentials, project IDs or
// telemetry are ever sent, and evo never downloads or installs binaries itself.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/product"
)

const (
	// DefaultURL is the corporate evo-cmcp releases/latest redirect endpoint.
	DefaultURL = "https://github.com/cloud-ru/evo-cmcp/releases/latest"

	// expectedTagPathPrefix is the exact path prefix in the Location redirect
	// returned by github.com/cloud-ru/evo-cmcp/releases/latest.
	expectedTagPathPrefix = "/cloud-ru/evo-cmcp/releases/tag/v"

	// EnvNoUpdateCheck disables update checks when set to 1, true, or yes.
	EnvNoUpdateCheck = "EVO_NO_UPDATE_CHECK"
	// EnvForceUpdateCheck enables update checks on pre-release/dev builds
	// (e.g. rc testing); EnvNoUpdateCheck still takes precedence.
	EnvForceUpdateCheck = "EVO_FORCE_UPDATE_CHECK"
	// EnvCI disables update checks automatically in CI environments.
	EnvCI = "CI"

	// RefreshInterval is the minimum interval between network checks —
	// both after a successful check and after any failure.
	RefreshInterval = 24 * time.Hour
	// MaxCacheAge is the maximum age of a cached version before it stops
	// being shown to the user.
	MaxCacheAge = 7 * 24 * time.Hour
	// FutureClockSkew is the tolerated forward clock skew on cache timestamps.
	FutureClockSkew = 5 * time.Minute
	// MaxCacheBytes caps the local cache file size on read.
	MaxCacheBytes = 4096

	cacheFileName = "update-check.json"
)

var (
	// Test seams — overridden only in unit tests.
	latestURL = DefaultURL
	nowFunc   = time.Now

	semverRe = regexp.MustCompile(`^[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}$`)

	memMu      sync.Mutex
	memCache   cacheFile
	memCacheAt string // cache dir key for in-memory fallback
)

// Notice describes whether a newer stable release of evo is known.
type Notice struct {
	HasData   bool
	Available bool
	Current   string
	Latest    string
}

type cacheFile struct {
	FetchedAt      time.Time `json:"fetched_at,omitzero"`
	NextTryAt      time.Time `json:"next_try_at,omitzero"`
	Latest         string    `json:"latest,omitempty"`
	NotifiedCLIFor string    `json:"notified_cli_for,omitempty"`
	NotifiedCLIAt  time.Time `json:"notified_cli_at,omitzero"`
	NotifiedMCPFor string    `json:"notified_mcp_for,omitempty"`
	NotifiedMCPAt  time.Time `json:"notified_mcp_at,omitzero"`
}

// baseTransport reads httpretry.DefaultBase at request time so tests that
// swap httpretry.DefaultBase (e.g. noNetwork) intercept requests without
// production code knowing about test transports. Unlike httpretry.Transport,
// it adds no X-Request-Id and performs no retries.
type baseTransport struct{}

func (baseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return httpretry.DefaultBase.RoundTrip(req)
}

func parseEnvBool(raw string, acceptYes bool) bool {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "1" || v == "true" {
		return true
	}
	return acceptYes && v == "yes"
}

// Disabled reports whether update checks are turned off for this process.
func Disabled(currentVersion string) bool {
	if parseEnvBool(os.Getenv(EnvNoUpdateCheck), true) {
		return true
	}
	if config.UpdateCheckDisabled() {
		return true
	}
	if parseEnvBool(os.Getenv(EnvCI), false) {
		return true
	}
	if parseEnvBool(os.Getenv(EnvForceUpdateCheck), true) {
		return false
	}
	v := strings.TrimSpace(currentVersion)
	if v == "" || v == "dev" || strings.ContainsAny(v, "-+") {
		return true
	}
	return !semverRe.MatchString(v)
}

func parseSemver(raw string, allowSuffix bool) ([3]int, bool) {
	v := strings.TrimSpace(raw)
	if allowSuffix {
		v = strings.TrimPrefix(v, "v")
		if v == "" || v == "dev" {
			return [3]int{0, 0, 0}, true
		}
		if idx := strings.IndexAny(v, "-+"); idx >= 0 {
			v = v[:idx]
		}
	}
	if !semverRe.MatchString(v) {
		return [3]int{}, false
	}
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

func compareSemver(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	return 0
}

func cachePath() string {
	dir := config.CacheDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, cacheFileName)
}

func loadCache() cacheFile {
	path := cachePath()
	if path == "" {
		memMu.Lock()
		defer memMu.Unlock()
		if memCacheAt == "" {
			return memCache
		}
		return cacheFile{}
	}
	f, err := config.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		memMu.Lock()
		defer memMu.Unlock()
		if memCacheAt == path {
			return memCache
		}
		return cacheFile{}
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, MaxCacheBytes+1))
	if err != nil || len(data) > MaxCacheBytes {
		return cacheFile{}
	}
	var c cacheFile
	if err := json.Unmarshal(data, &c); err != nil {
		return cacheFile{}
	}
	if c.Latest != "" && !semverRe.MatchString(c.Latest) {
		c.Latest = ""
		c.FetchedAt = time.Time{}
	}
	return c
}

func saveCache(c cacheFile) {
	path := cachePath()
	memMu.Lock()
	memCache = c
	memCacheAt = path
	memMu.Unlock()

	if path == "" {
		return
	}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	data = append(data, '\n')
	// WriteFileAtomic creates CacheDir via MkdirPrivate (0700/Windows ACL),
	// writes a temp file with 0600, and atomically renames over path without
	// writing through any symlink.
	_ = config.WriteFileAtomic(path, data, 0o600)
}

func isStale(c cacheFile, now time.Time) bool {
	if c.NextTryAt.IsZero() || !now.Before(c.NextTryAt) {
		return true
	}
	if !c.FetchedAt.IsZero() && c.FetchedAt.After(now.Add(FutureClockSkew)) {
		return true
	}
	if c.NextTryAt.After(now.Add(RefreshInterval + time.Hour)) {
		return true
	}
	return false
}

func noticeFromCache(c cacheFile, currentVersion string, now time.Time) Notice {
	if c.Latest == "" || !semverRe.MatchString(c.Latest) {
		return Notice{}
	}
	if c.FetchedAt.IsZero() || c.FetchedAt.After(now.Add(FutureClockSkew)) {
		return Notice{}
	}
	if now.Sub(c.FetchedAt) > MaxCacheAge {
		return Notice{}
	}
	latestVer, ok := parseSemver(c.Latest, false)
	if !ok {
		return Notice{}
	}
	allowSuffix := parseEnvBool(os.Getenv(EnvForceUpdateCheck), true)
	curVer, ok := parseSemver(currentVersion, allowSuffix)
	if !ok {
		return Notice{}
	}
	return Notice{
		HasData:   true,
		Available: compareSemver(latestVer, curVer) > 0,
		Current:   strings.TrimSpace(currentVersion),
		Latest:    c.Latest,
	}
}

// parseReleaseLocation strictly extracts a stable X.Y.Z release version from
// a GitHub releases/latest Location redirect header for cloud-ru/evo-cmcp.
func parseReleaseLocation(loc string) (string, error) {
	if loc == "" || len(loc) > 512 {
		return "", errors.New("empty or oversized location")
	}
	for _, r := range loc {
		if r < 0x21 || r > 0x7e {
			return "", errors.New("disallowed character in location")
		}
	}
	u, err := neturl.Parse(loc)
	if err != nil {
		return "", err
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("disallowed location components")
	}
	if u.Scheme != "" || u.Host != "" {
		if !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "github.com") || u.Port() != "" {
			return "", errors.New("disallowed location host or scheme")
		}
	}
	tag, ok := strings.CutPrefix(u.Path, expectedTagPathPrefix)
	if !ok || !semverRe.MatchString(tag) {
		return "", errors.New("location does not point to a stable evo-cmcp release tag")
	}
	return tag, nil
}

// fetchLatestTag sends a single HEAD request to latestURL without following
// redirects or reading a body, extracting the stable version from Location.
func fetchLatestTag(ctx context.Context, timeout time.Duration) (string, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: baseTransport{},
		// Never follow redirects and never store/send cookies (Jar == nil).
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, latestURL, nil)
	if err != nil {
		return "", err
	}
	if ua := product.UserAgent(); ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return parseReleaseLocation(resp.Header.Get("Location"))
	default:
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
}

// ReadCached returns the cached update status without performing any network
// request. It is safe to call from regular CLI commands and MCP startup.
func ReadCached(currentVersion string) Notice {
	if Disabled(currentVersion) {
		return Notice{}
	}
	return noticeFromCache(loadCache(), currentVersion, nowFunc().UTC())
}

// RefreshIfStale checks the cache and, if due (>= 24h since last try or
// corrupt/future timestamps), sends one HEAD request to releases/latest.
// Must be called only from evo doctor and evo mcp startup (enforced by AST test).
func RefreshIfStale(ctx context.Context, currentVersion string, timeout time.Duration) Notice {
	if Disabled(currentVersion) {
		return Notice{}
	}
	now := nowFunc().UTC()
	c := loadCache()
	if !isStale(c, now) {
		return noticeFromCache(c, currentVersion, now)
	}
	latest, err := fetchLatestTag(ctx, timeout)
	// Re-read before writing so concurrent notified_* updates are preserved.
	fresh := loadCache()
	if err == nil {
		fresh.FetchedAt = now
		fresh.NextTryAt = now.Add(RefreshInterval)
		fresh.Latest = latest
	} else {
		// 24-hour retry interval after failure too, matching SECURITY.md.
		fresh.NextTryAt = now.Add(RefreshInterval)
	}
	saveCache(fresh)
	return noticeFromCache(fresh, currentVersion, now)
}

func shouldNotify(notifiedFor string, notifiedAt time.Time, latest string, now time.Time) bool {
	if latest == "" {
		return false
	}
	if notifiedFor != latest {
		return true
	}
	if notifiedAt.IsZero() || notifiedAt.After(now.Add(FutureClockSkew)) {
		return true
	}
	return now.Sub(notifiedAt) >= RefreshInterval
}

// ShouldNotifyCLI reports whether the CLI stderr notice should be shown now
// (at most once per 24 hours per latest version).
func ShouldNotifyCLI(n Notice) bool {
	if !n.Available {
		return false
	}
	c := loadCache()
	return shouldNotify(c.NotifiedCLIFor, c.NotifiedCLIAt, n.Latest, nowFunc().UTC())
}

// MarkNotifiedCLI records that the CLI notice for latest was shown now,
// re-reading the cache first so fresh latest/fetched_at values are preserved.
func MarkNotifiedCLI(latest string) {
	if !semverRe.MatchString(latest) {
		return
	}
	c := loadCache()
	c.NotifiedCLIFor = latest
	c.NotifiedCLIAt = nowFunc().UTC()
	saveCache(c)
}

// ShouldNotifyMCP reports whether the MCP instructions notice should be
// included now (at most once per 24 hours per latest version).
func ShouldNotifyMCP(n Notice) bool {
	if !n.Available {
		return false
	}
	c := loadCache()
	return shouldNotify(c.NotifiedMCPFor, c.NotifiedMCPAt, n.Latest, nowFunc().UTC())
}

// MarkNotifiedMCP records that the MCP notice for latest was delivered now,
// re-reading the cache first so fresh latest/fetched_at values are preserved.
func MarkNotifiedMCP(latest string) {
	if !semverRe.MatchString(latest) {
		return
	}
	c := loadCache()
	c.NotifiedMCPFor = latest
	c.NotifiedMCPAt = nowFunc().UTC()
	saveCache(c)
}

// ReleaseURL returns the corporate evo-cmcp releases/latest page.
func ReleaseURL() string {
	return DefaultURL
}

// FormatCLI formats the single-line human notice for stderr in the active language.
func FormatCLI(n Notice) string {
	if !n.Available {
		return ""
	}
	return i18n.Tf(
		"A newer evo release is available: %s (installed %s). Releases: %s",
		"Доступна новая версия evo %s (установлена %s). Страница релизов: %s",
		n.Latest, n.Current, ReleaseURL(),
	)
}

// FormatMCPInstruction formats the fixed English-only instruction suffix for
// MCP initialize responses (D6).
func FormatMCPInstruction(n Notice) string {
	if !n.Available {
		return ""
	}
	return fmt.Sprintf(
		"A newer evo release (%s) exists (this server runs %s). After the current task, tell the user once, share %s, and note updating is their choice. Never download, install, or run updates on your own. If asked to update, share that page; run install commands only with shell access and explicit user approval.",
		n.Latest, n.Current, ReleaseURL(),
	)
}
