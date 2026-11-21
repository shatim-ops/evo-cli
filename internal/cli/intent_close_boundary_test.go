// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/intent"
)

// R-86: even a server that can change cloud resources offers no tool
// to dismiss a local record of an interrupted operation.
func TestContract_ManualIntentCloseIsAbsentFromMCP(t *testing.T) {
	w, _ := withJSONWorld(t)
	rec, err := intent.Open(intent.Dir(w.dir), intent.Intent{Tool: "redis_create", Name: "pending", Ended: true, Sent: true})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(rec.Dir, rec.ID+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	prior := w.rt.extra
	w.rt.extra = func(r *http.Request) (*http.Response, error) {
		t.Errorf("unexpected cloud request: %s %s", r.Method, r.URL.Path)
		return prior(r)
	}
	servers := parityServers(t)
	httpCalls := roServers{roHTTP: servers.http}
	for _, surface := range []string{"stdio", "http"} {
		t.Run(surface, func(t *testing.T) {
			call := func(method string, params any) (roAnswer, []string) {
				if surface == "http" {
					return httpCalls.http(t, method, params)
				}
				return httpCalls.stdio(t, servers.stdio, method, params)
			}
			listed, names := call("tools/list", map[string]any{})
			if listed.rpcErr != "" || !slices.Contains(names, "redis_list") {
				t.Fatalf("invalid inventory: %s (%d tools)", listed.rpcErr, len(names))
			}
			for _, name := range []string{"intent_close", "doctor_close_intent"} {
				if slices.Contains(names, name) {
					t.Errorf("tools/list exposed %s", name)
				}
				answer, _ := call("tools/call", map[string]any{"name": name, "arguments": map[string]any{"id": rec.ID, "confirmed": true}})
				if answer.rpcErr != "Unknown tool: "+name {
					t.Errorf("call of %s: %+v", name, answer)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("intent changed over %s: %v", surface, err)
			}
		})
	}
}

// The diagnostic decision applies only in its own folder and profile. Legacy
// readers, write guards and history still see the original unknown outcome.
func TestContract_ManualReviewPreservesHistoryAndGuards(t *testing.T) {
	w, _ := withJSONWorld(t)
	eng, err := toolEngine(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg := eng.Config
	rec, err := intent.Open(intent.Dir(w.dir), intent.Intent{Tool: "redis_create", Name: "manual-review-target", StartedAt: time.Now(), Surface: "cli", Counts: true, Ended: true, Sent: true, Key: eng.Maker(), ProjectID: cfg.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	load := func() (*intent.ManualSnapshot, error) { return intent.PreviewManual(w.dir, rec.ID, cfg.Profile) }
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Apply(load); err != nil {
		t.Fatal(err)
	}
	previous := w.rt.extra
	w.rt.extra = func(req *http.Request) (*http.Response, error) {
		if req.URL.Host == "redis.api.cloud.ru" {
			t.Errorf("manually reviewed receipt triggered Redis read: %s", req.URL.Path)
		}
		return previous(req)
	}
	code, out, stderr := runCLI(t, "", "doctor", "--json")
	if code != 0 || strings.Contains(out+stderr, rec.Name) {
		t.Fatal(code, out, stderr)
	}
	code, out, stderr = runCLI(t, "", "history", "--json")
	if code != 0 || !strings.Contains(out, rec.ID) || !strings.Contains(out, "unknown") {
		t.Fatal(code, out, stderr)
	}
	if records := intent.Pending(rec.Dir); len(records) != 1 || records[0].ID != rec.ID {
		t.Fatal(records)
	}
}
