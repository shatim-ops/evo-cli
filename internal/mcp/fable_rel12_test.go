// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"net"
	"slices"
	"strings"
	"testing"
)

// Fable's release audit of 1.2 (REL12-*).

// REL12-2: ca_get gives the env of a container as name/value pairs, and the
// masking looked at the keys only: the value of DB_PASSWORD left the host
// over HTTP and on a --read-only server. A pair named as a credential key
// is masked as one; a pair with another name is not.
func TestFable_REL12_2_NameValuePairs(t *testing.T) {
	const body = `{"template":{"containers":[{"env":[{"name":"DB_PASSWORD","value":"S3cret"},{"name":"LOG_LEVEL","value":"debug"}]}]}}`
	for _, readOnly := range []bool{false, true} {
		s := newTestServer()
		fakeResult(s, "ca_get", body)
		if readOnly {
			s.SetReadOnly()
		} else {
			NewHTTPHandler(s, HTTPOptions{}).Close()
		}
		text, _ := call(s, "ca_get", nil)
		if strings.Contains(text, "S3cret") {
			t.Errorf("read-only %v: the value of DB_PASSWORD left the host:\n%s", readOnly, text)
		}
		if !strings.Contains(text, `"value": "debug"`) {
			t.Errorf("read-only %v: LOG_LEVEL is no credential:\n%s", readOnly, text)
		}
		if got, want := redactedOf(t, text), []string{"template.containers[].env[].value"}; !slices.Equal(got, want) {
			t.Errorf("read-only %v: redacted %v, want %v", readOnly, got, want)
		}
	}

	// key/value (a preview's params, a cloud object's metadata), a number
	// value, and the exceptions of a key hold for a name too.
	in := `{"params":[{"key":"admin_password","value":"pw-admin"},{"key":"flavor","value":"s"}],` +
		`"metadata":[{"key":"pin","value":1234},{"key":"api_key","value":20251021},{"name":"secret_name","value":"db"},{"name":"token","value":true}]}`
	out := redactSecrets("vm_get", in, httpWording)
	for _, leak := range []string{"pw-admin", "20251021"} {
		if strings.Contains(out, leak) {
			t.Errorf("%s left the host:\n%s", leak, out)
		}
	}
	for _, kept := range []string{`"value": "s"`, `"value": 1234`, `"value": "db"`, `"value": true`} {
		if !strings.Contains(out, kept) {
			t.Errorf("%s is no credential:\n%s", kept, out)
		}
	}
	if got, want := redactedOf(t, out), []string{"metadata[].value", "params[].value"}; !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v", got, want)
	}
	// Not a pair: a "name" with no "value" beside it.
	if in := `{"name":"password","id":"x"}`; redactSecrets("pg_get", in, httpWording) != in {
		t.Errorf("%s changed", in)
	}
}

// REL12-10: echoes masked every string equal to a masked value, and a short
// one is a common word: "password":"ok" masked "status":"ok". A value
// shorter than minEchoLen is masked under its own key only; a longer one
// is still masked wherever it is repeated (RP-2).
func TestFable_REL12_10_ShortValuesDoNotEcho(t *testing.T) {
	out := redactSecrets("pg_get", `{"password":"ok","status":"ok","users":[{"note":"ok"}]}`, httpWording)
	if !strings.Contains(out, `"status": "ok"`) || !strings.Contains(out, `"note": "ok"`) {
		t.Errorf("an unrelated field is masked:\n%s", out)
	}
	if got := redactedOf(t, out); !slices.Equal(got, []string{"password"}) {
		t.Errorf("redacted %v, want [password]", got)
	}

	const secret = "s3cretOK" // minEchoLen characters
	if len([]rune(secret)) != minEchoLen {
		t.Fatalf("%q is not %d characters", secret, minEchoLen)
	}
	in := `{"password":"` + secret + `","status":"` + secret + `","redacted":["` + secret + `"],"user":{"redacted":["` + secret + `"]}}`
	out = redactSecrets("pg_get", in, httpWording)
	if strings.Contains(out, secret) {
		t.Errorf("a repeated secret left the host:\n%s", out)
	}
	if got, want := redactedOf(t, out), []string{"password", "status", "user.redacted[]"}; !slices.Equal(got, want) {
		t.Errorf("redacted %v, want %v", got, want)
	}
	// Characters, not bytes: seven Cyrillic letters are fourteen bytes.
	out = redactSecrets("pg_get", `{"password":"пароль1","status":"пароль1"}`, httpWording)
	if !strings.Contains(out, `"status": "пароль1"`) {
		t.Errorf("a seven-character value echoed:\n%s", out)
	}
}

// REL12-5: over HTTP the refusal of H11 was not the outermost: the
// restrictions of the transport (H1, H2, H4, H10) wrapped the handlers
// after SetReadOnly, so a call the read-only server refuses answered H2 (an
// oracle of the host's files) or H4 (a DNS request for repo_url) first.
func TestFable_REL12_5_ReadOnlyRefusalOutermostOverHTTP(t *testing.T) {
	lookups := 0
	orig := lookupIP
	lookupIP = func(context.Context, string) ([]net.IP, error) {
		lookups++
		return []net.IP{net.ParseIP("93.184.216.34")}, nil
	}
	t.Cleanup(func() { lookupIP = orig })

	s := newTestServer()
	s.SetReadOnly()
	NewHTTPHandler(s, HTTPOptions{}).Close()
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"deploy", map[string]any{"compose_file": "/etc/passwd", "confirmed": true}},
		{"k8s_helm", map[string]any{"cluster_id": "c", "action": "install", "release": "web", "chart": "repo/app",
			"repo_url": "https://charts.example.com", "confirmed": true}},
	} {
		text, isErr := call(s, c.tool, c.args)
		if !isErr || !strings.Contains(text, `"reason": "USAGE"`) || !strings.Contains(text, c.tool+" is refused: this server runs with --read-only") {
			t.Errorf("%s: want the refusal of H11, got %s", c.tool, text)
		}
	}
	if lookups != 0 {
		t.Errorf("a refused call resolved repo_url %d times", lookups)
	}

	// A read goes on under the gate, through the checks of the transport.
	if text, _ := call(s, "k8s_helm", map[string]any{"cluster_id": "c", "action": "status", "release": "web",
		"repo_url": "http://charts.example.com"}); !strings.Contains(text, "only https:// and oci:// URLs") {
		t.Errorf("k8s_helm status skipped H4: %s", text)
	}
	if text, _ := call(s, "doctor", nil); !strings.Contains(text, "doctor is disabled over HTTP") {
		t.Errorf("doctor skipped H10: %s", text)
	}
}

// REL12-6: "logs pod -f" was refused over HTTP and on a --read-only server
// (-f read as --filename), though -f of logs is --follow, as --follow and
// tools_kube.go have it.
func TestFable_REL12_6_LogsFollow(t *testing.T) {
	for _, w := range []wording{httpWording, readOnlyWording} {
		for _, c := range []string{"logs web-0 -f", "logs -f web-0 --tail=10", "log web-0 -f", "logs web-0 --follow"} {
			if err := kubectlCheck(strings.Fields(c), false, w); err != nil {
				t.Errorf("%q: %v", c, err)
			}
		}
		for _, c := range []string{"get pods -f /etc/passwd", "describe -f x.yaml", "logs -f web-0 --kubeconfig=/x"} {
			if err := kubectlCheck(strings.Fields(c), false, w); err == nil {
				t.Errorf("%q is allowed", c)
			}
		}
	}
}
