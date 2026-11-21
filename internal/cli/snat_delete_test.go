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
func TestSNATDeleteLostReplySurfaces(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		before, deleteCode, after int
		want                      string // terminal status on success, error reason on failure
	}{
		{"foreign project", 200, 0, 200, clierrors.CodeAPIError},
		{"foreign project preview", 200, 0, 200, clierrors.CodeAPIError},
		{"foreign flat owner", 200, 0, 200, clierrors.CodeAPIError},
		{"project 404 before consent", 404, 0, 200, clierrors.CodeNotFound},
		{"acknowledged", 200, 204, 200, "deleted"},
		{"lost, gone", 200, 0, 200, "deleted"},
		{"absent before consent", 200, 0, 200, "already_deleted"},
		{"read denied", 403, 0, 200, clierrors.CodeAuthFailed},
		{"write denied", 200, 403, 200, clierrors.CodeAuthFailed},
		{"verification denied", 200, 0, 403, clierrors.CodeAPIUnreachable},
		{"project invisible", 200, 0, 404, clierrors.CodeAPIUnreachable},
		{"null page", 200, 0, 200, clierrors.CodeAPIUnreachable},
		{"still present", 200, 0, 200, clierrors.CodeAPIUnreachable},
	} {
		surfaces := []string{"CLI", "MCP"}
		if tc.name == "lost, gone" {
			surfaces = append(surfaces, "ru", "en")
		}
		for _, surface := range surfaces {
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				world := withParityWorld(t)
				t.Setenv("CLOUDRU_KEY_ID", "00000000-0000-0000-0000-0000000000a9")
				writes, postReads, preReads := 0, 0, 0
				rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					code := tc.before
					body := `{"items":[{"id":"` + parityVM + `","name":"gateway"}],"total":1}`
					if req.Header.Get("Authorization") == "" || req.URL.Query().Get("project_id") != pvProject {
						t.Fatalf("unscoped request %s %s", req.Method, req.URL)
					}
					switch {
					case req.Method == "DELETE" && req.URL.Path == "/api/v1/nat-gateways/"+parityVM:
						writes++
						code = tc.deleteCode
						if code == 0 {
							return nil, errors.New("read tcp: connection reset by peer")
						}
					case req.Method == "GET" && req.URL.Path == "/api/v1/nat-gateways":
						if writes > 0 {
							postReads++
							code = tc.after
						} else {
							preReads++
						}
						switch {
						case strings.HasPrefix(tc.name, "foreign project"):
							body = `{"items":[{"id":"` + parityVM + `","project":{"id":"foreign"}}]}`
						case tc.name == "foreign flat owner":
							body = `{"items":[{"id":"` + parityVM + `","project_id":"foreign"}]}`
						case tc.name == "absent before consent" || (writes > 0 && tc.name == "lost, gone"):
							body = `{"items":[],"total":0}`
						case writes > 0 && tc.name == "null page":
							body = "null"
						}
					default:
						t.Fatalf("unexpected request %s %s", req.Method, req.URL)
					}
					if code >= 400 {
						body = `{"detail":"refused"}`
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
				if surface == "ru" || surface == "en" {
					t.Setenv("EVO_LANG", surface)
					i18n.SetLang(surface)
					code, out, stderr := runCLI(t, "", "--yes", "snat", "delete", parityVM)
					needle := "not acknowledge"
					if surface == "ru" {
						needle = "Ответ сервиса"
					}
					if code != 0 || !strings.Contains(out+stderr, needle) {
						t.Fatalf("exit=%d stdout=%s stderr=%s", code, out, stderr)
					}
					got = "deleted"
				} else if surface == "CLI" {
					i18n.SetLang(i18n.RU)
					code, out, errOut := runCLI(t, "", "--json", "--yes", "snat", "delete", parityVM)
					if code == 0 {
						result := parseObject(out)
						got, _ = result["status"].(string)
						if tc.name == "lost, gone" && !strings.Contains(out, "not acknowledge") {
							t.Errorf("missing observation: %s", out)
						}
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
					if strings.HasPrefix(tc.name, "foreign") {
						servers.plain.SetProjectBound()
					}
					ans, _ := servers.stdio(t, servers.plain, "tools/call", map[string]any{"name": "snat_delete", "arguments": map[string]any{"id": parityVM, "confirmed": tc.name != "foreign project preview"}})
					if ans.rpcErr != "" {
						t.Fatal(ans.rpcErr)
					}
					m := parseObject(ans.text)
					got, _ = m["status"].(string)
					if tc.name == "lost, gone" && !strings.Contains(ans.text, "not acknowledge") {
						t.Errorf("missing observation: %s", ans.text)
					}
					if ans.isErr {
						got, _ = m["reason"].(string)
					}
					if tc.name == "verification denied" && (m["check_reason"] != clierrors.CodeAuthFailed) {
						t.Fatalf("verification failure: %s", ans.text)
					}
				}
				wantWrites := 0
				if !strings.HasPrefix(tc.name, "foreign") && tc.before == 200 && tc.name != "absent before consent" {
					wantWrites = 1
				}
				wantChecks := 0
				if wantWrites == 1 && tc.deleteCode == 0 {
					wantChecks = 1
				}
				pending := intent.Pending(intent.Dir(world.dir))
				if tc.want == clierrors.CodeAPIUnreachable {
					if len(pending) != 1 || pending[0].Tool != "snat_delete" || pending[0].Target != "id="+parityVM || !pending[0].Sent || !pending[0].Ended || pending[0].Key == nil || pending[0].Key.KeyID != config.MaskKeyID("00000000-0000-0000-0000-0000000000a9") {
						t.Errorf("unresolved gateway deletion for doctor: %+v", pending)
					}
				} else if len(pending) != 0 {
					t.Errorf("known outcome retained: %+v", pending)
				}
				wantPreReads := 1
				if surface != "MCP" && wantWrites == 1 {
					wantPreReads = 3
				}
				if preReads != wantPreReads {
					t.Errorf("initial list reads=%d, want %d", preReads, wantPreReads)
				}
				if got != tc.want || writes != wantWrites || postReads != wantChecks {
					t.Fatalf("got=%s DELETE=%d verification GET=%d; want %s %d %d", got, writes, postReads, tc.want, wantWrites, wantChecks)
				}
			})
		}
	}
}

func TestSNATDeleteConnectionRefused(t *testing.T) {
	checkDeleteConnectionRefused(t, "snat", "delete", "snat_delete", "id", "nat-gateways", parityVM)
}
