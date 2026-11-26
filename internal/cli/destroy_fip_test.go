// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/httpretry"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// The normal parity world returns an empty project FIP list. Here an
// extra address can still be listed after an accepted cleanup request.
type destroyExtraAddressWorld struct {
	provision.CloudAPI
	id string
}

func (w destroyExtraAddressWorld) ListFloatingIPs(context.Context, string) ([]*provision.FloatingIP, error) {
	return []*provision.FloatingIP{{ID: w.id, Name: "extra", State: "available"}}, nil
}

// Exercise both D127 answers through the CLI with a real saved deployment.
// An accepted DELETE is followed first by a still-visible paid address,
// then on the next destroy by a GET confirming its disappearance.
func TestDestroyJSON_OwnAddressConfirmedOnRetry(t *testing.T) {
	testDestroyPaidAddressOnRetry(t, false)
}

func TestDestroyJSON_ExtraAddressConfirmedOnRetry(t *testing.T) {
	testDestroyPaidAddressOnRetry(t, true)
}

func testDestroyPaidAddressOnRetry(t *testing.T, extra bool) {
	t.Helper()
	w, _ := withJSONWorld(t)
	_, done := paritySetup["deploy"](t, w)
	defer done()
	if code, out, errOut := runCLI(t, "", "--json", "deploy", "--yes"); code != 0 {
		t.Fatalf("deploy: exit %d, %s%s", code, out, errOut)
	}
	addressID := parityVM
	if extra {
		addressID = "00000000-0000-4000-8000-000000000008"
		base := toolEngine
		toolEngine = func(o map[string]any) (*engine.Engine, error) {
			e, err := base(o)
			if err == nil {
				e.Cloud = destroyExtraAddressWorld{e.Cloud, addressID}
			}
			return e, err
		}
		defer func() { toolEngine = base }()
		path := state.DefaultPath(w.dir)
		sf, err := state.Read(path)
		if err != nil {
			t.Fatal(err)
		}
		rec := sf.ActiveDeployment()
		sf.LostFloatingIPs = []state.LostFloatingIP{{ID: addressID, Name: "extra", VMID: rec.VMID, ProjectID: rec.ProjectID}}
		if err := state.Write(path, sf); err != nil {
			t.Fatal(err)
		}
	}
	previous := w.rt.extra
	var deletes, reads int
	gone := false
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/floating-ips/"+addressID {
			switch r.Method {
			case http.MethodDelete:
				deletes++
				return contractRT{status: http.StatusAccepted, body: `{}`}.RoundTrip(r)
			case http.MethodGet:
				if deletes > 0 {
					reads++
					if gone {
						return contractRT{status: http.StatusNotFound, body: `{"message":"gone"}`}.RoundTrip(r)
					}
					return contractRT{status: http.StatusOK, body: `{"id":"` + addressID + `","state":"deleting"}`}.RoundTrip(r)
				}
				return contractRT{status: http.StatusOK, body: `{"id":"` + addressID + `","state":"available"}`}.RoundTrip(r)
			}
		}
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/security-groups/") && strings.Contains(r.URL.Path, "/rules/") {
			return contractRT{status: http.StatusOK, body: `{}`}.RoundTrip(r)
		}
		return previous(r)
	}
	for _, want := range []string{"partial", "destroyed"} {
		gone = want == "destroyed"
		before := reads
		code, out, errOut := runCLI(t, "", "--json", "destroy", "--yes")
		var result struct {
			Status      string  `json:"status"`
			Pending     *bool   `json:"cleanup_pending"`
			Hint        *string `json:"hint"`
			CleanupHint *string `json:"cleanup_hint"`
		}
		if code != 0 || json.Unmarshal([]byte(out), &result) != nil || result.Status != want ||
			result.Pending == nil || *result.Pending == gone || reads == before {
			t.Fatalf("%s: exit %d, reads %d, %s%s", want, code, reads-before, out, errOut)
		}
		if result.Hint != nil || (!gone && (result.CleanupHint == nil || !strings.Contains(*result.CleanupHint, "evo cleanup receipts"))) ||
			(gone && result.CleanupHint != nil) {
			t.Fatalf("%s: expected only the pending address to have a cleanup_hint, got %s", want, out)
		}
		sf, err := state.Read(state.DefaultPath(w.dir))
		if err != nil {
			t.Fatal(err)
		}
		rec := sf.ActiveDeployment()
		if extra && ((!gone && (len(sf.LostFloatingIPs) != 1 || sf.LostFloatingIPs[0].ID != addressID)) ||
			(gone && len(sf.LostFloatingIPs) != 0)) {
			t.Fatalf("%s: extra address receipt %+v", want, sf.LostFloatingIPs)
		}
		if (!gone && (rec == nil || rec.FloatingIPID != parityVM)) || (gone && rec != nil) {
			t.Fatalf("%s: receipt %+v", want, rec)
		}
	}
}

