// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestUpgradeSubmissionRefusal(t *testing.T) {
	for _, tool := range []string{"k8s_upgrade", "k8s_nodepool_upgrade"} {
		for _, status := range []int{400, 403, 409, 408, 503} {
			t.Run(fmt.Sprintf("%s/%d", tool, status), func(t *testing.T) {
				w, _ := withJSONWorld(t)
				id, arg, group := upgradeParityCluster, "cluster_id", "clusters"
				if tool == "k8s_nodepool_upgrade" {
					id, arg, group = upgradeParityPool, "nodepool_id", "nodePools"
				}
				old := w.rt.extra
				posts := 0
				w.rt.extra = func(req *http.Request) (*http.Response, error) {
					if req.Method == http.MethodPost {
						if req.URL.Host != "mk8s.api.cloud.ru" || req.URL.Path != "/v3/"+group+"/"+id+":upgrade" || req.URL.Query().Get("project_id") != pvProject {
							t.Fatalf("unexpected submission: %s", req.URL)
						}
						posts++
						return contractRT{status: status, body: `{"message":"upgrade refused"}`}.RoundTrip(req)
					}
					return old(req)
				}
				first := processUpgradeStdio(t, newROServers(t).plain)
				second := processUpgradeStdio(t, newROServers(t).plain)
				call := func(client func(map[string]any) roAnswer, approval string) map[string]any {
					args := map[string]any{arg: id, "version": upgradeTo}
					if approval != "" {
						args["confirmed"], args["preview_id"] = true, approval
					}
					response := client(map[string]any{"name": tool, "arguments": args})
					return parseObject(response.text)
				}
				offer := func(client func(map[string]any) roAnswer) string {
					answer := call(client, "")
					id, _ := answer["preview_id"].(string)
					if answer["status"] != "confirmation_required" || id == "" {
						t.Fatalf("preview: %v", answer)
					}
					return id
				}
				a, b := offer(first), offer(second)
				answer := call(first, a)
				if posts != 1 {
					t.Fatalf("first submission: posts=%d answer=%v", posts, answer)
				}
				if status == 408 || status >= 500 {
					if answer["status"] != "outcome_unknown" || call(second, b)["status"] != "outcome_unknown" || posts != 1 {
						t.Fatalf("ambiguous submission replayed or lost: posts=%d answer=%v", posts, answer)
					}
					return
				}
				if answer["status"] != "rejected" {
					t.Fatalf("definitive HTTP %d classified as %v", status, answer)
				}
				for _, stale := range []map[string]any{call(first, a), call(second, b)} {
					if stale["status"] != "confirmation_required" || posts != 1 {
						t.Fatalf("old consent reused: posts=%d answer=%v", posts, stale)
					}
				}
				if answer := call(second, offer(second)); posts != 2 || answer["status"] != "rejected" {
					t.Fatalf("new approved attempt: posts=%d answer=%v", posts, answer)
				}
			})
		}
	}
}

func TestUpgradeLegacyUncertainReceipt(t *testing.T) {
	w, _ := withJSONWorld(t)
	dir := filepath.Join(w.dir, "k8s-upgrades")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := sha256.Sum256([]byte(pvProject + "\x00k8s_upgrade\x00" + upgradeParityCluster))
	// The pre-fix format cannot distinguish rejection from a lost reply.
	legacy := map[string]string{"Project": pvProject, "Subject": "parity-test-actor", "Tool": "k8s_upgrade", "Resource": upgradeParityCluster, "From": upgradeFrom, "Target": upgradeTo, "Operation": ""}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, fmt.Sprintf("%x.json", key))
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	r := upgradeResource{tool: "k8s_upgrade", verb: "upgrade", argument: "cluster_id", id: upgradeParityCluster}
	for _, surface := range []string{"stdio", "CLI"} {
		text, failed := r.call(t, surface, upgradeTo, true)
		if !failed || len(w.rt.takeWrites()) != 0 {
			t.Fatal(surface, text)
		}
		if surface == "stdio" {
			answer := parseObject(text)
			args, _ := answer["next_args"].(map[string]any)
			if answer["status"] != "outcome_unknown" || answer["next_tool"] != "k8s_get" || args["cluster_id"] != r.id || len(args) != 1 {
				t.Fatal(text)
			}
		}
	}
	saved, err := os.ReadFile(path)
	if err != nil || string(saved) != string(data) {
		t.Fatal("legacy receipt changed", err)
	}
}

func TestUpgradeCLIRecoveryAndReturnedIDs(t *testing.T) {
	for _, surface := range []string{"CLI", "stdio"} {
		t.Run(surface, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			r := upgradeResource{tool: "k8s_nodepool_upgrade", verb: "nodepool-upgrade", argument: "nodepool_id", id: upgradeParityPool}
			old := w.rt.extra
			posts := 0
			w.rt.extra = func(req *http.Request) (*http.Response, error) {
				if req.Method == http.MethodPost && req.URL.Path == "/v3/nodePools/"+r.id+":upgrade" {
					posts++
					if posts == 1 {
						return contractRT{status: 409, body: `{"message":"another upgrade is in progress"}`}.RoundTrip(req)
					}
				}
				return old(req)
			}
			if surface == "stdio" {
				r.call(t, surface, upgradeTo, false)
			}
			text, failed := r.call(t, surface, upgradeTo, true)
			if !failed || posts != 1 {
				t.Fatal(posts, text)
			}
			if surface == "stdio" {
				r.call(t, surface, upgradeTo, false)
			}
			text, failed = r.call(t, surface, upgradeTo, true)
			answer := parseObject(text)
			marks, _ := answer["_untrusted"].([]any)
			if failed || posts != 2 || answer["operation_id"] != upgradePoolOp || answer["cluster_id"] != upgradeParityParent || !slices.Contains(marks, "operation_id") || !slices.Contains(marks, "cluster_id") || !slices.Contains(marks, "next_args") {
				t.Fatal(posts, text)
			}
		})
	}
}

