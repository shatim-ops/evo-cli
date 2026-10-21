// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"
	"testing"
)

// TestReviewParity_RP_2_RedactedListKeepsSecretValues: a masked object must
// list field paths in "redacted", never the values (docs/mcp.md). A cloud
// object that already carries a "redacted" array, or nests one beside a
// credential, keeps the secret string in the answer.
func TestReviewParity_RP_2_RedactedListKeepsSecretValues(t *testing.T) {
	const secret = "s3cret-value"
	cases := []string{
		`{"password":"` + secret + `","redacted":["` + secret + `"]}`,
		`{"user":{"password":"` + secret + `","redacted":["` + secret + `"]}}`,
	}
	for _, in := range cases {
		out := redactSecrets("vm_get", in, httpWording)
		if strings.Contains(out, secret) {
			t.Errorf("secret value left the host:\n%s", out)
		}
	}
}

// TestReviewParity_RP_3_NonStringCredentialUnmasked: a credential under a
// secret root leaves in the clear when it is not a string. The decoder
// keeps JSON numbers as json.Number (UseNumber); maskAll replaces only
// non-empty strings, so the number, the bool and the nested number are
// returned and "redacted" does not name them.
func TestReviewParity_RP_3_NonStringCredentialUnmasked(t *testing.T) {
	in := `{"password":1000000001,"linux_password":true,"token":1000000002,"users":[{"password":{"pin":1000000003}}]}`
	out := redactSecrets("pg_get", in, httpWording)
	for _, leak := range []string{"1000000001", "1000000002", "1000000003", "true"} {
		if strings.Contains(out, leak) {
			t.Errorf("credential %s left in the clear:\n%s", leak, out)
		}
	}
}

// TestReviewParity_RP_4_DottedCredentialKeyUnmasked: the match drops "_" and
// "-" and ignores case, so private_key, private-key and privateKey are one
// root. The same words separated by a dot are not: private.key and api.key
// leave in the clear.
func TestReviewParity_RP_4_DottedCredentialKeyUnmasked(t *testing.T) {
	const secret = "s3cret-dotted"
	in := `{"private.key":"` + secret + `","api.key":"` + secret + `-2"}`
	out := redactSecrets("cert_get", in, httpWording)
	if strings.Contains(out, secret) {
		t.Errorf("dotted credential key left in the clear:\n%s", out)
	}
}
