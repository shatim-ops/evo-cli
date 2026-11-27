// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// D83 (owner's decision 05.11, #941): destroy without a target in a folder
// with several records — provision's VM next to deploy's — refuses before
// the question with the code it gave after it, CONFIG_INVALID (2), with
// and without consent, on every surface; nothing is deleted. Without
// --yes, evo tools call destroy gave 3 (the question) — the breaking part,
// «Миграция» 1.8.
func TestRun_DestroySeveralRecordsIsConfigInvalidBeforeTheQuestion(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("CLOUDRU_KEY_ID", "")
	t.Setenv("CLOUDRU_SECRET", "")
	t.Setenv("CLOUDRU_API_KEY", "")
	oldTTY := confirm.StdinIsTTY
	confirm.StdinIsTTY = func() bool { return false }
	t.Cleanup(func() { confirm.StdinIsTTY = oldTTY })

	now := time.Now()
	sf := &state.File{Version: 1}
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-a", VMID: "vm-a", Status: "running", Source: state.SourceProvision, CreatedAt: now})
	sf.UpsertDeployment(state.DeploymentRecord{ID: "vm-b", VMID: "vm-b", Status: "running", ComposePath: "/srv/app/docker-compose.yml", CreatedAt: now})
	if err := state.Write(state.DefaultPath(dir), sf); err != nil {
		t.Fatal(err)
	}
	want := clerrors.ExitCode(clerrors.New(clerrors.CodeConfigInvalid, "m", ""))
	for _, args := range [][]string{
		{"destroy"},
		{"destroy", "--yes"},
		{"tools", "call", "destroy"},
		{"tools", "call", "destroy", "--yes"},
	} {
		if got := run(args); got != want {
			t.Errorf("run(%v) = %d, want %d (CONFIG_INVALID, before the question)", args, got, want)
		}
	}
	after, _ := state.Read(state.DefaultPath(dir))
	if n := len(after.ActiveDeployments()); n != 2 {
		t.Fatalf("records changed: %d active", n)
	}
}

// The VM and its IP are still visible after DELETE. Distinguish a refusal from
// an accepted but unconfirmed mutation, in the actual CLI and MCP envelopes.
func TestDestroy_KeepsKnownTargetOnFailure(t *testing.T) {
	for _, surface := range []string{"CLI", "targeted CLI", "stdio", "HTTP", "deploy toolset"} {
		for _, refusal := range []bool{false, true} {
			want := "partial"
			if refusal {
				want = "refused"
			}
			t.Run(surface+"/"+want, func(t *testing.T) {
				w, _ := withJSONWorld(t)
				_, done := recordDeployment(t, w)
				t.Cleanup(done)
				original := w.rt.extra
				w.rt.extra = func(r *http.Request) (*http.Response, error) {
					if r.Method == http.MethodDelete {
						if r.URL.Path != "/api/v1/vms/"+parityVM {
							t.Fatalf("cleanup before proving VM deletion: %s", r.URL)
						}
						if refusal {
							return contractRT{status: 409, body: `{"code":"vm_can_not_be_deleted","message":"current state"}`}.RoundTrip(r)
						}
						return contractRT{status: http.StatusAccepted, body: `{}`}.RoundTrip(r)
					}
					return original(r)
				}
				base := toolEngine
				toolEngine = func(args map[string]any) (*engine.Engine, error) {
					e, err := base(args)
					if err == nil {
						tm := e.Timings
						tm.DestroyPollTimeout, tm.DestroyPollInterval = 20*time.Millisecond, time.Millisecond
						tm.DeletableWaitTimeout = 10 * time.Millisecond
						e.Timings = tm
					}
					return e, err
				}
				t.Cleanup(func() { toolEngine = base })
				var fields map[string]any
				var hint string
				if surface == "CLI" || surface == "targeted CLI" {
					args := []string{"destroy", "--json", "--yes"}
					if surface == "targeted CLI" {
						args = []string{"vm", "delete", parityVM, "--release-ip=false", "--json", "--yes"}
					}
					code, out, stderr := runCLI(t, "", args...)
					response, ok := errorEnvelope(stderr)
					if !ok || code != 5 || response.Reason != "API_ERROR" {
						t.Fatalf("expected API_ERROR/5: exit %d, %s%s", code, out, stderr)
					}
					fields, hint = response.Details, response.Hint
					if !strings.Contains(hint, "evo ") {
						t.Fatalf("no CLI recovery in %q", hint)
					}
					if surface == "targeted CLI" && (!strings.Contains(hint, parityVM) || !strings.Contains(hint, "--release-ip=false")) {
						t.Fatalf("terminal retry lost its target or IP choice: %q", hint)
					}
				} else {
					agent := mcp.New(toolEngine, "fixture")
					declareStateDir(agent)
					if surface == "deploy toolset" {
						if err := agent.SetToolsets([]string{"deploy"}); err != nil {
							t.Fatal(err)
						}
					}
					var answer roAnswer
					var servers roServers
					args := map[string]any{"vm_id": parityVM, "confirmed": true, "release_ip": false}
					params := map[string]any{"name": "destroy", "arguments": args}
					if surface == "HTTP" {
						h := mcp.NewHTTPHandler(agent, mcp.HTTPOptions{BearerToken: "parity-token"})
						defer h.Close()
						servers.roHTTP = httptest.NewServer(h)
						defer servers.roHTTP.Close()
						answer, _ = servers.http(t, "tools/call", params)
					} else {
						answer, _ = servers.stdio(t, agent, "tools/call", params)
					}
					if !answer.isErr || json.Unmarshal([]byte(answer.text), &fields) != nil || fields["reason"] != "API_ERROR" {
						t.Fatalf("expected tool error: %+v", answer)
					}
					hint, _ = fields["hint"].(string)
					if surface == "deploy toolset" && strings.Contains(hint, "vm_get") {
						t.Fatalf("the deploy-only server recommended an unavailable tool: %q", hint)
					}
					if !strings.Contains(hint, `vm_id="`+parityVM+`"`) || !strings.Contains(hint, "release_ip=false") || strings.Contains(hint, "evo ") {
						t.Fatalf("agent cannot follow the retry: %q", hint)
					}
				}
				if fields["vm_id"] != parityVM || fields["floating_ip_id"] != parityRecordedFIP || fields["cleanup_pending"] != true {
					t.Fatalf("missing known cleanup state: %v", fields)
				}
				if (fields["status"] == "partial") == refusal {
					t.Fatalf("incorrect outcome after DELETE refusal=%t: %v", refusal, fields)
				}
				sf, err := state.Read(state.DefaultPath(w.dir))
				if err != nil || sf.ActiveDeployment() == nil || sf.ActiveDeployment().FloatingIPID != parityRecordedFIP {
					t.Fatal("lost receipt", sf, err)
				}
			})
		}
	}
}
