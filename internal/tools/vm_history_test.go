// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

func TestVMHistorySecondPage(t *testing.T) {
	for _, scenario := range []string{"complete", "forbidden", "repeat", "early_empty", "foreign", "null_item"} {
		t.Run(scenario, func(t *testing.T) {
			vm, first, second := faultID(21), faultID(22), faultID(23)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.Method != "GET" || r.URL.Path != "/api/v1/tasks" || q.Get("project_id") != faultProject || q.Get("entity_id") != vm {
					t.Errorf("unexpected %s %s", r.Method, r.URL)
					http.NotFound(w, r)
					return
				}
				task, entity := first, vm
				page := 0
				switch q.Get("offset") {
				case "0", "":
				case "1":
					page = 1
					task = second
					switch scenario {
					case "forbidden":
						w.WriteHeader(403)
						return
					case "repeat":
						task = first
					case "foreign":
						entity = faultID(24)
					}
				default:
					t.Errorf("unexpected offset %s", q.Get("offset"))
					http.NotFound(w, r)
					return
				}
				rows := []map[string]any{{"id": task, "name": "Create VM", "entity_id": entity, "entity_type": "vm", "created_time": "2026-11-21T05:53:30Z", "status": "complete", "user_email": "private@example.invalid"}}
				if scenario == "early_empty" && page == 1 {
					rows = []map[string]any{}
				}
				if scenario == "null_item" && page == 1 {
					rows[0] = nil
				}
				json.NewEncoder(w).Encode(map[string]any{"items": rows, "total": 2, "offset": page, "limit": 1})
			}))
			defer srv.Close()
			dir := t.TempDir()
			reg := New(func(map[string]any) (*engine.Engine, error) {
				adapter := provision.NewCloudRuAdapter(srv.Client(), srv.URL)
				return &engine.Engine{StateDir: dir, Config: config.Config{ProjectID: faultProject}, Auth: staticToken{}, Cloud: adapter, Printer: output.NewWithWriter(&bytes.Buffer{}, output.ModeJSON, false)}, nil
			})
			reg.SetStateDir(dir)
			if _, ok := reg.Lookup("vm_history"); !ok {
				t.Fatal("vm_history is absent from the registry")
			}
			m, body, isErr := callNamed(t, reg, "vm_history", map[string]any{"vm_id": vm})
			if strings.Contains(body, "private@example.invalid") || !strings.Contains(body, first) {
				t.Fatalf("unexpected visible rows: %s", body)
			}
			if scenario == "forbidden" && (m["http_status"] != float64(403) || m["access_denied"] != true) {
				t.Fatalf("403 lost its structured denial: %s", body)
			}
			if scenario == "complete" {
				if isErr || !strings.Contains(body, second) {
					t.Fatalf("missing successful second page: %s", body)
				}
			} else if m["partial"] != true || strings.Contains(body, second) {
				t.Fatalf("invalid page must leave an explicit partial result: %s", body)
			}
		})
	}
}

// The OpenAPI page requires items and total. Only an actual empty array
// with total=0 establishes an empty selection.
func TestVMHistoryPageShape(t *testing.T) {
	for _, c := range []struct {
		body  string
		valid bool
	}{
		{`{"items":[],"total":0}`, true},
		{`null`, false},
		{`{}`, false},
		{`{"items":null,"total":0}`, false},
		{`{"items":[]}`, false},
		{`{"items":[],"total":-1}`, false},
		{`{"items":[],"total":0,"limit":0}`, false},
		{`{"items":[],"total":0,"offset":1}`, false},
		{`{"items":[],"total":0,"limit":101}`, false},
	} {
		var p *provision.VMTaskPage
		if err := json.Unmarshal([]byte(c.body), &p); err != nil {
			t.Fatal(err)
		}
		if got := validTaskPage(p, 0, 100, -1); got != c.valid {
			t.Errorf("%s: valid=%v, want %v", c.body, got, c.valid)
		}
	}
}
