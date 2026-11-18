// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// A string the static rules of the schema refuse (D36: format uuid or
// cidr, an enum) is refused by its argument and the rule, never by its
// text: a secret given where an id goes would reach the output, a model
// and a log before consent (#2437, Qwen). Every such argument of every
// tool, through CheckArgs, the one point; in either language («Риск»,
// conclusion 378: a mutant that put the value in the Russian text only
// lived).
func TestCheckArgs_RuleRefusalDoesNotEchoTheValue(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for _, lang := range []string{i18n.EN, i18n.RU} {
		i18n.SetLang(lang)
		t.Run(lang, checkRuleRefusals)
	}
}

func checkRuleRefusals(t *testing.T) {
	const secret = "s3cr3t-rule-61803398874989"
	checked := 0
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name, p := range props {
			pm, _ := p.(map[string]any)
			// A list of such strings is checked by the rules of its
			// items (ar list --statuses, backup list --states).
			rule, v := pm, any(secret)
			if pm["type"] == "array" {
				rule, _ = pm["items"].(map[string]any)
				v = []any{"x", secret}
			}
			_, enum := rule["enum"]
			if rule["type"] != "string" || (rule["format"] != "uuid" && rule["format"] != "cidr" && !enum) {
				continue
			}
			err := CheckArgs(tl, map[string]any{name: v})
			if err == nil {
				continue
			}
			checked++
			if strings.Contains(strings.ToLower(err.Error()), "s3cr3t") {
				t.Errorf("%s %s: the refusal echoes the value: %v", tl.Name, name, err)
			}
		}
	}
	// A filter of a list by several states checks each value by the same
	// rule in the handler, before the list is read (enumValue).
	r := New(nil)
	for _, c := range []struct{ tool, arg string }{{"ar_list", "statuses"}, {"backup_list", "states"}} {
		_, text, isErr := callNamed(t, r, c.tool, map[string]any{c.arg: []any{secret}})
		checked++
		if !isErr || !strings.Contains(text, c.arg) || strings.Contains(strings.ToLower(text), "s3cr3t") {
			t.Errorf("%s %s: want a refusal naming the argument, not the value: %s", c.tool, c.arg, text)
		}
	}
	if checked < 90 {
		t.Fatalf("only %d refusals checked: the walk lost the schema rules", checked)
	}
	checkNumberRefusals(t)
}

// A number the bounds of the schema refuse — minimum, maximum, the range
// of an int32 — is refused by its argument and the bound, never by the
// number: a secret given where a size goes reached stderr, the --json
// envelope and the answer over MCP before consent (Астра #2576; the
// owner's rule of #2459: a refusal of form does not repeat the value).
// Every integer and number argument of every tool and every integer item
// of a list, both signs.
func checkNumberRefusals(t *testing.T) {
	const digits = "27183650904512"
	checked := 0
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name, p := range props {
			pm, _ := p.(map[string]any)
			for _, n := range []float64{27183650904512, -27183650904512} {
				var v any
				switch {
				case pm["type"] == "integer" || pm["type"] == "number":
					v = n
				case pm["type"] == "array":
					if items, _ := pm["items"].(map[string]any); items["type"] == "integer" {
						v = []any{float64(1), n}
					}
				}
				if v == nil {
					continue
				}
				err := CheckArgs(tl, map[string]any{name: v})
				if err == nil {
					continue
				}
				checked++
				if strings.Contains(err.Error(), digits) {
					t.Errorf("%s %s = %v: the refusal echoes the number: %v", tl.Name, name, v, err)
				}
				if !strings.Contains(err.Error(), name) {
					t.Errorf("%s %s = %v: the refusal does not name the argument: %v", tl.Name, name, v, err)
				}
			}
		}
	}
	if checked < 100 {
		t.Fatalf("only %d number refusals checked: the walk lost the bounds", checked)
	}
}
