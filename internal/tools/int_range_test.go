// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"math"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/engine"
	clierrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// intArgs are the integer arguments of the registry: those of the top
// level and the items of a list of integers ("ports[0]"), with the schema
// of each.
func intArgs(t Tool) map[string]map[string]any {
	out := map[string]map[string]any{}
	props, _ := t.InputSchema["properties"].(map[string]any)
	for k, raw := range props {
		p, _ := raw.(map[string]any)
		if p["type"] == "integer" {
			out[k] = p
		}
		if items, _ := p["items"].(map[string]any); items["type"] == "integer" {
			out[k+"[0]"] = items
		}
	}
	return out
}

// An integer outside 32 bits is refused by the one check of the arguments
// of every tool, before the handler: 1e30 passed as "an integer" and became
// a negative int in intArg, and 2^31 went on to the cloud (release plan
// 1.9, «Целые аргументы по MCP»; «Риск», conclusion 171). The refusal is
// USAGE and names the argument; no engine is built for it.
func TestIntegerArgs_OutOf32BitsRefusedBeforeTheHandler(t *testing.T) {
	reg := New(func(map[string]any) (*engine.Engine, error) {
		t.Error("an engine is built for an integer out of range")
		return nil, stderrors.New("no engine")
	})
	outside := map[string]any{
		"1e30":            1e30,
		"2^31":            float64(math.MaxInt32) + 1,
		"-2^31-1":         float64(math.MinInt32) - 1,
		"longer than i64": json.Number("99999999999999999999999"),
		"a Go int64":      int64(math.MaxInt32) + 1,
	}
	checked := 0
	for _, tool := range reg.Tools() {
		for arg, p := range intArgs(tool) {
			checked++
			name, inList := strings.CutSuffix(arg, "[0]")
			for label, v := range outside {
				var value any = v
				if inList {
					value = []any{v}
				}
				text, isErr := tool.Handler(context.Background(), map[string]any{name: value})
				var a struct{ Reason, Argument string }
				_ = json.Unmarshal([]byte(text), &a)
				if !isErr || a.Reason != clierrors.CodeUsage || a.Argument != arg {
					t.Errorf("%s %s=%s: isErr %v, reason %q, argument %q; want a USAGE refusal naming %s\n%s",
						tool.Name, arg, label, isErr, a.Reason, a.Argument, arg, text)
				}
			}
			// The edge of the range is an integer as before: only a
			// narrower rule of the schema refuses it.
			err := ruleArg(arg, p, float64(math.MaxInt32))
			max, ruled := p["maximum"].(int)
			if narrower := ruled && max < math.MaxInt32; (err != nil) != narrower {
				t.Errorf("%s %s=2^31-1: %v, a narrower maximum in the schema: %v", tool.Name, arg, err, narrower)
			}
		}
	}
	if checked < 60 {
		t.Fatalf("%d integer arguments checked: the registry has more", checked)
	}
}

// A narrower rule of the schema refuses in its own words, as before the
// range of 32 bits was checked («Риск», conclusion 171, У2).
func TestIntegerArgs_TheSchemasNarrowerRuleSpeaksFirst(t *testing.T) {
	tool, _ := New(nil).Lookup("vpc_operation")
	for _, v := range []any{float64(46), 1e30} {
		err := CheckArgs(tool, map[string]any{"operation_id": "00000000-0000-4000-8000-000000000001", "wait_seconds": v})
		if err == nil || !strings.Contains(err.Error(), "is above the maximum 45") {
			t.Errorf("wait_seconds=%v: %v, want the schema's «above the maximum 45»", v, err)
		}
	}
}
