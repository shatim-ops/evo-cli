// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func TestBackupDeleteLostReplySurfaces(t *testing.T) {
	for _, tc := range []struct {
		name                string
		before, send, after int // send=0 means that the answer was lost.
		status, reason      string
		writes              int
	}{
		{"lost then gone", 200, 0, 404, "deleted", "", 1},
		{"initially absent", 404, 0, 404, "already_deleted", "", 0},
		{"read forbidden", 403, 0, 404, "", clierrors.CodeAuthFailed, 0},
		{"delete forbidden", 200, 403, 404, "", clierrors.CodeAuthFailed, 1},
		{"check forbidden", 200, 0, 403, "", clierrors.CodeAPIUnreachable, 1},
		{"still present", 200, 0, 200, "", clierrors.CodeAPIUnreachable, 1},
	} {
		for _, surface := range []string{"cli", "mcp"} {
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				withParityWorld(t)
				old := toolEngine
				writes, checks := 0, 0
				rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					body := parityDiskBackupJSON
					code := tc.before
					switch {
					case req.Method == "GET" && req.URL.Path == "/api/v1/backups":
						code, body = 200, `{"items":[],"total":0}`
					case req.URL.Path != "/api/v1/backups/"+parityDiskBackup:
						t.Errorf("unexpected %s %s", req.Method, req.URL)
						code = 400
					case req.Method == "DELETE":
						writes++
						code = tc.send
						if code == 0 && writes == 1 {
							return nil, errors.New("read tcp: connection reset by peer")
						}
						if code == 0 {
							code = http.StatusNotFound
						}
					case req.Method == "GET":
						if writes > 0 {
							checks++
							code = tc.after
						}
					default:
						t.Errorf("unexpected method %s", req.Method)
						code = 400
					}
					if code >= 400 {
						body = `{"detail":"backup request refused"}`
					}
					return contractRT{status: code, body: body}.RoundTrip(req)
				})
				adapter := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
				adapter.Backoff = time.Millisecond
				toolEngine = func(o map[string]any) (*engine.Engine, error) {
					e, err := old(o)
					if err == nil {
						e.Cloud = adapter
					}
					return e, err
				}
				t.Cleanup(func() { toolEngine = old })
				lang := i18n.Lang()
				t.Cleanup(func() { i18n.SetLang(lang) })
				var status, reason string
				if surface == "cli" {
					i18n.SetLang(i18n.RU)
					code, out, errOut := runCLI(t, "", "--json", "--yes", "backup", "delete", parityDiskBackup)
					status, _ = parseObject(out)["status"].(string)
					if code != 0 {
						env, ok := errorEnvelope(errOut)
						if !ok || code != clierrors.ExitCode(clierrors.New(tc.reason, "", "")) || cyrillic.MatchString(env.Message) {
							t.Fatalf("exit=%d: %s", code, errOut)
						}
						reason = env.Reason
					}
				} else {
					i18n.SetLang(i18n.EN)
					servers := newROServers(t)
					ans, _ := servers.stdio(t, servers.plain, "tools/call", map[string]any{"name": "backup_delete", "arguments": map[string]any{"backup_id": parityDiskBackup, "confirmed": true}})
					if ans.rpcErr != "" || ans.isErr != (tc.reason != "") || strings.TrimSpace(ans.text) == "" {
						t.Fatalf("MCP: %+v", ans)
					}
					m := parseObject(ans.text)
					if !ans.isErr {
						status, _ = m["status"].(string)
					}
					reason, _ = m["reason"].(string)
				}
				if writes != tc.writes || status != tc.status || reason != tc.reason || (tc.send == 0 && tc.writes == 1 && checks == 0) {
					t.Fatalf("status=%s reason=%s writes=%d checks=%d; want %s %s writes=%d", status, reason, writes, checks, tc.status, tc.reason, tc.writes)
				}
			})
		}
	}
}
