// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

func TestGlobalAZPreviewAndAlias(t *testing.T) {
	for _, tc := range []struct{ flag, value string }{
		{"--az", "ru.AZ-1"}, {"--az", pvZone}, {"--region", "ru.AZ-1"}, {"--region", pvZone},
	} {
		t.Run(tc.flag+tc.value, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			t.Setenv("CLOUDRU_REGION", "unknown-profile-zone")
			t.Setenv("EVO_LANG", "ru")
			defer i18n.SetLang(i18n.Lang())
			i18n.SetLang(i18n.RU)
			code, out, stderr := runCLI(t, "", tc.flag, tc.value, "snat", "create", "az-test", "--dry-run", "--json")
			if code != 0 || !json.Valid([]byte(out)) || !strings.Contains(out, pvZone) || strings.Contains(out, "warning\":\"--region") || len(w.rt.takeWrites()) != 0 {
				t.Fatal(code, out, stderr)
			}
			if tc.flag == "--region" {
				var warn struct{ Warning string }
				if err := json.Unmarshal([]byte(stderr), &warn); err != nil || !strings.Contains(warn.Warning, "--az") || !strings.Contains(warn.Warning, "2.0") || hasCyrillic(warn.Warning) {
					t.Fatal(stderr, err)
				}
			} else if strings.Contains(stderr, "--region") {
				t.Fatal(stderr)
			}
		})
	}
}

func TestGlobalAZRejectsEmpty(t *testing.T) {
	w, _ := withJSONWorld(t)
	code, out, stderr := runCLI(t, "", "--json", "snat", "create", "az-test", "--az= ", "--yes")
	if code != 1 || !strings.Contains(out+stderr, "USAGE") || !strings.Contains(out+stderr, "empty") || len(w.rt.takeWrites()) != 0 {
		t.Fatal(code, out, stderr)
	}
}

func TestGlobalAZConfigureAndConflict(t *testing.T) {
	fc := setupCLI(t)
	fc.zones = zoneList
	saveProfile(t, "default", pvProject)
	for _, args := range [][]string{{"--az", "ru.az-2", "configure"}, {"configure", "--az", zoneAZ2}} {
		code, _, stderr := runCLI(t, "", args...)
		p, _, _, err := config.LoadProfile("default")
		if code != 0 || err != nil || p.Region != zoneAZ2 || strings.Contains(stderr, "--region") {
			t.Fatal(args, code, stderr, p.Region, err)
		}
	}
	for _, args := range [][]string{{"--region", zoneAZ1, "configure", "--az", zoneAZ1}, {"--az", zoneAZ2, "configure", "--region", zoneAZ1}} {
		before := fc.zoneCalls.Load()
		code, _, stderr := runCLI(t, "", append([]string{"--json"}, args...)...)
		p, _, _, err := config.LoadProfile("default")
		if code != 1 || !strings.Contains(stderr, "USAGE") || !strings.Contains(stderr, "--az") || !strings.Contains(stderr, "--region") || p.Region != zoneAZ2 || err != nil || fc.zoneCalls.Load() != before {
			t.Fatal(args, code, stderr, p.Region, err)
		}
	}
}

func TestGlobalAZMCPServerScope(t *testing.T) {
	w, _ := withJSONWorld(t)
	t.Setenv("CLOUDRU_REGION", "another-zone")
	t.Setenv("EVO_LANG", "ru")
	defer i18n.SetLang(i18n.Lang())
	i18n.SetLang(i18n.RU)
	oldClient := httpClient
	httpClient = &http.Client{Transport: w.rt}
	t.Cleanup(func() { httpClient = oldClient })
	e, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	cache := auth.FileCache{Path: config.TokenCachePath(e.Config.Profile)}
	key := auth.CacheKey(auth.Credentials{KeyID: e.Config.KeyID, Secret: e.Config.Secret})
	if err := cache.Store(key, auth.Token{Value: "fake-token", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	old := w.rt.extra
	w.rt.extra = func(req *http.Request) (*http.Response, error) {
		if req.URL.Path == "/api/v1/availability-zones" {
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(zoneList)), Request: req}, nil
		}
		return old(req)
	}
	body := roRequest("tools/call", map[string]any{"name": "snat_create", "arguments": map[string]any{"name": "az-test"}})
	code, out, stderr := runCLI(t, stdioInit+string(body)+"\n", "mcp", "--az", "ru.AZ-2")
	if code != 0 {
		t.Fatal(code, stderr)
	}
	_, reply, _ := strings.Cut(out, "\n")
	answer, _ := roDecode(t, []byte(reply))
	if !strings.Contains(answer.text, zoneAZ2) || hasCyrillic(answer.text) || len(w.rt.takeWrites()) != 0 {
		t.Fatal(answer, stderr)
	}
}

func TestGlobalAZLegacyWarningLanguage(t *testing.T) {
	setupCLI(t)
	for _, lang := range []string{i18n.RU, i18n.EN} {
		t.Run(lang, func(t *testing.T) {
			t.Setenv("EVO_LANG", lang)
			defer i18n.SetLang(i18n.Lang())
			i18n.SetLang(lang)
			code, out, stderr := runCLI(t, "", "version", "--region", zoneAZ1)
			if code != 0 || strings.Contains(out, "--az") || strings.Count(stderr, "--az") != 1 || !strings.Contains(stderr, "2.0") || hasCyrillic(stderr) != (lang == i18n.RU) {
				t.Fatal(code, out, stderr)
			}
		})
	}
}

func TestGlobalAZLocalZoneWins(t *testing.T) {
	w, _ := withJSONWorld(t)
	code, out, stderr := runCLI(t, "", "--az", "unlisted", "snat", "create", "az-test", "--zone", "ru.AZ-1", "--dry-run", "--json")
	if code != 0 || !strings.Contains(out, pvZone) || len(w.rt.takeWrites()) != 0 {
		t.Fatal(code, out, stderr)
	}
}

func TestGlobalAZUnknownName(t *testing.T) {
	w, _ := withJSONWorld(t)
	code, out, stderr := runCLI(t, "", "snat", "create", "az-test", "--az", "unlisted", "--yes", "--json")
	if code != 1 || !strings.Contains(out+stderr, "USAGE") || len(w.rt.takeWrites()) != 0 {
		t.Fatal(code, out, stderr)
	}
}

func TestGlobalAZDeploy(t *testing.T) {
	w := withParityWorld(t)
	writeFile(t, filepath.Join(w.dir, "docker-compose.yml"), "services:\n  web:\n    image: nginx:1.27\n")
	t.Setenv("CLOUDRU_REGION", "unlisted")
	code, out, stderr := runCLI(t, "", "deploy", "--az", pvZone, "--dry-run", "--json")
	if code != 0 || !strings.Contains(out, pvZone) || len(w.rt.takeWrites()) != 0 {
		t.Fatal(code, out, stderr)
	}
}
