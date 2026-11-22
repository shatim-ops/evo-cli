// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
)

func TestVPCResultIDProvenance(t *testing.T) {
	const name = "f1-target-network"
	const cloudID = "00000000-0000-4000-8000-00000000c101"
	const directID = "00000000-0000-4000-8000-00000000d202"
	const operation = "00000000-0000-4000-8000-00000000e303"
	for _, tc := range []struct {
		label, tool, key, arg, target string
		lists, deletes, operations    int
	}{
		{"occupied_name", "vpc_create", "name", name, cloudID, 1, 0, 0},
		{"delete_direct_uuid", "vpc_delete", "id", directID, directID, 0, 1, 1},
		{"delete_catalog_name", "vpc_delete", "id", name, cloudID, 1, 1, 1},
	} {
		t.Run(tc.label, func(t *testing.T) {
			var lists, cards, deletes, operations, posts int
			model := &apiFake{own: func(w http.ResponseWriter, req *http.Request) bool {
				switch {
				case req.Method == "GET" && req.URL.Path == "/v1/vpcs":
					lists++
					if req.URL.Query().Get("project_id") != dbaasProject {
						t.Error("lookup outside model project")
					}
					fmt.Fprintf(w, `{"vpcs":[{"id":%q,"name":%q,"projectId":%q}]}`, cloudID, name, dbaasProject)
				case req.Method == "GET" && req.URL.Path == "/v1/vpcs/"+tc.target:
					cards++
					fmt.Fprintf(w, `{"id":%q,"name":%q,"projectId":%q,"status":"ACTIVE"}`, tc.target, name, dbaasProject)
				case req.Method == "DELETE" && req.URL.Path == "/v1/vpcs/"+tc.target:
					deletes++
					fmt.Fprintf(w, `{"id":%q,"done":false}`, operation)
				case req.Method == "GET" && req.URL.Path == "/v1/vpcs/operations/"+operation:
					operations++
					fmt.Fprintf(w, `{"id":%q,"done":true}`, operation)
				default:
					if req.Method == "POST" {
						posts++
					}
					t.Errorf("unplanned request %s %s", req.Method, req.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
				return true
			}}
			args := map[string]any{tc.key: tc.arg, "confirmed": true}
			registry := apiFakeRegistry(t, model)
			result, _, refused := callNamed(t, registry, tc.tool, args)
			raw, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("tool=%s input=%s:%q fixture_uuid=%s fixture_operation=%s result=%s lists=%d cards=%d deletes=%d operations=%d posts=%d", tc.tool, tc.key, tc.arg, tc.target, operation, raw, lists, cards, deletes, operations, posts)
			if lists != tc.lists || deletes != tc.deletes || operations != tc.operations || posts != 0 {
				t.Fatalf("unexpected transport counts")
			}
			marks := stringList(result["_untrusted"])
			if tc.tool == "vpc_create" {
				if !refused || result["reason"] != "USAGE" || result["existing_id"] != cloudID {
					t.Fatalf("occupied VPC refusal did not carry the cloud ID")
				}
				if !slices.Contains(marks, "existing_id") {
					t.Errorf("cloud catalog existing_id absent from _untrusted")
				}
				return
			}
			if refused || result["status"] != "deleted" || result["id"] != tc.target || result["operation_id"] != operation || cards == 0 {
				t.Fatal("delete did not complete for the expected target and operation")
			}
			if !slices.Contains(marks, "operation_id") {
				t.Error("control failed: cloud operation_id absent from _untrusted")
			}
			if slices.Contains(marks, "id") != (tc.arg == name) {
				t.Error("delete result id provenance does not match the input")
			}
		})
	}
}
