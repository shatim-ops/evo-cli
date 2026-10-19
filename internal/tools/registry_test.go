// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
)

// nilFactory is an EngineFactory that fails — for tests that must not reach
// the cloud.
func nilFactory(map[string]any) (*engine.Engine, error) {
	return nil, errors.New("no engine in tests")
}

// newTestServer keeps the name the handler tests had in internal/mcp.
func newTestServer() *Registry {
	return New(nilFactory)
}

// Every tool has exactly one handler and every handler belongs to a tool.
func TestRegistry_OneHandlerPerTool(t *testing.T) {
	r := newTestServer()
	seen := map[string]bool{}
	for _, tl := range r.Tools() {
		if seen[tl.Name] {
			t.Errorf("duplicate tool %q", tl.Name)
		}
		seen[tl.Name] = true
		if tl.Handler == nil {
			t.Errorf("tool %q has no handler", tl.Name)
		}
	}
	for name := range r.handlers {
		if !seen[name] {
			t.Errorf("handler %q has no tool definition", name)
		}
	}
	if len(r.Tools()) != 152 {
		t.Errorf("registry has %d tools, want 152 (146 + vm_list + image_list + k8s_addons, k8s_addon_versions, k8s_add_addon + marketplace_list)", len(r.Tools()))
	}
}

func TestRegistry_EveryToolHasAKnownGroup(t *testing.T) {
	known := map[string]bool{}
	for _, g := range Groups() {
		known[g] = true
	}
	if len(known) != 23 {
		t.Errorf("%d groups, want 23: %v", len(known), Groups())
	}
	used := map[string]bool{}
	for _, tl := range newTestServer().Tools() {
		if !known[tl.Group] {
			t.Errorf("tool %q: group %q is not a known group", tl.Name, tl.Group)
		}
		used[tl.Group] = true
	}
	for g := range known {
		if !used[g] {
			t.Errorf("group %q has no tools", g)
		}
	}
}

func TestRegistry_Mutating(t *testing.T) {
	r := newTestServer()
	cases := map[string]bool{
		"disk_delete":      true,
		"deploy":           true,
		"s3_bucket_create": true,
		"cert_private_key": false,
		"secret_value":     false,
		"list_zones":       false,
		"vm_get":           false,
		"pg_list":          false,
	}
	for name, want := range cases {
		tl, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("tool %q not found", name)
		}
		if tl.Mutating != want {
			t.Errorf("%s: mutating = %v, want %v", name, tl.Mutating, want)
		}
	}
	if _, ok := r.Lookup("nope"); ok {
		t.Error("Lookup found a tool that does not exist")
	}
}

// Wrap reaches every caller, including the in-process calls of the stack
// orchestrator (callTool), not only the transport's dispatch.
// Every confirmed argument has the one description (with a note where a
// tool asks only for some verbs): agents read the same rule everywhere.
func TestRegistry_ConfirmedHasOneDescription(t *testing.T) {
	n := 0
	for _, tl := range New(nilFactory).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		p, ok := props["confirmed"].(map[string]any)
		if !ok {
			continue
		}
		n++
		if d, _ := p["description"].(string); !strings.HasSuffix(d, confirmedDesc) {
			t.Errorf("%s: confirmed is %q, want confirmedProp()", tl.Name, d)
		}
	}
	if n < 60 {
		t.Errorf("only %d tools take confirmed", n)
	}
}

func TestRegistry_WrapReachesInProcessCalls(t *testing.T) {
	r := newTestServer()
	ok := r.Wrap("cert_private_key", func(Handler) Handler {
		return func(context.Context, map[string]any) (string, bool) {
			return ErrResult(errors.New("blocked by policy")), true
		}
	})
	if !ok {
		t.Fatal("Wrap: tool not found")
	}
	if _, err := r.callTool(context.Background(), "cert_private_key", map[string]any{"certificate_id": "c", "confirmed": true}); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Errorf("in-process call bypassed the wrapper: %v", err)
	}
	tl, _ := r.Lookup("cert_private_key")
	if text, _ := tl.Handler(context.Background(), nil); !strings.Contains(text, "blocked by policy") {
		t.Errorf("Lookup returns the unwrapped handler: %s", text)
	}
	if r.Wrap("nope", func(h Handler) Handler { return h }) {
		t.Error("Wrap of an unknown tool reported success")
	}
}

// A call refused for its arguments says so with the USAGE reason, so the CLI
// exits 1 (bad input) rather than 5 (API error) — and nothing reaches the
// cloud. The code is in "reason", not in the text.
func TestUsageResult_MarksArgumentErrors(t *testing.T) {
	r := newTestServer()
	text, isErr := r.handlers["vm_get"](context.Background(), map[string]any{})
	if !isErr || !strings.Contains(text, `"reason": "USAGE"`) || !strings.Contains(text, `"error": "vm_id or name is required"`) {
		t.Fatalf("vm_get without arguments: %s", text)
	}
}
