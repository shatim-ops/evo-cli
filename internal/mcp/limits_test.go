// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
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
