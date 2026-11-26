// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"io"
	"net/http"
	"testing"
)

// A protobuf collection omitted from an object defaults to empty. A null
// collection, row or continuation is not evidence that the list is empty.
// Exercise the public tools so an apparent success or a different error code
// cannot hide a broken page. Field and token names vary between providers.
func TestListToolsDistinguishEmptyAndMalformedPages(t *testing.T) {
	for _, service := range []struct {
		tool, endpoint, field, token, result string
		args                                 map[string]any
	}{
		{"kafka_list", "kafka.api.cloud.ru/v1/clusters", "clusters", "next_page_token", "clusters", nil},
		{"redis_list", "redis.api.cloud.ru/v1/clusters", "clusters", "next_page_token", "clusters", nil},
		{"pg_list", "postgresql.api.cloud.ru/v1/clusters", "clusters", "next_page_token", "clusters", nil},
		{"lb_list", "nlb.api.cloud.ru/v2/networkLoadBalancers", "balancers", "next_page_token", "load_balancers", nil},
		{"vpc_list", "vpc.api.cloud.ru/v1/vpcs", "vpcs", "nextPageToken", "vpcs", nil},
		{"dns_zones", "dns.api.cloud.ru/v1/privateZones", "zones", "nextPageToken", "zones", nil},
		{"k8s_list", "mk8s.api.cloud.ru/v3/clusters", "clusters", "nextPageToken", "clusters", nil},
		{"ca_list", "containers.api.cloud.ru/v2/containers", "data", "nextPageToken", "containers", nil},
		{"ar_list", "ar.api.cloud.ru/v1/registries", "registries", "nextPageToken", "registries", nil},
		{"cert_list", "certificatemanager.api.cloud.ru/v1/certificate", "certificates", "", "count", nil},
		{"iam_service_accounts", "iam.api.cloud.ru/api/v2/serviceAccounts", "service_accounts", "next_page_token", "service_accounts", nil},
		{"org_projects", "organization.api.cloud.ru/v3/projects", "projects", "next_page_token", "projects", nil},
		{"billing_consumption", "organization.api.cloud.ru/v2/consumption", "consumptions", "next_page_token", "records", map[string]any{"start": "2026-01-01"}},
	} {
		type reply struct {
			name, body string
			status     int
			valid      bool
		}
		array := func(value string) string { return fmt.Sprintf("{%q:%s}", service.field, value) }
		cases := []reply{
			{"omitted collection", "{}", 200, true},
			{"empty collection", array("[]"), 200, true},
			{"empty body", "", 200, false},
			{"no content", "", 204, false},
			{"partial content", array("[]"), 206, false},
			{"null object", "null", 200, false},
			{"null collection", array("null"), 200, false},
			{"object as collection", array("{}"), 200, false},
			{"null row", array("[null]"), 200, false},
		}
		if service.token != "" {
			page := func(token string) string { return fmt.Sprintf("{%q:[],%q:%s}", service.field, service.token, token) }
			cases = append(cases,
				reply{"empty token", page(`""`), 200, true},
				reply{"null token", page("null"), 200, false},
				reply{"number as token", page("7"), 200, false},
			)
		}
		for _, tc := range cases {
			t.Run(service.tool+"/"+tc.name, func(t *testing.T) {
				fake := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
					if service.tool == "dns_zones" && r.Method == http.MethodGet && r.URL.Path == "/v1/publicZones" {
						_, _ = io.WriteString(w, `{"zones":[]}`)
						return true
					}
					if r.Method != http.MethodGet || r.URL.Host+r.URL.Path != service.endpoint {
						t.Fatalf("unexpected %s %s", r.Method, r.URL)
					}
					w.WriteHeader(tc.status)
					_, _ = io.WriteString(w, tc.body)
					return true
				}}
				result, raw, failed := callNamed(t, apiFakeRegistry(t, fake), service.tool, service.args)
				expectedRequests := 1
				if service.tool == "dns_zones" {
					expectedRequests++
				}
				if fake.count() != expectedRequests {
					t.Fatalf("made %d requests, want %d: %s", fake.count(), expectedRequests, raw)
				}
				if !tc.valid {
					if !failed || result["reason"] != "API_ERROR" || result[service.result] != nil {
						t.Fatalf("invalid page returned %s", raw)
					}
					return
				}
				if failed {
					t.Fatalf("valid empty page refused: %s", raw)
				}
				switch rows := result[service.result].(type) {
				case []any:
					if len(rows) != 0 {
						t.Fatalf("unexpected rows: %s", raw)
					}
				case float64:
					if rows != 0 {
						t.Fatalf("unexpected count: %s", raw)
					}
				default:
					t.Fatalf("missing empty collection or count: %s", raw)
				}
			})
		}
	}
}
