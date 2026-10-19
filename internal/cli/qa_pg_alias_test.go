// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"

	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
)

// QA-1 (QA 20.10): `evo --json --yes pg delete A --id B` удалял B —
// флаг --id побеждал позиционный <cluster-id>. У pg get и pg delete флага
// --id больше нет: это другое имя того же аргумента.
func TestPgTree_NoIDFlagBesidePositional(t *testing.T) {
	const a, b = "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002"
	for _, verb := range []string{"get", "delete"} {
		_, errOut, code := execCLI(t, "--json", "--yes", "pg", verb, a, "--id", b)
		if code != clerrors.ExitGeneral || !strings.Contains(errOut, "--id") {
			t.Errorf("pg %s A --id B: exit %d, %s", verb, code, errOut)
		}
		out, _, _ := execCLI(t, "pg", verb, "--help")
		if strings.Contains(out, "--id ") {
			t.Errorf("pg %s --help shows --id:\n%s", verb, out)
		}
	}
}
