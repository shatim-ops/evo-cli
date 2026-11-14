// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import "testing"

// A record names what a create makes by the arguments the create names it
// by (Decl.CreateName): a DNS zone by its name, else by its domain — the
// name the platform gives a zone without one. Two zones of one VPC made
// without a name are two records, not one: the held record of one is not
// closed by the answer about the other (release plan 1.9, line «Удержанная
// запись закрывается чужим ответом»; «Процесс · 1.9 (6)», #1989).
func TestIntentResource_TheNameTheCreateDeclares(t *testing.T) {
	for _, c := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"dns_zone_create", map[string]any{"domain": "a.example.com.", "vpc_id": "v-1"}, "a.example.com."},
		{"dns_zone_create", map[string]any{"domain": "a.example.com.", "name": "zone-a"}, "zone-a"},
		{"vpc_create", map[string]any{"name": "net"}, "net"},
		{"s3_bucket_create", map[string]any{"bucket": "b-1"}, "b-1"},
		{"secret_create", map[string]any{"path": "app/db"}, "app/db"},
	} {
		if got, _ := intentResource(c.tool, c.args); got != c.want {
			t.Errorf("%s %v: the record names %q, want %q", c.tool, c.args, got, c.want)
		}
	}
	a, _ := intentResource("dns_zone_create", map[string]any{"domain": "a.example.com.", "vpc_id": "v-1"})
	b, _ := intentResource("dns_zone_create", map[string]any{"domain": "b.example.com.", "vpc_id": "v-1"})
	if a == b {
		t.Errorf("two zones of one VPC are one record: %q", a)
	}
}
