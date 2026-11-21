// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/mcp"
)

const (
	upgradeFrom      = "v1.35.9"
	upgradeTo        = "v1.36.5"
	upgradeClusterOp = "00000000-0000-4000-8000-00000000c001"
	upgradePoolOp    = "00000000-0000-4000-8000-00000000c002"
)

type upgradeResource struct {
	stdio                                           func(map[string]any) roAnswer
	previewID                                       string
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
		if r.stdio == nil {
			r.stdio = processUpgradeStdio(t, newROServers(t).plain)
		}
		args := map[string]any{r.argument: r.id, "version": version, "confirmed": confirm}
		if confirm && r.previewID != "" {
			args["preview_id"] = r.previewID
		}
		result := r.stdio(map[string]any{"name": r.tool, "arguments": args})
		if id, ok := parseObject(result.text)["preview_id"].(string); ok {
			r.previewID = id
		}
		return result.text, result.isErr
	}
	newProcess()
	flag := "--dry-run"
	if confirm {
		flag = "--yes"
	}
	code, out, stderr := runCLI(t, "", "--json", flag, "k8s", r.verb, r.id, "--version", version)
	if out == "" {
		out = stderr
	}
	return out, code != 0
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
					status := "dry_run"
					if surface == "stdio" {
						status = "confirmation_required"
					}
					if failed != (surface == "stdio") || parseObject(text)["status"] != status || r.posts != before || !strings.Contains(text, upgradeFrom) || !strings.Contains(text, upgradeTo) || !strings.Contains(text, r.id) {
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
				if surface == "stdio" {
					checkPreview(&cluster)
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

const (
	upgradeParityCluster = "00000000-0000-4000-8000-00000000c003"
	upgradeParityPool    = "00000000-0000-4000-8000-00000000c004"
	upgradeParityParent  = "00000000-0000-4000-8000-00000000c005"
)

// A pool eligible for its own upgrade belongs to a control plane already at the target.
func parityUpgradeResponse(req *http.Request, upgraded bool) *http.Response {
	if req.URL.Host != "mk8s.api.cloud.ru" {
		return nil
	}
	resource := upgradeResource{kind: "control plane", version: upgradeFrom, state: "OBJECT_STATUS_RUNNING", available: []string{upgradeTo}}
	switch req.URL.Path {
	case "/v3/clusters/" + upgradeParityCluster, "/v3/clusters/" + upgradeParityCluster + ":upgrade":
		resource.id, resource.operation = upgradeParityCluster, upgradeClusterOp
	case "/v3/nodePools/" + upgradeParityPool, "/v3/nodePools/" + upgradeParityPool + ":upgrade":
		resource.id, resource.operation, resource.kind = upgradeParityPool, upgradePoolOp, "node pool"
	case "/v3/clusters/" + upgradeParityParent:
		resource.id, resource.version = upgradeParityParent, upgradeTo
	default:
		return nil
	}
	if upgraded {
		resource.version = upgradeTo
	}
	if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, ":upgrade") {
		return consentJSON(http.StatusOK, fmt.Sprintf(`{"id":%q,"resourceId":%q,"done":false}`, resource.operation, resource.id))
	}
	if req.Method != http.MethodGet {
		return nil
	}
	card := resource.card()
	if resource.kind == "node pool" {
		card["clusterId"] = upgradeParityParent
	}
	b, _ := json.Marshal(card)
	return consentJSON(http.StatusOK, string(b))
}

// Every retry uses a fresh CLI or stdio registry over the same state directory.
func TestUpgradeRejectsUnverifiedEvidence(t *testing.T) {
	for _, scenario := range []string{"project", "changed before POST", "receipt resource", "missing receipt resource", "lost response", "receipt storage", "wrong operation", "wrong operation resource", "operation error", "card forbidden", "pool parent"} {
		t.Run(scenario, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			cluster := upgradeResource{kind: "control plane", tool: "k8s_upgrade", verb: "upgrade", argument: "cluster_id", id: addonClusterID, path: "/v3/clusters/" + addonClusterID, version: upgradeFrom, state: "OBJECT_STATUS_RUNNING", available: []string{upgradeTo}, operation: upgradeClusterOp}
			pool := upgradeResource{kind: "node pool", tool: "k8s_nodepool_upgrade", verb: "nodepool-upgrade", argument: "nodepool_id", id: parityVM, path: "/v3/nodePools/" + parityVM, version: upgradeFrom, state: "OBJECT_STATUS_RUNNING", available: []string{upgradeTo}, operation: upgradePoolOp}
			target := &cluster
			if scenario == "pool parent" {
				target = &pool
				cluster.version = upgradeTo
			}
			gets, posts := 0, 0
			w.rt.extra = func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "mk8s.api.cloud.ru" || req.URL.Query().Get("project_id") != pvProject {
					t.Fatalf("unexpected URL %s", req.URL)
				}
				answer := func(status int, value any) (*http.Response, error) {
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					return contractRT{status: status, body: string(data)}.RoundTrip(req)
				}
				switch {
				case req.Method == "GET" && req.URL.Path == target.path:
					gets++
					card := target.card()
					if scenario == "project" {
						card["projectId"] = parityVM
					}
					if scenario == "changed before POST" && gets > 1 {
						card["versionUpgrade"] = map[string]any{}
					}
					if scenario == "card forbidden" && posts > 0 {
						return answer(403, map[string]string{"message": "forbidden"})
					}
					if scenario == "pool parent" {
						card["clusterId"] = upgradeParityParent
					}
					return answer(200, card)
				case req.Method == "GET" && req.URL.Path == "/v3/clusters/"+upgradeParityParent:
					return answer(200, cluster.card()) // a different cluster ID than the one requested
				case req.Method == "POST" && req.URL.Path == target.path+":upgrade":
					posts++
					op := map[string]any{"id": target.operation, "resourceId": target.id, "done": false}
					switch scenario {
					case "receipt resource":
						op["resourceId"] = parityVM
					case "missing receipt resource":
						delete(op, "resourceId")
					case "lost response":
						return nil, io.ErrUnexpectedEOF
					case "receipt storage":
						// Move the prepared receipt aside and make its destination unwritable as a file.
						files, err := filepath.Glob(filepath.Join(w.dir, "k8s-upgrades", "*.json"))
						if err != nil || len(files) != 1 {
							t.Fatalf("prepared receipts %v, %v", files, err)
						}
						path := files[0]
						if err := os.Rename(path, path+".saved"); err != nil {
							t.Fatal(err)
						}
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
						t.Cleanup(func() { _ = os.Remove(path); _ = os.Rename(path+".saved", path) })
					}
					return answer(200, op)
				case req.Method == "GET" && req.URL.Path == "/v3/operations/"+target.operation:
					op := map[string]any{"id": target.operation, "resourceId": target.id, "done": true}
					switch scenario {
					case "wrong operation":
						op["id"] = upgradePoolOp
					case "wrong operation resource":
						op["resourceId"] = parityVM
					case "operation error":
						op["error"] = map[string]any{"code": 5, "message": "failed"}
					}
					return answer(200, op)
				default:
					t.Fatalf("unexpected request %s %s", req.Method, req.URL)
					return nil, io.EOF
				}
			}
			first, failed := target.call(t, "CLI", upgradeTo, true)
			if scenario == "project" || scenario == "changed before POST" || scenario == "pool parent" {
				if !failed || posts != 0 {
					t.Fatalf("invalid precondition: %s; POST=%d", first, posts)
				}
				return
			}
			if posts != 1 {
				t.Fatalf("first request: %s; POST=%d", first, posts)
			}
			target.version = upgradeTo
			second, failed := target.call(t, "stdio", upgradeTo, true)
			if !failed || saysUpgradeComplete(second) || posts != 1 {
				t.Fatalf("unverified result: %s; POST=%d", second, posts)
			}
		})
	}
}

