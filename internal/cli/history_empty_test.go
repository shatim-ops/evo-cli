// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/clock"
	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/sgrules"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// An empty since is not «the last 30 days»: it is refused USAGE, as an
// empty project_id beside it is (D100), over MCP and in the CLI — an agent
// that built the argument from an empty variable reads another window than
// it meant (A19-33, #2435).
func TestHistory_EmptySinceIsUsage(t *testing.T) {
	w, _ := withJSONWorld(t)
	srv := parityServers(t)
	for _, arg := range []string{"since", "project_id"} {
		m := stdioDetails(t, srv.stdio, "history", map[string]any{arg: ""})
		if m["reason"] != "USAGE" || m["argument"] != arg {
			t.Errorf("mcp %s: \"\": want USAGE naming it: %v", arg, m)
		}
		forgetRun(w)
		code, out, errOut := runCLI(t, "", "history", "--"+strings.ReplaceAll(arg, "_", "-"), "", "--json")
		if code == 0 || !strings.Contains(out+errOut, "USAGE") {
			t.Errorf("cli --%s \"\": want USAGE, code %d: %s%s", arg, code, out, errOut)
		}
	}
}

func TestContract_HistoryAfterPartialComposeCleanup(t *testing.T) {
	w, _ := withJSONWorld(t)
	_, done := paritySetup["deploy"](t, w)
	defer done()
	old := w.rt.extra
	refuseRelease := true
	releases := 0
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == "DELETE" && strings.Contains(r.URL.Path, "/security-groups/") && strings.Contains(r.URL.Path, "/rules/") {
			return contractRT{status: 200, body: "{}"}.RoundTrip(r)
		}
		if r.Method == "DELETE" && r.URL.Path == "/api/v1/floating-ips/"+parityVM {
			releases++
			if refuseRelease {
				return contractRT{status: 403, body: `{"message":"release refused"}`}.RoundTrip(r)
			}
		}
		return old(r)
	}
	code, stdout, stderr := runCLI(t, "", "--json", "deploy", "--yes")
	if code != 0 {
		t.Fatalf("deploy: %s%s", stdout, stderr)
	}
	if code, _, _ := runCLI(t, "", "--json", "destroy", "--yes"); code == 0 || releases == 0 {
		t.Fatalf("partial destroy: exit %d, releases %d", code, releases)
	}
	later := clock.Now().Add(time.Hour)
	clock.Now = func() time.Time { return later }
	check := func() roAnswer {
		return callStdioTool(t, "history", map[string]any{"check": true}, "--read-only", "--allow-project", pvProject)
	}
	a := check()
	var v struct {
		Live []struct {
			Tool, Outcome string
			Result        string `json:"check_result"`
			ID            string `json:"resource_id"`
		}
		Would []struct{ Tool string } `json:"would_close"`
	}
	if a.isErr || json.Unmarshal([]byte(a.text), &v) != nil {
		t.Fatal(a.text)
	}
	found := false
	for _, line := range v.Live {
		if line.Tool == "destroy" && line.ID == parityVM && line.Outcome == "unknown" && line.Result == "cleanup_unconfirmed" {
			found = true
		}
	}
	if !found {
		t.Errorf("missing pending destroy: %s", a.text)
	}
	for _, line := range v.Would {
		if line.Tool == "destroy" {
			t.Errorf("would close a destroy with an outstanding address: %s", a.text)
		}
	}
	failedAttempts := releases
	refuseRelease = false
	newProcess()
	code, stdout, stderr = runCLI(t, "", "--json", "destroy", "--yes")
	if code != 0 || releases <= failedAttempts {
		t.Fatalf("retry: exit %d releases %d: %s%s", code, releases, stdout, stderr)
	}
	a = check()
	if a.isErr || json.Unmarshal([]byte(a.text), &v) != nil || len(v.Live) != 0 {
		t.Fatalf("after retry: %s", a.text)
	}
}

