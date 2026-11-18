// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// handlerArgValue is a value that passes the schema for p, to fill the
// arguments a call requires besides the one under test.
func handlerArgValue(p map[string]any) any {
	if e, ok := p["enum"].([]any); ok && len(e) > 0 {
		return e[0]
	}
	switch p["type"] {
	case "integer", "number":
		return float64(1)
	case "boolean":
		return false
	case "array":
		return []any{"x"}
	case "object":
		return map[string]any{}
	}
	if p["format"] == "uuid" {
		return qaID1
	}
	return "x"
}

// A refusal of the form of a value — a date, a duration, a port, a job id,
// a name the rules of its resource do not accept — does not repeat the
// value, in either language: a secret given in the wrong place would reach
// the output, a model and a log before consent (#2459; owner's rule: the
// form, without the value; a resource not found by its name may be named).
// Every string argument of every tool, past the schema (CheckArgs, #2437),
// into its handler, in a world with no answer from the cloud.
func TestHandlers_FormRefusalDoesNotEchoTheValue(t *testing.T) {
	// Upper case and "_" make it a name no resource takes.
	const marker = "s3cr3t_FORM"
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		rt := &contractRT{kind: "500"}
		reg := contractReg(t, rt)
		usage := 0
		for _, tl := range reg.Tools() {
			props, _ := tl.InputSchema["properties"].(map[string]any)
			base := map[string]any{}
			for _, req := range schemaRequired(tl) {
				pm, _ := props[req].(map[string]any)
				base[req] = handlerArgValue(pm)
			}
			names := make([]string, 0, len(props))
			for k := range props {
				names = append(names, k)
			}
			sort.Strings(names)
			for _, k := range names {
				pm, _ := props[k].(map[string]any)
				if pm["type"] != "string" {
					continue
				}
				args := map[string]any{}
				for a, v := range base {
					args[a] = v
				}
				args[k] = marker
				if CheckArgs(tl, args) != nil {
					continue // the schema's refusal: TestCheckArgs_RuleRefusalDoesNotEchoTheValue
				}
				text, isErr, _ := contractCall(tl, args, 300*time.Millisecond)
				if !isErr || !strings.Contains(text, `"USAGE"`) {
					continue
				}
				usage++
				if strings.Contains(strings.ToLower(text), "s3cr3t_form") {
					t.Errorf("%s: %s %s: the refusal repeats the value: %.300s", lang, tl.Name, k, text)
				}
			}
		}
		if usage < 5 {
			t.Fatalf("%s: only %d refusals of a handler: the walk lost the tools", lang, usage)
		}
	}
}
