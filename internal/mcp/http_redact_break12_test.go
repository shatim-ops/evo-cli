// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"strings"
	"testing"
)

// TestBreak12_B12_2_PascalCaseNameValue: a name/value pair is recognised
// only when the keys are literally "name" or "key" and "value". The same
// pair in PascalCase (container env from a Go API, {"Name","Value"} and
// {"Key","Value"}) keeps the credential. hasRoot itself ignores case, so
// the value's name is a credential key. A pair whose name is not a
// credential stays in the clear.
func TestBreak12_B12_2_PascalCaseNameValue(t *testing.T) {
	const secret = "s3cret-value"
	in := `{"template":{"containers":[{"env":[{"Name":"DB_PASSWORD","Value":"` + secret +
		`"},{"Key":"CLIENT_SECRET","Value":"` + secret + `-2"},{"name":"LOG_LEVEL","value":"debug"}]}]}}`
	out := redactSecrets("ca_get", in, httpWording)
	if strings.Contains(out, secret) {
		t.Errorf("PascalCase Name/Value and Key/Value left the credential in the clear:\n%s", out)
	}
	if !strings.Contains(out, "debug") {
		t.Errorf("a pair that is not a credential was masked:\n%s", out)
	}
}

// TestBreak12_B12_3_SecretInsideJSONString: echoes masks a string only when
// the whole string equals a masked value. A credential repeated inside a
// nested JSON string (user_data, kubectl.kubernetes.io/last-applied-configuration)
// stays. The mask must not start decoding arbitrary base64.
func TestBreak12_B12_3_SecretInsideJSONString(t *testing.T) {
	const secret = "s3cret-value"
	in := `{"password":"` + secret + `","user_data":"{\"admin_password\":\"` + secret + `\"}"}`
	out := redactSecrets("vm_get", in, httpWording)
	if strings.Contains(out, secret) {
		t.Errorf("the credential is still inside the nested JSON string:\n%s", out)
	}
}

// TestBreak12_B12_4_NumericEcho: a number under a credential key is masked,
// and the same number elsewhere is not. echoes looks at strings only, so a
// json.Number equal to the masked value (eight digits or more) stays.
// A shorter value is a common word and must not be chased (REL12-10).
func TestBreak12_B12_4_NumericEcho(t *testing.T) {
	in := `{"password":123456789,"copies":123456789}`
	out := redactSecrets("pg_get", in, httpWording)
	if strings.Contains(out, "123456789") {
		t.Errorf("the masked number is echoed under copies:\n%s", out)
	}
}

// TestBreak12_B12_5_TlsKey: Kubernetes Secret data names a private key
// "tls.key" and an SSH key "id_rsa". normKey joins the dot away
// ("tlskey", "idrsa"), and neither contains a root of HTTPSecretFields
// (private_key, secret, …). "ssh-private-key" already contains private_key
// and is masked. A certificate under "tls.crt" is not a private key and
// stays.
func TestBreak12_B12_5_TlsKey(t *testing.T) {
	const key = "LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tQUJDRA=="
	const idRSA = "idrsa-secret-value"
	const crt = "crt-public-material"
	in := `{"data":{"tls.key":"` + key + `","id_rsa":"` + idRSA + `","tls.crt":"` + crt + `"}}`
	out := redactSecrets("k8s_get", in, httpWording)
	if strings.Contains(out, key) || strings.Contains(out, idRSA) {
		t.Errorf("tls.key / id_rsa left the key material in the clear:\n%s", out)
	}
	if !strings.Contains(out, crt) {
		t.Errorf("tls.crt is a certificate, not a private key:\n%s", out)
	}
}
