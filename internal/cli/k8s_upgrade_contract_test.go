// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
)

const (
	upgradeFrom      = "v1.35.9"
	upgradeTo        = "v1.36.5"
	upgradeClusterOp = "00000000-0000-4000-8000-00000000c001"
	upgradePoolOp    = "00000000-0000-4000-8000-00000000c002"
)

type upgradeResource struct {
	kind, tool, verb, argument, id, path, operation string
	version, state, desired, phase                  string
	available                                       []string
	posts, operationReads                           int
	done                                            bool
}

func (r *upgradeResource) card() map[string]any {
	out := map[string]any{"id": r.id, "projectId": pvProject, "name": r.kind, "status": r.state,
		"versionUpgrade": map[string]any{"upgradeAvailable": len(r.available) > 0, "availableVersions": r.available}}
	version := map[string]any{"version": r.version}
	if r.desired != "" {
		version["upgradeInfo"] = map[string]any{"desiredVersion": r.desired, "phase": r.phase}
	}
	if r.kind == "control plane" {
		out["controlPlane"] = version
	} else {
		out["clusterId"] = addonClusterID
		for k, v := range version {
			out[k] = v
		}
	}
	return out
}

func (r *upgradeResource) call(t *testing.T, surface, version string, confirm bool) (string, bool) {
	t.Helper()
	if surface == "stdio" {
		result := callStdioTool(t, r.tool, map[string]any{r.argument: r.id, "version": version, "confirmed": confirm})
		return result.text, result.isErr
	}
	newProcess()
	flag := "--dry-run"
	if confirm {
		flag = "--yes"
	}
	code, out, stderr := runCLI(t, "", "--json", flag, "k8s", r.verb, r.id, "--version", version)
	return out + stderr, code != 0
}

