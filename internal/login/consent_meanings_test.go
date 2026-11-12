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

// tag strips the markup of a page.
var tag = regexp.MustCompile(`<[^>]*>`)

// B-6, the meanings the consent of evo login must carry («Риск»,
// conclusion 26; R-42, R-47), read on the page and in the terminal of a
// key for an agent, as the person sees them (Russian, the language of the
// conclusion). V3 keeps some meanings in the hints of a «?» — by the
// owner's decision of 11.11 («Риск», conclusion 100.4) — so the page is
// read twice: what is visible (outside .tip) and what is in a hint. A
// meaning of the visible list moved into a hint fails here; the move is
// an owner's decision and an edit of the list.
//
// Visible, for an agent: the role by its name and the date the access
// holds until (the plan); «any resources of the project, those made not
// through evo too» — once on the page and once in the terminal; the right
// to manage service accounts and keys (conclusion 100.3); that the key's
// secret does not pass the browser and is never shown.
// In a hint: revoking is two actions — the key on this computer, the role
// in the console.
// Nowhere: a claim the platform removes the role by itself (not checked
// live, R-50), a command on the page. Access is granted by the page's form
// with its own token alone (R-47).
func TestConsent_Meanings(t *testing.T) {
	prev := i18n.Lang()
	i18n.SetLang(i18n.RU)
	t.Cleanup(func() { i18n.SetLang(prev) })

	w := newWorld(t, loginworld.Variant{})
	r := login(t, w, &browser{}, Choice{For: ForAgent, Days: 7}, nil, nil)
	if r.err != nil {
		t.Fatalf("login: %v", r.err)
	}
	seen, hints := visibleText(r.b.consent), tipText(r.b.consent)
	term := strings.Join(strings.Fields(r.out), " ")
	until := regexp.MustCompile(`<span class="x s-t-7">([^<]+)</span>`).FindStringSubmatch(r.b.consent)
	plan := ""
	if m := regexp.MustCompile(`(?s)<div class="plan".*?</div>`).FindString(r.b.consent); m != "" {
		plan = visibleText(m)
	}

	for _, c := range []struct {
		what, in string
		ok       bool
	}{
		{"visible: the role by its name (R-42)", seen, strings.Contains(seen, auth.LoginRoleDisplayName)},
		{"visible: the plan names the date the access holds until", plan, until != nil && strings.Contains(plan, until[1])},
		{"visible: «любые ресурсы проекта» once on the page (conclusion 26.3)", seen,
			strings.Count(seen, "любые ресурсы проекта") == 1 && !strings.Contains(hints, "любые ресурсы проекта")},
		{"visible: «в том числе созданные не через» (conclusion 26.3)", seen, strings.Contains(seen, "в том числе созданные не через")},
		{"visible: «сервисными аккаунтами и ключами» for an agent (conclusion 100.3)", seen, strings.Contains(seen, "сервисными аккаунтами и ключами")},
		{"visible: the secret does not pass the browser and is never shown (R-47)", seen, strings.Contains(seen, "Секрет ключа не проходит через браузер и нигде не показывается")},
		{"«любые ресурсы проекта» once in the terminal (conclusion 26.3)", term, strings.Count(term, "любые ресурсы проекта") == 1},
		{"hint: revoking — the key on this computer (conclusion 26.2)", hints, strings.Contains(hints, "ключ будет удален") && strings.Contains(hints, "этом компьютере")},
		{"hint: revoking — the role in the personal account (conclusion 26.2)", hints, strings.Contains(hints, "Роль снимается в личном кабинете")},
		{"no «снимается сама»: not checked live (conclusion 26.1, R-50)", seen + hints, !strings.Contains(seen+hints, "снимается сама")},
		{"no command on the page (conclusion 26.2)", seen + hints, !strings.Contains(seen+hints, "evo logout")},
		{"the grant is the page's form with its token (R-47)", r.b.consent, consentField.MatchString(r.b.consent) &&
			regexp.MustCompile(`(?s)<form[^>]*method="post"[^>]*action="/grant"|<form[^>]*action="/grant"[^>]*method="post"`).MatchString(r.b.consent)},
	} {
		if !c.ok {
			t.Errorf("%s:\n%.600s", c.what, c.in)
		}
	}
}
