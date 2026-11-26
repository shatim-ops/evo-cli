// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

type destroyWireAnswer struct {
	failed bool
	body   map[string]any
	text   string
}

func destroyOver(t *testing.T, surface string, servers parityMCP) destroyWireAnswer {
	t.Helper()
	var a destroyWireAnswer
	if strings.HasPrefix(surface, "CLI") {
		args := []string{"destroy", "--yes"}
		if surface == "CLI targeted" {
			args = []string{"vm", "delete", parityVM, "--yes"}
		}
		if surface != "CLI text" {
			args = append(args, "--json")
		}
		exit, out, stderr := runCLI(t, "", args...)
		a.failed = exit != 0
		a.text = out + stderr
		if exit != 0 && exit != 5 {
			t.Fatalf("unexpected destroy exit %d: %s", exit, a.text)
		}
		if surface == "CLI text" {
			return a
		}
		if a.failed {
			e, ok := errorEnvelope(stderr)
			if !ok || e.Code != 5 {
				t.Fatalf("missing API error: %s", a.text)
			}
			a.body = e.Details
			if a.body == nil {
				a.body = map[string]any{}
			}
			a.body["hint"] = e.Hint
			return a
		}
		if err := json.Unmarshal([]byte(out), &a.body); err != nil {
			t.Fatalf("invalid CLI JSON: %v: %s", err, out)
		}
		return a
	}
	args := map[string]any{"name": "destroy", "arguments": map[string]any{"vm_id": parityVM, "confirmed": true}}
	wires := roServers{roHTTP: servers.http}
	var answer roAnswer
	if surface == "stdio" {
		answer, _ = wires.stdio(t, servers.stdio, "tools/call", args)
	} else {
		answer, _ = wires.http(t, "tools/call", args)
	}
	a.failed, a.text = answer.isErr, answer.text
	if answer.rpcErr != "" || json.Unmarshal([]byte(a.text), &a.body) != nil {
		t.Fatalf("invalid MCP answer: %+v", answer)
	}
	return a
}

