// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	"github.com/shatim-ops/evo-cli/internal/tools"
)

// nilFactory is an EngineFactory that fails — used for tests that must not
// reach the cloud. It returns an error rather than a nil engine so a handler
// that skips its confirmation gate surfaces that as a plain error instead of a
// nil-pointer panic.
func nilFactory(map[string]any) (*engine.Engine, error) {
	return nil, errors.New("no engine in tests")
}

func newTestServer() *Server {
	return New(nilFactory, "test")
}

// roundTrip feeds a single JSON-RPC request line and returns the decoded response.
func roundTrip(t *testing.T, s *Server, req string) map[string]any {
	t.Helper()
	out := &bytes.Buffer{}
	if err := s.Run(strings.NewReader(req+"\n"), out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out.Len() == 0 {
		return nil // notification: no response
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", out.String(), err)
	}
	return resp
}

func TestListTools(t *testing.T) {
	s := newTestServer()
	tools := s.ListTools()

	// A core set that must always be present (tools are added over time, so we
	// assert a subset rather than a frozen count/order).
	core := []string{"deploy", "status", "list", "logs", "destroy", "list_zones", "list_images"}
	byName := make(map[string]Tool, len(tools))
	for _, tl := range tools {
		if _, dup := byName[tl.Name]; dup {
			t.Errorf("duplicate tool name %q", tl.Name)
		}
		byName[tl.Name] = tl
	}
	for _, name := range core {
		tl, ok := byName[name]
		if !ok {
			t.Errorf("core tool %q missing", name)
			continue
		}
		if tl.Description == "" {
			t.Errorf("tool %q: empty description", name)
		}
		if tl.InputSchema["type"] != "object" {
			t.Errorf("tool %q: inputSchema.type should be object", name)
		}
	}

	// Every declared tool must have a registered handler and vice versa.
	for name := range byName {
		if _, ok := s.handlers[name]; !ok {
			t.Errorf("tool %q declared but has no handler", name)
		}
	}
	if len(s.handlers) != len(tools) {
		t.Errorf("handler/definition count mismatch: %d handlers, %d tool defs", len(s.handlers), len(tools))
	}
}

func TestInitialize(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c","version":"1"}}}`)

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %v", resp)
	}
	// Server must echo the client's protocol version when supported.
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion: got %v, want echoed 2025-06-18", result["protocolVersion"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != serverName {
		t.Errorf("serverInfo.name: got %v, want %s", info["name"], serverName)
	}
	// The display name is the title; the name stays "evo" for clients
	// that key their config on it.
	if info["name"] != "evo" || info["title"] != "Cloud.ru Evolution CLI CREW edition" {
		t.Errorf("serverInfo: name %v, title %v", info["name"], info["title"])
	}
	if _, ok := result["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("capabilities.tools missing: %v", result["capabilities"])
	}
}

func TestInitializeDefaultVersion(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	result := resp["result"].(map[string]any)
	if result["protocolVersion"] != defaultProtocolVersion {
		t.Errorf("default protocolVersion: got %v, want %s", result["protocolVersion"], defaultProtocolVersion)
	}
}

func TestNotificationNoResponse(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if resp != nil {
		t.Errorf("notification should produce no response, got %v", resp)
	}
}

func TestPing(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":"abc","method":"ping"}`)
	// id must be preserved verbatim, including string ids.
	if resp["id"] != "abc" {
		t.Errorf("ping id: got %v, want abc", resp["id"])
	}
	if _, ok := resp["result"].(map[string]any); !ok {
		t.Errorf("ping result should be an object, got %v", resp["result"])
	}
}

func TestToolsList(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	result := resp["result"].(map[string]any)
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("expected a non-empty tools list, got %v", result["tools"])
	}
	first := tools[0].(map[string]any)
	if _, ok := first["inputSchema"]; !ok {
		t.Errorf("tool missing inputSchema key: %v", first)
	}
}

