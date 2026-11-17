// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// The CLI refuses an argument of the wrong type as the tool does: the
// argument named, its value never — with --json, without, and with
// --dry-run (Астра, #2241).
func TestToolsCall_TypeRefusalDoesNotEchoTheValue(t *testing.T) {
	const secret = "93718244560173"
	for _, extra := range [][]string{nil, {"--json"}, {"--dry-run"}, {"--dry-run", "--json"}} {
		args := append([]string{"tools", "call", "secret_create", "--args-json", `{"path":"app/db","value":` + secret + `}`}, extra...)
		out, errOut, code := execCLI(t, args...)
		if code == 0 || !strings.Contains(out+errOut, "value") {
			t.Fatalf("%v: want a refusal naming value, code %d: %s%s", extra, code, out, errOut)
		}
		if strings.Contains(out+errOut, secret) {
			t.Errorf("%v: the refusal echoes the value: %s%s", extra, out, errOut)
		}
	}
}
