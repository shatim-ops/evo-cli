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

// A pair of --arg the CLI cannot read — no "=", no name, a name the tool
// does not take, a value that is not of the argument's type — is refused
// by its argument (or its place) alone, not by its text: the text may be a
// secret (Астра, #2256), and so may what stands before an "=" in it — a
// value in Base64 ends with one (Астра, #2316).
func TestToolsCall_BadArgPairDoesNotEchoTheValue(t *testing.T) {
	const secret = "s3cr3t-93718244560173"
	for _, c := range []struct {
		tool string
		pair string
	}{
		{"secret_create", secret},
		{"secret_create", "=" + secret},
		{"secret_create", "value:" + secret + "="},
		{"secret_create", "value:" + secret + "=="},
		{"secret_add_version", "value:" + secret + "="},
		{"pg_user_create", "password:" + secret + "="},
		{"image_create", "user_data_template:" + secret + "="},
		{"secret_create", secret + "=x"},
		{"disk_create", "size_gb=" + secret},
		{"open_ports", "ports=[" + secret},
		{"stack_destroy", "wait=" + secret},
	} {
		for _, extra := range [][]string{nil, {"--json"}, {"--dry-run", "--json"}} {
			args := append([]string{"tools", "call", c.tool, "--arg", c.pair}, extra...)
			out, errOut, code := execCLI(t, args...)
			if code == 0 {
				t.Fatalf("%s %q %v: want a refusal: %s%s", c.tool, c.pair, extra, out, errOut)
			}
			if strings.Contains(out+errOut, secret) || strings.Contains(out+errOut, "s3cr3t") {
				t.Errorf("%s %q %v: the refusal echoes the value: %s%s", c.tool, c.pair, extra, out, errOut)
			}
		}
	}
}
