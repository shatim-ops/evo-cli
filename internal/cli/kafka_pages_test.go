// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"net/http"
	"strings"
	"testing"
)

func TestKafkaPagesCLI(t *testing.T) {
	const takenPage = `{"clusters":[{"id":"cluster-2","name":"on-page-two","status":"READY"}]}`
	for _, tc := range []struct {
		name       string
		args       []string
		pageStatus int
		pageBody   string
		want       string
		success    bool
	}{
		{"list", []string{"list"}, 200, takenPage, "on-page-two", true},
		{"free name", []string{"create", "free", "--cpu", "2", "--ram", "4", "--dry-run"}, 200, "{}", "will_create", true},
		{"taken name", []string{"create", "on-page-two", "--yes"}, 200, takenPage, "existing_id", false},
		{"null list page", []string{"list"}, 200, "null", "API_ERROR", false},
		{"null page", []string{"create", "free", "--cpu", "2", "--ram", "4", "--yes"}, 200, "null", "API_ERROR", false},
		{"page error", []string{"create", "free", "--yes"}, 400, `{"message":"page unavailable"}`, "page unavailable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := withParityWorld(t)
			var pages int
			w.rt.extra = func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "kafka.api.cloud.ru" || r.URL.Path != "/v1/clusters" || r.Method != http.MethodGet {
					return nil, nil
				}
				pages++
				q := r.URL.Query()
				if q.Get("project_id") != pvProject || q.Get("product_type") != "kafka" || q.Get("page_size") != "50" {
					t.Errorf("unexpected query: %s", r.URL.RawQuery)
				}
				if pages == 1 && !q.Has("page_token") {
					return contractRT{status: 200, body: `{"next_page_token":"cursor+2"}`}.RoundTrip(r)
				}
				if pages != 2 || q.Get("page_token") != "cursor+2" {
					t.Errorf("unexpected page %d: %s", pages, r.URL.RawQuery)
				}
				return contractRT{status: tc.pageStatus, body: tc.pageBody}.RoundTrip(r)
			}
			newProcess()
			code, out, stderr := runCLI(t, "", append([]string{"--json", "kafka"}, tc.args...)...)
			if writes := w.rt.takeWrites(); len(writes) != 0 {
				t.Errorf("unexpected writes: %v", writes)
			}
			if tc.pageBody == "null" && code != 5 {
				t.Errorf("incomplete list: exit %d, want 5", code)
			}
			if (code == 0) != tc.success || !strings.Contains(out+stderr, tc.want) || pages != 2 {
				t.Fatalf("exit %d, pages %d: %s %s", code, pages, out, stderr)
			}
		})
	}
}