// Every tool carries MCP annotations: readOnlyHint is the opposite of
// Mutating, every *_delete and the listed destroying tools are destructive,
// a read is idempotent and never destructive. A tool revealing a secret
// (secret_value, cert_private_key) is not read-only, so the client asks the
// person, and says it destroys nothing (SEC-4).
func TestToolsList_Annotations(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	listed := resp["result"].(map[string]any)["tools"].([]any)
	mutating := map[string]bool{}
	sensitive := map[string]bool{}
	sideEffects := map[string]bool{}
	for _, tl := range tools.New(nil).Tools() {
		mutating[tl.Name] = tl.Mutating
		sensitive[tl.Name] = tools.IsSensitive(tl)
		sideEffects[tl.Name] = tools.HasSideEffects(tl)
	}
	for _, name := range []string{"stack_status", "k8s_kubeconfig"} {
		if !sideEffects[name] || mutating[name] {
			t.Errorf("%s: side effects=%v mutating=%v, want a read with side effects", name, sideEffects[name], mutating[name])
		}
	}
	for _, name := range []string{"secret_value", "cert_private_key"} {
		if !sensitive[name] || mutating[name] {
			t.Errorf("%s: sensitive=%v mutating=%v, want a sensitive read", name, sensitive[name], mutating[name])
		}
	}
	destructive := map[string]bool{"destroy": true, "stack_destroy": true, "release_floating_ip": true,
		"s3_rm": true, "s3_put": true, "k8s_kubectl": true, "k8s_apply": true, "k8s_helm": true, "k8s_add_addon": true}
	seen := map[string]bool{}
	for _, raw := range listed {
		tl := raw.(map[string]any)
		name := tl["name"].(string)
		seen[name] = true
		a, ok := tl["annotations"].(map[string]any)
		if !ok {
			t.Errorf("%s: no annotations", name)
			continue
		}
		// hint reads a hint; a missing one has its default from the spec.
		hint := func(k string, def bool) bool {
			if v, ok := a[k].(bool); ok {
				return v
			}
			return def
		}
		if sensitive[name] {
			if a["readOnlyHint"] == true || a["destructiveHint"] != false || a["idempotentHint"] != true {
				t.Errorf("%s: annotations %v, want readOnlyHint absent, destructiveHint false, idempotentHint true", name, a)
			}
			continue
		}
		// A read that is not read-only (A9, REL12-4): stack_status advances
		// the stack it shows, k8s_kubeconfig mints the cluster's token — a
		// client must not approve them as reads.
		if sideEffects[name] {
			if a["readOnlyHint"] == true || a["destructiveHint"] != false || a["idempotentHint"] == true {
				t.Errorf("%s: annotations %v, want readOnlyHint absent, destructiveHint false, idempotentHint absent", name, a)
			}
			continue
		}
		if hint("readOnlyHint", false) != !mutating[name] {
			t.Errorf("%s: readOnlyHint=%v, Mutating=%v", name, a["readOnlyHint"], mutating[name])
		}
		if mutating[name] {
			// The default is true: a mutation must say it explicitly.
			if _, ok := a["destructiveHint"].(bool); !ok {
				t.Errorf("%s: mutation without destructiveHint", name)
			}
			want := strings.HasSuffix(name, "_delete") || destructive[name]
			if hint("destructiveHint", true) != want {
				t.Errorf("%s: destructiveHint=%v, want %v", name, a["destructiveHint"], want)
			}
		}
		if hint("idempotentHint", false) != !mutating[name] {
			t.Errorf("%s: idempotentHint=%v, want %v", name, a["idempotentHint"], !mutating[name])
		}
		// The default (true) is what every evo tool is; false would be wrong.
		if !hint("openWorldHint", true) {
			t.Errorf("%s: openWorldHint=%v, want the default true", name, a["openWorldHint"])
		}
	}
	for name := range destructive {
		if !seen[name] || !mutating[name] {
			t.Errorf("destructive tool %q is not a mutating tool of the registry (renamed?)", name)
		}
	}
}

func TestMethodNotFound(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":3,"method":"bogus/method"}`)
	rpcErr, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error object, got %v", resp)
	}
	if rpcErr["code"].(float64) != codeMethodNotFound {
		t.Errorf("expected code %d, got %v", codeMethodNotFound, rpcErr["code"])
	}
}

// Релизный аудит, R-07: неизвестный инструмент — ошибка протокола -32602
// (MCP 2025-06-18, Tools → Error Handling), а не результат с isError.
func TestUnknownToolIsProtocolError(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope","arguments":{}}}`)
	rpcErr, ok := resp["error"].(map[string]any)
	if !ok || resp["result"] != nil {
		t.Fatalf("expected a JSON-RPC error, got %v", resp)
	}
	if rpcErr["code"].(float64) != codeInvalidParams || rpcErr["message"] != "Unknown tool: nope" {
		t.Errorf("unknown tool: %v", rpcErr)
	}
}

func TestParseError(t *testing.T) {
	s := newTestServer()
	resp := roundTrip(t, s, `{not json`)
	rpcErr, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected parse error, got %v", resp)
	}
	if rpcErr["code"].(float64) != codeParseError {
		t.Errorf("expected parse error code %d, got %v", codeParseError, rpcErr["code"])
	}
}

// TestSingleLineFraming ensures each response is a single newline-terminated
// line with no embedded raw newlines (MCP stdio transport requirement).
func TestSingleLineFraming(t *testing.T) {
	s := newTestServer()
	out := &bytes.Buffer{}
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n")
	if err := s.Run(in, out); err != nil {
		t.Fatal(err)
	}
	data := out.Bytes()
	if data[len(data)-1] != '\n' {
		t.Error("response must be newline-terminated")
	}
	if bytes.Count(data, []byte("\n")) != 1 {
		t.Errorf("response must be a single line, found %d newlines", bytes.Count(data, []byte("\n")))
	}
}