// The two services return different evidence of the project. Start with
// the lines an async CLI delete actually records, including its projectless line.
func TestContract_HistoryAfterAsyncClusterDelete(t *testing.T) {
	for _, tc := range []struct {
		service, host string
		size          []string
	}{
		{"pg", "postgresql.api.cloud.ru", []string{"--specification-id", "std"}},
		{"redis", "redis.api.cloud.ru", []string{"--cpu", "2", "--ram", "4"}},
	} {
		t.Run(tc.service, func(t *testing.T) {
			// The restricted MCP factory obtains a token from IAM.
			oldClient := httpClient
			httpClient = &http.Client{Transport: contractRT{status: 200, body: `{"access_token":"model","expires_in":3600}`}}
			t.Cleanup(func() { httpClient = oldClient })
			w, _ := withJSONWorld(t)
			// A new process must also check the journal written in the legacy data directory.
			if err := os.Mkdir(filepath.Join(w.dir, config.LegacyDataDirName), 0700); err != nil {
				t.Fatal(err)
			}
			old := w.rt.extra
			cardProject, cardID, phase := "", parityVM, "READY"
			listed, absent := true, false
			reads := 0
			created := false
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if !created || r.Method != "GET" || r.URL.Host != tc.host {
					return old(r)
				}
				switch r.URL.Path {
				case "/v1/clusters/" + parityVM:
					reads++
					if absent {
						return contractRT{status: 404, body: `{"message":"cluster not found"}`}.RoundTrip(r)
					}
					card := map[string]string{"id": cardID, "status": phase}
					if cardProject != "" {
						card["project_id"] = cardProject
					}
					body, err := json.Marshal(card)
					if err != nil {
						t.Fatal(err)
					}
					return contractRT{status: 200, body: string(body)}.RoundTrip(r)
				case "/v1/clusters":
					if r.URL.Query().Get("project_id") != pvProject {
						t.Errorf("unexpected list query: %s", r.URL)
					}
					if !listed {
						return contractRT{status: 200, body: `{"clusters":[]}`}.RoundTrip(r)
					}
					return contractRT{status: 200, body: fmt.Sprintf(`{"clusters":[{"id":%q}]}`, parityVM)}.RoundTrip(r)
				}
				return old(r)
			}
			newProcess()
			args := append([]string{"--json", "--yes", tc.service, "create", "sample"}, tc.size...)
			code, out, stderr := runCLI(t, "", args...)
			id, _ := parseObject(out)["cluster_id"].(string)
			if code != 0 || id != parityVM {
				t.Fatalf("create: %s%s", out, stderr)
			}
			created = true
			if tc.service == "pg" {
				cardProject = pvProject
			}
			newProcess()
			code, out, stderr = runCLI(t, "", "--json", "--yes", tc.service, "delete", id, "--wait=false")
			if code != 0 || parseObject(out)["status"] != "deleting" {
				t.Fatalf("async delete: %s%s", out, stderr)
			}
			later := clock.Now().Add(time.Hour)
			clock.Now = func() time.Time { return later }
			type line struct {
				ID      string `json:"resource_id"`
				Project string `json:"project_id"`
				Result  string `json:"check_result"`
			}
			// The bound transports must skip the projectless delete before any
			// cloud read. The unbound CLI checks both this line and the create.
			calls := mcpTransports(t, nil)
			for name, call := range mcpTransports(t, []string{pvProject}) {
				calls["restricted "+name] = call
			}
			calls["CLI"] = func(_ string, _ map[string]any) roAnswer {
				newProcess()
				code, out, stderr := runCLI(t, "", "history", "--check", "--json")
				if code != 0 {
					t.Fatal(out, stderr)
				}
				return roAnswer{text: out}
			}
			w.rt.takeWrites() // create and delete above are the only mutations
			for _, c := range []struct {
				name, status, project, id, result string
				listed, absent                    bool
			}{
				{"own active", "ERASING", cardProject, id, "there", true, false},
				{"own terminal", "ERASED", cardProject, id, "gone", true, false},
				{"foreign terminal", "DELETED", pvImage, id, "read_failed", true, false},
				{"unknown project", "ERASED", "", id, "read_failed", false, false},
				{"wrong ID", "ERASED", pvProject, pvImage, "read_failed", true, false},
				{"absent without project evidence", "", "", id, "not_seen", false, true},
			} {
				t.Run(c.name, func(t *testing.T) {
					cardProject, cardID, phase, listed, absent = c.project, c.id, c.status, c.listed, c.absent
					for surface, call := range calls {
						reads = 0
						answer := call("history", map[string]any{"check": true})
						var view struct {
							Live       []line
							WouldClose []line `json:"would_close"`
							Hidden     int    `json:"hidden_without_project"`
							Checked    struct{ Read, Gone int }
						}
						if answer.isErr || answer.rpcErr != "" || json.Unmarshal([]byte(answer.text), &view) != nil {
							t.Fatal(surface, answer)
						}
						rows, hidden := 2, 0
						bound := strings.HasPrefix(surface, "restricted ")
						if bound {
							rows, hidden = 1, 1
						}
						if reads != rows || view.Checked.Read != rows || view.Hidden != hidden {
							t.Errorf("%s: %d reads, hidden=%d, checked=%+v; want %d reads and %d hidden", surface, reads, view.Hidden, view.Checked, rows, hidden)
						}
						if c.result == "gone" {
							if view.Checked.Gone != rows || len(view.Live) != 0 || len(view.WouldClose) != 0 {
								t.Errorf("%s: %s", surface, answer.text)
							}
							continue
						}
						if view.Checked.Gone != 0 || len(view.Live) != rows {
							t.Errorf("%s: %s", surface, answer.text)
						}
						for _, l := range view.Live {
							want := c.result
							if c.absent && l.Project == "" {
								want = "read_failed"
							}
							if l.ID != id || l.Result != want || (bound && l.Project != pvProject) {
								t.Errorf("%s: live %+v; want %s", surface, l, want)
							}
						}
						if c.absent {
							if len(view.WouldClose) != 1 || view.WouldClose[0].Project != pvProject {
								t.Errorf("%s: would_close %+v", surface, view.WouldClose)
							}
						} else if len(view.WouldClose) != 0 {
							t.Errorf("%s: unexpected would_close %+v", surface, view.WouldClose)
						}
					}
				})
			}
			if writes := w.rt.takeWrites(); len(writes) != 0 {
				t.Fatalf("history modified cloud resources: %v", writes)
			}
		})
	}
}

