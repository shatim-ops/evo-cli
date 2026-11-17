// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// deploy's refusal of a VM name another VM has: no status from the cloud is
// evo's word, not in quotes; the cloud's "unknown" stays quoted (Астра,
// #2153).
func TestForeignDeployVM_EmptyStatusIsEvosWord(t *testing.T) {
	t.Cleanup(func() { i18n.SetLang(i18n.EN) })
	for lang, w := range map[string]struct{ empty, literal string }{
		i18n.EN: {"status unknown", `status "unknown"`},
		i18n.RU: {"статус неизвестен", `статус "unknown"`},
	} {
		i18n.SetLang(lang)
		msg := foreignDeployVM(&provision.VM{ID: "vm-1", Name: "web"}).Error()
		if !strings.Contains(msg, w.empty) || strings.Contains(msg, `"unknown"`) {
			t.Errorf("%s, no status: %q, want %q, not in quotes", lang, msg, w.empty)
		}
		if msg := foreignDeployVM(&provision.VM{ID: "vm-1", Name: "web", Status: "unknown"}).Error(); !strings.Contains(msg, w.literal) {
			t.Errorf("%s, the cloud's status: %q, want %q", lang, msg, w.literal)
		}
	}
}
