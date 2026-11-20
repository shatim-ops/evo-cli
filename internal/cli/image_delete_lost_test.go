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

// A lost reply is distinct from a refusal of the write and from absence on
// the first read. Each surface sees a fresh copy of the same HTTP world.
func TestImageDeleteLostReplySurfaces(t *testing.T) {
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
		{"still present", 200, 0, 200, clierrors.CodeAPIUnreachable},
	} {
		for _, surface := range []string{"CLI", "MCP"} {
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				withParityWorld(t)
				writes, postReads := 0, 0
				rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					code := tc.before
					body := `{"id":"` + parityVM + `","name":"test image"}`
					if req.Method == "GET" && req.URL.Path == "/api/v1/images" && req.URL.Query().Get("project_id") == pvProject {
						return contractRT{status: 200, body: `{"items":[` + body + `],"total":1}`}.RoundTrip(req)
					}
					if req.URL.Path != "/api/v1/images/"+parityVM || req.Header.Get("Authorization") == "" {
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
						body = `{"detail":"image request refused"}`
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
					code, out, errOut := runCLI(t, "", "--json", "--yes", "image", "delete", parityVM)
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
					ans, _ := servers.stdio(t, servers.plain, "tools/call", map[string]any{"name": "image_delete", "arguments": map[string]any{"image_id": parityVM, "confirmed": true}})
					if ans.rpcErr != "" {
						t.Fatal(ans.rpcErr)
					}
					m := parseObject(ans.text)
					got, _ = m["status"].(string)
					if ans.isErr {
						got, _ = m["reason"].(string)
					}
					if tc.name == "verification denied" && (m["check_reason"] != clierrors.CodeAuthFailed || m["next_tool"] != "image_get") {
						t.Fatalf("verification failure: %s", ans.text)
					}
				}
				wantWrites := 0
				if tc.before == 200 {
					wantWrites = 1
				}
				wantChecks := 0
				if wantWrites == 1 && tc.deleteCode == 0 {
					wantChecks = 1
				}
				if got != tc.want || writes != wantWrites || postReads != wantChecks {
					t.Fatalf("got=%s DELETE=%d verification GET=%d; want %s %d %d", got, writes, postReads, tc.want, wantWrites, wantChecks)
				}
			})
		}
	}
}
