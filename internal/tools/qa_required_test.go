// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// requiredRE is how a handler says an argument is missing.
var requiredRE = regexp.MustCompile(`(?i)\b(is|are) required\b|\brequires\b|\bprovide (either )?\b`)

func usageRequired(text string) (bool, string) {
	var m map[string]any
	if json.Unmarshal([]byte(text), &m) != nil {
		return false, ""
	}
	msg, _ := m["error"].(string)
	return m["reason"] == clerrors.CodeUsage && requiredRE.MatchString(msg), msg
}

// QA-4 (QA 20.10): схема не говорила, что аргумент обязателен, а вызов без
// него отвечал USAGE «… is required» (pg_get, snat_create, open_ports, …).
// Каждый из 152 инструментов вызывается с пустыми аргументами (облако —
// транспорт, отвечающий 500): «required» в ответе — только у инструмента с
// required или с набором «одно из» (Tool.RequiredOneOf). Затем — только с
// аргументами из required и первым из каждого набора: «required» в ответе
// значит, что required неполон.
func TestQA4_RequiredMatchesHandlers(t *testing.T) {
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	n := 0
	for _, tl := range reg.Tools() {
		n++
		declared := len(schemaRequired(tl)) > 0 || len(tl.RequiredOneOf) > 0
		text, _, pan := contractCall(tl, nil, 300*time.Millisecond)
		if pan != "" {
			t.Errorf("%s: panic on empty arguments", tl.Name)
			continue
		}
		if req, msg := usageRequired(text); req && !declared {
			t.Errorf("%s: empty arguments answer %q, but the schema has no required and no one-of set", tl.Name, msg)
		}
		if !declared {
			continue
		}
		full := contractArgs(tl)
		for k, raw := range schemaProps(tl) {
			// contractArgs gives every array [80]: an array of strings
			// (dns_names) needs a string.
			p, _ := raw.(map[string]any)
			if items, _ := p["items"].(map[string]any); p["type"] == "array" && items["type"] == "string" {
				full[k] = []any{"web.example.invalid"}
			}
		}
		args := map[string]any{}
		for _, k := range schemaRequired(tl) {
			args[k] = full[k]
		}
		for _, set := range tl.RequiredOneOf {
			args[set[0]] = full[set[0]]
		}
		text, _, pan = contractCall(tl, args, 300*time.Millisecond)
		if pan != "" {
			t.Errorf("%s: panic on required arguments", tl.Name)
			continue
		}
		if req, msg := usageRequired(text); req {
			t.Errorf("%s args=%v: still %q — required is incomplete", tl.Name, args, msg)
		}
	}
	if n < 150 {
		t.Fatalf("only %d tools", n)
	}
}

// A tool with a one-of set answers USAGE for {} before the cloud, as a
// tool with required does (TestContract_EmptyRequiredIsUsage).
func TestQA4_EmptyOneOfIsUsage(t *testing.T) {
	rt := &contractRT{kind: "500"}
	reg := contractReg(t, rt)
	for name := range requiredOneOf {
		tl, ok := reg.Lookup(name)
		if !ok {
			t.Errorf("requiredOneOf names %s, not a tool", name)
			continue
		}
		for _, args := range []map[string]any{nil, {"confirmed": true}} {
			rt.take()
			text, isErr, pan := contractCall(tl, args, 200*time.Millisecond)
			reason, _ := contractReason(text)
			if pan != "" || !isErr || reason != clerrors.CodeUsage || len(rt.take()) != 0 {
				t.Errorf("%s args=%v: err=%v reason=%s text=%.160s", name, args, isErr, reason, text)
			}
		}
	}
}

// The schemas stay what every MCP client takes: no anyOf/oneOf/allOf at
// the top level (the Anthropic API refuses such a tool with 400 for the
// whole request). The one-of sets name properties of the tool, none of
// them in required.
func TestQA4_SchemaHasNoTopLevelComposition(t *testing.T) {
	reg := New(func(map[string]any) (*engine.Engine, error) { return nil, nil })
	for _, tl := range reg.Tools() {
		for _, k := range []string{"anyOf", "oneOf", "allOf", "not"} {
			if _, ok := tl.InputSchema[k]; ok {
				t.Errorf("%s: top-level %s in inputSchema", tl.Name, k)
			}
		}
		props := schemaProps(tl)
		req := map[string]bool{}
		for _, r := range schemaRequired(tl) {
			req[r] = true
		}
		for _, set := range tl.RequiredOneOf {
			if len(set) < 2 {
				t.Errorf("%s: one-of set %v", tl.Name, set)
			}
			for _, a := range set {
				if _, ok := props[a]; !ok {
					t.Errorf("%s: one-of %q is not a property", tl.Name, a)
				}
				if req[a] {
					t.Errorf("%s: one-of %q is also required", tl.Name, a)
				}
			}
		}
	}
}
