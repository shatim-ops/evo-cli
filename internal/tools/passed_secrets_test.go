// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"slices"
	"testing"
)

// The values of set and values k8s_helm hides: under a key of a secret by
// the rule of internal/secretkey, the password of an address under any key,
// every value in an object or a list under a key of a secret; not a flag,
// not a name of a secret (an exception of the rule), not a value under a key
// that names no secret (#2465).
func TestHelmPassedSecrets(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want []string
	}{
		{"several assignments in one set", map[string]any{"set": []any{"image.tag=1.27,db.password=pw-one,replicas=2"}}, []string{"pw-one"}},
		{"an escaped comma is the value's", map[string]any{"set": []any{`auth.rootPassword=a\,b`}}, []string{"a,b"}},
		{"an index in the path", map[string]any{"set": []any{"users[0].password=pw-two"}}, []string{"pw-two"}},
		{"the password of an address only", map[string]any{"set": []any{"db.url=postgres://app:pw-three@db/x"}}, []string{"pw-three"}},
		{"a name of a secret is no secret", map[string]any{"set": []any{"auth.existingSecret_name=my-secret"}}, nil},
		{"values: a key of a secret, an address, a container under a secret", map[string]any{"values": "auth:\n  rootPassword: pw-four\n  enablePassword: true\n" +
			"external:\n  dsn: mysql://u:pw-five@h/db\ncredentials:\n  user: admin-six\n  list: [pw-seven]\nservice:\n  name: web\n  port: 80\n"},
			[]string{"pw-four", "pw-five", "admin-six", "pw-seven"}},
		{"a number under a key of a secret", map[string]any{"values": "redis:\n  password: 123456789\n"}, []string{"123456789"}},
		{"nothing to hide", map[string]any{"set": []any{"image.tag=1.27"}, "values": "service:\n  name: web\n"}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := helmPassedSecrets(c.args)
			slices.Sort(got)
			want := slices.Clone(c.want)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("hidden %q, want %q", got, want)
			}
		})
	}
}