func TestUpgradeRefusalHistory(t *testing.T) {
	w, _ := withJSONWorld(t)
	old := w.rt.extra
	posts := 0
	w.rt.extra = func(req *http.Request) (*http.Response, error) {
		if req.Method == http.MethodPost && req.URL.Path == "/v3/clusters/"+upgradeParityCluster+":upgrade" {
			posts++
			return contractRT{status: 409, body: `{"message":"resource busy"}`}.RoundTrip(req)
		}
		return old(req)
	}
	r := upgradeResource{tool: "k8s_upgrade", argument: "cluster_id", id: upgradeParityCluster}
	r.call(t, "stdio", upgradeTo, false)
	oldID := r.previewID
	r.call(t, "stdio", upgradeTo, true)
	r.previewID = oldID
	text, _ := r.call(t, "stdio", upgradeTo, true)
	if parseObject(text)["status"] != "confirmation_required" || posts != 1 {
		t.Fatal(posts, text)
	}
	code, out, stderr := runCLI(t, "", "history", "--json")
	var h struct {
		Lines []struct{ Tool, Outcome, Reason string }
		Live  []json.RawMessage
	}
	if code != 0 || json.Unmarshal([]byte(out), &h) != nil || len(h.Lines) != 1 || len(h.Live) != 0 || h.Lines[0].Tool != "k8s_upgrade" || h.Lines[0].Outcome != "error" || h.Lines[0].Reason != "API_ERROR" {
		t.Fatal(code, out, stderr)
	}
}

// Run this filesystem fault injection as an unprivileged Unix user. A
// pre-existing writable lock file ensures the failure is at reservation
// publication or offer unlink, after reading and matching the visible offer.
func TestUpgradeLocalFailureBeforeSubmission(t *testing.T) {
	for _, resource := range []upgradeResource{
		{tool: "k8s_upgrade", verb: "upgrade", argument: "cluster_id", id: upgradeParityCluster},
		{tool: "k8s_nodepool_upgrade", verb: "nodepool-upgrade", argument: "nodepool_id", id: upgradeParityPool},
	} {
		for _, fault := range []string{"reservation", "offer"} {
			t.Run(resource.tool+"/"+fault, func(t *testing.T) {
				w, _ := withJSONWorld(t)
				r := resource
				text, _ := r.call(t, "stdio", upgradeTo, false)
				if r.previewID == "" || parseObject(text)["status"] != "confirmation_required" {
					t.Fatal(text)
				}
				dir := filepath.Join(w.dir, "k8s-upgrades")
				lock := filepath.Join(dir, fmt.Sprintf("%x.lock", sha256.Sum256([]byte(pvProject))))
				f, err := os.OpenFile(lock, os.O_CREATE|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.Close(); err != nil {
					t.Fatal(err)
				}
				if fault == "offer" {
					dir = filepath.Join(dir, "previews")
				}
				if err := os.Chmod(dir, 0500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { os.Chmod(dir, 0700) })
				probe, err := os.CreateTemp(dir, "probe")
				if err == nil {
					probe.Close()
					os.Remove(probe.Name())
					t.Skip("directory permissions are not enforced for this identity; run unprivileged")
				}
				if !os.IsPermission(err) {
					t.Fatalf("fault must be permission denied, got %v", err)
				}
				old := w.rt.extra
				posts := 0
				w.rt.extra = func(req *http.Request) (*http.Response, error) {
					if req.Method == http.MethodPost {
						posts++
					}
					return old(req)
				}
				text, failed := r.call(t, "stdio", upgradeTo, true)
				if !failed || posts != 0 {
					t.Fatal(posts, text)
				}
				if err := os.Chmod(dir, 0700); err != nil {
					t.Fatal(err)
				}
				code, out, stderr := runCLI(t, "", "history", "--json")
				var h struct{ Lines, Live []json.RawMessage }
				if code != 0 || json.Unmarshal([]byte(out), &h) != nil || len(h.Lines) != 0 || len(h.Live) != 0 {
					t.Errorf("history after unsent change: %s %s", out, stderr)
				}
				// A fresh connection and a newly approved preview can submit once.
				r.stdio = nil
				r.previewID = ""
				text, _ = r.call(t, "stdio", upgradeTo, false)
				if parseObject(text)["status"] != "confirmation_required" || r.previewID == "" || posts != 0 {
					t.Fatal("reservation blocked fresh preview", posts, text)
				}
				text, failed = r.call(t, "stdio", upgradeTo, true)
				if failed || posts != 1 || parseObject(text)["status"] != "submitted" {
					t.Fatal(posts, text)
				}
			})
		}
	}
}