func TestUpgradeSeparateConsentAndObservedVersion(t *testing.T) {
	for _, surface := range []string{"CLI", "stdio"} {
		for _, scenario := range []string{"preview", "target unavailable", "separate consent", "changed target", "lost response", "operation done with stale versions", "rollback", "cancelled pool"} {
			t.Run(surface+"/"+scenario, func(t *testing.T) {
				cluster := upgradeResource{kind: "control plane", tool: "k8s_upgrade", verb: "upgrade", argument: "cluster_id", id: addonClusterID, path: "/v3/clusters/" + addonClusterID, operation: upgradeClusterOp}
				pool := upgradeResource{kind: "node pool", tool: "k8s_nodepool_upgrade", verb: "nodepool-upgrade", argument: "nodepool_id", id: parityVM, path: "/v3/nodePools/" + parityVM, operation: upgradePoolOp}
				resources := []*upgradeResource{&cluster, &pool}
				w, _ := withJSONWorld(t)
				for _, r := range resources {
					r.version, r.state = upgradeFrom, "OBJECT_STATUS_RUNNING"
					r.available = []string{upgradeTo}
					if _, ok := newRegistry().Lookup(r.tool); !ok {
						t.Errorf("%s is absent from the registry", r.tool)
					}
				}
				if t.Failed() {
					t.FailNow()
				}
				w.rt.extra = func(req *http.Request) (*http.Response, error) {
					answer := func(data any) (*http.Response, error) {
						b, err := json.Marshal(data)
						if err != nil {
							return nil, err
						}
						return contractRT{status: 200, body: string(b)}.RoundTrip(req)
					}
					if req.URL.Host != "mk8s.api.cloud.ru" || req.URL.Query().Get("project_id") != pvProject {
						t.Errorf("unexpected project or host: %s", req.URL)
						return nil, fmt.Errorf("outside the test project")
					}
					for _, r := range resources {
						switch {
						case req.Method == "GET" && req.URL.Path == r.path:
							return answer(r.card())
						case req.Method == "GET" && req.URL.Path == "/v3/operations/"+r.operation:
							r.operationReads++
							return answer(map[string]any{"id": r.operation, "resourceId": r.id, "done": r.done})
						case req.Method == "POST" && req.URL.Path == r.path+":upgrade":
							r.posts++
							var body map[string]any
							if err := json.NewDecoder(req.Body).Decode(&body); err != nil || body["version"] != upgradeTo {
								t.Errorf("incorrect upgrade body for %s: %v", r.kind, body)
							}
							if !slices.Contains(r.available, upgradeTo) || (r == &pool && cluster.version != upgradeTo) {
								t.Errorf("premature upgrade of %s", r.kind)
							}
							r.desired, r.state = upgradeTo, "OBJECT_STATUS_UPGRADING"
							r.phase = "CONTROL_PLANE_UPGRADE_PHASE_RUNNING"
							if r == &pool {
								r.phase = "NODE_POOL_UPGRADE_PHASE_RUNNING"
							}
							if scenario == "lost response" {
								return nil, io.ErrUnexpectedEOF
							}
							return answer(map[string]any{"id": r.operation, "resourceId": r.id, "done": false})
						}
					}
					t.Errorf("unexpected %s %s", req.Method, req.URL.Path)
					return nil, fmt.Errorf("unexpected request")
				}
				checkPreview := func(r *upgradeResource) {
					t.Helper()
					before := r.posts
					text, failed := r.call(t, surface, upgradeTo, false)
					if failed || r.posts != before || !strings.Contains(text, upgradeFrom) || !strings.Contains(text, upgradeTo) || !strings.Contains(text, r.id) {
						t.Fatalf("preview for %s: %s", r.kind, text)
					}
				}
				if scenario == "preview" {
					checkPreview(&cluster)
					cluster.version = upgradeTo
					checkPreview(&pool)
					return
				}
				if scenario == "target unavailable" {
					cluster.available = nil
					_, failed := cluster.call(t, surface, upgradeTo, true)
					if !failed || cluster.posts+pool.posts != 0 {
						t.Fatal("unavailable version accepted")
					}
					return
				}
				text, failed := cluster.call(t, surface, upgradeTo, true)
				if scenario == "lost response" {
					if cluster.posts != 1 || saysUpgradeComplete(text) {
						t.Fatalf("ambiguous first attempt: %s", text)
					}
					cluster.state, cluster.desired = "OBJECT_STATUS_RUNNING", ""
					cluster.call(t, surface, upgradeTo, true)
					if cluster.posts != 1 || pool.posts != 0 {
						t.Fatalf("ambiguous request replayed: cluster=%d pool=%d", cluster.posts, pool.posts)
					}
					return
				}
				if failed || cluster.posts != 1 || pool.posts != 0 || !strings.Contains(text, cluster.operation) {
					t.Fatalf("cluster receipt or write count: %s", text)
				}
				if scenario == "changed target" {
					cluster.available = []string{"v1.36.6"}
					before := cluster.operationReads
					cluster.call(t, surface, upgradeTo, true)
					if cluster.operationReads == before {
						t.Error("resume did not read the recorded operation")
					}
					_, failed = cluster.call(t, surface, "v1.36.6", true)
					if !failed || cluster.posts != 1 || pool.posts != 0 {
						t.Fatal("changed or repeated the pending target")
					}
					return
				}
				cluster.done = true
				cluster.state = "OBJECT_STATUS_RUNNING"
				_, failed = pool.call(t, surface, upgradeTo, true)
				if !failed || pool.posts != 0 || cluster.posts != 1 {
					t.Fatal("pool accepted before observed control-plane version")
				}
				if scenario == "rollback" {
					cluster.phase = "CONTROL_PLANE_UPGRADE_PHASE_ROLLBACK"
					text, _ = cluster.call(t, surface, upgradeTo, true)
					if saysUpgradeComplete(text) || cluster.posts != 1 {
						t.Fatalf("rollback declared successful or replayed: %s", text)
					}
					return
				}
				cluster.version, cluster.desired = upgradeTo, ""
				checkPreview(&pool)
				pool.done = false
				text, failed = pool.call(t, surface, upgradeTo, true)
				if failed || pool.posts != 1 || cluster.posts != 1 || !strings.Contains(text, pool.operation) {
					t.Fatalf("pool receipt or write count: %s", text)
				}
				if scenario == "separate consent" {
					return
				}
				pool.done = true
				pool.state, pool.desired = "OBJECT_STATUS_RUNNING", ""
				if scenario == "cancelled pool" {
					pool.phase, pool.desired = "NODE_POOL_UPGRADE_PHASE_CANCELLED", upgradeTo
				}
				text, _ = pool.call(t, surface, upgradeTo, true)
				if saysUpgradeComplete(text) || pool.posts != 1 {
					t.Fatalf("unverified pool version treated as complete or replayed: %s", text)
				}
				if scenario == "operation done with stale versions" {
					pool.version, pool.desired = upgradeTo, ""
					text, failed = pool.call(t, surface, upgradeTo, true)
					if failed || !saysUpgradeComplete(text) || pool.posts != 1 || cluster.posts != 1 {
						t.Fatalf("observed target version was not recognized: %s", text)
					}
				}
			})
		}
	}
}

func saysUpgradeComplete(text string) bool {
	status, _ := parseObject(text)["status"].(string)
	return status == "already_upgraded" || status == "upgraded" || status == "ok"
}
