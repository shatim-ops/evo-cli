// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// Lists read every page (#98). Page fields and token names are those of
// the specifications (scripts/cloudru-specs.py) and, for service
// accounts, of the register (IAM-2: service_accounts / next_page_token
// live, over the camelCase of the spec). The UUIDs are made up.

const (
	pagesID1 = "00000000-0000-4000-8000-000000000001"
	pagesID2 = "00000000-0000-4000-8000-000000000002"
)

// specPage answers a list whose OpenAPI response carries a next-page token.
// queryName is the parameter the spec says to send back (page_token or
// pageToken); the other spelling is refused, as the audit API refused a
// parameter it did not know (AUD-2).
func specPage(w http.ResponseWriter, r *http.Request, queryName, tokenKey, field, item string) {
	other := map[string]string{"page_token": "pageToken", "pageToken": "page_token"}[queryName]
	if r.URL.Query().Has(other) {
		http.Error(w, `{"message":"unknown parameter `+other+`"}`, http.StatusUnsupportedMediaType)
		return
	}
	extra := ""
	if item != "" {
		extra = "," + item
	}
	if r.URL.Query().Get(queryName) == "" {
		_, _ = io.WriteString(w, `{`+field+`:[{"id":"`+pagesID1+`","name":"one","status":"READY"`+extra+`}]`+
			`,"`+tokenKey+`":"p2"}`)
		return
	}
	_, _ = io.WriteString(w, `{`+field+`:[{"id":"`+pagesID2+`","name":"page-two","status":"READY"`+extra+`}]}`)
}

