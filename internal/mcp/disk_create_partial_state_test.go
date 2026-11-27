// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shatim-ops/evo-cli/internal/config"
	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/output"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

const observedDiskID = "00000000-0000-4000-8000-0000000000d1"

type diskStateCloud struct {
	provisionCloud
	readID, state            string
	reads, creates, attaches int
}

func (*diskStateCloud) ListVMsInProject(context.Context, string) ([]*provision.VM, error) {
	return []*provision.VM{{ID: "00000000-0000-4000-8000-0000000000e1", Name: "web", Region: "00000000-0000-4000-8000-000000000002"}}, nil
}

func (c *diskStateCloud) CreateDisk(context.Context, provision.CreateDiskReq) (map[string]any, error) {
	c.creates++
	return map[string]any{"id": observedDiskID, "name": "data", "state": "creating"}, nil
}

func (c *diskStateCloud) ListDisks(context.Context, string) ([]map[string]any, error) {
	if c.creates == 0 {
		return nil, nil // the name is available before the create
	}
	c.reads++
	if c.readID == "" {
		return nil, errors.New("fixture read failed")
	}
	return []map[string]any{{"id": c.readID, "name": "data", "state": c.state}}, nil
}

func (c *diskStateCloud) AttachDisk(context.Context, string, string, string) (map[string]any, error) {
	c.attaches++
	return nil, &provision.StatusError{Status: http.StatusForbidden, Body: "fixture denied"}
}

func TestDiskCreate_ReportsObservedStateOverMCP(t *testing.T) {
	for _, transport := range []string{"stdio", "http"} {
		for _, tc := range []struct {
			name, readID, state, wantState string
			wantAttaches                   int
			atCallBudget                   bool
		}{
			{"terminal", observedDiskID, "error_creating", "error_creating", 0, false},
			{"attach_forbidden", observedDiskID, "available", "available", 1, false},
			{"unreadable", "", "", "", 0, false},
			{"different_disk", "00000000-0000-4000-8000-0000000000d2", "available", "", 0, false},
			{"unreadable_at_call_budget", "", "", "", 0, true},
		} {
			t.Run(transport+"/"+tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					c := &diskStateCloud{readID: tc.readID, state: tc.state}
					// The short engine timeout yields a partial refusal. A timeout beyond
					// the MCP budget yields a continuation under the same virtual clock.
					timeout := 3 * time.Second
					if tc.atCallBudget {
						timeout = time.Minute
					}
					dir := t.TempDir()
					s := New(func(map[string]any) (*engine.Engine, error) {
						return &engine.Engine{Cloud: c, Auth: breakToken{}, StateDir: dir,
							Config:  config.Config{ProjectID: "00000000-0000-4000-8000-000000000001"},
							Printer: output.NewWithWriter(io.Discard, output.ModeText, false),
							Timings: engine.Timings{VMPollInterval: time.Second, VMPollTimeout: timeout},
						}, nil
					}, "test")
					s.SetStateDir(dir)
					answer, isError := diskStateCall(t, s, transport)
					if c.creates != 1 || c.attaches != tc.wantAttaches || c.reads == 0 || answer["disk_id"] != observedDiskID {
						t.Fatalf("creates=%d attaches=%d reads=%d result=%v", c.creates, c.attaches, c.reads, answer)
					}
					if tc.atCallBudget {
						next, _ := answer["next_args"].(map[string]any)
						if isError || answer["next_tool"] != "disk_attach" || next["disk_id"] != observedDiskID || answer["disk_state"] != "unknown" {
							t.Fatalf("expected continuation for the same disk in unknown state: %v", answer)
						}
					} else if !isError || answer["status"] != "partial" || answer["reason"] != "API_ERROR" {
						t.Fatalf("expected partial tool refusal: %v", answer)
					}
					disk, _ := answer["disk"].(map[string]any)
					if disk["id"] != observedDiskID {
						t.Fatalf("disk lost its identity: %v", disk)
					}
					if tc.wantState != "" {
						if disk["state"] != tc.wantState {
							t.Errorf("disk.state=%v, want %s", disk["state"], tc.wantState)
						}
					} else {
						_, state := disk["state"]
						_, status := disk["status"]
						message, _ := answer["error"].(string)
						if state || status || (!tc.atCallBudget && !strings.Contains(strings.ToLower(message), "disk state could not be read")) {
							t.Errorf("unreadable disk has no verified state: %v", answer)
						}
					}
				})
			})
		}
	}
}

func diskStateCall(t *testing.T, s *Server, transport string) (map[string]any, bool) {
	t.Helper()
	const req = `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"disk_create","arguments":{"name":"data","size_gb":10,"disk_type":"SSD","attach_to":"web","confirmed":true}}}`
	var rpc map[string]any
	if transport == "stdio" {
		rpc = roundTrip(t, s, req)
	} else {
		h := NewHTTPHandler(s, HTTPOptions{})
		defer h.Close()
		r := httptest.NewRequest(http.MethodPost, "http://localhost/mcp", strings.NewReader(req))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rpc) != nil {
			t.Fatalf("HTTP %d: %s", w.Code, w.Body)
		}
	}
	result, _ := rpc["result"].(map[string]any)
	blocks, _ := result["content"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("invalid tool result: %v", rpc)
	}
	block, _ := blocks[0].(map[string]any)
	text, _ := block["text"].(string)
	var answer map[string]any
	if err := json.Unmarshal([]byte(text), &answer); err != nil {
		t.Fatal(err)
	}
	return answer, result["isError"] == true
}
