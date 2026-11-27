// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"net/http"
	"testing"
)

// The legacy CLI alias returns its local active project even if listing
// fails midstream. Scripts distinguish an unavailable list from an empty array.
func TestProjectList_OrganizationFallbackAcrossSurfaces(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		secondPage bool
		mcpError   bool
	}{
		{"service unavailable", 503, false, true},
		{"first page forbidden", 403, false, false},
		{"later page forbidden", 403, true, true},
		{"empty cloud list", 200, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := withParityWorld(t)
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/v3/projects" {
					return nil, nil
				}
				if tc.secondPage && r.URL.Query().Get("page_token") == "" {
					return contractRT{status: 200, body: `{"projects":[{"id":"seen-on-first-page"}],"next_page_token":"next"}`}.RoundTrip(r)
				}
				return contractRT{status: tc.status, body: `{"projects":[]}`}.RoundTrip(r)
			}
			checkList := func(surface string, obj map[string]any) {
				t.Helper()
				projects, has := obj["projects"]
				if !has || obj["active"] != pvProject {
					t.Fatalf("%s: missing local scope or projects: %v", surface, obj)
				}
				if tc.status == http.StatusOK {
					if list, ok := projects.([]any); !ok || len(list) != 0 {
						t.Errorf("%s: expected empty cloud list: %v", surface, obj)
					}
				} else if note, _ := obj["note"].(string); projects != nil || note == "" {
					t.Errorf("%s: expected null and note: %v", surface, obj)
				}
			}
			code, stdout, stderr := runCLI(t, "", "project", "list", "--json")
			if code != 0 {
				t.Fatalf("legacy exit %d: %s", code, stderr)
			}
			checkList("project list", parseObject(stdout))
			s := parityServers(t)
			for surface, obj := range map[string]map[string]any{
				"stdio": stdioDetails(t, s.stdio, "org_projects", map[string]any{}),
				"http":  httpDetails(t, s.http, "org_projects", map[string]any{}),
			} {
				if tc.mcpError {
					if obj["reason"] != "API_ERROR" || obj["active"] != nil {
						t.Errorf("%s: expected API_ERROR: %v", surface, obj)
					}
				} else {
					checkList(surface, obj)
				}
			}
		})
	}
}
