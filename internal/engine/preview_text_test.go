// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"
)

// The rule of PreviewText holds for the whole value a preview prints, not
// only a string leaf: a key of a map, a typed list or map, a list of the
// JSON («Риск», conclusion 234 to #1848).
func TestPreviewText_WholeValue(t *testing.T) {
	const bad = "a\nPROBE\u202e"
	for name, v := range map[string]any{
		"string":           bad,
		"list":             []any{"ok", bad},
		"key of a map":     map[string]any{bad: "v"},
		"value of a map":   map[string]any{"k": bad},
		"typed list":       []string{"ok", bad},
		"typed map":        map[string]string{"k": bad},
		"nested in a list": []any{map[string]any{"k": []any{bad}}},
	} {
		got := paramValue(v)
		if strings.ContainsAny(got, "\n\u202e") {
			t.Errorf("%s: %q prints a control or format character raw", name, got)
		}
		if !strings.Contains(got, `\n`) {
			t.Errorf("%s: %q does not show the line break escaped", name, got)
		}
	}
	if got := paramValue("plain value"); got != "plain value" {
		t.Errorf("a plain value changed: %q", got)
	}
}
