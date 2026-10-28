// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"
	"testing"
)

// Fable audit 1.5, FB15-44 (release plan 1.5, С6): an environment variable
// names its secret in words a JSON key does not — GITHUB_TOKEN, DB_PASS,
// AWS_ACCESS_KEY — and a connection URL carries the password inside the
// value whatever the name (DATABASE_URL). H7 masked the value of a pair by
// the roots of a key only, so ca_get over HTTP returned those values as
// they are. A name that is not a credential keeps its value.
func TestRedact_EnvPairNamesAndURLs(t *testing.T) {
	const secret = "hunter2-very-secret"
	for name, masked := range map[string]bool{
		"DB_PASSWORD": true, "JWT_SECRET": true, "API_KEY": true, // roots, as before
		"GITHUB_TOKEN": true, "BOT_TOKEN": true, "DB_PASS": true, "SMTP_PASS": true,
		"MYSQL_PWD": true, "CREDENTIALS": true, "AWS_ACCESS_KEY": true, "SESSION_KEY": true,
		"ENCRYPTION_KEY": true, "SSH_KEY": true,
		"PORT": false, "LOG_LEVEL": false, "PUBLIC_KEY": false, "SSH_PUBLIC_KEY": false,
		"ACCESS_KEY_ID": false, "TOKEN_URL": false, "PASSWORD_POLICY": false,
	} {
		body := `{"env":[{"name":"` + name + `","value":"` + secret + `"}]}`
		out := redactSecrets("ca_get", body, wording{mask: "***"})
		if got := !strings.Contains(out, secret); got != masked {
			t.Errorf("%s: masked=%v, want %v:\n%s", name, got, masked, out)
		}
	}
	// A URL with a password in it is a credential under any name; one
	// without a password is not.
	for value, masked := range map[string]bool{
		"postgres://app:" + secret + "@db.internal:5432/app": true,
		"redis://:" + secret + "@cache:6379/0":               true,
		"https://example.com/" + secret:                      false,
	} {
		body := `{"env":[{"name":"DATABASE_URL","value":"` + value + `"}]}`
		out := redactSecrets("ca_get", body, wording{mask: "***"})
		if got := !strings.Contains(out, secret); got != masked {
			t.Errorf("DATABASE_URL=%s: masked=%v, want %v:\n%s", value, got, masked, out)
		}
	}
}