// A rule retry and a receipt check are independent actions on the same destroy.
func TestDestroyJSON_RuleAndAddressHints(t *testing.T) {
	w, _ := withJSONWorld(t)
	_, done := recordDeployment(t, w)
	t.Cleanup(done)
	path := state.DefaultPath(w.dir)
	sf, err := state.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	d := sf.ActiveDeployment()
	d.OpenedRules = []state.SGRule{{ID: "pending-rule", SecurityGroupID: "own-group", Port: "8081/tcp"}}
	sf.LostFloatingIPs = []state.LostFloatingIP{{Name: "unconfirmed-address", VMID: d.VMID, ProjectID: d.ProjectID}}
	if err := state.Write(path, sf); err != nil {
		t.Fatal(err)
	}
	previous := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/security-groups/own-group/rules/pending-rule" {
			return contractRT{status: http.StatusForbidden, body: `{"message":"forbidden"}`}.RoundTrip(r)
		}
		return previous(r)
	}
	exit, out, stderr := runCLI(t, "", "--json", "destroy", "--yes")
	var result struct {
		Status      string
		Hint        string
		CleanupHint string `json:"cleanup_hint"`
	}
	if exit != 0 || json.Unmarshal([]byte(out), &result) != nil || result.Status != "partial" ||
		!strings.Contains(result.Hint, "evo destroy") || !strings.Contains(result.CleanupHint, "evo cleanup receipts") {
		t.Fatalf("expected both recovery actions: exit %d, %s%s", exit, out, stderr)
	}
}

// Keep the existing VM fixture but read and delete rules through the real
// Compute client, including HTTP response classification and pagination.
type ruleReceiptCloud struct {
	provision.CloudAPI
	groups *provision.CloudRuAdapter
}

func (c ruleReceiptCloud) ListSecurityGroups(ctx context.Context, project string) ([]*provision.SecurityGroup, error) {
	return c.groups.ListSecurityGroups(ctx, project)
}

func (c ruleReceiptCloud) ListSecurityGroupRules(ctx context.Context, group string) ([]*provision.SGRule, error) {
	return c.groups.ListSecurityGroupRules(ctx, group)
}

func (c ruleReceiptCloud) DeleteSecurityGroupRule(ctx context.Context, group, id string) error {
	return c.groups.DeleteSecurityGroupRule(ctx, group, id)
}

const receiptGroupPath = "/api/v1/security-groups"

// Groups of the same name and ingress rules for the same source and port have
// different IDs. Only the ID pair on the original receipt may be deleted.
type ruleReceiptHTTP struct {
	t       *testing.T
	phase   string
	deletes []string
}

func (c *ruleReceiptHTTP) RoundTrip(r *http.Request) (*http.Response, error) {
	const own = receiptGroupPath + "/owned-group/rules"
	const other = `{"id":"unrelated","direction":"ingress","ip_protocol":"tcp","port_range":"8081:8081","remote_ip_prefix":"0.0.0.0/0"}`
	f := contractRT{status: 200}
	service404 := contractRT{status: 404, body: `{"message":"Security group not found"}`}
	switch {
	case r.Method == http.MethodDelete:
		c.deletes = append(c.deletes, r.URL.Path)
		if r.URL.Path != own+"/owned-rule" {
			c.t.Errorf("DELETE outside the receipt: %s", r.URL.Path)
		}
		if c.phase == "group absent" {
			f = service404
		} else {
			f.status = http.StatusAccepted
		}
	case r.Method == http.MethodGet && r.URL.Path == receiptGroupPath:
		if r.URL.Query().Get("project_id") != pvProject {
			c.t.Errorf("wrong project in group list: %v", r.URL.Query())
		}
		f.body = `{"items":[{"id":"other-group","name":"same"}],"total":1}`
		switch c.phase {
		case "deleting", "rule absent", "group still listed":
			f.body = `{"items":[{"id":"other-group","name":"same"},{"id":"owned-group","name":"same"}],"total":2}`
		case "incomplete groups":
			f.body = `{"items":[],"total":1}`
		}
	case r.Method == http.MethodGet && r.URL.Path == receiptGroupPath+"/other-group/rules":
		f.body = `{"items":[` + other + `],"total":1}`
	case r.Method == http.MethodGet && r.URL.Path == own:
		switch c.phase {
		case "deleting":
			status := "active"
			if len(c.deletes) != 0 {
				status = "deleting"
			}
			f.body = `{"items":[` + other + `,{"id":"owned-rule","direction":"ingress","ip_protocol":"tcp","port_range":"8081:8081","remote_ip_prefix":"0.0.0.0/0","state":"` + status + `"}],"total":2}`
		case "rule absent":
			f.body = `{"items":[` + other + `],"total":1}`
		case "gateway":
			f = contractRT{status: 404, body: `{"code":5,"message":"Not Found"}`}
		case "unexplained 404":
			f = contractRT{status: 404, body: `{}`}
		case "forbidden":
			f = contractRT{status: 403, body: `{"message":"forbidden"}`}
		case "incomplete rules":
			f.body = `{"items":[],"total":1}`
		default:
			f = service404
		}
	default:
		c.t.Errorf("unexpected Compute request %s %s", r.Method, r.URL.Path)
		f.status = http.StatusMethodNotAllowed
	}
	return f.RoundTrip(r)
}

