// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"
	"testing"
	"unicode"
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
		tool string
		// want and never are lower case; a word in Cyrillic is looked for
		// in the Russian text, any other in the English one, so each
		// language says it by itself.
		want, never []string
	}{
		{"cert_issue_le", []string{"tls", "vm", "вм", "tls-сертификат"}, []string{"bind to the lb listener", "привязка к обработчику балансировщика"}},
		{"cert_list", []string{"vm", "вм"}, []string{"https load balancer", "https-балансировщика"}},
		{"lb_create", []string{"l4 tcp", "tls", "vm", "вм", "tls не завершает"}, nil},
		{"vpc_create", []string{"subnet", "подсет"}, nil},
		{"redis_create", []string{"three billed hosts", "три платных узла"}, nil},
		{"ca_deploy", []string{"billed", "оплачива"}, nil},
	} {
		en, ru := text(c.tool)
		en, ru = strings.ToLower(en), strings.ToLower(ru)
		in := func(w string) string {
			if strings.ContainsFunc(w, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) {
				return ru
			}
			return en
		}
		for _, w := range c.want {
			if !strings.Contains(in(w), w) {
				t.Errorf("%s: the description does not say %q", c.tool, w)
			}
		}
		for _, w := range c.never {
			if strings.Contains(in(w), w) {
				t.Errorf("%s: the description promises %q, which the product does not do", c.tool, w)
			}
		}
	}
}
