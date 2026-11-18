// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// A string the static rules of the schema refuse (D36: format uuid or
// cidr, an enum) is refused by its argument and the rule, never by its
// text: a secret given where an id goes would reach the output, a model
// and a log before consent (#2437, Qwen). Every such argument of every
// tool, through CheckArgs, the one point.
func TestCheckArgs_RuleRefusalDoesNotEchoTheValue(t *testing.T) {
	const secret = "s3cr3t-rule-61803398874989"
	checked := 0
	for _, tl := range New(nil).Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name, p := range props {
			pm, _ := p.(map[string]any)
			_, enum := pm["enum"]
			if pm["type"] != "string" || (pm["format"] != "uuid" && pm["format"] != "cidr" && !enum) {
				continue
			}
			err := CheckArgs(tl, map[string]any{name: secret})
			if err == nil {
				continue
			}
			checked++
			if strings.Contains(strings.ToLower(err.Error()), "s3cr3t") {
				t.Errorf("%s %s: the refusal echoes the value: %v", tl.Name, name, err)
			}
		}
	}
	if checked < 90 {
		t.Fatalf("only %d refusals checked: the walk lost the schema rules", checked)
	}
}
