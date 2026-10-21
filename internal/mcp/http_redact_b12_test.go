// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The details of the fixes of B12-2…B12-5 (http_redact_break12_test.go has
// the findings): what redacted lists and what stays as the tool wrote it.

// B12-2: the fields of a name/value pair are matched by normKey, the name
// by the rules of a key.
func TestRedact_B12_2_PairFieldsAnyCase(t *testing.T) {
	cases := []struct {
		in    string
		paths []string // nil: the result is not changed
	}{
		{`{"env":[{"Name":"DB_PASSWORD","Value":"fake-pw-1"},{"KEY":"client-secret","VALUE":7},{"name":"LOG_LEVEL","Value":"debug"}]}`,
			[]string{"env[].VALUE", "env[].Value"}},
		// Both spellings of value in one object: both are the pair's.
		{`{"name":"token","value":"fake-tk-1","Value":"fake-tk-2"}`, []string{"Value", "value"}},
		// An exception of a key holds for a name too; a name with no
		// value beside it is no pair.
		{`{"env":[{"Name":"SECRET_NAME","Value":"db"}]}`, nil},
		{`{"Name":"password","id":"x"}`, nil},
	}
	for _, c := range cases {
		out := redactSecrets("ca_get", c.in, httpWording)
		if c.paths == nil {
			if out != c.in {
				t.Errorf("%s changed: %s", c.in, out)
			}
			continue
		}
		if strings.Contains(out, "fake-") || strings.Contains(out, `: 7`) {
			t.Errorf("%s: a value of a pair left the host:\n%s", c.in, out)
		}
		if got := redactedOf(t, out); !slices.Equal(got, c.paths) {
			t.Errorf("%s: redacted %v, want %v", c.in, got, c.paths)
		}
	}
}

// B12-3: a string that is a JSON object or array is masked as a part of
// the result and written back as a string; one with nothing masked stays
// byte for byte, and base64 is not decoded.
func TestRedact_B12_3_NestedJSON(t *testing.T) {
	const secret = "fake-secret-1"
	const b64 = "eyJwYXNzd29yZCI6ImZha2UifQ==" // {"password":"fake"}, not decoded
	in := `{"password":"` + secret + `",` +
		`"user_data":"{\"admin_password\":\"x\",\"note\":\"` + secret + `\",\"inner\":\"[{\\\"token\\\":\\\"y\\\"}]\"}",` +
		`"config":"{ \"a\": 1 }","list":"[1, 2]","broken":"{not json","blob":"` + b64 + `"}`
	out := redactSecrets("vm_get", in, httpWording)
	if strings.Contains(out, secret) {
		t.Errorf("a secret inside a JSON string left the host:\n%s", out)
	}
	want := []string{"password", "user_data.admin_password", "user_data.inner[].token", "user_data.note"}
	if got := redactedOf(t, out); !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v", got, want)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	// Still a string, and the JSON in it is valid, masks in place.
	ud, ok := m["user_data"].(string)
	var inner map[string]any
	if !ok || json.Unmarshal([]byte(ud), &inner) != nil || inner["admin_password"] != redacted || inner["note"] != redacted {
		t.Errorf("user_data is not the masked JSON string: %#v", m["user_data"])
	}
	for k, v := range map[string]string{"config": `{ "a": 1 }`, "list": "[1, 2]", "broken": "{not json", "blob": b64} {
		if m[k] != v {
			t.Errorf("%s changed: %#v, want %q", k, m[k], v)
		}
	}

	// A masked value that is itself JSON, repeated: the string is masked
	// as a whole.
	out = redactSecrets("vm_get", `{"password":"[12345678]","copy":"[12345678]"}`, httpWording)
	if strings.Contains(out, "12345678") {
		t.Errorf("a repeated JSON secret left the host:\n%s", out)
	}
	if got, want := redactedOf(t, out), []string{"copy", "password"}; !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v", got, want)
	}

	// A JSON string with nothing to mask: the result as it was.
	if in := `{"user_data":"{\"a\":\"b\"}"}`; redactSecrets("vm_get", in, httpWording) != in {
		t.Errorf("%s changed", in)
	}
}

// B12-4: a number is an echo when its text, as the tool wrote it, is a
// masked value of minEchoLen characters or more, whether the masked one
// was a number or a string.
func TestRedact_B12_4_NumberEchoes(t *testing.T) {
	out := redactSecrets("pg_get", `{"password":123456789,"copies":123456789,"label":"123456789","other":123456780,"sci":1.23456789e8}`, httpWording)
	if got, want := redactedOf(t, out), []string{"copies", "label", "password"}; !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v:\n%s", got, want, out)
	}
	if !strings.Contains(out, `"other": 123456780`) || !strings.Contains(out, `"sci": 1.23456789e8`) {
		t.Errorf("another number is masked:\n%s", out)
	}
	out = redactSecrets("pg_get", `{"password":"123456789","n":123456789}`, httpWording)
	if got, want := redactedOf(t, out), []string{"n", "password"}; !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v", got, want)
	}
	// Shorter than minEchoLen: masked under its key only (REL12-10).
	out = redactSecrets("pg_get", `{"password":1234567,"count":1234567}`, httpWording)
	if !strings.Contains(out, `"count": 1234567`) {
		t.Errorf("a short number echoed:\n%s", out)
	}
}

// B12-5: the private key of a TLS Secret and the SSH identity files are
// credentials under their whole name, in any spelling; the certificate,
// the public key and a longer name are not.
func TestRedact_B12_5_KeyFileNames(t *testing.T) {
	for _, k := range []string{
		"tls.key", "TLS_KEY", "tlsKey", "tls-key",
		"id_rsa", "ID-RSA", "id_dsa", "id_ecdsa", "id_ecdsa_sk", "id_ed25519", "id_ed25519_sk", "idEd25519",
	} {
		if !secretKey(k) {
			t.Errorf("%s is not masked", k)
		}
	}
	for _, k := range []string{"tls.crt", "ca.crt", "id_rsa.pub", "id_ed25519.pub", "known_hosts", "authorized_keys", "tls.key.sha256"} {
		if secretKey(k) {
			t.Errorf("%s is masked", k)
		}
	}
}
