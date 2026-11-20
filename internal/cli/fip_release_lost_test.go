// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"errors"
	"net/http"
	"slices"
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
func TestFIPReleaseLostReplySurfaces(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		before, deleteCode, after int
		want                      string // terminal status on success, error reason on failure
	}{
		{"lost, gone", 200, 0, 404, "ok"},
		{"absent before consent", 404, 0, 404, "already_deleted"},
		{"foreign ID, project-bound", 200, 0, 404, clierrors.CodeNotFound},
		{"read denied", 403, 0, 404, clierrors.CodeAuthFailed},
		{"write denied", 200, 403, 404, clierrors.CodeAuthFailed},
		{"verification denied", 200, 0, 403, clierrors.CodeAPIUnreachable},
		{"by address, still present", 200, 0, 200, clierrors.CodeAPIUnreachable},
		{"by address, lost", 200, 0, 404, "ok"},
		{"NAT gateway", 200, 0, 200, clierrors.CodeUsage},
		{"bound interface", 200, 0, 200, clierrors.CodeUsage},
		{"wrong card before consent", 200, 0, 200, clierrors.CodeAPIError},
		{"by address, absent", 404, 0, 404, "already_deleted"},
		{"attached", 200, 0, 200, clierrors.CodeUsage},
		{"gateway 404", 200, 0, 404, clierrors.CodeAPIUnreachable},
		{"still present", 200, 0, 200, clierrors.CodeAPIUnreachable},
		{"different address on check", 200, 0, 200, clierrors.CodeAPIUnreachable},
	} {
		occupied := tc.name == "attached" || tc.name == "bound interface"
		surfaces := []string{"CLI", "MCP"}
		if occupied {
			surfaces = append(surfaces, "human-ru", "human-en")
		}
		for _, surface := range surfaces {
			if tc.name == "foreign ID, project-bound" && surface == "CLI" {
				continue
			}
			t.Run(tc.name+"/"+surface, func(t *testing.T) {
				world := withParityWorld(t)
				t.Setenv("CLOUDRU_KEY_ID", "00000000-0000-0000-0000-0000000000a9")
				writes, postReads := 0, 0
				rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
					code := tc.before
					body := `{"id":"` + parityVM + `","ip_address":"192.0.2.4","state":"available"}`
					if tc.name == "bound interface" {
						body = `{"id":"` + parityVM + `","ip_address":"192.0.2.4","state":"available","interface":{"id":"iface"}}`
					}
					if tc.name == "wrong card before consent" {
						body = `{"id":"` + parityStoppedVM + `","state":"available"}`
					}
					if tc.name == "NAT gateway" {
						body = `{"id":"` + parityVM + `","ip_address":"192.0.2.4","state":"available","nat_gateway":{"id":"gw"}}`
					}
					if tc.name == "attached" {
						body = `{"id":"` + parityVM + `","ip_address":"192.0.2.4","state":"in_use"}`
					}
					if req.Method == "GET" && req.URL.Path == "/api/v1/floating-ips" && req.URL.Query().Get("project_id") == pvProject {
						if tc.name == "by address, absent" || tc.name == "foreign ID, project-bound" {
							return contractRT{status: 200, body: `{"items":[],"total":0}`}.RoundTrip(req)
						}
						if writes > 0 {
							t.Fatal("release must verify the resolved ID even when called by address")
						}
						return contractRT{status: 200, body: `{"items":[` + body + `],"total":1}`}.RoundTrip(req)
					}
					if req.URL.Path != "/api/v1/floating-ips/"+parityVM || req.Header.Get("Authorization") == "" {
						t.Fatalf("unexpected request %s %s", req.Method, req.URL)
					}
					if tc.name == "foreign ID, project-bound" {
						t.Fatalf("project check allowed card access: %s", req.URL)
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
						body = `{"detail":"address request refused"}`
					}
					if tc.name == "different address on check" && writes > 0 && req.Method == "GET" {
						body = `{"id":"` + parityStoppedVM + `","state":"deleted"}`
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
				if strings.HasPrefix(surface, "human-") {
					lang := strings.TrimPrefix(surface, "human-")
					t.Setenv("EVO_LANG", lang)
					i18n.SetLang(lang)
					code, _, errOut := runCLI(t, "", "vpc", "fip-release", parityVM)
					if code != 1 {
						t.Fatalf("exit=%d: %s", code, errOut)
					}
					checkOccupiedFIPText(t, lang, errOut)
					got = clierrors.CodeUsage
				} else if surface == "CLI" {
					t.Setenv("EVO_LANG", "ru")
					i18n.SetLang(i18n.RU)
					args := []string{"--json", "--yes", "vpc", "fip-release"}
					if strings.HasPrefix(tc.name, "by address,") {
						args = append(args, "--ip-address", "192.0.2.4")
					} else {
						args = append(args, parityVM)
					}
					code, out, errOut := runCLI(t, "", args...)
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
						if occupied {
							checkOccupiedFIPText(t, "en", env.Message+"\n"+env.Hint)
							checkOccupiedFIPFields(t, env.Details)
						}
						got = env.Reason
					}
				} else {
					i18n.SetLang(i18n.EN)
					servers := newROServers(t)
					if tc.name == "foreign ID, project-bound" {
						servers.plain.SetProjectBound()
					}
					args := map[string]any{"id": parityVM, "confirmed": true}
					if strings.HasPrefix(tc.name, "by address,") {
						delete(args, "id")
						args["ip_address"] = "192.0.2.4"
					}
					ans, _ := servers.stdio(t, servers.plain, "tools/call", map[string]any{"name": "release_floating_ip", "arguments": args})
					if ans.rpcErr != "" {
						t.Fatal(ans.rpcErr)
					}
					m := parseObject(ans.text)
					if occupied {
						msg, _ := m["error"].(string)
						hint, _ := m["hint"].(string)
						checkOccupiedFIPText(t, "en", msg+"\n"+hint)
						checkOccupiedFIPFields(t, m)
					}
					got, _ = m["status"].(string)
					if ans.isErr {
						got, _ = m["reason"].(string)
					}
					if tc.name == "verification denied" && (m["check_reason"] != clierrors.CodeAuthFailed) {
						t.Fatalf("verification failure: %s", ans.text)
					}
				}
				wantWrites := 0
				if tc.before == 200 && (tc.want == "ok" || tc.want == clierrors.CodeAPIUnreachable || tc.deleteCode == 403) {
					wantWrites = 1
				}
				wantChecks := 0
				if wantWrites == 1 && tc.deleteCode == 0 {
					wantChecks = 1
				}
				pending := intent.Pending(intent.Dir(world.dir))
				if tc.want == clierrors.CodeAPIUnreachable {
					if len(pending) != 1 || pending[0].Tool != "release_floating_ip" || pending[0].Target != "id="+parityVM || !pending[0].Sent || !pending[0].Ended || pending[0].Key == nil || pending[0].Key.KeyID != config.MaskKeyID("00000000-0000-0000-0000-0000000000a9") {
						t.Errorf("unresolved address deletion for doctor: %+v", pending)
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

func checkOccupiedFIPText(t *testing.T, lang, text string) {
	t.Helper()
	message, hint := "is in use by a VM", "Detach it from the VM, then release it."
	if lang == "ru" {
		message, hint = "используется виртуальной машиной", "Отвяжите его от ВМ, затем освободите."
	}
	for _, want := range []string{message, hint, `"192.0.2.4"`, `"` + parityVM + `"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in output:\n%s", want, text)
		}
	}
}

func checkOccupiedFIPFields(t *testing.T, fields map[string]any) {
	t.Helper()
	marked, _ := fields["_untrusted"].([]any)
	if fields["id"] != parityVM || fields["ip_address"] != "192.0.2.4" || !slices.Contains(marked, any("id")) || !slices.Contains(marked, any("ip_address")) {
		t.Fatalf("cloud fields: %v", fields)
	}
}
