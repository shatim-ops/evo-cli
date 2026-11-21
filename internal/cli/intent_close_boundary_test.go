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
	"testing"

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
