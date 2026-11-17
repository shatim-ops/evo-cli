// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// argSecret stands for a secret a caller passed as a value of the wrong
// type: a number, a list, an object in place of the string.
const argSecret = "93718244560173"

// The refusal of an argument of the wrong type names the argument and the
// type it got, never the value: an argument may be a secret, and the
// refusal reaches a model and a log (Астра, #2241: secret_create value=…
// echoed the value, and with --dry-run too). One point: CheckArgs.
func TestCheckArgs_TypeRefusalDoesNotEchoTheValue(t *testing.T) {
	r := New(nil)
	for _, v := range []any{float64(93718244560173), []any{argSecret}, map[string]any{"v": argSecret}, true} {
		args := map[string]any{"path": "app/db", "value": v}
		_, text, isErr := callNamed(t, r, "secret_create", args)
		if !isErr || !strings.Contains(text, "value") {
			t.Fatalf("value %v: want a refusal that names the argument: %s", v, text)
		}
		if strings.Contains(text, argSecret) || strings.Contains(text, "9.3718") {
			t.Errorf("value %v: the refusal echoes it: %s", v, text)
		}
	}
	// A list argument and an item of it.
	for _, tl := range r.Tools() {
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for name, p := range props {
			pm, _ := p.(map[string]any)
			if pm["type"] != "array" {
				continue
			}
			if err := CheckArgs(tl, map[string]any{name: argSecret}); err != nil && strings.Contains(err.Error(), argSecret) {
				t.Errorf("%s %s: a list refusal echoes the value: %v", tl.Name, name, err)
			}
			if err := CheckArgs(tl, map[string]any{name: []any{map[string]any{"s": argSecret}}}); err != nil && strings.Contains(err.Error(), argSecret) {
				t.Errorf("%s %s: an item refusal echoes the value: %v", tl.Name, name, err)
			}
		}
	}
}
