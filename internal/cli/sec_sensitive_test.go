// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// SEC-4: «evo secret value» без терминала и без --yes — превью и код 3,
// секрет не запрашивается; --yes проходит подтверждение и идёт в облако
// (здесь — без учётных данных, код 2). «evo tools call cert_private_key»
// читает сертификат до вопроса (W6, D35): без учётных данных — код 2 уже
// без --yes, ключ не запрашивается.
func TestSecretValue_AsksForConfirmation(t *testing.T) {
	certKey := []string{"tools", "call", "cert_private_key", "--arg", "certificate_id=00000000-0000-0000-0000-000000000001"}
	for _, args := range [][]string{certKey, append(certKey[:len(certKey):len(certKey)], "--json")} {
		if out, errOut, code := execCLI(t, args...); code != clerrors.ExitAuth || strings.Contains(out+errOut, "confirmation_required") {
			t.Errorf("%v без учётных данных: exit %d, want 2 до вопроса: %s %s", args, code, out, errOut)
		}
	}
	for _, args := range [][]string{
		{"secret", "value", "app/db"},
		{"tools", "call", "secret_value", "--arg", "path=app/db"},
	} {
		_, errOut, code := execCLI(t, args...)
		// Согласие в CLI — --yes (подсказка ошибки; в --json — поле hint
		// ниже); confirmed=true — слово MCP, в CLI его нет (REQ-5, PAR-15).
		if code != clerrors.ExitConfirmationRequired || strings.Contains(errOut, "confirmed") {
			t.Errorf("%v без --yes: exit %d, want 3 (%s)", args, code, errOut)
		}
		out, _, code := execCLI(t, append(args, "--json")...)
		if code != clerrors.ExitConfirmationRequired || !strings.Contains(out, `"confirmation_required"`) ||
			!strings.Contains(out, `"hint": "Re-run with --yes`) || strings.Contains(out, "confirmed") {
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
