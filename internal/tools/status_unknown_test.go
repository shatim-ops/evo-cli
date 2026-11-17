// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/deployplan"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// A VM the cloud answers with no status: evo says so in its own words, not
// in quotes — quotes mark the cloud's value, and a quoted "unknown" read as
// the status the cloud gave (Астра, #2153). A status the cloud does give,
// "unknown" among them, stays in quotes.
func TestVMNameTaken_EmptyStatusIsEvosWord(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for lang, w := range map[string]struct{ empty, literal string }{
		i18n.EN: {"status unknown", `status "unknown"`},
		i18n.RU: {"статус неизвестен", `статус "unknown"`},
	} {
		i18n.SetLang(lang)
		for name, answer := range map[string]func(string) (string, bool){
			"foreignVMResult":  func(st string) (string, bool) { return foreignVMResult("web", "vm-1", st) },
			"recordedVMResult": func(st string) (string, bool) { return recordedVMResult("web", "vm-1", st) },
			"ForeignVMError": func(st string) (string, bool) {
				return toJSON(map[string]any{"error": (&deployplan.ForeignVMError{ID: "vm-1", Name: "web", Status: st}).Error()}), true
			},
		} {
			text, _ := answer("")
			msg, _ := parseAnswer(t, text)["error"].(string)
			if !strings.Contains(msg, w.empty) || strings.Contains(msg, `"unknown"`) {
				t.Errorf("%s %s, no status: %q, want %q, not in quotes", lang, name, msg, w.empty)
			}
			text, _ = answer("unknown")
			msg, _ = parseAnswer(t, text)["error"].(string)
			if !strings.Contains(msg, w.literal) {
				t.Errorf("%s %s, the cloud's status \"unknown\": %q, want %q", lang, name, msg, w.literal)
			}
		}
	}
}
