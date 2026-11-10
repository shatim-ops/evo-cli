// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"errors"
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
)

// B-5 (г), the owner's decision 10.11 (#1142): a profile of evo login has
// no zone, and the refusal sent the person to 'evo configure', which asks
// for the key again. The hint names the zone in the call (a name, the list
// in 'evo vm zones') or region in .evo.yaml — no evo configure.
func TestNoZoneError_HintWithoutConfigure(t *testing.T) {
	for _, lang := range []string{i18n.EN, i18n.RU} {
		old := i18n.Lang()
		i18n.SetLang(lang)
		var ce *clerrors.CLIError
		if !errors.As(NoZoneError("zone"), &ce) {
			t.Fatal("not a CLIError")
		}
		i18n.SetLang(old)
		for _, want := range []string{"zone", "ru.AZ-2", "evo vm zones", ".evo.yaml"} {
			if !strings.Contains(ce.Hint, want) {
				t.Errorf("%s: the hint does not name %q: %s", lang, want, ce.Hint)
			}
		}
		if strings.Contains(ce.Hint, "configure") {
			t.Errorf("%s: the hint sends to evo configure: %s", lang, ce.Hint)
		}
	}
}