// TestPgCreate_TakenNameOnLaterPage: openapi-postgresql.yaml
// GET /v1/clusters is {clusters, next_page_token}, query page_token
// (default page size 100). The cluster named "page-two" is only on page
// 2. pg_create must refuse it (USAGE, nothing created): the name check
// read one page and POST /v1/clusters still went out (#98, B-1).
func TestPgCreate_TakenNameOnLaterPage(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "postgresql.api.cloud.ru" {
			return false
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/clusters":
			specPage(w, r, "page_token", "next_page_token", `"clusters"`, "")
		case r.Method == http.MethodGet && r.URL.Path == "/v1/versions":
			_, _ = io.WriteString(w, `{"versions":[{"id":"v17","name":"17"}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/specifications":
			_, _ = io.WriteString(w, `{"specifications":[{"id":"std","display_name":"std","cpu":2,"memory":4,"deployment_mode":"STANDARD","min_storage_gb":20,"max_hosts":2}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/clusters":
			_, _ = io.WriteString(w, `{"id":"op-1","resource_id":"`+pagesID2+`","done":false}`)
		default:
			http.Error(w, `{"message":"no such path"}`, 404)
		}
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "pg_create", map[string]any{
		"name": "page-two", "specification_id": "std", "confirmed": true,
	})
	if !isErr || m["reason"] != "USAGE" || m["existing_id"] != pagesID2 {
		t.Fatalf("name on page 2 was not refused: %s", text)
	}
	if body, ok := f.posted("postgresql.api.cloud.ru"); ok {
		t.Fatalf("POST /v1/clusters sent for a name that already exists on page 2: %s", body)
	}
}

// TestArCreate_TakenNameOnLaterPage: ar/openapi.yaml
// GET /v1/registries is {registries, nextPageToken}, query pageToken.
// ar_create must not POST a second billable registry (#98, B-2).
func TestArCreate_TakenNameOnLaterPage(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "ar.api.cloud.ru" {
			return false
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/registries" {
			specPage(w, r, "pageToken", "nextPageToken", `"registries"`, `"registryType":"DOCKER"`)
			return true
		}
		_, _ = io.WriteString(w, `{"id":"op-1","resourceId":"`+pagesID2+`","done":false}`)
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "ar_create", map[string]any{"name": "page-two", "confirmed": true})
	if !isErr || m["reason"] != "USAGE" || m["existing_id"] != pagesID2 {
		t.Fatalf("name on page 2 was not refused: %s", text)
	}
	if body, ok := f.posted("ar.api.cloud.ru"); ok {
		t.Fatalf("POST /v1/registries sent for a name that already exists on page 2: %s", body)
	}
}

// TestLists_FollowNextPage: list tools whose spec (or the live register)
// returns a next-page token answer every page, not only the first: the
// second page's id was absent, so anything done from that list missed a
// real resource (#98, B-3).
func TestLists_FollowNextPage(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Host + " " + r.URL.Path {
		case "nlb.api.cloud.ru /v2/networkLoadBalancers":
			specPage(w, r, "page_token", "next_page_token", `"balancers"`, "")
		case "nlb.api.cloud.ru /v2/targetGroups":
			specPage(w, r, "page_token", "next_page_token", `"target_groups"`, "")
		case "vpc.api.cloud.ru /v1/vpcs":
			specPage(w, r, "pageToken", "nextPageToken", `"vpcs"`, "")
		case "vpc.api.cloud.ru /v1/vpcs/" + pagesID1 + "/routes/static":
			specPage(w, r, "pageToken", "nextPageToken", `"routes"`, `"subnet":"10.0.0.0/24"`)
		case "dns.api.cloud.ru /v1/publicZones", "dns.api.cloud.ru /v1/privateZones":
			specPage(w, r, "pageToken", "nextPageToken", `"zones"`, `"domain":"example.test"`)
		case "dns.api.cloud.ru /v1/publicRecordsSole":
			specPage(w, r, "pageToken", "nextPageToken", `"records"`, `"type":"A","values":["192.0.2.1"]`)
		case "mk8s.api.cloud.ru /v3/clusters":
			specPage(w, r, "pageToken", "nextPageToken", `"clusters"`, `"createdAt":"2026-10-18T00:00:00Z"`)
		case "containers.api.cloud.ru /v2/containers":
			specPage(w, r, "pageToken", "nextPageToken", `"data"`, "")
		case "postgresql.api.cloud.ru /v1/clusters":
			specPage(w, r, "page_token", "next_page_token", `"clusters"`, "")
		case "ar.api.cloud.ru /v1/registries":
			specPage(w, r, "pageToken", "nextPageToken", `"registries"`, "")
		case "organization.api.cloud.ru /v3/projects":
			specPage(w, r, "page_token", "next_page_token", `"projects"`, `"organization_unit_id":"ou"`)
		case "organization.api.cloud.ru /v3/quotas/usages":
			// The id of the page goes in resource_type_code: org_quotas
			// answers only resource, limit and usage.
			if r.URL.Query().Has("pageToken") {
				http.Error(w, `{"message":"unknown parameter pageToken"}`, http.StatusUnsupportedMediaType)
				return true
			}
			if r.URL.Query().Get("page_token") == "" {
				_, _ = io.WriteString(w, `{"usages":[{"resource_type_code":"`+pagesID1+`","individual_limit":1,"individual_usage":0}],"next_page_token":"p2"}`)
				return true
			}
			_, _ = io.WriteString(w, `{"usages":[{"resource_type_code":"`+pagesID2+`","individual_limit":1,"individual_usage":0}]}`)
		case "iam.api.cloud.ru /api/v2/serviceAccounts":
			// IAM-2: live body is snake_case. Spec query for the next page is pageToken.
			if r.URL.Query().Has("page_token") {
				http.Error(w, `{"message":"unknown parameter page_token"}`, http.StatusUnsupportedMediaType)
				return true
			}
			if r.URL.Query().Get("pageToken") == "" {
				_, _ = io.WriteString(w, `{"service_accounts":[{"id":"`+pagesID1+`","name":"one","email":"a@example.test","type":"service","enabled":true}],"next_page_token":"p2"}`)
				return true
			}
			_, _ = io.WriteString(w, `{"service_accounts":[{"id":"`+pagesID2+`","name":"page-two","email":"b@example.test","type":"service","enabled":true}]}`)
		default:
			return false
		}
		return true
	}}
	r := apiFakeRegistry(t, f)
	zone := pagesID1
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"lb_list", nil},
		{"tg_list", nil},
		{"vpc_list", nil},
		{"vpc_routes", map[string]any{"vpc_id": zone}},
		{"dns_zones", nil},
		{"dns_records", map[string]any{"zone_id": zone}},
		{"k8s_list", nil},
		{"ca_list", nil},
		{"pg_list", nil},
		{"ar_list", nil},
		{"org_projects", nil},
		{"org_quotas", nil},
		{"iam_service_accounts", nil},
	}
	for _, c := range cases {
		_, text, isErr := callNamed(t, r, c.tool, c.args)
		if isErr {
			t.Errorf("%s: %s", c.tool, text)
			continue
		}
		if !strings.Contains(text, pagesID2) {
			t.Errorf("%s dropped the page after the next-page token: %s", c.tool, text)
		}
	}
}
