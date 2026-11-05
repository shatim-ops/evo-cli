// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-FileContributor: Тимур Шакирбеков
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"regexp"
	"strings"
	"testing"

	"github.com/shatim-ops/evo-cli/internal/auth"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/loginworld"
)

// tag strips the markup of a page: the meanings are read as the person
// reads them.
var tag = regexp.MustCompile(`<[^>]*>`)

// B-6, the meanings the consent of evo login must carry («Риск»,
// conclusion 26; R-42, R-47), read on the page and in the terminal of a
// key for an agent, as the person sees them (Russian, the language of the
// conclusion):
//   - the role by its name, and that the access holds until a date;
//   - «any resources of the project, those made not through evo too» — once
//     on the page and once in the terminal;
//   - no claim the platform removes the role by itself (not checked live,
//     R-50), and no command on the page: revoking is two actions — the key
//     on this computer, the role in the console;
//   - the key's secret is not shown; access is granted by the page's form
//     with its own token alone (R-47).
func TestConsent_Meanings(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })

	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{For: ForAgent, Days: 7}, nil, nil)
	if r.err != nil {
		t.Fatalf("login: %v", r.err)
	}
	page := strings.Join(strings.Fields(tag.ReplaceAllString(r.b.consent, " ")), " ")
	term := strings.Join(strings.Fields(r.out), " ")
	warn := strings.Join(strings.Fields(tAgentWarn("01.01.2027")), " ")

	for _, c := range []struct {
		what, in string
		ok       bool
	}{
		{"the role by its name (R-42)", page, strings.Contains(page, auth.LoginRoleDisplayName)},
		{"the warning names the date the access holds until", warn, strings.Contains(warn, "01.01.2027")},
		{"«любые ресурсы проекта» once on the page (conclusion 26.3)", page, strings.Count(page, "любые ресурсы проекта") == 1},
		{"«в том числе созданные не через» on the page (conclusion 26.3)", page, strings.Contains(page, "в том числе созданные не через")},
		{"«любые ресурсы проекта» once in the terminal (conclusion 26.3)", term, strings.Count(term, "любые ресурсы проекта") == 1},
		{"no «снимается сама»: not checked live (conclusion 26.1, R-50)", warn, !strings.Contains(warn, "снимается сама")},
		{"no command in the warning (conclusion 26.2)", warn, !strings.Contains(warn, "evo logout")},
		{"revoking: the key on this computer (conclusion 26.2)", warn, strings.Contains(warn, "ключ") && strings.Contains(warn, "этом компьютере")},
		{"revoking: the role in the personal account (conclusion 26.2)", warn, strings.Contains(warn, "личном кабинете")},
		{"the secret is not shown (R-47)", page, strings.Contains(page, "Секрет ключа не показывается")},
		{"the grant is the page's form with its token (R-47)", r.b.consent, consentField.MatchString(r.b.consent) &&
			regexp.MustCompile(`(?s)<form[^>]*method="post"[^>]*action="/grant"|<form[^>]*action="/grant"[^>]*method="post"`).MatchString(r.b.consent)},
	} {
		if !c.ok {
			t.Errorf("%s:\n%.600s", c.what, c.in)
		}
	}
}
