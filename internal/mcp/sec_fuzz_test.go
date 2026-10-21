// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// redactSecrets never panics, keeps a JSON result JSON, and leaves no value
// of a "password" key behind.
func FuzzRedactSecrets(f *testing.F) {
	for _, s := range []string{
		`{"password":"p"}`,
		`{"a":[{"Password":"x","b":{"secret_key":"y"}}],"page_token":"t"}`,
		`[1,"x",{"token":"t","id":"i"}]`,
		`not json`,
		`{"password":1,"secret":null}`,
		`{"accessToken":"a","password_expires_at":"2026","redacted":["x"]}`,
		`{"passwords":["a","b"],"secret_old":{"v":"c","n":[1,"d"]},"password_expires_at_raw":"e"}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := redactSecrets("", in, httpWording)
		if !json.Valid([]byte(in)) {
			if out != in {
				t.Fatalf("non-JSON changed: %q -> %q", in, out)
			}
			return
		}
		if !json.Valid([]byte(out)) {
			t.Fatalf("invalid JSON out of %q: %q", in, out)
		}
		var v any
		_ = json.Unmarshal([]byte(out), &v)
		// A masked object says so in redacted.
		if m, ok := v.(map[string]any); ok && out != in {
			if _, ok := m["redacted"].([]any); !ok {
				t.Fatalf("%q masked without redacted: %q", in, out)
			}
		}
		// Under a credential key no string survives, however deep.
		var allMasked func(string, any)
		allMasked = func(k string, x any) {
			switch t2 := x.(type) {
			case string:
				if t2 != "" && t2 != redacted {
					t.Fatalf("%q kept %s: %q", in, k, t2)
				}
			case map[string]any:
				for _, y := range t2 {
					allMasked(k, y)
				}
			case []any:
				for _, y := range t2 {
					allMasked(k, y)
				}
			}
		}
		var walk func(any)
		walk = func(x any) {
			switch t2 := x.(type) {
			case map[string]any:
				for k, y := range t2 {
					if secretKey(k) {
						allMasked(k, y)
						continue
					}
					walk(y)
				}
			case []any:
				for _, y := range t2 {
					walk(y)
				}
			}
		}
		walk(v)
	})
}

// kubectlCheck never lets a host-credential flag through, whatever
// surrounds it, as long as it comes before "--".
func FuzzKubectlHTTPCheck(f *testing.F) {
	for _, s := range []string{
		"get pods --kubeconfig=/x",
		"get pods -n x --server https://h",
		"exec p -- --kubeconfig=/x",
		"get secret s -o yaml",
		"cp a b",
	} {
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, args string, allow bool) {
		fields := strings.Fields(args)
		err := kubectlCheck(fields, allow, httpWording)
		for _, fl := range fields {
			if fl == "--" {
				break
			}
			name, _, _ := strings.Cut(fl, "=")
			if name == "--kubeconfig" || name == "--server" || name == "--token" || name == "-s" {
				if err == nil {
					t.Fatalf("allowed %q", args)
				}
			}
		}
	})
}
