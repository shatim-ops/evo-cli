// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// Release audit 1.8, L7 (Grok R1b №25): whoami said «Projects: unknown
// (the Organization API does not list projects for this key)» — the name
// of an API read as a failure. The line says what it means for the person:
// a service account key is not given the list, and that is no error.
func TestScopeText_UnknownSaysWhyNotAnAPI(t *testing.T) {
	prev := i18n.Lang()
	defer i18n.SetLang(prev)
	for _, lang := range []string{"en", "ru"} {
		i18n.SetLang(lang)
		s := ScopeText(0, false)
		if strings.Contains(s, "Organization API") || !(strings.Contains(s, "service account") || strings.Contains(s, "сервисного аккаунта")) ||
			!(strings.Contains(s, "not an error") || strings.Contains(s, "не ошибка")) {
			t.Errorf("%s: %q", lang, s)
		}
	}
}
