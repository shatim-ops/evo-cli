// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func TestDiskDeleteProjectOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, listOwner, cardOwner string
		allowed                    bool
	}{
		{"foreign list row", parityNIC, pvProject, false},
		{"foreign card", pvProject, parityNIC, false},
		{"both foreign", parityNIC, parityNIC, false},
		{"own", pvProject, pvProject, true},
		{"owner omitted", "", "", true},
	} {
		for _, confirmed := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/confirmed=%t", tc.name, confirmed), func(t *testing.T) {
				withParityWorld(t)
				writes := 0
				card := func(owner string) string {
					project := ""
					if owner != "" {
						project = fmt.Sprintf(`,"project_id":%q`, owner)
					}
					return fmt.Sprintf(`{"id":%q,"vms":[]%s}`, parityVM, project)
				}
				rt := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					switch {
					case r.Method == "GET" && r.URL.Path == "/api/v1/disks" && r.URL.Query().Get("project_id") == pvProject:
						return contractRT{status: 200, body: `{"items":[` + card(tc.listOwner) + `],"total":1}`}.RoundTrip(r)
					case r.Method == "GET" && r.URL.Path == "/api/v1/disks/"+parityVM:
						return contractRT{status: 200, body: card(tc.cardOwner)}.RoundTrip(r)
					case r.Method == "DELETE" && r.URL.Path == "/api/v1/disks/"+parityVM:
						writes++
						return contractRT{status: 204}.RoundTrip(r)
					default:
						t.Fatalf("unexpected %s %s", r.Method, r.URL)
						return nil, nil
					}
				})
				factory := toolEngine
				a := provision.NewCloudRuAdapter(&http.Client{Transport: rt}, "")
				toolEngine = func(o map[string]any) (*engine.Engine, error) {
					e, err := factory(o)
					if err == nil {
						e.Cloud = a
					}
					return e, err
				}
				t.Cleanup(func() { toolEngine = factory })
				servers := newROServers(t)
				servers.plain.SetProjectBound()
				ans, _ := servers.stdio(t, servers.plain, "tools/call", map[string]any{"name": "disk_delete", "arguments": map[string]any{"disk_id": parityVM, "confirmed": confirmed}})
				m := parseObject(ans.text)
				if !tc.allowed {
					if !ans.isErr || m["reason"] != "NOT_FOUND" || writes != 0 || strings.Contains(ans.text, parityNIC) {
						t.Errorf("foreign disk: writes=%d answer=%s", writes, ans.text)
					}
				} else if confirmed {
					if ans.isErr || m["status"] != "deleted" || writes != 1 {
						t.Errorf("own disk: writes=%d answer=%s", writes, ans.text)
					}
				} else if m["status"] != "confirmation_required" || writes != 0 {
					t.Errorf("preview: writes=%d answer=%s", writes, ans.text)
				}
			})
		}
	}
}
