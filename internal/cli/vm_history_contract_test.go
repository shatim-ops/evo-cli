// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Compute's task list survives deletion of the VM. Both public surfaces must
// retain the same filters and return only validated rows, even on a later error.
func TestContract_VMHistory(t *testing.T) {
	const otherVM = "00000000-0000-4000-8100-000000000002"
	for _, scenario := range []string{"two pages", "second VM", "empty", "unauthorized", "forbidden", "late denial", "wrong VM", "wrong type", "missing ID", "repeated ID", "early end", "total changed", "invalid argument"} {
		t.Run(scenario, func(t *testing.T) {
			w, _ := withJSONWorld(t)
			if _, ok := newRegistry().Lookup("vm_history"); !ok {
				t.Fatal("vm_history is absent from the registry")
			}
			target := parityVM
			if scenario == "second VM" {
				target = otherVM
			}
			ids := []string{"00000000-0000-4000-8100-000000000101", "00000000-0000-4000-8100-000000000102"}
			if target == otherVM {
				ids = []string{"00000000-0000-4000-8100-000000000201", "00000000-0000-4000-8100-000000000202"}
			}
			old := w.rt.extra
			var offsets []int
			w.rt.extra = func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "compute.api.cloud.ru" {
					return old(req)
				}
				if req.URL.Path == "/api/v1/vms/"+target && req.Method == "GET" {
					return contractRT{status: 404, body: `{"message":"VM was deleted"}`}.RoundTrip(req)
				}
				if req.Method != "GET" || req.URL.Path != "/api/v1/tasks" {
					t.Fatalf("unexpected Compute request: %s %s", req.Method, req.URL)
				}
				q := req.URL.Query()
				offset := 0
				if raw := q.Get("offset"); raw != "" {
					var err error
					offset, err = strconv.Atoi(raw)
					if err != nil {
						t.Fatal(err)
					}
				}
				if q.Get("entity_id") != target || q.Get("project_id") != pvProject || q.Get("entity_type") != "vm" || offset != len(offsets) || offset > 2 {
					t.Fatalf("unexpected task query %s after %v", req.URL, offsets)
				}
				offsets = append(offsets, offset)
				status := 200
				switch scenario {
				case "unauthorized":
					status = 401
				case "forbidden":
					status = 403
				case "late denial":
					if offset == 1 {
						status = 403
					}
				}
				if status != 200 {
					return contractRT{status: status, body: `{"message":"private-fixture-error"}`}.RoundTrip(req)
				}
				total := 2
				rows := []map[string]any{}
				if scenario == "empty" {
					total = 0
				} else if offset < 2 && !(scenario == "early end" && offset == 1) {
					row := map[string]any{"id": ids[offset], "entity_id": target, "entity_type": "vm", "name": []string{"Create VM", "Delete Vm"}[offset], "created_time": "2026-11-21T05:53:30Z", "status": "complete", "user_email": "private-fixture-user", "entity_name": "private-fixture-name", "metadata": map[string]any{"nested": "private-fixture-metadata"}}
					if offset == 1 {
						switch scenario {
						case "wrong VM":
							row["entity_id"] = otherVM
						case "wrong type":
							row["entity_type"] = "disk"
						case "missing ID":
							delete(row, "id")
						case "repeated ID":
							row["id"] = ids[0]
						case "total changed":
							total = 3
						}
					}
					rows = append(rows, row)
				}
				b, err := json.Marshal(map[string]any{"items": rows, "total": total, "limit": 1, "offset": offset})
				if err != nil {
					t.Fatal(err)
				}
				return contractRT{status: 200, body: string(b)}.RoundTrip(req)
			}
			arg := target
			if scenario == "invalid argument" {
				arg = "not-a-uuid"
			}
			var previous map[string]any
			for _, surface := range []string{"CLI", "MCP stdio"} {
				offsets = nil
				var answer map[string]any
				var failed bool
				var raw string
				if surface == "CLI" {
					newProcess()
					code, out, stderr := runCLI(t, "", "--json", "vm", "history", arg)
					failed = code != 0
					raw = out + stderr
					answer = parseObject(out)
					if failed {
						e, ok := errorEnvelope(stderr)
						if !ok {
							t.Fatalf("exit %d without error envelope: %s", code, stderr)
						}
						if answer == nil {
							answer = e.Details
							if answer == nil {
								answer = map[string]any{}
							}
							answer["reason"] = e.Reason
						}
					}
				} else {
					reply := callStdioTool(t, "vm_history", map[string]any{"vm_id": arg})
					failed = reply.isErr
					raw = reply.text
					answer = parseObject(raw)
				}
				if answer == nil || strings.Contains(raw, "private-fixture-") {
					t.Fatalf("%s leaked fixture data or returned invalid JSON", surface)
				}
				partial := answer["partial"] == true
				hasFirst, hasSecond := strings.Contains(raw, ids[0]), strings.Contains(raw, ids[1])
				wantReads := []int{0, 1}
				switch scenario {
				case "invalid argument", "unauthorized", "forbidden":
					wantReads = []int{0}
					if scenario == "invalid argument" {
						wantReads = nil
					}
					if !failed || answer["reason"] == nil || hasFirst || hasSecond {
						t.Fatalf("%s: expected a structured first-page or argument failure: %v", surface, answer)
					}
				case "empty":
					wantReads = []int{0}
					if failed || partial || hasFirst || hasSecond {
						t.Fatalf("%s: empty observed history: %v", surface, answer)
					}
				case "two pages", "second VM":
					if failed || partial || !hasFirst || !hasSecond || !strings.Contains(raw, "Create VM") || !strings.Contains(raw, "Delete Vm") || !strings.Contains(raw, "complete") {
						t.Fatalf("%s: incomplete successful history: %v", surface, answer)
					}
				default:
					if !partial || !hasFirst || hasSecond {
						t.Fatalf("%s: incomplete page must preserve only the first verified row: %v", surface, answer)
					}
				}
				if hasFirst || hasSecond || scenario == "empty" {
					src, ok := answer["source"].(string)
					if !ok || !strings.Contains(strings.ToLower(src), "compute") {
						t.Fatalf("%s: missing Compute source: %v", surface, answer)
					}
				}
				if hasFirst {
					checkVMTaskProjection(t, answer, ids[0], "Create VM")
				}
				if hasSecond {
					checkVMTaskProjection(t, answer, ids[1], "Delete Vm")
				}
				if len(wantReads) == 2 && len(offsets) == 3 && offsets[2] == 2 {
					wantReads = append(wantReads, 2)
				}
				if !reflect.DeepEqual(offsets, wantReads) {
					t.Fatalf("%s offsets %v, want %v", surface, offsets, wantReads)
				}
				observed := map[string]any{"first": hasFirst, "second": hasSecond, "partial": partial, "failed": failed, "reason": fmt.Sprint(answer["reason"])}
				if previous != nil && !reflect.DeepEqual(previous, observed) {
					t.Fatalf("CLI/MCP mismatch: %v %v", previous, observed)
				}
				previous = observed
			}
		})
	}
}

// Check the actual row in the wire result. A denied page can carry service
// metadata in its body even when neither of the two fixture task IDs appears.
func checkVMTaskProjection(t *testing.T, answer map[string]any, id, action string) {
	t.Helper()
	found := 0
	var visit func(any)
	visit = func(v any) {
		switch node := v.(type) {
		case map[string]any:
			for _, value := range node {
				if str, ok := value.(string); ok && str == id {
					found++
					values := map[string]bool{}
					for _, entry := range node {
						if text, ok := entry.(string); ok {
							values[text] = true
						}
					}
					if len(node) != 4 || !values[action] || !values["complete"] || !values["2026-11-21T05:53:30Z"] {
						t.Errorf("unexpected task projection for %s: %v", id, node)
					}
					return
				}
			}
			for _, entry := range node {
				visit(entry)
			}
		case []any:
			for _, entry := range node {
				visit(entry)
			}
		}
	}
	visit(answer)
	if found != 1 {
		t.Errorf("wanted a single task row for %s, got %d", id, found)
	}
}