// First and retry are independent CLI invocations or new MCP servers. The
// restart receives only the exact file written after the partial destroy.
func TestDestroy_ConfirmsRuleReceiptAfterRestart(t *testing.T) {
	for _, surface := range []string{"CLI text", "CLI JSON", "stdio", "HTTP"} {
		for _, scenario := range []string{"gone immediately", "gone on retry", "unconfirmed on first call", "deleting then rule gone", "legacy mask only"} {
			t.Run(surface+"/"+scenario, func(t *testing.T) {
				w, _ := withJSONWorld(t)
				probe := &ruleReceiptHTTP{t: t}
				adapter := provision.NewCloudRuAdapter(&http.Client{Transport: httpretry.UserAgent(probe)}, "https://fixture.invalid")
				adapter.Backoff = time.Millisecond
				base := toolEngine
				toolEngine = func(o map[string]any) (*engine.Engine, error) {
					e, err := base(o)
					if err == nil {
						if scenario == "gone immediately" {
							e.Config.ServiceAccountID = "fixture-service-account"
						}
						e.Cloud = ruleReceiptCloud{e.Cloud, adapter}
					}
					return e, err
				}
				t.Cleanup(func() { toolEngine = base })
				eng, err := toolEngine(nil)
				if err != nil {
					t.Fatal(err)
				}
				if eng.Maker() == nil {
					t.Fatal("fixture is missing its maker")
				}
				path := state.DefaultPath(w.dir)
				rec := state.DeploymentRecord{ID: "dep-rules", VMID: parityVM, VMName: "web", ProjectID: pvProject, Status: "running", CreatedBy: eng.Maker(),
					OpenedRules: []state.SGRule{{ID: "owned-rule", SecurityGroupID: "owned-group", Port: "8081/tcp"}}}
				if scenario == "legacy mask only" {
					rec.CreatedBy = &state.Maker{KeyID: eng.Maker().KeyID}
				}
				if err := state.Write(path, &state.File{Deployments: []state.DeploymentRecord{rec}}); err != nil {
					t.Fatal(err)
				}
				phases := []string{"group absent"}
				if scenario == "gone on retry" {
					phases = []string{"deleting", "gateway", "unexplained 404", "forbidden", "incomplete rules", "incomplete groups", "group still listed", "group absent"}
				} else if scenario == "unconfirmed on first call" {
					phases = []string{"group still listed", "group absent"}
				} else if scenario == "deleting then rule gone" {
					phases = []string{"deleting", "rule absent"}
				} else if scenario == "legacy mask only" {
					// A first-page 404 and empty project inventory cannot identify
					// the original account from its old display mask. A later
					// complete read of this group's own rules settles the receipt.
					phases = []string{"group absent", "group absent", "rule absent"}
				}
				for i, phase := range phases {
					if i > 0 {
						old, err := os.ReadFile(path)
						if err != nil {
							t.Fatal(err)
						}
						dir := t.TempDir()
						path = state.DefaultPath(dir)
						if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, old, 0600); err != nil {
							t.Fatal(err)
						}
						t.Chdir(dir)
					}
					probe.phase = phase
					answer := destroyRuleReceipt(t, surface)
					pending := i < len(phases)-1
					sf, err := state.Read(path)
					if err != nil {
						t.Fatal(err)
					}
					saved := sf.FindDeployment(rec.ID)
					if pending {
						if saved == nil || len(saved.OpenedRules) != 1 || saved.OpenedRules[0].ID != "owned-rule" || saved.OpenedRules[0].SecurityGroupID != "owned-group" || saved.OpenedRules[0].DeleteError == "" {
							t.Fatalf("%s: pending receipt lost: %+v, output %s", phase, saved, answer)
						}
					} else if saved != nil {
						t.Fatalf("%s: receipt remained: %+v, output %s", phase, saved, answer)
					}
					if surface == "CLI text" {
						want := "Closed ports 8081"
						if pending {
							want = "re-run 'evo destroy'"
						}
						if !strings.Contains(answer, want) || !strings.Contains(answer, "8081") {
							t.Fatalf("%s: human output %q lacks %q / port", phase, answer, want)
						}
					} else {
						var row struct {
							Status, Hint   string
							CleanupPending bool                              `json:"cleanup_pending"`
							Rules          struct{ Closed, Failed []string } `json:"security_group_rules"`
						}
						if err := json.Unmarshal([]byte(answer), &row); err != nil {
							t.Fatal(err)
						}
						if pending && (surface == "stdio" || surface == "HTTP") && strings.Contains(row.Hint, "evo ") {
							t.Fatalf("agent was sent to a CLI for a normal rule retry: %s", answer)
						}
						if row.CleanupPending != pending {
							t.Fatalf("%s: incorrect cleanup_pending: %s", phase, answer)
						}
						wantHint := `destroy with vm_id="` + parityVM + `"`
						wantStatus := "ok"
						if i > 0 {
							wantStatus = "already_deleted"
						}
						if surface == "CLI JSON" {
							wantHint, wantStatus = "evo destroy", "destroyed"
							if pending {
								wantStatus = "rules_pending"
								if i == 0 {
									wantStatus = "partial"
								}
							}
						}
						if row.Status != wantStatus {
							t.Fatalf("%s: status %q, want %q", phase, row.Status, wantStatus)
						}
						if pending {
							if !strings.Contains(row.Hint, wantHint) || len(row.Rules.Failed) != 1 || !strings.HasPrefix(row.Rules.Failed[0], "8081:") || len(row.Rules.Closed) != 0 {
								t.Fatalf("%s: missing failed port or recovery step: %s", phase, answer)
							}
						} else if !slices.Equal(row.Rules.Closed, []string{"8081"}) || len(row.Rules.Failed) != 0 {
							t.Fatalf("%s: missing confirmed closure: %s", phase, answer)
						}
					}
					for _, write := range w.rt.takeWrites() {
						if i != 0 || write != "DELETE compute.api.cloud.ru/api/v1/vms/"+parityVM {
							t.Fatalf("%s: unrelated write outside rule cleanup: %s", phase, write)
						}
					}
					if len(probe.deletes) > 1 || i > 0 && len(probe.deletes) != 1 {
						t.Fatalf("%s: unexpected rule DELETEs %v", phase, probe.deletes)
					}
				}
			})
		}
	}
}

