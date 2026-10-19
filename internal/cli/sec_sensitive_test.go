// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// SEC-4: «evo secret value» ведёт себя как «evo tools call cert_private_key»:
// без терминала и без --yes — превью и код 3, секрет не запрашивается;
// --yes проходит подтверждение и идёт в облако (здесь — без учётных
// данных, код 2).
func TestSecretValue_AsksForConfirmation(t *testing.T) {
	for _, args := range [][]string{
		{"secret", "value", "app/db"},
		{"tools", "call", "secret_value", "--arg", "path=app/db"},
		{"tools", "call", "cert_private_key", "--arg", "certificate_id=00000000-0000-0000-0000-000000000001"},
	} {
		_, errOut, code := execCLI(t, args...)
		if code != clerrors.ExitConfirmationRequired || !strings.Contains(errOut, "confirmed=true") {
			t.Errorf("%v без --yes: exit %d, want 3 (%s)", args, code, errOut)
		}
		out, _, code := execCLI(t, append(args, "--json")...)
		if code != clerrors.ExitConfirmationRequired || !strings.Contains(out, `"confirmation_required"`) {
			t.Errorf("%v --json: exit %d, out %q", args, code, out)
		}
		out, errOut, code = execCLI(t, append(args, "--yes")...)
		if code != clerrors.ExitAuth {
			t.Errorf("%v --yes: exit %d, want 2 (нет учётных данных): %s %s", args, code, out, errOut)
		}
	}
	out, _, code := execCLI(t, "secret", "value", "--help")
	if code != 0 || !strings.Contains(out, "--yes") {
		t.Errorf("справка secret value не говорит о подтверждении: %s", out)
	}
}
