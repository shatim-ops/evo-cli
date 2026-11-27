// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
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

// IAM-10 (acceptance B-5, 06.11): past its items the Organization API
// answers empty pages, each with a new token. org_projects read them to
// the limit of pages and failed; an empty page is the end of its list.
func TestOrgProjects_EmptyPagesWithATokenEndTheList(t *testing.T) {
	calls := 0
	f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Host+r.URL.Path != "organization.api.cloud.ru/v3/projects" {
			return false
		}
		calls++
		tok := r.URL.Query().Get("page_token")
		if tok == "" {
			_, _ = io.WriteString(w, `{"projects":[{"id":"`+pagesID1+`","name":"one","status":"active"}],"next_page_token":"p"}`)
			return true
		}
		_, _ = io.WriteString(w, `{"projects":[],"next_page_token":"`+tok+`p"}`)
		return true
	}}
	r := apiFakeRegistry(t, f)
	_, text, isErr := callNamed(t, r, "org_projects", nil)
	if isErr || !strings.Contains(text, pagesID1) || calls != 2 {
		t.Fatalf("org_projects with empty pages after the items: %d requests, %s", calls, text)
	}
}

func TestPgListAndCreate_RequireACompleteClusterList(t *testing.T) {
	for _, tc := range []struct {
		name string
		tool string
		args map[string]any
	}{
		{"list", "pg_list", nil},
		{"preview", "pg_create", map[string]any{"name": "new", "specification_id": "std"}},
		{"confirmed", "pg_create", map[string]any{"name": "new", "specification_id": "std", "confirmed": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var pages int
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host != "postgresql.api.cloud.ru" || r.Method != http.MethodGet || r.URL.Path != "/v1/clusters" {
					return pgFake(w, r)
				}
				pages++
				if r.URL.Query().Get("page_token") == "" {
					_, _ = io.WriteString(w, `{"clusters":[{"id":"one","name":"other","project_id":"proj","version":"17","specification_id":"std","storage":{"pg_data_gb":20}}],"next_page_token":"second"}`)
				} else {
					_, _ = io.WriteString(w, `{"clusters":{}}`)
				}
				return true
			}}
			r := apiFakeRegistry(t, f)
			m, text, failed := callNamed(t, r, tc.tool, tc.args)
			_, posted := f.posted("postgresql.api.cloud.ru/v1/clusters")
			if posted || !failed || m["reason"] != "API_ERROR" || pages != 2 || m["clusters"] != nil || m["will_create"] != nil {
				t.Fatalf("bad second page: %d pages, POST = %v, answer %s", pages, posted, text)
			}
		})
	}
}

func TestPgListAndCreate_ValidateClusterPage(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want int // -1 means the page is malformed.
	}{
		{"omitted array", `{}`, 0},
		{"empty array", `{"clusters":[]}`, 0},
		{"cluster", `{"clusters":[{"name":"other","id":"pg","project_id":"proj","version":"17","specification_id":"std","storage":{"pg_data_gb":20}}]}`, 1},
		{"object", `{"clusters":{}}`, -1},
		{"string", `{"clusters":"invalid"}`, -1},
		{"null", `{"clusters":null}`, -1},
		{"null element", `{"clusters":[null]}`, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host == "postgresql.api.cloud.ru" && r.Method == http.MethodGet && r.URL.Path == "/v1/clusters" {
					_, _ = io.WriteString(w, tc.body)
					return true
				}
				return pgFake(w, r)
			}}
			r := apiFakeRegistry(t, f)
			list, listText, failed := callNamed(t, r, "pg_list", nil)
			preview, previewText, _ := callNamed(t, r, "pg_create", map[string]any{"name": "new", "specification_id": "std"})
			if tc.want < 0 {
				if !failed || list["reason"] != "API_ERROR" || preview["reason"] != "API_ERROR" || preview["will_create"] != nil {
					t.Fatalf("malformed list: %s; create: %s", listText, previewText)
				}
			} else if clusters, ok := list["clusters"].([]any); !ok || failed || len(clusters) != tc.want || preview["status"] != "confirmation_required" {
				t.Fatalf("valid list: %s; create: %s", listText, previewText)
			}
			if _, posted := f.posted("postgresql.api.cloud.ru/v1/clusters"); posted {
				t.Fatal("preview sent a create")
			}
		})
	}
}

