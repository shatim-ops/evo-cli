// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/provision"
	"github.com/shatim-ops/evo-cli/internal/state"
)

// Compute accepted the one-rule DELETE. Its identity in the full list,
// including while deleting, determines whether the receipt can be removed.
func TestClose_ConfirmExactRuleAfterAcceptedDelete(t *testing.T) {
	for _, scenario := range []string{"visible", "read_failed", "removed"} {
		t.Run(scenario, func(t *testing.T) {
			const path = "/api/v1/security-groups/group/rules"
			visible, refused := scenario != "removed", scenario == "read_failed"
			deletes, reads := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "DELETE" && r.URL.Path == path+"/owned":
					deletes++
					w.WriteHeader(http.StatusAccepted)
				case r.Method == "GET" && r.URL.Path == path:
					reads++
					if refused {
						w.WriteHeader(http.StatusForbidden)
						return
					}
					items := []map[string]string{{"id": "other", "port_range": "80:80"}}
					if visible {
						items = append(items, map[string]string{"id": "owned", "state": "deleting"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"items": items, "total": len(items)})
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(405)
				}
			}))
			defer srv.Close()
			cloud := provision.NewCloudRuAdapter(srv.Client(), srv.URL)
			cloud.Backoff = time.Millisecond
			sf := &state.File{Deployments: []state.DeploymentRecord{{ID: "vm", OpenedRules: []state.SGRule{{ID: "owned", SecurityGroupID: "group", Port: "80/tcp"}}}}}
			got := Close(t.Context(), cloud, sf, "vm")
			Retire(sf, "vm")
			if scenario == "removed" {
				if len(got.Closed) != 1 || len(got.Failed) != 0 || sf.FindDeployment("vm") != nil || reads == 0 || deletes != 1 {
					t.Fatalf("confirmed removal: closed=%v failed=%v reads=%d DELETE=%d", got.Closed, got.Failed, reads, deletes)
				}
				return
			}
			rec := sf.FindDeployment("vm")
			if len(got.Closed) != 0 || len(got.Failed) != 1 || rec == nil || len(rec.OpenedRules) != 1 || rec.OpenedRules[0].ID != "owned" || rec.OpenedRules[0].DeleteError == "" || reads == 0 {
				t.Fatalf("unconfirmed after 202: closed=%v failed=%v receipt=%+v reads=%d", got.Closed, got.Failed, rec, reads)
			}
			refused, visible = false, false
			got = Close(t.Context(), cloud, sf, "vm")
			Retire(sf, "vm")
			if len(got.Closed) != 1 || len(got.Failed) != 0 || sf.FindDeployment("vm") != nil || deletes != 1 {
				t.Fatalf("retry after disappearance: closed=%v failed=%v DELETE=%d", got.Closed, got.Failed, deletes)
			}
		})
	}
}
