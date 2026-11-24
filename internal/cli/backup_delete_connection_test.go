// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"net/http"
	"strings"
	"testing"
)

func TestBackupDeleteRejectsForeignCard(t *testing.T) {
	for _, surface := range []string{"preview", "confirmed", "mcp preview", "mcp confirmed"} {
		t.Run(surface, func(t *testing.T) {
			withParityWorld(t)
			writes := 0
			rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" {
					writes++
					return contractRT{status: 400, body: "{}"}.RoundTrip(r)
				}
				if r.URL.Path == "/api/v1/backups" {
					return contractRT{status: 200, body: `{"items":[],"total":0}`}.RoundTrip(r)
				}
				if r.URL.Path != "/api/v1/backups/"+parityDiskBackup {
					t.Fatalf("%s %s", r.Method, r.URL.Path)
				}
				return contractRT{status: 200, body: fmt.Sprintf(`{"id":%q,"name":"different-backup","state":"created"}`, parityVM)}.RoundTrip(r)
			})
			prev := toolEngine
			a := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
			toolEngine = func(o map[string]any) (*engine.Engine, error) {
				e, err := prev(o)
				if err == nil {
					e.Cloud = a
				}
				return e, err
			}
			t.Cleanup(func() { toolEngine = prev })
			var out string
			if !strings.HasPrefix(surface, "mcp") {
				flag := "--dry-run"
				if surface == "confirmed" {
					flag = "--yes"
				}
				code, so, se := runCLI(t, "", "--json", flag, "backup", "delete", parityDiskBackup)
				if code == 0 {
					t.Error("wrong card accepted")
				}
				out = so + se
			} else {
				s := newROServers(t)
				a, _ := s.stdio(t, s.plain, "tools/call", map[string]any{"name": "backup_delete", "arguments": map[string]any{"backup_id": parityDiskBackup, "confirmed": surface == "mcp confirmed"}})
				if !a.isErr {
					t.Error("wrong card accepted")
				}
				out = a.text
			}

			if writes != 0 || strings.Contains(out, "different-backup") || !strings.Contains(out, "API_ERROR") {
				t.Fatalf("wrong card: writes=%d output=%s", writes, out)
			}
		})
	}
}

func TestBackupDeleteConnectionRefused(t *testing.T) {
	checkDeleteConnectionRefused(t, "backup", "delete", "backup_delete", "backup_id", "backups", parityDiskBackup)
}
