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

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// A lost reply is distinct from a refusal of the write and from absence on
// the first read. Each surface sees a fresh copy of the same HTTP world.
func TestDiskDeleteLostReplySurfaces(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		before, deleteCode, after int
		want                      string // terminal status on success, error reason on failure
	}{
		{"lost, gone", 200, 0, 404, "deleted"},
		{"absent before consent", 404, 0, 404, "already_deleted"},
		{"read denied", 403, 0, 404, clierrors.CodeAuthFailed},
		{"write denied", 200, 403, 404, clierrors.CodeAuthFailed},
		{"verification denied", 200, 0, 403, clierrors.CodeAPIUnreachable},
		{"attached", 200, 0, 200, clierrors.CodeUsage},
		{"gateway 404", 200, 0, 404, clierrors.CodeAPIUnreachable},
		{"still present", 200, 0, 200, clierrors.CodeAPIUnreachable},
	} {
		for _, surface := range []string{"CLI", "MCP"} {
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				world := withParityWorld(t)
				t.Setenv("CLOUDRU_KEY_ID", "00000000-0000-0000-0000-0000000000a9")
				writes, postReads := 0, 0
				rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					code := tc.before
					body := `{"id":"` + parityVM + `","name":"test disk","vms":[]}`
					if tc.name == "attached" {
						body = `{"id":"` + parityVM + `","vms":[{"id":"` + parityStoppedVM + `","name":"attached-vm","primary":true}]}`
					}
					if req.Method == "GET" && req.URL.Path == "/api/v1/disks" && req.URL.Query().Get("project_id") == pvProject {
						return contractRT{status: 200, body: `{"items":[` + body + `],"total":1}`}.RoundTrip(req)
					}
					if req.URL.Path != "/api/v1/disks/"+parityVM || req.Header.Get("Authorization") == "" {
						t.Fatalf("unexpected request %s %s", req.Method, req.URL)
					}
					switch req.Method {
					case "DELETE":
						if req.URL.Query().Get("project_id") != pvProject {
							t.Errorf("delete query: %s", req.URL)
						}
						writes++
						code = tc.deleteCode
						if code == 0 && writes == 1 {
							return nil, errors.New("read tcp: connection reset by peer")
						}
						if code == 0 {
							code = 404
						}
					case "GET":
						if writes > 0 {
							postReads++
							code = tc.after
						}
					default:
						t.Fatalf("unexpected method %s", req.Method)
					}
					if code >= 400 {
						body = `{"detail":"disk request refused"}`
					}
					if tc.name == "gateway 404" && writes > 0 && req.Method == "GET" {
						body = "404 page not found"
					}
					return contractRT{status: code, body: body}.RoundTrip(req)
				})
				a := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
				a.Backoff = time.Millisecond
				factory := toolEngine
				toolEngine = func(o map[string]any) (*engine.Engine, error) {
					e, err := factory(o)
					if err == nil {
						e.Cloud = a
					}
					return e, err
				}
				lang := i18n.Lang()
				t.Cleanup(func() { toolEngine = factory; i18n.SetLang(lang) })
				var got string
				if surface == "CLI" {
					i18n.SetLang(i18n.RU)
					code, out, errOut := runCLI(t, "", "--json", "--yes", "disk", "delete", parityVM)
					if code == 0 {
						got, _ = parseObject(out)["status"].(string)
					} else {
						env, ok := errorEnvelope(errOut)
						if !ok || code != clierrors.ExitCode(clierrors.New(tc.want, "", "")) || cyrillic.MatchString(env.Message) {
							t.Fatalf("exit=%d: %s", code, errOut)
						}
						if tc.name == "verification denied" && !strings.Contains(env.Message, clierrors.CodeAuthFailed) {
							t.Fatalf("verification failure: %s", errOut)
						}
						got = env.Reason
					}
				} else {
					i18n.SetLang(i18n.EN)
					servers := newROServers(t)
					ans, _ := servers.stdio(t, servers.plain, "tools/call", map[string]any{"name": "disk_delete", "arguments": map[string]any{"disk_id": parityVM, "confirmed": true}})
					if ans.rpcErr != "" {
						t.Fatal(ans.rpcErr)
					}
					m := parseObject(ans.text)
					got, _ = m["status"].(string)
					if ans.isErr {
						got, _ = m["reason"].(string)
					}
					if tc.name == "verification denied" && (m["check_reason"] != clierrors.CodeAuthFailed) {
						t.Fatalf("verification failure: %s", ans.text)
					}
				}
				wantWrites := 0
				if tc.before == 200 && tc.name != "attached" {
					wantWrites = 1
				}
				wantChecks := 0
				if wantWrites == 1 && tc.deleteCode == 0 {
					wantChecks = 1
				}
				pending := intent.Pending(intent.Dir(world.dir))
				if tc.want == clierrors.CodeAPIUnreachable {
					if len(pending) != 1 || pending[0].Tool != "disk_delete" || pending[0].Target != "disk_id="+parityVM || !pending[0].Sent || !pending[0].Ended || pending[0].Key == nil || pending[0].Key.KeyID != config.MaskKeyID("00000000-0000-0000-0000-0000000000a9") {
						t.Errorf("unresolved disk deletion for doctor: %+v", pending)
					}
				} else if len(pending) != 0 {
					t.Errorf("known outcome retained: %+v", pending)
				}
				if got != tc.want || writes != wantWrites || postReads != wantChecks {
					t.Fatalf("got=%s DELETE=%d verification GET=%d; want %s %d %d", got, writes, postReads, tc.want, wantWrites, wantChecks)
				}
			})
		}
	}
}
