// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "testing"

// The top-level schema which the agent sees and the argument check used by
// the handler must both reject an unknown property. confirmed is a reserved
// argument accepted even when a read-only tool needs no confirmation.
func TestRootSchemaMatchesArgumentCheck(t *testing.T) {
	registry := newTestServer(t)
	for _, tool := range registry.Tools() {
		t.Run(tool.Name, func(t *testing.T) {
			if tool.InputSchema["additionalProperties"] != false {
				t.Error("root schema permits undeclared properties")
			}
			values := contractArgs(tool)
			args := map[string]any{"confirmed": true}
			for _, required := range schemaRequired(tool) {
				args[required] = values[required]
			}
			if err := CheckArgs(tool, args); err != nil {
				t.Fatalf("argument check rejected the valid sample: %v", err)
			}
			args["undeclared_test_argument"] = true
			if err := CheckArgs(tool, args); err == nil {
				t.Error("argument check accepted the same sample with an undeclared property")
			}
		})
	}
}

func TestRootSchemaLeavesStringDictionariesOpen(t *testing.T) {
	registry := newTestServer(t)
	for _, tc := range []struct{ tool, field string }{
		{"ca_deploy", "env"},
		{"deploy_plan", "image_metadata"},
		{"provision", "image_metadata"},
	} {
		t.Run(tc.tool+"/"+tc.field, func(t *testing.T) {
			tool, ok := registry.Lookup(tc.tool)
			if !ok {
				t.Fatalf("tool %q missing", tc.tool)
			}
			props, _ := tool.InputSchema["properties"].(map[string]any)
			field, _ := props[tc.field].(map[string]any)
			if field["type"] != "object" {
				t.Fatalf("%s is not an object: %v", tc.field, field)
			}
			// If omitted, additionalProperties allows extra entries by default.
			if constraint, present := field["additionalProperties"]; present {
				switch v := constraint.(type) {
				case bool:
					if !v {
						t.Errorf("%s rejects extra entries", tc.field)
					}
				case map[string]any:
					if typ, set := v["type"]; set && typ != "string" {
						t.Errorf("%s does not admit string values: %v", tc.field, v)
					}
				default:
					t.Errorf("invalid additionalProperties on %s: %v", tc.field, v)
				}
			}
			if err := CheckArgs(tool, map[string]any{tc.field: map[string]any{"EXAMPLE_KEY": "example"}}); err != nil {
				t.Errorf("argument check rejected a string dictionary: %v", err)
			}
		})
	}
}