func TestIAMServiceAccounts_OnlyReturnsFullAccountList(t *testing.T) {
	const first = `{"serviceAccounts":[{"id":"first-account","name":"first"}],"nextPageToken":"p2"}`
	for _, tc := range []struct {
		name, second string
		status       int
		ok           bool
	}{
		{"live spelling", `{"service_accounts":[{"id":"second-account","name":"second","secret":"PRIVATE-FIELD"}]}`, 200, true},
		{"invalid continuation", `{"message":"account not found"}`, 404, false},
		{"partial HTTP", `{"serviceAccounts":[]}`, 206, false},
		{"null array", `{"service_accounts":null}`, 200, false},
		{"null item", `{"service_accounts":[null]}`, 200, false},
		{"missing identity", `{"service_accounts":[{"name":"second"}]}`, 200, false},
		{"wrong token", `{"serviceAccounts":[],"next_page_token":false}`, 200, false},
		{"ignored cursor", `{"service_accounts":[{"id":"first-account","name":"first"}]}`, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			r := apiFakeRegistry(t, &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host+r.URL.Path != "iam.api.cloud.ru/api/v2/serviceAccounts" {
					return false
				}
				calls++
				switch r.URL.Query().Get("pageToken") {
				case "":
					io.WriteString(w, first)
				case "p2":
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.second)
				default:
					t.Errorf("unexpected token %q", r.URL.Query().Get("pageToken"))
					w.WriteHeader(404)
				}
				return true
			}})
			got, text, isErr := callNamed(t, r, "iam_service_accounts", nil)
			if calls != 2 {
				t.Fatalf("%d requests: %s", calls, text)
			}
			if tc.ok {
				if isErr || !strings.Contains(text, "first-account") || !strings.Contains(text, "second-account") || strings.Contains(text, "PRIVATE-FIELD") {
					t.Fatalf("unexpected result: %s", text)
				}
			} else if !isErr || got["reason"] != "API_ERROR" || got["service_accounts"] != nil || strings.Contains(text, "first-account") {
				t.Fatalf("incomplete list is not a clean API_ERROR: %s", text)
			}
		})
	}
}

func TestIAMServiceAccounts_DisplayNameCanBeOmitted(t *testing.T) {
	for _, tc := range []struct {
		name, item string
		ok         bool
	}{
		{"no display name", `{"id":"id-only","enabled":true}`, true},
		{"name is object", `{"id":"id-only","name":{}}`, false},
		{"null reference ID", `{"id":null,"name":"label"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := apiFakeRegistry(t, &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Host+r.URL.Path != "iam.api.cloud.ru/api/v2/serviceAccounts" {
					return false
				}
				io.WriteString(w, `{"serviceAccounts":[`+tc.item+`]}`)
				return true
			}})
			got, text, isErr := callNamed(t, r, "iam_service_accounts", nil)
			if !tc.ok {
				if !isErr || got["reason"] != "API_ERROR" || got["service_accounts"] != nil {
					t.Fatalf("bad row: %s", text)
				}
				return
			}
			rows, _ := got["service_accounts"].([]any)
			if isErr || len(rows) != 1 {
				t.Fatalf("missing account: %s", text)
			}
			account := rows[0].(map[string]any)
			name, present := account["name"]
			if account["id"] != "id-only" || !present || name != nil || account["enabled"] != true {
				t.Errorf("changed account: %s", text)
			}
		})
	}
}

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
