// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/history"
	"github.com/shatim-ops/evo-cli/internal/intent"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

func TestHistory_OnlyAdmittedProjectsOnBothTransports(t *testing.T) {
	const (
		allowed    = "00000000-0000-4000-8000-000000000001"
		denied     = "00000000-0000-4000-8000-000000000002"
		unresolved = "00000000-0000-4000-8000-000000000003"
	)
	for _, transport := range []string{"stdio", "http"} {
		t.Run(transport, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CLOUDRU_CONFIG_DIR", t.TempDir())
			s := New(func(over map[string]any) (*engine.Engine, error) {
				switch over["ProjectID"] {
				case denied:
					return nil, tools.ErrProjectNotAllowed
				case unresolved:
					return nil, errors.New("profile unavailable")
				}
				return &engine.Engine{Config: config.Config{ProjectID: allowed}}, nil
			}, "test")
			s.SetStateDir(dir)
			s.SetProjectBound()
			call := func(req string) map[string]any { return roundTrip(t, s, req) }
			if transport == "http" {
				h := NewHTTPHandler(s, HTTPOptions{BearerToken: testToken})
				ts := httptest.NewServer(h)
				t.Cleanup(ts.Close)
				t.Cleanup(h.Close)
				call = func(req string) map[string]any {
					var resp map[string]any
					decodeBody(t, post(t, ts, req, nil), &resp)
					return resp
				}
			}
			appendLine := func(project, id, name string) {
				t.Helper()
				line := history.Line{Time: "2026-11-17T10:00:00Z", Tool: "disk_create", ProjectID: project,
					Outcome: history.Created, IntentID: id, ResourceID: name, Kind: "disk"}
				if err := history.Append(history.DirOf(intent.Dir(dir)), line); err != nil {
					t.Fatal(err)
				}
			}
			const req = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"history","arguments":{"since":"2026-11-01",%s}}}`
			appendLine(allowed, "aa00000000000001", "disk-allowed")
			want := call(fmt.Sprintf(req, `"check":true`))
			raw, _ := json.Marshal(want)
			if !strings.Contains(string(raw), "disk-allowed") {
				t.Fatalf("the allowed line is missing: %s", raw)
			}
			appendLine(denied, "aa00000000000002", "disk-denied")
			appendLine(unresolved, "aa00000000000003", "disk-unresolved")
			if got := call(fmt.Sprintf(req, `"check":true`)); !reflect.DeepEqual(got, want) {
				t.Error("adding lines without proven access changed the history response")
			}
			for _, project := range []string{denied, unresolved} {
				resp := call(fmt.Sprintf(req, fmt.Sprintf(`"project_id":%q`, project)))
				result, _ := resp["result"].(map[string]any)
				raw, _ := json.Marshal(resp)
				if result["isError"] != true || strings.Contains(string(raw), "disk-") {
					t.Errorf("explicit unavailable project returned history: %s", raw)
				}
			}
		})
	}
}
