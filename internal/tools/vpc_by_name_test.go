// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
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
			answer, body, fail := callNamed(t, registry, tool, args)
			if fail || writes != 1 || args["vpc_id"] != "project-net" {
				t.Fatal(writes, args, body)
			}
			next, _ := answer["next_args"].(map[string]any)
			if _, carried := next["confirmed"]; carried {
				t.Fatalf("follow-up carries confirmation: %s", body)
			}
		})
	}
}

func TestVPCNameAmbiguityPrecedesEveryAction(t *testing.T) {
	for tool, args := range map[string]map[string]any{
		"lb_create": {"name": "web", "target_group_id": "tg-1", "port": float64(80), "vpc_id": "shared", "confirmed": true},
		"vpc_get":   {"id": "shared"}, "vpc_delete": {"id": "shared", "confirmed": true},
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

func TestVPCNameRequiresProjectBeforeLookup(t *testing.T) {
	r, rec := noProjectRegistry(t)
	m, body, fail := callNamed(t, r, "vpc_get", map[string]any{"id": "not-global"})
	if calls := rec.take(); !fail || m["reason"] != "CONFIG_INVALID" || len(calls) != 0 {
		t.Fatal(calls, body)
	}
}

func lbVPCFixture() *apiFake {
	return &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
		switch req.URL.Path {
		case "/v1/vpcs":
			fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":"Private-Net"}]}`, auditUUID)
		case "/v2/targetGroups/tg-1":
			fmt.Fprintf(w, `{"id":"tg-1","name":"tg","vpc_id":%q}`, auditUUID)
		case "/v2/networkLoadBalancers":
			if req.Method == "POST" {
				fmt.Fprint(w, `{"id":"op-1","done":false}`)
			} else {
				fmt.Fprint(w, `{"balancers":[]}`)
			}
		default:
			return false
		}
		return true
	}}
}

func TestLBVPCNameMatchesTargetGroup(t *testing.T) {
	f := lbVPCFixture()
	subs := raSubnets()
	subs[0].VPCID = auditUUID
	r := raRegistry(t, f, raCloud{subnets: subs}, nil)
	args := map[string]any{"name": "web", "vpc_id": "private-net", "target_group_id": "tg-1", "port": float64(80), "zone": "ru.AZ-1"}
	m, body, _ := callNamed(t, r, "lb_create", args)
	if m["status"] != "confirmation_required" || !strings.Contains(body, "Private-Net") || !strings.Contains(body, auditUUID) {
		t.Fatal(body)
	}
	args["preview_id"] = m["preview_id"]
	args["confirmed"] = true
	if _, body, failed := callNamed(t, r, "lb_create", args); failed {
		t.Fatal(body)
	}
	posts := 0
	for _, call := range f.calls {
		if strings.HasPrefix(call, "POST ") {
			posts++
		}
	}
	if posts != 1 {
		t.Fatalf("got %d POSTs: %v", posts, f.calls)
	}
	post, ok := f.posted("nlb.api.cloud.ru/v2/networkLoadBalancers")
	var sent struct {
		VPC string `json:"vpc_id"`
	}
	if !ok || json.Unmarshal([]byte(post), &sent) != nil || sent.VPC != auditUUID {
		t.Fatal(post)
	}
}

func TestLBVPCNameFailsOnIncompleteLaterPage(t *testing.T) {
	f := lbVPCFixture()
	requests := 0
	f.own = func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != "GET" || r.URL.Path != "/v1/vpcs" || r.URL.Query().Get("project_id") != dbaasProject {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(400)
			return true
		}
		requests++
		if r.URL.Query().Get("pageToken") == "rest" {
			fmt.Fprint(w, `{"vpcs":null}`)
		} else {
			fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":"Private-Net"}],"nextPageToken":"rest"}`, auditUUID)
		}
		return true
	}
	args := map[string]any{"name": "web", "port": float64(80), "target_group_id": "tg-1", "vpc_id": "private-net", "confirmed": true}
	m, body, failed := callNamed(t, apiFakeRegistry(t, f), "lb_create", args)
	if !failed || m["reason"] != "API_ERROR" || requests != 2 {
		t.Fatal(requests, body)
	}
}

