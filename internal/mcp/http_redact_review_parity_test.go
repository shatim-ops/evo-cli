// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"slices"
	"strings"
	"testing"
)

// TestReviewParity_RP_2_RedactedListKeepsSecretValues: a masked object must
// list field paths in "redacted", never the values (docs/mcp.md). A cloud
// object that already carries a "redacted" array, or nests one beside a
// credential, kept the secret string in the answer. The list is masking's
// own now: the tool's root "redacted" is replaced, and a masked value
// repeated anywhere else is masked too.
func TestReviewParity_RP_2_RedactedListKeepsSecretValues(t *testing.T) {
	const secret = "s3cret-value"
	cases := []struct {
		in    string
		paths []string
	}{
		{`{"password":"` + secret + `","redacted":["` + secret + `"]}`, []string{"password"}},
		{`{"user":{"password":"` + secret + `","redacted":["` + secret + `"]}}`, []string{"user.password", "user.redacted[]"}},
		{`{"id":"x","note":"` + secret + `","redacted":{"password":"` + secret + `"}}`, []string{"note"}},
	}
	for _, c := range cases {
		out := redactSecrets("vm_get", c.in, httpWording)
		if strings.Contains(out, secret) {
			t.Errorf("secret value left the host:\n%s", out)
		}
		if got := redactedOf(t, out); !slices.Equal(got, c.paths) {
			t.Errorf("%s: redacted %v, want %v", c.in, got, c.paths)
		}
	}
	// A "redacted" of the tool with nothing to mask: the answer as it was.
	if in := `{"redacted":["x"]}`; redactSecrets("vm_get", in, httpWording) != in {
		t.Errorf("nothing to mask, but %s changed", in)
	}
}

// TestReviewParity_RP_3_NonStringCredentialUnmasked: a credential under a
// secret root left in the clear when it was not a string. The owner's
// decision: a number under a credential key (also inside an object or a
// list under one) is masked like a string; true and false are not — a flag
// that a password is set is no credential.
func TestReviewParity_RP_3_NonStringCredentialUnmasked(t *testing.T) {
	in := `{"password":1000000001,"linux_password":true,"token":1000000002,"users":[{"password":{"pin":1000000003}}]}`
	out := redactSecrets("pg_get", in, httpWording)
	for _, leak := range []string{"1000000001", "1000000002", "1000000003"} {
		if strings.Contains(out, leak) {
			t.Errorf("credential %s left in the clear:\n%s", leak, out)
		}
	}
	if !strings.Contains(out, `"linux_password": true`) {
		t.Errorf("a flag under a credential key is not a credential:\n%s", out)
	}
	if got, want := redactedOf(t, out), []string{"password", "token", "users[].password.pin"}; !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v", got, want)
	}
}

// TestReviewParity_RP_4_DottedCredentialKeyUnmasked: the match drops "_" and
// "-" and ignores case, so private_key, private-key and privateKey are one
// root. The same words separated by a dot (or a space) were not: private.key
// and api.key left in the clear.
func TestReviewParity_RP_4_DottedCredentialKeyUnmasked(t *testing.T) {
	const secret = "s3cret-dotted"
	in := `{"private.key":"` + secret + `","api.key":"` + secret + `-2","Private Key":"` + secret + `-3","secret.id":"sid","password.expires.at":"2026"}`
	out := redactSecrets("cert_get", in, httpWording)
	if strings.Contains(out, secret) {
		t.Errorf("dotted credential key left in the clear:\n%s", out)
	}
	if got, want := redactedOf(t, out), []string{"Private Key", "api.key", "private.key"}; !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v (an exception is one with a dot too)", got, want)
	}
}
