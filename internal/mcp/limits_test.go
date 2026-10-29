// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"fmt"
	"testing"
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
	for _, tl := range New(nilFactory, "test").ListTools() {
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

// toolsListBytes is the size of tools/list now (29.10, 157 tools, about 30
// thousand tokens); toolsListSlack is how much it may shrink before the
// number must follow.
const (
	toolsListBytes = 119998
	toolsListSlack = 1024
)

func TestToolsList_SizeBudget(t *testing.T) {
	tools := New(nilFactory, "test").ListTools()
	b, err := json.Marshal(tools)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > maxToolsListBytes {
		t.Errorf("tools/list: %d bytes for %d tools, budget %d: split into toolsets or shorten the descriptions", len(b), len(tools), maxToolsListBytes)
	}
	// The ratchet under the budget (process backlog P7, release plan 1.5):
	// tools/list does not grow by the way. A PR that makes it larger raises
	// toolsListBytes in its diff, with why in its description; one that
	// makes it smaller by more than toolsListSlack lowers it.
	switch n := len(b); {
	case n > toolsListBytes:
		t.Errorf("tools/list grew to %d bytes from %d (+%d): raise toolsListBytes in this PR and say why in its description, or shorten", n, toolsListBytes, n-toolsListBytes)
	case n < toolsListBytes-toolsListSlack:
		t.Errorf("tools/list shrank to %d bytes from %d: lower toolsListBytes to %d — the ratchet keeps what was won", n, toolsListBytes, n)
	}
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