func destroyRuleReceipt(t *testing.T, surface string) string {
	t.Helper()
	if strings.HasPrefix(surface, "CLI") {
		args := []string{"destroy", "--yes"}
		if surface == "CLI JSON" {
			args = append(args, "--json")
		}
		code, out, errOut := runCLI(t, "", args...)
		if code != 0 {
			t.Fatalf("destroy exit=%d: %s%s", code, out, errOut)
		}
		if surface == "CLI text" {
			return out + errOut
		}
		return out
	}
	agent := mcp.New(toolEngine, "fixture")
	declareStateDir(agent)
	params := map[string]any{"name": "destroy", "arguments": map[string]any{"vm_id": parityVM, "confirmed": true}}
	var servers roServers
	var a roAnswer
	if surface == "stdio" {
		a, _ = servers.stdio(t, agent, "tools/call", params)
	} else {
		h := mcp.NewHTTPHandler(agent, mcp.HTTPOptions{BearerToken: "parity-token"})
		defer h.Close()
		servers.roHTTP = httptest.NewServer(h)
		defer servers.roHTTP.Close()
		a, _ = servers.http(t, "tools/call", params)
	}
	if a.isErr || a.rpcErr != "" {
		t.Fatalf("destroy MCP refusal: %+v", a)
	}
	return a.text
}

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
