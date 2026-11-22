// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net/http"
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