func TestUpgradeHTTPAndSecondClientDoNotReplay(t *testing.T) {
	w, _ := withJSONWorld(t)
	old := w.rt.extra
	var posts atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	var unblock sync.Once
	resume := func() { unblock.Do(func() { close(release) }) }
	defer resume()
	w.rt.extra = func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost && req.URL.Path == "/v3/clusters/"+upgradeParityCluster+":upgrade" {
			if posts.Add(1) == 1 {
				close(started)
				select {
				case <-release:
				case <-req.Context().Done():
					return nil, req.Context().Err()
				}
			}
			return nil, io.ErrUnexpectedEOF
		}
		return old(req)
	}
	first := mcp.NewHTTPHandler(newROServers(t).plain, mcp.HTTPOptions{})
	second := mcp.NewHTTPHandler(newROServers(t).plain, mcp.HTTPOptions{})
	t.Cleanup(first.Close)
	t.Cleanup(second.Close)
	open := func(h http.Handler) string {
		_, session := retryRPC(t, h, "", "initialize", map[string]any{"protocolVersion": "2025-03-26"})
		if session == "" {
			t.Fatal("missing session")
		}
		return session
	}
	a, b := open(first), open(second)
	var previewID string
	params := func(confirmed bool) map[string]any {
		return map[string]any{"name": "k8s_upgrade", "arguments": map[string]any{"cluster_id": upgradeParityCluster, "version": upgradeTo, "confirmed": confirmed, "preview_id": previewID}}
	}
	preview, _ := retryRPC(t, first, a, "tools/call", params(false))
	if parseObject(preview.text)["status"] != "confirmation_required" || posts.Load() != 0 {
		t.Fatalf("preview: %s", preview.text)
	}
	previewID, _ = parseObject(preview.text)["preview_id"].(string)
	result := make(chan roAnswer, 1)
	go func() { ans, _ := retryRPC(t, first, a, "tools/call", params(true)); result <- ans }()
	select {
	case <-started:
	case got := <-result:
		t.Fatalf("no upgrade request: %s", got.text)
	case <-t.Context().Done():
		t.Fatal("first call did not reach its request")
	}
	other, _ := retryRPC(t, second, b, "tools/call", params(true))
	resume()
	got := <-result
	if !other.isErr || !got.isErr || posts.Load() != 1 {
		t.Fatalf("POST=%d first=%s second=%s", posts.Load(), got.text, other.text)
	}
	again, _ := retryRPC(t, second, b, "tools/call", params(true))
	if !again.isErr || posts.Load() != 1 || saysUpgradeComplete(again.text) {
		t.Fatalf("ambiguous POST repeated: %d %s", posts.Load(), again.text)
	}
}

