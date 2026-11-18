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
}
