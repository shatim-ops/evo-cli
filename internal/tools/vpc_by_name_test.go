// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestVPCNameRequiresUniqueProjectMatch(t *testing.T) {
	for _, tc := range []struct {
		label, second, ref, reason string
		pages, cards               int
	}{
		{"unique", `{"vpcs":[]}`, "tEsT-net", "", 2, 1},
		{"uuid", `{"vpcs":[]}`, auditUUID, "", 0, 1},
		{"ambiguous", `{"vpcs":[{"id":"00000000-0000-4000-8000-000000000042","name":"test-net"}]}`, "test-net", "USAGE", 2, 0},
		{"incomplete", `{"vpcs":[],"nextPageToken":3}`, "test-net", "API_ERROR", 2, 0},
		{"missing list", `{}`, "test-net", "API_ERROR", 2, 0},
		{"null item", `{"vpcs":[null]}`, "test-net", "API_ERROR", 2, 0},
		{"foreign project", `{"vpcs":[{"id":"00000000-0000-4000-8000-000000000042","name":"private","projectId":"another"}]}`, "test-net", "API_ERROR", 2, 0},
		{"unknown", `{"vpcs":[]}`, "unlisted", "USAGE", 2, 0},
		{"full-length name", `{"vpcs":[]}`, "stringxx-name-with-four-abcdefgh1234", "", 2, 1},
	} {
		t.Run(tc.label, func(t *testing.T) {
			pages, cards, writes := 0, 0, 0
			firstName := "TEST-Net"
			if tc.label == "full-length name" {
				firstName = tc.ref
			}
			f := &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
				switch {
				case req.Method != "GET":
					writes++
					w.WriteHeader(400)
				case req.URL.Path == "/v1/vpcs":
					pages++
					if req.URL.Query().Get("project_id") != dbaasProject {
						t.Errorf("unscoped list: %s", req.URL)
					}
					if req.URL.Query().Get("pageToken") == "second" {
						fmt.Fprint(w, tc.second)
					} else {
						fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":%q}],"nextPageToken":"second"}`, auditUUID, firstName)
					}
				case req.URL.Path == "/v1/vpcs/"+auditUUID:
					cards++
					fmt.Fprintf(w, `{"id":%q,"name":"TEST-Net","projectId":%q}`, auditUUID, dbaasProject)
				default:
					w.WriteHeader(404)
				}
				return true
			}}
			r := apiFakeRegistry(t, f)
			m, body, failed := callNamed(t, r, "vpc_get", map[string]any{"id": tc.ref})
			if failed != (tc.reason != "") || (failed && m["reason"] != tc.reason) || (!failed && !strings.Contains(body, auditUUID)) || pages != tc.pages || cards != tc.cards || writes != 0 {
				t.Fatalf("list pages %d, cards %d, writes %d: %s", pages, cards, writes, body)
			}
		})
	}
}

func TestVPCRoutePreviewNamesParentAndUsesID(t *testing.T) {
	const route = "00000000-0000-4000-8000-000000000063"
	for _, tool := range []string{"vpc_route_create", "vpc_route_delete"} {
		t.Run(tool, func(t *testing.T) {
			writes := 0
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				switch {
				case r.Method == "GET" && r.URL.Path == "/v1/vpcs":
					fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":"Project-Net"}]}`, auditUUID)
				case r.Method == "GET" && r.URL.Path == "/v1/vpcs/"+auditUUID:
					fmt.Fprintf(w, `{"id":%q,"name":"Project-Net","projectId":%q}`, auditUUID, dbaasProject)
				case r.Method == "GET" && r.URL.Path == "/v1/vpcs/"+auditUUID+"/routes/static":
					fmt.Fprintf(w, `{"routes":[{"id":%q,"subnet":"10.9.0.0/24"}]}`, route)
				case r.Method == "POST" && r.URL.Path == "/v1/vpcs/"+auditUUID+"/routes/static", r.Method == "DELETE" && r.URL.Path == "/v1/vpcs/"+auditUUID+"/routes/static/"+route:
					writes++
					fmt.Fprint(w, `{"id":"op-route","done":false}`)
				default:
					w.WriteHeader(404)
				}
				return true
			}}
			registry := apiFakeRegistry(t, f)
			args := map[string]any{"vpc_id": "project-net", "route_id": route}
			if tool == "vpc_route_create" {
				args = map[string]any{"vpc_id": "project-net", "subnet": "10.10.0.0/24", "next_hop_vip_id": auditUUID}
			}
			m, body, _ := callNamed(t, registry, tool, args)
			if m["status"] != "confirmation_required" || !strings.Contains(body, "Project-Net") || !strings.Contains(body, auditUUID) || writes != 0 {
				t.Fatal(writes, body)
			}
			args["confirmed"] = true
			if _, body, fail := callNamed(t, registry, tool, args); fail || writes != 1 {
				t.Fatal(writes, body)
			}
		})
	}
}

func TestVPCNameAmbiguityPrecedesEveryAction(t *testing.T) {
	for tool, args := range map[string]map[string]any{
		"vpc_get": {"id": "shared"}, "vpc_delete": {"id": "shared", "confirmed": true},
		"vpc_routes":       {"vpc_id": "shared"},
		"vpc_route_create": {"vpc_id": "shared", "subnet": "10.5.0.0/24", "next_hop_vip_id": auditUUID, "confirmed": true},
		"vpc_route_delete": {"vpc_id": "shared", "route_id": auditUUID, "confirmed": true},
		"dns_zone_create":  {"domain": "example.internal", "type": "private", "vpc_id": "shared", "confirmed": true},
		"subnet_create":    {"name": "private", "cidr": "10.5.0.0/24", "vpc_id": "shared", "confirmed": true},
	} {
		t.Run(tool, func(t *testing.T) {
			extra := 0
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == "GET" && r.URL.Path == "/v1/vpcs" && r.URL.Query().Get("project_id") == dbaasProject {
					fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":"SHARED"},{"id":"00000000-0000-4000-8000-000000000042","name":"shared"}]}`, auditUUID)
				} else {
					extra++
					w.WriteHeader(400)
				}
				return true
			}}
			m, body, fail := callNamed(t, apiFakeRegistry(t, f), tool, args)
			if !fail || m["reason"] != "USAGE" || !strings.Contains(body, auditUUID) || !strings.Contains(body, "00000000-0000-4000-8000-000000000042") || extra != 0 {
				t.Fatal(extra, body)
			}
		})
	}
}