func TestUpgradeStdioNewProcessNeedsOwnPreview(t *testing.T) {
	w, _ := withJSONWorld(t)
	args := map[string]any{"cluster_id": upgradeParityCluster, "version": upgradeTo}
	preview := callStdioTool(t, "k8s_upgrade", args)
	args["confirmed"] = true
	args["preview_id"] = parseObject(preview.text)["preview_id"]
	fresh := callStdioTool(t, "k8s_upgrade", args)
	if parseObject(preview.text)["status"] != "confirmation_required" || parseObject(fresh.text)["status"] != "confirmation_required" {
		t.Fatalf("first process: %s; fresh process: %s", preview.text, fresh.text)
	}
	if writes := w.rt.takeWrites(); len(writes) != 0 {
		t.Fatalf("two processes shared approval: %v", writes)
	}
}

func TestUpgradeLostPreviewResponse(t *testing.T) {
	for _, surface := range []string{"stdio", "HTTP"} {
		for _, kind := range []string{"control plane", "node pool"} {
			t.Run(surface+"/"+kind, func(t *testing.T) {
				w, _ := withJSONWorld(t)
				r := upgradeResource{kind: kind, tool: "k8s_upgrade", argument: "cluster_id", id: upgradeParityCluster, version: upgradeFrom, state: "OBJECT_STATUS_RUNNING", available: []string{upgradeTo}}
				path := "/v3/clusters/" + r.id
				if kind == "node pool" {
					r.tool, r.argument, r.id = "k8s_nodepool_upgrade", "nodepool_id", upgradeParityPool
					path = "/v3/nodePools/" + r.id
				}
				old := w.rt.extra
				posts := 0
				w.rt.extra = func(req *http.Request) (*http.Response, error) {
					if req.Method == "GET" && req.URL.Path == path {
						card := r.card()
						if kind == "node pool" {
							card["clusterId"] = upgradeParityParent
						}
						b, err := json.Marshal(card)
						if err != nil {
							return nil, err
						}
						return consentJSON(200, string(b)), nil
					}
					if req.Method == "POST" && req.URL.Path == path+":upgrade" {
						posts++
					}
					return old(req)
				}
				server := newROServers(t).plain
				var call func(map[string]any) roAnswer
				if surface == "stdio" {
					call = processUpgradeStdio(t, server)
				} else {
					h := mcp.NewHTTPHandler(server, mcp.HTTPOptions{})
					t.Cleanup(h.Close)
					_, session := retryRPC(t, h, "", "initialize", map[string]any{"protocolVersion": "2025-03-26"})
					call = func(params map[string]any) roAnswer { a, _ := retryRPC(t, h, session, "tools/call", params); return a }
				}
				args := map[string]any{r.argument: r.id, "version": upgradeTo}
				params := map[string]any{"name": r.tool, "arguments": args}
				preview := call(params)
				if id, ok := parseObject(preview.text)["preview_id"].(string); ok {
					args["preview_id"] = id
				}
				args["confirmed"] = true
				r.version = "v1.35.10"
				call(params) // The replacement preview is lost before the client can read it.
				retry := call(params)
				if posts != 0 {
					t.Fatalf("old confirmation replayed after a lost preview: %d POST", posts)
				}
				id, _ := parseObject(retry.text)["preview_id"].(string)
				if id == "" || id == args["preview_id"] || !strings.Contains(retry.text, r.version) {
					t.Fatalf("missing replacement preview: %s", retry.text)
				}
				args["preview_id"] = id
				result := call(params)
				if result.isErr || posts != 1 {
					t.Fatalf("fresh approval: %s, POST=%d", result.text, posts)
				}
			})
		}
	}
}
