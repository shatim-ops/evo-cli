// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/tools"
)

// These calls have no required arguments. A client using tools/list and
// the server's own argument check should accept the reserved boolean on
// both a read and a mutation, while rejecting an unrelated boolean key.
// Evaluate just the object/boolean keywords used for these properties.
func TestPublishedSchemaAndValidatorAgreeOnConfirmed(t *testing.T) {
	const request = `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	server := newTestServer(t)
	response := roundTrip(t, server, request)
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var stdio, http struct{ Result struct{ Tools []Tool } }
	if err := json.Unmarshal(encoded, &stdio); err != nil {
		t.Fatal(err)
	}
	ts, _ := newHTTPTest(t, HTTPOptions{BearerToken: testToken})
	decodeBody(t, post(t, ts, request, nil), &http)
	for transport, published := range map[string][]Tool{"stdio": stdio.Result.Tools, "http": http.Result.Tools} {
		for _, name := range []string{"pg_list", "destroy"} {
			t.Run(transport+"/"+name, func(t *testing.T) {
				var schema map[string]any
				for _, row := range published {
					if row.Name == name {
						schema = row.InputSchema
						break
					}
				}
				tool, ok := server.reg.Lookup(name)
				if !ok || schema == nil {
					t.Fatalf("missing %s in registry or tools/list", name)
				}
				for _, key := range []string{"confirmed", "undeclared_test_argument"} {
					want := key == "confirmed"
					if advertised := rootAdmitsBoolean(t, schema, key); advertised != want {
						t.Errorf("schema accepts %s: %v, want %v", key, advertised, want)
					}
					if accepted := tools.CheckArgs(tool, map[string]any{key: true}) == nil; accepted != want {
						t.Errorf("validator accepts %s: %v, want %v", key, accepted, want)
					}
				}
			})
		}
	}
}

func rootAdmitsBoolean(t *testing.T, schema map[string]any, key string) bool {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	rule, matched := props[key]
	rules := []any{}
	if matched {
		rules = append(rules, rule)
	}
	patterns, _ := schema["patternProperties"].(map[string]any)
	for pattern, spec := range patterns {
		ok, err := regexp.MatchString(pattern, key)
		if err != nil {
			t.Fatalf("invalid patternProperties expression %q: %v", pattern, err)
		}
		if ok {
			rules = append(rules, spec)
		}
	}
	if len(rules) == 0 {
		if extra, ok := schema["additionalProperties"]; ok {
			rules = append(rules, extra)
		}
	}
	for _, spec := range rules {
		switch v := spec.(type) {
		case bool:
			if !v {
				return false
			}
		case map[string]any:
			if typ, ok := v["type"]; ok && typ != "boolean" {
				return false
			}
		default:
			t.Fatalf("unexpected schema for %s: %v", key, v)
		}
	}
	return true
}
