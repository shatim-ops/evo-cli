// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/shatim-ops/evo-cli/internal/httpretry"
)

// Limits of the descriptions in tools/list that some MCP clients cut to
// without a word (found live, FM-2): 1024 characters of a tool's
// description, 512 of a property's. A cut description loses its end, where
// a tool may say it needs consent. Counted in bytes, which is never less
// than in characters or UTF-16 units, whichever a client counts.
const (
	maxToolDescription     = 1024
	maxPropertyDescription = 512
)

// What tools/list sends, the notes the registry adds included, stays within
// the limits: every tool's description and every property's, nested ones
// (items, properties of an object) too.
func TestToolsList_DescriptionLimits(t *testing.T) {
	longest, longestProp := 0, 0
	for _, tl := range newTestServer(t).ListTools() {
		if n := len(tl.Description); n > maxToolDescription {
			t.Errorf("%s: description of %d bytes, limit %d", tl.Name, n, maxToolDescription)
		} else {
			longest = max(longest, n)
		}
		propertyDescriptions(tl.Name, tl.InputSchema, func(path, d string) {
			if n := len(d); n > maxPropertyDescription {
				t.Errorf("%s: description of %d bytes, limit %d", path, n, maxPropertyDescription)
			} else {
				longestProp = max(longestProp, n)
			}
		})
	}
	t.Logf("longest description within the limits: tool %d bytes, property %d", longest, longestProp)
}

// maxToolsListBytes is the budget of the whole tools/list: what an agent
// reads before its first call, in its context window. 154 tools are about
// 117 KB (about 29 thousand tokens) at 1.2.2; the budget leaves room for
// the groups of 1.3–1.4, and running into it means toolsets (evo mcp
// --toolsets, 1.3) or shorter descriptions, not a higher number (process
// audit 1.2.1, PR 5). Bytes, not time: the same on any runner.
const maxToolsListBytes = 160 << 10

func TestToolsList_SizeBudget(t *testing.T) {
	tools := newTestServer(t).ListTools()
	b, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxToolsListBytes {
		t.Errorf("tools/list: %d bytes for %d tools, budget %d: split into toolsets or shorten the descriptions", len(b), len(tools), maxToolsListBytes)
	}
	// The ratchet under the budget is not a number here (it made every PR
	// that touches a description conflict with the one before it): the
	// workflow tools-list.yml compares tools/list of the PR with its base,
	// puts the growth in the run's summary, and wants «tools/list: рост —
	// <причина>» in the description when it grows (owner's decisions 29.10,
	// #597, item 12, and 04.11); it runs this test on the PR's head.
	t.Logf("tools/list: %d bytes for %d tools, budget %d", len(b), len(tools), maxToolsListBytes)
}

// propertyDescriptions calls f with the path and the description of every
// property of schema, at any depth.
func propertyDescriptions(path string, schema map[string]any, f func(path, description string)) {
	if props, ok := schema["properties"].(map[string]any); ok {
		for name, raw := range props {
			p, _ := raw.(map[string]any)
			at := path + "." + name
			if d, ok := p["description"].(string); ok {
				f(at, d)
			}
			propertyDescriptions(at, p, f)
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if sub, ok := schema[key].(map[string]any); ok {
			at := fmt.Sprintf("%s[%s]", path, key)
			if d, ok := sub["description"].(string); ok {
				f(at, d)
			}
			propertyDescriptions(at, sub, f)
		}
	}
}

// One slow delete occupies the last available HTTP call slot. After 45 seconds
// its session stays open, but another session can use that slot for a read.
func TestCAWait_HTTPBudgetFreesSlotForAnotherSession(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		deletes, reads := 0, 0
		old := httpretry.DefaultBase
		httpretry.DefaultBase = roundTripper(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/v2/containers/web" {
				return nil, fmt.Errorf("unexpected app path %q", r.URL.Path)
			}
			var body string
			switch r.Method {
			case http.MethodDelete:
				deletes++
				body = `{"done":false}`
			case http.MethodGet:
				reads++
				body = `{"name":"web","status":"DELETING"}`
			default:
				return nil, fmt.Errorf("unexpected app method %s", r.Method)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		defer func() { httpretry.DefaultBase = old }()

		h := NewHTTPHandler(provisionServer(t, &provisionCloud{}), HTTPOptions{})
		defer h.Close()
		h.calls = make(chan struct{}, 1)
		writer, reader := h.newSession(), h.newSession()
		call := func(ctx context.Context, session, params string) bool {
			req := &rpcRequest{JSONRPC: "2.0", Method: "tools/call", ID: json.RawMessage("1"), Params: json.RawMessage(params)}
			answer, _ := h.dispatch(ctx, session, req)
			result, ok := answer.Result.(map[string]any)
			return ok && result["isError"] == false
		}

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		var accepted bool
		go func() {
			defer close(done)
			accepted = call(ctx, writer, `{"name":"ca_delete","arguments":{"name":"web","wait":true,"confirmed":true}}`)
		}()
		defer func() { cancel(); <-done }()
		synctest.Wait()
		if deletes != 1 || len(h.calls) != 1 {
			t.Fatalf("active call: deletes=%d, slots in use=%d", deletes, len(h.calls))
		}
		time.Sleep(45 * time.Second)
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("the delete still holds the HTTP call at the 45-second budget")
		}
		if !accepted || len(h.calls) != 0 {
			t.Fatalf("expired wait: success=%v, slots in use=%d", accepted, len(h.calls))
		}
		before := reads
		if !call(t.Context(), reader, `{"name":"ca_get","arguments":{"name":"web"}}`) || reads != before+1 || deletes != 1 {
			t.Fatalf("other session: additional reads=%d, total deletes=%d", reads-before, deletes)
		}
	})
}
