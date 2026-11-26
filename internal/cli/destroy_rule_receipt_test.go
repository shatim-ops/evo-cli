// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
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
	"github.com/shatim-ops/evo-cli/internal/mcp"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

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
						if row.CleanupPending != pending {
							t.Fatalf("%s: incorrect cleanup_pending: %s", phase, answer)
						}
						wantHint := "evo vm delete"
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