func TestVPCNameListFiltersNeedCompleteCatalog(t *testing.T) {
	for _, tool := range []string{"list_subnets", "lb_list", "tg_list"} {
		t.Run(tool, func(t *testing.T) {
			pages, downstream := 0, 0
			f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
				if r.Method == "GET" && r.URL.Path == "/v1/vpcs" {
					pages++
					if r.URL.Query().Get("project_id") != dbaasProject {
						t.Error("missing project")
					}
					if r.URL.Query().Get("pageToken") == "last" {
						fmt.Fprint(w, `{"vpcs":null}`)
					} else {
						fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":"Shared"}],"nextPageToken":"last"}`, auditUUID)
					}
					return true
				}
				downstream++
				fmt.Fprint(w, `{"items":[],"balancers":[],"targetGroups":[]}`)
				return true
			}}
			m, body, failed := callNamed(t, apiFakeRegistry(t, f), tool, map[string]any{"vpc_id": "shared"})
			if !failed || m["reason"] != "API_ERROR" || pages != 2 || downstream != 0 {
				t.Fatal(pages, downstream, body)
			}
		})
	}
}

func TestVPCFilterCatalogReadCounts(t *testing.T) {
	for _, tc := range []struct {
		tool, ref, targetPath string
	}{
		{"list_subnets", "Shared", "/api/v1/subnets"},
		{"list_subnets", auditUUID, "/api/v1/subnets"},
		{"lb_list", "Shared", "/v2/networkLoadBalancers"},
		{"lb_list", auditUUID, "/v2/networkLoadBalancers"},
		{"tg_list", "Shared", "/v2/targetGroups"},
		{"tg_list", auditUUID, "/v2/targetGroups"},
		{"vpc_list", "", ""},
	} {
		for _, page := range []struct {
			name, body string
			wantError  bool
		}{
			{"empty-array", `{"vpcs":[]}`, false},
			{"null-list", `{"vpcs":null}`, true},
		} {
			name := tc.tool
			if tc.ref != "" {
				name += "/" + tc.ref
			}
			t.Run(name+"/"+page.name, func(t *testing.T) {
				reads, targets := 0, 0
				f := &apiFake{own: func(w http.ResponseWriter, r *http.Request) bool {
					switch {
					case r.Method == "GET" && r.URL.Path == "/v1/vpcs":
						reads++
						token := r.URL.Query().Get("pageToken")
						switch {
						case reads == 1 && token == "":
							fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":"Shared"}],"nextPageToken":"last"}`, auditUUID)
						case reads == 2 && token == "last":
							fmt.Fprint(w, page.body)
						default:
							t.Errorf("unexpected catalog GET #%d: %s", reads, r.URL)
							w.WriteHeader(http.StatusBadRequest)
						}
					case r.Method == "GET" && r.URL.Path == tc.targetPath:
						targets++
						fmt.Fprint(w, `{"items":[],"balancers":[],"targetGroups":[]}`)
					default:
						t.Errorf("unexpected request %s %s", r.Method, r.URL)
						w.WriteHeader(http.StatusBadRequest)
					}
					return true
				}}
				var args map[string]any
				wantTargets := 0
				if tc.ref != "" {
					args = map[string]any{"vpc_id": tc.ref}
					if !page.wantError {
						wantTargets = 1
					}
				}
				result, body, failed := callNamed(t, apiFakeRegistry(t, f), tc.tool, args)
				if reads != 2 || targets != wantTargets || failed != page.wantError {
					t.Fatalf("catalog GETs=%d target GETs=%d failed=%t: %s", reads, targets, failed, body)
				}
				if page.wantError {
					if result["reason"] != "API_ERROR" || strings.Contains(body, auditUUID) {
						t.Fatal(body)
					}
				} else if !strings.Contains(body, auditUUID) {
					t.Fatal(body)
				}
			})
		}
	}
}
