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

// Имена и поля страниц — из скачанных спецификаций (.cache/cloudru-specs)
// и, для сервисных аккаунтов, из реестра IAM-2 (service_accounts /
// next_page_token важнее camelCase спецификации). UUID выдуманы.

const (
	breakPage1 = "00000000-0000-4000-8000-000000000001"
	breakPage2 = "00000000-0000-4000-8000-000000000002"
)

// specPage answers a list whose OpenAPI response carries a next-page token.
// queryName is the parameter the spec says to send back (page_token or pageToken).
func specPage(w http.ResponseWriter, r *http.Request, queryName, tokenKey, field, item string) {
	extra := ""
	if item != "" {
		extra = "," + item
	}
	if r.URL.Query().Get(queryName) == "" {
		_, _ = io.WriteString(w, `{`+field+`:[{"id":"`+breakPage1+`","name":"one","status":"READY"`+extra+`}]`+
			`,"`+tokenKey+`":"p2"}`)
		return
	}
	_, _ = io.WriteString(w, `{`+field+`:[{"id":"`+breakPage2+`","name":"page-two","status":"READY"`+extra+`}]}`)
}

// TestBreak_PgCreate_TakenNameOnNextPage: openapi-postgresql.yaml
// GET /v1/clusters is {clusters, next_page_token}, query page_token
// (default page size 100). The cluster named "db" is only on page 2.
// pg_create must refuse it (USAGE, nothing created). On main the name
// check reads one page and POST /v1/clusters still goes out.
func TestBreak_PgCreate_TakenNameOnNextPage(t *testing.T) {
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
			_, _ = io.WriteString(w, `{"id":"op-1","resource_id":"`+breakPage2+`","done":false}`)
		default:
			http.Error(w, `{"message":"no such path"}`, 404)
		}
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "pg_create", map[string]any{
		"name": "page-two", "specification_id": "std", "confirmed": true,
	})
	if !isErr || m["reason"] != "USAGE" || m["existing_id"] != breakPage2 {
		t.Fatalf("name on page 2 was not refused: %s", text)
	}
	if body, ok := f.posted("postgresql.api.cloud.ru"); ok {
		t.Fatalf("POST /v1/clusters sent for a name that already exists on page 2: %s", body)
	}
}

// TestBreak_ArCreate_TakenNameOnNextPage: ar/openapi.yaml
// GET /v1/registries is {registries, nextPageToken}, query pageToken.
// ar_create must not POST a second billable registry.
func TestBreak_ArCreate_TakenNameOnNextPage(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host != "ar.api.cloud.ru" {
			return false
		}
		if r.Method == http.MethodGet && r.URL.Path == "/v1/registries" {
			specPage(w, r, "pageToken", "nextPageToken", `"registries"`, `"registryType":"DOCKER"`)
			return true
		}
		_, _ = io.WriteString(w, `{"id":"op-1","resourceId":"`+breakPage2+`","done":false}`)
		return true
	}}
	r := apiFakeRegistry(t, f)
	m, text, isErr := callNamed(t, r, "ar_create", map[string]any{"name": "page-two", "confirmed": true})
	if !isErr || m["reason"] != "USAGE" || m["existing_id"] != breakPage2 {
		t.Fatalf("name on page 2 was not refused: %s", text)
	}
	if body, ok := f.posted("ar.api.cloud.ru"); ok {
		t.Fatalf("POST /v1/registries sent for a name that already exists on page 2: %s", body)
	}
}

// TestBreak_List_DropsNextPage: list tools whose spec (or the live register)
// returns a next-page token answer only the first page. The second page's id
// is absent, so anything the user does from that list misses a real resource.
func TestBreak_List_DropsNextPage(t *testing.T) {
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		switch r.URL.Host + " " + r.URL.Path {
		case "nlb.api.cloud.ru /v2/networkLoadBalancers":
			specPage(w, r, "page_token", "next_page_token", `"balancers"`, "")
		case "nlb.api.cloud.ru /v2/targetGroups":
			specPage(w, r, "page_token", "next_page_token", `"target_groups"`, "")
		case "vpc.api.cloud.ru /v1/vpcs":
			specPage(w, r, "pageToken", "nextPageToken", `"vpcs"`, "")
		case "vpc.api.cloud.ru /v1/vpcs/" + breakPage1 + "/routes/static":
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
		case "iam.api.cloud.ru /api/v2/serviceAccounts":
			// IAM-2: live body is snake_case. Spec query for the next page is pageToken.
			if r.URL.Query().Get("pageToken") == "" && r.URL.Query().Get("page_token") == "" {
				_, _ = io.WriteString(w, `{"service_accounts":[{"id":"`+breakPage1+`","name":"one","email":"a@example.test","type":"service","enabled":true}],"next_page_token":"p2"}`)
				return true
			}
			_, _ = io.WriteString(w, `{"service_accounts":[{"id":"`+breakPage2+`","name":"page-two","email":"b@example.test","type":"service","enabled":true}]}`)
		default:
			return false
		}
		return true
	}}
	r := apiFakeRegistry(t, f)
	zone := breakPage1
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
		{"iam_service_accounts", nil},
	}
	for _, c := range cases {
		m, text, isErr := callNamed(t, r, c.tool, c.args)
		if isErr {
			t.Errorf("%s: %s", c.tool, text)
			continue
		}
		if !strings.Contains(text, breakPage2) {
			t.Errorf("%s dropped the page after next-page token: %s", c.tool, text)
		}
		_ = m
	}
}