// The resource state is independent of the response: accepting DELETE does
// not prove the VM or its open rule disappeared. An unrestricted MCP client
// can retry its own tool; evidence about an old IP request is human work.
func TestDestroyContinuationAcrossClients(t *testing.T) {
	for _, surface := range []string{"CLI text", "CLI JSON", "CLI targeted", "stdio", "HTTP"} {
		for _, scenario := range []string{"vm_still_visible", "rule_still_visible", "receipt_needs_person", "complete"} {
			t.Run(surface+"/"+scenario, func(t *testing.T) {
				w, _ := withJSONWorld(t)
				lang := i18n.Lang()
				i18n.SetLang(i18n.EN)
				t.Cleanup(func() { i18n.SetLang(lang) })
				_, done := recordDeployment(t, w)
				t.Cleanup(done)
				path := state.DefaultPath(w.dir)
				sf, err := state.Read(path)
				if err != nil {
					t.Fatal(err)
				}
				rec := sf.ActiveDeployment()
				rec.VMIP, rec.FloatingIPID = "", ""
				probe := &ruleReceiptHTTP{t: t, phase: "deleting"}
				adapter := provision.NewCloudRuAdapter(&http.Client{Transport: httpretry.UserAgent(probe)}, "https://compute.fixture.invalid")
				adapter.Backoff = time.Millisecond
				base := toolEngine
				toolEngine = func(opts map[string]any) (*engine.Engine, error) {
					e, err := base(opts)
					if err == nil {
						e.Timings.DestroyPollTimeout = 40 * time.Millisecond
						if scenario == "rule_still_visible" {
							e.Cloud = ruleReceiptCloud{e.Cloud, adapter}
						}
					}
					return e, err
				}
				t.Cleanup(func() { toolEngine = base })
				eng, err := toolEngine(nil)
				if err != nil {
					t.Fatal(err)
				}
				rec.CreatedBy = eng.Maker()
				if scenario == "rule_still_visible" {
					rec.OpenedRules = []state.SGRule{{ID: "owned-rule", SecurityGroupID: "owned-group", Port: "8081/tcp"}}
				}
				if scenario == "receipt_needs_person" {
					sf.LostFloatingIPs = []state.LostFloatingIP{{Name: "pending-address", VMID: rec.VMID, ProjectID: rec.ProjectID}}
				}
				if err := state.Write(path, sf); err != nil {
					t.Fatal(err)
				}
				holdVM := scenario == "vm_still_visible"
				old := w.rt.extra
				readsAfterDelete, deletes := 0, 0
				w.rt.extra = func(r *http.Request) (*http.Response, error) {
					if r.URL.Path == "/api/v1/vms/"+parityVM {
						if r.Method == "DELETE" {
							deletes++
							if holdVM {
								return contractRT{202, "{}"}.RoundTrip(r)
							}
						}
						if r.Method == "GET" && deletes > 0 && holdVM {
							readsAfterDelete++
							return contractRT{200, `{"id":"` + parityVM + `","name":"evo-web","state":"deleting"}`}.RoundTrip(r)
						}
					}
					return old(r)
				}
				servers := parityServers(t)
				a := destroyOver(t, surface, servers)
				wantFailed := scenario == "vm_still_visible"
				if a.failed != wantFailed {
					t.Fatalf("failed=%t, want %t: %s", a.failed, wantFailed, a.text)
				}
				pending := scenario != "complete"
				sf, err = state.Read(path)
				if err != nil {
					t.Fatal(err)
				}
				if deletes != 1 || (wantFailed && readsAfterDelete == 0) {
					t.Fatalf("fixture: VM DELETE=%d, later GET=%d", deletes, readsAfterDelete)
				}
				if (sf.FindDeployment(rec.ID) != nil) != pending {
					t.Fatalf("pending=%t did not match the local receipt: %s", pending, a.text)
				}
				if surface == "CLI text" {
					if pending {
						command := "evo destroy"
						if scenario == "receipt_needs_person" {
							command = "evo cleanup receipts"
						}
						if !strings.Contains(a.text, command) {
							t.Errorf("missing runnable continuation %q: %s", command, a.text)
						}
					}
				} else {
					id := a.body["vm_id"]
					if id == nil {
						id = a.body["deleted_vm"]
					}
					if id != parityVM {
						t.Errorf("missing or incorrect known VM ID: %v", a.body)
					}
					if wantFailed && !strings.HasPrefix(surface, "CLI") && (a.body["status"] != "partial" || a.body["reason"] != "API_ERROR") {
						t.Errorf("uncertain deletion lost its error/partial outcome: %v", a.body)
					}
					if pending && a.body["cleanup_pending"] != true {
						t.Errorf("unsettled cleanup lacks cleanup_pending=true: %v", a.body)
					}
					if surface == "CLI JSON" && !wantFailed {
						wantStatus := "destroyed"
						if pending {
							wantStatus = "partial"
						}
						if a.body["status"] != wantStatus || a.body["cleanup_pending"] != pending {
							t.Errorf("directory CLI contract: %v", a.body)
						}
					}
				}
				if !pending {
					return
				}
				if surface == "stdio" || surface == "HTTP" {
					hint := fmt.Sprint(a.body["hint"], " ", a.body["next_step"], " ", a.body["next_call"], " ", a.body["next_tool"], " ", a.body["next_args"])
					var names []string
					wires := roServers{roHTTP: servers.http}
					if surface == "stdio" {
						_, names = wires.stdio(t, servers.stdio, "tools/list", map[string]any{})
					} else {
						_, names = wires.http(t, "tools/list", map[string]any{})
					}
					if !slices.Contains(names, "destroy") {
						t.Fatal("the recommended retry tool is absent from this server")
					}
					if scenario == "receipt_needs_person" {
						h := strings.ToLower(hint)
						// The actor must be identified for the local receipts command
						// itself. A later request for evidence does not authorize this
						// client to run shell commands on someone else's machine.
						at := strings.Index(h, "evo cleanup receipts")
						pre := ""
						if at >= 0 {
							pre = h[:at]
						}
						if at < 0 || !(strings.Contains(pre, "person") || strings.Contains(pre, "operator") || strings.Contains(pre, "human") || strings.Contains(pre, "owner") || strings.Contains(pre, "user")) {
							t.Errorf("local verification not handed to person: %s", hint)
						}
					} else if !(strings.Contains(hint, "destroy") && strings.Contains(hint, "vm_id") && strings.Contains(hint, parityVM)) {
						t.Errorf("MCP continuation lacks callable destroy and its actual vm_id: %s", hint)
					}
				}
				if scenario == "receipt_needs_person" {
					exit, listing, stderr := runCLI(t, "", "cleanup", "receipts", "--json", "--deployment-id", rec.ID)
					if exit != 0 || !strings.Contains(listing, "pending-address") {
						t.Fatalf("human receipt lookup: %d %s%s", exit, listing, stderr)
					}
					return
				}
				// Verify the recommended same-target operation actually settles the
				// real receipt when the provider subsequently confirms completion.
				holdVM = false
				probe.phase = "rule absent"
				retried := destroyOver(t, surface, servers)
				sf, err = state.Read(path)
				if retried.failed || err != nil || sf.FindDeployment(rec.ID) != nil {
					t.Fatalf("retry did not settle the same deployment: %v %s", err, retried.text)
				}
			})
		}
	}
}
