// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"strings"
	"testing"
)

// The refusals of evo mcp --auth and --toolsets do not repeat what was
// typed, in any case of it: a secret in the wrong place (Астра #2370,
// #2371). Every branch of httpAuthToken that names a value: an unknown
// mode, --auth none off loopback, a short bearer token off loopback.
func TestMCPAuthAndToolsets_DoNotRepeatTheValue(t *testing.T) {
	setupCLI(t)
	const secret = "s3cr3tAstra27183650904512"
	for _, c := range []struct {
		args  []string
		token string
	}{
		{args: []string{"mcp", "--http", ":8080", "--auth", secret}},
		{args: []string{"--json", "--dry-run", "mcp", "--http", ":8080", "--auth", secret}},
		{args: []string{"mcp", "--http", secret + ":8080", "--auth", "none"}},
		{args: []string{"--json", "mcp", "--http", secret + ":8080", "--auth", "none"}},
		{args: []string{"mcp", "--http", secret + ":8080", "--auth", "bearer"}, token: "short"},
		{args: []string{"mcp", "--toolsets", secret}},
		{args: []string{"mcp", "--toolsets", "vm," + secret}},
		{args: []string{"--json", "--dry-run", "mcp", "--toolsets", secret}},
	} {
		newProcess()
		t.Setenv(envBearerToken, c.token)
		code, out, errOut := runCLI(t, "", c.args...)
		if code == 0 {
			t.Errorf("%v: exit 0, want a refusal: %s%s", c.args, out, errOut)
		}
		if strings.Contains(strings.ToLower(out+errOut), "s3cr3t") {
			t.Errorf("%v: repeats the value: %s%s", c.args, out, errOut)
		}
	}
}
