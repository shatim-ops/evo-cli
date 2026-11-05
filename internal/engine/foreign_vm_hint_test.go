// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// The hint of deploy to a VM name another VM has, as provision's
// (findings 2.1, №9; owner's decision 05.11): another project_name, or
// ask the person whether to delete their VM — never «delete it yourself».
func TestDeployNameHint_SaysDeleteIsThePersonsCall(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for lang, w := range map[string]struct{ ask, notSelf string }{
		i18n.EN: {"ask the person", "delete it yourself"},
		i18n.RU: {"спросите человека", "удалите её сами"},
	} {
		i18n.SetLang(lang)
		hint := deployNameHint()
		if strings.Contains(hint, w.notSelf) || !strings.Contains(hint, w.ask) || !strings.Contains(hint, "project_name") {
			t.Errorf("%s: hint %q: want another project_name or ask the person, not a delete by the agent", lang, hint)
		}
	}
}
