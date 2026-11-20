// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// The card disappears independently while the DELETE cannot connect.
// doctor must not reconcile this call as one which changed the resource.
func checkDeleteConnectionRefused(t *testing.T, group, command, tool, key, collection, id string) {
	t.Helper()
	surfaces := []string{"human", "JSON", "MCP"}
	if tool == "destroy" {
		surfaces = append(surfaces, "compose")
	}
	for _, surface := range surfaces {
		t.Run(surface, func(t *testing.T) {
			world := withParityWorld(t)
			t.Setenv("EVO_LANG", "ru")
			lang := i18n.Lang()
			t.Cleanup(func() { i18n.SetLang(lang) })
			i18n.SetLang(i18n.RU)
			t.Setenv("CLOUDRU_KEY_ID", "fixture-test-key")
			dials, written := 0, 0
			transport := &http.Transport{DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				dials++
				return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
			}}
			t.Cleanup(transport.CloseIdleConnections)
			card := fmt.Sprintf(`{"id":%q,"state":"available","name":"fixture","vms":[],"ip_address":"192.0.2.4"}`, id)
			if surface == "compose" {
				sf := &state.File{Deployments: []state.DeploymentRecord{{ID: id, VMID: id, ProjectID: pvProject, Status: "running"}}}
				if err := state.Write(state.DefaultPath(world.dir), sf); err != nil {
					t.Fatal(err)
				}
			}
			path := "/api/v1/" + collection
			rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == path && r.Method == "GET" {
					return contractRT{status: 200, body: `{"items":[` + card + `],"total":1}`}.RoundTrip(r)
				}
				if tool == "destroy" && r.Method == "GET" && r.URL.Path != path+"/"+id {
					return contractRT{status: 200, body: `{"items":[],"total":0}`}.RoundTrip(r)
				}
				if r.URL.Path != path+"/"+id {
					t.Fatalf("unexpected %s %s", r.Method, r.URL)
				}
				switch r.Method {
				case "DELETE":
					trace := &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { written++ }}
					return transport.RoundTrip(r.WithContext(httptrace.WithClientTrace(r.Context(), trace)))
				case "GET":
					if dials != 0 {
						return contractRT{status: 404, body: `{"detail":"missing"}`}.RoundTrip(r)
					}
					return contractRT{status: 200, body: card}.RoundTrip(r)
				default:
					t.Fatalf("unexpected method %s", r.Method)
					return nil, nil
				}
			})
			old := toolEngine
			api := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
			api.Backoff = time.Millisecond
			toolEngine = func(o map[string]any) (*engine.Engine, error) {
				e, err := old(o)
				if err == nil {
					e.Cloud = api
				}
				return e, err
			}
			t.Cleanup(func() { toolEngine = old })
			if surface == "MCP" {
				s := newROServers(t)
				ans, _ := s.stdio(t, s.plain, "tools/call", map[string]any{"name": tool, "arguments": map[string]any{key: id, "confirmed": true}})
				if !ans.isErr || parseObject(ans.text)["reason"] != "API_UNREACHABLE" {
					t.Errorf("unexpected MCP result: %s", ans.text)
				}
			} else {
				args := []string{"--yes", group, command, id}
				if surface == "compose" {
					args = []string{"--yes", "--json", "destroy"}
				}
				if surface == "JSON" {
					args = append(args, "--json")
				}
				code, out, stderr := runCLI(t, "", args...)
				if code == 0 || stderr == "" {
					t.Errorf("exit=%d stdout=%s stderr=%s", code, out, stderr)
				}
				if surface == "human" && !strings.Contains(stderr, "Не удалось установить соединение") && !strings.Contains(stderr, "The connection failed before") {
					t.Errorf("missing cause: %s", stderr)
				}
				if surface == "JSON" {
					env, ok := errorEnvelope(stderr)
					if !ok || env.Reason != "API_UNREACHABLE" {
						t.Errorf("unexpected JSON error: %s", stderr)
					}
				}
			}
			if pending := intent.Pending(intent.Dir(world.dir)); len(pending) != 0 {
				t.Errorf("unexecuted delete retained: %+v", pending)
			}
			local := httptest.NewServer(http.NotFoundHandler())
			defer local.Close()
			previousBase := httpretry.DefaultBase
			httpretry.DefaultBase = roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host == local.Listener.Addr().String() {
					return local.Client().Transport.RoundTrip(req)
				}
				return previousBase.RoundTrip(req)
			})
			t.Cleanup(func() { httpretry.DefaultBase = previousBase })
			oldIAM, oldCompute := auth.IAMEndpoint, tools.ComputeBaseURL
			auth.IAMEndpoint, tools.ComputeBaseURL = local.URL+"/token", local.URL
			t.Cleanup(func() { auth.IAMEndpoint, tools.ComputeBaseURL = oldIAM, oldCompute })
			s := newROServers(t)
			report, _ := s.stdio(t, s.plain, "tools/call", map[string]any{"name": "doctor", "arguments": map[string]any{}})
			if report.rpcErr != "" || parseObject(report.text)["checks"] == nil || strings.Contains(report.text, id) {
				t.Errorf("doctor after unsent request: %s", report.text)
			}
			if dials != 1 || written != 0 {
				t.Fatalf("got %d dials, %d HTTP writes", dials, written)
			}
		})
	}
}