// After a lost VM response, inventory of rules is the only way to
// reconcile the original operation. The rule ID was never in state.
type labelInventoryCloud struct {
	provision.CloudAPI
	available bool
	present   bool
	deletes   int
}

func (c *labelInventoryCloud) ListSecurityGroups(_ context.Context, _ string) ([]*provision.SecurityGroup, error) {
	if !c.available {
		return nil, errors.New("test inventory unavailable")
	}
	return []*provision.SecurityGroup{{ID: "sg-test"}}, nil
}
func (c *labelInventoryCloud) ListSecurityGroupRules(_ context.Context, group string) ([]*provision.SGRule, error) {
	if group != "sg-test" || !c.available {
		return nil, errors.New("test inventory unavailable")
	}
	if !c.present {
		return nil, nil
	}
	return []*provision.SGRule{{
		ID: "label-test", Direction: "ingress", IPProtocol: "tcp",
		PortRange: "8081:8081", Description: sgrules.RuleLabel("8081", parityVM),
	}}, nil
}
func (c *labelInventoryCloud) DeleteSecurityGroupRule(_ context.Context, group, id string) error {
	if group != "sg-test" || id != "label-test" || !c.present {
		return errors.New("unexpected cleanup")
	}
	c.present = false
	c.deletes++
	return nil
}

func TestDestroy_LabelInventoryResumesOriginalJournalEntry(t *testing.T) {
	w, fake := withJSONWorld(t)
	inventory := &labelInventoryCloud{present: true}
	base := toolEngine
	toolEngine = func(over map[string]any) (*engine.Engine, error) {
		e, err := base(over)
		if err == nil {
			inventory.CloudAPI = e.Cloud
			e.Cloud = inventory
		}
		return e, err
	}
	t.Cleanup(func() { toolEngine = base })
	eng, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := state.DeploymentRecord{ID: "dep-with-label", VMID: parityVM, ProjectID: pvProject, Status: "running", CreatedBy: eng.Maker()}
	if err := state.Write(state.DefaultPath(w.dir), &state.File{Deployments: []state.DeploymentRecord{rec}}); err != nil {
		t.Fatal(err)
	}
	previous := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodDelete && r.URL.Path == "/api/v1/vms/"+parityVM {
			fake.mu.Lock()
			fake.vmGone = true
			fake.mu.Unlock()
			return nil, io.ErrUnexpectedEOF
		}
		return previous(r)
	}
	if _, failed := destroyLegacyPrimary(t, "CLI", false); !failed {
		t.Fatal("first VM response should be lost")
	}
	w.rt.extra = previous
	dir := intent.Dir(w.dir)
	pending := intent.Pending(dir)
	if len(pending) != 1 {
		t.Fatalf("lost response should leave one intent, got %d", len(pending))
	}
	original := pending[0].ID
	for _, available := range []bool{false, true} {
		inventory.available = available
		answer, failed := destroyLegacyPrimary(t, "CLI", false)
		var result struct{ Status string }
		if failed || json.Unmarshal([]byte(answer), &result) != nil {
			t.Fatalf("destroy failed: %s", answer)
		}
		pending = intent.Pending(dir)
		log, err := history.ReadAll(history.DirOf(dir), func(l history.Line) bool {
			return l.IntentID == original && l.Outcome == history.Deleted && l.ResourceID == parityVM
		})
		if err != nil {
			t.Fatal(err)
		}
		if !available {
			if result.Status != "partial" || len(pending) != 1 || pending[0].ID != original ||
				len(log.Lines) != 0 || !inventory.present || inventory.deletes != 0 {
				t.Errorf("unavailable: status=%s original pending=%v confirmed=%d rule present=%t deletes=%d",
					result.Status, pending, len(log.Lines), inventory.present, inventory.deletes)
			}
		} else if result.Status != "destroyed" || len(pending) != 0 || len(log.Lines) != 1 || inventory.present || inventory.deletes != 1 {
			t.Errorf("restored: status=%s pending=%d original confirmed=%d rule present=%t deletes=%d",
				result.Status, len(pending), len(log.Lines), inventory.present, inventory.deletes)
		}
	}
}
