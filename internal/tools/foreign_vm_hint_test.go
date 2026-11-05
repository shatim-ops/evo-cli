// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Agent run of 1.7.0 (c1, c0, m0, m2; findings 2.1, №9): the hint of
// provision to a VM name another VM has told the agent «delete it
// yourself», while CHANGELOG 1.7.0 promised «only another name»: the VM
// may be someone else's. Owner's decision 05.11: another name, or ask the
// person whether to delete their VM — a delete is the person's call, in
// both languages.
func TestForeignVMHint_SaysDeleteIsThePersonsCall(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for lang, w := range map[string]struct{ ask, notSelf string }{
		i18n.EN: {"ask the person", "delete it yourself"},
		i18n.RU: {"спросите человека", "удалите её сами"},
	} {
		i18n.SetLang(lang)
		text, _ := foreignVMResult("web", "vm-1", "running")
		m := parseAnswer(t, text)
		hint, _ := m["hint"].(string)
		if strings.Contains(hint, w.notSelf) || !strings.Contains(hint, w.ask) || !strings.Contains(hint, "name") {
			t.Errorf("%s: hint %q: want another name (name) or ask the person, not a delete by the agent", lang, hint)
		}
		if m["reason"] != clerrors.CodeUsage {
			t.Errorf("%s: reason %v, want USAGE as before", lang, m["reason"])
		}
	}
}
