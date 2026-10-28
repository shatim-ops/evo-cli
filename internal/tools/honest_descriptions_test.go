// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
)

// Fable audit 1.5 (release plan 1.5, С8): a description promises what the
// tool and the platform do, in both languages. The load balancer is L4 TCP
// without TLS, so a certificate is not bound to it — HTTPS ends on the VM
// (FB15-20); vpc_create makes no subnet (FB15-29); MASTER_REPLICA is three
// billed hosts per shard (FB15-37); min_instances above 0 is billed all the
// time (FB15-45).
func TestDescriptions_SayWhatIsBuilt(t *testing.T) {
	r := New(nil)
	text := func(name string) (en, ru string) {
		tl, ok := r.Lookup(name)
		if !ok {
			t.Fatalf("no tool %s", name)
		}
		en = tl.Description
		for _, d := range tl.ArgDescriptionsRU {
			ru += " " + d
		}
		props, _ := tl.InputSchema["properties"].(map[string]any)
		for _, p := range props {
			if m, ok := p.(map[string]any); ok {
				en += " " + strings.TrimSpace(m["description"].(string))
			}
		}
		return en, tl.DescriptionRU + ru
	}
	for _, c := range []struct {
		tool        string
		want, never []string // lower case, in English and Russian
	}{
		{"cert_issue_le", []string{"tls", "vm", "вм"}, []string{"bind to the lb listener", "привязка к обработчику балансировщика"}},
		{"cert_list", []string{"vm", "вм"}, []string{"https load balancer", "https-балансировщика"}},
		{"lb_create", []string{"l4 tcp", "tls", "vm", "вм"}, nil},
		{"vpc_create", []string{"subnet", "подсет"}, nil},
		{"redis_create", []string{"three billed hosts", "три платных узла"}, nil},
		{"ca_deploy", []string{"billed", "оплачива"}, nil},
	} {
		en, ru := text(c.tool)
		both := strings.ToLower(en + " " + ru)
		for _, w := range c.want {
			if !strings.Contains(both, w) {
				t.Errorf("%s: the description does not say %q", c.tool, w)
			}
		}
		for _, w := range c.never {
			if strings.Contains(both, w) {
				t.Errorf("%s: the description promises %q, which the product does not do", c.tool, w)
			}
		}
	}
}
