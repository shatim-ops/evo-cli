// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/mcp"
)

// Keep one real stdio server running across preview and confirmation.
func processUpgradeStdio(t *testing.T, server *mcp.Server) func(map[string]any) roAnswer {
	t.Helper()
	in, write := io.Pipe()
	read, out := io.Pipe()
	done := make(chan error, 1)
	go func() { err := server.Run(in, out); out.CloseWithError(err); done <- err }()
	t.Cleanup(func() {
		write.Close()
		read.Close()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	decoder := json.NewDecoder(read)
	exchange := func(line []byte) json.RawMessage {
		t.Helper()
		if _, err := write.Write(line); err != nil {
			t.Fatal(err)
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	exchange([]byte(stdioInit))
	return func(params map[string]any) roAnswer {
		t.Helper()
		ans, _ := roDecode(t, exchange(append(roRequest("tools/call", params), '\n')))
		return ans
	}
}

func TestProcessUpgradeConsent(t *testing.T) {
	for _, surface := range []string{"CLI", "stdio", "HTTP"} {
		modes := []string{"first-cluster", "first-pool", "unchanged", "unchanged-pool", "source-version-changed", "pool-parent-changed", "pool-version-changed", "target-changed", "cluster-changed", "project-changed", "lost-pool-response", "foreign-project"}
		if surface != "CLI" {
			modes = append(modes, "pool-after-cluster-preview")
		}
		if surface == "HTTP" {
			modes = append(modes, "other-session", "no-session")
		}
		for _, mode := range modes {
			t.Run(surface+"/"+mode, func(t *testing.T) {
				w, _ := withJSONWorld(t)
				cluster := upgradeResource{kind: "control plane", tool: "k8s_upgrade", verb: "upgrade", argument: "cluster_id", id: addonClusterID, path: "/v3/clusters/" + addonClusterID, operation: upgradeClusterOp, version: upgradeFrom, state: "OBJECT_STATUS_RUNNING", available: []string{upgradeTo}}
				pool := upgradeResource{kind: "node pool", tool: "k8s_nodepool_upgrade", verb: "nodepool-upgrade", argument: "nodepool_id", id: parityVM, path: "/v3/nodePools/" + parityVM, operation: upgradePoolOp, version: upgradeFrom, state: "OBJECT_STATUS_RUNNING", available: []string{upgradeTo}}
				parent, project := cluster.id, pvProject
				effectiveProject := pvProject
				if mode == "project-changed" {
					original := toolEngine
					toolEngine = func(opts map[string]any) (*engine.Engine, error) {
						e, err := original(opts)
						if err == nil {
							e.Config.ProjectID = effectiveProject
						}
						return e, err
					}
					t.Cleanup(func() { toolEngine = original })
				}
				target := &cluster
				if mode == "pool-parent-changed" || mode == "pool-version-changed" || mode == "lost-pool-response" || mode == "first-pool" || mode == "unchanged-pool" {
					target = &pool
					cluster.version = upgradeTo
				}
				version := upgradeTo
				if mode == "target-changed" {
					cluster.available = append(cluster.available, "v1.36.6")
				}
				w.rt.extra = func(r *http.Request) (*http.Response, error) {
					response := func(v any) (*http.Response, error) {
						b, e := json.Marshal(v)
						if e != nil {
							t.Fatal(e)
						}
						return consentJSON(200, string(b)), nil
					}
					if r.URL.Host != "mk8s.api.cloud.ru" || r.URL.Query().Get("project_id") != effectiveProject {
						t.Fatalf("unexpected origin or project %s", r.URL)
					}
					for _, obj := range []*upgradeResource{&cluster, &pool} {
						if r.Method == "GET" && r.URL.Path == obj.path {
							card := obj.card()
							card["projectId"] = project
							if obj == &pool {
								card["clusterId"] = parent
							}
							return response(card)
						}
						if r.Method == "POST" && r.URL.Path == obj.path+":upgrade" {
							var body map[string]any
							if e := json.NewDecoder(r.Body).Decode(&body); e != nil || len(body) != 1 || body["version"] != version {
								t.Fatalf("unexpected body %v: %v", body, e)
							}
							obj.posts++
							if mode == "lost-pool-response" {
								return nil, io.ErrUnexpectedEOF
							}
							return response(map[string]any{"id": obj.operation, "resourceId": obj.id, "done": false})
						}
					}
					if r.Method == "GET" && r.URL.Path == "/v3/clusters/"+parent && parent != cluster.id {
						card := cluster.card()
						card["id"] = parent
						return response(card)
					}
					t.Fatalf("unexpected request %s %s", r.Method, r.URL)
					return nil, fmt.Errorf("unexpected request")
				}
				var rpc func(map[string]any) roAnswer
				var initialize func() string
				var session string
				if surface != "CLI" {
					srv := newROServers(t)
					if surface == "stdio" {
						rpc = processUpgradeStdio(t, srv.plain)
					} else {
						h := mcp.NewHTTPHandler(srv.plain, mcp.HTTPOptions{})
						t.Cleanup(h.Close)
						initialize = func() string {
							t.Helper()
							_, id := retryRPC(t, h, "", "initialize", map[string]any{"protocolVersion": "2025-03-26"})
							if id == "" {
								t.Fatal("no session")
							}
							return id
						}
						session = initialize()
						rpc = func(params map[string]any) roAnswer { a, _ := retryRPC(t, h, session, "tools/call", params); return a }
					}
				}
				call := func(confirm bool) roAnswer {
					if surface == "CLI" {
						text, failed := target.call(t, "CLI", version, confirm)
						return roAnswer{text: text, isErr: failed}
					}
					return rpc(map[string]any{"name": target.tool, "arguments": map[string]any{target.argument: target.id, "version": version, "confirmed": confirm}})
				}
				checkQuestion := func(answer roAnswer) {
					t.Helper()
					status := parseObject(answer.text)["status"]
					if answer.rpcErr != "" || (status != "confirmation_required" && !(surface == "CLI" && status == "dry_run")) || cluster.posts+pool.posts != 0 ||
						!strings.Contains(answer.text, effectiveProject) || !strings.Contains(answer.text, target.id) || !strings.Contains(answer.text, target.version) || !strings.Contains(answer.text, version) ||
						(target == &pool && !strings.Contains(answer.text, parent)) {
						t.Fatalf("expected visible current preview and zero POST, got %+v POST=%d/%d", answer, cluster.posts, pool.posts)
					}
				}
				checkSent := func(a roAnswer) {
					t.Helper()
					if a.rpcErr != "" || a.isErr || target.posts != 1 || cluster.posts+pool.posts != 1 || parseObject(a.text)["operation_id"] != target.operation {
						t.Fatalf("expected one approved %s POST: %+v POST=%d/%d", target.kind, a, cluster.posts, pool.posts)
					}
				}
				if mode == "first-cluster" || mode == "first-pool" {
					a := call(true)
					if surface != "CLI" {
						if cluster.posts+pool.posts != 0 || (!a.isErr && a.rpcErr == "" && parseObject(a.text)["status"] != "confirmation_required") {
							t.Fatalf("first unbound MCP confirmation: %+v POST=%d/%d", a, cluster.posts, pool.posts)
						}
						checkQuestion(call(false))
						a = call(true)
					}
					checkSent(a)
					return
				}
				checkQuestion(call(false))
				switch mode {
				case "source-version-changed":
					cluster.version = "v1.35.10"
				case "pool-version-changed":
					target.version = "v1.35.10"
				case "pool-after-cluster-preview":
					cluster.version = upgradeTo
					target = &pool
				case "pool-parent-changed":
					parent = "00000000-0000-4000-8000-000000000321"
				case "target-changed":
					version = "v1.36.6"
				case "cluster-changed":
					cluster.id = "00000000-0000-4000-8000-000000000321"
					cluster.path = "/v3/clusters/" + cluster.id
				case "other-session":
					old := session
					session = initialize()
					if old == session {
						t.Fatal("same session")
					}
				case "no-session":
					session = ""
				case "project-changed":
					effectiveProject = "00000000-0000-4000-8000-000000000322"
					project = effectiveProject
				case "foreign-project":
					project = "00000000-0000-4000-8000-000000000322"
				}
				answer := call(true)
				if mode == "unchanged" || mode == "unchanged-pool" {
					checkSent(answer)
					return
				}
				if mode == "lost-pool-response" {
					if !answer.isErr || pool.posts != 1 || cluster.posts != 0 {
						t.Fatalf("first uncertain write %+v POST=%d/%d", answer, cluster.posts, pool.posts)
					}
					for _, surface := range []string{"CLI", "stdio"} {
						txt, _ := target.call(t, surface, upgradeTo, true)
						if saysUpgradeComplete(txt) || pool.posts != 1 || cluster.posts != 0 {
							t.Fatalf("%s after EOF: %s POST=%d/%d", surface, txt, cluster.posts, pool.posts)
						}
					}
					return
				}
				if mode == "foreign-project" || mode == "no-session" {
					if cluster.posts+pool.posts != 0 || (!answer.isErr && answer.rpcErr == "" && parseObject(answer.text)["status"] != "confirmation_required") {
						t.Fatalf("expected refusal or question: %s POST=%d/%d", answer.text, cluster.posts, pool.posts)
					}
					return
				}
				if mode == "other-session" {
					if cluster.posts+pool.posts != 0 {
						t.Fatal("previous session authorized a POST in the new session")
					}
					checkQuestion(call(false))
				} else {
					checkQuestion(answer)
				}
				checkSent(call(true))
			})
		}
	}
}
