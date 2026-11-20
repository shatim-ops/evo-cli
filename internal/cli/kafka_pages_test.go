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
	for _, tc := range []struct {
		name       string
		args       []string
		pageStatus int
		want       string
		success    bool
	}{
		{"list", []string{"list"}, 200, "on-page-two", true},
		{"free name", []string{"create", "free", "--cpu", "2", "--ram", "4", "--dry-run"}, 200, "will_create", true},
		{"taken name", []string{"create", "on-page-two", "--yes"}, 200, "existing_id", false},
		{"page error", []string{"create", "free", "--yes"}, 400, "page unavailable", false},
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
					return contractRT{status: 200, body: `{"clusters":[],"next_page_token":"cursor+2"}`}.RoundTrip(r)
				}
				if pages != 2 || q.Get("page_token") != "cursor+2" {
					t.Errorf("unexpected page %d: %s", pages, r.URL.RawQuery)
				}
				body := `{"clusters":[{"id":"cluster-2","name":"on-page-two","status":"READY"}]}`
				if tc.pageStatus != 200 {
					body = `{"message":"page unavailable"}`
				}
				return contractRT{status: tc.pageStatus, body: body}.RoundTrip(r)
			}
			newProcess()
			code, out, stderr := runCLI(t, "", append([]string{"--json", "kafka"}, tc.args...)...)
			if (code == 0) != tc.success || !strings.Contains(out+stderr, tc.want) || pages != 2 {
				t.Fatalf("exit %d, pages %d: %s %s", code, pages, out, stderr)
			}
			if writes := w.rt.takeWrites(); len(writes) != 0 {
				t.Fatalf("unexpected writes: %v", writes)
			}
		})
	}
}
